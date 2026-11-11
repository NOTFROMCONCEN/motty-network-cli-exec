package gate

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// opsBackend is a network CLI backend whose command outputs a test can name.
type opsBackend struct {
	fakeBackend
	outputs map[string]string
}

func (b *opsBackend) Sessions(context.Context) ([]SessionInfo, error) {
	return []SessionInfo{
		{ID: "2", Title: "sw", Address: "sw:23", Ready: true, ExecMode: ExecModeNetworkCLI, Platform: "huawei_vrp"},
		{ID: "4", Title: "sw2", Address: "sw2:23", Ready: true, ExecMode: ExecModeNetworkCLI, Platform: "huawei_vrp"},
		{ID: "1", Title: "web", Address: "web:22", Ready: true, ExecMode: ExecModeShell},
		{ID: "3", Title: "fw", Address: "fw:23", Ready: true, ExecMode: ExecModeNetworkCLI, Platform: "generic"},
	}, nil
}

func (b *opsBackend) Exec(_ context.Context, a tools.TerminalExecArgs) (ExecResult, error) {
	b.execs.Add(1)
	b.lastCommand = a.Command
	return ExecResult{Output: b.outputs[a.Command], Status: "prompt_restored"}, nil
}

func opsGate(backend Backend, asked *atomic.Int32) Gate {
	return Gate{
		Backend: backend,
		Policy:  risk.Policy{Enabled: true, AutoUpTo: risk.Low},
		Approver: func(context.Context, Request) (bool, string, error) {
			if asked != nil {
				asked.Add(1)
			}
			return true, "", nil
		},
	}
}

func TestDeviceFactsRunsThePlatformVersionCommand(t *testing.T) {
	backend := &opsBackend{outputs: map[string]string{
		"display version": "Huawei Versatile Routing Platform Software\n" +
			"VRP (R) software, Version 5.170 (S5720 V200R019C10SPC500)\n" +
			"HUAWEI S5720-28X-SI-AC Routing Switch uptime is 123 days, 4 hours, 5 minutes\n",
	}}
	var asked atomic.Int32
	res := opsGate(backend, &asked).Run(context.Background(), tools.NameDeviceFacts, []byte(`{"session":"2"}`))
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{"platform: huawei_vrp", "version: 5.170", "model: S5720-28X-SI-AC", "uptime: 123 days"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("result missing %q:\n%s", want, res.Text)
		}
	}
	if backend.lastCommand != "display version" || asked.Load() != 0 {
		t.Fatalf("command = %q, asked = %d", backend.lastCommand, asked.Load())
	}
}

func TestDeviceFactsRefusesAShellOrGenericSession(t *testing.T) {
	backend := &opsBackend{}
	g := opsGate(backend, nil)
	for _, session := range []string{"1", "3"} {
		res := g.Run(context.Background(), tools.NameDeviceFacts, []byte(`{"session":"`+session+`"}`))
		if !res.IsError || backend.execs.Load() != 0 {
			t.Fatalf("session %s: %+v, execs %d", session, res, backend.execs.Load())
		}
	}
}

func TestInterfaceStatusParsesAndFilters(t *testing.T) {
	backend := &opsBackend{outputs: map[string]string{
		"display interface brief": "Interface                   PHY     Protocol  InUti OutUti   inErrors  outErrors\n" +
			"GigabitEthernet0/0/1        up      up           0%     0%          0          0\n" +
			"GigabitEthernet0/0/2        down    down         0%     0%          0          0\n",
	}}
	res := opsGate(backend, nil).Run(context.Background(), tools.NameInterfaceStatus,
		[]byte(`{"session":"2","interface":"GigabitEthernet0/0/1","include_down":false}`))
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{"interfaces: 1", "up 1", "GigabitEthernet0/0/1", "PHY"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("result missing %q:\n%s", want, res.Text)
		}
	}
	if strings.Contains(res.Text, "GigabitEthernet0/0/2") {
		t.Fatalf("the filtered row leaked:\n%s", res.Text)
	}
}

func TestConfigDiffIsReadOnly(t *testing.T) {
	backend := &opsBackend{}
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true}, Mode: ModeObserve}
	res := g.Run(context.Background(), tools.NameConfigDiff, []byte(`{"before":"a\nb","after":"a\nc"}`))
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{"added: 1", "removed: 1", "-b", "+c", "before_sha256:"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("result missing %q:\n%s", want, res.Text)
		}
	}
	if backend.execs.Load() != 0 {
		t.Fatal("config_diff ran a command")
	}
}

func TestGroupRunRefusesADangerousCommand(t *testing.T) {
	backend := &opsBackend{}
	res := opsGate(backend, nil).Run(context.Background(), tools.NameGroupRun,
		[]byte(`{"targets":[{"session":"2","commands":["reboot"]}]}`))
	if !res.IsError || !strings.Contains(res.Text, "not a recognized read-only") {
		t.Fatalf("result = %+v", res)
	}
	if backend.execs.Load() != 0 {
		t.Fatal("a dangerous command reached the backend")
	}
}

func TestGroupRunRunsReadOnlyCommandsWithOneApproval(t *testing.T) {
	backend := &opsBackend{outputs: map[string]string{
		"display version": "Version 5.170\n",
		"display clock":   "2026-11-06 10:00:00\n",
	}}
	var asked atomic.Int32
	res := opsGate(backend, &asked).Run(context.Background(), tools.NameGroupRun,
		[]byte(`{"targets":[{"session":"2","commands":["display version","display clock"]},{"session":"4","commands":["display clock"]}]}`))
	if res.IsError {
		t.Fatalf("result = %+v", res)
	}
	if asked.Load() != 1 || backend.execs.Load() != 3 {
		t.Fatalf("asked = %d, execs = %d", asked.Load(), backend.execs.Load())
	}
	for _, want := range []string{"== sw (2) ==", "== sw2 (4) ==", "$ display version", "2026-11-06"} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("result missing %q:\n%s", want, res.Text)
		}
	}
}

func TestGroupRunDeclinedRunsNothing(t *testing.T) {
	backend := &opsBackend{}
	g := Gate{Backend: backend, Policy: risk.Policy{Enabled: true},
		Approver: func(context.Context, Request) (bool, string, error) { return false, "", nil }}
	res := g.Run(context.Background(), tools.NameGroupRun,
		[]byte(`{"targets":[{"session":"2","commands":["display version"]}]}`))
	if !res.Denied || backend.execs.Load() != 0 {
		t.Fatalf("result = %+v, execs %d", res, backend.execs.Load())
	}
}

func TestGroupRunRefusesAShellOrGenericTarget(t *testing.T) {
	backend := &opsBackend{}
	g := opsGate(backend, nil)
	for _, session := range []string{"1", "3"} {
		res := g.Run(context.Background(), tools.NameGroupRun,
			[]byte(`{"targets":[{"session":"`+session+`","commands":["display version"]}]}`))
		if !res.IsError || backend.execs.Load() != 0 {
			t.Fatalf("session %s: %+v, execs %d", session, res, backend.execs.Load())
		}
	}
}

// maipuOpsBackend keeps the MyPower session out of the other tests' session
// list while still using the same output map and command recorder.
type maipuOpsBackend struct{ opsBackend }

func (b *maipuOpsBackend) Sessions(context.Context) ([]SessionInfo, error) {
	return []SessionInfo{
		{ID: "5", Title: "mp", Address: "mp:23", Ready: true, ExecMode: ExecModeNetworkCLI, Platform: "maipu_mypower"},
	}, nil
}

// The read tools take their command straight from the platform table. A
// MyPower session therefore runs MyPower's own words, and the read is rated
// Low instead of asking the user as it would have on the generic table.
func TestMaipuReadToolsUseMyPowerCommands(t *testing.T) {
	backend := &maipuOpsBackend{opsBackend{outputs: map[string]string{
		"show version": "MyPower (R) Operating System Software\n" +
			"RM1800-35E-AC(V2) system image file (ge0: vmlinux), version 7.1.0.35(integrity)\n" +
			"Hardware Model : RM1800-35E-AC(V2) with 512 MBytes SDRAM, 4096 MBytes flash\n" +
			"Software Version : 7.1.0.35(integrity)\n" +
			"System Uptime is 0 hour 6 minutes 23 seconds\n",
		"show ip interface brief": "Interface IP-Address Status Protocol Description\n" +
			"gigabitethernet0 130.255.20.29 up up\n" +
			"gigabitethernet2 unassigned down down\n",
	}}}
	var asked atomic.Int32
	g := opsGate(backend, &asked)

	res := g.Run(context.Background(), tools.NameDeviceFacts, []byte(`{"session":"5"}`))
	if res.IsError {
		t.Fatalf("device_facts = %+v", res)
	}
	for _, want := range []string{
		"platform: maipu_mypower", "Maipu MyPower",
		"version: 7.1.0.35(integrity)", "model: RM1800-35E-AC(V2)",
		"uptime: 0 hour 6 minutes 23 seconds",
	} {
		if !strings.Contains(res.Text, want) {
			t.Fatalf("result missing %q:/n%s", want, res.Text)
		}
	}
	if backend.lastCommand != "show version" || asked.Load() != 0 {
		t.Fatalf("command = %q, asked = %d; want the version command with no approval", backend.lastCommand, asked.Load())
	}

	res = g.Run(context.Background(), tools.NameInterfaceStatus, []byte(`{"session":"5"}`))
	if res.IsError {
		t.Fatalf("interface_status = %+v", res)
	}
	if backend.lastCommand != "show ip interface brief" || asked.Load() != 0 {
		t.Fatalf("command = %q, asked = %d; want the interface command with no approval", backend.lastCommand, asked.Load())
	}
	if !strings.Contains(res.Text, "gigabitethernet0") {
		t.Fatalf("interface rows missing:/n%s", res.Text)
	}
}

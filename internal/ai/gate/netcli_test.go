package gate

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
)

// cliBackend is a fakeBackend whose sessions carry execution modes, so the
// gate's routing by mode can be tested without a window. result replaces the
// default outcome when a test needs a specific one.
type cliBackend struct {
	fakeBackend
	result *ExecResult
}

func (b *cliBackend) Exec(_ context.Context, a tools.TerminalExecArgs) (ExecResult, error) {
	b.execs.Add(1)
	b.lastCommand = a.Command
	if b.result != nil {
		return *b.result, nil
	}
	// Most sessions in this file are network CLI ones, whose commands report
	// completion rather than an exit status.
	return ExecResult{Output: "out", Status: "prompt_restored"}, nil
}

func (b *cliBackend) Sessions(context.Context) ([]SessionInfo, error) {
	return []SessionInfo{
		{ID: "1", Title: "web", Address: "web:22", Active: true, Ready: true, ExecMode: ExecModeShell},
		{ID: "2", Title: "sw", Address: "sw:23", Ready: true, ExecMode: ExecModeNetworkCLI, Platform: "huawei_vrp"},
		{ID: "3", Title: "fw", Address: "fw:23", Ready: true, ExecMode: ExecModeNetworkCLI, Platform: "generic"},
	}, nil
}

func cliGate(t *testing.T, backend Backend, asked *atomic.Int32) Gate {
	t.Helper()
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

func TestCLIRunsAreRatedByMode(t *testing.T) {
	var asked atomic.Int32
	backend := &cliBackend{}
	g := cliGate(t, backend, &asked)

	// A read-only device command on a network CLI session runs without asking.
	res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"display version"}`))
	if res.IsError || res.Denied || asked.Load() != 0 {
		t.Fatalf("display version: %+v asked %d", res, asked.Load())
	}
	if !strings.Contains(res.Text, "[device prompt restored") {
		t.Fatalf("completion not reported: %q", res.Text)
	}

	// An unrecognized device command asks, even on the auto policy.
	res = g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"undo everything"}`))
	if asked.Load() != 1 {
		t.Fatalf("unknown device command asked %d times", asked.Load())
	}
	if res.IsError {
		t.Fatalf("the approved command should have run: %+v", res)
	}

	// A multi-line device command never runs unattended.
	res = g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"display version\nsystem-view"}`))
	if asked.Load() != 2 || res.IsError {
		t.Fatalf("multi-line: %+v asked %d", res, asked.Load())
	}

	// Shell text is still parsed like a shell on shell sessions.
	backend.execs.Store(0)
	res = g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"1","command":"ls -la"}`))
	if res.IsError || asked.Load() != 2 {
		t.Fatalf("shell ls: %+v asked %d", res, asked.Load())
	}
}

func TestCLIErrorIsReportedAsAnErrorResult(t *testing.T) {
	result := ExecResult{Output: "Error: Unrecognized command", CLIError: "Error: Unrecognized command", Status: "prompt_restored"}
	backend := &cliBackend{result: &result}
	g := cliGate(t, backend, nil)
	res := g.Run(context.Background(), tools.NameTerminalExec, []byte(`{"session":"2","command":"display xyz"}`))
	if !res.IsError || !strings.Contains(res.Text, "[device CLI error:") {
		t.Fatalf("CLI error not surfaced: %+v", res)
	}
}

func TestExecManyRefusesMixedModes(t *testing.T) {
	var asked atomic.Int32
	backend := &cliBackend{}
	g := cliGate(t, backend, &asked)
	res := g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["1","2"],"command":"display version"}`))
	if !res.IsError || !strings.Contains(res.Text, "mixes network CLI sessions") {
		t.Fatalf("mixed batch: %+v", res)
	}
	if asked.Load() != 0 {
		t.Fatalf("a mixed batch asked %d times", asked.Load())
	}
}

func TestExecManyAllCLIRatesAgainstEveryPlatform(t *testing.T) {
	var asked atomic.Int32
	backend := &cliBackend{}
	g := cliGate(t, backend, &asked)
	// A batch of network CLI sessions: a read-only command is at least medium
	// (it runs on several machines) and asks once for the whole batch.
	res := g.Run(context.Background(), tools.NameTerminalExecMany, []byte(`{"sessions":["2","3"],"command":"display version"}`))
	if res.IsError || asked.Load() != 1 {
		t.Fatalf("all-CLI batch: %+v asked %d", res, asked.Load())
	}
	if !strings.Contains(res.Text, "[device prompt restored") {
		t.Fatalf("completion not reported per session: %q", res.Text)
	}
}

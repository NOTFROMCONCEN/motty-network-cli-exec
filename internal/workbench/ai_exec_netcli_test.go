package workbench

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"motty/internal/netcli"
)

// cliTestSession is a connected session whose host is pinned to the network
// CLI with the Huawei VRP table, sitting at a ready device prompt.
func cliTestSession(t *testing.T, mode, platform string) (*State, *fakeShell, *Session) {
	t.Helper()
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return nil }}
	state, shell := assistantTestSession(t, model)
	session := &state.sessions[0]
	session.host.ExecMode = mode
	session.host.CLIPlatform = platform
	return state, shell, session
}

func seedPrompt(t *testing.T, state *State, shell *fakeShell, prompt string) {
	t.Helper()
	shell.say(prompt)
	state.Wake()
}

// awaitExec runs the wait in the background and keeps waking the window thread
// until it returns: a network CLI command ends on the window thread (the
// quiet-window timer posts there), so the wait cannot finish without wakes.
func awaitExec(t *testing.T, state *State, exec *aiExec, timeout time.Duration) gate.ExecResult {
	t.Helper()
	delivered := make(chan gate.ExecResult, 1)
	go func() { delivered <- state.aiExecWait(exec, timeout, nil) }()
	var result gate.ExecResult
	got := false
	pumpUntil(t, state, "the command wait to return", func() bool {
		if got {
			return true
		}
		select {
		case result = <-delivered:
			got = true
		default:
		}
		return got
	})
	return result
}

func waitExec(t *testing.T, state *State, exec *aiExec) gate.ExecResult {
	t.Helper()
	return awaitExec(t, state, exec, 20*time.Second)
}

// A device command cannot be a background job: it has no paged capture, and
// the quiet-window completion never reaches a job record, so the job would
// run forever.
func TestTerminalExecCLIRefusesBackgroundJobs(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")

	if _, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display version", Background: true}); err == nil {
		t.Fatal("a background device command was accepted")
	} else if !strings.Contains(err.Error(), "terminal_exec") {
		t.Fatalf("the refusal does not point at terminal_exec: %v", err)
	}
	if shell.typed() != "" {
		t.Fatalf("the refused command still typed %q", shell.typed())
	}
	if state.ai.jobs != nil && len(state.ai.jobs) > 0 {
		t.Fatalf("the refusal left %d jobs behind", len(state.ai.jobs))
	}
}

func TestTerminalExecCLISendsRawCommand(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")

	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display version"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	typed := shell.typed()
	if typed != "display version\r" {
		t.Fatalf("the device received %q; want the raw command line only", typed)
	}
	if strings.ContainsAny(typed, "{}$") || strings.Contains(typed, "GOPAI_") || strings.Contains(typed, "printf") {
		t.Fatalf("shell wrapping reached the device: %q", typed)
	}

	shell.say("<Huawei>display version\r\nVRP (R) software, Version 8.180\r\n<Huawei>")
	result := waitExec(t, state, exec)
	if result.TimedOut || result.ExitKnown || result.Status != "prompt_restored" {
		t.Fatalf("result %+v", result)
	}
	if result.Output != "VRP (R) software, Version 8.180" {
		t.Fatalf("output %q", result.Output)
	}
}

func TestTerminalExecCLIRefusesMultiline(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	if _, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display version\nsystem-view"}); err != errCLIOneLine {
		t.Fatalf("multiline command: %v", err)
	}
	if _, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display version\rmore"}); err != errCLIOneLine {
		t.Fatalf("CR in command: %v", err)
	}
	if shell.typed() != "" {
		t.Fatalf("a refused command was typed: %q", shell.typed())
	}
}

func TestTerminalExecCLIRefusesWithoutPrompt(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	// The terminal shows a login prompt, not a ready device CLI prompt.
	seedPrompt(t, state, shell, "Username:")
	if _, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display version"}); err != errCLINoPrompt {
		t.Fatalf("login prompt: %v", err)
	}
	if shell.typed() != "" {
		t.Fatalf("a command was typed without a prompt: %q", shell.typed())
	}
}

func TestTerminalExecCLIPagerContinues(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display current-configuration"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	shell.say("<Huawei>display current-configuration\r\nsysname Huawei\r\n  ---- More ----")
	pumpUntil(t, state, "the page key to be typed", func() bool {
		return strings.Contains(shell.typed(), "display current-configuration\r ") &&
			strings.HasSuffix(shell.typed(), " ")
	})
	shell.say("\r\n#\r\n<Huawei>")
	result := waitExec(t, state, exec)
	// The first page's line is kept, the pager text itself is not output, and
	// the page typed past the prompt is; the prompt line is not.
	if result.Status != "prompt_restored" || result.Output != "sysname Huawei\n#" {
		t.Fatalf("result %+v", result)
	}
}

func TestTerminalExecCLITimeoutDoesNotInterruptTheDevice(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display log"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	shell.say("<Huawei>display log\r\nstill printing\r\n")
	result := awaitExec(t, state, exec, 150*time.Millisecond)
	if !result.TimedOut || result.ExitKnown || result.Status != "timeout" {
		t.Fatalf("result %+v", result)
	}
	if strings.ContainsRune(shell.typed(), 3) {
		t.Fatal("an interrupt byte was sent to the device on timeout")
	}
}

func TestTerminalExecCLIUserCancel(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display log"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	shell.say("<Huawei>display log\r\nstill printing\r\n")
	state.Wake()
	// The user takes the terminal back with Ctrl-C; the byte goes to the device.
	if !session.term.send([]byte{3}) {
		t.Fatal("the terminal refused the interrupt")
	}
	result := waitExec(t, state, exec)
	if result.TimedOut || result.Status != "cancelled" {
		t.Fatalf("result %+v", result)
	}
	if !strings.ContainsRune(shell.typed(), 3) {
		t.Fatal("the user's Ctrl-C never reached the device")
	}
}

func TestTerminalExecCLIDisconnect(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display log"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	shell.say("<Huawei>display log\r\nstill printing\r\n")
	state.Wake()
	shell.end(nil)
	state.Wake()
	result := waitExec(t, state, exec)
	if result.TimedOut || result.Status != "disconnected" {
		t.Fatalf("result %+v", result)
	}
}

func TestTerminalExecShellPathUnchanged(t *testing.T) {
	state, shell, _ := cliTestSession(t, "", "") // an SSH host defaults to the shell
	seedPrompt(t, state, shell, "$ ")

	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(state.sessions[0].ID), Command: "echo hi"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	typed := shell.typed()
	if !strings.Contains(typed, "{ echo hi") || !strings.Contains(typed, "GOPAI_") {
		t.Fatalf("the shell capture wrapper is gone: %q", typed)
	}
	nonce := regexp.MustCompile(`GOPAI_ ([0-9a-f]{16})`).FindStringSubmatch(typed)
	if nonce == nil {
		t.Fatalf("no nonce in %q", typed)
	}
	echo := strings.ReplaceAll(typed, "\n", "\r\n")
	shell.say(echo + "hi\r\n\r\nGOPAI_" + nonce[1] + "0\r\n")
	result := waitExec(t, state, exec)
	if !result.ExitKnown || result.ExitCode != 0 || result.Output != "hi" || result.TimedOut {
		t.Fatalf("result %+v", result)
	}
}

func TestTerminalExecCLIGenericPlatformAcceptsAnyPrompt(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "")
	seedPrompt(t, state, shell, "device>")
	// The generic table makes no promise about prompt shapes: it accepts the
	// line and completes only on that exact line.
	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "show clock"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	if shell.typed() != "show clock\r" {
		t.Fatalf("typed %q", shell.typed())
	}
	shell.say("12:00:00\r\ndevice>")
	result := waitExec(t, state, exec)
	if result.Status != "prompt_restored" {
		t.Fatalf("result %+v", result)
	}
}

func TestTerminalExecCLIRefusesALoginPromptOnGenericPlatform(t *testing.T) {
	// The generic table cannot prove a line is a device prompt, so it refuses
	// the one that would be worst to type at: a sign-in question.
	state, shell, session := cliTestSession(t, "network_cli", "")
	for _, prompt := range []string{"Username:", "Password:", "localhost login:"} {
		seedPrompt(t, state, shell, prompt)
		if _, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "show clock"}); err != errCLINoPrompt {
			t.Fatalf("%q: %v", prompt, err)
		}
	}
	if shell.typed() != "" {
		t.Fatalf("a command was typed into a login prompt: %q", shell.typed())
	}
}

func TestExecModeResolutionAndPersistence(t *testing.T) {
	if got := resolveExecMode(Host{Protocol: protocolSSH}); got != shellMode {
		t.Fatalf("SSH default = %v", got)
	}
	if got := resolveExecMode(Host{Protocol: protocolTelnet}); got != cliMode {
		t.Fatalf("Telnet default = %v", got)
	}
	if got := resolveExecMode(Host{Protocol: protocolSerial}); got != cliMode {
		t.Fatalf("serial default = %v", got)
	}
	if got := resolveExecMode(Host{Protocol: protocolTelnet, ExecMode: "shell"}); got != shellMode {
		t.Fatalf("explicit shell on Telnet = %v", got)
	}
	if got := resolveExecMode(Host{Protocol: protocolSSH, ExecMode: "network_cli"}); got != cliMode {
		t.Fatalf("explicit CLI on SSH = %v", got)
	}

	// The saved transport round-trips the setting.
	host := Host{Protocol: protocolSSH, ExecMode: "network_cli", CLIPlatform: "huawei_vrp"}
	saved := host.transport()
	if saved.ExecMode != "network_cli" || saved.CLIPlatform != "huawei_vrp" {
		t.Fatalf("saved transport %+v", saved)
	}
	var back Host
	if !back.applyTransport(saved) {
		t.Fatal("applyTransport refused its own output")
	}
	if back.ExecMode != "network_cli" || back.CLIPlatform != "huawei_vrp" {
		t.Fatalf("restored host %+v", back)
	}

	// "generic" is a real choice now and keeps its spelling; an unknown but
	// well-formed id is kept so a missing user platform file cannot drop a
	// host, while a malformed id is still refused.
	host.CLIPlatform = "generic"
	if saved := host.transport(); saved.CLIPlatform != "generic" {
		t.Fatalf("generic saved as %q", saved.CLIPlatform)
	}
	var bad Host
	if bad.applyTransport(savedTransport{ExecMode: "bogus"}) {
		t.Fatal("an unknown mode was accepted")
	}
	if !bad.applyTransport(savedTransport{ExecMode: "shell", CLIPlatform: "nope"}) || bad.CLIPlatform != "nope" {
		t.Fatal("a well-formed unknown platform must be kept until its table returns")
	}
	if bad.applyTransport(savedTransport{ExecMode: "shell", CLIPlatform: "Not A Platform"}) {
		t.Fatal("a malformed platform id was accepted")
	}

	// The desktops have no terminal to configure.
	var desk Host
	if !desk.applyTransport(savedTransport{Protocol: "vnc", ExecMode: "network_cli"}) || desk.ExecMode != "" {
		t.Fatal("a VNC host took an execution mode")
	}
}

func TestExecSettingSurvivesAFullSaveAndLoad(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "state.json")
	// An old entry without the setting keeps its shell behavior; the new
	// setting is saved and read back. A well-formed platform whose table is not
	// loaded is kept too: deleting a user platform file must not drop hosts.
	err := saveState(options.StatePath, savedState{Hosts: []savedHost{
		{Label: "Old", Address: "old.example.com", User: "me"},
		{Label: "Sw", Address: "sw.example.com", User: "me", savedTransport: savedTransport{
			Protocol: "telnet", ExecMode: "network_cli", CLIPlatform: "huawei_vrp",
		}},
		{Label: "Linux", Address: "linux.example.com", User: "me", savedTransport: savedTransport{
			ExecMode: "shell",
		}},
		{Label: "Broken", Address: "broken.example.com", User: "me", savedTransport: savedTransport{
			Protocol: "telnet", ExecMode: "network_cli", CLIPlatform: "not_a_platform",
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	byLabel := map[string]Host{}
	for _, host := range state.hosts {
		byLabel[host.Label] = host
	}
	if len(state.hosts) != 4 {
		t.Fatalf("hosts = %+v; a well-formed unknown platform must not drop a host", state.hosts)
	}
	if host := byLabel["Old"]; host.ExecMode != "" || host.CLIPlatform != "" {
		t.Fatalf("an old entry gained a setting: %+v", host)
	}
	if host := byLabel["Sw"]; host.ExecMode != "network_cli" || host.CLIPlatform != "huawei_vrp" {
		t.Fatalf("the network CLI entry lost its setting: %+v", host)
	}
	if host := byLabel["Linux"]; host.ExecMode != "shell" || host.CLIPlatform != "" {
		t.Fatalf("the shell entry lost its setting: %+v", host)
	}
	if host := byLabel["Broken"]; host.CLIPlatform != "not_a_platform" {
		t.Fatalf("the unknown platform lost its id: %+v", host)
	}

	// Saving again keeps the generic spelling on disk.
	state.hosts = append(state.hosts, Host{
		Label: "Gen", Address: "gen.example.com", User: "me",
		Protocol: protocolTelnet, ExecMode: "network_cli", CLIPlatform: netcli.GenericID,
	})
	state.save()
	raw, err := os.ReadFile(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	loaded := newTestState(t, options)
	for _, host := range loaded.hosts {
		if host.Label == "Gen" && (host.ExecMode != "network_cli" || host.CLIPlatform != netcli.GenericID) {
			t.Fatalf("the generic platform was saved as %q", host.CLIPlatform)
		}
	}
	if !strings.Contains(string(raw), `"exec_mode":"network_cli"`) {
		t.Fatalf("the setting is not in the file: %s", raw)
	}
}

func TestSessionExecModeNames(t *testing.T) {
	_, _, session := cliTestSession(t, "", "")
	if session.execModeName() != gate.ExecModeShell || session.cliPlatformName() != "" {
		t.Fatalf("SSH session reports %q/%q", session.execModeName(), session.cliPlatformName())
	}
	session.host.ExecMode = "network_cli"
	session.host.CLIPlatform = "huawei_vrp"
	if session.execModeName() != gate.ExecModeNetworkCLI || session.cliPlatformName() != "huawei_vrp" {
		t.Fatalf("CLI session reports %q/%q", session.execModeName(), session.cliPlatformName())
	}
	session.host.CLIPlatform = "lost_platform"
	if session.cliPlatformName() != netcli.GenericID {
		t.Fatalf("a stale platform id reads as %q", session.cliPlatformName())
	}
}

func TestListSessionsShowsTheExecutionMode(t *testing.T) {
	state, shell, _ := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	backend := &aiBackend{state: state}
	// Sessions reads the window's state, so it runs on the window thread.
	type reply struct {
		sessions []gate.SessionInfo
		err      error
	}
	delivered := make(chan reply, 1)
	go func() {
		sessions, err := backend.Sessions(context.Background())
		delivered <- reply{sessions, err}
	}()
	var got reply
	done := false
	pumpUntil(t, state, "list_sessions to answer", func() bool {
		if done {
			return true
		}
		select {
		case got = <-delivered:
			done = true
		default:
		}
		return done
	})
	if got.err != nil {
		t.Fatalf("Sessions: %v", got.err)
	}
	if len(got.sessions) != 1 || got.sessions[0].ExecMode != gate.ExecModeNetworkCLI || got.sessions[0].Platform != "huawei_vrp" {
		t.Fatalf("sessions %+v", got.sessions)
	}
}

func TestBeginCLIExecStampsTheSettingFloor(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	state.ai.settings.cliTimeoutSeconds = 120
	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "display version"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	if exec.cliWait != 120*time.Second {
		t.Fatalf("cliWait = %v, want the 2m floor", exec.cliWait)
	}
}

func TestBeginShellExecKeepsNoFloor(t *testing.T) {
	state, _, session := cliTestSession(t, "", "")
	state.ai.settings.cliTimeoutSeconds = 120
	exec, err := state.beginExec(tools.TerminalExecArgs{Session: sessionHandle(session.ID), Command: "echo hello"})
	if err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	if exec.cliWait != 0 {
		t.Fatalf("a shell command got the CLI floor: %v", exec.cliWait)
	}
}

func TestEffectiveWaitRaisesToTheFloor(t *testing.T) {
	floored := &aiExec{cliWait: 2 * time.Minute}
	if got := floored.effectiveWait(30 * time.Second); got != 2*time.Minute {
		t.Fatalf("30s under a 2m floor = %v", got)
	}
	if got := floored.effectiveWait(5 * time.Minute); got != 5*time.Minute {
		t.Fatalf("5m over a 2m floor = %v", got)
	}
	if got := (&aiExec{}).effectiveWait(time.Second); got != time.Second {
		t.Fatalf("without a floor the call kept %v", got)
	}
}

func TestBackendExecWaitUsesTheCLIFloor(t *testing.T) {
	state, shell, session := cliTestSession(t, "network_cli", "huawei_vrp")
	seedPrompt(t, state, shell, "<Huawei>")
	state.ai.settings.cliTimeoutSeconds = 2 // a floor above the call's own one second
	backend := &aiBackend{state: state}
	type reply struct {
		result gate.ExecResult
		err    error
	}
	delivered := make(chan reply, 1)
	started := time.Now()
	go func() {
		result, err := backend.Exec(context.Background(), tools.TerminalExecArgs{
			Session: sessionHandle(session.ID), Command: "display log", TimeoutSeconds: 1,
		})
		delivered <- reply{result, err}
	}()
	var got reply
	done := false
	pumpUntil(t, state, "the backend wait to give up", func() bool {
		if done {
			return true
		}
		select {
		case got = <-delivered:
			done = true
		default:
		}
		return done
	})
	if got.err != nil {
		t.Fatalf("Exec: %v", got.err)
	}
	if !got.result.TimedOut || got.result.Status != "timeout" {
		t.Fatalf("result %+v", got.result)
	}
	if elapsed := time.Since(started); elapsed < 1500*time.Millisecond {
		t.Fatalf("the wait gave up after %v; the 2s floor did not apply", elapsed)
	}
}

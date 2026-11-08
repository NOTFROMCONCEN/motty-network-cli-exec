package workbench

import (
	"errors"
	"strings"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/netcli"
)

// The network CLI backend of terminal_exec. A device CLI understands neither
// the shell capture wrapper nor exit statuses: one command goes in as the
// device expects it, and the command is over when the device prompt, captured
// before the command was sent, is seen again at a line boundary. The terminal
// shows exactly what the device sent back, so the model's command and the
// device's answer stay visible like any user's session.

var (
	errCLIOneLine  = errors.New("a network CLI session takes one command line; send each line as its own terminal_exec call")
	errCLINoPrompt = errors.New("no device prompt is visible in this terminal yet; wait until the device shows one (for example after its login banner) before running commands")
	errCLINoJobs   = errors.New("a network CLI session has no background jobs; run the command with terminal_exec and wait for its answer")
)

// cliWaitFloor is the smallest wait a network CLI command gets, from the
// assistant's Net CLI timeout setting; 0 keeps whatever the call asked for.
func cliWaitFloor(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// effectiveWait raises the call's requested wait to the command's floor. A
// device command ends when its prompt comes back, so the wait is only a
// ceiling: a floor never delays a finished command, it only keeps a slow or
// paging device from being given up on too early.
func (exec *aiExec) effectiveWait(requested time.Duration) time.Duration {
	if exec.cliWait > requested {
		return exec.cliWait
	}
	return requested
}

// beginCLIExec builds the collector for one raw device command. It refuses
// anything that is not a single command line and any session whose terminal
// does not currently show a recognizable device prompt: both would send the
// device something nobody could match later. The floor stamps the command's
// smallest wait (see effectiveWait).
func beginCLIExec(session *Session, t *terminal, args tools.TerminalExecArgs, floor time.Duration) (*aiExec, []byte, error) {
	if args.Background {
		// A background job polls its output page by page from the shell
		// capture; a device command has no such capture, and its quiet-window
		// completion never reaches the job record, so the job would run
		// forever. Run device commands with terminal_exec instead.
		return nil, nil, errCLINoJobs
	}
	command := strings.TrimSpace(args.Command)
	if command == "" || strings.ContainsAny(command, "\n\r\t") || strings.IndexByte(command, 0) >= 0 {
		return nil, nil, errCLIOneLine
	}
	// The prompt is what the cursor's line holds up to the cursor, not the
	// whole row: a command whose output is still arriving leaves the cursor
	// mid-line, and only the text before it can be a prompt.
	prompt := t.consolePrompt()
	plat := session.host.cliPlatform()
	if prompt == "" {
		return nil, nil, errCLINoPrompt
	}
	// A platform that knows its prompt shape proves the terminal is at a ready
	// device prompt. The generic table cannot, so it refuses only the line it
	// must: a sign-in question, where a command would be typed as an account or
	// a password.
	if plat.Shaped() {
		if _, ok := plat.MatchPrompt(prompt); !ok {
			return nil, nil, errCLINoPrompt
		}
	} else if netcli.LooksLikeLoginPrompt(prompt) {
		return nil, nil, errCLINoPrompt
	}
	tap := netcli.NewTap(plat, prompt, command, maxExecOutput)
	tap.SetSend(func(p []byte) bool { return t.sendProtocol(p) })
	send := append([]byte(command), '\r')
	return &aiExec{t: t, tap: cliTap{tap}, cli: tap, cliWait: floor, done: make(chan gate.ExecResult, 1)}, send, nil
}

// sendCLI types one raw command line into a device CLI session. Unlike the
// shell capture wrapper, the bytes are what the terminal displays and what the
// device records, so they go into the session's recording like a user's own
// input; command history, autofill and the input guard stay out of it.
func (t *terminal) sendCLI(p []byte) bool {
	if t.shell == nil || len(p) == 0 {
		return false
	}
	t.selection.clear()
	t.screen.ScrollToBottom()
	accepted := t.shell.Write(p)
	if accepted {
		t.inputSent = true
		t.recording.append(t.recordingNow(), terminalRecordingInput, p)
	}
	return accepted
}

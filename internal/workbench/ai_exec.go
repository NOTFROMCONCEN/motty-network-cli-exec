package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"

	"motty/internal/ai/capture"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/netcli"
)

// aiExec is one command the assistant typed into a visible terminal and is
// waiting to see finish. A terminal has at most one.
type aiExec struct {
	t    *terminal
	tap  execTap
	cli  *netcli.Tap          // the network CLI collector; nil on the shell path
	done chan gate.ExecResult // buffered; receives exactly once
	// confirm is the quiet-window timer behind a candidate prompt (see
	// netcli.Tap.Pending); nil while the shell path runs or output keeps
	// arriving.
	confirm *time.Timer
}

// execTap is the per-command collector the pump feeds shell output through: it
// filters what the terminal shows and reports the command's output and how it
// ended. The shell capture and the network CLI collector both satisfy it.
type execTap interface {
	Feed(p []byte) (display []byte, done bool)
	// Abort releases what the collector still holds back from the display, so
	// the terminal keeps everything after the wait ended.
	Abort() (display []byte)
	Result() gate.ExecResult
}

// shellTap adapts the POSIX capture Tap; the displayed bytes and the recorded
// output are the shell path's own, unchanged.
type shellTap struct{ tap *capture.Tap }

func (s shellTap) Feed(p []byte) ([]byte, bool) { return s.tap.Feed(p) }

func (s shellTap) Abort() []byte { return s.tap.Abort() }

func (s shellTap) Result() gate.ExecResult {
	r := s.tap.Result()
	return gate.ExecResult{Output: r.Output, ExitCode: r.ExitCode, Truncated: r.Truncated, ExitKnown: true}
}

// cliTap adapts the network CLI collector: everything the device sent is shown
// unchanged, and the result carries completion instead of an exit status.
type cliTap struct{ tap *netcli.Tap }

func (c cliTap) Feed(p []byte) ([]byte, bool) { return p, c.tap.Feed(p) }

func (cliTap) Abort() []byte { return nil }

func (c cliTap) Result() gate.ExecResult {
	r := c.tap.Result()
	return gate.ExecResult{
		Output:    r.Output,
		Truncated: r.Truncated,
		TimedOut:  r.Status == netcli.StatusTimeout,
		ExitKnown: false,
		Status:    r.Status.String(),
		CLIError:  r.CLIError,
	}
}

// maxExecOutput bounds the output kept for the model, whatever the terminal shows.
const maxExecOutput = 64 << 10

var (
	errNoSession  = errors.New("no such session; call list_sessions")
	errNoShell    = errors.New("the session has no open shell yet")
	errExecBusy   = errors.New("the terminal is already running another assistant command")
	errExecRefuse = errors.New("the terminal did not accept input")
)

// aiSession finds the session a tool call names: "" is the active one.
func (state *State) aiSession(handle string) (*Session, error) {
	if handle == "" {
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
			return session, nil
		}
		for index := range state.sessions {
			if t := state.sessions[index].term; t != nil && t.shell != nil {
				return &state.sessions[index], nil
			}
		}
		return nil, errNoSession
	}
	id, err := strconv.ParseUint(handle, 10, 32)
	if err != nil {
		return nil, errNoSession
	}
	session, ok := state.sessionByIDPointer(uint32(id))
	if !ok {
		return nil, errNoSession
	}
	return session, nil
}

// beginExec types the command into the session's terminal. Window thread. The
// session's execution mode picks the backend: a POSIX shell gets the capture
// wrapper as before, a network device gets its raw command line.
func (state *State) beginExec(args tools.TerminalExecArgs) (*aiExec, error) {
	session, err := state.aiSession(args.Session)
	if err != nil {
		return nil, err
	}
	t := session.term
	if t == nil || t.shell == nil || !t.live {
		return nil, errNoShell
	}
	if t.exec != nil {
		return nil, errExecBusy
	}
	var exec *aiExec
	var send []byte
	if session.sessionExecMode() == cliMode {
		if exec, send, err = beginCLIExec(session, t, args); err != nil {
			return nil, err
		}
	} else {
		exec, send, err = beginShellExec(t, args)
		if err != nil {
			return nil, err
		}
	}
	t.exec = exec
	if t.aiRows == nil {
		t.aiRows = map[uint64]struct{}{}
	}
	// The prompt row is where the assistant's command will be shown, so mark it
	// before the shell echoes anything. A line ID survives scrolling.
	line := t.screen.Row(t.screen.Cursor().Y).ID
	t.aiStartID = line
	t.aiRows[line] = struct{}{}
	// A CLI command is typed as the device expects it and the terminal shows
	// exactly that, so it goes into the session's recording like a user's own
	// input; the shell wrapper never does.
	sender := t.sendAssistant
	if exec.cli != nil {
		sender = t.sendCLI
	}
	if !sender(send) {
		delete(t.aiRows, line)
		t.exec = nil
		return nil, errExecRefuse
	}
	return t.exec, nil
}

// beginShellExec builds the shell capture for one command, exactly as before:
// the wrapped bytes are typed into the shell, and the tap hides the wrapper's
// echo and waits for the marker.
func beginShellExec(t *terminal, args tools.TerminalExecArgs) (*aiExec, []byte, error) {
	nonce := capture.NewNonce()
	wrapped := capture.Wrap(args.Command, nonce)
	tap := capture.NewTap(nonce, maxExecOutput)
	tap.SetEcho(wrapped)
	tap.SetDisplay(capture.DisplayCommand(args.Command))
	return &aiExec{t: t, tap: shellTap{tap}, done: make(chan gate.ExecResult, 1)}, wrapped, nil
}

// markAIBlock gives the assistant's command and its output rows the AI gutter
// mark. While the command runs, a line that is still being written is included;
// the final write ends before the shell prompt, which belongs to the next user
// action. Scrolling back leaves the visible rows untouched.
func (t *terminal) markAIBlock(final bool) {
	if t.aiRows == nil || t.aiStartID == 0 || t.screen.ViewOffset() > 0 {
		return
	}
	_, rows := t.screen.Size()
	cursor := t.screen.Cursor()
	end := cursor.Y
	if final && end > 0 {
		// The prompt is written before this method runs, in the same chunk or a
		// later one. Never leave its row marked as the assistant's command; the
		// next thing typed there is the user's.
		delete(t.aiRows, t.screen.Row(end).ID)
		end--
	}
	start := 0
	for row := 0; row < rows; row++ {
		if t.screen.Row(row).ID == t.aiStartID {
			start = row
			break
		}
	}
	for row := start; row <= end && row < rows; row++ {
		t.aiRows[t.screen.Row(row).ID] = struct{}{}
	}
}

// feedExec passes shell output through the running command's tap and returns
// what the terminal should show. When the command ended, it is finished.
func (t *terminal) feedExec(output []byte) []byte {
	display, done := t.exec.tap.Feed(output)
	if done {
		t.finishExec(false, false)
	}
	return display
}

// cliQuiet is how long a device must have stopped sending before a candidate
// prompt counts as the command's end (see netcli.Tap.Pending).
const cliQuiet = 400 * time.Millisecond

// watchCLIExec arms the quiet-window confirmation of a network CLI command
// after each output chunk: a candidate prompt is confirmed only once the
// device has stayed silent for cliQuiet, and retracted by any output in
// between. Window thread.
func (state *State) watchCLIExec(t *terminal) {
	exec := t.exec
	if exec == nil || exec.cli == nil {
		return
	}
	switch {
	case !exec.cli.Pending():
		if exec.confirm != nil {
			exec.confirm.Stop()
			exec.confirm = nil
		}
	case exec.confirm != nil:
		exec.confirm.Reset(cliQuiet)
	default:
		exec.confirm = time.AfterFunc(cliQuiet, func() {
			state.aiPost(func(*State) {
				if t.exec == exec && exec.cli.Confirm() {
					t.endConfirmed(exec)
				}
			})
		})
	}
}

// endConfirmed completes a network CLI command whose quiet window passed.
func (t *terminal) endConfirmed(exec *aiExec) {
	if t.exec != exec {
		return
	}
	t.exec = nil
	if exec.confirm != nil {
		exec.confirm.Stop()
		exec.confirm = nil
	}
	// The shell path finishes inside the pump, which marks the block right
	// after; this path ends on a timer, so it closes the mark itself.
	t.markAIBlock(true)
	exec.done <- exec.tap.Result()
}

// finishExec ends the running command: normally when its marker arrived, or
// because the wait timed out, was canceled, or the user pressed Ctrl-C. The
// command may still be running on the remote side then, so an interrupt is sent.
func (t *terminal) finishExec(timedOut, interrupt bool) {
	t.endExec(netcli.StatusCancelled, timedOut, interrupt)
}

// endExec is finishExec with a say in how a network CLI wait ended: a timeout
// and a cancellation read differently from a connection that died under the
// command. A shell command ignores the status; its result is its exit code.
func (t *terminal) endExec(cliStatus netcli.Status, timedOut, interrupt bool) {
	exec := t.exec
	if exec == nil {
		return
	}
	t.exec = nil
	if exec.confirm != nil {
		exec.confirm.Stop()
		exec.confirm = nil
	}
	if exec.cli != nil {
		// A network device gets no interrupt character from us: the same bytes
		// mean different things to different CLIs, so a timed-out or cancelled
		// wait ends quietly and says so, and the user's own Ctrl-C reaches the
		// device through their typing.
		status := cliStatus
		if timedOut {
			status = netcli.StatusTimeout
		}
		exec.cli.Abort(status)
	}
	if rest := exec.tap.Abort(); len(rest) > 0 {
		t.writeRemote(rest, t.recordingNow())
	}
	result := exec.tap.Result()
	result.TimedOut = result.TimedOut || timedOut
	if interrupt && exec.cli == nil && t.shell != nil {
		t.sendProtocol([]byte{3})
	}
	if exec.cli != nil {
		// The pump closes a shell block's mark itself; a CLI wait that ends
		// here never sees the pump again.
		t.markAIBlock(true)
	}
	exec.done <- result
}

// userInterrupts reports whether keyboard input contains Ctrl-C, which ends the
// assistant's wait: the user is taking the terminal back.
func userInterrupts(p []byte) bool { return bytes.IndexByte(p, 3) >= 0 }

// aiExecWait waits for the command, giving up at the deadline or when cancel
// closes; it then interrupts the command, which stays visible in the terminal.
func (state *State) aiExecWait(exec *aiExec, timeout time.Duration, cancel <-chan struct{}) gate.ExecResult {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	abort := func() gate.ExecResult {
		finished := make(chan gate.ExecResult, 1)
		if !state.aiPost(func(*State) {
			if exec.t.exec == exec {
				exec.t.finishExec(true, true)
			}
			select {
			case r := <-exec.done:
				finished <- r
			default:
				finished <- gate.ExecResult{TimedOut: true}
			}
		}) {
			return gate.ExecResult{TimedOut: true}
		}
		select {
		case r := <-finished:
			return r
		case <-time.After(5 * time.Second):
			return gate.ExecResult{TimedOut: true}
		}
	}
	select {
	case r := <-exec.done:
		return r
	case <-timer.C:
		return abort()
	case <-cancel:
		return abort()
	}
}

func execTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = tools.DefaultTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func sessionHandle(id uint32) string { return fmt.Sprint(id) }

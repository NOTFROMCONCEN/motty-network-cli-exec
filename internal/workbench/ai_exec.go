package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"motty/internal/ai/capture"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/charset"
	"motty/internal/netcli"
	"motty/internal/shellkind"
)

// aiExec is one command the assistant typed into a visible terminal and is
// waiting to see finish. A terminal has at most one.
type aiExec struct {
	t    *terminal
	tap  execTap
	cli  *netcli.Tap          // the network CLI collector; nil on the shell path
	done chan gate.ExecResult // buffered; receives exactly once
	job  *aiJob               // set for a background command nobody is waiting on
	// cliWait is a network CLI command's smallest wait (see effectiveWait);
	// zero on the shell path, which keeps the call's own timeout.
	cliWait time.Duration
	// confirm is the quiet-window timer behind a candidate prompt (see
	// netcli.Tap.Pending); nil while the shell path runs or output keeps
	// arriving.
	confirm *time.Timer
	// confirmGen counts every quiet window armed or retracted for this
	// command. A timer that already fired leaves its callback queued; the
	// generation tells that stale callback the window it was armed for is
	// gone, so it cannot confirm a newer candidate early.
	confirmGen int
	// A device console has no completion marker. The prompt seen before the
	// command is the signal: once it is drawn again on a later line, the command
	// has returned.
	consolePrompt string
	consoleRow    uint64
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
	return gate.ExecResult{Output: r.Output, ExitCode: r.ExitCode, Truncated: r.Truncated, Retained: r.Retained, RetainLost: r.RetainLost, ExitKnown: true}
}

// consoleTap adapts the Telnet/serial device-console capture: the command is
// typed exactly as given and the prompt's return completes it. There is no
// exit status, which the negative code the tap reports says.
type consoleTap struct{ tap *capture.Tap }

func (c consoleTap) Feed(p []byte) ([]byte, bool) { return c.tap.Feed(p) }

func (c consoleTap) Abort() []byte { return c.tap.Abort() }

func (c consoleTap) Result() gate.ExecResult {
	r := c.tap.Result()
	return gate.ExecResult{Output: r.Output, ExitCode: r.ExitCode, Truncated: r.Truncated, Retained: r.Retained, RetainLost: r.RetainLost, ExitKnown: r.ExitCode >= 0}
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
		PagerSeen: r.PagerSeen,
	}
}

// execEnd is why a command stopped being the one the assistant waits on.
type execEnd uint8

const (
	endMarker  execEnd = iota // the command finished and printed its marker
	endUser                   // the user pressed Ctrl-C and took the terminal back
	endShell                  // the shell ended
	endTimeout                // the wait ran out or was canceled
	endStop                   // the assistant asked for the command to be stopped
)

// maxExecOutput bounds the output kept for the model, whatever the terminal shows.
const maxExecOutput = 64 << 10

var (
	errNoSession  = errors.New("no such session; call list_sessions")
	errNoShell    = errors.New("the session has no open shell yet")
	errExecBusy   = errors.New("the terminal is already running another assistant command")
	errExecRefuse = errors.New("the terminal did not accept input")
	// errNoConsolePrompt means there is no prompt to wait for. Typing a command
	// then would have no completion signal.
	errNoConsolePrompt = errors.New("the device console is not showing a prompt; read the terminal and wait for its prompt, then try again")
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

// shellKinder is a Shell that knows which family of shell it runs. Others, such
// as a test double, are treated as POSIX, which is how every shell was typed to
// before the family was known.
type shellKinder interface{ Kind() shellkind.Kind }

func (t *terminal) shellKind() shellkind.Kind {
	if k, ok := t.shell.(shellKinder); ok {
		return k.Kind()
	}
	return shellkind.Unknown
}

// beginExec types the command into the session's terminal. Window thread. The
// session's execution mode picks the backend: a shell gets the capture wrapper
// typed for its family, a network device gets its raw command line.
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
		if exec, send, err = beginCLIExec(session, t, args, cliWaitFloor(state.ai.settings.cliTimeoutSeconds)); err != nil {
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
	// The block starts at the command's line: everything above it is the
	// user's own output, on the screen before the assistant ran, and none of
	// it carries the mark. markAIBlock counts from aiFront within the buffer
	// generation the command began in.
	t.aiFront = t.cursorLine()
	t.aiGeneration = t.screen.BufferGeneration()
	// The prompt row is where the assistant's command will be shown, so mark it
	// before the shell echoes anything. A line ID survives scrolling. A row
	// never written to has no ID yet; marking the zero ID would claim every
	// empty line on the screen.
	line := t.screen.Row(t.screen.Cursor().Y).ID
	if line != 0 {
		t.aiRows[line] = struct{}{}
	}
	// A CLI command is typed as the device expects it and the terminal shows
	// exactly that, so it goes into the session's recording like a user's own
	// input; the shell wrapper never does.
	sender := t.sendAssistant
	if exec.cli != nil {
		sender = t.sendCLI
	}
	if !sender(send) {
		if line != 0 {
			delete(t.aiRows, line)
		}
		t.exec = nil
		return nil, errExecRefuse
	}
	return t.exec, nil
}

// beginShellExec builds the shell capture for one command, typed for the
// shell's family: the wrapped bytes are typed into the shell, and the tap
// hides the wrapper's echo and waits for the marker.
func beginShellExec(t *terminal, args tools.TerminalExecArgs) (*aiExec, []byte, error) {
	kind := t.shellKind()
	nonce := ""
	consolePrompt := ""
	if kind == shellkind.Console {
		// Read the prompt from the live screen. This is what a person types at,
		// and it is also the only completion signal a device gives.
		t.screen.ScrollToBottom()
		consolePrompt = t.consolePrompt()
		if consolePrompt == "" {
			return nil, nil, errNoConsolePrompt
		}
	} else {
		nonce = capture.NewNonce()
	}
	wrapped, err := capture.WrapFor(kind, args.Command, nonce)
	if err != nil {
		return nil, nil, err
	}
	tap := capture.NewTap(nonce, maxExecOutput)
	if kind == shellkind.Console {
		tap = capture.NewConsoleTap(maxExecOutput)
	}
	switch {
	case args.Background:
		tap.RetainWindow(jobRetainWindow)
	case args.RetainOutput:
		tap.Retain(gate.MaxRetainedOutput)
	}
	tap.SetEcho(wrapped)
	// PowerShell's line editor redraws what is typed and asks the terminal where
	// its cursor is, and it waits for the answer. Holding that output back to swap
	// the wrapper for the plain command would withhold the question, so the user
	// sees what the shell echoes.
	if kind != shellkind.PowerShell && kind != shellkind.Console {
		tap.SetDisplay(capture.DisplayCommandFor(kind, args.Command))
	}
	if kind == shellkind.Console {
		// The console command is typed exactly as given, and the prompt seen
		// before it is the only completion signal a device gives.
		return &aiExec{
			t: t, tap: consoleTap{tap}, done: make(chan gate.ExecResult, 1),
			consolePrompt: consolePrompt, consoleRow: t.screen.Row(t.screen.Cursor().Y).ID,
		}, wrapped, nil
	}
	return &aiExec{t: t, tap: shellTap{tap}, done: make(chan gate.ExecResult, 1)}, wrapped, nil
}

// cursorLine is the number of the screen line the cursor is on, in the numbering
// that survives output and scrolling.
func (t *terminal) cursorLine() uint64 {
	return t.screen.LineNumber(t.screen.Cursor().Y)
}

// markAIBlock gives the assistant's command and its output rows the AI gutter
// mark. Rows are taken from the line numbers the command wrote, not from what is
// in view: output can scroll past between two writes, and the user may be
// reading scrollback while it arrives, so neither may leave a hole in the mark.
// The block is stored as line IDs, which survive resizing, so the mark stays
// with the text in scrollback.
func (t *terminal) markAIBlock(final bool) {
	if t.aiRows == nil || t.screen.Modes().AltScreen {
		// A full-screen program owns the alternate screen and paints over it;
		// the mark belongs to the main screen and resumes when the program ends.
		return
	}
	if generation := t.screen.BufferGeneration(); generation != t.aiGeneration {
		// The screen was cleared: line numbers no longer name what they did.
		// Everything before the cursor was already marked, so the block goes on
		// from the line the command is writing now.
		t.aiGeneration = generation
		t.aiFront = t.cursorLine()
	}
	end := t.cursorLine()
	if final && end > 1 {
		// The prompt written after the marker owns the cursor's line: the next
		// thing typed there is the user's, not the command's output.
		end--
	}
	for line := t.aiFront; line <= end; line++ {
		if id, ok := t.screen.LineID(line); ok {
			t.aiRows[id] = struct{}{}
		}
	}
	if end >= t.aiFront {
		t.aiFront = end + 1
	}
	if final {
		// Take the prompt line's mark even when an earlier write had given it
		// one: the line was already on screen when the marker was removed.
		delete(t.aiRows, t.screen.Row(t.screen.Cursor().Y).ID)
	}
}

// feedExec passes shell output through the running command's tap and returns
// what the terminal should show. When the command ended, it is finished.
func (t *terminal) feedExec(output []byte) []byte {
	display, done := t.exec.tap.Feed(output)
	if done {
		t.finishExec(endMarker)
	}
	return display
}

// consolePrompt returns the text from the start of the cursor's line to the
// cursor, which is the prompt a device console is waiting at.
func (t *terminal) consolePrompt() string {
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if cursor.Y < 0 || cursor.Y >= rows || cursor.X <= 0 {
		return ""
	}
	text, ok := t.screen.AppendCellText(nil, cursor.Y, 0, cursor.X)
	if !ok {
		return ""
	}
	return strings.TrimRight(string(text), " ")
}

// consoleReturnedToPrompt reports whether the running console command has come
// back to the prompt it started from. The start row is excluded: it still shows
// that prompt until the device echoes the command, and accepting it then would
// finish before the command ran.
func (t *terminal) consoleReturnedToPrompt(exec *aiExec) bool {
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if cursor.Y < 0 || cursor.Y >= rows || t.screen.Row(cursor.Y).ID == exec.consoleRow {
		return false
	}
	return t.screen.RowText(cursor.Y) == exec.consolePrompt
}

// finishConsoleExec ends a device-console command once its prompt is back. It
// has no exit status, so the result says so rather than inventing one.
func (t *terminal) finishConsoleExec() {
	exec := t.exec
	if exec == nil || exec.consolePrompt == "" || !t.consoleReturnedToPrompt(exec) {
		return
	}
	if console, ok := exec.tap.(consoleTap); ok {
		console.tap.FinishConsole(exec.consolePrompt, -1)
	}
	t.finishExec(endMarker)

}

// captureTap pulls the concrete capture out of a shell or console collector;
// a network CLI command has none.
func captureTap(t execTap) *capture.Tap {
	switch tap := t.(type) {
	case shellTap:
		return tap.tap
	case consoleTap:
		return tap.tap
	}
	return nil
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
		exec.confirmGen++
		if exec.confirm != nil {
			exec.confirm.Stop()
			exec.confirm = nil
		}
	case exec.confirm != nil:
		// The candidate may be a different one than the timer was armed for,
		// so the window starts over under a new generation.
		exec.confirmGen++
		exec.confirm.Reset(cliQuiet)
	default:
		exec.confirmGen++
		generation := exec.confirmGen
		exec.confirm = time.AfterFunc(cliQuiet, func() {
			state.aiPost(func(*State) {
				if t.exec == exec && exec.confirmGen == generation && exec.cli.Confirm() {
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
	exec.confirmGen++
	if exec.confirm != nil {
		exec.confirm.Stop()
		exec.confirm = nil
	}
	// The shell path finishes inside the pump, which marks the block right
	// after; this path ends on a timer, so it closes the mark itself.
	t.markAIBlock(true)
	result := exec.tap.Result()
	result.Output = t.utf8ExecOutput(result.Output)
	exec.done <- result
}

// finishExec ends the running command: normally when its marker arrived, or
// because the wait timed out, was canceled, or the user pressed Ctrl-C. The
// command may still be running on the remote side then, so an interrupt is sent.
func (t *terminal) finishExec(end execEnd) {
	t.endExecAt(end, netcli.StatusCancelled, end == endTimeout, end == endTimeout || end == endStop)
}

// endExec is finishExec for the paths that know how a network CLI wait ended:
// a connection that died under the command reports itself, and the shell
// timeout wrapper. A shell command ignores the status; its result is its exit
// code.
func (t *terminal) endExec(cliStatus netcli.Status, timedOut, interrupt bool) {
	t.endExecAt(endStop, cliStatus, timedOut, interrupt)
}

// endExecAt is the one ending: it knows both why the wait ended and, for a
// shell command, the execEnd the job record keeps.
func (t *terminal) endExecAt(end execEnd, cliStatus netcli.Status, timedOut, interrupt bool) {
	exec := t.exec
	if exec == nil {
		return
	}
	t.exec = nil
	exec.confirmGen++
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
	result.Output = t.utf8ExecOutput(result.Output)
	result.TimedOut = result.TimedOut || timedOut
	if interrupt && exec.cli == nil && t.shell != nil {
		t.sendProtocol([]byte{3})
	}
	if exec.cli != nil {
		// The pump closes a shell block's mark itself; a CLI wait that ends
		// here never sees the pump again.
		t.markAIBlock(true)
	}
	if exec.job != nil {
		exec.job.finish(end, result.ExitCode)
	}
	exec.done <- result
}

// utf8ExecOutput turns the tap's cleaned bytes into UTF-8 with the session's
// charset: the screen shows the device through this decoder, and the answer
// the model reads has to be the same text. The tap watches raw bytes so it can
// recognize prompts and pagers, so the conversion happens once, at the end.
func (t *terminal) utf8ExecOutput(output string) string {
	if t.encoding == "" || t.encoding == charset.UTF8 {
		return output
	}
	return string(charset.NewDecoder(t.encoding).Append(nil, []byte(output)))
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
				exec.t.finishExec(endTimeout)
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

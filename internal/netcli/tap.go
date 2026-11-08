package netcli

import (
	"bytes"
	"strconv"
	"strings"
)

// Status says how a CLI command ended. A network CLI reports no exit status,
// so a status is the whole truth about completion.
type Status uint8

const (
	// StatusRunning means output is still arriving.
	StatusRunning Status = iota
	// StatusPrompt means the device prompt came back after the command: the
	// device takes input again.
	StatusPrompt
	// StatusTimeout means the wait gave up before the prompt came back. The
	// command may or may not still be producing output.
	StatusTimeout
	// StatusCancelled means the user took the terminal back (for instance with
	// Ctrl-C, which reaches the device and usually aborts the command).
	StatusCancelled
	// StatusDisconnected means the connection ended under the command.
	StatusDisconnected
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusPrompt:
		return "prompt_restored"
	case StatusTimeout:
		return "timeout"
	case StatusCancelled:
		return "cancelled"
	case StatusDisconnected:
		return "disconnected"
	}
	return "unknown"
}

// Result is what a Tap learned about one CLI command.
type Result struct {
	// Output is the ANSI-stripped plain text the device printed, without the
	// echoed command line and without the final prompt, bounded to the tap's
	// maxOutput (head and tail kept, the middle replaced by an
	// "[... N bytes omitted ...]" marker).
	Output    string
	Truncated bool
	// Status is how the command ended; StatusRunning while it has not.
	Status Status
	// CLIError is the first device error line the output contained, or "".
	// Its presence does not end the command: many devices print the error and
	// the prompt after it.
	CLIError string
	// Pages is how many pager screens were advanced.
	Pages int
	// PagerSeen says the device stopped to page and the platform table has no
	// way to continue, so a timeout means the rest of the output is unknown.
	PagerSeen bool
	// Done is set once the prompt was recognized again.
	Done bool
}

// maxPages bounds how often a tap continues a pager, so a misread prompt can
// never turn into an endless space-pressing loop.
const maxPages = 1000

// maxLineBytes bounds one line of cleaned output before it is marked as cut.
const maxLineBytes = 64 << 10

type ansiState uint8

const (
	stGround ansiState = iota
	stEsc
	stEscInter
	stCSI
	stOSC
	stOSCEsc
)

// Tap watches raw device output for the prompt that ends one command. A Tap is
// not safe for concurrent use.
//
// Everything the device sends is shown to the user unchanged: the terminal
// shows the command as the device echoed it and the output as the device
// printed it. The tap only watches a cleaned copy of the same bytes, the way a
// terminal renders them (escape sequences removed, backspaces and carriage
// returns applied), and declares the command over when a complete line at a
// line boundary is the prompt again.
type Tap struct {
	plat   *Platform
	before string // the prompt captured before the command was sent, normalized
	host   string // the hostname token of that prompt, "" on the generic table
	echo   string // the line the device echoes for the command, "" when unknown
	// bareEcho is the command without the prompt: a character-mode device
	// echoes the bytes it received, so its first line back is the bare
	// command rather than prompt+command.
	bareEcho string
	send     func([]byte) bool

	// The cleaned copy of the current line, and how it is being drawn: a
	// carriage return restarts the line, a backspace steps back over it, and
	// escape sequences pass unseen.
	state     ansiState
	line      []byte
	col       int
	erased    bool
	lineOver  bool // the current line grew past maxLineBytes and was cut
	pendingCR bool

	// Detection state. The echo is decided once: the first complete line either
	// is the echoed command (and is left out of the output) or the device does
	// not echo, and both are fine.
	echoDecided bool
	// pending says the output ends in a candidate prompt (see Pending).
	pending   bool
	pages     int
	pagerSeen bool
	cliErr    string
	status    Status
	done      bool
	aborted   bool

	// Bounded output text, head and tail kept.
	head, tail       []byte
	headCap, tailCap int
	total            int
	outTruncated     bool
}

// NewTap returns a Tap for one command sent to a device whose prompt was
// prompt. prompt must be the normalized text of the device prompt as the
// terminal showed it before the command was sent: it is what completion is
// recognized against, and an empty or unrecognizable one means the caller
// refuses to run anything rather than wait blindly. maxOutput bounds the
// recorded output text (default 64 KiB when <= 0).
func NewTap(plat *Platform, prompt, command string, maxOutput int) *Tap {
	if maxOutput <= 0 {
		maxOutput = 64 << 10
	}
	if maxOutput < 64 {
		maxOutput = 64
	}
	before := normalizePrompt(prompt)
	host, ok := plat.MatchPrompt(before)
	if !ok {
		// Without a shape match only the exact line can end the command.
		host = ""
	}
	bare := normalizePrompt(command)
	return &Tap{
		plat:     plat,
		before:   before,
		host:     host,
		echo:     before + bare,
		bareEcho: bare,
		headCap:  maxOutput / 2,
		tailCap:  maxOutput - maxOutput/2,
		status:   StatusRunning,
	}
}

// SetSend gives the tap a way to type into the device, used only to continue a
// pager the platform table knows. nil leaves the pager alone.
func (t *Tap) SetSend(send func([]byte) bool) { t.send = send }

// normalizePrompt is how prompt text is compared: surrounding spaces (a prompt
// followed by typing room) and stray control padding go away.
func normalizePrompt(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

// Feed takes the next chunk of raw device output. It reports whether the
// command has completed — which happens only after [Confirm] (see Pending).
// Everything shown to the user passes through unchanged; Feed only watches.
func (t *Tap) Feed(p []byte) bool {
	if t.done || t.aborted {
		return t.done
	}
	// The last chunk ended on a candidate prompt; this chunk carries more
	// output, so that candidate was an output line after all.
	t.pending = false
	for _, b := range p {
		t.step(b)
		if t.done {
			return true
		}
	}
	// The prompt of a waiting device has no trailing newline: it only becomes
	// the last line of a chunk. A pager prompt waits mid-line the same way.
	if !t.done {
		t.atLineEnd()
	}
	return t.done
}

// Pending reports that the current output ends in what looks exactly like the
// device prompt again. Output that merely looks like the prompt can precede
// more output — a fragmented echo, a line of a file listing — so a candidate
// only becomes a completion once the device has stayed quiet for a moment and
// nothing has retracted it. The caller confirms a candidate that survives
// with [Confirm].
func (t *Tap) Pending() bool { return t.pending && !t.done && !t.aborted }

// Confirm completes a candidate that has stayed quiet: the device prompt is
// back. It reports whether the command is now done.
func (t *Tap) Confirm() bool {
	if !t.Pending() {
		return t.done
	}
	t.pending = false
	t.line = t.line[:0]
	t.col = 0
	t.finish()
	return true
}

func (t *Tap) step(b byte) {
	switch t.state {
	case stGround:
		t.ground(b)
	case stEsc:
		switch {
		case b == '[':
			t.state = stCSI
		case b == ']' || b == 'P' || b == 'X' || b == '^' || b == '_':
			t.state = stOSC
		case b >= 0x20 && b <= 0x2f:
			t.state = stEscInter
		case b == 0x1b:
			// stay
		default:
			t.state = stGround
		}
	case stEscInter:
		if b < 0x20 || b > 0x2f {
			t.state = stGround
		}
	case stCSI:
		if b >= 0x40 && b <= 0x7e || b == 0x1b || b == 0x18 || b == 0x1a {
			t.state = stGround
		}
	case stOSC:
		switch b {
		case 0x07, 0x18, 0x1a:
			t.state = stGround
		case 0x1b:
			t.state = stOSCEsc
		}
	case stOSCEsc:
		if b == '\\' {
			t.state = stGround
		} else {
			t.state = stEsc
			t.step(b)
		}
	}
}

func (t *Tap) ground(b byte) {
	switch {
	case b == 0x1b:
		t.state = stEsc
	case b == '\n':
		t.commit()
		t.pendingCR = false
	case b == '\r':
		// A carriage return the device sent to redraw a progress line must not
		// lose the line before it: "line\r\n" commits "line". Only when text
		// follows the return does the line restart.
		t.pendingCR = true
	case b == '\b':
		if t.col > 0 {
			t.col--
		}
		t.erased = true
	case b == '\t' || b >= 0x20 && b != 0x7f:
		if t.pendingCR {
			// Text after a bare carriage return redraws the line from column 0.
			t.line = t.line[:0]
			t.col = 0
			t.erased = true
			t.pendingCR = false
		}
		switch {
		case t.col < len(t.line):
			t.line[t.col] = b
			t.col++
		case len(t.line) >= maxLineBytes:
			t.lineOver = true
			t.outTruncated = true
		default:
			t.line = append(t.line, b)
			t.col++
		}
	}
}

// commit evaluates the completed line: the echoed command, a pager line, the
// returning prompt, an error line, or plain output.
func (t *Tap) commit() {
	line := strings.TrimSpace(string(t.line))
	t.line = t.line[:0]
	t.col = 0
	t.erased = false
	t.lineOver = false
	t.pending = false
	if line == "" {
		return
	}
	if !t.echoDecided {
		t.echoDecided = true
		// A line-mode device echoes the prompt and the command; a
		// character-mode one echoes only the bytes it received.
		if line == t.echo || (t.bareEcho != "" && line == t.bareEcho) {
			return // the device echoed the command; the terminal already shows it
		}
	}
	if t.isPager(line) {
		return
	}
	if t.isPrompt(line) {
		t.hold()
		return
	}
	if t.cliErr == "" {
		t.cliErr = t.plat.errFirst(line)
	}
	t.put(line)
}

// atLineEnd evaluates the current line as it stands, when the device has
// stopped sending: that is where a waiting prompt sits, since a prompt has no
// trailing newline to commit it.
func (t *Tap) atLineEnd() {
	line := strings.TrimSpace(string(t.line))
	if line == "" || t.isPager(line) {
		return
	}
	if !t.echoDecided && (strings.HasPrefix(t.echo, line) || (t.bareEcho != "" && strings.HasPrefix(t.bareEcho, line))) {
		// The echoed command starts with the prompt itself: what looks like the
		// prompt coming back can still be the first characters of the echo,
		// arriving in pieces. Wait until the echo is decided.
		return
	}
	if t.isPrompt(line) {
		t.hold()
	}
}

// hold marks the output as a candidate prompt (see Pending).
func (t *Tap) hold() {
	t.pending = true
}

func (t *Tap) isPager(line string) bool {
	matched := false
	for _, pager := range t.plat.pagers {
		if strings.HasSuffix(line, pager) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	t.pagerSeen = true
	t.pages++
	if t.send != nil && t.plat.pagerKey != nil && t.pages <= maxPages {
		// The key comes from the platform table: a verified page-continue is
		// typed, an unknown device is left alone.
		t.send(t.plat.pagerKey)
	}
	// The pager text sits on the last output line; the device erases it with
	// backspaces once a key arrives, and until then the line stays open.
	return true
}

// isPrompt reports whether the line is the device prompt again. It is not
// enough for a line to look like one: a complete line must match the prompt
// captured before the command either exactly or, on a platform that knows its
// prompt shape, carry the same host name. Output that merely ends with prompt
// characters never matches.
func (t *Tap) isPrompt(line string) bool {
	if line == t.before {
		return true
	}
	if t.host == "" {
		return false
	}
	host, ok := t.plat.MatchPrompt(line)
	return ok && strings.EqualFold(host, t.host)
}

func (t *Tap) finish() {
	t.done = true
	t.status = StatusPrompt
}

// Abort stops the capture after a timeout, a cancellation or a disconnect. A
// Tap that already saw the prompt keeps its result. status says why the wait
// ended; it is only recorded when the prompt never came back.
func (t *Tap) Abort(status Status) {
	if t.done || t.aborted {
		return
	}
	t.aborted = true
	t.status = status
}

// Result reports the output captured so far and how the command ended.
func (t *Tap) Result() Result {
	head, tail := t.head, t.tail
	if len(tail) > t.tailCap {
		tail = tail[len(tail)-t.tailCap:]
	}
	var out []byte
	out = append(out, head...)
	if omitted := t.total - len(head) - len(tail); omitted > 0 {
		out = append(out, "\n[... "...)
		out = strconv.AppendInt(out, int64(omitted), 10)
		out = append(out, " bytes omitted ...]\n"...)
	}
	out = append(out, tail...)
	// An unterminated line is the command still drawing, or the prompt the tap
	// has not declared yet; either way it belongs to what the model sees.
	if line := strings.TrimSpace(string(t.line)); line != "" {
		out = append(out, line...)
	}
	out = bytes.TrimSuffix(out, []byte{'\n'})
	return Result{
		Output:    string(out),
		Truncated: t.outTruncated || t.total > t.headCap+t.tailCap,
		Status:    t.status,
		CLIError:  t.cliErr,
		Pages:     t.pages,
		PagerSeen: t.pagerSeen,
		Done:      t.done,
	}
}

// put appends one committed output line to the bounded head/tail store.
func (t *Tap) put(line string) {
	b := append([]byte(line), '\n')
	t.total += len(b)
	if len(t.head) < t.headCap {
		n := min(t.headCap-len(t.head), len(b))
		t.head = append(t.head, b[:n]...)
		b = b[n:]
	}
	if len(b) == 0 {
		return
	}
	if len(b) >= t.tailCap {
		t.tail = append(t.tail[:0], b[len(b)-t.tailCap:]...)
		return
	}
	t.tail = append(t.tail, b...)
	if len(t.tail) >= 2*t.tailCap {
		n := copy(t.tail, t.tail[len(t.tail)-t.tailCap:])
		t.tail = t.tail[:n]
	}
}

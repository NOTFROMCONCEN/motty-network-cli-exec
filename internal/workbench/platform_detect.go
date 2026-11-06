package workbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/netcli"
)

// A device says what it is in its login banner, and its prompt then proves the
// CLI is ready. Either alone is ambiguous (Huawei and H3C share a prompt shape,
// and a Linux root prompt can look like Cisco), so adoption needs both. The
// scan runs only after output has gone quiet, never types anything, and never
// replaces a setting the user made.

const (
	// platformDetectQuiet is how long the device must have stopped sending
	// before its prompt is read. A banner and prompt arrive together; reading a
	// fragment would only create false candidates.
	platformDetectQuiet = 350 * time.Millisecond
	// platformDetectTries bounds the scans of a terminal that never shows a
	// vendor banner, so every later prompt does not repeat the search.
	platformDetectTries = 5
	// platformDetectText bounds how much retained output is searched for a
	// banner. A login banner is at the top of the session; 64 KiB is far more.
	platformDetectText = 64 << 10
)

// platformProbe is one terminal's detection state. Runtime only: a reconnect
// gets a fresh one because the old screen may still show the old prompt. The
// deadline uses the workbench's monotonic clock, the same one state.now reads.
type platformProbe struct {
	deadline time.Duration
	armed    bool
	done     bool
	attempts int
}

func (probe *platformProbe) reset() { *probe = platformProbe{} }

// output records that the device sent something and moves the quiet deadline.
func (probe *platformProbe) output(now time.Duration) {
	if probe.done {
		return
	}
	probe.deadline = now + platformDetectQuiet
	probe.armed = true
}

// pending reports how long until the quiet deadline, and whether a scan is
// owed at all.
func (probe *platformProbe) pending(now time.Duration) (time.Duration, bool) {
	if probe.done || !probe.armed {
		return 0, false
	}
	if wait := probe.deadline - now; wait > 0 {
		return wait, true
	}
	return 0, true
}

// platformDetectEligible reports whether this terminal can be a device CLI
// whose platform is worth detecting. A shell farm, a local shell, a desktop
// and a running assistant command are all left alone.
func (state *State) platformDetectEligible(session *Session) bool {
	if session == nil || session.term == nil {
		return false
	}
	t := session.term
	if t.shell == nil || t.ended || t.screen == nil || t.exec != nil {
		return false
	}
	if t.screen.Modes().AltScreen || t.screen.ViewOffset() != 0 {
		return false
	}
	if session.isLocal() || session.host.isDesktop() || session.containerCommand != "" {
		return false
	}
	// An explicit shell or an explicit platform is the user's decision.
	if session.host.ExecMode == "shell" {
		return false
	}
	if session.host.CLIPlatform != "" && !session.host.platformDetected {
		return false
	}
	return true
}

// platformPromptCandidate reports whether the last few rows already show a
// device prompt. Arming only then keeps an idle shell from asking for frames
// forever: there is nothing to detect until a prompt exists.
func platformPromptCandidate(session *Session) bool {
	t := session.term
	screen := t.screen
	_, rows := screen.Size()
	cursor := screen.Cursor()
	for offset := 0; offset < 4; offset++ {
		row := cursor.Y - offset
		if row < 0 || row >= rows {
			continue
		}
		line := strings.TrimSpace(screen.RowText(row))
		if line == "" {
			continue
		}
		if netcli.LooksLikeLoginPrompt(line) {
			return false
		}
		for _, table := range netcli.Platforms() {
			if !table.Shaped() {
				continue
			}
			if _, ok := table.MatchPrompt(line); ok {
				return true
			}
		}
	}
	return false
}

// requestPlatformDetectFrames asks for the frame that reads a prompt once the
// device has gone quiet. A terminal that is waiting on its quiet window asks
// for exactly one.
func (state *State) requestPlatformDetectFrames(canvas *ui.Canvas) {
	now := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		if !state.platformDetectEligible(session) {
			continue
		}
		if wait, pending := session.term.platformProbe.pending(now); pending {
			if wait <= 0 {
				wait = time.Millisecond
			}
			canvas.RequestFrameIn(wait)
		}
	}
}

// scanPlatform looks for a vendor banner and a prompt shape that agree. It
// adopts the result only when exactly one table matches and the host's
// setting is still automatic (or was itself detected).
func (state *State) scanPlatform(session *Session) {
	t := session.term
	wait, pending := t.platformProbe.pending(state.now())
	if !pending || wait > 0 {
		return
	}
	t.platformProbe.armed = false
	if !state.platformDetectEligible(session) {
		return
	}
	t.platformProbe.attempts++
	if t.platformProbe.attempts > platformDetectTries {
		t.platformProbe.done = true
		return
	}
	host := &session.host
	// An explicit shell or an explicit platform is the user's decision. Only
	// automatic and previously detected values may change.
	if host.ExecMode == "shell" || host.CLIPlatform != "" && !host.platformDetected {
		t.platformProbe.done = true
		return
	}

	screen := t.screen
	_, rows := screen.Size()
	cursor := screen.Cursor()
	promptLines := make([]string, 0, 4)
	for offset := 0; offset < 4; offset++ {
		row := cursor.Y - offset
		if row < 0 || row >= rows {
			continue
		}
		if line := strings.TrimSpace(screen.RowText(row)); line != "" {
			promptLines = append(promptLines, line)
		}
	}
	if len(promptLines) == 0 {
		return
	}
	text := string(screen.AppendRetainedText(nil))
	if len(text) > platformDetectText {
		text = text[len(text)-platformDetectText:]
	}

	matches := make([]*netcli.Platform, 0, 1)
	seen := map[string]bool{}
	for _, table := range netcli.Platforms() {
		if !table.Shaped() || seen[table.ID] {
			continue
		}
		if _, ok := table.BannerMatch(text); !ok {
			continue
		}
		for _, line := range promptLines {
			if _, ok := table.MatchPrompt(line); ok {
				matches = append(matches, table)
				seen[table.ID] = true
				break
			}
		}
	}
	if len(matches) != 1 {
		return
	}
	chosen := matches[0]
	if host.CLIPlatform == chosen.ID {
		t.platformProbe.done = true
		return
	}
	state.adoptPlatform(session, chosen.ID)
	t.platformProbe.done = true
}

// adoptPlatform records a confident detection on the session and on every
// tab of the same saved host. The status line says what changed; the host
// dialog remains the place to undo it.
func (state *State) adoptPlatform(session *Session, id string) {
	// Pin the execution mode only when this host would otherwise run a shell.
	// Telnet and serial already resolve to the network CLI, and leaving them
	// automatic keeps the transport's default meaningful.
	switched := session.host.ExecMode == "" && resolveExecMode(session.host) == shellMode
	set := func(host *Host) {
		host.CLIPlatform = id
		host.platformDetected = true
		if switched {
			host.ExecMode = "network_cli"
		}
	}
	set(&session.host)
	if session.HostID != 0 {
		for index := range state.hosts {
			if state.hosts[index].ID == session.HostID {
				set(&state.hosts[index])
				break
			}
		}
		for index := range state.sessions {
			if state.sessions[index].HostID == session.HostID {
				set(&state.sessions[index].host)
			}
		}
	}
	state.status = fmt.Sprintf(state.messages.Tools.CLIPlatformDetectedFmt, netcli.Name(id))
	state.save()
}

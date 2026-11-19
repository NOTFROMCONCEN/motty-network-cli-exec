package workbench

import (
	"github.com/vivaui/viva/gfx"
	"motty/internal/netcli"
	"motty/internal/sshconn"
)

// The terminal status bar: a fork-only strip at the window's bottom that names
// the focused session's connection, execution mode and latency, and keeps the
// log-coloring eye at hand. It reserves its room the way the command drawer
// does — a height the panes give up — and paints into it after the page, so the
// shared files carry only a handful of fork-marked lines.

// statusBarHits is the place of the controls the status bar paints.
var statusBarHits = tree.Feature("status-bar")

// hitStatusBarLogColor is the eye: the same toggle the toolbar dock's
// log-coloring entry drives.
var hitStatusBarLogColor = tree.ID(statusBarHits, "log-color")

// The fork's settings rows share the settings scope, the way tab_numbers.go
// names its rows beside the page they belong to.
var (
	hitForkStatusBar      = tree.ID(settingsHits, "fork-status-bar")
	hitForkCompactDensity = tree.ID(settingsHits, "fork-compact-density")
)

// statusBarHeight is the strip the status bar keeps at the window's bottom,
// subtracted from the panes like the command drawer's reserve. Zero hides it:
// off the terminal page, over a fullscreen desktop, or when the user turned
// the bar off.
func (state *State) statusBarHeight() float32 {
	if state.statusBarHidden || !state.terminalPage() || state.fullscreenDesktop() != nil {
		return 0
	}
	if compactDensity {
		return 22
	}
	return 24
}

// statusBarSession is the session whose facts the bar shows: the focused one.
func (state *State) statusBarSession() *Session {
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	return session
}

// statusBarPhase names a connection that is not connected yet, so a dialing
// session reads as one. Connected, failed and closed phases name nothing: the
// address alone, or the tab's own state, carries those.
func (state *State) statusBarPhase(session *Session) (string, gfx.Color) {
	switch session.phase {
	case sshconn.PhaseConnecting, sshconn.PhaseHostKeyPrompt:
		return state.messages.Fork.StatusBarConnecting, state.pal.warning
	case sshconn.PhaseAuthenticating, sshconn.PhaseKeyPassphrasePrompt, sshconn.PhasePasswordPrompt:
		return state.messages.Fork.StatusBarAuthenticating, state.pal.warning
	}
	return "", gfx.Color{}
}

// statusBarExecLabel names the session's execution mode when it speaks a
// device CLI: the platform its table resolves to, or the mode itself when no
// table is known. A shell session names nothing, so the bar stays quiet about
// the fork's machinery on plain terminals.
func statusBarExecLabel(session *Session) string {
	if session.sessionExecMode() != cliMode {
		return ""
	}
	if id := session.cliPlatformName(); id != "" {
		if name := netcli.Name(id); name != "" {
			return name
		}
		return id
	}
	return "CLI"
}

// clickFork answers the clicks of the fork's own surfaces: the status bar and
// the two settings rows its features own. One delegate at the head of the
// click chain, so no upstream dispatch learns these identities.
func (state *State) clickFork(id gfx.ID) bool {
	if id == hitStatusBarLogColor {
		// The eye is the toolbar's log-coloring entry by another door: same
		// state, same hint, same save.
		if state.setHighlighting(!state.highlighting.on) {
			state.save()
		}
		return true
	}
	switch id {
	case hitForkStatusBar:
		state.statusBarHidden = !state.statusBarHidden
		state.save()
		return true
	case hitForkCompactDensity:
		state.applyDensity(!compactDensity)
		state.save()
		return true
	case hitForkRailFold:
		// The rail's fold button and handle (toolbar_rail_fold.go).
		state.clickForkRailFold()
		state.save()
		return true
	}
	return false
}

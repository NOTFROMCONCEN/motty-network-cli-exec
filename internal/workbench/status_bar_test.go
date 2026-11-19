package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sshconn"
)

// statusBarState gives a test a connected session the way a user reaches one:
// saved hosts, a double-click on the first row, a connection the fake dial
// answers. The bar itself is opt-in, so the test asks for it as its settings
// row would.
func statusBarState(t testing.TB) (*State, *Session) {
	t.Helper()
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("no session opened")
	}
	session.phase = sshconn.PhaseConnected
	state.statusBarHidden = false
	return state, session
}

func checkWhole(t testing.TB, state *State, page string) {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1000, Height: 640})
	snapshot := app.Snapshot{Width: 1000, Height: 640, Texts: canvas.Texts(), Hits: canvas.Hits()}
	for _, problem := range headless.CheckLayout(snapshot) {
		t.Errorf("%s: %s %q %s at %+v", page, problem.Kind, problem.Text, problem.Detail, problem.Bounds)
	}
}

func TestStatusBarKeepsTheLowestStripAndTheEyeInIt(t *testing.T) {
	state, _ := statusBarState(t)
	if height := state.statusBarHeight(); height != 24 {
		t.Fatalf("the status bar keeps %v of the window, want 24", height)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1000, Height: 640})
	eye, ok := hitByID(&canvas, hitStatusBarLogColor)
	if !ok {
		t.Fatal("the status bar's eye is not painted")
	}
	// The bar sits at the window's bottom edge; the eye rides it, centered in
	// the strip the panes left.
	if barTop := 640 - state.statusBarHeight(); eye.Bounds.Y < barTop || eye.Bounds.Bottom() > 640 {
		t.Errorf("the eye sits at %+v, outside the bar that starts at %v", eye.Bounds, barTop)
	}
	// The panes gave the strip up: the workspace ends where the bar begins.
	if bounds := state.fullWorkspacePixelBounds(gfx.Size{Width: 1000, Height: 640}); bounds.Bottom() != 640-24 {
		t.Errorf("the workspace ends at %v, want %v", bounds.Bottom(), 640-24)
	}
	checkWhole(t, state, "terminal with status bar")
}

func TestStatusBarEyeTogglesHighlightingLikeTheToolbarDoes(t *testing.T) {
	state, _ := statusBarState(t)
	before := state.highlighting.on
	state.Click(hitStatusBarLogColor)
	if state.highlighting.on == before {
		t.Fatalf("pressing the eye left highlighting at %v", before)
	}
	if !state.clickFork(hitStatusBarLogColor) {
		t.Fatal("the eye's click does not answer to the fork's delegate")
	}
}

func TestStatusBarSettingsRowHidesItAndGivesTheRoomBack(t *testing.T) {
	state, _ := statusBarState(t)
	state.Click(hitForkStatusBar)
	if height := state.statusBarHeight(); height != 0 {
		t.Fatalf("the status bar still keeps %v after its row turned it off", height)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1000, Height: 640})
	if _, ok := hitByID(&canvas, hitStatusBarLogColor); ok {
		t.Error("a hidden status bar still paints its eye")
	}
	if bounds := state.fullWorkspacePixelBounds(gfx.Size{Width: 1000, Height: 640}); bounds.Bottom() != 640 {
		t.Errorf("the hidden bar's room was not given back: the workspace ends at %v, want 640", bounds.Bottom())
	}
}

func TestStatusBarStaysOffTheSectionPages(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	if state.terminalPage() {
		t.Fatal("a fresh state opens on the terminal page")
	}
	if height := state.statusBarHeight(); height != 0 {
		t.Fatalf("the hosts page reserves %v for the status bar, want none", height)
	}
}

// The bar is opt-in: a fresh state's window reads as upstream's until the user
// asks for the bar in Settings.
func TestStatusBarStaysHiddenUntilAskedFor(t *testing.T) {
	state, _ := statusBarState(t)
	state.statusBarHidden = true
	if height := state.statusBarHeight(); height != 0 {
		t.Fatalf("a state that never asked for the bar keeps %v of it", height)
	}
	// Its settings row shows the bar, the way the row reads.
	state.statusBarHidden = false
	if height := state.statusBarHeight(); height != 24 {
		t.Fatalf("after the row turned the bar on it keeps %v, want 24", height)
	}
}

func TestStatusBarNamesADialingSession(t *testing.T) {
	state, session := statusBarState(t)
	session.phase = sshconn.PhaseAuthenticating
	_, canvasTexts := paintedTexts(state)
	if !containsText(canvasTexts, state.messages.Fork.StatusBarAuthenticating) {
		t.Errorf("an authenticating session's bar does not say %q", state.messages.Fork.StatusBarAuthenticating)
	}
	session.phase = sshconn.PhaseConnected
	_, canvasTexts = paintedTexts(state)
	if containsText(canvasTexts, state.messages.Fork.StatusBarAuthenticating) {
		t.Error("a connected session's bar still says it is authenticating")
	}
}

func TestStatusBarNamesADeviceCLISession(t *testing.T) {
	state, session := statusBarState(t)
	session.host.ExecMode = "network_cli"
	session.host.CLIPlatform = "generic"
	label := statusBarExecLabel(session)
	if label == "" {
		t.Fatal("a network-CLI session's bar names no platform")
	}
	_, canvasTexts := paintedTexts(state)
	if !containsText(canvasTexts, label) {
		t.Errorf("the bar does not show the platform label %q", label)
	}
	// A shell session stays quiet about the fork's machinery.
	session.host.ExecMode = "shell"
	if label := statusBarExecLabel(session); label != "" {
		t.Errorf("a shell session's bar names an execution mode %q", label)
	}
}

func containsText(texts []string, want string) bool {
	for _, text := range texts {
		if text == want {
			return true
		}
	}
	return false
}

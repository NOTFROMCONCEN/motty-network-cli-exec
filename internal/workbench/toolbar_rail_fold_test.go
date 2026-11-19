package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// railFoldState gives a test a connected session on the terminal page, where
// the tool rail shows.
func railFoldState(t testing.TB) *State {
	t.Helper()
	state, _ := statusBarState(t)
	return state
}

func TestRailFoldCollapsesToAHandleAndBack(t *testing.T) {
	state := railFoldState(t)
	// The functions take the physical side: left, like the rest of the dock.
	leftSide := state.terminalToolbarPosition == terminalToolbarLeft
	if folded := state.toolbarRailFoldedOnSide(leftSide); folded {
		t.Fatal("a fresh state's rail is folded")
	}
	if extent := state.forkRailExtent(leftSide); extent != state.terminalToolbarMetrics().railExtent {
		t.Fatalf("an unfolded rail keeps %v, want the metrics' %v", extent, state.terminalToolbarMetrics().railExtent)
	}

	// The fold button at the column's foot collapses it.
	state.Click(hitForkRailFold)
	if !state.toolbarRailFolded {
		t.Fatal("pressing the fold button did not fold the rail")
	}
	if extent := state.forkRailExtent(leftSide); extent != railFoldHandleExtent {
		t.Fatalf("a folded rail keeps %v, want the handle's %v", extent, railFoldHandleExtent)
	}
	if reserve := state.toolbarDockSideReserveFor(leftSide, true); reserve != railFoldHandleExtent {
		t.Errorf("the folded rail reserves %v of the panes' room, want %v", reserve, railFoldHandleExtent)
	}

	// The painted strip is just the handle: one hit at the edge, none of the
	// tools.
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	edge := float32(1400) - railFoldHandleExtent
	inStrip := 0
	handleFound := false
	for _, h := range canvas.Hits() {
		if h.Bounds.X >= edge {
			inStrip++
			if h.ID == hitForkRailFold {
				handleFound = true
			}
		}
	}
	if inStrip != 1 || !handleFound {
		t.Errorf("the folded rail paints %d hits in its strip, want only the handle (found=%v)", inStrip, handleFound)
	}

	// The handle brings the column back.
	state.Click(hitForkRailFold)
	if state.toolbarRailFolded {
		t.Fatal("pressing the handle did not unfold the rail")
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	found := false
	for _, h := range canvas.Hits() {
		if h.ID == hitForkRailFold && h.Bounds.X >= float32(1400)-state.terminalToolbarMetrics().railExtent {
			found = true
		}
	}
	if !found {
		t.Error("the unfolded rail does not wear its fold button")
	}
}

func TestRailFoldStaysOnTheToolbarSide(t *testing.T) {
	state := railFoldState(t)
	state.Click(hitForkRailFold)
	leftSide := state.terminalToolbarPosition == terminalToolbarLeft
	other := !leftSide
	if folded := state.toolbarRailFoldedOnSide(other); folded {
		t.Error("the side without the tools folded with them")
	}
	if extent := state.forkRailExtent(other); extent != state.terminalToolbarMetrics().railExtent {
		t.Errorf("the other side's extent follows the fold: %v", extent)
	}
}

func TestRailFoldLeavesTheNotesSidebarItsRoom(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionTerminal
	state.noteSidebarFor(0).open = true
	if !state.noteSidebarVisible() {
		t.Fatal("the test's notes sidebar is not visible")
	}
	state.toolbarRailFolded = true
	if folded := state.toolbarRailFoldedOnSide(false); folded {
		t.Error("the fold took the slot the notes sidebar borrows")
	}
	if extent := state.forkRailExtent(false); extent != state.terminalToolbarMetrics().railExtent {
		t.Errorf("the notes sidebar's slot follows the fold: %v", extent)
	}
}

func TestRailFoldTravelsThroughTheStateFile(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitForkRailFold)
	state.save()

	reopened := newTestState(t, options)
	if !reopened.toolbarRailFolded {
		t.Error("the fold did not survive the state file")
	}
}

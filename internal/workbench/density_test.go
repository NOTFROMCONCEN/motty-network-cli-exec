package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The interface density is package state, so a test that flips it puts it back
// before it returns: no other test may meet the compact look by accident. The
// cleanup runs while the state is still alive (cleanups unwind in reverse), so
// the restore may rebuild the theme through it.
func withCompactDensity(t testing.TB, state *State) {
	t.Helper()
	state.applyDensity(true)
	t.Cleanup(func() { state.applyDensity(false) })
}

func TestCompactDensityTightensTheChromeAndRestoresIt(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	withCompactDensity(t, state)
	if navigationWidth != 184 {
		t.Errorf("compact navigation keeps %v, want 184", navigationWidth)
	}
	if hostRowHeight != 54 || hostGroupRowHeight != 36 {
		t.Errorf("compact host rows keep %v/%v, want 54/36", hostRowHeight, hostGroupRowHeight)
	}
	if metrics.FontSizeM != 13 {
		t.Errorf("compact body text draws at %v, want the framework's compact 13", metrics.FontSizeM)
	}
}

func TestCompactDensityKeepsEveryPageWhole(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	withCompactDensity(t, state)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1000, Height: 640})
	// The bar's buttons stay in the bar the platform sized.
	if newTab, ok := hitByID(&canvas, hitNewTab); ok {
		if newTab.Bounds.Bottom() > TitlebarHeight {
			t.Errorf("the new-tab button ends at %v, under the bar's %v", newTab.Bounds.Bottom(), TitlebarHeight)
		}
	}
	checkWhole(t, state, "hosts at compact density")
	state.Click(hitSettings)
	checkWhole(t, state, "settings at compact density")
}

func TestCompactDensityTravelsThroughTheStateFile(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.applyDensity(true)
	t.Cleanup(func() { state.applyDensity(false) })
	state.save()

	// A second state on the same file opens compact, and its chrome agrees.
	reopened := newTestState(t, options)
	if !compactDensity {
		t.Error("the compact choice did not survive the state file")
	}
	if workspaceHeaderHeight != 32 {
		t.Errorf("the reopened window's header keeps %v, want the compact 32", workspaceHeaderHeight)
	}
	var canvas gfx.Canvas
	reopened.Paint(&canvas, gfx.Size{Width: 1000, Height: 640})
}

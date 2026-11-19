package workbench

import (
	"github.com/vivaui/viva/gfx"
)

// The tool rail's fold. The rail lines a dozen tools up the window's edge, and
// an operations session with transfers, monitoring and the assistant open
// spends a strip of the screen on icons it is not reading. The fold collapses
// the column to a handle at the edge; pressing the handle — or the fold button
// at the column's foot — brings the tools back. A click toggles, deliberately:
// a hover-expanded rail would shift the terminal under the pointer's feet.

// railFoldHandleExtent is the width the folded column keeps: just room for the
// handle, so the terminal gains the rest and the window edge stays framed.
const railFoldHandleExtent = float32(20)

// hitForkRailFold is both the fold button at the column's foot and the handle
// the folded column wears: pressing either toggles the fold.
var hitForkRailFold = tree.ID(shellHits, "fork-rail-fold")

// toolbarRailFoldedOnSide reports whether this side's tool column is collapsed
// to its handle. The fold belongs to the side the terminal tools call home —
// the side the toolbar position names — and only while a tool column actually
// shows there: when no terminal holds the rail's slot, the notes sidebar
// borrows it, and its width stays.
func (state *State) toolbarRailFoldedOnSide(left bool) bool {
	if !state.toolbarRailFolded {
		return false
	}
	if (state.terminalToolbarPosition == terminalToolbarLeft) != left {
		return false
	}
	_, hasTerminal := state.terminalToolbarSession()
	if !hasTerminal && state.noteSidebarVisible() {
		return false
	}
	return true
}

// forkRailExtent is the width the side's tool column takes: upstream's rail
// extent, or the handle's while the column is folded.
func (state *State) forkRailExtent(left bool) float32 {
	if state.toolbarRailFoldedOnSide(left) {
		return railFoldHandleExtent
	}
	return state.terminalToolbarMetrics().railExtent
}

// toolbarRailFoldCapableOnSide reports whether this side's column wears the
// fold button at its foot: the toolbar side only. The opposite side's rail is
// a docking convenience and keeps every row of its width.
func (state *State) toolbarRailFoldCapableOnSide(left bool) bool {
	return (state.terminalToolbarPosition == terminalToolbarLeft) == left
}

// forkRailColumnBounds is the room an unfolded column's groups take: the side's
// own bounds, minus the strip the fold button wears at the foot. The folded
// column needs no such room — its whole strip is the handle.
func (state *State) forkRailColumnBounds(size gfx.Size, left bool) gfx.Rect {
	bounds := state.toolbarDockSideBoundsFor(size, left)
	if state.toolbarRailFoldCapableOnSide(left) && !state.toolbarRailFoldedOnSide(left) {
		bounds.Height = max(bounds.Height-state.terminalToolbarMetrics().toolSize-4, 0)
	}
	return bounds
}

// clickForkRailFold answers the fold button and the handle. Saving stays with
// the click chain, which saves after a fold just as it does for the other
// fork toggles.
func (state *State) clickForkRailFold() bool {
	state.toolbarRailFolded = !state.toolbarRailFolded
	return true
}

// paintRailFoldHandle draws the folded column: the frame's own surface with one
// chevron in the middle, pointing the way the column will grow. The whole
// strip is the hit, so throwing the pointer at the edge finds it.
func (state *State) paintRailFoldHandle(canvas *gfx.Canvas, bounds gfx.Rect, left bool) {
	canvas.Fill(bounds, state.windowFrameColor())
	size := float32(16)
	icon := iconChevronLeft
	if left {
		icon = iconChevronRight
	}
	rect := gfx.Rect{X: bounds.X + (bounds.Width-size)/2, Y: bounds.Y + (bounds.Height-size)/2, Width: size, Height: size}
	canvas.DrawIcon(lucideIcons[icon], rect, state.pal.muted)
	canvas.HitWithOptions(hitForkRailFold, bounds, state.messages.Fork.RailExpandHint, gfx.HitOptions{Role: gfx.RoleButton})
}

// paintRailFoldToggle draws the fold button at the foot of an unfolded column:
// a chevron pair collapsing inward, the one tool on the rail that names the
// fold rather than a feature.
func (state *State) paintRailFoldToggle(canvas *gfx.Canvas, bounds gfx.Rect) {
	metrics := state.terminalToolbarMetrics()
	rect := gfx.Rect{X: bounds.X + (bounds.Width-metrics.toolSize)/2, Y: bounds.Bottom() - metrics.toolSize - 2, Width: metrics.toolSize, Height: metrics.toolSize}
	state.paintIconButtonSized(canvas, hitForkRailFold, iconChevronsCollapse, rect, state.messages.Fork.RailFoldHint, false, iconButtonQuiet, state.windowFrameColor(), metrics.iconSize)
}

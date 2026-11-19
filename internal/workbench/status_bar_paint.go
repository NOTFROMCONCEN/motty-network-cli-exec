package workbench

import (
	"github.com/vivaui/viva/gfx"
)

// The status bar's painting. Everything reads from the palette's own tokens —
// shell ground, line hairline, muted facts — so the bar follows whichever look
// is showing without colors of its own.

// paintStatusBar draws the strip the panes left at the window's bottom: the
// focused session's connection on the left, its device-CLI mode beside it, and
// the latency reading with the log-coloring eye on the right.
func (state *State) paintStatusBar(canvas *gfx.Canvas, size gfx.Size) {
	height := state.statusBarHeight()
	if height <= 0 {
		return
	}
	bar := gfx.Rect{X: 0, Y: max(size.Height-height, 0), Width: size.Width, Height: height}
	pal := state.pal
	canvas.Fill(bar, pal.shell)
	// A top hairline separates the bar from the pane above it, as the command
	// drawer's does.
	canvas.Fill(gfx.Rect{X: bar.X, Y: bar.Y, Width: bar.Width, Height: 1}, pal.line)

	pad := float32(10)
	pillHeight := height - 8
	pillY := bar.Y + (height-pillHeight)/2
	textStyle := gfx.TextStyle{Size: metrics.FontSizeS, Color: pal.text}
	mutedStyle := gfx.TextStyle{Size: metrics.FontSizeS, Color: pal.muted}

	// The right side is laid first, so the left knows how far it may run.
	eyeSide := pillHeight
	eyeRect := gfx.Rect{X: bar.Width - pad - eyeSide, Y: pillY, Width: eyeSide, Height: pillHeight}
	rightCursor := eyeRect.X
	latencyText, latencyColor, latencyOK := "", gfx.Color{}, false
	session := state.statusBarSession()
	if session != nil {
		latencyText, latencyColor, latencyOK = state.latencyInfo(session)
	}
	if latencyOK {
		w := canvas.TextWidthWeight(latencyText, metrics.FontSizeS, gfx.WeightRegular)
		bounds := gfx.Rect{X: rightCursor - 8 - w, Y: bar.Y, Width: w, Height: height}
		canvas.Label(latencyText, bounds, gfx.TextStyle{Size: metrics.FontSizeS, Color: latencyColor})
		rightCursor = bounds.X
	}

	// The eye is the toolbar's log-coloring toggle by another door: on shows the
	// open eye in the accent, off the closed one in muted, the hint the toolbar's.
	icon, iconColor := iconEye, pal.accent
	if !state.highlighting.on {
		icon, iconColor = iconEyeOff, pal.muted
	}
	canvas.DrawIcon(lucideIcons[icon], eyeRect.Inset(gfx.All(2)), iconColor)
	canvas.HitWithOptions(hitStatusBarLogColor, eyeRect, state.highlightHint(), gfx.HitOptions{Role: gfx.RoleButton})

	if session == nil {
		canvas.Label(state.messages.Fork.StatusBarNoSession,
			gfx.Rect{X: pad, Y: bar.Y, Width: max(rightCursor-pad-8, 0), Height: height}, mutedStyle)
		return
	}

	x := pad
	if !session.isLocal() {
		// The protocol's pill, the same one the hosts page wears.
		label := state.hostProtocolTagLabel(session.host)
		color := hostProtocolTagColor(pal, session.host.Protocol)
		w := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium) + 14
		pill := gfx.Rect{X: x, Y: pillY, Width: w, Height: pillHeight}
		paintHostProtocolTag(canvas, pill, label, color)
		x = pill.X + pill.Width + 8
	}
	// The address carries the session when the title and it differ; a local
	// terminal has only its title.
	name := session.Address
	if name == "" || session.isLocal() {
		name = session.Title
	}
	nameWidth := canvas.TextWidthWeight(name, metrics.FontSizeS, gfx.WeightRegular)
	maxName := max(rightCursor-x-8-pad, 0)
	nameWidth = min(nameWidth, maxName)
	nameBounds := gfx.Rect{X: x, Y: bar.Y, Width: nameWidth, Height: height}
	canvas.Label(statusBarFit(canvas, name, nameWidth, textStyle), nameBounds, textStyle)
	x = nameBounds.X + nameWidth + 8

	if phase, phaseColor := state.statusBarPhase(session); phase != "" {
		w := canvas.TextWidthWeight(phase, metrics.FontSizeS, gfx.WeightMedium)
		bounds := gfx.Rect{X: x, Y: bar.Y, Width: min(w, max(rightCursor-x-8-pad, 0)), Height: height}
		canvas.Label(phase, bounds, gfx.TextStyle{Size: metrics.FontSizeS, Color: phaseColor, Weight: gfx.WeightMedium})
		x = bounds.X + bounds.Width + 8
	}

	// The device-CLI badge: which platform the session's commands answer to.
	if label := statusBarExecLabel(session); label != "" {
		w := canvas.TextWidthWeight(label, metrics.FontSizeS, gfx.WeightMedium) + 14
		pill := gfx.Rect{X: x, Y: pillY, Width: min(w, max(rightCursor-x-pad, 0)), Height: pillHeight}
		canvas.Rounded(pill, pill.Height/2, withAlpha(pal.success, 0.14))
		canvas.Label(label, gfx.Rect{X: pill.X + 7, Y: pill.Y, Width: max(pill.Width-14, 0), Height: pill.Height},
			gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.success})
	}
}

// statusBarFit trims a label to the width its bounds give it, the way the
// assistant's chips trim theirs: whole runes, then an ellipsis.
func statusBarFit(canvas *gfx.Canvas, text string, width float32, style gfx.TextStyle) string {
	if canvas.TextWidthWeight(text, style.Size, style.Weight) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		if canvas.TextWidthWeight(string(runes)+"…", style.Size, style.Weight) <= width {
			return string(runes) + "…"
		}
	}
	return "…"
}

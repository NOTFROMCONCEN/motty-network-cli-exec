package workbench

// The interface-density choice. The fork's compact look tightens the framework
// metrics and the workbench's own chrome so a screen fits more terminals: an
// operations workbench spends its pixels on sessions, not on air. Everything
// here lives in fork-only files; the shared files keep the identifiers they
// always had, converted from constants to variables whose values this file
// owns, so a rebase meets only declaration lines.

// compactDensity is the compact look's switch. A package variable because the
// geometry it resizes — metrics, radius and the chrome variables below — is
// package state too; the process runs one workbench State.
var compactDensity = false

// applyDensity turns the compact look on or off: the framework metrics, the
// theme that copied them, and every chrome variable the conversion touched.
// Saving stays the caller's business.
func (state *State) applyDensity(compact bool) {
	if compactDensity == compact {
		return
	}
	compactDensity = compact
	// applyInterfaceTextSize rebuilds metrics and radius at the user's text
	// scale, rebuilds the theme that copied the metrics by value, and refreshes
	// the windows that draw with both; the chrome variables follow here.
	applyChromeMetrics()
	state.applyInterfaceTextSize()
}

// applyChromeMetrics sets the workbench's own geometry to its two sizes. The
// default branch restates upstream's values exactly, so switching back reads as
// the stock look. windowBarHeight keeps its constant 36 everywhere: the
// platform's titlebar strip is sized once at window creation and cannot follow.
func applyChromeMetrics() {
	if compactDensity {
		infoBarHeight = 30
		navigationWidth = 184
		verticalTabsWidth = 184
		verticalTabsRail = 40
		verticalTabHeight = 30
		verticalTabsHeader = 34
		verticalTabsFooter = 36
		barButtonSize = 24
		barMargin = 10
		barTabHeight = 24
		barTabMin = 88
		barTabMax = 160
		workspaceHeaderHeight = 32
		workspacePaneTabsHeight = 30
		hostGroupRowHeight = 36
		hostRowHeight = 54
		hostHeaderRowHeight = 26
		hostsToolbarTop = 10
		hostsBodyMargin = 14
		hostsBodyBottom = 12
		hostCardMinWidth = 260
		hostCardHeight = 54
		hostCardGap = 10
	} else {
		infoBarHeight = 34
		navigationWidth = 204
		verticalTabsWidth = navigationWidth
		verticalTabsRail = 48
		verticalTabHeight = 34
		verticalTabsHeader = 40
		verticalTabsFooter = 42
		barButtonSize = 28
		barMargin = 12
		barTabHeight = 28
		barTabMin = 96
		barTabMax = 170
		workspaceHeaderHeight = 38
		workspacePaneTabsHeight = 36
		hostGroupRowHeight = 42
		hostRowHeight = 64
		hostHeaderRowHeight = 28
		hostsToolbarTop = 14
		hostsBodyMargin = 18
		hostsBodyBottom = 18
		hostCardMinWidth = 280
		hostCardHeight = 64
		hostCardGap = 12
	}
	// barTabReach keeps the tab's click region covering its share of the bar
	// (Fitts's law); it derives from the two values above.
	barTabReach = (TitlebarHeight - barTabHeight) / 2
	// The assistant's title row stays as tall as the terminal's info bar.
	aiTitleRow = infoBarHeight - 1
}

// compactDensityOn reports the switch's current side, for the settings row.
func compactDensityOn() bool {
	return compactDensity
}

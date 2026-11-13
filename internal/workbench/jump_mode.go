package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// The jump mode: this machine sits behind a bastion, so every host without a
// jump route of its own connects through the saved SSH host the mode names.
// The switch and the host chooser live in the settings' connection page; the
// choice itself belongs to this computer and is never synced. The dialing side
// of the mode is hostCatalog.defaultJumpFor, which sshConfigFor consults when
// a host has no route of its own.

// setJumpMode switches the mode on or off and reports whether anything changed.
func (state *State) setJumpMode(on bool) bool {
	if state.jumpMode == on {
		return false
	}
	state.jumpMode = on
	state.syncDefaultJump()
	return true
}

// setJumpHost names the saved host the mode connects through, by catalog id.
func (state *State) setJumpHost(catalogID string) {
	if state.jumpHost == catalogID {
		return
	}
	state.jumpHost = catalogID
	state.syncDefaultJump()
}

// syncDefaultJump copies the mode's choice into the catalog the dialer reads.
func (state *State) syncDefaultJump() {
	state.hostCatalog.defaultJump.on = state.jumpMode
	state.hostCatalog.defaultJump.catalogID = state.jumpHost
}

// jumpModeSettingVisible is the chooser's guard: it only lives on the
// connection page, and a search draws the page itself, not the selects.
func (state *State) jumpModeSettingVisible() bool {
	return state.activeSection == sectionSettings && state.settingsCategory == settingsConnection && !state.settingsSearching()
}

// paintJumpSettings declares the mode's rows: the switch, and — while it is
// on — the chooser of the host every unrouted connection leaves through.
func (state *State) paintJumpSettings(page *settingsPage) {
	m := state.messages
	page.toggleRow(m.SettingsJumpMode, m.SettingsJumpModeDescription, state.jumpMode, hitSettingsJumpMode)
	if state.jumpMode {
		page.add(settingsControl{kind: controlJumpHost, slot: page.rowFrame(m.SettingsJumpHostRow, m.SettingsJumpHostDescription)})
	}
}

// jumpHostSummary is what the closed chooser shows: the chosen host's name.
func (state *State) jumpHostSummary() string {
	if state.jumpHost == "" {
		return state.messages.SettingsJumpHostNone
	}
	if host, ok := state.hostByRouteEnd(state.jumpHost, "", "", 0); ok {
		return host.Label
	}
	return state.messages.SettingsJumpHostNone
}

// paintJumpHostSelect paints the mode's chooser in the menu pass, outside the
// settings page's clip, so its popup can leave the page. It lists the saved
// SSH hosts: a jump host is reached over SSH.
func (state *State) paintJumpHostSelect(canvas *gfx.Canvas) {
	picker, m := &state.jumpPicker, state.messages
	bounds := picker.bounds
	if !state.jumpModeSettingVisible() || !state.jumpMode || state.modal != modalNone || bounds.Width <= 0 || bounds.Y < windowBarHeight || bounds.Y+bounds.Height > state.viewport.Height {
		picker.open = false
		return
	}
	jumps := state.jumpHostCandidates()
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(jumps)*32), max(above, below))
	content := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	scroll := state.selectScroll(picker.open, hitSettingsJumpDismiss)
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitSettingsJumpHosts, Label: m.SettingsJumpHostRow, Value: state.jumpHostSummary(),
		Open: picker.open, Content: content, Layer: popupLayer,
		Scope: hitSettingsJumpDismiss, DismissID: hitSettingsJumpDismiss, Viewport: state.windowRect(), Scroll: scroll,
	})
	defer control.End()
	if picker.open {
		state.menuPlaced(control.ContentBounds())
		for index, host := range jumps {
			control.AddItem(components.SelectItem{ID: hitSettingsJumpHostList.At(index), Label: host.Label, Selected: host.CatalogID == state.jumpHost})
		}
	}
}

// jumpHostCandidates lists the hosts a jump mode may name: the saved SSH
// hosts, in catalog order.
func (state *State) jumpHostCandidates() []Host {
	jumps := make([]Host, 0, len(state.hosts))
	for _, host := range state.hosts {
		if host.isSSH() && host.CatalogID != "" {
			jumps = append(jumps, host)
		}
	}
	return jumps
}

// clickJumpHost works the chooser: the trigger opens it, an item picks its
// host and closes, a press anywhere else closes it. It reports whether the id
// belonged to the mode's controls.
func (state *State) clickJumpHost(id gfx.ID) bool {
	if !state.jumpModeSettingVisible() {
		return false
	}
	picker := &state.jumpPicker
	switch id {
	case hitSettingsJumpMode:
		if state.setJumpMode(!state.jumpMode) {
			if !state.jumpMode {
				picker.open = false
			}
			state.save()
		}
		return true
	case hitSettingsJumpHosts:
		if !state.jumpMode {
			return true // the mode is off: the chooser stays closed
		}
		picker.open = !picker.open
		return true
	case hitSettingsJumpDismiss:
		if picker.open {
			picker.open = false
			state.save()
		}
		return true
	}
	jumps := state.jumpHostCandidates()
	index, ok := id.In(hitSettingsJumpHostList)
	if !ok || index < 0 || index >= len(jumps) {
		return false
	}
	state.setJumpHost(jumps[index].CatalogID)
	picker.open = false
	state.save()
	return true
}

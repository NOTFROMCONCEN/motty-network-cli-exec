package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
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
	if !on {
		state.jumpPicker.open = false
	}
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
		state.declareJumpPicker(page)
	}
}

// jumpHostSummary is what the closed chooser shows: the chosen host's name.
func (state *State) jumpHostSummary() string {
	if state.jumpHost == "" {
		return state.messages.SettingsJumpHostNone
	}
	if host, ok := state.hostByRouteEnd(state.jumpHost, "", "", 0, Host.isSSH); ok {
		return host.Label
	}
	return state.messages.SettingsJumpHostNone
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

// declareJumpPicker declares the mode's select: its trigger in the row, and the
// menu of saved SSH hosts. A choice picks the host and closes the menu; a press
// anywhere else closes it and saves.
func (state *State) declareJumpPicker(page *settingsPage) {
	picker, m, c := &state.jumpPicker, state.messages, page.c
	if picker.open && (!state.jumpModeSettingVisible() || state.modal != modalNone || !state.jumpMode) {
		picker.open = false
	}
	if !picker.open && picker.shown {
		picker.shown = false
		state.save()
	}
	controlWidth := max(min(float32(300), max(float32(190), page.width*0.40)), 0)
	controlWidth = min(controlWidth, max(page.width-60, 0))
	page.lineWith(m.SettingsJumpHostRow, m.SettingsJumpHostDescription, controlWidth, func() {
		summary := state.jumpHostSummary()
		trigger := ui.Button(c, summary).Outline().Icon(lucideIcons[iconChevronDown]).Width(controlWidth).
			HitWith(hitSettingsJumpHosts, m.SettingsJumpHostRow, gfx.HitOptions{Role: gfx.RoleCombobox, Value: summary, Expanded: picker.open})
		if !picker.open {
			return
		}
		picker.shown = true
		ui.PopupLayer(c, popupLayer)
		ui.Menu(c, trigger, &picker.open, func(items *ui.MenuItems) {
			items.RowHeight(contextMenuRow)
			for _, host := range state.jumpHostCandidates() {
				host := host
				if items.Item(host.Label).Clicked() {
					c.After(func() {
						state.setJumpHost(host.CatalogID)
						picker.open = false
						state.save()
					})
				}
			}
		})
	})
}

// clickJumpHost works the mode's controls: the switch flips the mode, and the
// chooser's trigger opens the host menu while the mode is on — a second press
// closes it.
func (state *State) clickJumpHost(id gfx.ID) bool {
	if !state.jumpModeSettingVisible() {
		return false
	}
	if id == hitSettingsJumpMode {
		if state.setJumpMode(!state.jumpMode) {
			state.save()
		}
		return true
	}
	if id == hitSettingsJumpHosts {
		if !state.jumpMode {
			return true // the mode is off: the chooser stays closed
		}
		state.jumpPicker.open = !state.jumpPicker.open
		return true
	}
	return false
}

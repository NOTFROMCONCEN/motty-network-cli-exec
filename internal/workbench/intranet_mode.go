package workbench

import "motty/internal/netguard"

// The intranet mode: the switch lives in Settings under Security, the state
// belongs to this computer (see savedState.IntranetMode), and applying it sets
// the process-wide gate every internet-facing feature asks before it dials.

// setIntranetMode switches the mode on or off and applies it to the process at
// once. It reports whether the mode changed, so the caller saves only a real
// change.
func (state *State) setIntranetMode(on bool) bool {
	if state.intranetMode == on {
		return false
	}
	state.intranetMode = on
	netguard.SetEnabled(on)
	return true
}

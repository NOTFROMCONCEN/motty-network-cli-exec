package workbench

import (
	"testing"

	"motty/internal/netguard"
	"motty/internal/update"
)

// The intranet mode refuses the update check on purpose: the release feed is
// out of this network. The user asked for the check, so the window must say
// what happened in the user's words and where the switch is — not show the
// raw refusal of the gate, which names an address the user never asked for.
func TestTheUpdateCheckSaysTheIntranetModeRefusedIt(t *testing.T) {
	netguard.SetEnabled(true)
	defer netguard.SetEnabled(false)

	_, options := newWorld(t)
	options.Invalidate = func() {}
	options.CheckUpdate = func() (update.Release, error) {
		return update.Release{}, netguard.CheckURL("https://api.github.com/repos/t8y2/motty/releases/latest")
	}
	state := newTestState(t, options)
	state.Click(hitSettingsUpdateCheck)
	waitUpdate(t, state, func() bool { return !state.update.checking && state.update.text != "" })

	if state.update.text != state.messages.Tools.UpdateBlockedByIntranet {
		t.Fatalf("the update text is %q, want the intranet notice %q",
			state.update.text, state.messages.Tools.UpdateBlockedByIntranet)
	}
	// No release was found, so nothing is offered.
	if state.update.available || state.modal != modalNone {
		t.Fatalf("a refused check offered a release: available=%v modal=%v", state.update.available, state.modal)
	}
}

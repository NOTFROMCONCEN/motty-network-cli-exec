package workbench

import (
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/netcli"
)

// The execution setting of the host dialog: how AI-run commands are typed into
// this host's terminals, and which network CLI platform to assume. Following the
// transport's default stays the first choice; the two explicit values exist so
// an SSH device can speak its CLI and a Telnet shell farm can keep its shells.

// execChoice indexes the host dialog's execution-mode segmented control.
type execChoice uint8

const (
	execChoiceAuto execChoice = iota // follow the transport's default
	execChoiceShell
	execChoiceCLI
)

var hostExecUiHits = tree.Feature("host-exec-ui")

var (
	hitDialogExecAuto         = tree.ID(hostExecUiHits, "dialog-exec-auto")
	hitDialogExecShell        = tree.ID(hostExecUiHits, "dialog-exec-shell")
	hitDialogExecCLI          = tree.ID(hostExecUiHits, "dialog-exec-cli")
	hitDialogCLIPlatform      = tree.ID(hostExecUiHits, "dialog-cli-platform")
	hitDialogCLIPlatformDism  = tree.ID(hostExecUiHits, "dialog-cli-platform-dismiss")
	hitDialogCLIPlatformItems = tree.Sub(hostExecUiHits, tree.Name("dialog-cli-platform-item"))
)

// savedExecMode is the state-file form of the dialog's choice; automatic is empty.
func savedExecMode(choice execChoice) string {
	switch choice {
	case execChoiceShell:
		return "shell"
	case execChoiceCLI:
		return "network_cli"
	}
	return ""
}

// execChoiceFor reads a saved mode into the dialog's choice; an unknown value
// reads as automatic, the way an entry without the setting would.
func execChoiceFor(saved string) execChoice {
	switch saved {
	case "shell":
		return execChoiceShell
	case "network_cli":
		return execChoiceCLI
	}
	return execChoiceAuto
}

// netcliIDs is the base platform list the dialog offers: automatic detection,
// generic, then every loaded table.
func netcliIDs() []string { return append([]string{""}, netcli.IDs()...) }

// platformIDs is the base list plus the edited host's own id when its table is
// not loaded. Keeping it in the list means editing a host whose platform file
// is missing cannot silently turn the saved platform into automatic detection.
func (editor *hostEditor) platformIDs() []string {
	ids := netcliIDs()
	if saved := editor.cliPlatformSaved; saved != "" && !slices.Contains(ids, saved) {
		ids = append(ids, saved)
	}
	return ids
}

// platformIndexFor is the dialog's list position of a saved platform id. The
// empty id is the first entry, automatic detection; a foreign id is only
// reachable when platformIDs appended it.
func platformIndexFor(ids []string, saved string) int {
	if saved == "" {
		return 0
	}
	if index := slices.Index(ids, saved); index >= 0 {
		return index
	}
	return 0
}

// clipPlatformName is the label of a platform id in the dialog. netcli cannot
// name the empty id, which is the automatic choice.
func clipPlatformName(id string, m i18n.ToolsMessages) string {
	if id == "" {
		return m.CLIPlatformAuto
	}
	return netcli.Name(id)
}

// execPaintable reports whether the host being edited has a terminal whose
// execution mode can be set: everything except the remote desktops.
func execPaintable(protocol hostProtocol) bool {
	return protocol != protocolVNC && protocol != protocolRDP
}

func (state *State) paintExecModeChoice(canvas *ui.Canvas, bounds ui.Rect) {
	m := state.messages.Tools
	content := components.BeginField(canvas, state.Theme(), bounds).Paint(m.ExecModeField, "", "")
	options := [...]components.SegmentOption{
		{ID: hitDialogExecAuto, Label: m.ExecModeAuto},
		{ID: hitDialogExecShell, Label: m.ExecModeShell},
		{ID: hitDialogExecCLI, Label: m.ExecModeNetworkCLI},
	}
	components.BeginSegmentedControl(canvas, state.Theme(), content, options[:], options[state.editor.execChoice].ID).Paint()
	state.rememberHostControl(options[state.editor.execChoice].ID, content)
}

func (state *State) paintCLIPlatformChoice(canvas *ui.Canvas, bounds ui.Rect) {
	editor, theme := &state.editor, state.Theme()
	m := state.messages.Tools
	ids := editor.platformIDs()
	content := components.BeginField(canvas, theme, bounds).Paint(m.CLIPlatformField, "", "")
	state.rememberHostControl(hitDialogCLIPlatform, content)
	portal := state.beginHostFormPopup(canvas, editor.cliPlatformOpen)
	defer state.endHostFormPopup(canvas, portal)
	// A user file can add many vendors. Keep the popup a comfortable height and
	// let the select scroll rather than run off the window.
	list := state.hostAuthListBounds(content, min(len(ids), 12))
	selectControl := components.BeginSelect(canvas, theme, content, components.SelectOptions{
		TriggerID: hitDialogCLIPlatform, Label: m.CLIPlatformField, Value: clipPlatformName(ids[editor.cliPlatform], m),
		Open: editor.cliPlatformOpen, Content: list,
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitDialogCLIPlatformDism, Viewport: state.windowRect(),
		Scroll: state.menuScrollFor(hitDialogCLIPlatformDism),
	})
	defer selectControl.End()
	if editor.cliPlatformOpen {
		state.menuPlaced(list)
	}
	for index, id := range ids {
		selectControl.AddItem(components.SelectItem{
			ID: hitDialogCLIPlatformItems.At(index), Label: clipPlatformName(id, m), Selected: index == editor.cliPlatform,
		})
	}
}

// clickExecChoice handles a click on the execution setting and reports whether
// it was one.
func (state *State) clickExecChoice(id ui.ID) bool {
	editor := &state.editor
	switch {
	case id == hitDialogExecAuto:
		editor.execChoice = execChoiceAuto
	case id == hitDialogExecShell:
		editor.execChoice = execChoiceShell
	case id == hitDialogExecCLI:
		editor.execChoice = execChoiceCLI
	case id == hitDialogCLIPlatform:
		editor.cliPlatformOpen = !editor.cliPlatformOpen
	case id == hitDialogCLIPlatformDism:
		editor.cliPlatformOpen = false
	case id.Scope() == hitDialogCLIPlatformItems:
		index := item(id)
		if index >= len(editor.platformIDs()) {
			return false
		}
		editor.cliPlatform = index
		editor.cliPlatformOpen = false
	default:
		return false
	}
	state.dialogError = ""
	return true
}

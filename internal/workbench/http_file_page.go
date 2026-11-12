package workbench

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/httpfile"
	"motty/internal/i18n"
)

// The page's own hit identities: the two actions of the header, the three of
// the address row and the browse button of the settings card.
var httpFileHits = tree.Feature("http-file")

var (
	hitHTTPFileStart  = tree.ID(httpFileHits, "start")
	hitHTTPFileStop   = tree.ID(httpFileHits, "stop")
	hitHTTPFileCopy   = tree.ID(httpFileHits, "copy-address")
	hitHTTPFileOpen   = tree.ID(httpFileHits, "open-address")
	hitHTTPFileBrowse = tree.ID(httpFileHits, "browse")
	hitHTTPFileDir    = tree.ID(httpFileHits, "directory")
	hitHTTPFilePort   = tree.ID(httpFileHits, "port")
)

// httpFileEntered is called when the page is opened, so the two fields show
// the stored choices rather than whatever the last visit left in them.
func (state *State) httpFileEntered() {
	state.httpFile.fieldsSynced = false
	state.httpFile.notice, state.httpFile.noticeBad = "", false
}

// httpFileSyncFields fills the two editors from the stored choices once per
// visit. A start normalizes them (an empty port becomes the default), so it
// asks for a sync again.
func (state *State) httpFileSyncFields() {
	view := &state.httpFile
	if view.fieldsSynced {
		return
	}
	view.fieldsSynced = true
	view.dirEditor = text.Editor{}
	if view.root != "" {
		view.dirEditor.Insert(view.root)
	}
	view.portEditor = text.Editor{}
	if view.port != 0 {
		view.portEditor.Insert(strconv.Itoa(int(view.port)))
	}
}

// declareHTTPServerWorkspace declares the HTTP file-server page in the
// workbench page: the header with its one action, the listener's state and
// counters, and the settings a start reads. The directory and the port are
// read-only while the server runs: changing either means a restart, so the
// page says so instead of taking an edit that would not take effect.
func (state *State) declareHTTPServerWorkspace(c ui.C) {
	m, pal := state.messages.HTTPFile, state.pal
	view := &state.httpFile
	state.httpFileSyncFields()
	snapshot := state.httpFileSnapshot()
	running := snapshot.Running

	if !view.dirBound {
		view.dirField.Bind(&view.dirEditor)
		view.dirBound = true
	}
	if !view.portBound {
		view.portField.Bind(&view.portEditor)
		view.portBound = true
	}

	body := ui.Column(c).Fill().PadSides(14, 20, 0, 20)
	body.Clip().Children(func() {
		state.declareHTTPFileHeader(c, m, running)
		state.declareHTTPFileStatus(c, m, pal, snapshot, running)
		state.declareHTTPFileSettings(c, m, pal, running)
	})
	// The counters move while the listener is up; one frame a second keeps
	// them honest without a timer of the page's own.
	if running {
		c.Canvas().RequestFrameIn(time.Second)
	}
}

// declareHTTPFileHeader is the page's title, what it is for, and the one
// action the listener's state allows.
func (state *State) declareHTTPFileHeader(c ui.C, m i18n.HTTPFileMessages, running bool) {
	pal := state.pal
	ui.Row(c).MinHeight(56).Gap(10).MarginSides(0, 0, 12, 0).Items(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(3).MarginSides(2, 0, 0, 0).Children(func() {
			ui.Text(c, m.Title).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
			ui.Paragraph(c, m.Description, 2).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(19)
		})
		if running {
			state.declareButtonSized(c, hitHTTPFileStop, m.Stop, iconStop, false, true, true, toolbarFont, false).
				Height(toolbarHeight).Shrink(0)
			return
		}
		state.declareButtonSized(c, hitHTTPFileStart, m.Start, iconPlay, true, false, true, toolbarFont, false).
			Height(toolbarHeight).Shrink(0)
	})
}

// declareHTTPFileStatus is the card that says whether the listener is up, what
// it answers on, and what it has done so far.
func (state *State) declareHTTPFileStatus(c ui.C, m i18n.HTTPFileMessages, pal *palette, snapshot httpfile.Snapshot, running bool) {
	view := &state.httpFile
	ui.Column(c).FillWidth().Gap(10).MarginSides(0, 0, 12, 0).PadSides(14, 16, 14, 16).
		Radius(radius.popup).Background(pal.surface).Children(func() {
		// The state line: a coloured dot, the state in words, and when this
		// run began.
		ui.Row(c).Height(22).Gap(8).Items(ui.Center).Children(func() {
			dot, stateText := pal.faint, m.StatusStopped
			if running {
				dot, stateText = pal.success, m.StatusRunning
			}
			ui.Text(c, "●").FontSize(13).Color(dot)
			ui.Text(c, stateText).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text)
			ui.Spacer(c).Grow(1)
			if running && !snapshot.StartedAt.IsZero() {
				ui.Text(c, fmt.Sprintf(m.SinceFmt, snapshot.StartedAt.Format("15:04:05"))).
					FontSize(metrics.FontSizeS).Color(pal.faint)
			}
		})
		// The address row: the one the page offers first, with the two things
		// done with it, then the rest of this machine's addresses quietly.
		ui.Row(c).Height(36).Gap(8).Items(ui.Center).Children(func() {
			ui.Text(c, m.AddressLabel).FontSize(metrics.FontSizeS).Color(pal.muted).Shrink(0)
			address := state.httpFilePrimaryAddress()
			if address == "" {
				address = "—"
			}
			ui.Text(c, address).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).
				MonoFace(state.terminalFontFace).Grow(1).Ellipsis()
			if running {
				state.declareButtonSized(c, hitHTTPFileCopy, m.CopyAddress, iconCopy, false, false, true, toolbarFont, true).
					Size(36, 32).Shrink(0)
				state.declareButtonSized(c, hitHTTPFileOpen, m.OpenAddress, iconExternal, false, false, true, toolbarFont, true).
					Size(36, 32).Shrink(0)
			}
		})
		if running {
			if rest := httpFileOtherAddresses(snapshot.Addresses); rest != "" {
				ui.Text(c, rest).FontSize(metrics.FontSizeS).Color(pal.faint).MonoFace(state.terminalFontFace).
					MaxWidth(560).Ellipsis()
			}
			ui.Paragraph(c, m.ExternalHint, 2).FontSize(metrics.FontSizeS).Color(pal.faint).LineHeight(18)
		}
		// The counters of this run.
		ui.Row(c).Height(46).Gap(10).MarginSides(2, 0, 0, 0).Children(func() {
			state.declareHTTPFileStat(c, pal, fmt.Sprintf(m.RequestsFmt, snapshot.Requests), m.StatsRequests)
			state.declareHTTPFileStat(c, pal, httpfile.HumanSize(int64(snapshot.Uploaded)), m.StatsUploaded)
			state.declareHTTPFileStat(c, pal, httpfile.HumanSize(int64(snapshot.Downloaded)), m.StatsDownloaded)
			state.declareHTTPFileStat(c, pal, fmt.Sprintf(m.ActiveFmt, snapshot.Active), m.StatsActive)
		})
		// What the last action said.
		if view.notice != "" {
			color := pal.muted
			if view.noticeBad {
				color = pal.danger
			}
			ui.Text(c, view.notice).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(color).
				MaxWidth(620).Ellipsis()
		}
	})
}

// declareHTTPFileStat is one counter of the status card: its value over its name.
func (state *State) declareHTTPFileStat(c ui.C, pal *palette, value, label string) {
	ui.Column(c).Grow(1).Gap(2).MinWidth(90).Children(func() {
		ui.Text(c, value).FontSize(15).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
		ui.Text(c, label).FontSize(metrics.FontSizeS).Color(pal.faint).Ellipsis()
	})
}

// declareHTTPFileSettings is the card of what is shared and what a visitor may
// do with it. The switches apply to a running server at once; the directory and
// the port belong to the next start.
func (state *State) declareHTTPFileSettings(c ui.C, m i18n.HTTPFileMessages, pal *palette, running bool) {
	view := &state.httpFile
	ui.Column(c).FillWidth().Gap(12).PadSides(14, 16, 16, 16).
		Radius(radius.popup).Background(pal.surface).Children(func() {
		ui.Text(c, m.SettingsTitle).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text)
		if running {
			// The values in effect are shown as text: an edit here would not
			// reach the listener that is up.
			state.declareHTTPFileReadOnlyRow(c, pal, m.DirectoryLabel, view.root)
			state.declareHTTPFileReadOnlyRow(c, pal, m.PortLabel, strconv.Itoa(int(view.port)))
			ui.Text(c, m.LockedHint).FontSize(metrics.FontSizeS).Color(pal.faint)
		} else {
			ui.Row(c).Height(36).Gap(8).Items(ui.Center).Children(func() {
				ui.TextFieldID(c, hitHTTPFileDir, &view.dirField, m.DirectoryLabel, m.DirectoryPlaceholder).
					Height(36).Grow(1).Shrink(0).NavigationKeys()
				state.declareButtonSized(c, hitHTTPFileBrowse, m.Browse, iconFolderOpen, false, false, true, toolbarFont, false).
					Height(toolbarHeight).Shrink(0)
			})
			ui.Row(c).Height(36).Gap(10).Items(ui.Center).Children(func() {
				ui.Text(c, m.PortLabel).FontSize(metrics.FontSizeS).Color(pal.muted).Shrink(0)
				ui.TextFieldID(c, hitHTTPFilePort, &view.portField, m.PortLabel, m.PortPlaceholder).
					Width(130).Height(36).Shrink(0).NavigationKeys()
				ui.Spacer(c).Grow(1)
				ui.Text(c, m.PortHint).FontSize(metrics.FontSizeS).Color(pal.faint)
			})
		}
		ui.Divider(c).Background(pal.line).MarginSides(2, 0, 2, 0)
		state.declareHTTPFileSwitch(c, m.AllowUpload, m.AllowUploadHint, &view.allowUpload)
		state.declareHTTPFileSwitch(c, m.AllowDelete, m.AllowDeleteHint, &view.allowDelete)
		state.declareHTTPFileSwitch(c, m.AllowListing, m.AllowListingHint, &view.allowListing)
	})
}

// declareHTTPFileReadOnlyRow is one setting shown as text while the server
// runs: its name, then its value in effect.
func (state *State) declareHTTPFileReadOnlyRow(c ui.C, pal *palette, label, value string) {
	ui.Row(c).Height(30).Gap(10).Items(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(metrics.FontSizeS).Color(pal.muted).Shrink(0)
		ui.Text(c, value).FontSize(metrics.FontSizeS).Color(pal.text).MonoFace(state.terminalFontFace).
			Grow(1).Ellipsis()
	})
}

// declareHTTPFileSwitch is one right a visitor has: the switch, its name and
// what it means. A change is pushed into the running server at once, so the
// switches never need a restart.
func (state *State) declareHTTPFileSwitch(c ui.C, label, hint string, value *bool) {
	pal := state.pal
	ui.Row(c).MinHeight(36).Gap(10).Items(ui.Center).Children(func() {
		if ui.Switch(c, value, "").Changed() {
			state.httpFileApplySwitches()
		}
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, label).FontSize(metrics.FontSizeM).Color(pal.text)
			ui.Text(c, hint).FontSize(metrics.FontSizeS).Color(pal.faint)
		})
	})
}

// httpFileOtherAddresses is the addresses the primary one is not, joined for
// one quiet line under the address row.
func httpFileOtherAddresses(addresses []string) string {
	primary := ""
	if len(addresses) > 0 {
		primary = addresses[0]
	}
	var rest []string
	for _, address := range addresses {
		if address != primary && !strings.HasPrefix(address, "http://127.0.0.1") {
			rest = append(rest, address)
		}
	}
	return strings.Join(rest, "   ·   ")
}

// clickHTTPServer applies the page's own controls: the two header actions, the
// address row's two, and the browse button. It answers only while the page is
// the one on show and no dialog owns the pointer.
func (state *State) clickHTTPServer(id gfx.ID) bool {
	if state.activeSection != sectionHTTPFile || state.modal != modalNone {
		return false
	}
	switch id {
	case hitHTTPFileStart:
		state.startHTTPServer()
	case hitHTTPFileStop:
		state.stopHTTPServer()
	case hitHTTPFileCopy:
		state.httpFileCopyAddress()
	case hitHTTPFileOpen:
		state.httpFileOpenAddress()
	case hitHTTPFileBrowse:
		state.httpFileBrowse()
	default:
		return false
	}
	return true
}

// httpFileKey gives the page's fields the keys the framework leaves to the
// application: Enter in the port field starts the server, the way the header's
// button does.
func (state *State) httpFileKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionHTTPFile || state.modal != modalNone || event.Release || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Key != gfx.KeyEnter {
		return gfx.EventResult{}, false
	}
	switch event.Target {
	case hitHTTPFilePort:
		state.startHTTPServer()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case hitHTTPFileDir:
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

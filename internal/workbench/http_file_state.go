package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/httpfile"
)

// httpFileState is the HTTP file-server page's half of the window: what is
// shared, the switches a visitor's rights hang on, and the server they run.
// The root, the port and the switches survive a restart; everything else is
// view state of one window.
type httpFileState struct {
	root         string
	port         uint16
	allowUpload  bool
	allowDelete  bool
	allowListing bool

	// The two fields the page declares, each bound to an editor the state
	// keeps, so a start can read them and a restore can fill them.
	dirEditor  text.Editor
	portEditor text.Editor
	dirField   ui.Field
	portField  ui.Field
	dirBound   bool
	portBound  bool
	// fieldsSynced says the two editors hold the stored choices of this visit;
	// entering the page and a start both ask for a sync again.
	fieldsSynced bool

	// notice is what the last action reported, and noticeBad whether it was a
	// failure.
	notice    string
	noticeBad bool

	// server is made on first use and keeps the listener across repaints.
	server    *httpfile.Server
	serverMu  sync.Mutex
	startedAt time.Time
}

// defaultHTTPFileRoot is where a fresh window points the shared folder: the
// user's home directory, so the first start has something real to share.
func defaultHTTPFileRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return home
}

// httpFileServer returns the page's server, making it on first use. The wake
// function lets a background change ask the window to repaint; this server has
// none, so it is only there for a future that does.
func (state *State) httpFileServer() *httpfile.Server {
	state.httpFile.serverMu.Lock()
	defer state.httpFile.serverMu.Unlock()
	if state.httpFile.server == nil {
		state.httpFile.server, _ = httpfile.New(state.httpFileConfig())
	}
	return state.httpFile.server
}

// httpFileConfig is the configuration the page's current choices describe.
func (state *State) httpFileConfig() httpfile.Config {
	return httpfile.Config{
		Root:         state.httpFile.root,
		Port:         state.httpFile.port,
		AllowUpload:  state.httpFile.allowUpload,
		AllowDelete:  state.httpFile.allowDelete,
		AllowListing: state.httpFile.allowListing,
	}
}

// httpFileRunning reports whether the listener is up. The server is the truth;
// this is what a paint reads.
func (state *State) httpFileRunning() bool {
	server := state.httpFileServer()
	return server != nil && server.Running()
}

// httpFileSnapshot is the server's state and counters, or a zero snapshot when
// no server has been made yet.
func (state *State) httpFileSnapshot() httpfile.Snapshot {
	server := state.httpFileServer()
	if server == nil {
		return httpfile.Snapshot{}
	}
	return server.Snapshot()
}

// httpFilePortValue reads the port field. An empty field means the default.
func (state *State) httpFilePortValue() (uint16, bool) {
	value := strings.TrimSpace(state.httpFile.portEditor.String())
	if value == "" {
		return httpfile.DefaultPort, true
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return uint16(port), true
}

// httpFileRootValue reads the directory field, or the stored root when the
// field is empty.
func (state *State) httpFileRootValue() string {
	value := strings.TrimSpace(state.httpFile.dirEditor.String())
	if value == "" {
		return state.httpFile.root
	}
	return value
}

// startHTTPServer takes the choices on the page and brings the listener up.
// The fields are read first, so a typo is reported before anything changes.
func (state *State) startHTTPServer() {
	port, ok := state.httpFilePortValue()
	if !ok {
		state.httpFileNotice(state.messages.HTTPFile.InvalidPort, true)
		return
	}
	root := state.httpFileRootValue()
	if root == "" {
		state.httpFileNotice(state.messages.HTTPFile.InvalidRoot, true)
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		state.httpFileNotice(state.messages.HTTPFile.InvalidRoot, true)
		return
	}
	state.httpFile.root = root
	state.httpFile.port = port
	state.httpFileNotice("", false)
	// A server that is already up is stopped first: its root or port may be
	// exactly what changed.
	server := state.httpFileServer()
	if server.Running() {
		if err := server.Stop(); err != nil {
			state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.StartFailedFmt, err), true)
			return
		}
	}
	state.httpFile.serverMu.Lock()
	state.httpFile.server, err = httpfile.New(state.httpFileConfig())
	state.httpFile.serverMu.Unlock()
	if err != nil {
		state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.StartFailedFmt, err), true)
		return
	}
	if err := state.httpFile.server.Start(); err != nil {
		state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.StartFailedFmt, httpFileError(err)), true)
		return
	}
	state.httpFile.startedAt = time.Now()
	state.httpFile.fieldsSynced = false
	state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.StartedFmt, state.httpFilePrimaryAddress()), false)
	state.save()
}

// stopHTTPServer closes the listener and keeps the choices for the next start.
func (state *State) stopHTTPServer() {
	server := state.httpFileServer()
	if server == nil || !server.Running() {
		return
	}
	if err := server.Stop(); err != nil {
		state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.StopFailedFmt, err), true)
		return
	}
	state.httpFileNotice(state.messages.HTTPFile.Stopped, false)
	state.save()
}

// httpFileApplySwitches pushes the page's switches into the running server, so
// an upload, delete or listing switch takes effect without a restart.
func (state *State) httpFileApplySwitches() {
	server := state.httpFileServer()
	if server != nil && server.Running() {
		server.SetSwitches(state.httpFile.allowUpload, state.httpFile.allowDelete, state.httpFile.allowListing)
	}
	state.save()
}

// httpFilePrimaryAddress is the address the page shows first: loopback when
// that is all there is, otherwise the first address of a real interface.
func (state *State) httpFilePrimaryAddress() string {
	snapshot := state.httpFileSnapshot()
	if len(snapshot.Addresses) == 0 {
		return ""
	}
	for _, address := range snapshot.Addresses {
		if !strings.HasPrefix(address, "http://127.0.0.1") {
			return address
		}
	}
	return snapshot.Addresses[0]
}

// httpFileCopyAddress puts the primary address on the clipboard.
func (state *State) httpFileCopyAddress() {
	address := state.httpFilePrimaryAddress()
	if address == "" {
		return
	}
	if err := state.options.SetClipboardText(address); err != nil {
		state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.OpenFailedFmt, err), true)
		return
	}
	state.httpFileNotice(state.messages.HTTPFile.AddressCopied, false)
}

// httpFileOpenAddress opens the primary address in the system browser.
func (state *State) httpFileOpenAddress() {
	address := state.httpFilePrimaryAddress()
	if address == "" {
		return
	}
	if err := state.options.OpenURL(address); err != nil {
		state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.OpenFailedFmt, err), true)
	}
}

// httpFileBrowse asks the system for a directory to share.
func (state *State) httpFileBrowse() {
	chosen, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: state.messages.HTTPFile.DirectoryLabel})
	if err != nil {
		state.httpFileNotice(fmt.Sprintf(state.messages.HTTPFile.BrowseFailedFmt, err), true)
		return
	}
	if chosen == "" {
		return
	}
	state.httpFileNotice("", false)
	state.httpFile.dirEditor = text.Editor{}
	state.httpFile.dirEditor.Insert(filepath.Clean(chosen))
}

// httpFileNotice records what the last action reported.
func (state *State) httpFileNotice(text string, bad bool) {
	state.httpFile.notice, state.httpFile.noticeBad = text, bad
}

// httpFileError turns a listener failure into something a user can act on: a
// port already taken is the one they will hit, and its wording names the port.
func httpFileError(err error) error {
	var opErr *os.SyscallError
	if errors.As(err, &opErr) {
		return fmt.Errorf("%s (%s)", opErr.Err, opErr.Syscall)
	}
	return err
}

// closeHTTPServer stops the listener when the window closes. The choices stay
// in the saved state; the next run starts from them again.
func (state *State) closeHTTPServer() {
	state.httpFile.serverMu.Lock()
	server := state.httpFile.server
	state.httpFile.server = nil
	state.httpFile.serverMu.Unlock()
	if server != nil && server.Running() {
		server.Stop()
	}
}

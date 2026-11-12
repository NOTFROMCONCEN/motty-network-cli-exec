package workbench

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/httpfile"
)

// httpFileState is a state on the file-server page, with a shared folder of
// its own and nothing else changed.
func httpFileTestState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionHTTPFile
	state.httpFile.root = t.TempDir()
	state.httpFile.port = httpfile.DefaultPort
	state.httpFileEntered()
	t.Cleanup(func() { state.closeHTTPServer() })
	return state
}

func TestHTTPFilePageDeclaresItsControls(t *testing.T) {
	state := httpFileTestState(t)
	canvas, texts := paintedTexts(state)
	for _, hit := range []gfx.ID{hitHTTPFileStart, hitHTTPFileDir, hitHTTPFilePort, hitHTTPFileBrowse} {
		if _, ok := hitByID(canvas, hit); !ok {
			t.Fatalf("the page does not declare %v", hit)
		}
	}
	if !contains(texts, state.messages.HTTPFile.Title) {
		t.Fatalf("texts = %v, want the page title", texts)
	}
	if !contains(texts, state.messages.HTTPFile.StatusStopped) {
		t.Fatalf("texts = %v, want the stopped state", texts)
	}
}

func TestHTTPFileNavOpensThePage(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.activeSection == sectionHTTPFile {
		t.Fatal("a fresh window starts somewhere else")
	}
	state.Click(hitHTTPFile)
	if state.activeSection != sectionHTTPFile {
		t.Fatalf("activeSection = %d, want the file server", state.activeSection)
	}
}

func TestHTTPFileStartServesTheFolderAndStopClosesIt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := httpFileTestState(t)
	state.httpFile.root = root
	state.httpFile.port = freeWorkbenchPort(t)
	state.httpFileEntered()

	state.Click(hitHTTPFileStart)
	if !state.httpFileRunning() {
		t.Fatalf("the server did not start: %q", state.httpFile.notice)
	}
	if state.httpFile.notice == "" || state.httpFile.noticeBad {
		t.Fatalf("notice = %q bad=%v, want the started line", state.httpFile.notice, state.httpFile.noticeBad)
	}
	// The folder is served: a file comes back and the counters move.
	address := state.httpFilePrimaryAddress()
	response, err := http.Get(address + "/note.txt")
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	body := make([]byte, 32)
	count, _ := response.Body.Read(body)
	response.Body.Close()
	if string(body[:count]) != "shared" {
		t.Fatalf("served %q, want the shared file", body[:count])
	}
	if snapshot := state.httpFileSnapshot(); snapshot.Downloaded == 0 || snapshot.Requests == 0 {
		t.Fatalf("snapshot = %+v, want the request counted", snapshot)
	}
	// The fields are read-only while it runs: the page shows the values in
	// effect instead of taking an edit that would not reach the listener.
	_, texts := paintedTexts(state)
	if !contains(texts, root) {
		t.Fatalf("texts = %v, want the shared folder in effect", texts)
	}

	state.Click(hitHTTPFileStop)
	if state.httpFileRunning() {
		t.Fatal("the server is still running")
	}
	if state.httpFile.notice != state.messages.HTTPFile.Stopped {
		t.Fatalf("notice = %q, want the stopped line", state.httpFile.notice)
	}
}

func TestHTTPFileStartReportsABadPort(t *testing.T) {
	state := httpFileTestState(t)
	state.httpFile.portEditor.Insert("99999")
	state.Click(hitHTTPFileStart)
	if state.httpFileRunning() {
		t.Fatal("the server started on a port that is not a port")
	}
	if !state.httpFile.noticeBad || state.httpFile.notice != state.messages.HTTPFile.InvalidPort {
		t.Fatalf("notice = %q bad=%v, want the invalid-port line", state.httpFile.notice, state.httpFile.noticeBad)
	}
}

func TestHTTPFileStartReportsAMissingFolder(t *testing.T) {
	state := httpFileTestState(t)
	state.httpFile.root = filepath.Join(t.TempDir(), "gone")
	state.httpFileEntered()
	state.Click(hitHTTPFileStart)
	if state.httpFileRunning() {
		t.Fatal("the server started without a folder")
	}
	if !state.httpFile.noticeBad || state.httpFile.notice != state.messages.HTTPFile.InvalidRoot {
		t.Fatalf("notice = %q bad=%v, want the invalid-folder line", state.httpFile.notice, state.httpFile.noticeBad)
	}
}

func TestHTTPFileSwitchesApplyToTheRunningServer(t *testing.T) {
	root := t.TempDir()
	state := httpFileTestState(t)
	state.httpFile.root = root
	state.httpFile.port = freeWorkbenchPort(t)
	state.httpFile.allowUpload = false
	state.httpFileEntered()
	state.Click(hitHTTPFileStart)
	if !state.httpFileRunning() {
		t.Fatalf("the server did not start: %q", state.httpFile.notice)
	}
	address := state.httpFilePrimaryAddress()
	put := func() int {
		request, _ := http.NewRequest(http.MethodPut, address+"/incoming.txt", stringReader("x"))
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("PUT: %v", err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if code := put(); code != http.StatusForbidden {
		t.Fatalf("PUT with uploads off = %d", code)
	}
	// The switch is flipped on the page and reaches the listener at once.
	state.httpFile.allowUpload = true
	state.httpFileApplySwitches()
	if code := put(); code != http.StatusCreated {
		t.Fatalf("PUT after the switch = %d, want it allowed", code)
	}
	data, err := os.ReadFile(filepath.Join(root, "incoming.txt"))
	if err != nil || string(data) != "x" {
		t.Fatalf("uploaded file = %q err=%v", data, err)
	}
}

func TestHTTPFileChoicesSurviveARestart(t *testing.T) {
	options := freshOptions(t)
	first := newTestState(t, options)
	root := t.TempDir()
	first.httpFile.root = root
	first.httpFile.port = 18080
	first.httpFile.allowUpload = true
	first.httpFile.allowListing = false
	first.save()

	second := newTestState(t, options)
	if second.httpFile.root != root {
		t.Fatalf("restored root = %q, want %q", second.httpFile.root, root)
	}
	if second.httpFile.port != 18080 {
		t.Fatalf("restored port = %d", second.httpFile.port)
	}
	if !second.httpFile.allowUpload {
		t.Fatal("the upload switch did not survive")
	}
	if second.httpFile.allowListing {
		t.Fatal("the listing switch did not survive")
	}
	// The listener itself never survives: a restart starts from the choices.
	if second.httpFileRunning() {
		t.Fatal("a restarted window is serving")
	}
}

func TestHTTPFileCloseStopsTheListener(t *testing.T) {
	state := httpFileTestState(t)
	state.httpFile.port = freeWorkbenchPort(t)
	state.Click(hitHTTPFileStart)
	if !state.httpFileRunning() {
		t.Fatalf("the server did not start: %q", state.httpFile.notice)
	}
	state.closeHTTPServer()
	if state.httpFileRunning() {
		t.Fatal("closing the window left the listener up")
	}
}

// The page keeps its controls inside a narrow window: nothing hangs out of the
// workspace, where a press would land on nothing.
func TestHTTPFilePageStaysInsideANarrowWindow(t *testing.T) {
	state := httpFileTestState(t)
	state.Click(hitHTTPFileStart)
	if !state.httpFileRunning() {
		t.Fatalf("the server did not start: %q", state.httpFile.notice)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 640, Height: 500})
	for _, hit := range canvas.Hits() {
		if hit.ID.Scope() != httpFileHits {
			continue
		}
		if hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > 640 || hit.Bounds.Y+hit.Bounds.Height > 500 {
			t.Errorf("%q spans %+v, outside the 640x500 window", hit.Label, hit.Bounds)
		}
	}
}

func TestHTTPFilePortEnterStarts(t *testing.T) {
	state := httpFileTestState(t)
	state.httpFile.port = freeWorkbenchPort(t)
	state.httpFileEntered()
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	result, handled := state.httpFileKey(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitHTTPFilePort})
	if !handled || !result.Handled {
		t.Fatal("Enter in the port field was not taken")
	}
	if !state.httpFileRunning() {
		t.Fatalf("Enter did not start the server: %q", state.httpFile.notice)
	}
}

// freeWorkbenchPort asks the OS for a port nothing listens on, for a test that
// starts the real listener.
func freeWorkbenchPort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no free port: %v", err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	return port
}

// stringReader is a request body from a string.
func stringReader(value string) *strings.Reader { return strings.NewReader(value) }

package httpfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// freePort asks the OS for a port nothing listens on. The listener is closed
// again at once; the port is then used by the server under test.
func freePort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no free port: %v", err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	return port
}

// startRoot is a directory with one file and one subdirectory in it.
func startRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hello motty"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runningServer(t *testing.T, config Config) (*Server, string) {
	t.Helper()
	server, err := New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { server.Stop() })
	// The listener is up the moment Start returns, but the first request may
	// still race the goroutine; one retry keeps the tests honest about that.
	base := fmt.Sprintf("http://127.0.0.1:%d", config.Port)
	waitServing(t, base)
	return server, base
}

// waitServing waits until the listener takes a connection. A dial proves the
// server is up without touching the counters a test goes on to read.
func waitServing(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server never answered on %s: %v", base, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConfigValidate(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name   string
		config Config
		want   error
	}{
		{"root missing", Config{Port: 8080}, ErrNotDirectory},
		{"root is a file", Config{Root: filepath.Join(root, ".."), Port: 8080}, nil},
		{"port zero", Config{Root: root}, ErrBadPort},
		{"user without password", Config{Root: root, Port: 8080, User: "u"}, ErrAuthIncomplete},
		{"password without user", Config{Root: root, Port: 8080, Password: "p"}, ErrAuthIncomplete},
		{"both credentials", Config{Root: root, Port: 8080, User: "u", Password: "p"}, nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if test.want == nil {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("Validate = %v, want %v", err, test.want)
			}
		})
	}
	file := filepath.Join(root, "plain.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	if err := (Config{Root: file, Port: 8080}).Validate(); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("a file as root: %v", err)
	}
}

func TestStartStopLifecycle(t *testing.T) {
	port := freePort(t)
	server, err := New(Config{Root: startRoot(t), Port: port, AllowListing: true})
	if err != nil {
		t.Fatal(err)
	}
	if server.Running() {
		t.Fatal("a new server runs")
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	if !server.Running() {
		t.Fatal("Start left it stopped")
	}
	if err := server.Start(); !errors.Is(err, ErrRunning) {
		t.Fatalf("second Start = %v", err)
	}
	snapshot := server.Snapshot()
	if !snapshot.Running || snapshot.Port != port || len(snapshot.Addresses) == 0 || snapshot.StartedAt.IsZero() {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Addresses[0] != fmt.Sprintf("http://127.0.0.1:%d", port) {
		t.Fatalf("first address = %q", snapshot.Addresses[0])
	}
	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	if server.Running() {
		t.Fatal("Stop left it running")
	}
	if err := server.Stop(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("second Stop = %v", err)
	}
	if server.Snapshot().Running {
		t.Fatal("a stopped server reports running")
	}
}

func TestDownloadCountsBytes(t *testing.T) {
	port := freePort(t)
	server, base := runningServer(t, Config{Root: startRoot(t), Port: port})
	response, err := http.Get(base + "/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "hello motty" {
		t.Fatalf("body = %q", body)
	}
	snapshot := server.Snapshot()
	if snapshot.Downloaded != uint64(len("hello motty")) {
		t.Fatalf("downloaded = %d", snapshot.Downloaded)
	}
	if snapshot.Requests == 0 || snapshot.Active != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestNestedDownload(t *testing.T) {
	port := freePort(t)
	_, base := runningServer(t, Config{Root: startRoot(t), Port: port})
	response, err := http.Get(base + "/docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "# guide" {
		t.Fatalf("body = %q", body)
	}
}

func TestPathTraversalRefused(t *testing.T) {
	port := freePort(t)
	root := startRoot(t)
	outside := filepath.Dir(root)
	secret := filepath.Join(outside, "secret.txt")
	os.WriteFile(secret, []byte("outside"), 0o644)
	defer os.Remove(secret)
	_, base := runningServer(t, Config{Root: root, Port: port, AllowListing: true})
	for _, path := range []string{
		"/../secret.txt",
		"/..%2fsecret.txt",
		"/docs/../../secret.txt",
		"/%2e%2e/secret.txt",
	} {
		response, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode == http.StatusOK && !bytes.Contains(body, []byte("<!DOCTYPE html>")) {
			t.Fatalf("%s leaked: %d %q", path, response.StatusCode, body)
		}
		if bytes.Contains(body, []byte("outside")) {
			t.Fatalf("%s read the file outside the root", path)
		}
	}
}

func TestListing(t *testing.T) {
	port := freePort(t)
	_, base := runningServer(t, Config{Root: startRoot(t), Port: port, AllowListing: true, AllowUpload: true})
	response, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	page := string(body)
	for _, want := range []string{"readme.txt", `href="docs/"`, "<form", "multipart/form-data"} {
		if !strings.Contains(page, want) {
			t.Fatalf("listing misses %q:\n%s", want, page)
		}
	}
	// A subdirectory lists its own entries and links back up.
	response, err = http.Get(base + "/docs/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	page = string(body)
	for _, want := range []string{"guide.md", "../"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the docs listing misses %q:\n%s", want, page)
		}
	}
}

func TestListingOff(t *testing.T) {
	port := freePort(t)
	_, base := runningServer(t, Config{Root: startRoot(t), Port: port})
	response, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestUploadPut(t *testing.T) {
	port := freePort(t)
	root := startRoot(t)
	server, base := runningServer(t, Config{Root: root, Port: port, AllowUpload: true})
	request, _ := http.NewRequest(http.MethodPut, base+"/incoming/deep/report.bin", strings.NewReader("payload"))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", response.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(root, "incoming", "deep", "report.bin"))
	if err != nil {
		t.Fatalf("the upload did not land: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("report = %q", data)
	}
	if server.Snapshot().Uploaded != uint64(len("payload")) {
		t.Fatalf("uploaded = %d", server.Snapshot().Uploaded)
	}
}

func TestUploadPutOff(t *testing.T) {
	port := freePort(t)
	root := startRoot(t)
	_, base := runningServer(t, Config{Root: root, Port: port})
	request, _ := http.NewRequest(http.MethodPut, base+"/nope.txt", strings.NewReader("x"))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "nope.txt")); err == nil {
		t.Fatal("the file was written anyway")
	}
}

func TestUploadMultipart(t *testing.T) {
	port := freePort(t)
	root := startRoot(t)
	_, base := runningServer(t, Config{Root: root, Port: port, AllowUpload: true})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("from the form"))
	part2, err := writer.CreateFormFile("files", "second.txt")
	if err != nil {
		t.Fatal(err)
	}
	part2.Write([]byte("another"))
	writer.Close()
	response, err := http.Post(base+"/docs", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d", response.StatusCode)
	}
	for name, want := range map[string]string{"note.txt": "from the form", "second.txt": "another"} {
		data, err := os.ReadFile(filepath.Join(root, "docs", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q", name, data)
		}
	}
}

func TestUploadOverwrites(t *testing.T) {
	port := freePort(t)
	root := startRoot(t)
	_, base := runningServer(t, Config{Root: root, Port: port, AllowUpload: true})
	request, _ := http.NewRequest(http.MethodPut, base+"/readme.txt", strings.NewReader("replaced"))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	data, _ := os.ReadFile(filepath.Join(root, "readme.txt"))
	if string(data) != "replaced" {
		t.Fatalf("readme = %q", data)
	}
}

func TestDelete(t *testing.T) {
	port := freePort(t)
	root := startRoot(t)
	_, base := runningServer(t, Config{Root: root, Port: port, AllowDelete: true})
	request, _ := http.NewRequest(http.MethodDelete, base+"/readme.txt", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "readme.txt")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	// A folder with something in it is refused; an empty one goes.
	request, _ = http.NewRequest(http.MethodDelete, base+"/docs", nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("non-empty folder status = %d", response.StatusCode)
	}
}

func TestDeleteOff(t *testing.T) {
	port := freePort(t)
	root := startRoot(t)
	_, base := runningServer(t, Config{Root: root, Port: port})
	request, _ := http.NewRequest(http.MethodDelete, base+"/readme.txt", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "readme.txt")); err != nil {
		t.Fatal("the file was deleted anyway")
	}
}

func TestBasicAuth(t *testing.T) {
	port := freePort(t)
	_, base := runningServer(t, Config{Root: startRoot(t), Port: port, User: "motty", Password: "s3cret"})
	response, err := http.Get(base + "/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", response.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodGet, base+"/readme.txt", nil)
	request.SetBasicAuth("motty", "wrong")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d", response.StatusCode)
	}
	request, _ = http.NewRequest(http.MethodGet, base+"/readme.txt", nil)
	request.SetBasicAuth("motty", "s3cret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "hello motty" {
		t.Fatalf("signed in: %d %q", response.StatusCode, body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	port := freePort(t)
	_, base := runningServer(t, Config{Root: startRoot(t), Port: port})
	request, _ := http.NewRequest(http.MethodPatch, base+"/readme.txt", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestAddresses(t *testing.T) {
	addresses := Addresses(8080)
	if len(addresses) == 0 || addresses[0] != "http://127.0.0.1:8080" {
		t.Fatalf("addresses = %v", addresses)
	}
}

func TestHumanSize(t *testing.T) {
	for _, test := range []struct {
		in   int64
		want string
	}{
		{0, "0 B"}, {512, "512 B"}, {1024, "1 KB"}, {1536, "1.5 KB"}, {5 << 20, "5 MB"}, {3 << 30, "3 GB"},
	} {
		if got := HumanSize(test.in); got != test.want {
			t.Fatalf("humanSize(%d) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestSymlinkEscapeRefused(t *testing.T) {
	root := startRoot(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "hidden.txt"), []byte("hidden"), 0o644)
	if err := os.Symlink(filepath.Join(outside, "hidden.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	port := freePort(t)
	_, base := runningServer(t, Config{Root: root, Port: port})
	response, err := http.Get(base + "/link.txt")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("a symlink out of the root was followed")
	}
}

// TestRootReachedThroughASymlinkServes is the other side of the same rule: a
// folder the user shares by a path that goes through a symlink is served all
// the same, because the root itself is resolved before anything is compared.
func TestRootReachedThroughASymlinkServes(t *testing.T) {
	real := startRoot(t)
	link := filepath.Join(t.TempDir(), "shared")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	port := freePort(t)
	_, base := runningServer(t, Config{Root: link, Port: port, AllowListing: true})
	for _, path := range []string{"/readme.txt", "/docs/guide.md", "/"} {
		response, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s through a symlinked root = %d", path, response.StatusCode)
		}
	}
}

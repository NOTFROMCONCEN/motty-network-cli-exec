// Package httpfile serves one local directory over HTTP so other machines can
// download from it and upload to it.
//
// The server is a plain HTTP/1.1 listener on every interface of this machine:
// a GET streams a file out, a PUT or a multipart POST writes one in, and a
// DELETE removes one, each guarded by the switches in Config. A directory read
// answers with a small listing page that carries an upload form while uploads
// are allowed. Nothing here keeps state between requests beyond the counters a
// Snapshot reports, so stopping and starting again is all a restart costs.
package httpfile

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Errors a caller can act on; test them with errors.Is.
var (
	// ErrNotDirectory is a root that is missing or is not a directory.
	ErrNotDirectory = errors.New("httpfile: the shared folder is not a directory")
	// ErrBadPort is a port outside 1..65535.
	ErrBadPort = errors.New("httpfile: the port must be between 1 and 65535")
	// ErrRunning is a second Start while the server is up.
	ErrRunning = errors.New("httpfile: the server is already running")
	// ErrNotRunning is a Stop while the server is down.
	ErrNotRunning = errors.New("httpfile: the server is not running")
	// ErrAuthIncomplete is a user without a password or the other way round.
	ErrAuthIncomplete = errors.New("httpfile: a user name and a password are required together")
)

// DefaultPort is the port a fresh configuration starts on.
const DefaultPort uint16 = 8080

// Config says what is shared and what visitors may do with it.
type Config struct {
	// Root is the directory that is served. It must exist; nothing creates it.
	Root string
	// Port is the TCP port the listener takes on every interface.
	Port uint16
	// AllowUpload lets a PUT or a multipart POST write a file under Root.
	AllowUpload bool
	// AllowDelete lets a DELETE remove a file under Root.
	AllowDelete bool
	// AllowListing answers a directory read with a listing page. With it off a
	// directory read is a 403: files are still served by their exact path.
	AllowListing bool
	// User and Password guard every request with HTTP basic authentication
	// when both are set; empty means the network this machine is on is trusted.
	User     string
	Password string
}

// Validate reports the first reason the configuration cannot run.
func (config Config) Validate() error {
	if strings.TrimSpace(config.Root) == "" {
		return ErrNotDirectory
	}
	info, err := os.Stat(config.Root)
	if err != nil || !info.IsDir() {
		return ErrNotDirectory
	}
	if config.Port < 1 {
		return ErrBadPort
	}
	if (config.User == "") != (config.Password == "") {
		return ErrAuthIncomplete
	}
	return nil
}

// Snapshot is what the server looks like from outside its mutex.
type Snapshot struct {
	Running bool
	Port    uint16
	Root    string
	// Addresses is every http://host:port the server answers on, loopback
	// first, then the addresses of this machine's interfaces.
	Addresses []string
	// Requests is every request answered since the last start.
	Requests uint64
	// Uploaded and Downloaded are the bytes written to and read from disk.
	Uploaded   uint64
	Downloaded uint64
	// Active is the number of requests in flight right now.
	Active int32
	// StartedAt is when the current run began; zero while stopped.
	StartedAt time.Time
	// Err is why the last Start failed; cleared by the next one.
	Err error
}

// Server serves one directory. Its mutex protects the listener and the run
// state; the counters are atomic so handlers never take it.
type Server struct {
	mu     sync.Mutex
	config Config
	// rootReal is the root with every symlink on the way resolved: a shared
	// folder reached through one is compared against its real path, so a
	// request is never refused for the way the user named it.
	rootReal string
	listener net.Listener
	http     *http.Server
	running  bool
	started  time.Time
	err      error

	requests   atomic.Uint64
	uploaded   atomic.Uint64
	downloaded atomic.Uint64
	active     atomic.Int32
}

// New validates the configuration and returns a stopped server.
func New(config Config) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	root := filepath.Clean(config.Root)
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	return &Server{config: config, rootReal: rootReal}, nil
}

// Config is the configuration the server runs with.
func (server *Server) Config() Config {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.config
}

// SetSwitches changes what visitors may do while the server runs: every request
// reads them again, so an upload, delete or listing switch takes effect at once
// and needs no restart. The root and the port are not switches: changing either
// one means stopping and starting again.
func (server *Server) SetSwitches(allowUpload, allowDelete, allowListing bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.config.AllowUpload = allowUpload
	server.config.AllowDelete = allowDelete
	server.config.AllowListing = allowListing
}

// Start takes the port on every interface and serves until Stop. A port that
// is taken is reported as the error and leaves the server stopped.
func (server *Server) Start() error {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.running {
		return ErrRunning
	}
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(int(server.config.Port)))
	if err != nil {
		server.err = err
		return err
	}
	server.listener = listener
	server.http = &http.Server{
		Handler: http.HandlerFunc(server.serve),
		// A file server has no keep-alive worth guarding: a stalled visitor
		// must not hold a slot open forever.
		ReadHeaderTimeout: 30 * time.Second,
	}
	server.running = true
	server.started = time.Now()
	server.err = nil
	server.requests.Store(0)
	server.uploaded.Store(0)
	server.downloaded.Store(0)
	server.active.Store(0)
	go server.http.Serve(listener)
	return nil
}

// Stop closes the listener and waits for the requests in flight to finish. It
// is safe to call on a server that is not running.
func (server *Server) Stop() error {
	server.mu.Lock()
	if !server.running {
		server.mu.Unlock()
		return ErrNotRunning
	}
	httpServer := server.http
	server.running = false
	server.http, server.listener = nil, nil
	server.mu.Unlock()

	// Shutdown closes the listener itself and waits for the requests in
	// flight; it answers ErrServerClosed for the listener it closed, which is
	// the answer every successful shutdown gives.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := httpServer.Shutdown(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		// A visitor that will not let go is cut off rather than waited for
		// forever.
		httpServer.Close()
		err = nil
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Running reports whether the server is up.
func (server *Server) Running() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.running
}

// Snapshot is the server's state and counters, safe to read from any thread.
func (server *Server) Snapshot() Snapshot {
	server.mu.Lock()
	config, running, started, err := server.config, server.running, server.started, server.err
	server.mu.Unlock()
	snapshot := Snapshot{
		Running:    running,
		Port:       config.Port,
		Root:       config.Root,
		Requests:   server.requests.Load(),
		Uploaded:   server.uploaded.Load(),
		Downloaded: server.downloaded.Load(),
		Active:     server.active.Load(),
		StartedAt:  started,
		Err:        err,
	}
	if running {
		snapshot.Addresses = Addresses(config.Port)
	}
	return snapshot
}

// Addresses is every http://host:port this machine answers on right now:
// loopback first, then one per non-loopback interface address. A machine with
// no network still answers on loopback, so the list is never empty.
func Addresses(port uint16) []string {
	urls := []string{fmt.Sprintf("http://127.0.0.1:%d", port)}
	interfaces, err := net.Interfaces()
	if err != nil {
		return urls
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch value := addr.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			urls = append(urls, fmt.Sprintf("http://%s:%d", ip.String(), port))
		}
	}
	return urls
}

// resolve maps a request path to a file under the root. It refuses anything
// that climbs out of the root, including a Windows volume name or a path the
// root itself is reached through symlinks by.
func (server *Server) resolve(requestPath string) (string, error) {
	cleaned := filepath.Clean("/" + filepath.FromSlash(requestPath))
	full := filepath.Join(server.rootReal, cleaned)
	if full != server.rootReal && !strings.HasPrefix(full, server.rootReal+string(filepath.Separator)) {
		return "", errEscapes
	}
	// A symlink inside the root may point anywhere; the check above is on the
	// lexical path only. Refuse a link that leaves the root rather than follow it.
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		if resolved != server.rootReal && !strings.HasPrefix(resolved, server.rootReal+string(filepath.Separator)) {
			return "", errEscapes
		}
	}
	return full, nil
}

// errEscapes is a request whose path leaves the shared directory.
var errEscapes = errors.New("httpfile: the path leaves the shared folder")

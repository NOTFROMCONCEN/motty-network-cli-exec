package httpfile

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// serve answers one request. It counts the request, watches it in flight and
// sends anything the switches do not allow away before a file is touched.
func (server *Server) serve(w http.ResponseWriter, r *http.Request) {
	server.requests.Add(1)
	server.active.Add(1)
	defer server.active.Add(-1)

	if !server.authorized(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		server.serveRead(w, r)
	case http.MethodPut:
		server.serveUpload(w, r, r.URL.Path)
	case http.MethodPost:
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			server.serveForm(w, r)
			return
		}
		// A POST with a plain body is an upload to the path it names, the way
		// curl -X POST --data-binary @file sends one.
		server.serveUpload(w, r, r.URL.Path)
	case http.MethodDelete:
		server.serveDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// authorized answers the basic authentication challenge when a user and a
// password are configured.
func (server *Server) authorized(w http.ResponseWriter, r *http.Request) bool {
	server.mu.Lock()
	user, password := server.config.User, server.config.Password
	server.mu.Unlock()
	if user == "" && password == "" {
		return true
	}
	gotUser, gotPassword, ok := r.BasicAuth()
	if !ok || gotUser != user || gotPassword != password {
		w.Header().Set("WWW-Authenticate", `Basic realm="motty file server"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// serveRead streams a file out, or answers a directory with the listing page.
func (server *Server) serveRead(w http.ResponseWriter, r *http.Request) {
	server.mu.Lock()
	listing := server.config.AllowListing
	server.mu.Unlock()
	full, err := server.resolve(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		if !listing {
			http.Error(w, "directory listing is off", http.StatusForbidden)
			return
		}
		server.writeListing(w, r, full)
		return
	}
	file, err := os.Open(full)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer file.Close()
	server.downloaded.Add(uint64(info.Size()))
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

// serveUpload writes the request body to the file the path names. The parent
// directories are made on the way; a half-written file is removed when the
// copy fails, so a failed upload never leaves a truncated file behind.
func (server *Server) serveUpload(w http.ResponseWriter, r *http.Request, requestPath string) {
	server.mu.Lock()
	allowed := server.config.AllowUpload
	server.mu.Unlock()
	if !allowed {
		http.Error(w, "uploads are off", http.StatusForbidden)
		return
	}
	full, err := server.resolve(requestPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if info, err := os.Stat(full); err == nil && info.IsDir() {
		http.Error(w, "the path is a directory", http.StatusConflict)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, "cannot make the folder", http.StatusInternalServerError)
		return
	}
	written, err := writeFile(full, r.Body)
	if err != nil {
		http.Error(w, "cannot write the file", http.StatusInternalServerError)
		return
	}
	server.uploaded.Add(uint64(written))
	w.WriteHeader(http.StatusCreated)
}

// serveForm saves every file a multipart POST carries, under the directory the
// path names. Each part keeps its own name; a name that tries to climb out of
// the directory is refused.
func (server *Server) serveForm(w http.ResponseWriter, r *http.Request) {
	server.mu.Lock()
	allowed := server.config.AllowUpload
	server.mu.Unlock()
	if !allowed {
		http.Error(w, "uploads are off", http.StatusForbidden)
		return
	}
	directory, err := server.resolve(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		http.Error(w, "cannot make the folder", http.StatusInternalServerError)
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "not a multipart request", http.StatusBadRequest)
		return
	}
	saved := 0
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "cannot read the upload", http.StatusBadRequest)
			return
		}
		if part.FileName() == "" {
			part.Close()
			continue
		}
		name := filepath.Base(filepath.Clean("/" + filepath.FromSlash(part.FileName())))
		if name == "." || name == string(filepath.Separator) {
			part.Close()
			continue
		}
		written, err := writeFile(filepath.Join(directory, name), part)
		part.Close()
		if err != nil {
			http.Error(w, "cannot write the file", http.StatusInternalServerError)
			return
		}
		server.uploaded.Add(uint64(written))
		saved++
	}
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "%d files saved", saved)
}

// serveDelete removes the file the path names. A directory is removed only when
// it is empty.
func (server *Server) serveDelete(w http.ResponseWriter, r *http.Request) {
	server.mu.Lock()
	allowed := server.config.AllowDelete
	server.mu.Unlock()
	if !allowed {
		http.Error(w, "deletes are off", http.StatusForbidden)
		return
	}
	full, err := server.resolve(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(full)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if info.IsDir() {
		if err := os.Remove(full); err != nil {
			http.Error(w, "the folder is not empty", http.StatusConflict)
			return
		}
	} else if err := os.Remove(full); err != nil {
		http.Error(w, "cannot delete the file", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeFile copies body into path and returns how many bytes landed. A failure
// removes what was written.
func writeFile(path string, body io.Reader) (int64, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	written, err := io.Copy(file, body)
	closeErr := file.Close()
	if err != nil {
		os.Remove(path)
		return written, err
	}
	if closeErr != nil {
		os.Remove(path)
		return written, closeErr
	}
	return written, nil
}

// writeListing answers a directory read with a page of its entries. Names are
// escaped and the paths they link to are encoded, so a name with a space, a
// quote or a slash in it is a row and nothing more.
func (server *Server) writeListing(w http.ResponseWriter, r *http.Request, directory string) {
	server.mu.Lock()
	upload := server.config.AllowUpload
	server.mu.Unlock()
	entries, err := os.ReadDir(directory)
	if err != nil {
		http.Error(w, "cannot read the folder", http.StatusInternalServerError)
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	relative, err := filepath.Rel(server.rootReal, directory)
	if err != nil {
		relative = "."
	}
	title := "/"
	if relative != "." {
		title = "/" + filepath.ToSlash(relative)
	}

	var body strings.Builder
	body.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\">")
	body.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	body.WriteString("<title>motty · " + html.EscapeString(title) + "</title><style>")
	body.WriteString(`:root{color-scheme:light dark}
body{font:15px/1.5 system-ui,sans-serif;margin:0;padding:24px;background:#f6f7f9;color:#1c1d21}
main{max-width:860px;margin:0 auto}
h1{font-size:20px;margin:0 0 4px;word-break:break-all}
.path{color:#6b6f76;font-size:13px;margin:0 0 20px;word-break:break-all}
table{width:100%;border-collapse:collapse;background:#fff;border:1px solid #e3e5ea;border-radius:10px;overflow:hidden}
td,th{padding:9px 14px;text-align:left;border-bottom:1px solid #eef0f3;font-size:14px}
th{font-size:12px;text-transform:uppercase;letter-spacing:.04em;color:#6b6f76;background:#fafbfc}
tr:last-child td{border-bottom:none}
a{color:#2f6bd8;text-decoration:none}a:hover{text-decoration:underline}
.size,.time{color:#6b6f76;white-space:nowrap}
.dir{font-weight:600}
form{margin:20px 0 0;padding:16px;background:#fff;border:1px dashed #c7ccd4;border-radius:10px}
input[type=file]{font-size:14px}
button{margin-top:10px;padding:8px 18px;font-size:14px;border:none;border-radius:8px;background:#2f6bd8;color:#fff;cursor:pointer}
.empty{color:#6b6f76;padding:20px 4px}
@media (prefers-color-scheme:dark){body{background:#17181b;color:#e8e9ec}table{background:#1f2024;border-color:#2c2e33}td,th{border-bottom-color:#2a2c31}th{background:#232529;color:#9aa0a8}.path,.size,.time,.empty{color:#9aa0a8}form{background:#1f2024;border-color:#3a3d44}a{color:#7aa5f0}}
</style></head><body><main>`)
	fmt.Fprintf(&body, "<h1>%s</h1><p class=\"path\">%s</p>", html.EscapeString(title), html.EscapeString(title))
	if relative != "." {
		parent := path.Dir("/" + filepath.ToSlash(relative))
		body.WriteString(`<p><a href="` + html.EscapeString(url.PathEscape(parent)+"/") + `">../</a></p>`)
	}
	body.WriteString("<table><tr><th>Name</th><th style=\"width:110px\">Size</th><th style=\"width:180px\">Modified</th></tr>")
	if len(entries) == 0 {
		body.WriteString(`</table><p class="empty">This folder is empty.</p>`)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		href := url.PathEscape(name)
		if entry.IsDir() {
			href += "/"
		}
		size := "—"
		if !entry.IsDir() {
			size = HumanSize(info.Size())
		}
		class := ""
		label := html.EscapeString(name)
		if entry.IsDir() {
			class = ` class="dir"`
			label += "/"
		}
		fmt.Fprintf(&body, "<tr><td%s><a href=\"%s\">%s</a></td><td class=\"size\">%s</td><td class=\"time\">%s</td></tr>",
			class, href, label, size, info.ModTime().Format("2006-01-02 15:04"))
	}
	body.WriteString("</table>")
	if upload {
		body.WriteString(`<form method="post" enctype="multipart/form-data">`)
		body.WriteString(`<input type="file" name="files" multiple>`)
		body.WriteString(`<br><button type="submit">Upload</button></form>`)
	}
	body.WriteString("</main></body></html>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		io.WriteString(w, body.String())
	}
}

// HumanSize is a file's size the way a listing shows it.
func HumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatInt(size, 10) + " B"
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0") + " " + suffix
		}
	}
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', 1, 64), ".0") + " PB"
}

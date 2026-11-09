package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func TestParsingBatchHostLines(t *testing.T) {
	cases := []struct {
		line    string
		address string
		user    string
		port    uint16
		label   string
	}{
		{"192.168.1.10", "192.168.1.10", "", 22, "192.168.1.10"},
		{"admin@10.0.0.5:2222 web front", "10.0.0.5", "admin", 2222, "web front"},
		{"ssh://deploy@db.internal.example", "db.internal.example", "deploy", 22, "db.internal.example"},
		{"[::1]:2222 local", "::1", "", 2222, "local"},
		{"2001:db8::15", "2001:db8::15", "", 22, "2001:db8::15"},
		// The scheme is matched without case, the name it carries is kept as written.
		{"SSH://Admin@Example.COM", "Example.COM", "Admin", 22, "Example.COM"},
		{"host.example:8443", "host.example", "", 8443, "host.example"},
	}
	for _, want := range cases {
		host, err := parseBatchHostLine(want.line)
		if err != nil {
			t.Fatalf("%q was refused: %v", want.line, err)
		}
		if host.Address != want.address || host.User != want.user || host.Port != want.port || host.Label != want.label {
			t.Fatalf("%q parsed to %+v, want %s/%s/%d as %q", want.line, host, want.address, want.user, want.port, want.label)
		}
	}
}

func TestParsingBatchHostLineRejectsNonsense(t *testing.T) {
	for _, line := range []string{"admin@", "host:", "host:0", "host:70000", "[::1", "[example]", "a::b::c"} {
		if _, err := parseBatchHostLine(line); err == nil {
			t.Fatalf("%q was accepted", line)
		}
	}
}

func TestBatchAddingHostsFromAPastedList(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	before := len(state.hosts)

	state.Click(hitImportMenu)
	state.Click(hitBatchAddPaste)
	if state.modal != modalForm {
		t.Fatalf("the import menu did not open the paste form: modal %d", state.modal)
	}
	// One new host; the second line repeats the first within the batch, and the
	// sample catalog's router is a duplicate from the saved hosts.
	state.form.set(0, "# lab\nadmin@10.0.0.5:2222 web\nadmin@10.0.0.5:2222 web again\nadmin@192.168.1.1")
	state.Click(hitFormSubmit)
	if state.modal != modalHostImportResult || len(state.hosts) != before+1 {
		t.Fatalf("modal = %d, hosts = %d (before %d)", state.modal, len(state.hosts), before)
	}
	host := state.hosts[len(state.hosts)-1]
	if host.Label != "web" || host.Address != "10.0.0.5" || host.User != "admin" || host.Port != 2222 || host.Group != "" {
		t.Fatalf("the pasted host = %+v", host)
	}
	if !strings.Contains(state.status, "Added 1") || !strings.Contains(state.status, "2") {
		t.Fatalf("the summary said %q", state.status)
	}
	state.Click(hitHostImportClose)
	if len(state.hosts) != before+1 {
		t.Fatalf("closing the result changed the catalog: %d hosts", len(state.hosts))
	}
}

func TestABadLineKeepsThePasteFormOpen(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitImportMenu)
	state.Click(hitBatchAddPaste)
	list := "ok@10.0.0.9\nbroken@"
	state.form.set(0, list)
	state.Click(hitFormSubmit)
	if state.modal != modalForm || state.form.dialogError == "" {
		t.Fatalf("modal = %d, error = %q", state.modal, state.form.dialogError)
	}
	if !strings.Contains(state.form.dialogError, "2") {
		t.Fatalf("the error did not name the second line: %q", state.form.dialogError)
	}
	if state.form.value(0) != list {
		t.Fatalf("the pasted list was lost: %q", state.form.value(0))
	}
	// Fixing the line adds the hosts.
	state.form.set(0, "ok@10.0.0.9\nbroken@10.0.0.8")
	state.Click(hitFormSubmit)
	if state.modal != modalHostImportResult || len(state.hosts) != len(sampleHosts)+2 {
		t.Fatalf("modal = %d, hosts = %d (before %d)", state.modal, len(state.hosts), len(sampleHosts))
	}
}

func TestTheGroupMenuPrefillsTheBatchGroup(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionHosts
	state.refreshHostGroups()
	state.openHostGroupActions("Servers", ui.Point{})
	state.Click(hitGroupBatchHosts)
	if state.modal != modalForm {
		t.Fatalf("the group menu did not open the paste form: modal %d", state.modal)
	}
	if state.form.value(1) != "Servers" {
		t.Fatalf("the group was prefilled with %q", state.form.value(1))
	}
	state.form.set(0, "web@10.9.0.1")
	state.Click(hitFormSubmit)
	if state.modal != modalHostImportResult || len(state.hosts) != 4 {
		t.Fatalf("modal = %d, hosts = %d", state.modal, len(state.hosts))
	}
	if host := state.hosts[len(state.hosts)-1]; host.Group != "Servers" {
		t.Fatalf("the host went to group %q", host.Group)
	}
}

func TestImportingAnXshellDirectoryUnderAGroupPrefix(t *testing.T) {
	root := t.TempDir()
	writeXshellSession(t, filepath.Join(root, "AreaA", "Servers", "web.xsh"),
		"[CONNECTION]\nHost=web.example.test\nProtocol=SSH\nPort=2222\n\n[CONNECTION:AUTHENTICATION]\nUsername=root\n")
	writeXshellSession(t, filepath.Join(root, "AreaA", "db.xsh"), "[CONNECTION]\nHost=db.example.test\nProtocol=SSH\n")
	writeXshellSession(t, filepath.Join(root, "AreaB", "telnet.xsh"), "[CONNECTION]\nHost=telnet.example.test\nProtocol=TELNET\n")
	writeXshellSession(t, filepath.Join(root, "broken.xsh"), "not a session at all")

	_, options := newWorld(t)
	options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return root, nil }
	state := newTestState(t, options)

	state.Click(hitImportSessions)
	if state.modal != modalImportSessions {
		t.Fatalf("the chooser did not open: modal %d", state.modal)
	}
	state.Click(hitImportXshellDirectory)
	if state.modal != modalForm {
		t.Fatalf("the folder was not followed by the prefix dialog: modal %d", state.modal)
	}
	if !strings.Contains(state.form.description, "3") {
		t.Fatalf("the dialog did not count the sessions: %q", state.form.description)
	}
	if state.form.value(0) != filepath.Base(root) {
		t.Fatalf("the prefix was prefilled with %q", state.form.value(0))
	}
	state.form.set(0, "Region A")
	state.Click(hitFormSubmit)
	if state.modal != modalHostImportResult || len(state.hosts) != len(sampleHosts)+3 {
		t.Fatalf("modal = %d, hosts = %d (before %d)", state.modal, len(state.hosts), len(sampleHosts))
	}
	byLabel := map[string]Host{}
	for _, host := range state.hosts {
		byLabel[host.Label] = host
	}
	web, ok := byLabel["web"]
	if !ok || web.Group != "Region A/AreaA/Servers" || web.Address != "web.example.test" || web.User != "root" || web.Port != 2222 {
		t.Fatalf("web = %+v (found %v)", byLabel["web"], ok)
	}
	if db := byLabel["db"]; db.Group != "Region A/AreaA" {
		t.Fatalf("db = %+v", db)
	}
	if telnet := byLabel["telnet"]; telnet.Group != "Region A/AreaB" || telnet.isSSH() {
		t.Fatalf("telnet = %+v", telnet)
	}
}

func TestASessionsFolderImportsWithoutAPrefix(t *testing.T) {
	root := t.TempDir()
	sessions := filepath.Join(root, "Sessions")
	writeXshellSession(t, filepath.Join(sessions, "nas.xsh"), "[CONNECTION]\nHost=nas.example.test\nProtocol=SSH\n")

	_, options := newWorld(t)
	options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return sessions, nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportXshellDirectory)
	if state.modal != modalForm || state.form.value(0) != "" {
		t.Fatalf("modal = %d, prefix = %q", state.modal, state.form.value(0))
	}
	state.Click(hitFormSubmit)
	if state.modal != modalHostImportResult || len(state.hosts) != len(sampleHosts)+1 {
		t.Fatalf("modal = %d, hosts = %d (before %d)", state.modal, len(state.hosts), len(sampleHosts))
	}
	if host := state.hosts[len(state.hosts)-1]; host.Group != "" {
		t.Fatalf("the session went to group %q", host.Group)
	}
}

func TestAnEmptyFolderIsReportedNotImported(t *testing.T) {
	_, options := newWorld(t)
	options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return t.TempDir(), nil }
	state := newTestState(t, options)
	state.Click(hitImportSessions)
	state.Click(hitImportXshellDirectory)
	if state.modal != modalNone || len(state.hosts) != 3 {
		t.Fatalf("an empty folder opened modal %d with %d hosts", state.modal, len(state.hosts))
	}
}

// writeXshellSession writes one .xsh INI under dir, creating the folders.
func writeXshellSession(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

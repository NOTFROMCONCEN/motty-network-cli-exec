package sessionimport

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseXshellDirectoryKeepsSubfolderGroups(t *testing.T) {
	root := t.TempDir()
	writeSession := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSession(filepath.Join(root, "区域A", "服务器", "web.xsh"),
		"[CONNECTION]\nHost=web.example.test\nProtocol=SSH\nPort=2222\n\n[CONNECTION:AUTHENTICATION]\nUsername=root\n")
	writeSession(filepath.Join(root, "区域A", "db.xsh"), "[CONNECTION]\nHost=db.example.test\nProtocol=SSH\n")
	writeSession(filepath.Join(root, "区域B", "telnet.xsh"), "[CONNECTION]\nHost=telnet.example.test\nProtocol=TELNET\n")
	// A session of a kind that is not read, and a file that is no session at all.
	writeSession(filepath.Join(root, "区域B", "rdp.xsh"), "[CONNECTION]\nHost=rdp.example.test\nProtocol=RDP\n")
	writeSession(filepath.Join(root, "broken.xsh"), "not a session at all")

	result, err := ParseXshellDirectory(root)
	if err != nil {
		t.Fatalf("the folder was refused: %v", err)
	}
	if len(result.Entries) != 3 || result.Warnings == nil {
		t.Fatalf("entries = %+v, warnings = %v", result.Entries, result.Warnings)
	}
	byName := map[string]Entry{}
	for _, entry := range result.Entries {
		byName[entry.Name] = entry
	}
	web, ok := byName["web"]
	if !ok || web.Group != "区域A/服务器" || web.Host != "web.example.test" || web.User != "root" || web.Port != 2222 {
		t.Fatalf("web = %+v (found %v)", byName["web"], ok)
	}
	if db := byName["db"]; db.Group != "区域A" || db.Port != 0 {
		t.Fatalf("db = %+v", byName["db"])
	}
	if telnet := byName["telnet"]; telnet.Group != "区域B" || telnet.Protocol != Telnet {
		t.Fatalf("telnet = %+v", byName["telnet"])
	}
	if !slices.ContainsFunc(result.Warnings, func(w string) bool { return strings.Contains(w, "broken.xsh") }) {
		t.Fatalf("the broken file is not among the warnings: %v", result.Warnings)
	}
	if result.Skipped != 1 {
		t.Fatalf("the RDP session was not counted as skipped: %d", result.Skipped)
	}
}

func TestParseXshellDirectoryOnAnEmptyAndAMissingFolder(t *testing.T) {
	result, err := ParseXshellDirectory(t.TempDir())
	if err != nil || len(result.Entries) != 0 {
		t.Fatalf("an empty folder = %+v, %v", result, err)
	}
	if _, err := ParseXshellDirectory(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing folder was accepted")
	}
}

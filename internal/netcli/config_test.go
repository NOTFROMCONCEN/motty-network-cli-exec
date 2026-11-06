package netcli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"motty/internal/ai/risk"
)

const juniperYAML = `version: 1
platforms:
  - id: juniper_junos
    name: Juniper Junos
    prompt: hash
    banners: ["JUNOS", "Juniper Networks"]
    pagers: ["---(more)---"]
    pager_key: space
    errors: ["syntax error", "unknown command"]
    read_only: ["show "]
    config_read: ["show configuration"]
    facts_command: show version
    interface_command: show interfaces terse
    config_command: show configuration
    risky: ["configure", "commit", "rollback", "delete", "request"]
    critical: ["reboot", "zeroize"]
    critical_prefix: ["request system reboot", "request system zeroize"]
`

func TestParseConfigAddsAPlatform(t *testing.T) {
	tables, err := ParseConfig([]byte(juniperYAML))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("got %d tables; want 1", len(tables))
	}
	SetUserPlatforms(tables)
	t.Cleanup(ResetUserPlatforms)

	table, ok := Lookup("juniper_junos")
	if !ok {
		t.Fatal("user platform is not found")
	}
	if table.Name != "Juniper Junos" || !table.Shaped() {
		t.Fatalf("table = %+v", table)
	}
	if string(table.pagerKey) != " " {
		t.Fatalf("pager key = %q; want a space", table.pagerKey)
	}
	if table.FactsCommand() != "show version" || table.InterfaceCommand() != "show interfaces terse" || table.ConfigCommand() != "show configuration" {
		t.Fatalf("commands = %q / %q / %q", table.FactsCommand(), table.InterfaceCommand(), table.ConfigCommand())
	}
	if banner, ok := table.BannerMatch("... JUNOS 21.4R3 ..."); !ok || banner != "JUNOS" {
		t.Fatalf("BannerMatch = (%q, %v)", banner, ok)
	}
	if !slices.Contains(IDs(), "juniper_junos") {
		t.Fatalf("IDs() = %v", IDs())
	}
	if host, ok := table.MatchPrompt("edge1#"); !ok || host != "edge1" {
		t.Fatalf("MatchPrompt = (%q, %v)", host, ok)
	}

	tests := []struct {
		command string
		want    risk.Level
	}{
		{"show version", risk.Low},
		{"show configuration", risk.Medium},
		{"configure", risk.High},
		{"commit", risk.High},
		{"request support information", risk.High},
		{"request system reboot", risk.Critical},
		{"request system zeroize", risk.Critical},
		{"delete system", risk.High},
	}
	for _, tc := range tests {
		if got := Classify("juniper_junos", tc.command); got.Level != tc.want {
			t.Errorf("Classify(%q) = %v; want %v (%v)", tc.command, got.Level, tc.want, got.Reasons)
		}
	}
}

func TestParseConfigRejects(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"unknown field", "version: 1\nplatforms:\n  - id: x\n    name: X\n    colour: red\n", "field colour not found"},
		{"wrong version", "version: 2\nplatforms: []\n", "version 2 is not supported"},
		{"duplicate id", "version: 1\nplatforms:\n  - {id: x, name: X}\n  - {id: x, name: Y}\n", "appears more than once"},
		{"builtin id", "version: 1\nplatforms:\n  - {id: cisco_ios, name: X}\n", "built-in"},
		{"generic id", "version: 1\nplatforms:\n  - {id: generic, name: X}\n", "reserved"},
		{"bad prompt", "version: 1\nplatforms:\n  - {id: x, name: X, prompt: telepathy}\n", "must be angle, hash, regex or none"},
		{"regex without host", "version: 1\nplatforms:\n  - {id: x, name: X, prompt: regex, prompt_regex: '^x>$'}\n", "named capture group"},
		{"bad regex", "version: 1\nplatforms:\n  - {id: x, name: X, prompt: regex, prompt_regex: '['}\n", "prompt_regex"},
		{"multiword risky", "version: 1\nplatforms:\n  - {id: x, name: X, risky: [terminal length]}\n", "must be one word"},
		{"bad pager key", "version: 1\nplatforms:\n  - {id: x, name: X, pager_key: escape}\n", "pager_key"},
		{"multiline command", "version: 1\nplatforms:\n  - {id: x, name: X, facts_command: \"show version\\nshow clock\"}\n", "one command line"},
		{"empty name", "version: 1\nplatforms:\n  - {id: x, name: '  '}\n", "name is empty"},
		{"two documents", "version: 1\nplatforms: []\n---\nversion: 1\n", "one YAML document"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadFileMissingClearsUserTables(t *testing.T) {
	tables, err := ParseConfig([]byte(juniperYAML))
	if err != nil {
		t.Fatal(err)
	}
	SetUserPlatforms(tables)
	count, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil || count != 0 {
		t.Fatalf("LoadFile = (%d, %v); want (0, nil)", count, err)
	}
	if _, ok := Lookup("juniper_junos"); ok {
		t.Fatal("a missing file must clear the tables it previously added")
	}
}

func TestLoadFileBrokenKeepsInstalledTables(t *testing.T) {
	tables, err := ParseConfig([]byte(juniperYAML))
	if err != nil {
		t.Fatal(err)
	}
	SetUserPlatforms(tables)
	t.Cleanup(ResetUserPlatforms)
	path := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nplatforms: [{id: x, name: X, colour: red}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil {
		t.Fatal("LoadFile accepted an unknown field")
	}
	if _, ok := Lookup("juniper_junos"); !ok {
		t.Fatal("a broken file must not remove a working table")
	}
}

func TestPlatformsListsBuiltinsAndUserTables(t *testing.T) {
	tables, err := ParseConfig([]byte(juniperYAML))
	if err != nil {
		t.Fatal(err)
	}
	SetUserPlatforms(tables)
	t.Cleanup(ResetUserPlatforms)
	all := Platforms()
	if len(all) != len(platforms)+1 {
		t.Fatalf("Platforms() has %d tables; want %d", len(all), len(platforms)+1)
	}
	if all[len(all)-1].ID != "juniper_junos" {
		t.Fatalf("last table = %q; want the user table", all[len(all)-1].ID)
	}
}

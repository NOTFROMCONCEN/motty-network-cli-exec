package workbench

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/configstore"
)

// TestTheJumpModeToggleSitsInSettingsAndPersists walks the switch and the
// chooser: the mode is a row on the connection page, it is off until asked,
// the chooser lists only saved SSH hosts, the choice shows by name, and both
// survive a restart and reach the dialer.
func TestTheJumpModeToggleSitsInSettingsAndPersists(t *testing.T) {
	state := newTestState(t, testOptions(t))
	saved := savedState{Hosts: []savedHost{
		{ID: "bastion", Label: "Bastion", Address: "bastion.example.com", User: "ops", Port: 22},
		{ID: "plain", Label: "Plain", Address: "plain.example.com", User: "root", Port: 22},
		{ID: "telnetbox", Label: "TelnetBox", Address: "10.0.0.3", Port: 23,
			savedTransport: savedTransport{Protocol: "telnet"}},
	}}
	state.restoreSettings(saved)
	if state.jumpMode || state.jumpHost != "" {
		t.Fatal("the jump mode must default to off with no host chosen")
	}

	state.Click(hitSettings)
	state.selectSettingsCategory(settingsConnection)
	if !contains(paintedAt(state, 1400, 3400), state.messages.SettingsJumpMode) {
		t.Fatal("the jump mode row is missing from the connection page")
	}
	if contains(paintedAt(state, 1400, 3400), state.messages.SettingsJumpHostRow) {
		t.Fatal("the chooser row must wait for the mode to turn on")
	}
	state.Click(hitSettingsJumpHosts)
	if state.jumpPicker.open {
		t.Fatal("the chooser must stay closed while the mode is off")
	}

	// On: the chooser row appears, closed until asked.
	state.Click(hitSettingsJumpMode)
	if !state.jumpMode {
		t.Fatal("the switch did not turn the mode on")
	}
	if !contains(paintedAt(state, 1400, 3400), state.messages.SettingsJumpHostRow) {
		t.Fatal("the chooser row is missing while the mode is on")
	}
	if state.jumpHostSummary() != state.messages.SettingsJumpHostNone {
		t.Fatalf("summary %q before a choice", state.jumpHostSummary())
	}

	// The chooser lists the SSH hosts only: a Telnet host is no jump host.
	state.Click(hitSettingsJumpHosts)
	if !state.jumpPicker.open {
		t.Fatal("the chooser did not open")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitSettingsJumpHostList.At(1)); !ok {
		t.Fatal("the chooser lists fewer than the two SSH hosts")
	}
	if _, ok := hitByID(&canvas, hitSettingsJumpHostList.At(2)); ok {
		t.Fatal("the chooser offered a third host: the Telnet host must not be listed")
	}

	// Choosing a host records its catalog id and closes the chooser.
	plain, ok := state.hostByConnection("plain.example.com", "root", 22)
	if !ok {
		t.Fatal("the catalog lacks Plain")
	}
	state.Click(hitSettingsJumpHostList.At(1))
	if state.jumpHost != plain.CatalogID || state.jumpPicker.open {
		t.Fatalf("choice %q open %t: picking a host must record it and close", state.jumpHost, state.jumpPicker.open)
	}
	if state.jumpHostSummary() != plain.Label {
		t.Fatalf("summary %q, want the chosen host's name %q", state.jumpHostSummary(), plain.Label)
	}

	// The mode and its host survive a restart and reach the dialer.
	state.save()
	restored := newTestState(t, state.options)
	if !restored.jumpMode || restored.jumpHost != plain.CatalogID {
		t.Fatalf("the restored state lost the mode (%t) or the host (%q)", restored.jumpMode, restored.jumpHost)
	}
	if !restored.hostCatalog.defaultJump.on || restored.hostCatalog.defaultJump.catalogID != plain.CatalogID {
		t.Fatalf("the restored catalog is not wired: %+v", restored.hostCatalog.defaultJump)
	}
	bastion, ok := restored.hostByConnection("bastion.example.com", "ops", 22)
	if !ok {
		t.Fatal("the restored catalog lacks Bastion")
	}
	config, _, valid := restored.hostCatalog.sshConfigFor(bastion, bastion, connectEnv{})
	if !valid || len(config.ProxyJumps) != 1 || config.ProxyJumps[0].Host != "plain.example.com" {
		t.Fatalf("Bastion's unrouted connection leaves through %+v", config.ProxyJumps)
	}

	// Turning the mode off hands the dialer back to direct.
	restored.Click(hitSettings)
	restored.selectSettingsCategory(settingsConnection)
	restored.Click(hitSettingsJumpMode)
	if restored.jumpMode {
		t.Fatal("the switch did not turn the mode off")
	}
	config, _, valid = restored.hostCatalog.sshConfigFor(bastion, bastion, connectEnv{})
	if !valid || len(config.ProxyJumps) != 0 {
		t.Fatalf("with the mode off Bastion must dial direct, got %+v", config.ProxyJumps)
	}
}

// TestTheJumpModeStaysOnThisDevice keeps the choice out of the shared records.
// Where this machine sits — behind which bastion its connections leave — is
// the computer's own topology, never a setting to carry to others.
func TestTheJumpModeStaysOnThisDevice(t *testing.T) {
	if syncedSettings["jump_mode"] || syncedSettings["jump_host"] {
		t.Fatal("the jump mode is a synced setting")
	}
	path := filepath.Join(t.TempDir(), "config.db")
	want := richState()
	want.JumpMode = true
	want.JumpHost = "bastion"
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, report, err := loadState(path)
	if err != nil || report.outcome != loadOK {
		t.Fatalf("load: %+v, %v", report, err)
	}
	if !got.JumpMode || got.JumpHost != "bastion" {
		t.Fatalf("the saved mode (%t) or host (%q) did not come back", got.JumpMode, got.JumpHost)
	}
	store, err := configstore.Open(path, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, local, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if bytes.Contains(entry.Payload, []byte("jump_mode")) || bytes.Contains(entry.Payload, []byte("jump_host")) {
			t.Fatalf("the jump mode became a shared record: %s/%s", entry.Kind, entry.ID)
		}
	}
	if !bytes.Contains(local[localKey], []byte(`"jump_mode":true`)) || !bytes.Contains(local[localKey], []byte(`"jump_host":"bastion"`)) {
		t.Fatalf("the mode is not in the local state: %s", local[localKey])
	}
}

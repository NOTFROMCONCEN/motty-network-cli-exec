package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"motty/internal/netcli"
)

func TestUserPlatformFileLoadsBeforeSavedHosts(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(t.TempDir(), "state.json")
	platforms := `version: 1
platforms:
  - id: juniper_junos
    name: Juniper Junos
    prompt: hash
    banners: ["JUNOS"]
    read_only: ["show "]
    critical_prefix: ["request system reboot"]
`
	if err := os.WriteFile(netcliPlatformPath(options.StatePath), []byte(platforms), 0o600); err != nil {
		t.Fatal(err)
	}
	err := saveState(options.StatePath, savedState{Hosts: []savedHost{{
		Label: "Edge", Address: "edge.example.com", User: "me", savedTransport: savedTransport{
			ExecMode: "network_cli", CLIPlatform: "juniper_junos",
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(netcli.ResetUserPlatforms)

	state := newTestState(t, options)
	if len(state.hosts) != 1 {
		t.Fatalf("hosts = %+v; the custom platform must not drop its host", state.hosts)
	}
	if got := state.hosts[0].CLIPlatform; got != "juniper_junos" {
		t.Fatalf("platform = %q", got)
	}
	if got := netcli.Name("juniper_junos"); got != "Juniper Junos" {
		t.Fatalf("platform name = %q; the file was not loaded before the host", got)
	}
}

func TestBrokenPlatformFileKeepsTheSavedHost(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(netcliPlatformPath(options.StatePath), []byte("version: 1\nplatforms: [{id: x, name: X, colour: red}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := saveState(options.StatePath, savedState{Hosts: []savedHost{{
		Label: "Edge", Address: "edge.example.com", User: "me", savedTransport: savedTransport{
			ExecMode: "network_cli", CLIPlatform: "juniper_junos",
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(netcli.ResetUserPlatforms)

	state := newTestState(t, options)
	if len(state.hosts) != 1 || state.hosts[0].CLIPlatform != "juniper_junos" {
		t.Fatalf("hosts = %+v; a broken platform file must not drop a host", state.hosts)
	}
}

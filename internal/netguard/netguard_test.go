package netguard

import (
	"errors"
	"testing"
)

func TestCheckFollowsTheSwitch(t *testing.T) {
	SetEnabled(false)
	if err := Check("api.github.com"); err != nil {
		t.Fatalf("mode off: %v", err)
	}
	SetEnabled(true)
	defer SetEnabled(false)
	if err := Check("api.github.com"); err == nil {
		t.Fatal("mode on: a public host passed")
	}
	if err := Check("api.github.com:443"); err == nil {
		t.Fatal("mode on: a public host with a port passed")
	}
}

func TestCheckPassesLocalAndPrivateHosts(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	passing := []string{
		"",
		"localhost",
		"127.0.0.1",
		"127.8.8.8",
		"::1",
		"0.0.0.0",
		"192.168.1.10",
		"192.168.1.10:9000",
		"10.0.0.5",
		"172.16.0.9",
		"172.31.255.254",
		"169.254.10.20",
		"fd12:3456:789a::1",
		"fe80::1",
		"fe80::1%en0",
		"::ffff:192.168.0.1",
		"nas",
		"nas.local",
		"printer.lan",
		"server.internal",
		"box.home.arpa",
		"[::1]",
	}
	for _, host := range passing {
		if err := CheckAddress(host); err != nil {
			t.Errorf("CheckAddress(%q) = %v, want nil", host, err)
		}
	}
	blocking := []string{
		"api.github.com",
		"example.com",
		"8.8.8.8",
		"172.32.0.1",
		"1.1.1.1:53",
		"[::ffff:8.8.8.8]:443",
		"nas.example.com",
		// An IPv6 literal carries no dot, so a name that reads as a single
		// label would let a public address through. These are public
		// resolvers and routes, and the mode must refuse them.
		"2001:4860::1",
		"2001:4860:4860::8888",
		"2606:4700:4700::1111",
		"[2001:4860::1]",
		"[2001:4860::1]:443",
	}
	for _, host := range blocking {
		if err := CheckAddress(host); err == nil {
			t.Errorf("CheckAddress(%q) = nil, want blocked", host)
		}
	}
}

func TestCheckURLJudgesTheHost(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	if err := CheckURL("http://localhost:11434/api/show"); err != nil {
		t.Errorf("localhost URL = %v, want nil", err)
	}
	if err := CheckURL("https://192.168.1.7:9000/bucket"); err != nil {
		t.Errorf("private URL = %v, want nil", err)
	}
	if err := CheckURL("https://api.openai.com/v1"); err == nil {
		t.Error("public URL = nil, want blocked")
	}
	if err := CheckURL("not a url at all"); err == nil {
		t.Error("unparsable URL = nil, want blocked")
	}
}

func TestErrorsWrapErrBlocked(t *testing.T) {
	SetEnabled(true)
	defer SetEnabled(false)
	if err := Check("example.com"); err == nil || !errors.Is(err, ErrBlocked) {
		t.Fatalf("Check error %v does not wrap ErrBlocked", err)
	}
}

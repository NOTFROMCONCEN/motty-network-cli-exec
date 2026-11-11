package workbench

import (
	"strings"
	"testing"
	"time"

	"motty/internal/ai/llm"
)

// detectTestSession is a connected SSH host with no execution setting yet and
// a clock the test moves by hand, so a quiet window can pass without sleeping.
func detectTestSession(t *testing.T) (*State, *fakeShell, *Session, *time.Duration) {
	t.Helper()
	model := &scriptedModel{turn: func(int, llm.Request) []llm.Event { return nil }}
	state, shell := assistantTestSession(t, model)
	clock := new(time.Duration)
	state.options.Now = func() time.Duration { return *clock }
	session := &state.sessions[0]
	session.host.ExecMode = ""
	session.host.CLIPlatform = ""
	session.host.platformDetected = false
	return state, shell, session, clock
}

// feedDevice sends text, then moves the clock past the quiet window and wakes
// the window thread again so the detector can read the now-still screen.
func feedDevice(t *testing.T, state *State, shell *fakeShell, clock *time.Duration, text string) {
	t.Helper()
	shell.say(text)
	state.Wake()
	*clock += platformDetectQuiet + time.Millisecond
	state.Wake()
}

func TestDetectsHuaweiFromBannerAndPrompt(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	feedDevice(t, state, shell, clock,
		"Huawei Versatile Routing Platform Software\r\nVRP (R) software, Version 8.180\r\n\r\n<Huawei>\r\n")

	if session.host.CLIPlatform != "huawei_vrp" {
		t.Fatalf("platform = %q; want huawei_vrp", session.host.CLIPlatform)
	}
	// The detection records the platform, and only the platform: the banner
	// it matched may be no more than a config file catted through a shell, so
	// an SSH session's execution mode stays the user's choice.
	if session.host.ExecMode != "" {
		t.Fatalf("exec mode = %q; the detection must not flip it", session.host.ExecMode)
	}
	if !session.host.platformDetected {
		t.Fatal("the detection did not mark the platform as automatic")
	}
	if state.hosts[0].CLIPlatform != "huawei_vrp" || state.hosts[0].ExecMode != "" {
		t.Fatalf("saved host = %+v", state.hosts[0])
	}
	if !strings.Contains(state.status, "Huawei VRP") {
		t.Fatalf("status = %q; want the detected platform named", state.status)
	}
}

func TestDetectionLeavesAnExplicitShellAlone(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	session.host.ExecMode = "shell"
	feedDevice(t, state, shell, clock,
		"Huawei Versatile Routing Platform Software\r\n<Huawei>\r\n")
	if session.host.CLIPlatform != "" || session.host.ExecMode != "shell" {
		t.Fatalf("an explicit shell host was changed: %+v", session.host)
	}
}

func TestDetectionLeavesAnExplicitPlatformAlone(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	session.host.CLIPlatform = "cisco_ios"
	session.host.ExecMode = "network_cli"
	feedDevice(t, state, shell, clock,
		"Huawei Versatile Routing Platform Software\r\n<Huawei>\r\n")
	if session.host.CLIPlatform != "cisco_ios" {
		t.Fatalf("an explicit platform was replaced: %q", session.host.CLIPlatform)
	}
}

func TestDetectionNeedsABanner(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	feedDevice(t, state, shell, clock, "<Huawei>\r\n")
	if session.host.CLIPlatform != "" {
		t.Fatalf("a prompt shape alone was adopted: %q", session.host.CLIPlatform)
	}
}

func TestDetectionRefusesAmbiguousBanners(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	feedDevice(t, state, shell, clock,
		"Huawei Versatile Routing Platform Software\r\nH3C Comware Software\r\n<Huawei>\r\n")
	if session.host.CLIPlatform != "" {
		t.Fatalf("an ambiguous banner was adopted: %q", session.host.CLIPlatform)
	}
}

func TestDetectionUsesTheBannerToSplitASharedPromptShape(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	feedDevice(t, state, shell, clock,
		"H3C Comware Software, Version 7.1.070\r\n<H3C-SW1>\r\n")
	if session.host.CLIPlatform != "h3c_comware" {
		t.Fatalf("platform = %q; want h3c_comware", session.host.CLIPlatform)
	}
}

func TestDetectsMaipuFromItsBannerAndTheSharedHashPrompt(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	feedDevice(t, state, shell, clock,
		"MyPower (R) Operating System Software\r\n"+
			"Copyright (C) 2014 Maipu Communication Technology Co., Ltd. All Rights Reserved.\r\n"+
			"\r\nHostname#\r\n")
	if session.host.CLIPlatform != "maipu_mypower" {
		t.Fatalf("platform = %q; want maipu_mypower", session.host.CLIPlatform)
	}
	if !strings.Contains(state.status, "Maipu MyPower") {
		t.Fatalf("status = %q; want the detected platform named", state.status)
	}
}

// The new table must not claim an IOS session: both use a hash prompt, so a
// banner that is too broad would silently change what a Cisco host is.
func TestAMaipuTableDoesNotStealACiscoSession(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	feedDevice(t, state, shell, clock,
		"Cisco IOS Software, C2960X Software, Version 15.2(2)E9\r\nRouter#\r\n")
	if session.host.CLIPlatform != "cisco_ios" {
		t.Fatalf("platform = %q; want cisco_ios", session.host.CLIPlatform)
	}
}

func TestDetectionKeepsATelnetHostsAutomaticMode(t *testing.T) {
	state, shell, session, clock := detectTestSession(t)
	session.host.Protocol = protocolTelnet
	feedDevice(t, state, shell, clock,
		"Huawei Versatile Routing Platform Software\r\n<Huawei>\r\n")
	if session.host.CLIPlatform != "huawei_vrp" {
		t.Fatalf("platform = %q; want huawei_vrp", session.host.CLIPlatform)
	}
	if session.host.ExecMode != "" {
		t.Fatalf("a Telnet host was pinned to %q; automatic already means network CLI", session.host.ExecMode)
	}
	if session.sessionExecMode() != cliMode {
		t.Fatal("the detected Telnet host no longer resolves to the network CLI")
	}
}

func TestReconnectResetsTheDetection(t *testing.T) {
	state, _, session, _ := detectTestSession(t)
	session.term.platformProbe.done = true
	session.term.platformProbe.armed = true
	state.finishStartingConnection(session, true)
	if session.term.platformProbe.done || session.term.platformProbe.armed {
		t.Fatal("a reconnect kept the previous connection's detection state")
	}
}

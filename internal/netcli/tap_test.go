package netcli

import (
	"math/rand"
	"strings"
	"testing"
)

// feed emulates how a connection delivers bytes: in irregular chunks, since a
// detection result must not depend on the chunking.
func feed(tap *Tap, stream string) bool {
	return feedN(tap, []byte(stream), 1)
}

func feedN(tap *Tap, b []byte, seed int64) bool {
	rng := rand.New(rand.NewSource(seed))
	done := false
	for len(b) > 0 {
		n := min(1+rng.Intn(11), len(b))
		done = tap.Feed(b[:n]) || done
		b = b[n:]
	}
	return done
}

// complete feeds the stream and confirms the waiting prompt, the way the
// workbench does once the device has gone quiet.
func complete(tap *Tap, stream string) Result {
	feed(tap, stream)
	tap.Confirm()
	return tap.Result()
}

func newVRPTap(prompt, command string) *Tap {
	return NewTap(Table("huawei_vrp"), prompt, command, 4096)
}

func TestTapCompletesOnPrompt(t *testing.T) {
	tap := newVRPTap("<Huawei>", "display version")
	stream := "<Huawei>display version\r\n" +
		"Huawei Versatile Routing Platform Software\r\n" +
		"VRP (R) software, Version 8.180 (CE6800 V200R019C00SPC500)\r\n" +
		"<Huawei>"
	r := complete(tap, stream)
	if !r.Done {
		t.Fatalf("never completed: %+v", r)
	}
	if r.Status != StatusPrompt {
		t.Fatalf("status %+v", r)
	}
	want := "Huawei Versatile Routing Platform Software\nVRP (R) software, Version 8.180 (CE6800 V200R019C00SPC500)"
	if r.Output != want {
		t.Fatalf("output %q; want %q", r.Output, want)
	}
	if r.CLIError != "" || r.Truncated || r.Pages != 0 {
		t.Fatalf("unexpected result %+v", r)
	}
}

func TestTapCandidateNeedsQuiet(t *testing.T) {
	// A candidate prompt that is followed by more output was an output line:
	// only the quiet confirmation ends the command.
	tap := newVRPTap("<R1>", "display version")
	if done := tap.Feed([]byte("<R1>display version\r\nbanner\r\n<R1>")); done {
		t.Fatal("completed without the quiet window")
	}
	if !tap.Pending() {
		t.Fatal("no candidate was raised")
	}
	if done := tap.Feed([]byte("more banner\r\n")); done || tap.Pending() {
		t.Fatal("output did not retract the candidate")
	}
	r := complete(tap, "<R1>")
	if !r.Done || !strings.Contains(r.Output, "more banner") {
		t.Fatalf("done=%v output %q", r.Done, r.Output)
	}
}

func TestTapEchoOff(t *testing.T) {
	tap := newVRPTap("<Huawei>", "display clock")
	r := complete(tap, "2026-11-04 12:00:00\r\nWednesday\r\nTimezone : UTC\r\n<Huawei>")
	if !r.Done {
		t.Fatalf("never completed with echo off: %+v", r)
	}
	want := "2026-11-04 12:00:00\nWednesday\nTimezone : UTC"
	if r.Output != want {
		t.Fatalf("output %q", r.Output)
	}
}

func TestTapSilentCommand(t *testing.T) {
	// Echo on, and the command prints nothing: the echo line, then the prompt.
	tap := newVRPTap("<Huawei>", "return")
	r := complete(tap, "<Huawei>return\r\n<Huawei>")
	if !r.Done || r.Output != "" {
		t.Fatalf("done=%v output %q; want silent completion", r.Done, r.Output)
	}
}

func TestTapFragmentedArrival(t *testing.T) {
	stream := "<Quidway>display version\r\nline one\r\nline two\r\n<Quidway>"
	for seed := int64(1); seed < 20; seed++ {
		tap := newVRPTap("<Quidway>", "display version")
		feedN(tap, []byte(stream), seed)
		r := Result{}
		if tap.Confirm() {
			r = tap.Result()
		}
		if !r.Done || r.Status != StatusPrompt {
			t.Fatalf("seed %d: never completed: %+v", seed, tap.Result())
		}
		if out := r.Output; out != "line one\nline two" {
			t.Fatalf("seed %d: output %q", seed, out)
		}
	}
}

func TestTapANSIAndCarriageReturns(t *testing.T) {
	tap := newVRPTap("<R1>", "display health")
	stream := "\x1b[1m<R1>\x1b[0mdisplay health\r\n" +
		"loading 40%\rloading 80%\rdone    \r\n" +
		"\x1b[32mOK\x1b[0m\r\n<R1>"
	r := complete(tap, stream)
	if !r.Done {
		t.Fatal("never completed")
	}
	want := "done\nOK"
	if r.Output != want {
		t.Fatalf("output %q; want %q", r.Output, want)
	}
}

func TestTapPromptLookalikeDoesNotEnd(t *testing.T) {
	// A line that only carries the prompt inside text, and one with a
	// different host, must not end the command.
	tap := newVRPTap("<R1>", "display log")
	if r := complete(tap, "see <R1> for details\r\n<R2> free\r\nmore output\r\n"); r.Done {
		t.Fatal("ended on a lookalike line")
	}
	if r := complete(tap, "<R1>"); !r.Done {
		t.Fatal("never completed on the real prompt")
	}
}

func TestTapViewChangeEndsCommand(t *testing.T) {
	// The view changed while the command ran; the host token still matches.
	tap := newVRPTap("<R1>", "display device")
	if r := complete(tap, "<R1>display device\r\nboard ok\r\n[R1]"); !r.Done {
		t.Fatal("never completed after the view changed")
	}
	// A different host must not count.
	tap = newVRPTap("<R1>", "display device")
	if r := complete(tap, "<R1>display device\r\nboard ok\r\n[R9]"); r.Done {
		t.Fatal("ended on another host's prompt")
	}
}

func TestTapCiscoPrompt(t *testing.T) {
	tap := NewTap(Table("cisco_ios"), "Router#", "show version", 4096)
	if r := complete(tap, "Router#show version\r\nCisco IOS XE Software, Version 17.06.04a\r\nRouter#"); !r.Done {
		t.Fatal("never completed")
	}
	if out := tap.Result().Output; out != "Cisco IOS XE Software, Version 17.06.04a" {
		t.Fatalf("output %q", out)
	}
	// A configuration view of the same host counts too.
	tap = NewTap(Table("cisco_ios"), "Router#", "show run", 4096)
	if r := complete(tap, "Router#show run\r\nhostname Router\r\nRouter(config)#"); !r.Done {
		t.Fatal("config view did not complete")
	}
	// Hash-terminated output lines do not, unless the whole line is the prompt.
	tap = NewTap(Table("cisco_ios"), "Router#", "show file", 4096)
	if r := complete(tap, "Router#show file\r\nsecret 7 0822455D0A16#\r\n"); r.Done {
		t.Fatal("ended on a hash-terminated output line")
	}
}

func TestTapPagerContinuesAndCompletes(t *testing.T) {
	tap := newVRPTap("<Huawei>", "display current-configuration")
	var sent []byte
	tap.SetSend(func(p []byte) bool { sent = append(sent, p...); return true })
	page := strings.Repeat("config line\r\n", 24)
	stream := "<Huawei>display current-configuration\r\n" + page + "  ---- More ----"
	if done := tap.Feed([]byte(stream)); done {
		t.Fatal("completed while paging")
	}
	if !strings.Contains(string(sent), " ") {
		t.Fatalf("no page key sent: %q", sent)
	}
	// The device goes on with a newline that commits the pager line itself.
	r := complete(tap, "\r\n"+strings.Repeat("more line\r\n", 24)+"  ---- More ----"+"sysname Huawei\r\n"+"<Huawei>")
	if !r.Done {
		t.Fatal("never completed after paging")
	}
	if r.Pages != 2 {
		t.Fatalf("pages %d", r.Pages)
	}
	if !strings.Contains(r.Output, "config line") || !strings.Contains(r.Output, "sysname Huawei") {
		t.Fatalf("output lost pages: %q", r.Output)
	}
}

func TestTapPagerWithoutSendWaits(t *testing.T) {
	// The generic table detects the common pagers but has no key to continue
	// them: the tap must neither send anything nor complete, only report.
	tap := NewTap(Table(GenericID), "device>", "show run", 4096)
	var sent []byte
	tap.SetSend(func(p []byte) bool { sent = append(sent, p...); return true })
	if done := tap.Feed([]byte("device>show run\r\nline\r\n---- More ----")); done {
		t.Fatal("completed on a pager of an unknown platform")
	}
	if len(sent) != 0 {
		t.Fatalf("sent %q to an unknown device", sent)
	}
	r := tap.Result()
	if !r.PagerSeen {
		t.Fatal("pager not reported")
	}
	tap.Abort(StatusTimeout)
	r = tap.Result()
	if r.Status != StatusTimeout || r.Done {
		t.Fatalf("status %+v", r)
	}
}

func TestTapErrorLine(t *testing.T) {
	tap := newVRPTap("<Huawei>", "display xyz")
	r := complete(tap, "<Huawei>display xyz\r\nError: Unrecognized command found at '^' position.\r\n<Huawei>")
	if !r.Done {
		t.Fatal("never completed")
	}
	if r.CLIError == "" || !strings.Contains(r.CLIError, "Unrecognized") {
		t.Fatalf("cli error %q", r.CLIError)
	}
	if r.Status != StatusPrompt {
		t.Fatalf("an error line does not end the command: %+v", r)
	}
}

func TestTapAbortStatuses(t *testing.T) {
	for _, tc := range []struct {
		status Status
		want   string
	}{{StatusTimeout, "timeout"}, {StatusCancelled, "cancelled"}, {StatusDisconnected, "disconnected"}} {
		tap := newVRPTap("<Huawei>", "display version")
		tap.Feed([]byte("<Huawei>display version\r\nstill printing\r\n"))
		tap.Abort(tc.status)
		r := tap.Result()
		if r.Done || r.Status != tc.status || r.Status.String() != tc.want {
			t.Fatalf("abort %v: got %+v", tc.status, r)
		}
		if !strings.Contains(r.Output, "still printing") {
			t.Fatalf("partial output lost: %q", r.Output)
		}
		// Aborting twice changes nothing; feeding passes through unseen.
		tap.Abort(StatusCancelled)
		if r2 := tap.Result(); r2.Status != tc.status {
			t.Fatalf("second abort changed the status: %+v", r2)
		}
	}
	// A tap that saw the prompt keeps it.
	tap := newVRPTap("<Huawei>", "display version")
	complete(tap, "<Huawei>display version\r\nx\r\n<Huawei>")
	tap.Abort(StatusTimeout)
	if r := tap.Result(); !r.Done || r.Status != StatusPrompt {
		t.Fatalf("completed tap aborted: %+v", r)
	}
}

func TestTapBoundedOutput(t *testing.T) {
	tap := NewTap(Table("huawei_vrp"), "<Huawei>", "display log", 200)
	tap.Feed([]byte("<Huawei>display log\r\n"))
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("0123456789\r\n")
	}
	b.WriteString("<Huawei>")
	r := complete(tap, b.String())
	if !r.Done {
		t.Fatal("never completed")
	}
	if !r.Truncated || !strings.Contains(r.Output, "bytes omitted") {
		t.Fatalf("not bounded: truncated=%v %q", r.Truncated, r.Output)
	}
	if !strings.HasPrefix(r.Output, "0123456789") || !strings.HasSuffix(r.Output, "0123456789") {
		t.Fatalf("head and tail not kept: %q", r.Output)
	}
}

func TestTapGenericExactPromptOnly(t *testing.T) {
	tap := NewTap(Table(GenericID), "device>", "show clock", 4096)
	// Another shape that carries the same text inside a longer line is not the
	// exact prompt.
	if r := complete(tap, "device>show clock\r\ndevice> done\r\n"); r.Done {
		t.Fatal("ended on a lookalike")
	}
	if r := complete(tap, "device>"); !r.Done {
		t.Fatal("never completed on the exact prompt")
	}
}

func TestTapEchoOffSilentCommandTimesOut(t *testing.T) {
	// The one honest blind spot: with the echo off and a command that prints
	// nothing, a waiting prompt is indistinguishable from the echo starting to
	// arrive, so the tap waits and the caller times out instead of finishing a
	// command it cannot see end.
	tap := newVRPTap("<Huawei>", "return")
	if r := complete(tap, "<Huawei>"); r.Done {
		t.Fatal("completed on bytes that could be the echo's first half")
	}
	tap.Abort(StatusTimeout)
	if r := tap.Result(); r.Status != StatusTimeout {
		t.Fatalf("status %+v", r)
	}
}

func TestTapEchoOffWithOutputCompletes(t *testing.T) {
	// With the echo off, the first line that is not the prompt proves the echo
	// will not come: completion then works normally.
	tap := newVRPTap("<Huawei>", "display clock")
	if r := complete(tap, "12:00:00\r\n"); r.Done {
		t.Fatal("ended while the echo was still possible")
	}
	if r := complete(tap, "<Huawei>"); !r.Done {
		t.Fatal("never completed after the echo was ruled out")
	}
}

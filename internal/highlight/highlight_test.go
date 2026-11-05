package highlight

import (
	"math/rand"
	"strings"
	"testing"
)

func marked(set *Set, line string) string {
	marks := make([]Slot, len(line))
	set.Mark([]byte(line), marks)
	var out strings.Builder
	for index := range marks {
		if marks[index] == None {
			out.WriteByte('.')
		} else {
			out.WriteString(string("0RGYBMC"[marks[index]]))
		}
	}
	return out.String()
}

func TestLiteralRulesIgnoreCaseAndTreatTextAsText(t *testing.T) {
	set := Compile([]Rule{{Pattern: "a.b", Slot: Red}})
	if got := marked(set, "xA.Bx axb"); got != ".RRR....." {
		t.Fatalf("got %q", got)
	}
	exact := Compile([]Rule{{Pattern: "Abc", Slot: Green, CaseSensitive: true}})
	if got := marked(exact, "abc Abc"); got != "....GGG" {
		t.Fatalf("got %q", got)
	}
}

func TestRegexRulesAndPriority(t *testing.T) {
	set := Compile([]Rule{
		{Pattern: `\d+`, Regex: true, Slot: Cyan},
		{Pattern: `ab12`, Slot: Red},
	})
	// The earlier rule keeps the bytes both match; the later one takes the rest.
	if got := marked(set, "ab12 7"); got != "RRCC.C" {
		t.Fatalf("got %q", got)
	}
}

func TestInvalidRulesAreRefusedAndSkipped(t *testing.T) {
	for name, rule := range map[string]Rule{
		"empty":       {Slot: Red},
		"no color":    {Pattern: "x"},
		"bad regex":   {Pattern: "(", Regex: true, Slot: Red},
		"matches all": {Pattern: "x*", Regex: true, Slot: Red},
		"too long":    {Pattern: strings.Repeat("a", MaxPatternLength+1), Slot: Red},
	} {
		if Validate(rule) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	set := Compile([]Rule{{Pattern: "(", Regex: true, Slot: Red}, {Pattern: "ok", Slot: Green}})
	if got := marked(set, "ok"); got != "GG" {
		t.Fatalf("got %q", got)
	}
}

func TestANoRuleSetMarksNothing(t *testing.T) {
	var set *Set
	if set.Mark([]byte("error"), make([]Slot, 5)) || !set.Empty() {
		t.Fatal("a nil set marked something")
	}
}

// wordPack is the word-and-address pack the fast-path tests exercise, the
// rules WordRule and IPv4Rule build (the logcolor package ships their
// counterparts for real).
func wordPack() []Rule {
	return []Rule{
		WordRule(Red, "error", "errors", "err", "fail", "fails", "failed", "failure", "fatal", "panic", "denied", "refused", "critical", "exception", "traceback", "segfault", "segmentation fault"),
		WordRule(Yellow, "warn", "warning", "warnings", "deprecated", "timeout", "timed out", "retrying"),
		WordRule(Green, "ok", "success", "successful", "successfully", "passed", "done", "connected", "active (running)"),
		IPv4Rule(),
	}
}

func TestBuiltinRulesFindFailuresWarningsSuccessesAndAddresses(t *testing.T) {
	set := Compile(wordPack())
	for line, want := range map[string]string{
		"Permission denied":         "...........RRRRRR",
		"WARNING: disk almost full": "YYYYYYY..................",
		"build OK on 10.0.0.12":     "......GG....CCCCCCCCC",
		"terror and errorless":      "....................",
		"2 failed, 3 passed":        "..RRRRRR....GGGGGG",
	} {
		if got := marked(set, line); got != want {
			t.Errorf("%q: got %s want %s", line, got, want)
		}
	}
}

func BenchmarkMarkABuiltinLine(b *testing.B) {
	set := Compile(wordPack())
	line := []byte("2026-10-17T10:11:12 host sshd[123]: Failed password for root from 192.168.1.20 port 22 ssh2")
	marks := make([]Slot, len(line))
	b.ReportAllocs()
	for b.Loop() {
		clear(marks)
		set.Mark(line, marks)
	}
}

// regexOnly strips the fast matcher from rules, so the same rules compile to
// the expressions the fast path has to reproduce byte for byte.
func regexOnly(rules []Rule) []Rule {
	rules = append([]Rule(nil), rules...)
	for index := range rules {
		rules[index].fast = nil
	}
	return rules
}

func TestBuiltinFastPathMatchesTheExpressions(t *testing.T) {
	fast, expressions := Compile(wordPack()), Compile(regexOnly(wordPack()))
	lines := []string{
		"",
		"Failed password",
		"ERROR: failed, fatal, panic, denied, refused",
		"errorless terror errors failure",
		"ok ok ok done connected passed",
		"active (running) ",
		"active (running)x",
		"timed out retrying deprecated",
		"0.0.0.0 999.999.999.999 1.2.3.4",
		"1.2.3.4.5 a1.2.3.4 1.2.3.4b 1.2.3",
		"10.0.0.12:22 2026-10-17T10:11:12",
		"中文 error 10.0.0.12",
		"ＡＢＣ ok",
		"segmentation fault: WARNING",
	}
	for _, line := range lines {
		if got, want := markedBy(fast, line), markedBy(expressions, line); got != want {
			t.Errorf("%q: fast %s, expressions %s", line, got, want)
		}
	}
	// A generated corpus mixes alternatives, boundaries, punctuation, dots and
	// digits in orders a hand-written table does not think of.
	vocabulary := []string{
		"error", "errors", "err", "fail", "fails", "failed", "failure", "fatal", "panic", "denied", "refused",
		"critical", "exception", "traceback", "segfault", "segmentation fault", "warn", "warning", "warnings",
		"deprecated", "timeout", "timed out", "retrying", "ok", "success", "successful", "successfully",
		"passed", "done", "connected", "active (running)", "errorless", "terror", "token", "look",
		"10.0.0.12", "1.2.3.4", "999.1.2.3", "1.2.3", "1.2.3.4.5", "a1.2.3.4", "1.2.3.4b",
		"2026-10-17", "x", "é", "中文", "\t", ":", ".", "_",
	}
	random := rand.New(rand.NewSource(7))
	for trial := 0; trial < 2000; trial++ {
		var builder strings.Builder
		for part := 0; part < 1+random.Intn(10); part++ {
			builder.WriteString(vocabulary[random.Intn(len(vocabulary))])
			if random.Intn(2) == 1 {
				builder.WriteByte(' ')
			}
		}
		line := builder.String()
		if got, want := markedBy(fast, line), markedBy(expressions, line); got != want {
			t.Fatalf("%q: fast %s, expressions %s", line, got, want)
		}
	}
}

func markedBy(set *Set, line string) string {
	marks := make([]Slot, len(line))
	set.Mark([]byte(line), marks)
	var out strings.Builder
	for _, slot := range marks {
		if slot == None {
			out.WriteByte('.')
		} else {
			out.WriteString(string("0RGYBMC"[slot]))
		}
	}
	return out.String()
}

// A plain ASCII line must take the fast path: the expressions allocate a match
// slice, the literal matcher does not.
func TestBuiltinMarkDoesNotAllocateOnASCIILines(t *testing.T) {
	set := Compile(wordPack())
	line := []byte("2026-10-17T10:11:12 host sshd[123]: Failed password for root from 192.168.1.20 port 22")
	marks := make([]Slot, len(line))
	if allocs := testing.AllocsPerRun(100, func() {
		clear(marks)
		set.Mark(line, marks)
	}); allocs != 0 {
		t.Fatalf("marking an ASCII line allocates %v times", allocs)
	}
}

func BenchmarkMarkABuiltinPlainLine(b *testing.B) {
	set := Compile(wordPack())
	line := []byte("2026-10-17T10:11:12 host app[42]: GET /api/v1/users 200 12ms")
	marks := make([]Slot, len(line))
	b.ReportAllocs()
	for b.Loop() {
		clear(marks)
		set.Mark(line, marks)
	}
}

func TestBuiltinPatternsKeepTheirExpressions(t *testing.T) {
	want := []string{
		`\b(error|errors|err|fail|fails|failed|failure|fatal|panic|denied|refused|critical|exception|traceback|segfault|segmentation fault)\b`,
		`\b(warn|warning|warnings|deprecated|timeout|timed out|retrying)\b`,
		`\b(ok|success|successful|successfully|passed|done|connected|active \(running\))\b`,
		`\b\d{1,3}(\.\d{1,3}){3}\b`,
	}
	rules := wordPack()
	if len(rules) != len(want) {
		t.Fatalf("%d builtin rules, want %d", len(rules), len(want))
	}
	for index, rule := range rules {
		if rule.Pattern != want[index] {
			t.Errorf("rule %d pattern:\n got %s\nwant %s", index, rule.Pattern, want[index])
		}
		if err := Validate(rule); err != nil {
			t.Errorf("rule %d does not validate: %v", index, err)
		}
	}
}

func TestBuiltinFastPathKeepsUserRulePriority(t *testing.T) {
	fast := Compile(append([]Rule{{Pattern: `failed`, Slot: Blue}}, wordPack()...))
	expressions := Compile(append([]Rule{{Pattern: `failed`, Slot: Blue}}, regexOnly(wordPack())...))
	for _, line := range []string{"failed error", "10.0.0.12 failed", "failed failed failed"} {
		if got, want := markedBy(fast, line), markedBy(expressions, line); got != want {
			t.Errorf("%q: fast %s, expressions %s", line, got, want)
		}
	}
}

func TestBuiltinFastPathHandlesLongLines(t *testing.T) {
	line := strings.Repeat("plain text ", 100) + "error 10.0.0.12 " + strings.Repeat("tail ", 20)
	if len(line) <= fastLineLimit {
		t.Fatalf("test line is only %d bytes", len(line))
	}
	fast, expressions := Compile(wordPack()), Compile(regexOnly(wordPack()))
	if got, want := markedBy(fast, line), markedBy(expressions, line); got != want {
		t.Errorf("long line: fast %s, expressions %s", got, want)
	}
}

func TestBuiltinFastPathMatchesTheQuadExpression(t *testing.T) {
	fast, expressions := Compile(wordPack()), Compile(regexOnly(wordPack()))
	random := rand.New(rand.NewSource(11))
	alphabet := []byte("0123456789.a_ ")
	line := make([]byte, 64)
	for trial := 0; trial < 5000; trial++ {
		size := random.Intn(len(line))
		for index := 0; index < size; index++ {
			line[index] = alphabet[random.Intn(len(alphabet))]
		}
		text := string(line[:size])
		if got, want := markedBy(fast, text), markedBy(expressions, text); got != want {
			t.Fatalf("%q: fast %s, expressions %s", text, got, want)
		}
	}
}

func FuzzBuiltinFastPathMatchesTheExpressions(f *testing.F) {
	for _, seed := range []string{
		"", "error", "errorless", "active (running) ", "active (running)x",
		"10.0.0.12", "1.2.3.4.5", "a1.2.3.4b", "中文 error 10.0.0.12",
		"segmentation fault", "timed out", "WARNING ok passed",
	} {
		f.Add(seed)
	}
	fast, expressions := Compile(wordPack()), Compile(regexOnly(wordPack()))
	f.Fuzz(func(t *testing.T, line string) {
		if len(line) > 2000 {
			line = line[:2000]
		}
		if got, want := markedBy(fast, line), markedBy(expressions, line); got != want {
			t.Fatalf("%q: fast %s, expressions %s", line, got, want)
		}
	})
}

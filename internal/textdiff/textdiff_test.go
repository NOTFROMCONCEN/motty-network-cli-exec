package textdiff

import (
	"strings"
	"testing"
)

func TestCompareIdentical(t *testing.T) {
	result := Compare("a\nb\nc\n", "a\nb\nc\n", 3)
	if result.Unified != "" || result.Added != 0 || result.Removed != 0 {
		t.Fatalf("result = %+v", result)
	}
	if result.BeforeHash != result.AfterHash || result.BeforeHash == "" {
		t.Fatalf("hashes = %q / %q", result.BeforeHash, result.AfterHash)
	}
}

func TestCompareSimpleChange(t *testing.T) {
	result := Compare("a\nb\nc\n", "a\nB\nc\n", 1)
	if result.Added != 1 || result.Removed != 1 {
		t.Fatalf("stats = +%d -%d", result.Added, result.Removed)
	}
	for _, want := range []string{"-b", "+B", " a", " c", "@@ -1,3 +1,3 @@"} {
		if !strings.Contains(result.Unified, want) {
			t.Fatalf("unified diff missing %q:\n%s", want, result.Unified)
		}
	}
}

func TestCompareContextZero(t *testing.T) {
	result := Compare("a\nb\nc\nd\n", "a\nb\nC\nd\n", 0)
	if !strings.Contains(result.Unified, "-c") || !strings.Contains(result.Unified, "+C") {
		t.Fatalf("unified = %s", result.Unified)
	}
	if strings.Contains(result.Unified, " a") || strings.Contains(result.Unified, " d") {
		t.Fatalf("context 0 kept context lines:\n%s", result.Unified)
	}
}

func TestCompareLargeMiddleIsCoarse(t *testing.T) {
	var before, after strings.Builder
	for index := 0; index < 2500; index++ {
		before.WriteString("old line\n")
		after.WriteString("new line\n")
	}
	result := Compare(before.String(), after.String(), 3)
	if !result.Coarse {
		t.Fatal("a 2500x2500 middle was aligned line by line")
	}
	if result.Added != 2500 || result.Removed != 2500 {
		t.Fatalf("stats = +%d -%d", result.Added, result.Removed)
	}
}

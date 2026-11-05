package highlight

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseJSONRules(t *testing.T) {
	document := []byte(`[
		{"pattern": "%SNMP-3-", "color": "red"},
		{"pattern": "ALARM", "color": "Yellow", "regex": true, "case_sensitive": true},
		{"pattern": " maintenance", "color": "cyan", "caseSensitive": true},
		{"pattern": "", "color": "red"},
		{"pattern": "fine", "color": "teal"},
		{"pattern": "(", "color": "red", "regex": true},
		{"color": "red"}
	]`)
	rules, skipped, err := ParseJSONRules(document)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 4 {
		t.Fatalf("skipped %d entries, want 4", skipped)
	}
	if len(rules) != 3 || rules[0].Pattern != "%SNMP-3-" || rules[0].Slot != Red || rules[0].Regex {
		t.Fatalf("rules[0] = %+v", rules[0])
	}
	if !rules[1].Regex || !rules[1].CaseSensitive || rules[1].Slot != Yellow {
		t.Fatalf("rules[1] = %+v", rules[1])
	}
	if !rules[2].CaseSensitive || rules[2].Slot != Cyan {
		t.Fatalf("rules[2] = %+v", rules[2])
	}
}

func TestParseJSONRulesReadsARulesObjectAndDropsTheByteOrderMark(t *testing.T) {
	document := "\ufeff{\"rules\": [{\"pattern\": \"down\", \"color\": \"red\"}]}"
	rules, skipped, err := ParseJSONRules([]byte(document))
	if err != nil || skipped != 0 {
		t.Fatalf("err %v, skipped %d", err, skipped)
	}
	if len(rules) != 1 || rules[0].Pattern != "down" || rules[0].Slot != Red {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestParseJSONRulesRefusesADocumentOfAnotherShape(t *testing.T) {
	for _, document := range []string{
		`{"pattern": "x", "color": "red"}`,
		`"rules"`,
		`<xml/>`,
		``,
		`{"rules": "no"}`,
	} {
		if _, _, err := ParseJSONRules([]byte(document)); !errors.Is(err, ErrNotRules) {
			t.Errorf("%q: err %v, want ErrNotRules", document, err)
		}
	}
}

// A saved file reads back as the rules it saved, including a pattern a JSON
// writer of the HTML-escaping kind would have mangled.
func TestJSONRulesRoundTripThroughParseJSONRules(t *testing.T) {
	rules := []Rule{
		{Pattern: `%SNMP-3-`, Slot: Red},
		{Pattern: `ALARM`, Slot: Yellow, Regex: true, CaseSensitive: true},
		{Pattern: `<IfName>&\d+`, Slot: Cyan, Regex: true},
		{Pattern: "中文 down", Slot: Red},
	}
	document := JSONRules(rules)
	if bytes.Contains(document, []byte("\\u003c")) || !bytes.Contains(document, []byte("<IfName>&")) {
		t.Fatalf("the document escaped a pattern the HTML way: %s", document)
	}
	back, skipped, err := ParseJSONRules(document)
	if err != nil || skipped != 0 {
		t.Fatalf("err %v, skipped %d", err, skipped)
	}
	if len(back) != len(rules) {
		t.Fatalf("%d rules read back, want %d: %s", len(back), len(rules), document)
	}
	for index, rule := range rules {
		if back[index] != rule {
			t.Errorf("rule %d: got %+v, want %+v", index, back[index], rule)
		}
	}
}

func TestJSONRulesOfNoRulesIsAnEmptyArray(t *testing.T) {
	document := JSONRules(nil)
	back, skipped, err := ParseJSONRules(document)
	if err != nil || skipped != 0 || len(back) != 0 {
		t.Fatalf("err %v, skipped %d, rules %v: %s", err, skipped, back, document)
	}
}

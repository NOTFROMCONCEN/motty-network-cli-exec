package highlight

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// ErrNotRules says the bytes are not a coloring-rules document at all, as
// ParseJSONRules reports for a JSON file of another shape.
var ErrNotRules = errors.New("highlight: the data is not a rules document")

// jsonRule is one entry of a rules document. caseSensitive is accepted beside
// case_sensitive, because both spellings come naturally to a hand-written file.
type jsonRule struct {
	Pattern            string `json:"pattern"`
	Color              string `json:"color"`
	Regex              bool   `json:"regex"`
	CaseSensitive      bool   `json:"case_sensitive"`
	CaseSensitiveAlias bool   `json:"caseSensitive"`
}

// jsonRuleOut is the entry a saved document carries: the flags stay out while
// they are false, so a backup reads as short as the rules it holds.
type jsonRuleOut struct {
	Pattern       string `json:"pattern"`
	Color         string `json:"color"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
}

// JSONRules writes rules as the JSON document ParseJSONRules reads: an array
// of rule objects, indented the way a file a person reads is, and without the
// HTML escaping that would turn a pattern's < into \u003c. The bytes end in a
// newline, as a saved file does. A rule carries only strings, booleans and
// color names, so the encoding cannot fail on one.
func JSONRules(rules []Rule) []byte {
	entries := make([]jsonRuleOut, 0, len(rules))
	for _, rule := range rules {
		entries = append(entries, jsonRuleOut{Pattern: rule.Pattern, Color: rule.Slot.Name(), Regex: rule.Regex, CaseSensitive: rule.CaseSensitive})
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		return nil
	}
	return out.Bytes()
}

// ParseJSONRules reads a batch of rules from a JSON document: either a bare
// array of rules or an object holding one under "rules". A rule is
//
//	{"pattern": "%SNMP-3-", "color": "red", "regex": true, "case_sensitive": false}
//
// where color is a slot's name (red, yellow, green, cyan, blue, magenta) and
// the flags are optional, false by default. Entries with a pattern Validate
// refuses or an unknown color are skipped, and skipped counts them. The order
// is the document's: the first rule wins a tie of two that match the same
// text. A leading byte-order mark, which Windows editors write, is dropped.
// An error of ErrNotRules says the bytes hold no rules list at all.
func ParseJSONRules(data []byte) (rules []Rule, skipped int, err error) {
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	// Entries decode one by one: a hand-edited file with one malformed entry
	// (a "regex": "true", a number where a string belongs) costs that entry,
	// not the whole document — the same tolerance the text parser has.
	entries, err := jsonEntries(data)
	if err != nil {
		return nil, 0, ErrNotRules
	}
	for _, raw := range entries {
		var entry jsonRule
		if err := json.Unmarshal(raw, &entry); err != nil {
			skipped++
			continue
		}
		pattern := strings.TrimSpace(entry.Pattern)
		slot := SlotFromName(strings.ToLower(strings.TrimSpace(entry.Color)))
		rule := Rule{Pattern: pattern, Regex: entry.Regex, CaseSensitive: entry.CaseSensitive || entry.CaseSensitiveAlias, Slot: slot}
		if pattern == "" || slot == None || Validate(rule) != nil {
			skipped++
			continue
		}
		rules = append(rules, rule)
	}
	return rules, skipped, nil
}

// jsonEntries pulls the rule objects out of either shape the document takes:
// a bare array, or an object with a "rules" array. A document that holds
// neither is not a rules document.
func jsonEntries(data []byte) ([]json.RawMessage, error) {
	var bare []json.RawMessage
	if err := json.Unmarshal(data, &bare); err == nil {
		return bare, nil
	}
	var wrapped struct {
		Rules []json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil || wrapped.Rules == nil {
		return nil, ErrNotRules
	}
	return wrapped.Rules, nil
}

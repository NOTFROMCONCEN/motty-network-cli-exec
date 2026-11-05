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
	var bare []jsonRule
	if err := json.Unmarshal(data, &bare); err != nil {
		var wrapped struct {
			Rules []jsonRule `json:"rules"`
		}
		if err := json.Unmarshal(data, &wrapped); err != nil || wrapped.Rules == nil {
			return nil, 0, ErrNotRules
		}
		bare = wrapped.Rules
	}
	for _, entry := range bare {
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

package netcli

import (
	"strings"

	"motty/internal/ai/risk"
)

// Classify rates one network device CLI command line for the approval gate.
// It is advice for the human approval prompt, never a guarantee: a device
// command's effect is only as well known as its platform table, and the
// generic platform knows none at all.
//
// A command the tables do not recognize as a read-only check is never rated
// for automatic execution — it asks. A command that cannot be a single CLI
// line (newlines, tabs) is rated High so it never runs unattended either.
func Classify(platformID, command string) risk.Assessment {
	p := Table(platformID)
	line := strings.TrimSpace(command)
	if line == "" {
		return risk.Assessment{Level: risk.High, Reasons: []string{"the command is empty"}}
	}
	if strings.ContainsAny(line, "\n\r\t") || strings.IndexByte(line, 0) >= 0 {
		return risk.Assessment{Level: risk.High, Reasons: []string{
			"a network CLI takes one command line; send each line as its own call"}}
	}
	word := firstWord(line)
	if _, ok := longestPrefix(line, p.criticalPrefix); ok {
		return risk.Assessment{Level: risk.Critical, Reasons: []string{
			"may destroy the device, its data or the access to it"}}
	}
	for _, w := range p.critical {
		if word == w {
			return risk.Assessment{Level: risk.Critical, Reasons: []string{
				"may destroy the device, its data or the access to it"}}
		}
	}
	for _, prefix := range p.configRead {
		if hasPrefixFold(line, prefix) {
			return risk.Assessment{Level: risk.Medium, Reasons: []string{
				"reads the device configuration, which may contain credentials"}}
		}
	}
	for _, prefix := range p.readOnly {
		if hasPrefixFold(line, prefix) {
			return risk.Assessment{Level: risk.Low}
		}
	}
	if _, ok := longestPrefix(line, p.riskyPrefix); ok {
		return risk.Assessment{Level: risk.High, Reasons: []string{
			"may change the device's state or enter another mode"}}
	}
	for _, w := range p.risky {
		if word == w {
			return risk.Assessment{Level: risk.High, Reasons: []string{
				"may change the device's state or enter another mode"}}
		}
	}
	return risk.Assessment{Level: risk.High, Reasons: []string{
		"is not a recognized read-only command of this device platform"}}
}

// firstWord is the command's first word, lowercased.
func firstWord(line string) string {
	word, _, _ := strings.Cut(line, " ")
	return strings.ToLower(strings.TrimSpace(word))
}

// hasPrefixFold reports whether line starts with prefix, ignoring case.
func hasPrefixFold(line, prefix string) bool {
	return len(line) >= len(prefix) && strings.EqualFold(line[:len(prefix)], prefix)
}

// longestPrefix returns the longest case-insensitive prefix match, so a table
// can list "reload" and "reload in" without the shorter one winning.
func longestPrefix(line string, prefixes []string) (string, bool) {
	best := ""
	for _, prefix := range prefixes {
		if len(prefix) <= len(best) || !hasPrefixFold(line, prefix) {
			continue
		}
		best = prefix
	}
	return best, best != ""
}

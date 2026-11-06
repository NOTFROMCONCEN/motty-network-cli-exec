// Package cliparse turns the text a network device prints for common read
// commands into tables. It is deliberately tolerant: a row it cannot place is
// kept as a warning, never dropped silently.
package cliparse

import "strings"

// Table is one parsed command output. Columns and Rows are parallel; a cell is
// empty when the platform did not print that field.
type Table struct {
	Kind     string
	Columns  []string
	Rows     [][]string
	Warnings []string
}

// Truncate cuts s to at most n runes, for warnings and summaries.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}

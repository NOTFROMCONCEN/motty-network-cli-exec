// Package textdiff compares two texts by line and returns a bounded unified
// diff. It is pure: no terminal, no vendor, no UI. The network tools and the
// change-plan store share it.
package textdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Result is one comparison. Unified is empty when the texts are equal.
type Result struct {
	Unified    string
	Added      int
	Removed    int
	Coarse     bool // the middle differed too much to align line by line
	Truncated  bool
	BeforeHash string
	AfterHash  string
}

const (
	// maxCellWork bounds the LCS table: 4M cells is 16 MiB of int32.
	maxCellWork = 4_000_000
	// maxUnifiedBytes and maxUnifiedLines bound what goes back to a model.
	maxUnifiedBytes = 64 << 10
	maxUnifiedLines = 800
)

type opKind uint8

const (
	opEqual opKind = iota
	opRemove
	opAdd
)

type op struct {
	kind opKind
	line string
}

// Compare returns a unified diff of before and after. Common prefix and suffix
// are trimmed first, so a normal configuration change only builds a table for
// the changed middle; a middle too large to align becomes one coarse change.
func Compare(before, after string, context int) Result {
	if context < 0 {
		context = 0
	}
	result := Result{BeforeHash: hashText(before), AfterHash: hashText(after)}
	a, b := splitLines(before), splitLines(after)
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a), len(b)
	for endA > start && endB > start && a[endA-1] == b[endB-1] {
		endA--
		endB--
	}
	ops := make([]op, 0, len(a)+len(b))
	for index := 0; index < start; index++ {
		ops = append(ops, op{kind: opEqual, line: a[index]})
	}
	middleA, middleB := a[start:endA], b[start:endB]
	if len(middleA)*len(middleB) <= maxCellWork {
		ops = append(ops, lcsOps(middleA, middleB)...)
	} else {
		result.Coarse = true
		for _, line := range middleA {
			ops = append(ops, op{kind: opRemove, line: line})
		}
		for _, line := range middleB {
			ops = append(ops, op{kind: opAdd, line: line})
		}
	}
	for index := endA; index < len(a); index++ {
		ops = append(ops, op{kind: opEqual, line: a[index]})
	}
	for _, item := range ops {
		switch item.kind {
		case opAdd:
			result.Added++
		case opRemove:
			result.Removed++
		}
	}
	result.Unified = unified(ops, context, &result.Truncated)
	return result
}

func splitLines(text string) []string {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// lcsOps aligns two middles with a classic LCS table. The caller has already
// bounded the product of the lengths.
func lcsOps(a, b []string) []op {
	n, m := len(a), len(b)
	dp := make([][]int32, n+1)
	for index := range dp {
		dp[index] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	ops := make([]op, 0, n+m)
	for i, j := 0, 0; i < n || j < m; {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, op{kind: opEqual, line: a[i]})
			i, j = i+1, j+1
		case j >= m || i < n && dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, op{kind: opRemove, line: a[i]})
			i++
		default:
			ops = append(ops, op{kind: opAdd, line: b[j]})
			j++
		}
	}
	return ops
}

// unified renders change groups with context lines around them. It stops at
// the output bounds and says so rather than growing without limit.
func unified(ops []op, context int, truncated *bool) string {
	var changes []int
	for index, item := range ops {
		if item.kind != opEqual {
			changes = append(changes, index)
		}
	}
	if len(changes) == 0 {
		return ""
	}
	// Line numbers before each op, for the hunk headers.
	aAt, bAt := make([]int, len(ops)+1), make([]int, len(ops)+1)
	aLine, bLine := 1, 1
	for index, item := range ops {
		aAt[index], bAt[index] = aLine, bLine
		switch item.kind {
		case opEqual:
			aLine, bLine = aLine+1, bLine+1
		case opRemove:
			aLine++
		case opAdd:
			bLine++
		}
	}
	aAt[len(ops)], bAt[len(ops)] = aLine, bLine

	var out strings.Builder
	lines := 0
	for index := 0; index < len(changes); {
		first := max(changes[index]-context, 0)
		last := changes[index]
		next := index + 1
		for next < len(changes) && changes[next]-last <= 2*context+1 {
			last = changes[next]
			next++
		}
		end := min(last+context+1, len(ops))
		aStart, bStart := aAt[first], bAt[first]
		aCount, bCount := 0, 0
		for _, item := range ops[first:end] {
			switch item.kind {
			case opEqual:
				aCount, bCount = aCount+1, bCount+1
			case opRemove:
				aCount++
			case opAdd:
				bCount++
			}
		}
		// An empty side is addressed from line 0, as unified diff spells it.
		if aCount == 0 {
			aStart--
		}
		if bCount == 0 {
			bStart--
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		lines++
		for _, item := range ops[first:end] {
			// Budget for the line about to be written, not the bytes already
			// written: one long line must not blow past the cap.
			if out.Len()+len(item.line)+1 >= maxUnifiedBytes || lines >= maxUnifiedLines {
				*truncated = true
				return out.String() + "[diff truncated]\n"
			}
			switch item.kind {
			case opEqual:
				out.WriteByte(' ')
			case opRemove:
				out.WriteByte('-')
			case opAdd:
				out.WriteByte('+')
			}
			out.WriteString(item.line)
			out.WriteByte('\n')
			lines++
		}
		index = next
	}
	return out.String()
}

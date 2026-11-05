// Package logcolor holds the coloring rules of a network terminal: the common
// words of any CLI output, and the rules of a device's own log — the severity
// of a syslog tag, the mnemonics of authentication failures, and the state
// words of links. The highlight package marks the bytes of a line; this
// package only says which rules deserve it. Its rules come from the logs of
// real fleets: the %FACILITY-SEVERITY-MNEMONIC family (Cisco, Ruijie), the
// %%01MODULE/SEVERITY/MNEMONIC family (Huawei VRP) and the state words of
// interface tables.
package logcolor

import "motty/internal/highlight"

// RuleCount is how many rules Rules returns. Compile takes the user's rules
// first, so this is also the room the built-in pack reserves out of
// highlight.MaxRules.
const RuleCount = 13

// Rules are the built-in rules in priority order: where two match the same
// text, the earlier one colors it.
func Rules() []highlight.Rule {
	return append(common(), device()...)
}

// common are the words any CLI output colors by: failures in red, warnings in
// yellow, successes in green and addresses in cyan. They are what an operator
// scanning a log is looking for.
func common() []highlight.Rule {
	return []highlight.Rule{
		highlight.WordRule(highlight.Red, "error", "errors", "err", "fail", "fails", "failed", "failure", "fatal", "panic", "denied", "refused", "critical", "exception", "traceback", "segfault", "segmentation fault"),
		highlight.WordRule(highlight.Yellow, "warn", "warning", "warnings", "deprecated", "timeout", "timed out", "retrying"),
		highlight.WordRule(highlight.Green, "ok", "success", "successful", "successfully", "passed", "done", "connected", "active (running)"),
		highlight.IPv4Rule(),
	}
}

// device colors a network device's own log. The severity rules color the tag
// alone — %SNMP-3-AUTHFAIL turns red while the message after it keeps the
// device's plain text — and severities 5 to 7, the routine traffic of a fleet
// (authentication passed, command executed), stay uncolored so a log keeps
// readable. The state rules say what a severity cannot: a link that changed
// state to up is good news, whatever number the tag carries.
func device() []highlight.Rule {
	return []highlight.Rule{
		// %FACILITY-SEVERITY-MNEMONIC: the Cisco and Ruijie families.
		{Slot: highlight.Red, Regex: true, Pattern: `%[A-Za-z0-9_]+-[0-3]-[A-Za-z0-9_]+`},
		{Slot: highlight.Yellow, Regex: true, Pattern: `%[A-Za-z0-9_]+-4-[A-Za-z0-9_]+`},
		// %%01MODULE/SEVERITY/MNEMONIC: the Huawei VRP family.
		{Slot: highlight.Red, Regex: true, Pattern: `%%[0-9]{2}[A-Za-z0-9_]+/[0-3]/[A-Za-z0-9_]+`},
		{Slot: highlight.Yellow, Regex: true, Pattern: `%%[0-9]{2}[A-Za-z0-9_]+/4/[A-Za-z0-9_]+`},
		// A failure mnemonic (LOGIN_FAIL, USER_ONLINE_FAIL, SNMP_AUTHEN_FAILED)
		// can sit at a severity that is otherwise left alone.
		{Slot: highlight.Red, Regex: true, Pattern: `\b[A-Za-z0-9_]*FAIL[A-Za-z0-9_]*\b`},
		// The state a link changed to says more than its severity.
		{Slot: highlight.Green, Regex: true, Pattern: `changed (state )?to up`},
		{Slot: highlight.Red, Regex: true, Pattern: `changed (state )?to down`},
		// The bare states, last of all: the up and down columns of an interface
		// table, and the state words a message mentions on their own.
		highlight.WordRule(highlight.Green, "up"),
		highlight.WordRule(highlight.Red, "down"),
	}
}

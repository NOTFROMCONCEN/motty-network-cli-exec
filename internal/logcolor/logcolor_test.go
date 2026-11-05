package logcolor

import (
	"strings"
	"testing"

	"motty/internal/highlight"
)

// slotOf marks a line and reads the slot of the bytes of want, which must be
// one unbroken run of that slot.
func slotOf(t *testing.T, line, want string) highlight.Slot {
	t.Helper()
	set := highlight.Compile(Rules())
	marks := make([]highlight.Slot, len(line))
	set.Mark([]byte(line), marks)
	at := strings.Index(line, want)
	if at < 0 {
		t.Fatalf("%q is not in %q", want, line)
	}
	slot := marks[at]
	for index := at; index < at+len(want); index++ {
		if marks[index] != slot {
			t.Fatalf("%q in %q is not one run of %v", want, line, slot)
		}
	}
	return slot
}

func TestRulesMatchRuleCount(t *testing.T) {
	if len(Rules()) != RuleCount {
		t.Fatalf("Rules returns %d rules, RuleCount says %d", len(Rules()), RuleCount)
	}
}

// The lines are from real fleets: an operator's switch and a batch inspection
// of sixty-odd devices. Errors and critical faults turn red, warnings yellow.
func TestSyslogSeverityTagsColorByLevel(t *testing.T) {
	for line, tag := range map[string]string{
		"*Nov  5 14:26:56: %LINK-3-UPDOWN: Interface GigabitEthernet 0/5, changed state to up.":         "%LINK-3-UPDOWN",
		"*Oct 26 09:01:12: %SNMP-3-AUTHFAIL: SNMP request from 93.1.99.100 failed authentication.":      "%SNMP-3-AUTHFAIL",
		"Sep  4 10:34:37: %DAEMON-2-SYSTEM_MSG: A daemon reports a critical fault.":                     "%DAEMON-2-SYSTEM_MSG",
		"Jun  6 23:35:46+08:00 HRP_M<FW> %%01IFNET/2/linkDown_clear(l): The interface status changes.":  "%%01IFNET/2/linkDown_clear",
		"Jun  6 23:35:47+08:00 HRP_M<FW> %%01DEVM/3/hwLocalFaultAlarm_clear(l): The alarm has resumed.": "%%01DEVM/3/hwLocalFaultAlarm_clear",
	} {
		if slotOf(t, line, tag) != highlight.Red {
			t.Errorf("%q: %q did not turn red", line, tag)
		}
	}
	for line, tag := range map[string]string{
		"*Oct 26 09:01:12: %OPTICAL_MODULE-4-ALARM: The module of Interface GigabitEthernet 0/5 alarm.":    "%OPTICAL_MODULE-4-ALARM",
		"*Oct 26 09:01:12: %ARP-4-ARPCHANGEMAC: The MAC of 10.93.19.7 changed.":                            "%ARP-4-ARPCHANGEMAC",
		"Jun  6 23:35:46+08:00 HRP_M<FW> %%01DEVM/4/hwPortUp(l): The physical status of the port changed.": "%%01DEVM/4/hwPortUp",
	} {
		if slotOf(t, line, tag) != highlight.Yellow {
			t.Errorf("%q: %q did not turn yellow", line, tag)
		}
	}
}

// The routine traffic of a fleet — severity 5 to 7 — keeps the device's plain
// text, so a log stays readable.
func TestRoutineTagsStayClean(t *testing.T) {
	for line, tag := range map[string]string{
		"*Nov  5 14:26:33: %DP-5-PROB: Board probing has completed.":                             "%DP-5-PROB",
		"*Oct 26 08:58:27: %AAA-6-USER_AUTH_PASSED: User authenticated. Username: kylinos.":      "%AAA-6-USER_AUTH_PASSED",
		"*Oct 26 08:58:27: %CLI-5-EXEC_CMD: Executed from vty0(93.1.99.100) by check.sd command": "%CLI-5-EXEC_CMD",
		"Jun  6 23:35:46+08:00 HRP_M<FW> %%01DEVM/6/hwBoardNotify(l): A board notice.":           "%%01DEVM/6/hwBoardNotify",
	} {
		if slot := slotOf(t, line, tag); slot != highlight.None {
			t.Errorf("%q: the routine tag %q turned %v", line, tag, slot)
		}
	}
}

func TestLinkStatePhrasesSayWhatSeverityCannot(t *testing.T) {
	if slotOf(t, "*Nov  5 14:26:56: %LINEPROTO-5-UPDOWN: Line protocol on Interface GigabitEthernet 0/5, changed state to up.", "changed state to up") != highlight.Green {
		t.Error("a link that came up did not turn green")
	}
	if slotOf(t, "*Nov  5 14:27:01: %LINEPROTO-5-UPDOWN: Line protocol on Interface Tunnel 1, changed state to down.", "changed state to down") != highlight.Red {
		t.Error("a link that went down did not turn red")
	}
	if slotOf(t, "Jun  6 23:35:46+08:00 HRP_M<FW> %%01DEVM/4/hwPortUp(l): The physical status of the port changed to up. (IfName=10GE6/0/7)", "changed to up") != highlight.Green {
		t.Error("a Huawei port that came up did not turn green")
	}
}

func TestBareStatesColorAnInterfaceTable(t *testing.T) {
	row := "GigabitEthernet 0/47                     down     up             "
	if slotOf(t, row, "down") != highlight.Red {
		t.Error("a down interface in a table did not turn red")
	}
	if slotOf(t, row, "up") != highlight.Green {
		t.Error("an up interface in a table did not turn green")
	}
	for _, line := range []string{"shutdown", "backup", "update", "screen-length 0 temporary", "GroupUP"} {
		if slot := slotOf(t, line, line); slot != highlight.None {
			t.Errorf("%q turned %v: the bare states must not color inside words", line, slot)
		}
	}
}

func TestFailureMnemonicsTurnRedAtAnySeverity(t *testing.T) {
	line := "*Oct 26 09:02:44: %LOGIN-5-LOGIN_FAIL: Login failed. User: admin, source 10.93.19.7."
	if slotOf(t, line, "LOGIN_FAIL") != highlight.Red {
		t.Error("LOGIN_FAIL did not turn red")
	}
	if slotOf(t, line, "failed") != highlight.Red {
		t.Error("the failure word did not turn red")
	}
	if slotOf(t, line, "10.93.19.7") != highlight.Cyan {
		t.Error("the source address did not turn cyan")
	}
}

func TestCommandFeedbackKeepsItsWords(t *testing.T) {
	if slotOf(t, "Error: Unrecognized command found at '^' position.", "Error") != highlight.Red {
		t.Error("a command error did not turn red")
	}
	if slotOf(t, "Warning: Unauthorized access are forbidden!!!", "Warning") != highlight.Yellow {
		t.Error("a login warning did not turn yellow")
	}
}

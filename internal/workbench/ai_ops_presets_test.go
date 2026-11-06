package workbench

import (
	"strings"
	"testing"

	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
)

func TestNetworkOpsExamplesReplaceTheGenericOnes(t *testing.T) {
	state, _, session := cliTestSession(t, "network_cli", "huawei_vrp")
	examples, ok := state.aiOpsExamples(session)
	if !ok || examples[0] == "" || examples[1] == "" || examples[2] == "" {
		t.Fatalf("examples = %v, ok = %v", examples, ok)
	}
	if examples[0] == state.messages.AIPanel.Example1 {
		t.Fatal("a device session kept the generic examples")
	}
}

func TestNetworkOpsNoteOnlyForDeviceSessions(t *testing.T) {
	state, _, _ := cliTestSession(t, "network_cli", "huawei_vrp")
	note := state.aiNetworkOpsNote(gate.SessionInfo{ExecMode: gate.ExecModeNetworkCLI, Platform: "huawei_vrp"}, nil)
	for _, want := range []string{"Network operations", "one raw device command", "change plan", "Huawei VRP"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note missing %q:\n%s", want, note)
		}
	}
	if note := state.aiNetworkOpsNote(gate.SessionInfo{ExecMode: gate.ExecModeShell}, nil); note != "" {
		t.Fatalf("a shell request got a device note: %s", note)
	}
}

func TestToolRiskBadgeUsesTheDeviceTable(t *testing.T) {
	state, _, _ := cliTestSession(t, "network_cli", "huawei_vrp")
	tests := []struct {
		name string
		call llm.ToolCall
		want risk.Level
		ok   bool
	}{
		{"display", llm.ToolCall{Name: "terminal_exec", Arguments: `{"command":"display version"}`}, risk.Low, true},
		{"reboot", llm.ToolCall{Name: "terminal_exec", Arguments: `{"command":"reboot"}`}, risk.Critical, true},
		{"unknown", llm.ToolCall{Name: "terminal_exec", Arguments: `{"command":"system-view"}`}, risk.High, true},
		{"facts", llm.ToolCall{Name: "device_facts", Arguments: `{}`}, risk.Low, true},
		{"batch", llm.ToolCall{Name: "group_run", Arguments: `{"targets":[]}`}, risk.Medium, true},
		{"diff", llm.ToolCall{Name: "config_diff", Arguments: `{}`}, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			level, ok := state.aiToolRisk(tc.call)
			if ok != tc.ok || ok && level != tc.want {
				t.Fatalf("got (%v, %v); want (%v, %v)", level, ok, tc.want, tc.ok)
			}
		})
	}
}

package workbench

import (
	"encoding/json"

	"motty/internal/ai/gate"
	"motty/internal/ai/llm"
	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
	"motty/internal/netcli"
)

// aiOpsExamples replaces the generic empty-panel examples when the locked
// target is a device CLI: the questions people actually ask a switch.
func (state *State) aiOpsExamples(session *Session) ([aiExamples]string, bool) {
	if session == nil || session.sessionExecMode() != cliMode {
		return [aiExamples]string{}, false
	}
	m := state.messages.AIPanel
	return [aiExamples]string{m.OpsExample1, m.OpsExample2, m.OpsExample3}, true
}

// aiNetworkOpsNote is the system-prompt addendum for a request that includes a
// device CLI. It is per request, so it never edits the shared system constant.
func (state *State) aiNetworkOpsNote(target gate.SessionInfo, references []gate.SessionInfo) string {
	cli := target.ExecMode == gate.ExecModeNetworkCLI
	for _, reference := range references {
		if reference.ExecMode == gate.ExecModeNetworkCLI {
			cli = true
			break
		}
	}
	if !cli {
		return ""
	}
	note := "\n\nNetwork operations: a device CLI is in scope. Send exactly one raw device command per terminal_exec call, never wrapped in shell syntax; a device reports completion by showing its prompt again, not an exit status. Prefer device_facts, interface_status and group_run for reads. For a multi-command change, propose a change plan and let the user approve it; nothing runs before that approval. A command the platform table does not recognize as read-only is never run unattended."
	if target.ExecMode == gate.ExecModeNetworkCLI && target.Platform != "" {
		note += " The primary device's platform is " + netcli.Name(target.Platform) + "."
	}
	return note
}

// aiToolRisk is the badge on a tool card. A device command is rated by its
// platform table, not by the shell parser, so the badge agrees with the gate.
func (state *State) aiToolRisk(call llm.ToolCall) (risk.Level, bool) {
	switch call.Name {
	case tools.NameDeviceFacts, tools.NameInterfaceStatus:
		return risk.Low, true
	case tools.NameGroupRun:
		return risk.Medium, true
	case tools.NameConfigDiff:
		return 0, false
	case tools.NameTerminalExec:
		var args struct {
			Session string `json:"session"`
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(call.Arguments), &args) != nil || args.Command == "" {
			return 0, false
		}
		return state.aiCommandRisk(args.Session, args.Command), true
	case tools.NameTerminalExecMany:
		var args struct {
			Sessions []string `json:"sessions"`
			Command  string   `json:"command"`
		}
		if json.Unmarshal([]byte(call.Arguments), &args) != nil || args.Command == "" {
			return 0, false
		}
		handle := ""
		if len(args.Sessions) > 0 {
			handle = args.Sessions[0]
		}
		return state.aiCommandRisk(handle, args.Command), true
	}
	return 0, false
}

func (state *State) aiCommandRisk(handle, command string) risk.Level {
	if session, err := state.aiSession(handle); err == nil && session.sessionExecMode() == cliMode {
		return netcli.Classify(session.cliPlatformName(), command).Level
	}
	return risk.Classify(command).Level
}

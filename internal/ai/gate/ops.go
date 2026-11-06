package gate

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"motty/internal/ai/risk"
	"motty/internal/ai/tools"
	"motty/internal/cliparse"
	"motty/internal/netcli"
	"motty/internal/textdiff"
)

// The network operations tools. They sit beside the terminal tools rather than
// inside them: each names one read path a platform table knows, so the model
// asks for "facts" instead of guessing a vendor's version command.

// runOps dispatches the tools added by the network-operations workbench. It
// runs before the upstream switch, so a rebase keeps this file and only the
// call site in gate.go changes.
func (g Gate) runOps(ctx context.Context, name string, raw []byte) (Result, bool) {
	switch name {
	case tools.NameDeviceFacts:
		return g.deviceFacts(ctx, raw), true
	case tools.NameInterfaceStatus:
		return g.interfaceStatus(ctx, raw), true
	case tools.NameConfigDiff:
		return g.configDiff(raw), true
	case tools.NameGroupRun:
		return g.groupRun(ctx, raw), true
	}
	return Result{}, false
}

func (g Gate) deviceFacts(ctx context.Context, raw []byte) Result {
	args, err := tools.DecodeDeviceFacts(raw)
	if err != nil {
		return failure(err)
	}
	if args.Session, err = g.targetSession(args.Session); err != nil {
		return failure(err)
	}
	mode, platform := g.sessionExec(ctx, args.Session)
	if mode != ExecModeNetworkCLI {
		return Result{Text: "device_facts needs a network CLI session; this session runs a shell. Set the host's command execution to Network CLI first.", IsError: true}
	}
	table := netcli.Table(platform)
	command := table.FactsCommand()
	if command == "" {
		return Result{Text: "this device platform has no version command in its table; name the platform in the host's settings, or add facts_command to its platform file", IsError: true}
	}
	if denied := g.ask(ctx, tools.NameDeviceFacts, tools.Exec, args.Session, command, netcli.Classify(platform, command)); denied != nil {
		return *denied
	}
	result, err := g.Backend.Exec(ctx, tools.TerminalExecArgs{Session: args.Session, Command: command, TimeoutSeconds: args.TimeoutSeconds})
	if err != nil {
		return failure(err)
	}
	facts := netcli.ParseFacts(platform, result.Output)
	var text strings.Builder
	fmt.Fprintf(&text, "platform: %s (%s)\n", platform, netcli.Name(platform))
	if facts.Version != "" {
		fmt.Fprintf(&text, "version: %s\n", facts.Version)
	}
	if facts.Model != "" {
		fmt.Fprintf(&text, "model: %s\n", facts.Model)
	}
	if facts.Uptime != "" {
		fmt.Fprintf(&text, "uptime: %s\n", facts.Uptime)
	}
	if facts.Version == "" && facts.Model == "" && facts.Uptime == "" {
		text.WriteString("no structured fields matched; raw output follows:\n")
		text.WriteString(boundedLines(result.Output, 40))
	}
	appendExecEnding(&text, result)
	return Result{Text: strings.TrimRight(text.String(), "\n"), IsError: result.TimedOut || result.CLIError != ""}
}

func (g Gate) interfaceStatus(ctx context.Context, raw []byte) Result {
	args, err := tools.DecodeInterfaceStatus(raw)
	if err != nil {
		return failure(err)
	}
	if args.Session, err = g.targetSession(args.Session); err != nil {
		return failure(err)
	}
	mode, platform := g.sessionExec(ctx, args.Session)
	if mode != ExecModeNetworkCLI {
		return Result{Text: "interface_status needs a network CLI session; this session runs a shell. Set the host's command execution to Network CLI first.", IsError: true}
	}
	table := netcli.Table(platform)
	command := table.InterfaceCommand()
	if command == "" {
		return Result{Text: "this device platform has no interface command in its table; name the platform in the host's settings, or add interface_command to its platform file", IsError: true}
	}
	if denied := g.ask(ctx, tools.NameInterfaceStatus, tools.Exec, args.Session, command, netcli.Classify(platform, command)); denied != nil {
		return *denied
	}
	result, err := g.Backend.Exec(ctx, tools.TerminalExecArgs{Session: args.Session, Command: command, TimeoutSeconds: args.TimeoutSeconds})
	if err != nil {
		return failure(err)
	}
	parsed := cliparse.ParseInterfaces(result.Output)
	if len(parsed.Rows) == 0 {
		var text strings.Builder
		text.WriteString("no interface table could be parsed")
		if len(parsed.Warnings) > 0 {
			text.WriteString(": " + strings.Join(parsed.Warnings, "; "))
		}
		text.WriteString("\nraw output:\n")
		text.WriteString(boundedLines(result.Output, 40))
		appendExecEnding(&text, result)
		return Result{Text: strings.TrimRight(text.String(), "\n"), IsError: true}
	}
	stateColumn := interfaceStateColumn(parsed.Columns)
	filter := strings.ToLower(strings.TrimSpace(args.Interface))
	rows := make([][]string, 0, len(parsed.Rows))
	counts := map[string]int{}
	matched := 0
	for _, row := range parsed.Rows {
		if filter != "" && !strings.Contains(strings.ToLower(row[0]), filter) {
			continue
		}
		matched++
		state := ""
		if stateColumn >= 0 && stateColumn < len(row) {
			state = strings.ToLower(strings.TrimSpace(row[stateColumn]))
		}
		switch {
		case strings.Contains(state, "administratively"):
			counts["admin-down"]++
		case state == "up":
			counts["up"]++
		case state == "":
			counts["unknown"]++
		default:
			counts["down"]++
		}
		if !args.IncludeDown && state != "up" {
			continue
		}
		rows = append(rows, row)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "platform: %s (%s)\n", platform, netcli.Name(platform))
	fmt.Fprintf(&text, "interfaces: %d (up %d, down %d, administratively down %d", matched, counts["up"], counts["down"], counts["admin-down"])
	if counts["unknown"] > 0 {
		fmt.Fprintf(&text, ", state unknown %d", counts["unknown"])
	}
	text.WriteString(")\n")
	if args.Interface != "" {
		fmt.Fprintf(&text, "filter: %s\n", args.Interface)
	}
	if len(rows) == 0 {
		text.WriteString("no interface matched\n")
	} else {
		text.WriteString(strings.Join(parsed.Columns, " | "))
		text.WriteByte('\n')
		limit := min(len(rows), 200)
		for _, row := range rows[:limit] {
			text.WriteString(strings.Join(row, " | "))
			text.WriteByte('\n')
		}
		if len(rows) > limit {
			fmt.Fprintf(&text, "… %d more rows\n", len(rows)-limit)
		}
	}
	if len(parsed.Warnings) > 0 {
		text.WriteString("unparsed lines: " + strings.Join(parsed.Warnings, "; ") + "\n")
	}
	appendExecEnding(&text, result)
	return Result{Text: strings.TrimRight(text.String(), "\n"), IsError: result.TimedOut || result.CLIError != ""}
}

func (g Gate) configDiff(raw []byte) Result {
	args, err := tools.DecodeConfigDiff(raw)
	if err != nil {
		return failure(err)
	}
	result := textdiff.Compare(args.Before, args.After, args.ContextLines)
	var text strings.Builder
	fmt.Fprintf(&text, "added: %d\nremoved: %d\n", result.Added, result.Removed)
	fmt.Fprintf(&text, "before_sha256: %s\nafter_sha256: %s\n", result.BeforeHash, result.AfterHash)
	if result.Coarse {
		text.WriteString("note: the middle differs too much to align line by line; all differing lines are one block\n")
	}
	if result.Truncated {
		text.WriteString("note: the diff is shorter than the change; raise context or compare a narrower range\n")
	}
	if result.Unified == "" {
		text.WriteString("the texts are identical\n")
	} else {
		text.WriteString("--- before\n+++ after\n")
		text.WriteString(result.Unified)
	}
	return Result{Text: strings.TrimRight(text.String(), "\n")}
}

func (g Gate) groupRun(ctx context.Context, raw []byte) Result {
	args, err := tools.DecodeGroupRun(raw)
	if err != nil {
		return failure(err)
	}
	sessions, err := g.Backend.Sessions(ctx)
	if err != nil {
		return failure(err)
	}
	type groupTarget struct {
		id, label, platform string
		commands            []string
	}
	targets := make([]groupTarget, 0, len(args.Targets))
	var problems []string
	for _, requested := range args.Targets {
		if _, err := g.targetSession(requested.Session); err != nil {
			return failure(err)
		}
		info, found := sessionInfoByID(sessions, requested.Session)
		if !found {
			problems = append(problems, requested.Session+" is not an open session")
			continue
		}
		if !info.Ready {
			problems = append(problems, requested.Session+" ("+info.Title+") has no shell that can take commands")
			continue
		}
		if info.ExecMode != ExecModeNetworkCLI {
			problems = append(problems, requested.Session+" ("+info.Title+") is a shell session; group_run only runs network CLI commands")
			continue
		}
		// Refuse the whole batch before asking: a model must not smuggle a
		// state-changing command into a read-only fleet call.
		for _, command := range requested.Commands {
			if assessment := netcli.Classify(info.Platform, command); assessment.Level > risk.Low {
				return Result{Text: fmt.Sprintf("nothing was run: %q on %s is not a recognized read-only command (%s). Use a change plan for changes.", command, info.Title, strings.Join(assessment.Reasons, "; ")), IsError: true}
			}
		}
		targets = append(targets, groupTarget{id: info.ID, label: info.Title, platform: info.Platform, commands: requested.Commands})
	}
	if len(problems) > 0 {
		return Result{Text: "nothing was run: " + strings.Join(problems, "; "), IsError: true}
	}
	if len(targets) == 0 {
		return Result{Text: "nothing was run: no target was usable", IsError: true}
	}
	label := strings.TrimSpace(args.Label)
	summary := "read-only batch"
	if label != "" {
		summary = label
	}
	parts := make([]string, 0, len(targets))
	for _, target := range targets {
		parts = append(parts, fmt.Sprintf("%s (%s): %s", target.label, target.id, strings.Join(target.commands, "; ")))
	}
	summary += " — " + strings.Join(parts, " | ")
	// A batch always asks: even read-only, it is a decision across machines.
	assessment := risk.Assessment{Level: risk.Medium, Reasons: []string{fmt.Sprintf("runs on %d sessions", len(targets))}}
	others := make([]string, 0, len(targets)-1)
	for _, target := range targets[1:] {
		others = append(others, target.id)
	}
	if denied := g.ask(ctx, tools.NameGroupRun, tools.Exec, targets[0].id, summary, assessment, others...); denied != nil {
		return *denied
	}

	results := make(map[int][]ExecResult, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	limit := make(chan struct{}, 4)
	for index, target := range targets {
		wg.Add(1)
		go func(index int, target groupTarget) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			perCommand := make([]ExecResult, 0, len(target.commands))
			for _, command := range target.commands {
				result, err := g.Backend.Exec(ctx, tools.TerminalExecArgs{Session: target.id, Command: command, TimeoutSeconds: args.TimeoutSeconds})
				if err != nil {
					result = ExecResult{Output: err.Error(), ExitKnown: false, Status: "error"}
				}
				perCommand = append(perCommand, result)
			}
			mu.Lock()
			results[index] = perCommand
			mu.Unlock()
		}(index, target)
	}
	wg.Wait()

	var text strings.Builder
	failed, total := 0, 0
	for index, target := range targets {
		fmt.Fprintf(&text, "== %s (%s) ==\n", target.label, target.id)
		for commandIndex, command := range target.commands {
			result := results[index][commandIndex]
			fmt.Fprintf(&text, "$ %s\n%s\n", command, formatExec(result))
			total++
			if result.TimedOut || result.CLIError != "" || result.Status == "error" {
				failed++
			}
		}
	}
	return Result{Text: strings.TrimRight(text.String(), "\n"), IsError: total > 0 && failed == total}
}

func sessionInfoByID(sessions []SessionInfo, id string) (SessionInfo, bool) {
	for _, session := range sessions {
		if session.ID == id {
			return session, true
		}
	}
	return SessionInfo{}, false
}

func interfaceStateColumn(columns []string) int {
	for index, column := range columns {
		switch column {
		case "PHY", "Status":
			return index
		}
	}
	return -1
}

func boundedLines(text string, maxLines int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "…")
	}
	return strings.Join(lines, "\n")
}

// appendExecEnding adds the one line formatExec would add for a device command:
// how the wait ended, without repeating the output.
func appendExecEnding(text *strings.Builder, result ExecResult) {
	switch {
	case result.TimedOut && !result.ExitKnown:
		text.WriteString("[timed out; the device never showed its prompt again]\n")
	case result.CLIError != "":
		fmt.Fprintf(text, "[device CLI error: %s]\n", result.CLIError)
	case !result.ExitKnown && result.Status == "prompt_restored":
		text.WriteString("[device prompt restored; the command finished]\n")
	}
}

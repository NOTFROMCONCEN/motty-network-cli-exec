package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vivaui/viva/text"
	"motty/internal/sshconfig"
)

// Adding many hosts at once by pasting a list: one host per line, the way a
// server inventory is written down. The lines are read by the shared form
// dialog, and what they hold goes through the same import plan as the session
// importers, so duplicates are skipped and a failed save rolls everything back.

// openBatchHostForm asks for the pasted list. group prefills the group field,
// which the group context menu sets to the group the menu belongs to.
func (state *State) openBatchHostForm(group string) {
	m := state.messages
	edited := state.openForm(formDialog{
		title:       m.BatchAddTitle,
		description: m.BatchAddDescription,
		submit:      m.BatchAddSubmit,
		run:         (*State).submitBatchHosts,
	}, formField{label: m.BatchAddListField, placeholder: m.BatchAddPlaceholder, multiline: true},
		formField{label: m.FieldGroup})
	edited.fields[1].editor = text.Editor{}
	if group != "" {
		edited.fields[1].editor.Insert(group)
	}
}

// submitBatchHosts turns the pasted lines into hosts. An unreadable line keeps
// the form open with everything the user typed, naming the line that failed.
func (state *State) submitBatchHosts(form *formDialog) string {
	group := normalizeHostGroupPath(strings.TrimSpace(form.value(1)))
	list := strings.ReplaceAll(form.value(0), "\r\n", "\n")
	lines := strings.Split(list, "\n")
	var hosts []Host
	for index, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		host, err := parseBatchHostLine(line)
		if err != nil {
			return fmt.Sprintf(state.messages.BatchAddLineErrorFmt, index+1, strconv.Quote(line))
		}
		host.Group = group
		hosts = append(hosts, host)
	}
	if len(hosts) == 0 {
		return state.messages.BatchAddEmpty
	}
	builder := newHostImportBuilder(state, sshconfig.Result{})
	for _, host := range hosts {
		key := connectionKey(host.Address, host.User, host.Port)
		if _, exists := builder.connections[key]; exists {
			builder.plan.duplicateHosts++
			continue
		}
		host.ID = builder.plan.nextHostID
		host.CatalogID = freshCatalogID(builder.usedIDs)
		host.finish()
		builder.plan.nextHostID++
		builder.plan.hosts = append(builder.plan.hosts, host)
		builder.connections[key] = host
	}
	if err := state.stageSessionImport(builder.plan); err != nil {
		return fmt.Sprintf(state.messages.Tools.ImportHostSaveFailedFmt, err)
	}
	state.completeSessionImport(builder.plan, fmt.Sprintf(state.messages.BatchAddSummaryFmt, len(builder.plan.hosts), builder.plan.duplicateHosts), 0)
	return ""
}

// parseBatchHostLine reads one pasted line: "[ssh://][user@]host[:port] 显示名".
// An address without a port is SSH port 22. IPv6 addresses are written in
// brackets, as "[::1]:22"; a bare address of several colons is taken as an IPv6
// literal only when it parses as one.
func parseBatchHostLine(line string) (Host, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Host{}, errBatchLine
	}
	address, user, port, ok := parseBatchEndpoint(fields[0])
	if !ok {
		return Host{}, errBatchLine
	}
	host := Host{Label: address, Address: address, User: user, Port: port}
	if len(fields) > 1 {
		host.Label = strings.Join(fields[1:], " ")
	}
	return host, nil
}

func parseBatchEndpoint(spec string) (address, user string, port uint16, ok bool) {
	if len(spec) >= 6 && strings.EqualFold(spec[:6], "ssh://") {
		spec = spec[6:]
	}
	if at := strings.LastIndex(spec, "@"); at >= 0 {
		user, spec = spec[:at], spec[at+1:]
	}
	port = defaultPort
	if spec == "" {
		return "", "", 0, false
	}
	// "[::1]:22" names the address inside the brackets and the port after them.
	if strings.HasPrefix(spec, "[") {
		end := strings.Index(spec, "]")
		if end < 0 || net.ParseIP(spec[1:end]) == nil {
			return "", "", 0, false
		}
		address = spec[1:end]
		if rest := spec[end+1:]; rest != "" {
			if rest[0] != ':' || parseBatchPort(rest[1:], &port) != nil {
				return "", "", 0, false
			}
		}
		return address, user, port, true
	}
	switch colons := strings.Count(spec, ":"); {
	case colons == 1:
		host, portText, found := strings.Cut(spec, ":")
		if !found || parseBatchPort(portText, &port) != nil {
			return "", "", 0, false
		}
		return host, user, port, true
	case colons > 1:
		// "2001:db8::1" is an IPv6 literal; anything else with several colons is
		// a typo, and "2001:db8::1:22" a bracket-less address with a port that
		// this parser refuses to guess at.
		if net.ParseIP(spec) == nil {
			return "", "", 0, false
		}
	}
	return spec, user, port, true
}

// parseBatchPort reads a port of 1–65535 into *port.
func parseBatchPort(text string, port *uint16) error {
	parsed, err := strconv.ParseUint(text, 10, 16)
	if err != nil || parsed == 0 {
		return fmt.Errorf("port %q", text)
	}
	*port = uint16(parsed)
	return nil
}

// errBatchLine marks the endpoint token of a line as unreadable; the caller
// names the line and shows what it read.
var errBatchLine = fmt.Errorf("not [ssh://][user@]host[:port]")

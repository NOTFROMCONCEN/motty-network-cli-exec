package cliparse

import (
	"regexp"
	"strings"
)

// Interface brief output comes in two shapes: Huawei/H3C print PHY and
// Protocol columns, while Cisco/Ruijie print an address and a status. Both are
// whitespace-separated, and only the interface name can contain a space.

const (
	ifaceHuawei = iota
	ifaceCiscoIP
	ifaceCiscoIPFull
	ifaceCiscoNoIP
)

var (
	ansiPattern   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	slotPattern   = regexp.MustCompile(`^\d+(/\d+)*$`)
	promptPattern = regexp.MustCompile(`^(?:<[^>]+>|\[[^\]]+\]|[A-Za-z0-9._-]+(?:\([^)]*\))?[>#])$`)
)

// ParseInterfaces parses display interface brief / show ip interface brief.
func ParseInterfaces(output string) Table {
	table := Table{Kind: "interfaces"}
	lines := strings.Split(ansiPattern.ReplaceAllString(output, ""), "\n")
	start, kind := -1, ifaceHuawei
	for index, line := range lines {
		if k, columns, ok := interfaceHeader(strings.TrimSpace(line)); ok {
			start, kind = index, k
			table.Columns = columns
			break
		}
	}
	if start < 0 {
		table.Warnings = append(table.Warnings, "no interface table header found")
		return table
	}
	for _, raw := range lines[start+1:] {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			if len(table.Rows) > 0 {
				break
			}
			continue
		}
		if isPromptLine(line) || strings.Contains(line, "---- More ----") ||
			strings.Contains(line, "--More--") || strings.Contains(line, "---MORE---") {
			break
		}
		var row []string
		var ok bool
		switch kind {
		case ifaceHuawei:
			row, ok = parseHuaweiInterface(line)
		case ifaceCiscoIP:
			row, ok = parseCiscoInterface(line, false)
		case ifaceCiscoIPFull:
			row, ok = parseCiscoInterface(line, true)
		case ifaceCiscoNoIP:
			row, ok = parseCiscoStatusOnly(line)
		}
		if !ok {
			table.Warnings = append(table.Warnings, Truncate(line, 120))
			continue
		}
		table.Rows = append(table.Rows, row)
	}
	if len(table.Rows) == 0 {
		table.Warnings = append(table.Warnings, "no interface rows found")
	}
	return table
}

func interfaceHeader(line string) (int, []string, bool) {
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "interface") {
		return 0, nil, false
	}
	switch {
	case strings.Contains(lower, "phy") && strings.Contains(lower, "protocol"):
		return ifaceHuawei, []string{"Interface", "PHY", "Protocol", "InUti", "OutUti", "InErrors", "OutErrors"}, true
	case strings.Contains(lower, "ip-address") || strings.Contains(lower, "ip address"):
		if strings.Contains(lower, "ok?") && strings.Contains(lower, "method") {
			return ifaceCiscoIPFull, []string{"Interface", "IP-Address", "OK", "Method", "Status", "Protocol"}, true
		}
		return ifaceCiscoIP, []string{"Interface", "IP-Address", "Status", "Protocol"}, true
	case strings.Contains(lower, "status") && strings.Contains(lower, "protocol"):
		return ifaceCiscoNoIP, []string{"Interface", "Status", "Protocol"}, true
	}
	return 0, nil, false
}

func parseHuaweiInterface(line string) ([]string, bool) {
	tokens := strings.Fields(line)
	if len(tokens) < 3 {
		return nil, false
	}
	name := strings.TrimLeft(tokens[0], "*!")
	if name == "" {
		return nil, false
	}
	row := []string{name, tokens[1], tokens[2], "", "", "", ""}
	for index := 3; index < len(tokens) && index < len(row); index++ {
		row[index] = tokens[index]
	}
	return row, true
}

func parseCiscoInterface(line string, full bool) ([]string, bool) {
	tokens := strings.Fields(line)
	if len(tokens) < 3 {
		return nil, false
	}
	name, rest, ok := splitCiscoName(tokens)
	if !ok || len(rest) < 2 {
		return nil, false
	}
	protocol := rest[len(rest)-1]
	status := rest[len(rest)-2]
	statusStart := len(rest) - 2
	if len(rest) >= 3 && rest[len(rest)-3] == "administratively" {
		status = "administratively down"
		statusStart = len(rest) - 3
	}
	before := rest[:statusStart]
	ip, okField, method := "", "", ""
	switch {
	case len(before) >= 3:
		ip, okField, method = before[0], before[1], before[2]
	case len(before) >= 1:
		ip = before[0]
	}
	if full {
		return []string{name, ip, okField, method, status, protocol}, true
	}
	return []string{name, ip, status, protocol}, true
}

func parseCiscoStatusOnly(line string) ([]string, bool) {
	tokens := strings.Fields(line)
	if len(tokens) < 3 {
		return nil, false
	}
	name, rest, ok := splitCiscoName(tokens)
	if !ok || len(rest) < 2 {
		return nil, false
	}
	protocol := rest[len(rest)-1]
	status := rest[len(rest)-2]
	if len(rest) >= 3 && rest[len(rest)-3] == "administratively" {
		status = "administratively down"
	}
	return []string{name, status, protocol}, true
}

// splitCiscoName joins an interface whose slot is written as a second token,
// as some Ruijie builds do ("GigabitEthernet 0/1").
func splitCiscoName(tokens []string) (string, []string, bool) {
	name := strings.TrimLeft(tokens[0], "*!")
	if name == "" {
		return "", nil, false
	}
	if len(tokens) > 2 && slotPattern.MatchString(tokens[1]) {
		return name + " " + tokens[1], tokens[2:], true
	}
	return name, tokens[1:], true
}

func isPromptLine(line string) bool {
	return promptPattern.MatchString(strings.TrimSpace(line))
}

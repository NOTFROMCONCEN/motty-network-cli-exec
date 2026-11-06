package netcli

import (
	"regexp"
	"strings"
)

// Facts are the few stable fields a version command prints across vendors.
// Anything the patterns miss stays empty; the caller can still show the raw
// output.
type Facts struct {
	Platform string
	Version  string
	Model    string
	Uptime   string
}

var (
	factsVersion = regexp.MustCompile(`(?i)\bversion\s+([^\s,]+)`)
	factsUptime  = regexp.MustCompile(`(?i)\buptime is\s+(.+)$`)
	// Huawei, H3C and Ruijie put the model on the uptime line; Cisco puts it on
	// the processor line. A bare "Cisco IOS Software" line must not win.
	factsModelUptime    = regexp.MustCompile(`(?i)^(?:huawei|h3c|ruijie)\s+(\S+).*\buptime is\b`)
	factsModelProcessor = regexp.MustCompile(`(?i)^cisco\s+(\S+).*\bprocessor\b`)
)

// ParseFacts reads display version / show version output. It is tolerant by
// design: a field whose line is absent is simply empty.
func ParseFacts(platformID, output string) Facts {
	facts := Facts{Platform: platformID}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if facts.Version == "" {
			if match := factsVersion.FindStringSubmatch(line); match != nil {
				facts.Version = strings.TrimSpace(match[1])
			}
		}
		if facts.Uptime == "" {
			if match := factsUptime.FindStringSubmatch(line); match != nil {
				facts.Uptime = strings.TrimSpace(match[1])
			}
		}
		if facts.Model == "" {
			if match := factsModelUptime.FindStringSubmatch(line); match != nil {
				facts.Model = strings.TrimSpace(match[1])
			} else if match := factsModelProcessor.FindStringSubmatch(line); match != nil {
				facts.Model = strings.TrimSpace(match[1])
			}
		}
	}
	return facts
}

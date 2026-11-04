// Package netcli runs one command on a network device's interactive CLI and
// learns how it ended.
//
// A device CLI has no shell wrapper, no "$?" and no completion marker: the
// command is typed as the device expects it, and the only sign that it finished
// is the device prompt coming back. A [Tap] watches the terminal output for
// that prompt, handles a configured platform's pager, and recognizes common
// error lines. [Classify] rates what a command may do so the approval gate can
// ask before anything that is not a read-only check.
//
// Everything here is advice and pattern matching, never a security boundary:
// prompts can look like output lines, and a command's effect is only as well
// known as its platform table.
package netcli

import "strings"

// Platform describes one family of network device CLIs: what its prompt looks
// like, how its pager behaves, which lines report a command error, and what the
// first word of a command says about its effect.
type Platform struct {
	// ID is the stable name saved in a host's settings and shown to the model.
	ID string
	// Name is the human-readable label of the host settings UI. Platform names
	// are proper nouns and are not translated.
	Name string
	// matchPrompt reports whether line is exactly a device prompt of this
	// platform and returns the hostname it carries. A nil matchPrompt means the
	// platform makes no promise about prompts: only the exact prompt captured
	// before the command can end a command.
	matchPrompt func(line string) (host string, ok bool)
	// pagers are the texts a full output page ends with while the device
	// waits. They are detected on every platform that names any.
	pagers []string
	// pagerKey is what to type to see the next page. nil means the platform's
	// pager is detected but never continued automatically: an unverified key
	// must not be typed at an unknown device.
	pagerKey []byte
	// errors are substrings that mark an output line as a CLI error. The first
	// line that contains one is reported as the command's error.
	errors []string
	// readOnly are case-insensitive prefixes of commands that only inspect
	// state.
	readOnly []string
	// configRead are case-insensitive prefixes of read-only commands whose
	// output may hold credentials, so they rate above an ordinary read.
	configRead []string
	// risky are first words of commands that change state or enter another
	// mode. Anything not listed at all is rated like these: unknown CLI input
	// never runs without the user's approval.
	risky []string
	// critical are first words of commands that can destroy the device, its
	// data or the access to it.
	critical []string
}

// GenericID is the platform a network CLI session uses when it names none: it
// knows no prompts, pagers or commands, so it only ever completes on the exact
// prompt it saw before, and classifies no command as read-only.
const GenericID = "generic"

// anglePrompt matches the angle/bracket prompt shape of Huawei VRP and H3C
// Comware: user view "<host>", system view "[host]", a view suffix
// "[host-GigabitEthernet0/0/1]", and the uncommitted "~"/"*" markers some
// versions put inside the brackets.
func anglePrompt(line string) (string, bool) {
	switch {
	case strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">"):
		host := line[1 : len(line)-1]
		return host, validHostname(host)
	case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
		inner := strings.TrimLeft(line[1:len(line)-1], "~*")
		host, _, _ := strings.Cut(inner, "-")
		return host, validHostname(host)
	}
	return "", false
}

// hashPrompt matches the hash/gt prompt shape of Cisco IOS, IOS-XE and Ruijie
// RGOS: user mode "host>", privileged "host#", and a configuration view
// "host(config)#" / "host(config-if)#".
func hashPrompt(line string) (string, bool) {
	host, ok := strings.CutSuffix(line, "#")
	if !ok {
		host, ok = strings.CutSuffix(line, ">")
	}
	if !ok {
		return "", false
	}
	// A configuration view adds "(config...)" between the host and the prompt
	// character; drop it with the rest of the parenthesized suffix.
	if before, _, found := strings.Cut(host, "("); found {
		host = before
	}
	return host, validHostname(host)
}

// validHostname keeps the prompt matcher from reading arbitrary text as a
// hostname: one token of the characters a network OS puts in a host name.
func validHostname(host string) bool {
	if host == "" || len(host) > 64 || strings.ContainsFunc(host, func(r rune) bool {
		return r <= ' ' || r >= 0x7f || strings.ContainsRune("<>[]()#", r)
	}) {
		return false
	}
	return true
}

// errFirst returns the first error pattern the line contains.
func (p *Platform) errFirst(line string) string {
	if p == nil {
		return ""
	}
	for _, e := range p.errors {
		if strings.Contains(line, e) {
			return e
		}
	}
	return ""
}

// MatchPrompt is MatchShape with the exact-prompt fallback: the platform's own
// shape when it has one, or nothing at all when it is the generic platform.
func (p *Platform) MatchPrompt(line string) (string, bool) {
	if p == nil || p.matchPrompt == nil {
		return "", false
	}
	return p.matchPrompt(line)
}

// Shaped reports whether the platform knows what its prompts look like. A
// shaped platform can prove a terminal line is a ready device prompt; the
// generic platform cannot.
func (p *Platform) Shaped() bool { return p != nil && p.matchPrompt != nil }

// generic classifies nothing as known: every command asks. It still detects
// the two pager wordings every verified platform uses, and keeps the universal
// destructive words, but has no key to continue a pager with.
var generic = &Platform{
	ID:       GenericID,
	Name:     "Generic CLI",
	pagers:   []string{"---- More ----", "--More--"},
	critical: []string{"reboot", "reload", "format", "erase", "restore"},
}

// platforms are the platform tables verified well enough to autocomplete the
// pager and to call read-only commands by name. Each list is independent on
// purpose: a vendor's wording moves, and one platform's pager key is another's
// quit key.
var platforms = []*Platform{
	{
		ID:          "huawei_vrp",
		Name:        "Huawei VRP",
		matchPrompt: anglePrompt,
		pagers:      []string{"---- More ----"},
		pagerKey:    []byte{' '},
		errors: []string{
			"Unrecognized command", "Ambiguous command", "Incomplete command",
			"Too many parameters", "Wrong parameter", "Incorrect command",
			"Error:",
		},
		readOnly:   []string{"display ", "ping ", "tracert "},
		configRead: []string{"display current-configuration", "display saved-configuration"},
		risky: []string{
			"system-view", "save", "super", "undo", "interface", "vlan",
			"sysname", "user-interface", "aaa", "screen-length", "reset",
			"copy", "delete", "mkdir", "rmdir", "move", "rename", "execute",
		},
		critical: []string{"reboot", "format", "restore", "factory-reset"},
	},
	{
		ID:          "h3c_comware",
		Name:        "H3C Comware",
		matchPrompt: anglePrompt,
		pagers:      []string{"---- More ----"},
		pagerKey:    []byte{' '},
		errors: []string{
			"Unrecognized command", "Ambiguous command", "Incomplete command",
			"Too many parameters", "Wrong parameter", "Incorrect command",
			"Error:",
		},
		readOnly:   []string{"display ", "ping ", "tracert "},
		configRead: []string{"display current-configuration", "display saved-configuration", "display startup-configuration"},
		risky: []string{
			"system-view", "save", "super", "undo", "interface", "vlan",
			"sysname", "local-user", "user-interface", "aaa", "screen-length",
			"reset", "copy", "delete", "mkdir", "rmdir", "move", "rename", "execute",
		},
		critical: []string{"reboot", "format", "restore", "factory-reset"},
	},
	{
		ID:          "cisco_ios",
		Name:        "Cisco IOS/IOS-XE",
		matchPrompt: hashPrompt,
		pagers:      []string{"--More--"},
		pagerKey:    []byte{' '},
		errors: []string{
			"Invalid input detected", "Ambiguous command", "Incomplete command",
			"Unknown command", "Invalid input", "% Error",
		},
		readOnly:   []string{"show ", "ping ", "traceroute "},
		configRead: []string{"show running-config", "show startup-config", "show config", "more system:running-config"},
		risky: []string{
			"configure", "conf", "enable", "write", "copy", "delete", "rename",
			"terminal length", "terminal width", "setup", "license", "archive",
		},
		critical: []string{"reload", "erase", "format", "restore"},
	},
	{
		ID:          "ruijie_rgos",
		Name:        "Ruijie RGOS",
		matchPrompt: hashPrompt,
		pagers:      []string{"--More--"},
		pagerKey:    []byte{' '},
		errors: []string{
			"Invalid input detected", "Ambiguous command", "Incomplete command",
			"Unknown command", "Invalid input", "% Error",
		},
		readOnly:   []string{"show ", "ping ", "traceroute "},
		configRead: []string{"show running-config", "show startup-config"},
		risky: []string{
			"configure", "conf", "enable", "write", "copy", "delete", "rename",
			"terminal length", "setup", "ruijie", "archive",
		},
		critical: []string{"reboot", "reload", "erase", "format", "restore"},
	},
}

// Lookup returns the platform table with the given id, or false when the id is
// not one of the verified ones.
func Lookup(id string) (*Platform, bool) {
	for _, p := range platforms {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

// Table returns the platform for a saved setting: the named one, or the
// generic platform for an empty or unknown id. A session never fails because
// its platform name is stale; the generic table just promises less.
func Table(id string) *Platform {
	if p, ok := Lookup(id); ok {
		return p
	}
	return generic
}

// IDs lists the platform ids a host setting can name, generic first.
func IDs() []string {
	out := make([]string, 0, 1+len(platforms))
	out = append(out, GenericID)
	for _, p := range platforms {
		out = append(out, p.ID)
	}
	return out
}

// LooksLikeLoginPrompt reports whether a terminal line is a login, account or
// password prompt rather than a ready device CLI prompt. A session on the
// generic table cannot prove its prompt is a CLI one, so this is what keeps an
// unchecked command from being typed into a sign-in question.
func LooksLikeLoginPrompt(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	l = strings.TrimRight(l, " ")
	if l == "" {
		return false
	}
	// A sign-in question ends in ':' (or a bracketed default in ']'), and names
	// one of the words every login program uses.
	if !strings.HasSuffix(l, ":") && !strings.HasSuffix(l, "]:") && !strings.HasSuffix(l, ")") {
		return false
	}
	for _, word := range []string{"username", "user name", "login", "password", "passwd", "passcode", "passphrase", "user:", "account"} {
		if strings.Contains(l, word) {
			return true
		}
	}
	return strings.HasPrefix(l, "user:") || strings.HasPrefix(l, "login:")
}

// Name returns the display name of a platform id.
func Name(id string) string {
	if p, ok := Lookup(id); ok {
		return p.Name
	}
	if id == GenericID {
		return generic.Name
	}
	return id
}

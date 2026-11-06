package netcli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// A user platform file adds vendor tables without rebuilding the program. The
// built-in tables stay authoritative: a file can add a vendor, not redefine
// one, and generic always remains the fallback.
const (
	// ConfigVersion is the only platform file version this build understands.
	ConfigVersion = 1
	// MaxConfigBytes bounds the file before it is parsed.
	MaxConfigBytes = 1 << 20
	// MaxUserPlatforms bounds how many tables one file may add.
	MaxUserPlatforms = 64
	// maxPlatformListEntries bounds each command/string list in one table.
	maxPlatformListEntries = 256
)

var platformIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// platformDocument is the top-level YAML document. Strict decoding rejects
// unknown fields, so a typo cannot silently do nothing.
type platformDocument struct {
	Version   int             `yaml:"version"`
	Platforms []platformEntry `yaml:"platforms"`
}

// platformEntry is one table as the file spells it. The fields mirror what
// Platform promises, with prompt shapes named instead of code.
type platformEntry struct {
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Prompt         string   `yaml:"prompt"`
	PromptRegex    string   `yaml:"prompt_regex"`
	Banners        []string `yaml:"banners"`
	Pagers         []string `yaml:"pagers"`
	PagerKey       string   `yaml:"pager_key"`
	Errors         []string `yaml:"errors"`
	ReadOnly       []string `yaml:"read_only"`
	ConfigRead     []string `yaml:"config_read"`
	Risky          []string `yaml:"risky"`
	RiskyPrefix    []string `yaml:"risky_prefix"`
	Critical       []string `yaml:"critical"`
	CriticalPrefix []string `yaml:"critical_prefix"`
	// The three read paths this build knows how to run on the vendor.
	FactsCommand     string `yaml:"facts_command"`
	InterfaceCommand string `yaml:"interface_command"`
	ConfigCommand    string `yaml:"config_command"`
}

// ValidID reports whether id has the shape of a platform id. It does not
// require the table to be loaded: a host whose YAML file is missing must keep
// its setting and fall back to generic until the file returns.
func ValidID(id string) bool { return platformIDPattern.MatchString(id) }

// LoadFile reads path and installs its tables. A missing file clears the
// user-added tables and is not an error: most installations have no file. A
// broken file leaves the tables that were already installed untouched, so a
// typo does not silently remove a working vendor.
func LoadFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		SetUserPlatforms(nil)
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	tables, err := ParseConfig(data)
	if err != nil {
		return 0, fmt.Errorf("platform file %s: %w", path, err)
	}
	SetUserPlatforms(tables)
	return len(tables), nil
}

// ParseConfig validates one platform file and returns its tables.
func ParseConfig(data []byte) ([]*Platform, error) {
	if len(data) > MaxConfigBytes {
		return nil, fmt.Errorf("file is larger than %d bytes", MaxConfigBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document platformDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	var extra yaml.Node
	switch err := decoder.Decode(&extra); {
	case err == nil:
		return nil, errors.New("the file must contain one YAML document")
	case !errors.Is(err, io.EOF):
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	if document.Version != ConfigVersion {
		return nil, fmt.Errorf("version %d is not supported; use version %d", document.Version, ConfigVersion)
	}
	if len(document.Platforms) > MaxUserPlatforms {
		return nil, fmt.Errorf("more than %d platforms", MaxUserPlatforms)
	}
	tables := make([]*Platform, 0, len(document.Platforms))
	seen := make(map[string]bool, len(document.Platforms))
	for index, entry := range document.Platforms {
		table, err := buildPlatform(entry)
		if err != nil {
			return nil, fmt.Errorf("platform %d: %w", index+1, err)
		}
		if table.ID == GenericID {
			return nil, fmt.Errorf("platform %d: id %q is reserved", index+1, GenericID)
		}
		if _, builtin := builtinLookup(table.ID); builtin {
			return nil, fmt.Errorf("platform %d: id %q is a built-in platform", index+1, table.ID)
		}
		if seen[table.ID] {
			return nil, fmt.Errorf("platform %d: id %q appears more than once", index+1, table.ID)
		}
		seen[table.ID] = true
		tables = append(tables, table)
	}
	return tables, nil
}

// SetUserPlatforms installs the tables a file added. The slice is copied. The
// caller must have validated it (ParseConfig does); built-in tables are never
// replaced by Lookup.
func SetUserPlatforms(tables []*Platform) {
	userRegistry.Lock()
	userRegistry.tables = append([]*Platform(nil), tables...)
	userRegistry.Unlock()
}

// ResetUserPlatforms removes every user-added table. Tests use it to isolate
// themselves from a previous case; a reload after deleting the file clears the
// tables the same way.
func ResetUserPlatforms() { SetUserPlatforms(nil) }

// builtinLookup searches only the compiled-in tables. Validation uses it so a
// user file cannot collide with a built-in even when another user table is
// already installed.
func builtinLookup(id string) (*Platform, bool) {
	for _, p := range platforms {
		if p.ID == id {
			return p, true
		}
	}
	return nil, false
}

func buildPlatform(entry platformEntry) (*Platform, error) {
	id := strings.TrimSpace(entry.ID)
	if !platformIDPattern.MatchString(id) {
		return nil, fmt.Errorf("id %q must match ^[a-z0-9_]{1,64}$", id)
	}
	name := strings.TrimSpace(entry.Name)
	if name == "" {
		return nil, errors.New("name is empty")
	}
	if len(name) > 64 {
		return nil, errors.New("name is longer than 64 bytes")
	}
	matchPrompt, err := promptMatcher(entry)
	if err != nil {
		return nil, err
	}
	table := &Platform{ID: id, Name: name, matchPrompt: matchPrompt}
	if table.banners, err = configStrings("banners", entry.Banners, 32, 128); err != nil {
		return nil, err
	}
	if table.pagers, err = configStrings("pagers", entry.Pagers, 16, 64); err != nil {
		return nil, err
	}
	if table.errors, err = configStrings("errors", entry.Errors, 64, 128); err != nil {
		return nil, err
	}
	if table.readOnly, err = configStrings("read_only", entry.ReadOnly, maxPlatformListEntries, 128); err != nil {
		return nil, err
	}
	if table.configRead, err = configStrings("config_read", entry.ConfigRead, maxPlatformListEntries, 128); err != nil {
		return nil, err
	}
	if table.risky, err = configWords("risky", entry.Risky); err != nil {
		return nil, err
	}
	if table.critical, err = configWords("critical", entry.Critical); err != nil {
		return nil, err
	}
	if table.riskyPrefix, err = configStrings("risky_prefix", entry.RiskyPrefix, maxPlatformListEntries, 128); err != nil {
		return nil, err
	}
	if table.criticalPrefix, err = configStrings("critical_prefix", entry.CriticalPrefix, maxPlatformListEntries, 128); err != nil {
		return nil, err
	}
	if table.pagerKey, err = parsePagerKey(entry.PagerKey); err != nil {
		return nil, err
	}
	if table.factsCommand, err = configCommandLine("facts_command", entry.FactsCommand); err != nil {
		return nil, err
	}
	if table.interfaceCommand, err = configCommandLine("interface_command", entry.InterfaceCommand); err != nil {
		return nil, err
	}
	if table.configCommand, err = configCommandLine("config_command", entry.ConfigCommand); err != nil {
		return nil, err
	}
	return table, nil
}

// configCommandLine validates one command a tool may run on the platform. It
// must be a single line; the command itself is typed raw, so nothing else is
// normalized.
func configCommandLine(field, value string) (string, error) {
	command := strings.TrimSpace(value)
	if command == "" {
		return "", nil
	}
	if len(command) > 256 {
		return "", fmt.Errorf("%s is longer than 256 bytes", field)
	}
	if strings.ContainsAny(command, "\r\n\t") || strings.IndexByte(command, 0) >= 0 {
		return "", fmt.Errorf("%s must be one command line", field)
	}
	return command, nil
}

// promptMatcher turns the file's prompt name (or regex) into the closure the
// rest of the package expects. "none" keeps the generic behavior: only the
// exact prompt seen before a command can end it.
func promptMatcher(entry platformEntry) (func(string) (string, bool), error) {
	switch strings.ToLower(strings.TrimSpace(entry.Prompt)) {
	case "", "none":
		if entry.PromptRegex != "" {
			return nil, errors.New("prompt_regex needs prompt: regex")
		}
		return nil, nil
	case "angle":
		if entry.PromptRegex != "" {
			return nil, errors.New("prompt_regex is only used with prompt: regex")
		}
		return anglePrompt, nil
	case "hash":
		if entry.PromptRegex != "" {
			return nil, errors.New("prompt_regex is only used with prompt: regex")
		}
		return hashPrompt, nil
	case "regex":
		pattern := strings.TrimSpace(entry.PromptRegex)
		if pattern == "" {
			return nil, errors.New("prompt: regex needs prompt_regex")
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("prompt_regex: %w", err)
		}
		hostIndex := re.SubexpIndex("host")
		if hostIndex < 0 {
			return nil, errors.New(`prompt_regex must contain a named capture group "(?P<host>...)"`)
		}
		return func(line string) (string, bool) {
			match := re.FindStringSubmatch(line)
			if match == nil {
				return "", false
			}
			host := match[hostIndex]
			return host, validHostname(host)
		}, nil
	default:
		return nil, fmt.Errorf("prompt %q must be angle, hash, regex or none", entry.Prompt)
	}
}

// configStrings validates a list without changing its entries: a trailing
// space in "display " is meaningful to the prefix matcher.
func configStrings(field string, values []string, max, maxLen int) ([]string, error) {
	if len(values) > max {
		return nil, fmt.Errorf("%s has more than %d entries", field, max)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s contains an empty entry", field)
		}
		if len(value) > maxLen {
			return nil, fmt.Errorf("%s entry is longer than %d bytes", field, maxLen)
		}
		out = append(out, value)
	}
	return out, nil
}

// configWords validates first-word lists: Classify compares one word, so a
// multi-word entry could never match. Longer commands belong in *_prefix.
func configWords(field string, values []string) ([]string, error) {
	if len(values) > maxPlatformListEntries {
		return nil, fmt.Errorf("%s has more than %d entries", field, maxPlatformListEntries)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		word := strings.ToLower(strings.TrimSpace(value))
		if word == "" {
			return nil, fmt.Errorf("%s contains an empty entry", field)
		}
		if strings.ContainsAny(word, " \t\r\n") {
			return nil, fmt.Errorf("%s entry %q must be one word; use %s_prefix for a command line", field, value, field)
		}
		if len(word) > 64 {
			return nil, fmt.Errorf("%s entry is longer than 64 bytes", field)
		}
		out = append(out, word)
	}
	return out, nil
}

// parsePagerKey accepts the three keys a device pager actually uses, named or
// literal. An unlisted key would be typed at a device nobody verified.
func parsePagerKey(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	if value == " " {
		return []byte{' '}, nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "space":
		return []byte{' '}, nil
	case "enter":
		return []byte{'\r'}, nil
	case "q":
		return []byte{'q'}, nil
	}
	return nil, fmt.Errorf("pager_key %q must be space, enter or q", value)
}

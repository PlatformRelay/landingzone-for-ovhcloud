package release

import (
	"errors"
	"fmt"
	"strings"
)

// The [tools] table of mise.toml is read with a deliberately small TOML reader that refuses what
// it cannot read exactly, so a pin is never dropped or mangled: in [tools] only
// `key = "version"` or `key = 'version'` lines (bare or quoted keys, an optional trailing comment),
// no escapes, no dotted keys, no [tools.*] subtables, no top-level tools key, no escaped key
// at the top level or in a table name, and no multi-line strings anywhere. Other tables are skipped.

const (
	sectionRoot  = ""
	sectionTools = "tools"
	sectionOther = "other"
)

func parseTools(text string) (map[string]string, error) {
	if strings.Contains(text, `"""`) || strings.Contains(text, "'''") {
		return nil, errors.New("RELEASE_TOOLCHAIN mise.toml: multi-line strings are not read")
	}
	tools := map[string]string{}
	section := sectionRoot
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		var err error
		switch {
		case line == "" || line[0] == '#':
		case line[0] == '[':
			section, err = tableName(line)
		default:
			err = readToolLine(tools, section, line)
		}
		if err != nil {
			return nil, fmt.Errorf("RELEASE_TOOLCHAIN mise.toml line %d: %w", n+1, err)
		}
	}
	return tools, nil
}

// tableName reads a table header: tools, another table, or a refusal for a tools subtable or
// array of tables and a malformed header.
func tableName(line string) (string, error) {
	opener, closer := "[", "]"
	if strings.HasPrefix(line, "[[") {
		opener, closer = "[[", "]]"
	}
	end := strings.Index(line, closer)
	if end < 0 {
		return "", fmt.Errorf("malformed table header %q", line)
	}
	if rest := strings.TrimSpace(line[end+len(closer):]); rest != "" && rest[0] != '#' {
		return "", fmt.Errorf("text after the table header %q", line)
	}
	keys, err := dottedKeys(line[len(opener):end])
	switch {
	case err != nil:
		return "", fmt.Errorf("table header %q: %w", line, err)
	case keys[0] != sectionTools:
		return sectionOther, nil
	case len(keys) > 1 || opener == "[[":
		return "", fmt.Errorf("table %q: only `key = \"version\"` lines in [tools] are read", line)
	}
	return sectionTools, nil
}

// dottedKeys reads a table name as TOML keys separated by dots, each bare or quoted, so a quoted
// name is never compared with its quotes or spaces dropped.
func dottedKeys(s string) ([]string, error) {
	var keys []string
	for {
		key, rest, err := parseKey(strings.TrimLeft(s, " \t"))
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return keys, nil
		}
		if rest[0] != '.' {
			return nil, fmt.Errorf("unexpected %q", rest)
		}
		s = rest[1:]
	}
}

// readToolLine reads one key line: a pin in [tools], a refusal for a top-level tools key, and
// nothing in any other table.
func readToolLine(tools map[string]string, section, line string) error {
	if section == sectionOther {
		return nil
	}
	key, rest, err := parseKey(line)
	if section == sectionRoot {
		// A top-level key could define tools; one this reader cannot read (an escape) is refused,
		// a line that is no key (the inside of a multi-line array) is skipped.
		switch {
		case err == nil && key == sectionTools:
			return errors.New("a top-level tools key is not read")
		case err != nil && (line[0] == '"' || line[0] == '\''):
			return fmt.Errorf("a top-level key is not read: %w", err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "=") {
		return fmt.Errorf("%s: not `key = \"version\"` (dotted keys are not read)", key)
	}
	version, rest, err := parseString(strings.TrimLeft(rest[1:], " \t"))
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if rest = strings.TrimSpace(rest); rest != "" && rest[0] != '#' {
		return fmt.Errorf("%s: text after the value", key)
	}
	if !versionPin.MatchString(version) {
		return fmt.Errorf("%s: version %q is empty or holds whitespace", key, version)
	}
	if _, seen := tools[key]; seen {
		return fmt.Errorf("%s: pinned twice", key)
	}
	tools[key] = version
	return nil
}

// parseKey reads a bare or quoted key and returns it with the rest of the line.
func parseKey(s string) (string, string, error) {
	if s == "" {
		return "", "", errors.New("no key")
	}
	if s[0] == '"' || s[0] == '\'' {
		return parseString(s)
	}
	end := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	})
	if end < 0 {
		end = len(s)
	}
	if end == 0 {
		return "", "", fmt.Errorf("no key in %q", s)
	}
	return s[:end], s[end:], nil
}

// parseString reads a one-line basic ("…", no escapes) or literal ('…') string at the start of s
// and returns it with the rest of s; any other value is refused.
func parseString(s string) (string, string, error) {
	if s == "" || (s[0] != '"' && s[0] != '\'') {
		return "", "", errors.New("the value is not a quoted string")
	}
	end := strings.IndexByte(s[1:], s[0])
	if end < 0 {
		return "", "", errors.New("unterminated string")
	}
	value := s[1 : end+1]
	if s[0] == '"' && strings.ContainsRune(value, '\\') {
		return "", "", errors.New("escapes are not read")
	}
	return value, s[end+2:], nil
}

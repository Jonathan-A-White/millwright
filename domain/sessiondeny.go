package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The [session] table of the config file and its one key (mw-t5shinh.22).
const (
	SessionTable   = "session"
	SessionDenyKey = "deny"
)

// permissionRuleName is the name part of the shape of a Claude Code permission rule: a tool name, and
// optionally a specifier in parentheses (`Bash`, `Bash(scp *)`, `Read(~/.ssh/**)`,
// `mcp__server__tool`).
var permissionRuleName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

// CheckPermissionRule refuses a rule that is not of the shape `Name` or
// `Name(text)`: a Name of letters, digits, `_`, `.` and `-` beginning with a
// letter, and a text that is not empty. A rule holding a newline is refused
// whatever else it holds, since a settings file reads one rule to a line of
// thought and a newline could be the start of a second command. The error names
// the rule.
func CheckPermissionRule(rule string) error {
	if strings.ContainsAny(rule, "\n\r") {
		return fmt.Errorf("the permission rule %q holds a newline: a rule is one line", rule)
	}
	name, rest, hasText := strings.Cut(rule, "(")
	if !permissionRuleName.MatchString(name) {
		return fmt.Errorf("the permission rule %q is not of the shape Name or Name(text): %q is not a tool name", rule, name)
	}
	if !hasText {
		return nil
	}
	if !strings.HasSuffix(rest, ")") || len(rest) < 2 {
		return fmt.Errorf("the permission rule %q is not of the shape Name or Name(text): the text must end in a closing )", rule)
	}
	return nil
}

// CheckSessionDenyChange judges the config after table.key was set: of the
// `[session]` table only `deny` is its business, and it must be an array of
// strings that are each a permission rule (CheckPermissionRule), so that a
// typing slip is refused by `mw config set` and cannot stop a dispatch.
func CheckSessionDenyChange(after *ConfigTOML, table, key string) error {
	if table != SessionTable || key != SessionDenyKey {
		return nil
	}
	literal, ok := after.Lookup(table, key)
	if !ok {
		return nil
	}
	rules, err := SessionDenyRules(literal)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if err := CheckPermissionRule(rule); err != nil {
			return fmt.Errorf("%s.%s: %w", table, key, err)
		}
	}
	return nil
}

// SessionDenyRules is the strings of the `deny` value as the file spells it. A
// value that is not an array of strings is an error naming the key. The rules
// themselves are not judged here: CheckPermissionRule does that.
func SessionDenyRules(literal string) ([]string, error) {
	rules, err := TOMLStringArray(literal)
	if err != nil {
		return nil, fmt.Errorf("%s.%s %w", SessionTable, SessionDenyKey, err)
	}
	return rules, nil
}

// TOMLStringArray is the strings of an array literal such as ["a", 'b'], which
// may span lines and hold comments and a trailing comma. Anything else (a
// string, a number, an element that is not a string) is an error beginning "is
// not an array of strings".
func TOMLStringArray(literal string) ([]string, error) {
	notStrings := func(why string) error { return fmt.Errorf("is not an array of strings: %s (it is %s)", why, literal) }
	s := strings.TrimSpace(literal)
	if !strings.HasPrefix(s, "[") {
		return nil, notStrings("it is not an array")
	}
	items := []string{}
	i := 1
	skip := func() {
		for i < len(s) {
			switch {
			case strings.IndexByte(" \t\r\n", s[i]) >= 0:
				i++
			case s[i] == '#':
				for i < len(s) && s[i] != '\n' {
					i++
				}
			default:
				return
			}
		}
	}
	for {
		skip()
		if i >= len(s) {
			return nil, notStrings("it has no closing ]")
		}
		if s[i] == ']' {
			i++
			break
		}
		switch s[i] {
		case '"':
			end := i + 1
			for end < len(s) && s[end] != '"' {
				if s[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(s) {
				return nil, notStrings("a string has no closing quote")
			}
			text, err := strconv.Unquote(s[i : end+1])
			if err != nil {
				return nil, notStrings("a string cannot be read: " + err.Error())
			}
			items = append(items, text)
			i = end + 1
		case '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return nil, notStrings("a string has no closing quote")
			}
			items = append(items, s[i+1:i+1+end])
			i += end + 2
		default:
			return nil, notStrings("an element is not a string")
		}
		skip()
		if i < len(s) && s[i] == ',' {
			i++
		} else if i < len(s) && s[i] != ']' {
			return nil, notStrings("elements are not separated by commas")
		}
	}
	skip()
	if i < len(s) {
		return nil, notStrings("something follows the closing ]")
	}
	return items, nil
}

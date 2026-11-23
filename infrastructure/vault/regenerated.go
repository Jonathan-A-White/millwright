package vault

import (
	"fmt"
	"path"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// RegeneratedFilesKey is the key of a rig's file that lists its generated files
// and the commands that write them again (mw-gq6.1666). Its entries are tables:
//
//	[[regenerated_files]]
//	paths = ["docs/lxx-align/gen.txt"]
//	run   = "npm run lxx:align-expect -- --book gen"
//
// or, on one line, `regenerated_files = [{ paths = ["a.txt"], run = "make a" }]`.
// The tables, each ending where the next begins, go after every other key of
// the file, as TOML has it.
const RegeneratedFilesKey = "regenerated_files"

// regenTableHeader is the line that opens one entry of regenerated_files.
const regenTableHeader = "[[" + RegeneratedFilesKey + "]]"

// readRegenTableLine reads line i of a [[regenerated_files]] table (a `paths`
// list, possibly over several lines, or a quoted `run`) into entry, and says
// the last line it read.
func readRegenTableLine(entry *domain.RegeneratedFiles, lines []string, i int) (int, error) {
	key, value, found := strings.Cut(strings.TrimSpace(stripComment(lines[i])), "=")
	key, value = strings.TrimSpace(key), strings.TrimSpace(value)
	if !found || (key != "paths" && key != "run") {
		return i, fmt.Errorf("line %d: %q is inside %s, which holds only `paths` and `run` (the other keys of the rig's file go above it)", i+1, strings.TrimSpace(lines[i]), regenTableHeader)
	}
	if key == "run" {
		run, err := quotedStrings(value)
		if err != nil || len(run) != 1 || entry.Run != "" {
			return i, fmt.Errorf("line %d: expected one `run = \"<command>\"` in each %s, got %q", i+1, regenTableHeader, lines[i])
		}
		entry.Run = run[0]
		return i, nil
	}
	if !strings.HasPrefix(value, "[") || entry.Paths != nil {
		return i, fmt.Errorf("line %d: expected one `paths = [\"<file>\", ...]` in each %s, got %q", i+1, regenTableHeader, lines[i])
	}
	for !strings.Contains(value, "]") {
		if i++; i >= len(lines) {
			return i, fmt.Errorf("the list for paths in %s is never closed with ]", regenTableHeader)
		}
		value += " " + strings.TrimSpace(stripComment(lines[i]))
	}
	paths, err := quotedStrings(value[1:strings.Index(value, "]")])
	if err != nil {
		return i, fmt.Errorf("the paths of %s: %w", regenTableHeader, err)
	}
	entry.Paths = paths
	if entry.Paths == nil {
		entry.Paths = []string{}
	}
	return i, nil
}

// readRegenInline reads the value of `regenerated_files = [...]` that begins on
// line i, possibly running over several lines until its brackets and braces are
// closed, and says the last line it read.
func readRegenInline(value string, lines []string, i int) ([]domain.RegeneratedFiles, int, error) {
	for !balanced(value) {
		if i++; i >= len(lines) {
			return nil, i, fmt.Errorf("the list for %s is never closed with ]", RegeneratedFilesKey)
		}
		value += " " + strings.TrimSpace(stripComment(lines[i]))
	}
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
		return nil, i, fmt.Errorf("expected `%s = [{ paths = [\"<file>\"], run = \"<command>\" }]`, got %q", RegeneratedFilesKey, value)
	}
	var entries []domain.RegeneratedFiles
	for _, item := range splitTables(value[1 : len(value)-1]) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.HasPrefix(item, "{") || !strings.HasSuffix(item, "}") {
			return nil, i, fmt.Errorf("the list for %s: %q is not a table `{ paths = [...], run = \"...\" }`", RegeneratedFilesKey, item)
		}
		entry, err := regenInlineTable(item[1 : len(item)-1])
		if err != nil {
			return nil, i, fmt.Errorf("the list for %s: %w", RegeneratedFilesKey, err)
		}
		entries = append(entries, entry)
	}
	return entries, i, nil
}

// regenInlineTable reads `paths = ["a"], run = "b"`.
func regenInlineTable(body string) (domain.RegeneratedFiles, error) {
	var entry domain.RegeneratedFiles
	for _, part := range splitTables(body) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, found := strings.Cut(part, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch {
		case found && key == "run":
			run, err := quotedStrings(value)
			if err != nil || len(run) != 1 || entry.Run != "" {
				return entry, fmt.Errorf("expected one `run = \"<command>\"`, got %q", part)
			}
			entry.Run = run[0]
		case found && key == "paths" && strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") && entry.Paths == nil:
			paths, err := quotedStrings(value[1 : len(value)-1])
			if err != nil {
				return entry, fmt.Errorf("the paths: %w", err)
			}
			entry.Paths = paths
			if entry.Paths == nil {
				entry.Paths = []string{}
			}
		default:
			return entry, fmt.Errorf("%q is not `paths = [...]` or `run = \"...\"`", part)
		}
	}
	return entry, nil
}

// splitTables splits on the commas that are inside neither quotes, brackets nor
// braces.
func splitTables(list string) []string {
	var items []string
	var quote rune
	depth, start := 0, 0
	for i, r := range list {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '[' || r == '{':
			depth++
		case r == ']' || r == '}':
			depth--
		case r == ',' && depth == 0:
			items = append(items, list[start:i])
			start = i + 1
		}
	}
	return append(items, list[start:])
}

// balanced reports whether every bracket and brace of s outside quotes is
// closed, and it opens at least one.
func balanced(s string) bool {
	var quote rune
	depth, opened := 0, false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '[' || r == '{':
			depth, opened = depth+1, true
		case r == ']' || r == '}':
			depth--
		}
	}
	return opened && depth <= 0
}

// checkRegenerated refuses an entry of regenerated_files that names no file, no
// command, a file outside the rig or a glob that is none.
func checkRegenerated(entries []domain.RegeneratedFiles) error {
	for n, entry := range entries {
		if len(entry.Paths) == 0 || strings.TrimSpace(entry.Run) == "" {
			return fmt.Errorf("entry %d of %s needs both `paths` (at least one) and `run`", n+1, RegeneratedFilesKey)
		}
		for _, name := range entry.Paths {
			if path.IsAbs(name) || name != path.Clean(name) || name == ".." || strings.HasPrefix(name, "../") {
				return fmt.Errorf("the paths of %s: %q is not a path inside the rig (write it relative, as in %q)", RegeneratedFilesKey, name, "docs/lxx-align/gen.txt")
			}
			if _, err := path.Match(name, ""); err != nil {
				return fmt.Errorf("the paths of %s: %q is not a path or glob (%v)", RegeneratedFilesKey, name, err)
			}
		}
	}
	return nil
}

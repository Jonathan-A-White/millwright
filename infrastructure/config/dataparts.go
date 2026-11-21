package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DataPartsTable is the name of the tables of the config file that say which of a
// rig's files are its data and how that data is checked: `[data_parts.lampas]`,
// one table for a rig, DataPartsTablePrefix its name before the rig's.
const DataPartsTable = "data_parts"

// DataPartsTablePrefix begins the name of each such table.
const DataPartsTablePrefix = DataPartsTable + "."

// DataPart is a rig's data scope: Paths are globs of its data files (a file, a
// directory or a glob, `**` for any number of directories), and Command is the
// line that runs the data's own checks.
type DataPart struct {
	Paths   []string
	Command string
}

// DataParts reports each rig's data scope on this machine, by rig name, read from
// the `[data_parts.<rig>]` tables of ~/.config/mw/config.toml (mw-gq6.1589):
//
//	[data_parts.lampas]
//	paths = ["data/lxx/**", "docs/lxx-*.md"]
//	command = "npm ci && npx tsc --noEmit && npx vitest run tests/unit/lxx-*.test.ts"
//
// A story whose branch changes only files inside paths has mw check run command
// in place of the rig's gate, and mw next trusts a pass of it as it trusts the
// gate's parts. A rig with no such table, or one with no paths or no command, is
// gated whole as before, and a machine with none is not an error. The batch gate
// and the nightly suite never read it.
func DataParts() (map[string]DataPart, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	return dataPartsIn(filepath.Join(home, File))
}

// dataPartsIn reads the [data_parts.<rig>] tables of the config file at path. A
// paths array may run over several lines.
func dataPartsIn(path string) (map[string]DataPart, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]DataPart{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer file.Close()

	scopes := map[string]DataPart{}
	rig, inPaths, pathsText := "", false, ""
	finish := func() {
		scope := scopes[rig]
		scope.Paths = list(pathsText)
		scopes[rig] = scope
		inPaths, pathsText = false, ""
	}
	lines := bufio.NewScanner(file)
	lines.Buffer(nil, 1<<20)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if inPaths {
			line = withoutComment(line)
			pathsText += line
			if strings.Contains(line, "]") {
				finish()
			}
			continue
		}
		if strings.HasPrefix(line, "[") {
			table := strings.TrimSpace(strings.Trim(line, "[]"))
			rig = ""
			if name, ok := strings.CutPrefix(table, DataPartsTablePrefix); ok {
				rig = strings.Trim(strings.TrimSpace(name), `"'`)
			}
			continue
		}
		if rig == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch strings.Trim(strings.TrimSpace(key), `"'`) {
		case "command":
			scope := scopes[rig]
			scope.Command = unquote(value)
			scopes[rig] = scope
		case "paths":
			value = withoutComment(value)
			if strings.HasPrefix(value, "[") && !strings.Contains(value, "]") {
				inPaths, pathsText = true, value
			} else {
				pathsText = value
				finish()
			}
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	for rig, scope := range scopes {
		if len(scope.Paths) == 0 || scope.Command == "" {
			delete(scopes, rig)
		}
	}
	return scopes, nil
}

// withoutComment is a line of a config file up to a # that is not inside quotes.
func withoutComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
		case quote == 0 && c == '#':
			return strings.TrimSpace(line[:i])
		}
	}
	return strings.TrimSpace(line)
}

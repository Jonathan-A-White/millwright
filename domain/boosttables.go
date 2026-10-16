package domain

import (
	"fmt"
	"sort"
	"strings"
)

// BoostTableNames are the tables of the home's ~/.config/mw/config.toml that a
// Boost copies, in the order the vault's hosts/boost-config.toml holds them:
// each rig's tests command, and the command it runs on the end-to-end specs a
// story changed (mw-gq6.453, mw-gq6.456).
var BoostTableNames = []string{"tests", "changed_specs"}

// BoostTablesHeader opens the vault's hosts/boost-config.toml.
const BoostTablesHeader = `# The [tests] and [changed_specs] tables a Boost copies (contrib/boost-bootstrap.sh step 9).
# Written by mw config export-boost from the home's ~/.config/mw/config.toml: change a line
# there and run it again, never here. mw doctor's boost-config check says when they differ.
`

// BoostTables is each of BoostTableNames' lines, the command a rig's line
// holds by rig, by table.
type BoostTables map[string]map[string]string

// BoostTablesOf reads BoostTableNames out of a config file, and nothing else
// of it. A line whose value is not a string is refused, naming it: what a rig
// runs is a command line.
func BoostTablesOf(c *ConfigTOML) (BoostTables, error) {
	tables := BoostTables{}
	for _, name := range BoostTableNames {
		lines := map[string]string{}
		for rig, literal := range c.Table(name) {
			command, ok := TOMLString(literal)
			if !ok {
				return nil, fmt.Errorf("[%s] %s = %s is not a string", name, rig, literal)
			}
			lines[rig] = command
		}
		tables[name] = lines
	}
	return tables, nil
}

// Text is the tables as the vault's hosts/boost-config.toml holds them: the
// header, then each table, one `rig = "command"` line per rig in rig order,
// each command a one-line basic string, which is what the bootstrap's
// table_line copies whole into a Boost's config.toml.
func (b BoostTables) Text() string {
	var text strings.Builder
	text.WriteString(BoostTablesHeader)
	for _, name := range BoostTableNames {
		fmt.Fprintf(&text, "\n[%s]\n", name)
		for _, rig := range sortedKeys(b[name]) {
			key := rig
			if !tomlBareKey.MatchString(rig) {
				key = tomlBasicString(rig)
			}
			fmt.Fprintf(&text, "%s = %s\n", key, tomlBasicString(b[name][rig]))
		}
	}
	return text.String()
}

// BoostTablesDrift is every line of home's tables that vault does not hold
// alike, as "[table] rig: missing" (home has it, vault does not), "differs"
// or "not on the home" (vault has it, home does not), in table and then rig
// order; none when the two are level.
func BoostTablesDrift(home, vault BoostTables) []string {
	var drift []string
	for _, name := range BoostTableNames {
		rigs := map[string]bool{}
		for rig := range home[name] {
			rigs[rig] = true
		}
		for rig := range vault[name] {
			rigs[rig] = true
		}
		for _, rig := range sortedKeys(rigs) {
			want, onHome := home[name][rig]
			got, inVault := vault[name][rig]
			switch {
			case !inVault:
				drift = append(drift, fmt.Sprintf("[%s] %s: missing", name, rig))
			case !onHome:
				drift = append(drift, fmt.Sprintf("[%s] %s: not on the home", name, rig))
			case got != want:
				drift = append(drift, fmt.Sprintf("[%s] %s: differs", name, rig))
			}
		}
	}
	return drift
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

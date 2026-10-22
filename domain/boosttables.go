package domain

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// BoostTableNames are the one-table-for-all-rigs tables of the home's
// ~/.config/mw/config.toml that a Boost copies, in the order the vault's
// hosts/boost-config.toml holds them: each rig's tests command, and the command
// it runs on the end-to-end specs a story changed (mw-gq6.453, mw-gq6.456).
var BoostTableNames = []string{"tests", "changed_specs"}

// BoostPartsPrefix begins the name of the tables that split a rig's gate into
// parts, one table to a rig: [test_parts.lampas] (mw-gq6.567, mw-gq6.592). They
// are copied beside BoostTableNames, with the order of their lines kept, since
// `mw check --part N` counts the parts in that order.
const BoostPartsPrefix = "test_parts."

// BoostTablesHeader opens the vault's hosts/boost-config.toml.
const BoostTablesHeader = `# The [tests], [changed_specs] and [test_parts.<rig>] tables a Boost copies (contrib/boost-bootstrap.sh step 9).
# Written by mw config export-boost from the home's ~/.config/mw/config.toml: change a line
# there and run it again, never here. mw doctor's boost-config check says when they differ.
`

// BoostPart is one part of a rig's gate: the name and the command line of a
// line of its [test_parts.<rig>] table.
type BoostPart struct{ Name, Command string }

// BoostTables is what a Boost copies of a config file: Lines is each of
// BoostTableNames' lines, the command a rig's line holds by table and rig, and
// Parts is each rig's [test_parts.<rig>] lines, in the order the file has them.
type BoostTables struct {
	Lines map[string]map[string]string
	Parts map[string][]BoostPart
}

// BoostTablesOf reads BoostTableNames and the [test_parts.<rig>] tables out of
// a config file, and nothing else of it. A line whose value is not a string is
// refused, naming it: what a rig runs is a command line.
func BoostTablesOf(c *ConfigTOML) (BoostTables, error) {
	tables := BoostTables{Lines: map[string]map[string]string{}, Parts: map[string][]BoostPart{}}
	for _, name := range BoostTableNames {
		lines := map[string]string{}
		for rig, literal := range c.Table(name) {
			command, ok := TOMLString(literal)
			if !ok {
				return BoostTables{}, fmt.Errorf("[%s] %s = %s is not a string", name, rig, literal)
			}
			lines[rig] = command
		}
		tables.Lines[name] = lines
	}
	for _, name := range c.TableNames() {
		rig, ok := strings.CutPrefix(name, BoostPartsPrefix)
		if !ok || rig == "" {
			continue
		}
		for _, entry := range c.TableEntries(name) {
			command, ok := TOMLString(entry.Value)
			if !ok {
				return BoostTables{}, fmt.Errorf("[%s] %s = %s is not a string", name, entry.Key, entry.Value)
			}
			tables.Parts[rig] = append(tables.Parts[rig], BoostPart{Name: entry.Key, Command: command})
		}
	}
	return tables, nil
}

// Text is the tables as the vault's hosts/boost-config.toml holds them: the
// header, then each table, one `rig = "command"` line per rig in rig order,
// each command a one-line basic string, which is what the bootstrap's
// table_line copies whole into a Boost's config.toml; then each rig's
// [test_parts.<rig>] table in rig order, its lines in the order they were read,
// which the bootstrap's parts_table copies whole.
func (b BoostTables) Text() string {
	var text strings.Builder
	text.WriteString(BoostTablesHeader)
	for _, name := range BoostTableNames {
		fmt.Fprintf(&text, "\n[%s]\n", name)
		for _, rig := range sortedKeys(b.Lines[name]) {
			fmt.Fprintf(&text, "%s = %s\n", boostKey(rig), tomlBasicString(b.Lines[name][rig]))
		}
	}
	for _, rig := range sortedKeys(b.Parts) {
		if len(b.Parts[rig]) == 0 {
			continue
		}
		fmt.Fprintf(&text, "\n[%s%s]\n", BoostPartsPrefix, boostKey(rig))
		for _, part := range b.Parts[rig] {
			fmt.Fprintf(&text, "%s = %s\n", tomlBasicString(part.Name), tomlBasicString(part.Command))
		}
	}
	return text.String()
}

// boostKey is a rig as a TOML key: bare when it can be, else quoted.
func boostKey(rig string) string {
	if tomlBareKey.MatchString(rig) {
		return rig
	}
	return tomlBasicString(rig)
}

// BoostTablesDrift is every line of home's tables that vault does not hold
// alike, as "[table] rig: missing" (home has it, vault does not), "differs"
// or "not on the home" (vault has it, home does not), in table and then rig
// order, then the same for each rig's whole [test_parts.<rig>] table (its parts
// and their order); none when the two are level.
func BoostTablesDrift(home, vault BoostTables) []string {
	var drift []string
	for _, name := range BoostTableNames {
		rigs := map[string]bool{}
		for rig := range home.Lines[name] {
			rigs[rig] = true
		}
		for rig := range vault.Lines[name] {
			rigs[rig] = true
		}
		for _, rig := range sortedKeys(rigs) {
			want, onHome := home.Lines[name][rig]
			got, inVault := vault.Lines[name][rig]
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
	rigs := map[string]bool{}
	for rig := range home.Parts {
		rigs[rig] = true
	}
	for rig := range vault.Parts {
		rigs[rig] = true
	}
	for _, rig := range sortedKeys(rigs) {
		want, onHome := home.Parts[rig]
		got, inVault := vault.Parts[rig]
		table := BoostPartsPrefix + rig
		switch {
		case !inVault:
			drift = append(drift, fmt.Sprintf("[%s]: missing", table))
		case !onHome:
			drift = append(drift, fmt.Sprintf("[%s]: not on the home", table))
		case !slices.Equal(got, want):
			drift = append(drift, fmt.Sprintf("[%s]: differs", table))
		}
	}
	return drift
}

// BoostHeld is one line (or one whole [test_parts.<rig>] table) that a Boost
// holds differently from the vault's hosts/boost-config.toml: Key is "tests.rig"
// or "test_parts.rig", and Here and Vault are what each holds (a part table as
// its `"name" = "command"` lines, joined by "; ").
type BoostHeld struct{ Key, Here, Vault string }

// BoostTablesHeldDifferently is every line that both a Boost's tables and the
// vault's hold with different values, in table and then rig order, then each
// rig's [test_parts.<rig>] table when both hold it and its parts or their order
// differ (mw-gq6.704). A line only one of them holds is left out: sync appends
// what a Boost lacks (mw-gq6.593), and a Boost may hold a line the vault does
// not.
func BoostTablesHeldDifferently(here, vault BoostTables) []BoostHeld {
	var held []BoostHeld
	for _, name := range BoostTableNames {
		for _, rig := range sortedKeys(here.Lines[name]) {
			got := here.Lines[name][rig]
			if want, ok := vault.Lines[name][rig]; ok && got != want {
				held = append(held, BoostHeld{Key: name + "." + rig, Here: got, Vault: want})
			}
		}
	}
	for _, rig := range sortedKeys(here.Parts) {
		got := here.Parts[rig]
		if want, ok := vault.Parts[rig]; ok && !slices.Equal(got, want) {
			held = append(held, BoostHeld{Key: BoostPartsPrefix + rig, Here: partsText(got), Vault: partsText(want)})
		}
	}
	return held
}

func partsText(parts []BoostPart) string {
	lines := make([]string, len(parts))
	for i, part := range parts {
		lines[i] = tomlBasicString(part.Name) + " = " + tomlBasicString(part.Command)
	}
	return strings.Join(lines, "; ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// LevelBoostConfig brings a Boost's config file level with the vault's tables
// (mw-gq6.593): for each rig the config's [rigs] names, a [tests] or
// [changed_specs] line it lacks is appended to its table (the table too, when
// it lacks that), and a [test_parts.<rig>] table it lacks altogether is appended
// whole, with its parts in order. A line or table the config has is never edited
// or removed: one that differs from the vault's is only named, as
// "[tests] rig: differs" or "[test_parts.rig]: differs". out is the config's
// text with the additions, every other byte as it was; appended names each
// addition, as "[tests] rig" or "[test_parts.rig]".
func LevelBoostConfig(config string, vault BoostTables) (out string, appended, differing []string, err error) {
	file, err := ParseConfigTOML(config)
	if err != nil {
		return config, nil, nil, err
	}
	have, err := BoostTablesOf(file)
	if err != nil {
		return config, nil, nil, err
	}
	var rigs []string
	for rig := range file.Table("rigs") {
		rigs = append(rigs, rig)
	}
	sort.Strings(rigs)

	out = config
	for _, name := range BoostTableNames {
		for _, rig := range rigs {
			want, inVault := vault.Lines[name][rig]
			if !inVault {
				continue
			}
			got, has := have.Lines[name][rig]
			switch {
			case has && got != want:
				differing = append(differing, fmt.Sprintf("[%s] %s: differs", name, rig))
			case !has:
				// Re-read each time: the table may be the one just added.
				current, perr := ParseConfigTOML(out)
				if perr != nil {
					return config, nil, nil, perr
				}
				out = current.Set(name, boostKey(rig), tomlBasicString(want))
				appended = append(appended, fmt.Sprintf("[%s] %s", name, rig))
			}
		}
	}
	for _, rig := range rigs {
		want := vault.Parts[rig]
		if len(want) == 0 {
			continue
		}
		table := BoostPartsPrefix + boostKey(rig)
		if got, has := have.Parts[rig]; has {
			if !slices.Equal(got, want) {
				differing = append(differing, fmt.Sprintf("[%s%s]: differs", BoostPartsPrefix, rig))
			}
			continue
		}
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if strings.TrimSpace(out) != "" && !strings.HasSuffix(out, "\n\n") {
			out += "\n"
		}
		out += "[" + table + "]\n"
		for _, part := range want {
			out += tomlBasicString(part.Name) + " = " + tomlBasicString(part.Command) + "\n"
		}
		appended = append(appended, fmt.Sprintf("[%s%s]", BoostPartsPrefix, rig))
	}
	return out, appended, differing, nil
}

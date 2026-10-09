package vault

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// RigFileExt ends the name of a rig's file in the vault's rigs directory:
// rigs/<rig>.toml, which both hosts read because the vault is the one clone
// they share.
const RigFileExt = ".toml"

// The keys of a rig's file, all optional. The first two and version_files are
// lists of strings; guest is one quoted string, the owner of a rig that is not
// the Governor's.
const (
	EpicSectionsKey        = "epic_sections"
	EpicLastStoryLabelsKey = "epic_last_story_labels"
	GuestKey               = "guest"
	VersionFilesKey        = "version_files"
)

var _ application.EpicRules = (*Vault)(nil)

// EpicRequirements implements application.EpicRules: it reads
// rigs/<rig>.toml in the vault, which looks like this:
//
//	epic_sections          = ["Demo"]
//	epic_last_story_labels = ["demo"]
//	guest                  = "Luke"
//	version_files          = ["package.json", "package-lock.json"]
//
// A rig with no file asks nothing. A key this does not know is an error rather
// than a silence: a misspelt key would otherwise switch the requirement off
// without a word.
func (v *Vault) EpicRequirements(_ context.Context, rig string) (domain.EpicRequirements, error) {
	if err := safeName("rig", rig); err != nil {
		return domain.EpicRequirements{}, err
	}
	path := filepath.Join(v.dir, application.RigsDir, rig+RigFileExt)
	written, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return domain.EpicRequirements{}, nil
	case err != nil:
		return domain.EpicRequirements{}, fmt.Errorf("reading %s: %w", path, err)
	}
	requirements, err := parseRigFile(string(written))
	if err != nil {
		return domain.EpicRequirements{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return requirements, nil
}

// parseRigFile reads the little TOML a rig's file holds: `key = ["a", "b"]`,
// the list possibly running over several lines, `guest = "<owner>"`, and `#`
// comments.
func parseRigFile(written string) (domain.EpicRequirements, error) {
	var requirements domain.EpicRequirements
	lines := strings.Split(written, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(stripComment(lines[i]))
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if found && key == GuestKey {
			owner, err := quotedStrings(value)
			if err != nil || len(owner) != 1 {
				return requirements, fmt.Errorf("line %d: expected `%s = \"<owner>\"`, got %q", i+1, GuestKey, lines[i])
			}
			requirements.Guest = owner[0]
			continue
		}
		if !found || !strings.HasPrefix(value, "[") {
			return requirements, fmt.Errorf("line %d: expected `key = [\"name\", ...]`, got %q", i+1, lines[i])
		}
		for !strings.Contains(value, "]") {
			if i++; i >= len(lines) {
				return requirements, fmt.Errorf("the list for %s is never closed with ]", key)
			}
			value += " " + strings.TrimSpace(stripComment(lines[i]))
		}
		names, err := quotedStrings(value[1:strings.Index(value, "]")])
		if err != nil {
			return requirements, fmt.Errorf("the list for %s: %w", key, err)
		}
		switch key {
		case EpicSectionsKey:
			requirements.Sections = names
		case EpicLastStoryLabelsKey:
			requirements.LastStoryLabels = names
		case VersionFilesKey:
			for _, name := range names {
				if path.IsAbs(name) || name != path.Clean(name) || name == ".." || strings.HasPrefix(name, "../") {
					return requirements, fmt.Errorf("the list for %s: %q is not a path inside the rig (write it relative, as in %q)", key, name, "pwa/package.json")
				}
			}
			requirements.VersionFiles = names
		default:
			return requirements, fmt.Errorf("line %d: %q is not a key a rig's file has (it has %s, %s, %s and %s)",
				i+1, key, EpicSectionsKey, EpicLastStoryLabelsKey, GuestKey, VersionFilesKey)
		}
	}
	return requirements, nil
}

// stripComment cuts a trailing # comment off a line, leaving a # inside quotes.
func stripComment(line string) string {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#':
			return line[:i]
		}
	}
	return line
}

// quotedStrings reads `"a", 'b'` as the strings a and b. Anything between the
// commas that is not quoted is an error.
func quotedStrings(list string) ([]string, error) {
	var names []string
	for _, item := range splitOutsideQuotes(list) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if len(item) < 2 || item[0] != item[len(item)-1] || (item[0] != '"' && item[0] != '\'') {
			return nil, fmt.Errorf("%q is not a quoted string", item)
		}
		if name := strings.TrimSpace(item[1 : len(item)-1]); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// splitOutsideQuotes splits on the commas that are not inside quotes.
func splitOutsideQuotes(list string) []string {
	var items []string
	var quote rune
	start := 0
	for i, r := range list {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == ',':
			items = append(items, list[start:i])
			start = i + 1
		}
	}
	return append(items, list[start:])
}

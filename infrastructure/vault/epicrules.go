package vault

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	ChangelogFilesKey      = "changelog_files"
	UnionMergeFilesKey     = "union_merge_files"
	TimingTestsKey         = "timing_tests"
	PublicURLKey           = "public_url"
	SiteURLKey             = "site_url"
	HealthzURLKey          = "healthz_url"
	FilesExpectedKey       = "files_expected"
	NightlySuiteKey        = "nightly_suite"
	SuiteEpicKey           = "suite_epic"

	// The landing keys: how a rig's stories land (plans/0117). A value of these
	// that makes no sense reads as its default with a warning, never an error.
	LandingKey          = "landing"
	BatchEveryKey       = "batch_every"
	BatchMinKey         = "batch_min"
	BatchMaxKey         = "batch_max"
	BatchFixerModelKey  = "batch_fixer_model"
	BatchFixerEffortKey = "batch_fixer_effort"
	BatchFixerTurnsKey  = "batch_fixer_turns"
)

var _ application.EpicRules = (*Vault)(nil)
var _ application.LandingRules = (*Vault)(nil)

// beadID is what a suite_epic may be: a bead's id, as in mw-5r3p30.
var beadID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// EpicRequirements implements application.EpicRules: it reads
// rigs/<rig>.toml in the vault, which looks like this:
//
//	epic_sections          = ["Demo"]
//	epic_last_story_labels = ["demo"]
//	guest                  = "Luke"
//	version_files          = ["package.json", "package-lock.json"]
//	changelog_files        = ["public/changelog.json", "CHANGELOG.md"]
//	union_merge_files      = ["ATTRIBUTION.md", "docs/*.md"]
//	timing_tests           = ["tests/timing/**", "**/*.timing.test.ts"]
//	public_url             = "https://github.com/Jonathan-A-White/lampas"
//	site_url               = "https://lampas.allmymind.org/"
//	healthz_url            = "https://jonah.allmymind.org/api/leaderboard/healthz"
//	files_expected         = true
//	nightly_suite          = true
//	suite_epic             = "mw-5r3p30"
//	landing                = "batch"   # or "each", the default
//	batch_every            = "30m"
//	batch_min              = 2
//	batch_max              = 6
//	batch_fixer_model      = "sonnet"
//	batch_fixer_effort     = "high"
//	batch_fixer_turns      = 60
//
// A rig with no file asks nothing. A key this does not know is an error rather
// than a silence: a misspelt key would otherwise switch the requirement off
// without a word.
func (v *Vault) EpicRequirements(ctx context.Context, rig string) (domain.EpicRequirements, error) {
	requirements, _, err := v.readRigFile(ctx, rig, false)
	return requirements, err
}

// LandingRequirements implements application.LandingRules: the same file, read
// for a landing, which a key this build does not know must not stop (it races
// every older mw still running against a key set in the vault after it was
// built). The unknown keys come back beside what the known ones say; a
// malformed value of a key it does know is an error as in EpicRequirements.
func (v *Vault) LandingRequirements(ctx context.Context, rig string) (domain.EpicRequirements, []string, error) {
	return v.readRigFile(ctx, rig, true)
}

func (v *Vault) readRigFile(_ context.Context, rig string, lenient bool) (domain.EpicRequirements, []string, error) {
	if err := safeName("rig", rig); err != nil {
		return domain.EpicRequirements{}, nil, err
	}
	path := filepath.Join(v.dir, application.RigsDir, rig+RigFileExt)
	written, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return domain.EpicRequirements{}, nil, nil
	case err != nil:
		return domain.EpicRequirements{}, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	requirements, unknown, err := parseRigFile(string(written), lenient)
	if err != nil {
		return domain.EpicRequirements{}, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return requirements, unknown, nil
}

// knownKeys are the keys a rig's file has.
var knownKeys = map[string]bool{
	EpicSectionsKey: true, EpicLastStoryLabelsKey: true, GuestKey: true, VersionFilesKey: true, ChangelogFilesKey: true, UnionMergeFilesKey: true, TimingTestsKey: true,
	PublicURLKey: true, SiteURLKey: true, HealthzURLKey: true, FilesExpectedKey: true, NightlySuiteKey: true, SuiteEpicKey: true,
	LandingKey: true, BatchEveryKey: true, BatchMinKey: true, BatchMaxKey: true,
	BatchFixerModelKey: true, BatchFixerEffortKey: true, BatchFixerTurnsKey: true,
}

// bareKey is what a key may be for a lenient read to set it aside as one it
// does not know rather than as a line that is no `key = value` at all.
var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parseRigFile reads the little TOML a rig's file holds: `key = ["a", "b"]`,
// the list possibly running over several lines, `guest = "<owner>"`,
// `files_expected = true` (or false), `nightly_suite = true` (or false),
// `suite_epic = "<bead id>"`, and `#` comments. A lenient read sets a key it
// does not know aside, whatever its value and however many lines the value
// runs over, and names it in the second result; a strict one refuses it.
func parseRigFile(written string, lenient bool) (domain.EpicRequirements, []string, error) {
	var requirements domain.EpicRequirements
	var unknown []string
	lines := strings.Split(written, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(stripComment(lines[i]))
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if lenient && found && !knownKeys[key] && bareKey.MatchString(key) {
			unknown = append(unknown, key)
			for strings.HasPrefix(value, "[") && !strings.Contains(value, "]") && i+1 < len(lines) {
				i++
				value += " " + strings.TrimSpace(stripComment(lines[i]))
			}
			continue
		}
		if found && landingKeys[key] {
			if warning := setLandingKey(&requirements.Landing, key, value); warning != "" {
				requirements.Warnings = append(requirements.Warnings, fmt.Sprintf("line %d: %s", i+1, warning))
			}
			continue
		}
		if found && key == GuestKey {
			owner, err := quotedStrings(value)
			if err != nil || len(owner) != 1 {
				return requirements, nil, fmt.Errorf("line %d: expected `%s = \"<owner>\"`, got %q", i+1, GuestKey, lines[i])
			}
			requirements.Guest = owner[0]
			continue
		}
		if found && key == FilesExpectedKey {
			switch value {
			case "true", "false":
				requirements.FilesExpected = value == "true"
			default:
				return requirements, nil, fmt.Errorf("line %d: expected `%s = true` or `%s = false`, got %q", i+1, FilesExpectedKey, FilesExpectedKey, lines[i])
			}
			continue
		}
		if found && key == NightlySuiteKey {
			switch value {
			case "true", "false":
				requirements.NightlySuite = value == "true"
			default:
				return requirements, nil, fmt.Errorf("line %d: expected `%s = true` or `%s = false`, got %q", i+1, NightlySuiteKey, NightlySuiteKey, lines[i])
			}
			continue
		}
		if found && key == SuiteEpicKey {
			epic, err := quotedStrings(value)
			if err != nil || len(epic) != 1 || !beadID.MatchString(epic[0]) {
				return requirements, nil, fmt.Errorf("line %d: expected `%s = \"<bead id>\"`, got %q", i+1, SuiteEpicKey, lines[i])
			}
			requirements.SuiteEpic = epic[0]
			continue
		}
		if found && key == PublicURLKey {
			url, err := quotedStrings(value)
			if err != nil || len(url) != 1 || !strings.HasPrefix(url[0], "https://") {
				return requirements, nil, fmt.Errorf("line %d: expected `%s = \"https://github.com/<owner>/<rig>\"`, got %q", i+1, PublicURLKey, lines[i])
			}
			requirements.PublicURL = strings.TrimRight(url[0], "/")
			continue
		}
		if found && (key == SiteURLKey || key == HealthzURLKey) {
			url, err := quotedStrings(value)
			if err != nil || len(url) != 1 || !strings.HasPrefix(url[0], "http") || !strings.Contains(url[0], "://") {
				return requirements, nil, fmt.Errorf("line %d: expected `%s = \"https://<host>/...\"`, got %q", i+1, key, lines[i])
			}
			if key == SiteURLKey {
				requirements.SiteURL = url[0]
			} else {
				requirements.HealthzURL = url[0]
			}
			continue
		}
		if !found || !strings.HasPrefix(value, "[") {
			return requirements, nil, fmt.Errorf("line %d: expected `key = [\"name\", ...]`, got %q", i+1, lines[i])
		}
		for !strings.Contains(value, "]") {
			if i++; i >= len(lines) {
				return requirements, nil, fmt.Errorf("the list for %s is never closed with ]", key)
			}
			value += " " + strings.TrimSpace(stripComment(lines[i]))
		}
		names, err := quotedStrings(value[1:strings.Index(value, "]")])
		if err != nil {
			return requirements, nil, fmt.Errorf("the list for %s: %w", key, err)
		}
		switch key {
		case EpicSectionsKey:
			requirements.Sections = names
		case EpicLastStoryLabelsKey:
			requirements.LastStoryLabels = names
		case VersionFilesKey, ChangelogFilesKey, UnionMergeFilesKey, TimingTestsKey:
			for _, name := range names {
				if path.IsAbs(name) || name != path.Clean(name) || name == ".." || strings.HasPrefix(name, "../") {
					return requirements, nil, fmt.Errorf("the list for %s: %q is not a path inside the rig (write it relative, as in %q)", key, name, "pwa/package.json")
				}
				if ext := path.Ext(name); key == ChangelogFilesKey && ext != ".json" && ext != ".md" {
					return requirements, nil, fmt.Errorf("the list for %s: %q is neither a .json nor a .md file", key, name)
				}
			}
			switch key {
			case ChangelogFilesKey:
				requirements.ChangelogFiles = names
			case UnionMergeFilesKey, TimingTestsKey:
				for _, name := range names {
					if _, err := path.Match(name, ""); err != nil {
						return requirements, nil, fmt.Errorf("the list for %s: %q is not a path or glob (%v)", key, name, err)
					}
				}
				if key == TimingTestsKey {
					requirements.TimingTests = names
				} else {
					requirements.UnionMergeFiles = names
				}
			default:
				requirements.VersionFiles = names
			}
		default:
			return requirements, nil, fmt.Errorf("line %d: %q is not a key a rig's file has (it has %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s and the landing keys %s, %s, %s, %s, %s, %s and %s)",
				i+1, key, EpicSectionsKey, EpicLastStoryLabelsKey, GuestKey, VersionFilesKey, ChangelogFilesKey, UnionMergeFilesKey, TimingTestsKey, PublicURLKey, SiteURLKey, HealthzURLKey, FilesExpectedKey, NightlySuiteKey, SuiteEpicKey,
				LandingKey, BatchEveryKey, BatchMinKey, BatchMaxKey, BatchFixerModelKey, BatchFixerEffortKey, BatchFixerTurnsKey)
		}
	}
	return requirements, unknown, nil
}

// landingKeys are the keys of a rig's file that say how its stories land.
var landingKeys = map[string]bool{
	LandingKey: true, BatchEveryKey: true, BatchMinKey: true, BatchMaxKey: true,
	BatchFixerModelKey: true, BatchFixerEffortKey: true, BatchFixerTurnsKey: true,
}

// setLandingKey reads the value of one landing key into s. A value that makes no
// sense leaves the key unset, so that it reads as its default, and comes back
// as a warning: an older or newer mw must never stop a landing over this file.
func setLandingKey(s *domain.LandingSettings, key, value string) string {
	bad := func(want string) string {
		return fmt.Sprintf("%s = %s is not %s; read as the default", key, value, want)
	}
	switch key {
	case LandingKey:
		mode, err := quotedStrings(value)
		if err == nil && len(mode) == 1 && mode[0] == string(domain.LandingBatch) {
			s.Mode = domain.LandingBatch
			return ""
		}
		if err == nil && len(mode) == 1 && mode[0] == string(domain.LandingEach) {
			s.Mode = domain.LandingEach
			return ""
		}
		s.Mode = domain.LandingEach
		return bad(`"each" or "batch"`) + ` ("each")`
	case BatchEveryKey:
		text, err := quotedStrings(value)
		if err == nil && len(text) == 1 {
			if d, err := time.ParseDuration(text[0]); err == nil && d > 0 {
				s.BatchEvery = d
				return ""
			}
		}
		return bad(`a duration such as "30m"`)
	case BatchMinKey, BatchMaxKey, BatchFixerTurnsKey:
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return bad("a whole number above zero")
		}
		switch key {
		case BatchMinKey:
			s.BatchMin = n
		case BatchMaxKey:
			s.BatchMax = n
		default:
			s.FixerTurns = n
		}
	default: // BatchFixerModelKey, BatchFixerEffortKey
		text, err := quotedStrings(value)
		if err != nil || len(text) != 1 {
			return bad("one quoted word")
		}
		if key == BatchFixerModelKey {
			s.FixerModel = text[0]
		} else {
			s.FixerEffort = text[0]
		}
	}
	return ""
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

// TimingTests reads the timing_tests of every rig that has a file in the vault's
// rigs directory, by rig name; a rig that names none is left out. It is how the
// gate, mw suite and a Builder's session learn which tests to leave out
// (application.TimingEnv). A file that cannot be read is left out too, so that
// one rig's bad file cannot stop another rig's gate: that rig's own landing
// refuses the file, and until then its tests run in full.
func (v *Vault) TimingTests(ctx context.Context) map[string][]string {
	timing := map[string][]string{}
	entries, err := os.ReadDir(filepath.Join(v.dir, application.RigsDir))
	if err != nil {
		return timing
	}
	for _, entry := range entries {
		rig, isRigFile := strings.CutSuffix(entry.Name(), RigFileExt)
		if !isRigFile || entry.IsDir() {
			continue
		}
		requirements, _, err := v.LandingRequirements(ctx, rig)
		if err != nil || len(requirements.TimingTests) == 0 {
			continue
		}
		timing[rig] = requirements.TimingTests
	}
	return timing
}

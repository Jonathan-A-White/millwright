package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Jonathan-A-White/millwright/domain"
)

// fuelKnobsDoc is the page of the dials between a Max month and a Pro month
// (mw-gq6.431), read from this package's directory.
const fuelKnobsDoc = "../../docs/fuel-knobs.md"

func readFuelKnobs(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(fuelKnobsDoc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestTheFuelKnobsPageNamesEveryDialTheStoryAsksFor(t *testing.T) {
	doc := readFuelKnobs(t)
	for _, want := range []string{
		"`cap`", "`builder_jobs`", "`[[dispatch.windows]] cap`", "`[[dispatch.windows]] builder_jobs`",
		"`mw path set <story> model=<model> effort=<effort>`",
		"`[grist] daily_limit`", "`grinds/<kind>.json maxTurns`", "`grinds/<kind>.json model`",
		"## Seats that can be parked", "Ideas only", "`mw ideas-only`",
		"laptop", "desktop", "cloud3",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/fuel-knobs.md does not name %s", want)
		}
	}
}

// TestEveryDialOnTheFuelKnobsPageSaysTheCheaperWayAndWhatIsLost holds every
// row of every dial table to five filled columns: the dial, what turns it, its
// value now on each host, the cheaper way and what that loses.
func TestEveryDialOnTheFuelKnobsPageSaysTheCheaperWayAndWhatIsLost(t *testing.T) {
	rows := 0
	for _, line := range strings.Split(readFuelKnobs(t), "\n") {
		if !strings.HasPrefix(line, "|") || strings.HasPrefix(line, "|-") || strings.HasPrefix(line, "| Dial ") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 5 {
			t.Errorf("a dial row has %d columns, not 5 (dial, turned by, now, cheaper, lost): %s", len(cells), line)
			continue
		}
		for i, name := range []string{"dial", "turned by", "now", "cheaper", "lost"} {
			if strings.TrimSpace(cells[i]) == "" {
				t.Errorf("a dial row says nothing under %q: %s", name, line)
			}
		}
		if !strings.Contains(cells[1], "`") {
			t.Errorf("a dial row names no command or key that turns it: %s", line)
		}
		rows++
	}
	if rows < 15 {
		t.Errorf("the page has %d dial rows: fewer than the story's list holds", rows)
	}
}

// TestEveryCommandAndKeyOnTheFuelKnobsPageIsInTheCode checks each
// backticked span of the page against the code: an mw command against the
// command tree (its flags and a config or path key too), a config key against
// the keys mw reads, a unit against contrib/systemd.
func TestEveryCommandAndKeyOnTheFuelKnobsPageIsInTheCode(t *testing.T) {
	code := sourceOfMw(t)
	spans := regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(readFuelKnobs(t), -1)
	if len(spans) == 0 {
		t.Fatal("docs/fuel-knobs.md names no command or key at all")
	}
	for _, span := range spans {
		if err := checkFuelKnob(span[1], code); err != nil {
			t.Errorf("docs/fuel-knobs.md names `%s`: %v", span[1], err)
		}
	}
}

// TestTheFuelKnobCheckRefusesWhatIsNotInTheCode keeps the check above from
// passing everything.
func TestTheFuelKnobCheckRefusesWhatIsNotInTheCode(t *testing.T) {
	code := sourceOfMw(t)
	for _, span := range []string{
		"mw no-such-command", "mw config set no_such_root_key 1", "mw config set grist.no_such_key 1",
		"mw path set <story> no_such_field=1", "mw path set <story> model=gpt", "mw status --no-such-flag",
		"mw doctor no-such-check",
		"[grist] no_such_key", "[no_such_table] cap", "no_such_root_key", "MW_NO_SUCH_SETTING",
		"systemctl --user disable --now mw-no-such.timer", "grinds/<kind>.json noSuchField",
		"rigs/<rig>.toml no_such_rule", "a phrase the check does not know",
	} {
		if checkFuelKnob(span, code) == nil {
			t.Errorf("the check takes `%s`, which is not in the code", span)
		}
	}
}

// sourceOfMw is every non-test Go file of mw, read together, for a key to be
// grepped in.
func sourceOfMw(t *testing.T) string {
	t.Helper()
	var all strings.Builder
	for _, dir := range []string{"../../application", "../../domain", "../../infrastructure", "."} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			raw, err := os.ReadFile(path)
			all.Write(raw)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return all.String()
}

// checkFuelKnob says why span, a command or key the page names, is not in the
// code, or nil when it is.
func checkFuelKnob(span, code string) error {
	quoted := func(s string) bool { return strings.Contains(code, `"`+s+`"`) }
	words := strings.Fields(span)
	switch {
	case len(words) == 0:
		return fmt.Errorf("it is empty")
	case words[0] == "mw":
		return checkMwCommand(words[1:], code)
	case len(words) == 4 && strings.Join(words[:3], " ") == "systemctl --user disable":
		return fmt.Errorf("say disable --now, which also stops it")
	case len(words) == 5 && strings.Join(words[:4], " ") == "systemctl --user disable --now":
		if _, err := os.Stat(filepath.Join("../../contrib/systemd", words[4])); err != nil {
			return fmt.Errorf("contrib/systemd has no unit %s", words[4])
		}
		return nil
	case len(words) == 2 && words[0] == "grinds/<kind>.json":
		if !strings.Contains(code, `json:"`+words[1]+`"`) {
			return fmt.Errorf("no grind file field is %s", words[1])
		}
		return nil
	case len(words) == 2 && words[0] == "rigs/<rig>.toml":
		if !quoted(words[1]) {
			return fmt.Errorf("mw reads no rig rule %s", words[1])
		}
		return nil
	case len(words) == 2 && strings.HasPrefix(words[0], "["):
		table := strings.Trim(words[0], "[]")
		if !quoted(table) {
			return fmt.Errorf("mw reads no [%s] table", table)
		}
		if words[1] == "<host>" || words[1] == "<rig>" {
			return nil
		}
		if !quoted(words[1]) {
			return fmt.Errorf("mw reads no key %s", words[1])
		}
		return nil
	case len(words) == 1 && strings.HasPrefix(span, "MW_"):
		if !quoted(span) {
			return fmt.Errorf("mw reads no %s", span)
		}
		return nil
	case len(words) == 1 && regexp.MustCompile(`^[a-z_]+$`).MatchString(span):
		if !slices.Contains(domain.KnownRootConfigKeys, span) {
			return fmt.Errorf("it is not a root key mw reads (domain.KnownRootConfigKeys)")
		}
		return nil
	}
	return fmt.Errorf("the check does not know this form: name an mw command, a config key, an MW_ setting, a grind field, a rig rule or a unit")
}

// checkMwCommand walks the command tree by args, then holds what is left to
// the command's flags and, for mw config and mw path, to the keys they take.
func checkMwCommand(args []string, code string) error {
	cmd := newRootCmd()
	rest := args
	for len(rest) > 0 {
		var next *cobra.Command
		for _, child := range cmd.Commands() {
			if child.Name() == rest[0] || slices.Contains(child.Aliases, rest[0]) {
				next = child
			}
		}
		if next == nil {
			break
		}
		cmd, rest = next, rest[1:]
	}
	if cmd.Parent() == nil {
		return fmt.Errorf("mw has no command %q", strings.Join(args, " "))
	}
	for _, word := range rest {
		if name, ok := strings.CutPrefix(word, "--"); ok {
			name, _, _ = strings.Cut(name, "=")
			if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
				return fmt.Errorf("%s has no flag --%s", cmd.CommandPath(), name)
			}
		}
	}
	switch cmd.CommandPath() {
	case "mw config set", "mw config get":
		if len(rest) == 0 {
			return fmt.Errorf("%s names no key", cmd.CommandPath())
		}
		table, key, err := domain.SplitConfigKey(rest[0])
		if err != nil {
			return err
		}
		if table == "" {
			return nil
		}
		if !strings.Contains(code, `"`+table+`"`) || !strings.Contains(code, `"`+key+`"`) {
			return fmt.Errorf("mw reads no [%s] %s", table, key)
		}
	case "mw path set":
		for _, word := range rest {
			field, value, ok := strings.Cut(word, "=")
			if !ok {
				continue
			}
			if !slices.Contains(domain.Fields, field) {
				return fmt.Errorf("a path has no key %s", field)
			}
			if field == "model" && !strings.HasPrefix(value, "<") && !domain.KnownModel(domain.Model(value)) {
				return fmt.Errorf("the factory runs on no model %s", value)
			}
			if field == "effort" && !strings.HasPrefix(value, "<") && !domain.KnownEffort(domain.Effort(value)) {
				return fmt.Errorf("a session takes no effort %s", value)
			}
		}
	default:
		// A word left after the command is an argument it takes by name, such
		// as a doctor check: the code names it too.
		for _, word := range rest {
			if strings.HasPrefix(word, "-") || strings.HasPrefix(word, "<") {
				continue
			}
			if !strings.Contains(code, `"`+word+`"`) {
				return fmt.Errorf("%s takes no %q that the code names", cmd.CommandPath(), word)
			}
		}
	}
	return nil
}

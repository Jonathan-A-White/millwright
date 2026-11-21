package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// A finding that says `fix: <command>` promises the person that the command
// mends it, and the check's Cure is meant to run it for them. Three times
// (mw-gq6.1253, .1254, .1274) a finding named a command its Cure did not run, so
// a person ran it. These tests are the shared check on that:
//
//   - TestEveryFixAFindingNamesIsRunByTheCure runs, for each case in
//     curesThatRunTheirFix, the check's probe and cure, and fails when the
//     finding names a `fix: <command>` the cure did not run, unless the case is
//     marked hand-only and its finding says a person does it.
//   - TestEveryDoctorFileThatNamesAFixHasACase scans this package's source for
//     `fix: ` and fails on a file with no case, so a new check that names a
//     command cannot land without one.
//   - TestTheFixCheckFailsOnACureThatDoesNotRunTheFix is the test of the test.

// fixCase is one check, probed into a faulty finding, and how to tell what its
// cure did.
type fixCase struct {
	name  string
	check application.DoctorCheck
	// handOnly marks a check whose cure is a person's: the finding must say so
	// ("no cure" or "a person") and the cure must fail.
	handOnly bool
	// namesNoCommand marks a finding that names no `fix:` command at all (the
	// [test_parts.<rig>] table), whose cure must then fail saying a person does it.
	namesNoCommand bool
	// watch is called after the probe and before the cure, and returns the judge
	// called after it: whether the cure ran the command, given as shell words.
	// A command the judge does not know is not run.
	watch func(t *testing.T) func(words []string) bool
}

// fixCommands is every command a finding names after "fix: ", each cut at the
// first ';' or ')' outside quotes, or at the end, and split into shell words.
func fixCommands(finding string) [][]string {
	const marker = "fix: "
	var commands [][]string
	rest := finding
	for {
		at := strings.Index(rest, marker)
		if at < 0 {
			return commands
		}
		rest = rest[at+len(marker):]
		end := commandEnd(rest)
		commands = append(commands, shellWords(rest[:end]))
		rest = rest[end:]
	}
}

// commandEnd is where the command at the head of s ends.
func commandEnd(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ';' || c == ')':
			return i
		}
	}
	return len(s)
}

// shellWords splits a command the way a shell would: single quotes are
// literal, double quotes are Go-quoted (the finding writes them with %q).
func shellWords(command string) []string {
	var words []string
	var word strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == ' ' || c == '\t':
			flush()
		case c == '\'':
			inWord = true
			end := strings.IndexByte(command[i+1:], '\'')
			if end < 0 {
				end = len(command) - i - 1
			}
			word.WriteString(command[i+1 : i+1+end])
			i += end + 1
		case c == '"':
			inWord = true
			end := commandEnd(command[i:])
			if unquoted, err := strconv.Unquote(command[i:min(i+end, len(command))]); err == nil {
				word.WriteString(unquoted)
			} else {
				word.WriteString(command[i:min(i+end, len(command))])
			}
			i += end - 1
		default:
			inWord = true
			word.WriteByte(c)
		}
	}
	flush()
	return words
}

// cureProblems probes the case's check, runs its cure, and says what is wrong:
// a finding that names a command the cure did not run, or a hand-only check
// that does not say so. No problems means the finding keeps its promise.
func cureProblems(ctx context.Context, t *testing.T, c fixCase) []string {
	t.Helper()
	verdict, finding := c.check.Probe(ctx)
	if verdict != application.DoctorFaulty {
		return []string{fmt.Sprintf("the case's probe is %s, not faulty (%s), so it tests nothing", verdict, finding)}
	}
	fixes := fixCommands(finding)
	var ran func([]string) bool
	if c.watch != nil {
		ran = c.watch(t)
	}
	cureErr := c.check.Cure(ctx)
	says := strings.Contains(finding, "no cure") || strings.Contains(finding, "a person") ||
		(cureErr != nil && (strings.Contains(cureErr.Error(), "no cure") || strings.Contains(cureErr.Error(), "a person")))

	var problems []string
	switch {
	case c.handOnly:
		if !strings.Contains(finding, "no cure") && !strings.Contains(finding, "a person") {
			problems = append(problems, fmt.Sprintf("marked hand-only, but the finding does not say a person does it: %q", finding))
		}
		if cureErr == nil {
			problems = append(problems, "marked hand-only, but the cure ran without error")
		}
	case c.namesNoCommand:
		if len(fixes) > 0 {
			problems = append(problems, fmt.Sprintf("expected no `fix:` command in the finding, got %q", finding))
		}
		if cureErr == nil || !says {
			problems = append(problems, fmt.Sprintf("the finding names no command, so its cure must fail saying a person does it, got %v", cureErr))
		}
	case len(fixes) == 0:
		problems = append(problems, fmt.Sprintf("the finding names no `fix:` command, so the case tests nothing: %q", finding))
	default:
		for _, words := range fixes {
			if ran == nil || !ran(words) {
				problems = append(problems, fmt.Sprintf("the finding says `fix: %s` but the cure did not run it (cure error: %v)", strings.Join(words, " "), cureErr))
			}
		}
	}
	return problems
}

// configSetRan is the judge for a check whose cure runs `mw config set`: the
// config file, read after the cure, holds the value the command sets.
func configSetRan(config application.ConfigFile) func(t *testing.T) func([]string) bool {
	return func(t *testing.T) func([]string) bool {
		return func(words []string) bool {
			if len(words) != 5 || strings.Join(words[:3], " ") != "mw config set" {
				return false
			}
			data, _, err := config.Read()
			if err != nil {
				t.Fatalf("reading the config after the cure: %v", err)
			}
			parsed, err := domain.ParseConfigTOML(string(data))
			if err != nil {
				t.Fatalf("the config after the cure does not parse: %v", err)
			}
			table, key, err := domain.SplitConfigKey(words[3])
			if err != nil {
				return false
			}
			want, err := domain.TOMLLiteral(words[4])
			if err != nil {
				return false
			}
			have, found := parsed.Lookup(table, key)
			return found && domain.SameTOMLValue(have, want)
		}
	}
}

// systemctlRan is the judge for a check whose cure runs systemctl daemon-reload,
// reading the stand-in systemctl's call log: the command's own arguments must
// be one whole line of it.
func systemctlRan(callLog string) func(t *testing.T) func([]string) bool {
	return func(t *testing.T) func([]string) bool {
		return func(words []string) bool {
			if len(words) < 2 || words[0] != "systemctl" || words[len(words)-1] != "daemon-reload" {
				return false
			}
			data, err := os.ReadFile(callLog)
			if err != nil {
				t.Fatalf("reading the call log: %v", err)
			}
			for _, line := range strings.Split(string(data), "\n") {
				if line == strings.Join(words[1:], " ") {
					return true
				}
			}
			return false
		}
	}
}

func curesThatRunTheirFix(t *testing.T) []fixCase {
	t.Helper()
	var cases []fixCase

	boost := map[string]struct{ here, vault string }{
		"boost-config on a Boost, a [tests] line":  {"[tests]\nmillwright = \"make test\"\n", "[tests]\nmillwright = \"make test && scripts/check-codemap.sh\"\n"},
		"boost-config on a Boost, [changed_specs]": {"[changed_specs]\nargus = \"npx playwright test\"\n", "[changed_specs]\nargus = \"npx playwright test {specs}\"\n"},
		"boost-config on a Boost, [named_tests]":   {"[named_tests]\npostern = \"npm test -- -t {tests}\"\n", "[named_tests]\npostern = \"npm test -- --grep {tests}\"\n"},
		"boost-config on a Boost, [session] deny":  {"[session]\ndeny = [\"Bash(scp *)\"]\n", "[session]\ndeny = [\"Bash(scp *)\", \"Read(~/.ssh/**)\"]\n"},
	}
	names := make([]string, 0, len(boost))
	for name := range boost {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		check := boostConfigOnABoost(t, boost[name].here, boost[name].vault)
		cases = append(cases, fixCase{name: name, check: check, watch: configSetRan(check.Config)})
	}

	// A [test_parts.<rig>] table cannot be written by mw config set: the finding
	// names no command, and the cure says a person replaces it.
	parts := boostConfigOnABoost(t, "[test_parts.argus]\n\"unit\" = \"npm test\"\n", "[test_parts.argus]\n\"unit\" = \"npm run unit\"\n")
	cases = append(cases, fixCase{name: "boost-config on a Boost, a [test_parts] table", check: parts, namesNoCommand: true})

	userProgram, userLog := listingSystemctl(t, map[string]string{"mw-postern-mirror.service": "yes"})
	cases = append(cases, fixCase{
		name:  "daemon-reload on a user host",
		check: &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: userProgram},
		watch: systemctlRan(userLog),
	})
	rootProgram, rootLog := rootHostSystemctl(t, map[string]string{"mw-seat-tmux.service": "yes"})
	cases = append(cases, fixCase{
		name:  "daemon-reload on a root host",
		check: &doctor.DaemonReload{Units: []string{"mw-dispatch.service"}, Program: rootProgram, Root: func() bool { return true }},
		watch: systemctlRan(rootLog),
	})
	return cases
}

func TestEveryFixAFindingNamesIsRunByTheCure(t *testing.T) {
	for _, c := range curesThatRunTheirFix(t) {
		t.Run(c.name, func(t *testing.T) {
			for _, problem := range cureProblems(context.Background(), t, c) {
				t.Error(problem)
			}
		})
	}
}

// filesThatNameAFix are the files of this package whose findings say `fix: `,
// each with the cases above that hold its checks to their word.
var filesThatNameAFix = map[string][]string{
	"boostconfig.go":  {"boost-config on a Boost, a [tests] line"},
	"daemonreload.go": {"daemon-reload on a user host", "daemon-reload on a root host"},
}

func TestEveryDoctorFileThatNamesAFixHasACase(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `fix: `) {
			found[source] = true
		}
	}
	cases := map[string]bool{}
	for _, c := range curesThatRunTheirFix(t) {
		cases[c.name] = true
	}
	for source := range found {
		named, ok := filesThatNameAFix[source]
		if !ok {
			t.Errorf("%s has a finding that says `fix: <command>` and no case in curesThatRunTheirFix: add the check there so its cure is held to that command, or say in the finding that no cure runs it (\"no cure: a person runs ...\") and add it hand-only", source)
			continue
		}
		for _, name := range named {
			if !cases[name] {
				t.Errorf("filesThatNameAFix lists the case %q for %s, and curesThatRunTheirFix has none by that name", name, source)
			}
		}
	}
	for source := range filesThatNameAFix {
		if !found[source] {
			t.Errorf("filesThatNameAFix lists %s, which says no `fix: ` any longer: drop it", source)
		}
	}
}

// standInCheck is a check whose finding names a command and whose cure runs
// nothing.
type standInCheck struct{ finding string }

func (s *standInCheck) Name() string { return "stand-in" }
func (s *standInCheck) Probe(context.Context) (application.Verdict, string) {
	return application.DoctorFaulty, s.finding
}
func (s *standInCheck) Cure(context.Context) error   { return nil }
func (s *standInCheck) Damper() (time.Duration, int) { return 0, 1 }
func (s *standInCheck) WayBack() string              { return "none" }

func TestTheFixCheckFailsOnACureThatDoesNotRunTheFix(t *testing.T) {
	ctx := context.Background()
	check := &standInCheck{finding: "the frobnicator is stale (fix: mw frobnicate --now 'a b'); also fix: mw polish"}
	ranNothing := func(*testing.T) func([]string) bool { return func([]string) bool { return false } }
	ranFrobnicate := func(*testing.T) func([]string) bool {
		return func(words []string) bool { return strings.Join(words, "|") == "mw|frobnicate|--now|a b" }
	}

	// The command is read whole, quotes and all.
	if got := fixCommands(check.finding); len(got) != 2 || strings.Join(got[0], "|") != "mw|frobnicate|--now|a b" || strings.Join(got[1], "|") != "mw|polish" {
		t.Fatalf("read the commands wrongly: %q", got)
	}

	problems := cureProblems(ctx, t, fixCase{name: "stand-in", check: check, watch: ranNothing})
	if len(problems) != 2 {
		t.Fatalf("expected a problem for each command the cure did not run, got %q", problems)
	}
	// One of two run is still a failure, naming the one that did not.
	problems = cureProblems(ctx, t, fixCase{name: "stand-in", check: check, watch: ranFrobnicate})
	if len(problems) != 1 || !strings.Contains(problems[0], "mw polish") {
		t.Fatalf("expected the one command not run to be named, got %q", problems)
	}
	// A case with no judge at all runs nothing.
	if problems := cureProblems(ctx, t, fixCase{name: "stand-in", check: check}); len(problems) != 2 {
		t.Fatalf("expected both commands unrun with no judge, got %q", problems)
	}
	// Marked hand-only, a finding that does not say a person does it still fails;
	// one that does, with a cure that fails, passes.
	if problems := cureProblems(ctx, t, fixCase{name: "stand-in", check: check, handOnly: true}); len(problems) != 2 {
		t.Fatalf("expected hand-only without the words and with a cure that runs to fail twice, got %q", problems)
	}
	handOnly := &handOnlyCheck{}
	if problems := cureProblems(ctx, t, fixCase{name: "hand-only", check: handOnly, handOnly: true}); len(problems) != 0 {
		t.Fatalf("expected a hand-only check that says so to pass, got %q", problems)
	}
	// A check that is not faulty tests nothing, and says so.
	if problems := cureProblems(ctx, t, fixCase{name: "ok", check: &okCheck{}}); len(problems) != 1 {
		t.Fatalf("expected a case whose probe is not faulty to fail, got %q", problems)
	}
}

// handOnlyCheck names a command, says no cure runs it, and its cure fails.
type handOnlyCheck struct{ standInCheck }

func (h *handOnlyCheck) Probe(context.Context) (application.Verdict, string) {
	return application.DoctorFaulty, "no cure: a person runs mw polish"
}
func (h *handOnlyCheck) Cure(context.Context) error {
	return fmt.Errorf("no cure: a person runs mw polish")
}

type okCheck struct{ standInCheck }

func (o *okCheck) Probe(context.Context) (application.Verdict, string) {
	return application.DoctorOK, ""
}

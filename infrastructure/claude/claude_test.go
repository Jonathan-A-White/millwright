package claude

// The unit tests here cover the parts that are ours rather than Claude Code's:
// which flags a session is started with, what its environment says it is, and
// the quoting of the line a shell reads. The quoting is checked by running the
// line through a real /bin/sh with a harmless command in Claude Code's place —
// no session is ever started here, and nothing costs fuel.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// launch is a complete launch, with whatever a test wants to change applied.
func launch(change func(*application.Launch)) application.Launch {
	l := application.Launch{
		StoryID: "mw-gq6.6",
		Path: domain.Path{
			Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
			Model: domain.ModelOpus, Effort: domain.EffortHigh,
			Formula: "tdd-feature", Host: "vps",
		},
		Seat:       "builder",
		Host:       "vps",
		Dir:        "/root/.mw-worktrees/mw-gq6.6",
		BootFile:   "/root/millwright-vault/runs/mw-gq6.6/boot.md",
		ResultFile: "/root/millwright-vault/runs/mw-gq6.6/result.json",
		Kickoff:    "You are booted into the builder seat.",
	}
	if change != nil {
		change(&l)
	}
	return l
}

func TestSessionIsAShellLineThatKeepsTheResult(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}

	if len(spec.Command) != 3 || spec.Command[0] != Shell || spec.Command[1] != "-c" {
		t.Fatalf("expected %s -c <line>, got %q", Shell, spec.Command)
	}
	line := spec.Command[2]
	for _, want := range []string{
		"claude --print",
		"--output-format json",
		"--model opus",
		"--effort high",
		"--permission-mode auto",
		"--permission-prompts none",
		"--append-system-prompt-file /root/millwright-vault/runs/mw-gq6.6/boot.md",
		"--settings '" + SessionSettings + "'",
		"--name mw-gq6.6",
		"'You are booted into the builder seat.'",
		"> /root/millwright-vault/runs/mw-gq6.6/result.json.tmp",
		"; mv /root/millwright-vault/runs/mw-gq6.6/result.json.tmp /root/millwright-vault/runs/mw-gq6.6/result.json",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the line to carry %q, got %q", want, line)
		}
	}
	if spec.Name != "mw-gq6_6" {
		t.Errorf("expected the session to be named mw-gq6_6, got %q", spec.Name)
	}
	if spec.Dir != "/root/.mw-worktrees/mw-gq6.6" {
		t.Errorf("expected the session to run in the worktree, got %q", spec.Dir)
	}
}

// TestTheResultIsWrittenToATempPathAndRenamedIntoPlace pins the fix for
// mw-gq6.89: a session ran to completion and its real result never reached
// result.json, because a plain `> result.json` redirect opens (and truncates)
// that exact path the moment the session starts and keeps the same file
// descriptor for the session's whole run — so anything else that replaces the
// path in the meantime (a git operation elsewhere in the vault touched it, on
// the incident's evidence) orphans the descriptor, and the session's own,
// completed write lands nowhere anyone can read it. Redirecting into a temp
// path beside the real one and renaming it into place only once the session
// has fully exited means nothing but this session's own finished output ever
// reaches the real path, however the vault around it was touched meanwhile.
func TestTheResultIsWrittenToATempPathAndRenamedIntoPlace(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	line := spec.Command[2]

	if strings.Contains(line, "> /root/millwright-vault/runs/mw-gq6.6/result.json;") ||
		strings.HasSuffix(strings.TrimSpace(line), "> /root/millwright-vault/runs/mw-gq6.6/result.json") {
		t.Errorf("expected the session to redirect into a temp path, not its result path directly, got %q", line)
	}
	tmpThenRename := "> /root/millwright-vault/runs/mw-gq6.6/result.json.tmp; " +
		"mv /root/millwright-vault/runs/mw-gq6.6/result.json.tmp /root/millwright-vault/runs/mw-gq6.6/result.json"
	if !strings.Contains(line, tmpThenRename) {
		t.Errorf("expected the redirect to a temp path followed by a rename into the real one, got %q", line)
	}
}

// TestTheResultReplacesWhateverWasAtItsPathWhenTheSessionEnds runs the
// assembled line for real, through a stand-in for claude, with something
// already sitting at the result path when the session starts — standing in for
// a stale result an earlier attempt left, or a git operation that touched the
// path while this session ran. The rename at the end must still leave exactly
// this session's own output there, and nothing of the temp file behind.
func TestTheResultReplacesWhateverWasAtItsPathWhenTheSessionEnds(t *testing.T) {
	dir := t.TempDir()
	result := filepath.Join(dir, "result.json")
	if err := os.WriteFile(result, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seeding the result path: %v", err)
	}

	program := filepath.Join(dir, "claude")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s' '{\"ok\":true}'\n"), 0o755); err != nil {
		t.Fatalf("writing the stand-in claude: %v", err)
	}

	spec, err := New(WithProgram(program)).Session(launch(func(l *application.Launch) {
		l.ResultFile = result
	}))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if err := exec.Command(spec.Command[0], spec.Command[1:]...).Run(); err != nil {
		t.Fatalf("running the assembled line: %v", err)
	}

	got, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("reading the result back: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Errorf("expected the session's own result to have replaced what was there, got %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the directory back: %v", err)
	}
	if len(entries) != 2 { // the stand-in claude and result.json, no leftover .tmp
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected no leftover temp file, got %v", names)
	}
}

// TestTheSessionSignsNothing pins the one setting this factory cannot do
// without: Claude Code's default is to sign every commit it makes with a
// Co-Authored-By trailer and every pull request with a "Generated with" line,
// and this factory's commits carry neither. The setting is passed as a JSON
// string on the command line rather than as a file, because a file would have
// to live somewhere — and anywhere it could live in a rig's worktree is
// somewhere a session could commit it by accident.
func TestTheSessionSignsNothing(t *testing.T) {
	var settings struct {
		Attribution struct {
			Commit     *string `json:"commit"`
			PR         *string `json:"pr"`
			SessionURL *bool   `json:"sessionUrl"`
		} `json:"attribution"`
	}
	if err := json.Unmarshal([]byte(SessionSettings), &settings); err != nil {
		t.Fatalf("the settings the session is given are not JSON: %v", err)
	}
	switch {
	case settings.Attribution.Commit == nil || *settings.Attribution.Commit != "":
		t.Errorf("expected attribution.commit to be the empty string, got %v", settings.Attribution.Commit)
	case settings.Attribution.PR == nil || *settings.Attribution.PR != "":
		t.Errorf("expected attribution.pr to be the empty string, got %v", settings.Attribution.PR)
	case settings.Attribution.SessionURL == nil || *settings.Attribution.SessionURL:
		t.Errorf("expected attribution.sessionUrl to be false, got %v", settings.Attribution.SessionURL)
	}

	dir := t.TempDir()
	spec, err := New().Session(launch(func(l *application.Launch) { l.Dir = dir }))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}

	// The setting arrives as one word after --settings, quoted so that the
	// shell hands Claude Code the JSON whole.
	if want := "--settings '" + SessionSettings + "'"; !strings.Contains(spec.Command[2], want) {
		t.Errorf("expected the line to carry %q, got %q", want, spec.Command[2])
	}

	// Nothing was written into the worktree: no settings file for a session to
	// commit by accident, and none to clean up afterwards.
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the worktree back: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("expected the adapter to leave nothing in the worktree, got %v", left)
	}
}

// TestTheSessionMayRunBd pins the other half of the same settings document: a
// session tracks its own work in beads, and in `auto` mode the permission
// classifier has refused a `bd close` as a write to an external system, at
// random and on both hosts. With `--permission-prompts none` such a refusal is
// final. An allow rule for the bd program resolves the call before the
// classifier is asked. It is the whole program rather than a list of
// subcommands, by the Governor's decision (mw-gq6.43), and it rides in the same
// one JSON document as the attribution keys, because `--settings` takes one.
func TestTheSessionMayRunBd(t *testing.T) {
	var settings struct {
		Attribution map[string]any `json:"attribution"`
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(SessionSettings), &settings); err != nil {
		t.Fatalf("the settings the session is given are not JSON: %v", err)
	}
	if len(settings.Attribution) == 0 {
		t.Error("expected one document holding both halves, got no attribution in it")
	}
	if !slices.Contains(settings.Permissions.Allow, BeadsAllowRule) {
		t.Errorf("expected permissions.allow to hold %q, got %v", BeadsAllowRule, settings.Permissions.Allow)
	}

	// The rule is the program and a trailing wildcard: every bd command, not a
	// list of subcommands. The space before the `*` is part of the rule.
	if BeadsAllowRule != "Bash(bd *)" {
		t.Errorf("expected the rule to allow every bd command, got %q", BeadsAllowRule)
	}

	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if !strings.Contains(spec.Command[2], BeadsAllowRule) {
		t.Errorf("expected the line to carry %q, got %q", BeadsAllowRule, spec.Command[2])
	}
}

// TestTheSessionMayRunItsRigsOwnTests: a Builder session is refused a command
// the [tests] table names for its own rig unless an allow rule already covers
// it (mw-gq6.83). The whole line becomes a rule, and so does each side of a
// `&&`, so a Builder may run either half alone.
func TestTheSessionMayRunItsRigsOwnTests(t *testing.T) {
	h := New(WithTests(map[string]string{"spell-forge": "npm ci --no-audit --no-fund && npm test"}))

	spec, err := h.Session(launch(func(l *application.Launch) { l.Path.Rig = "spell-forge" }))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	got := settingsAllow(t, settingsFromLine(t, spec.Command[2]))

	want := []string{
		BeadsAllowRule,
		"Bash(npm ci --no-audit --no-fund && npm test)",
		"Bash(npm ci --no-audit --no-fund)",
		"Bash(npm test)",
	}
	if len(got) != len(want) {
		t.Fatalf("expected exactly %v, got %v", want, got)
	}
	for _, rule := range want {
		if !slices.Contains(got, rule) {
			t.Errorf("expected permissions.allow to hold %q, got %v", rule, got)
		}
	}
}

// TestARigWithNoTestsLineGetsOnlyTheBeadsRule: a rig this host's [tests] table
// does not name adds no rule at all — its tests, whatever they are, still go
// to the classifier.
func TestARigWithNoTestsLineGetsOnlyTheBeadsRule(t *testing.T) {
	h := New(WithTests(map[string]string{"spell-forge": "npm test"}))

	spec, err := h.Session(launch(func(l *application.Launch) { l.Path.Rig = "millwright" }))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if got := settingsFromLine(t, spec.Command[2]); got != SessionSettings {
		t.Errorf("expected the settings a rig with no tests line gets to be exactly SessionSettings, got %q", got)
	}
}

// TestTheTestsRuleIsTakenVerbatim: the rule text is the config's command line
// exactly, with no shell rewriting, and a command that could smuggle a second
// one past an allow rule meant for the one it names is refused at launch.
func TestTheTestsRuleIsTakenVerbatim(t *testing.T) {
	command := `go test -tags "beads_integration,slow"  ./...`
	h := New(WithTests(map[string]string{"spell-forge": command}))
	spec, err := h.Session(launch(func(l *application.Launch) { l.Path.Rig = "spell-forge" }))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	got := settingsAllow(t, settingsFromLine(t, spec.Command[2]))
	if !slices.Contains(got, "Bash("+command+")") {
		t.Errorf("expected the rule to carry the command verbatim, got %v", got)
	}

	for name, bad := range map[string]string{
		"a newline":   "npm ci\nrm -rf /",
		"a semicolon": "npm ci; rm -rf /",
	} {
		h := New(WithTests(map[string]string{"spell-forge": bad}))
		if _, err := h.Session(launch(func(l *application.Launch) { l.Path.Rig = "spell-forge" })); err == nil {
			t.Errorf("expected a tests command holding %s to be refused at launch, got no error", name)
		}
	}
}

// settingsFromLine is the word after --settings on a session's shell line: the
// line is one string, not an argv, and the JSON is always quoted in single
// quotes because it is never a plain shell word.
func settingsFromLine(t *testing.T, line string) string {
	t.Helper()
	const marker = "--settings '"
	start := strings.Index(line, marker)
	if start < 0 {
		t.Fatalf("expected the line to carry %s, got %q", marker, line)
	}
	start += len(marker)
	end := strings.Index(line[start:], "'")
	if end < 0 {
		t.Fatalf("expected the settings to end in a closing quote, got %q", line)
	}
	return line[start : start+end]
}

// settingsAllow is permissions.allow of a settings JSON document.
func settingsAllow(t *testing.T, settings string) []string {
	t.Helper()
	var doc struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(settings), &doc); err != nil {
		t.Fatalf("the settings are not JSON: %v", err)
	}
	return doc.Permissions.Allow
}

func TestSessionIsTheSeatOnThisHost(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	for name, want := range map[string]string{
		"BEADS_ACTOR": "builder@vps",
		"MW_SEAT":     "builder@vps",
		"MW_STORY":    "mw-gq6.6",
	} {
		if got := spec.Env[name]; got != want {
			t.Errorf("expected %s to be %q, got %q", name, want, got)
		}
	}
}

// TestAStorySessionDisablesBackgroundTasks pins mw-gq6.90: a story's session
// is headless and ends the moment its turn does, so a Builder that backgrounds
// a command with it (the test suite, most often) and then ends its turn saying
// it will check back loses whatever it never committed — this happened three
// times (mw-gq6.39, twice on mw-gq6.86) despite the rig memory saying not to in
// words. CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1 is the harness's own way to
// take that option away rather than trust it is never used.
func TestAStorySessionDisablesBackgroundTasks(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if got := spec.Env["CLAUDE_CODE_DISABLE_BACKGROUND_TASKS"]; got != "1" {
		t.Errorf("expected a story session to disable background tasks, got %q", got)
	}
}

func TestSessionTakesThePermissionModeItIsGiven(t *testing.T) {
	spec, err := New(WithPermissionMode(PermissionDontAsk)).Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	if !strings.Contains(spec.Command[2], "--permission-mode dontAsk") {
		t.Errorf("expected the given permission mode, got %q", spec.Command[2])
	}

	if _, err := New(WithPermissionMode("whatever")).Session(launch(nil)); err == nil {
		t.Error("expected a permission mode Claude Code does not take to be refused")
	}
}

func TestSessionRefusesAnIncompleteOrForeignLaunch(t *testing.T) {
	cases := map[string]func(*application.Launch){
		"no story":       func(l *application.Launch) { l.StoryID = "" },
		"no seat":        func(l *application.Launch) { l.Seat = "" },
		"no boot file":   func(l *application.Launch) { l.BootFile = "" },
		"no result file": func(l *application.Launch) { l.ResultFile = "" },
		"no kickoff":     func(l *application.Launch) { l.Kickoff = "" },
		"no model":       func(l *application.Launch) { l.Path.Model = "" },
		"no effort":      func(l *application.Launch) { l.Path.Effort = "" },
		"another harness": func(l *application.Launch) {
			l.Path.Harness = domain.Harness("herdr")
		},
	}
	for name, change := range cases {
		if _, err := New().Session(launch(change)); err == nil {
			t.Errorf("expected a launch with %s to be refused", name)
		}
	}
}

// TestTheShellReadsBackTheArgumentsItWasGiven runs an assembled line through a
// real shell, with a command that only prints its arguments, and checks that
// every argument arrived whole. Story titles and paths carry quotes, colons,
// dollar signs and newlines, and a line a shell re-splits would hand Claude
// Code something other than what the factory meant.
func TestTheShellReadsBackTheArgumentsItWasGiven(t *testing.T) {
	dir := filepath.Join(t.TempDir(), `a dir with a 'quote' and $HOME`)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the run directory: %v", err)
	}
	result := filepath.Join(dir, "result.json")

	kickoff := "Your story is mw-gq6.6: it's \"quoted\", it holds $dollars, `backticks`,\na newline and a ; semicolon"
	spec, err := New(WithProgram("printf")).Session(launch(func(l *application.Launch) {
		l.BootFile = filepath.Join(dir, "boot.md")
		l.ResultFile = result
		l.Kickoff = kickoff
	}))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}

	// printf repeats its format for every remaining argument, so each argument
	// of the line comes back on a line of its own.
	line := strings.Replace(spec.Command[2], "printf --print", `printf '[%s]\n' --print`, 1)
	if err := exec.Command(spec.Command[0], "-c", line).Run(); err != nil {
		t.Fatalf("running the assembled line: %v", err)
	}

	printed, err := os.ReadFile(result)
	if err != nil {
		t.Fatalf("the line did not write its output where it was told: %v", err)
	}
	for _, want := range []string{
		"[--model]\n[opus]\n",
		"[--append-system-prompt-file]\n[" + filepath.Join(dir, "boot.md") + "]\n",
		// The settings JSON must reach Claude Code as one argument, braces,
		// quotes and all.
		"[--settings]\n[" + SessionSettings + "]\n",
		"[" + kickoff + "]\n",
	} {
		if !strings.Contains(string(printed), want) {
			t.Errorf("expected the shell to pass %q, got:\n%s", want, printed)
		}
	}
}

func TestShellQuoteLeavesPlainWordsAloneAndWrapsTheRest(t *testing.T) {
	plain := []string{"claude", "--model", "opus", "/root/millwright-vault/runs/mw-gq6.6/boot.md", "a,b=c+d%e@f:g"}
	for _, word := range plain {
		if got := shellQuote(word); got != word {
			t.Errorf("expected %q to be left alone, got %q", word, got)
		}
	}

	wrapped := map[string]string{
		"":                  "''",
		"two words":         "'two words'",
		"it's":              `'it'\''s'`,
		"$HOME":             "'$HOME'",
		"a\nb":              "'a\nb'",
		"semi;colon":        "'semi;colon'",
		`back\slash`:        `'back\slash'`,
		`"quoted"`:          `'"quoted"'`,
		"~/millwright":      "'~/millwright'",
		"*":                 "'*'",
		"`backticks`":       "'`backticks`'",
		"ends with a quote": "'ends with a quote'",
	}
	for word, want := range wrapped {
		if got := shellQuote(word); got != want {
			t.Errorf("quoting %q: expected %q, got %q", word, want, got)
		}
	}
}

func TestShellLineJoinsTheWordsItQuoted(t *testing.T) {
	got := shellLine([]string{"claude", "--name", "mw-gq6.6", "it's a story"})
	want := `claude --name mw-gq6.6 'it'\''s a story'`
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestTheCloseOutIsChainedOnHoweverTheSessionEnds(t *testing.T) {
	spec, err := New().Session(launch(func(l *application.Launch) {
		l.After = []string{"/root/millwright/bin/mw", "next", "mw-gq6.6"}
	}))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}

	line := spec.Command[2]
	want := "mv /root/millwright-vault/runs/mw-gq6.6/result.json.tmp /root/millwright-vault/runs/mw-gq6.6/result.json" +
		"; /root/millwright/bin/mw next mw-gq6.6"
	if !strings.Contains(line, want) {
		t.Errorf("expected the close-out to be chained after the rename into place, got %q", line)
	}
	// `&&` would skip the close-out for exactly the sessions that need one: the
	// ones that failed, ran out of fuel or died.
	if strings.Contains(line, "&&") {
		t.Errorf("expected the close-out to run whatever the harness exited with, got %q", line)
	}
}

func TestASessionWithNothingAfterItEndsAtTheRename(t *testing.T) {
	spec, err := New().Session(launch(nil))
	if err != nil {
		t.Fatalf("assembling the session: %v", err)
	}
	line := spec.Command[2]
	if !strings.HasSuffix(line, "mv /root/millwright-vault/runs/mw-gq6.6/result.json.tmp /root/millwright-vault/runs/mw-gq6.6/result.json") {
		t.Errorf("expected nothing chained after the rename with no After, got %q", line)
	}
}

// seatLaunch is a complete seat launch, with whatever a test changes applied.
func seatLaunch(change func(*application.SeatLaunch)) application.SeatLaunch {
	l := application.SeatLaunch{
		Seat:    "mayor",
		Name:    "mayor-2026-09-19-13",
		Dir:     "/root/millwright-vault",
		Charter: "/root/millwright-vault/seats/mayor/charter.md",
		Model:   domain.ModelOpus,
		Effort:  domain.EffortHigh,
		Kickoff: "Boot by procedures.md. Newest handoff: seats/mayor/handoffs/2026-09-19-12.md",
	}
	if change != nil {
		change(&l)
	}
	return l
}

func TestASeatSessionIsInteractiveAndPrimedFromTheCharterFile(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(nil))
	if err != nil {
		t.Fatalf("assembling the seat's session: %v", err)
	}
	if spec.Name != "mayor-2026-09-19-13" || spec.Dir != "/root/millwright-vault" {
		t.Errorf("expected the window to be named after the session and to run in the vault, got %+v", spec)
	}
	if got := spec.Env[application.SeatEnv]; got != "mayor" {
		t.Errorf("expected %s to be the seat itself, got %q", application.SeatEnv, got)
	}

	// The command is the program and its arguments: no shell reads it, so the
	// kickoff reaches the session however it is written.
	if spec.Command[0] != Program {
		t.Errorf("expected the window to run %s itself, got %q", Program, spec.Command[0])
	}
	if last := spec.Command[len(spec.Command)-1]; last != seatLaunch(nil).Kickoff {
		t.Errorf("expected the kickoff to be the last argument, got %q", last)
	}
	line := strings.Join(spec.Command, " ")
	for _, want := range []string{
		"--model opus", "--effort high", "--permission-mode " + DefaultPermissionMode,
		"--append-system-prompt-file /root/millwright-vault/seats/mayor/charter.md",
		"--name mayor-2026-09-19-13",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the seat's session to carry %q, got %q", want, line)
		}
	}
	// Nobody is at the keyboard is exactly what a seat's session is not.
	for _, unwanted := range []string{"--print", "--output-format", "--permission-prompts"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("expected an interactive session to carry no %s, got %q", unwanted, line)
		}
	}
}

// TestASeatSessionDoesNotDisableBackgroundTasks: a seat (the Mayor, a
// Millhand) is not a Builder — its mail watcher and its waiters run in the
// background by design, and a seat's session outlives any one turn, so
// mw-gq6.90's fix is scoped to a story's own session and must not reach here.
func TestASeatSessionDoesNotDisableBackgroundTasks(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(nil))
	if err != nil {
		t.Fatalf("assembling the seat's session: %v", err)
	}
	if _, ok := spec.Env["CLAUDE_CODE_DISABLE_BACKGROUND_TASKS"]; ok {
		t.Errorf("expected a seat's session not to disable background tasks, got %q", spec.Env)
	}
}

// TestASeatSessionGetsTheSettingsABuilderGets: a seat's session signs nothing
// and may run bd without being asked, exactly as a Builder's does. Without
// --settings it boots with Claude Code's default attribution on, which ends
// every commit with a Co-Authored-By line the vault forbids and mw next
// refuses, and it is asked about every bd call.
func TestASeatSessionGetsTheSettingsABuilderGets(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(func(l *application.SeatLaunch) { l.Attended = true }))
	if err != nil {
		t.Fatalf("assembling the seat's session: %v", err)
	}
	// No shell reads the command, so the JSON is one argument of its own.
	i := slices.Index(spec.Command, "--settings")
	if i < 0 || i+1 >= len(spec.Command) {
		t.Fatalf("expected the seat's session to carry --settings, got %q", spec.Command)
	}
	if got := spec.Command[i+1]; got != SessionSettings {
		t.Errorf("expected --settings to be SessionSettings, got %q", got)
	}
	var settings struct {
		Attribution struct {
			Commit     *string `json:"commit"`
			PR         *string `json:"pr"`
			SessionURL *bool   `json:"sessionUrl"`
		} `json:"attribution"`
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(spec.Command[i+1]), &settings); err != nil {
		t.Fatalf("the settings the seat's session is given are not JSON: %v", err)
	}
	a := settings.Attribution
	if a.Commit == nil || *a.Commit != "" || a.PR == nil || *a.PR != "" || a.SessionURL == nil || *a.SessionURL {
		t.Errorf("expected attribution to be off, got %+v", a)
	}
	if !slices.Contains(settings.Permissions.Allow, BeadsAllowRule) {
		t.Errorf("expected permissions.allow to hold %q, got %v", BeadsAllowRule, settings.Permissions.Allow)
	}
}

// settingsOfSeat is the word after --settings on a seat session's command line.
func settingsOfSeat(t *testing.T, l application.SeatLaunch) string {
	t.Helper()
	spec, err := New().SeatSession(l)
	if err != nil {
		t.Fatalf("assembling the seat's session: %v", err)
	}
	i := slices.Index(spec.Command, "--settings")
	if i < 0 || i+1 >= len(spec.Command) {
		t.Fatalf("expected the seat's session to carry --settings, got %q", spec.Command)
	}
	return spec.Command[i+1]
}

// permissionHooks is the PermissionRequest part of a settings document.
type permissionHooks struct {
	Hooks struct {
		PermissionRequest []struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"PermissionRequest"`
	} `json:"hooks"`
}

// TestAnUnattendedSeatSessionDeniesWhatItWouldAsk: a seat nobody is watching
// must never hang on a permission prompt. `--permission-prompts none` does
// nothing in an interactive session, so the seat's settings carry a
// PermissionRequest hook that answers deny (Claude Code's hooks reference,
// "PermissionRequest decision control"), and the session stays interactive
// and in the permission mode it always had.
func TestAnUnattendedSeatSessionDeniesWhatItWouldAsk(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(nil))
	if err != nil {
		t.Fatalf("assembling the seat's session: %v", err)
	}
	line := strings.Join(spec.Command, " ")
	for _, unwanted := range []string{"--print", "--permission-prompts"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("expected an interactive session to carry no %s, got %q", unwanted, line)
		}
	}
	if !strings.Contains(line, "--permission-mode "+DefaultPermissionMode) {
		t.Errorf("expected the seat to stay in %s, got %q", DefaultPermissionMode, line)
	}

	settings := settingsOfSeat(t, seatLaunch(nil))
	var got permissionHooks
	if err := json.Unmarshal([]byte(settings), &got); err != nil {
		t.Fatalf("the settings the seat's session is given are not JSON: %v", err)
	}
	groups := got.Hooks.PermissionRequest
	if len(groups) != 1 || len(groups[0].Hooks) != 1 {
		t.Fatalf("expected one PermissionRequest hook, got %+v", groups)
	}
	if groups[0].Matcher != "" && groups[0].Matcher != "*" {
		t.Errorf("expected the hook to match every tool, got the matcher %q", groups[0].Matcher)
	}
	hook := groups[0].Hooks[0]
	if hook.Type != "command" {
		t.Errorf("expected a command hook, got %q", hook.Type)
	}

	// Run what Claude Code would run: what it prints must be a deny decision.
	out, err := exec.Command("/bin/sh", "-c", hook.Command).Output()
	if err != nil {
		t.Fatalf("running the hook %q: %v", hook.Command, err)
	}
	var decision struct {
		HookSpecificOutput struct {
			HookEventName string `json:"hookEventName"`
			Decision      struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
			} `json:"decision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &decision); err != nil {
		t.Fatalf("the hook printed %q, which is not JSON: %v", out, err)
	}
	d := decision.HookSpecificOutput
	if d.HookEventName != "PermissionRequest" || d.Decision.Behavior != "deny" || d.Decision.Message == "" {
		t.Errorf("expected a PermissionRequest deny with a reason, got %q", out)
	}

	// The rest of what a Builder's session is given is still there.
	var base struct {
		Attribution struct {
			Commit *string `json:"commit"`
		} `json:"attribution"`
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(settings), &base); err != nil {
		t.Fatal(err)
	}
	if base.Attribution.Commit == nil || !slices.Contains(base.Permissions.Allow, BeadsAllowRule) {
		t.Errorf("expected the unattended seat to keep the attribution and the bd rule, got %q", settings)
	}
}

// TestAnAttendedSeatSessionIsAskedAsItAlwaysWas: a seat a person brings up by
// hand to talk to is asked about permissions, so its settings are exactly what
// a Builder gets and carry no hook.
func TestAnAttendedSeatSessionIsAskedAsItAlwaysWas(t *testing.T) {
	settings := settingsOfSeat(t, seatLaunch(func(l *application.SeatLaunch) { l.Attended = true }))
	if settings != SessionSettings {
		t.Errorf("expected --settings to be SessionSettings, got %q", settings)
	}
	var got permissionHooks
	if err := json.Unmarshal([]byte(settings), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Hooks.PermissionRequest) != 0 || strings.Contains(settings, "hooks") {
		t.Errorf("expected an attended seat to carry no hook, got %q", settings)
	}
}

func TestASeatSessionLeavesOutWhatItWasNotGiven(t *testing.T) {
	spec, err := New().SeatSession(seatLaunch(func(l *application.SeatLaunch) {
		l.Model, l.Effort = "", ""
	}))
	if err != nil {
		t.Fatalf("assembling the seat's session: %v", err)
	}
	if line := strings.Join(spec.Command, " "); strings.Contains(line, "--model") || strings.Contains(line, "--effort") {
		t.Errorf("expected a session given no model or effort to ask for neither, got %q", line)
	}
}

func TestASeatSessionRefusesAModelOrEffortItCannotRun(t *testing.T) {
	for _, change := range []func(*application.SeatLaunch){
		func(l *application.SeatLaunch) { l.Model = "banana" },
		func(l *application.SeatLaunch) { l.Effort = "utmost" },
	} {
		if _, err := New().SeatSession(seatLaunch(change)); err == nil {
			t.Errorf("expected %+v to be refused", seatLaunch(change))
		}
	}
}

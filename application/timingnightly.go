package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The nightly timing run (docs/timing-tests.md): for each rig whose file names
// timing_tests, the tests the landing gate leaves out are run on their own, on
// a calm host, when a file they cover has changed since the last night they
// passed.
const (
	// NightlyTimingJobName is the name of the follower's job that runs it: its
	// events' actor is timing@<host>.
	NightlyTimingJobName = "timing"

	// TimingOnly is the mode that runs only a rig's timing tests: MW_TIMING=only.
	TimingOnly = "only"

	// TimingDir is where the results are kept in the vault, one directory a rig.
	TimingDir = "runs/timing"

	// TimingTreePrefix names the throwaway worktree a timing run is made in,
	// after the rig.
	TimingTreePrefix = "timing-"

	// LabelTiming is the label of a bug the nightly timing run files, which the
	// guard against a second bug for the same file looks for.
	LabelTiming = "timing"

	// TimingCoversLines is how many lines at the top of a timing test's file are
	// read for its covers: line.
	TimingCoversLines = 10

	// TimingFoundBy ends the description of every bug the nightly timing run
	// files.
	TimingFoundBy = "Found by the nightly timing run (MW_TIMING=only); no story's gate runs the timing tests."

	// timingCoveredShown is how many of the changed files a bug names.
	timingCoveredShown = 20
)

// TaggedTest is a file the rig's timing_tests globs name, and the globs of its
// covers: line: the files whose change makes the test worth running. None says
// the test covers the whole rig.
type TaggedTest struct {
	File   string
	Covers []string
}

var coversLine = regexp.MustCompile(`(?i)\bcovers:\s*(.*)$`)

// ParseCovers reads the covers: line of a test file from its first
// TimingCoversLines lines: the globs after it, split at commas and spaces, with
// a comment's closing (*/ or -->) and quotes taken off. A file with no such
// line, or with nothing after it, covers the whole rig and gives none.
func ParseCovers(head string) []string {
	lines := strings.Split(head, "\n")
	if len(lines) > TimingCoversLines {
		lines = lines[:TimingCoversLines]
	}
	for _, line := range lines {
		found := coversLine.FindStringSubmatch(line)
		if found == nil {
			continue
		}
		text := strings.TrimSpace(found[1])
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(text, "*/"), "-->"))
		var globs []string
		for _, word := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			word = strings.Trim(word, "\"'`")
			word = strings.TrimPrefix(word, "./")
			if word != "" {
				globs = append(globs, word)
			}
		}
		return globs
	}
	return nil
}

// TimingCovered is the files of changed that some tagged test covers, in the
// order of changed: a test's own file always, the files of its covers: globs,
// and any file at all for a test with no covers: line.
func TimingCovered(tagged []TaggedTest, changed []string) []string {
	var covered []string
	for _, file := range changed {
		for _, test := range tagged {
			if file == test.File || len(test.Covers) == 0 || coveredBy(test.Covers, file) {
				covered = append(covered, file)
				break
			}
		}
	}
	return covered
}

func coveredBy(globs []string, file string) bool {
	for _, glob := range globs {
		if domain.Covers(glob, file) {
			return true
		}
	}
	return false
}

// TimingFailure is one test file that failed in a timing run: the first failing
// test of it and the first lines of its error.
type TimingFailure struct {
	File  string `json:"file"`
	Test  string `json:"first_failing_test,omitempty"`
	Error string `json:"first_error,omitempty"`
}

// TimingResult is what a timing run keeps, in runs/timing/<rig>/<date>.json.
type TimingResult struct {
	Rig    string `json:"rig"`
	Host   string `json:"host"`
	Sha    string `json:"sha"`
	Branch string `json:"branch"`
	// Since is the commit of the last passing night this run was due since; empty
	// when there was none.
	Since           string          `json:"since,omitempty"`
	Started         string          `json:"started"`
	DurationSeconds float64         `json:"duration_seconds"`
	UptimeBefore    string          `json:"uptime_before"`
	UptimeAfter     string          `json:"uptime_after"`
	WaitedSeconds   float64         `json:"waited_seconds"`
	Command         string          `json:"command"`
	Globs           []string        `json:"timing_tests"`
	ChangedCovered  []string        `json:"changed_covered_files"`
	Passed          bool            `json:"passed"`
	Failing         []TimingFailure `json:"failing_files"`
	// Unnamed says how a failed run named no file.
	Unnamed string `json:"failed_without_a_file,omitempty"`
}

// TimingRecord is one kept result: the UTC day of the file it is in
// (YYYY-MM-DD) and what it holds.
type TimingRecord struct {
	Day    string
	Result TimingResult
}

// ParseTimingRecord reads one kept result file, named for its day.
func ParseTimingRecord(day, contents string) (TimingRecord, error) {
	var result TimingResult
	if err := json.Unmarshal([]byte(contents), &result); err != nil {
		return TimingRecord{}, fmt.Errorf("the timing result of %s is not JSON: %w", day, err)
	}
	return TimingRecord{Day: day, Result: result}, nil
}

// TimingHistory is the port the kept results of the timing run are read back
// through: the vault's runs/timing/<rig>/<date>.json files.
type TimingHistory interface {
	// LatestTiming is the newest result kept for rig, and false when none is.
	LatestTiming(ctx context.Context, rig string) (TimingRecord, bool, error)
	// LatestPassingTiming is the newest result that passed, and false when none did.
	LatestPassingTiming(ctx context.Context, rig string) (TimingRecord, bool, error)
}

// TimingRecords is the port a timing run's result files are written to the
// vault through. It reports the path of the file from the vault's root.
type TimingRecords interface {
	PutTimingFile(ctx context.Context, rig, name, contents string) (string, error)
}

// TimingRig is the port a rig's timing tests and what changed are read
// through, at a commit of the rig's origin.
type TimingRig interface {
	// Tagged lists the files at commit that the globs name, each with the globs
	// of its covers: line.
	Tagged(ctx context.Context, rigDir, commit string, globs []string) ([]TaggedTest, error)
	// ChangedBetween lists the files that differ between two commits (git diff --name-only).
	ChangedBetween(ctx context.Context, rigDir, from, to string) ([]string, error)
}

// TimingRunner is the port a timing run's command line is run through, in a
// directory, by the shell, with env added to the environment the rig's commands
// are given. A command that ran and failed is a Checked that did not pass; an
// error is a command that could not be run at all.
type TimingRunner interface {
	RunTiming(ctx context.Context, rig, dir, command string, env map[string]string) (Checked, error)
}

// Uptimes is the port the host's `uptime` line is read through.
type Uptimes interface {
	Uptime(ctx context.Context) (string, error)
}

// TimingFiler files what a timing run found: one held bug under the rig's
// suite_epic for each failing file, or a dated comment on the open one that
// already names it.
type TimingFiler struct {
	Tracker WorkTracker
	Rules   EpicRules
}

// Epic is the rig's suite_epic, or the refusal that it names none.
func (f TimingFiler) Epic(ctx context.Context, rig string) (string, error) {
	rules, err := f.Rules.EpicRequirements(ctx, rig)
	if err != nil {
		return "", fmt.Errorf("reading what the rig %s requires of its epics: %w", rig, err)
	}
	if strings.TrimSpace(rules.SuiteEpic) == "" {
		return "", fmt.Errorf("the rig %s has timing_tests but no suite_epic in its rules file in the vault: a timing bug has no epic to be filed under", rig)
	}
	return strings.TrimSpace(rules.SuiteEpic), nil
}

// File files each failing file of result under epic, and reports the bead each
// is on. A file that could not be filed does not stop the others.
func (f TimingFiler) File(ctx context.Context, epic string, result TimingResult, logPath string) ([]SuiteFiled, error) {
	if len(result.Failing) == 0 {
		return nil, nil
	}
	detail, err := f.Tracker.ShowEpic(ctx, epic)
	if err != nil {
		return nil, fmt.Errorf("reading the suite epic %s: %w", epic, err)
	}
	var filed []SuiteFiled
	var failures []string
	for _, failure := range result.Failing {
		one, err := f.fileOne(ctx, detail, result, failure, logPath)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", failure.File, err))
			continue
		}
		filed = append(filed, one)
	}
	if len(failures) > 0 {
		return filed, fmt.Errorf("the timing run did not file everything:\n  %s", strings.Join(failures, "\n  "))
	}
	return filed, nil
}

func (f TimingFiler) fileOne(ctx context.Context, epic EpicDetail, result TimingResult, failure TimingFailure, logPath string) (SuiteFiled, error) {
	if story, open := openTimingBug(epic, failure.File); open {
		if err := f.Tracker.CommentOnStory(ctx, story.Story.ID, timingComment(result, failure, logPath)); err != nil {
			return SuiteFiled{}, fmt.Errorf("commenting on %s: %w", story.Story.ID, err)
		}
		return SuiteFiled{ID: story.Story.ID, File: failure.File}, nil
	}
	path := epic.Defaults
	path.Files = failure.File
	id, err := f.Tracker.CreateStory(ctx, NewStory{
		EpicID:      epic.ID,
		Title:       fmt.Sprintf("[bug] %s main: %s fails its timing run", result.Rig, failure.File),
		Description: timingBugDescription(result, failure, logPath),
		Acceptance:  fmt.Sprintf("1. %s passes with MW_TIMING=only on main 5 runs in a row, on a calm host (uptime beside each). 2. The rig's gate passes.\n3. Sweep: none, because the nightly timing run already runs every tagged test of the rig and found this one.", failure.File),
		Overrides:   path,
		Labels:      []string{LabelBug, LabelTiming},
	})
	if err != nil {
		return SuiteFiled{}, fmt.Errorf("filing the bug: %w", err)
	}
	_, notes := KeepDemoLast(ctx, f.Tracker, f.Rules, result.Rig, epic.ID, id)
	return SuiteFiled{ID: id, File: failure.File, New: true, Notes: notes}, nil
}

// openTimingBug is the open bead under the suite epic, labelled timing, whose
// title names file.
func openTimingBug(epic EpicDetail, file string) (StoryDetail, bool) {
	for _, story := range epic.Stories {
		if story.Status != StatusClosed && carriesAny(story.Labels, []string{LabelTiming}) && namesFile(story.Story.Title, file) {
			return story, true
		}
	}
	return StoryDetail{}, false
}

func timingCoveredText(result TimingResult) string {
	switch {
	case result.Since == "":
		return "no night has passed before this one, so nothing is said to have changed"
	case len(result.ChangedCovered) == 0:
		return "none could be told"
	}
	shown := result.ChangedCovered
	more := ""
	if len(shown) > timingCoveredShown {
		more = fmt.Sprintf(" and %d more", len(shown)-timingCoveredShown)
		shown = shown[:timingCoveredShown]
	}
	return strings.Join(shown, ", ") + more
}

func timingBugDescription(result TimingResult, failure TimingFailure, logPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s failed the nightly timing run of %s main at %s.\n\n", failure.File, result.Rig, result.Sha)
	if failure.Test != "" {
		fmt.Fprintf(&b, "First failing test: %s\n\n", failure.Test)
	}
	if failure.Error != "" {
		fmt.Fprintf(&b, "First error lines:\n%s\n\n", failure.Error)
	}
	fmt.Fprintf(&b, "Main's sha: %s\n", result.Sha)
	if result.Since != "" {
		fmt.Fprintf(&b, "Last passing night: %s\n", result.Since)
	}
	fmt.Fprintf(&b, "Covered files that changed since: %s\n", timingCoveredText(result))
	fmt.Fprintf(&b, "Load before the run: %s\nLoad after the run: %s\n", result.UptimeBefore, result.UptimeAfter)
	fmt.Fprintf(&b, "The day's log, in the vault: %s\n\n", logPath)
	b.WriteString(TimingFoundBy)
	return b.String()
}

func timingComment(result TimingResult, failure TimingFailure, logPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Timing run %s: %s failed again on main at %s.", result.Started, failure.File, result.Sha)
	if failure.Error != "" {
		fmt.Fprintf(&b, "\nFirst error lines:\n%s", failure.Error)
	}
	fmt.Fprintf(&b, "\nLoad before: %s\nLoad after: %s\nThe day's log, in the vault: %s", result.UptimeBefore, result.UptimeAfter, logPath)
	return b.String()
}

// NightlyTiming is the follower's job that runs the timing tests of every rig
// whose file says timing_tests, on the home only, in the night after the suite.
//
// From Hour on, a rig whose newest kept timing result is not of today's date is
// due when a file a tagged test covers changed between the commit of its last
// passing night and the head of its origin (none yet: always due). A rig whose
// own file asks for the nightly suite waits for that suite's result of today,
// so the two never run together. The run waits for a calm host, takes this
// host's gate lock, and runs the rig's [tests] command with MW_TIMING=only in a
// throwaway worktree of origin's main; a host that never calms skips the night.
// A rig is tried once a day by this follower, whether or not it ran.
type NightlyTiming struct {
	// Hour is the UTC hour of the day from which the run is due: the suite's.
	Hour int
	// Rigs and Tests are the config file's tables, by rig name.
	Rigs  map[string]string
	Tests map[string]string
	// Rules is where each rig's timing_tests, nightly_suite and suite_epic are read from.
	Rules EpicRules
	// Suites is where the day's result of the whole suite is looked for.
	Suites  SuiteHistory
	History TimingHistory
	Records TimingRecords
	Files   VaultFiles
	Source  TimingRig
	Trees   SuiteTrees
	Heads   RigHeads
	Runner  TimingRunner
	Uptime  Uptimes
	Filer   TimingFiler
	// Load, LoadPoll, LoadBound and LoadWait are the calm wait's: Suite's own.
	Load      HostLoad
	LoadPoll  time.Duration
	LoadBound time.Duration
	LoadWait  func(ctx context.Context, d time.Duration) error
	// GateLock keeps the run to one rig's tests on this host at a time; nil
	// takes none.
	GateLock GateLock
	// Home says whether this host is the home: only the home runs it.
	Home func(ctx context.Context) (bool, error)
	Host string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Out is where the log lines are said.
	Out io.Writer

	mu    sync.Mutex
	tried map[string]string // rig -> the day it was last looked at
}

func (n *NightlyTiming) now() time.Time {
	if n.Now != nil {
		return n.Now().UTC()
	}
	return time.Now().UTC()
}

func (n *NightlyTiming) say(format string, args ...any) {
	if n.Out != nil {
		fmt.Fprintf(n.Out, format, args...)
	}
}

// suite is the Suite whose calm wait and worktree the run uses.
func (n *NightlyTiming) suite() Suite {
	return Suite{Trees: n.Trees, Heads: n.Heads, Load: n.Load, LoadPoll: n.LoadPoll, LoadBound: n.LoadBound, LoadWait: n.LoadWait, Host: n.Host, Out: n.Out}
}

// Job is the follower's job: no event springs it; every pass asks whether a
// rig is due.
func (n *NightlyTiming) Job() SpringJob {
	return SpringJob{
		Name:        NightlyTimingJobName,
		TellFailure: true,
		Probe: func(ctx context.Context) (string, bool) {
			due, _ := n.Due(ctx)
			if len(due) == 0 {
				return "", false
			}
			names := make([]string, len(due))
			for i, one := range due {
				names[i] = one.Rig
			}
			return "nightly timing run of " + strings.Join(names, ", "), true
		},
		Run: n.run,
	}
}

// TimingDue is a rig whose timing run is due, with what made it so.
type TimingDue struct {
	Rig, RigDir string
	Globs       []string
	// Head is the origin's head the run was found due at.
	Head string
	// Since is the commit of the last passing night, empty when there was none.
	Since string
	// Covered are the files a tagged test covers that changed since.
	Covered []string
}

// Due is the rigs whose timing run is to be made now, in name order. A rig
// looked at and found to need no run, because nothing a tagged test covers has
// changed, is told in the log and not looked at again that day. A rig whose
// rules or results cannot be read is left out, and the error comes back beside
// the rest.
func (n *NightlyTiming) Due(ctx context.Context) ([]TimingDue, error) {
	now := n.now()
	if now.Hour() < n.Hour {
		return nil, nil
	}
	day := now.Format("2006-01-02")
	names := make([]string, 0, len(n.Rigs))
	for name := range n.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)

	n.mu.Lock()
	defer n.mu.Unlock()
	type candidate struct {
		rig   string
		globs []string
	}
	var candidates []candidate
	var errs []error
	for _, name := range names {
		if n.tried[name] == day {
			continue
		}
		rules, err := n.Rules.EpicRequirements(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading the rules of %s: %w", name, err))
			continue
		}
		if len(rules.TimingTests) == 0 {
			continue
		}
		latest, kept, err := n.History.LatestTiming(ctx, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading the last timing run of %s: %w", name, err))
			continue
		}
		if kept && latest.Day >= day {
			continue
		}
		if strings.TrimSpace(rules.SuiteEpic) == "" {
			n.markTried(name, day)
			n.say("  timing  %s has timing_tests but no suite_epic in its rules file in the vault: its timing run has no epic to file a bug under, so it is not run\n", name)
			continue
		}
		if rules.NightlySuite && n.Suites != nil {
			suite, kept, err := n.Suites.LatestSuite(ctx, name)
			if err != nil {
				errs = append(errs, fmt.Errorf("reading the last suite of %s: %w", name, err))
				continue
			}
			if !kept || suite.Day < day {
				// The suite goes first; the pass that finds its result runs this.
				continue
			}
		}
		candidates = append(candidates, candidate{name, rules.TimingTests})
	}
	if len(candidates) == 0 {
		return nil, errors.Join(errs...)
	}
	// Home last, for it reads a file in the vault: asked only when a rig is a candidate.
	if at, err := n.Home(ctx); err != nil || !at {
		return nil, errors.Join(append(errs, err)...)
	}

	var due []TimingDue
	for _, c := range candidates {
		found, why, err := n.assess(ctx, c.rig, c.globs)
		if err != nil {
			errs = append(errs, fmt.Errorf("the timing run of %s: %w", c.rig, err))
			continue
		}
		if found == nil {
			n.markTried(c.rig, day)
			n.say("  timing  no timing run of %s: %s\n", c.rig, why)
			continue
		}
		due = append(due, *found)
	}
	return due, errors.Join(errs...)
}

func (n *NightlyTiming) markTried(rig, day string) {
	if n.tried == nil {
		n.tried = map[string]string{}
	}
	n.tried[rig] = day
}

// assess says whether the timing run of rig is due, from what origin's head
// and the last passing night say. When it is not, why says so.
func (n *NightlyTiming) assess(ctx context.Context, rig string, globs []string) (due *TimingDue, why string, err error) {
	rigDir := n.Rigs[rig]
	head, err := n.Heads.Resolve(ctx, rigDir, "")
	if err != nil {
		return nil, "", fmt.Errorf("reading %s's origin: %w", rig, err)
	}
	tagged, err := n.Source.Tagged(ctx, rigDir, head.Commit, globs)
	if err != nil {
		return nil, "", fmt.Errorf("listing the timing tests at %s: %w", short(head.Commit), err)
	}
	if len(tagged) == 0 {
		return nil, fmt.Sprintf("no file at %s matches its timing_tests", short(head.Commit)), nil
	}
	found := &TimingDue{Rig: rig, RigDir: rigDir, Globs: globs, Head: head.Commit}
	passed, kept, err := n.History.LatestPassingTiming(ctx, rig)
	if err != nil {
		return nil, "", fmt.Errorf("reading the last passing timing run: %w", err)
	}
	if !kept {
		return found, "", nil
	}
	found.Since = passed.Result.Sha
	if found.Since == head.Commit {
		return nil, fmt.Sprintf("main is still at %s, where its timing tests last passed", short(head.Commit)), nil
	}
	changed, err := n.Source.ChangedBetween(ctx, rigDir, found.Since, head.Commit)
	if err != nil {
		// What changed cannot be told (the commit may be gone): run, rather than
		// leave the tests unrun on a guess.
		n.say("  timing  what changed in %s since %s could not be read (%v): running it\n", rig, short(found.Since), err)
		return found, "", nil
	}
	found.Covered = TimingCovered(tagged, changed)
	if len(found.Covered) == 0 {
		return nil, fmt.Sprintf("nothing its timing tests cover changed since %s, where they last passed", short(found.Since)), nil
	}
	return found, "", nil
}

// run runs each due rig's timing tests, one at a time, marking every one of
// them tried before the first begins so that a pass asked about while this one
// runs finds nothing more to do. A rig that cannot be run does not stop the
// next; the pass then fails, naming each. A pass that made no run says why.
func (n *NightlyTiming) run(ctx context.Context) error {
	due, err := n.Due(ctx)
	if len(due) == 0 {
		if err != nil {
			return err
		}
		return SkippedPass("no timing run: no rig is due (none has timing tests, each has today's result or nothing a test covers changed, or this host is not the home)")
	}
	day := n.now().Format("2006-01-02")
	n.mu.Lock()
	for _, one := range due {
		n.markTried(one.Rig, day)
	}
	n.mu.Unlock()

	errs := []error{err}
	var skips []string
	ran := 0
	for _, one := range due {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		skipped, err := n.runRig(ctx, one)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("the timing run of %s: %w", one.Rig, err))
		case skipped != "":
			skips = append(skips, skipped)
		default:
			ran++
		}
	}
	if joined := errors.Join(errs...); joined != nil {
		return joined
	}
	if ran == 0 {
		return SkippedPass(strings.Join(skips, "; "))
	}
	return nil
}

// runRig makes one rig's timing run. A host that never calmed within the bound
// is no error: the night is skipped, and skipped says so, naming the load.
func (n *NightlyTiming) runRig(ctx context.Context, due TimingDue) (skipped string, err error) {
	rig := due.Rig
	tests := strings.TrimSpace(n.Tests[rig])
	if tests == "" {
		return "", fmt.Errorf("the rig %s has no [tests] entry in the config file: a timing run needs to be told what the tests are", rig)
	}
	// Refused before anything runs: a run that could not file what it finds is a
	// night's work thrown away.
	epic, err := n.Filer.Epic(ctx, rig)
	if err != nil {
		return "", err
	}

	suite := n.suite()
	reading, _, waited, gaveUp, err := suite.calmed(ctx, "the timing run")
	if err != nil {
		return "", fmt.Errorf("waiting for %s to calm: %w", n.Host, err)
	}
	if gaveUp {
		skipped = fmt.Sprintf("the timing run of %s was skipped: %s was still at load %.1f of %d cores after waiting %s", rig, n.Host, reading.Load, reading.Cores, waited)
		n.say("  timing  %s\n", skipped)
		return skipped, nil
	}

	if n.GateLock != nil {
		turn, err := n.GateLock.Take(ctx, "timing "+rig, func(by string) {
			n.say("  wait    %s's gate lock is held by %s: waiting for it before the timing run\n", n.Host, by)
		})
		if err != nil {
			n.say("  wait    %s's gate lock could not be taken (%v), so the timing run is made without it\n", n.Host, err)
		} else if turn.Holding != nil {
			defer turn.Holding.Release(context.WithoutCancel(ctx))
		}
	}

	dir, head, release, err := suite.cutTree(ctx, rig, due.RigDir, TimingTreePrefix)
	if err != nil {
		return "", err
	}
	defer release()

	before := n.uptime(ctx)
	started := n.now()
	n.say("  run     %s at %s on %s: %s\n", rig, short(head.Commit), head.Branch, tests)
	checked, err := n.Runner.RunTiming(ctx, rig, dir, tests, TimingEnv(TimingOnly, due.Globs))
	if err != nil {
		return "", fmt.Errorf("running `%s` in %s: %w", tests, dir, err)
	}
	if checked.NotRun {
		return "", fmt.Errorf("`%s` could not be run in %s, so there is no result to keep: %s", tests, dir, checked.Tail(5))
	}
	finished := n.now()
	after := n.uptime(ctx)

	result := TimingResult{
		Rig: rig, Host: n.Host, Sha: head.Commit, Branch: head.Branch, Since: due.Since,
		Started:         started.Format(time.RFC3339),
		DurationSeconds: math.Round(finished.Sub(started).Seconds()*1000) / 1000,
		UptimeBefore:    before, UptimeAfter: after, WaitedSeconds: waited.Seconds(),
		Command: tests, Globs: due.Globs, ChangedCovered: due.Covered,
		Passed: checked.Passed, Failing: []TimingFailure{},
	}
	if result.ChangedCovered == nil {
		result.ChangedCovered = []string{}
	}
	if !checked.Passed {
		for _, failed := range FailingFiles(checked.Output) {
			result.Failing = append(result.Failing, TimingFailure{File: failed.File, Test: failed.Test, Error: failed.Error})
		}
		if len(result.Failing) == 0 {
			result.Unnamed = fmt.Sprintf("`%s` failed and its output names no test file", tests)
			if short := FailingTestsShort(checked.Output); short != "" {
				result.Unnamed += " (" + short + ")"
			}
		}
	}

	logPath, err := n.keep(ctx, result, checked.Output, started)
	if err != nil {
		return "", err
	}
	switch {
	case result.Passed:
		n.say("  timing  %s at %s: the timing tests passed, %s\n", rig, short(head.Commit), finished.Sub(started).Round(time.Second))
	case result.Unnamed != "":
		return "", errors.New(result.Unnamed + " (the result is kept in " + logPath + "); nothing is filed")
	default:
		filed, err := n.Filer.File(ctx, epic, result, logPath)
		for _, one := range filed {
			how := "commented on"
			if one.New {
				how = "filed, held:"
			}
			n.say("  timing  %s %s for %s\n", how, one.ID, one.File)
		}
		if err != nil {
			return "", err
		}
	}
	return "", nil
}

// uptime is the host's uptime line, or what could not be read.
func (n *NightlyTiming) uptime(ctx context.Context) string {
	if n.Uptime == nil {
		return "unknown"
	}
	line, err := n.Uptime.Uptime(ctx)
	if err != nil {
		return "unknown (" + err.Error() + ")"
	}
	return strings.TrimSpace(line)
}

// keep writes the day's result and log in the vault and commits them, and
// reports the log's path from the vault's root.
func (n *NightlyTiming) keep(ctx context.Context, result TimingResult, output string, started time.Time) (string, error) {
	day := started.UTC().Format("2006-01-02")
	written, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("writing the result of the timing run: %w", err)
	}
	log := &suiteLog{}
	fmt.Fprintf(&log.b, "timing run of %s on %s at %s (%s), started %s\nuptime before: %s\nuptime after:  %s\n\n", result.Rig, result.Host, result.Sha, result.Branch, result.Started, result.UptimeBefore, result.UptimeAfter)
	log.section(result.Command, output)
	logPath, err := n.Records.PutTimingFile(ctx, result.Rig, day+".log", log.b.String())
	if err != nil {
		return "", fmt.Errorf("keeping the log of the timing run: %w", err)
	}
	resultPath, err := n.Records.PutTimingFile(ctx, result.Rig, day+".json", string(written)+"\n")
	if err != nil {
		return "", fmt.Errorf("keeping the result of the timing run: %w", err)
	}
	if n.Files != nil {
		said := "passed"
		if !result.Passed {
			said = fmt.Sprintf("%d %s failed", len(result.Failing), plural(len(result.Failing), "file", "files"))
		}
		if _, err := commitWritten(ctx, n.Files, fmt.Sprintf("mw timing %s %s: %s", result.Rig, day, said), []string{logPath, resultPath}); err != nil {
			return "", fmt.Errorf("committing the result of the timing run in the vault: %w", err)
		}
	}
	return path.Clean(logPath), nil
}

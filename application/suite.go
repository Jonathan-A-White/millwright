package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The whole-suite run (mw suite): the rig's [tests] command and then its
// [changed_specs] command with every spec, on a fresh worktree of the rig's
// origin, after the host is calm.
const (
	// SpecsPlaceholder is where a rig's changed-specs command takes the spec
	// files a story changed (mw-gq6.366); a whole-suite run puts nothing there.
	SpecsPlaceholder = "{specs}"

	// SuiteDir is where the results are kept in the vault, one directory a rig.
	SuiteDir = "runs/suite"

	// SuiteTreePrefix names the throwaway worktree a suite is run in, after the rig.
	SuiteTreePrefix = "suite-"

	// RecheckTreePrefix names the one a suite bug's spec is re-run in before the
	// bug is dispatched: not the suite's own, which a nightly run may be using.
	RecheckTreePrefix = "recheck-"

	// SuiteCalmBound is how long a suite waits for a busy host to calm, when no
	// flag says otherwise.
	SuiteCalmBound = 2 * time.Hour
)

// SuiteTrees is the port a suite's throwaway worktree is made and taken away
// through: a detached worktree of the rig at a commit, never the rig's own
// checkout, which after_landing uses.
type SuiteTrees interface {
	// AddDetached makes a worktree of the rig at dir, detached at commit.
	AddDetached(ctx context.Context, rigDir, dir, commit string) error
	// Exists and Remove are Worktrees' own.
	Exists(ctx context.Context, rigDir, dir, branch string) (bool, error)
	Remove(ctx context.Context, rigDir, dir, branch string) error
}

// SuiteRunner is the port a suite's command lines are run through, in a
// directory, by the shell. A command that ran and failed is a Checked that did
// not pass; an error is a command that could not be run at all.
type SuiteRunner interface {
	RunLine(ctx context.Context, dir, command string) (Checked, error)
}

// SuiteRecords is the port a suite's result files are written to the vault
// through. It reports the path of the file from the vault's root, as
// VaultFiles.Commit wants it.
type SuiteRecords interface {
	PutSuiteFile(ctx context.Context, rig, name, contents string) (string, error)
}

// Suite runs one rig's whole test suite on its origin's main.
type Suite struct {
	Trees   SuiteTrees
	Heads   RigHeads
	Runner  SuiteRunner
	Records SuiteRecords
	Files   VaultFiles
	Load    HostLoad

	// LoadPoll, LoadBound and LoadWait are Next's own: how often a busy host is
	// looked at again, how long it is waited for, and how the wait is made. Zero
	// reads GateLoadPoll and SuiteCalmBound.
	LoadPoll  time.Duration
	LoadBound time.Duration
	LoadWait  func(ctx context.Context, d time.Duration) error

	// Now is the clock; nil is time.Now.
	Now func() time.Time

	// Filer, when it is set, files what the run found under the rig's
	// suite_epic and mails the Mayor (mw suite --file); nil does neither.
	Filer *SuiteFiler

	Host string
	// Rigs, Tests and ChangedSpecs are the config file's tables, by rig name.
	Rigs         map[string]string
	Tests        map[string]string
	ChangedSpecs map[string]string
	Out          io.Writer
}

// SuiteLoad is the load a suite started at.
type SuiteLoad struct {
	Load  float64 `json:"load"`
	Cores int     `json:"cores"`
}

// SuiteFile is one test file that failed in the whole run: the first failing
// test of it and the first lines of its error, and how it went when it was run
// again alone. Flaky is a file that failed in the whole run and passed alone.
type SuiteFile struct {
	File  string `json:"file"`
	Phase string `json:"phase"`
	Test  string `json:"first_failing_test,omitempty"`
	Error string `json:"first_error,omitempty"`
	Flaky bool   `json:"flaky"`
	Alone string `json:"alone"`
}

// SuiteResult is the summary a suite writes beside its log.
type SuiteResult struct {
	Rig             string      `json:"rig"`
	Host            string      `json:"host"`
	Sha             string      `json:"sha"`
	Branch          string      `json:"branch"`
	Started         string      `json:"started"`
	DurationSeconds float64     `json:"duration_seconds"`
	LoadAtStart     *SuiteLoad  `json:"load_at_start,omitempty"`
	WaitedSeconds   float64     `json:"waited_seconds"`
	GaveUpWaiting   bool        `json:"gave_up_waiting"`
	Note            string      `json:"note,omitempty"`
	Commands        []string    `json:"commands"`
	Passed          bool        `json:"passed"`
	Failing         []SuiteFile `json:"failing_files"`
	Unnamed         []string    `json:"failed_without_a_file,omitempty"`
}

// SuiteReport is what a suite did: its result, where the two files are kept
// (from the vault's root), and whether any file failed.
type SuiteReport struct {
	Result     SuiteResult
	LogPath    string
	ResultPath string
	Failed     bool
	// Filed is what --file did with each failing file; nil without --file.
	Filed []SuiteFiled
}

// String is the short summary a suite prints.
func (r SuiteReport) String() string {
	var b strings.Builder
	switch {
	case r.Result.Passed:
		fmt.Fprintf(&b, "suite   %s at %s: all passed", r.Result.Rig, short(r.Result.Sha))
	case len(r.Result.Failing) > 0:
		flaky := 0
		for _, file := range r.Result.Failing {
			if file.Flaky {
				flaky++
			}
		}
		fmt.Fprintf(&b, "suite   %s at %s: %d %s failed", r.Result.Rig, short(r.Result.Sha), len(r.Result.Failing), plural(len(r.Result.Failing), "file", "files"))
		if flaky > 0 {
			fmt.Fprintf(&b, " (%d flaky)", flaky)
		}
	default:
		fmt.Fprintf(&b, "suite   %s at %s: failed", r.Result.Rig, short(r.Result.Sha))
	}
	fmt.Fprintf(&b, ", %s\n", time.Duration(r.Result.DurationSeconds*float64(time.Second)).Round(time.Second))
	for _, file := range r.Result.Failing {
		how := "failed twice"
		if file.Flaky {
			how = "flaky: passed alone"
		}
		fmt.Fprintf(&b, "  %s  %s", file.File, how)
		if file.Test != "" {
			fmt.Fprintf(&b, "  %s", file.Test)
		}
		b.WriteString("\n")
	}
	for _, said := range r.Result.Unnamed {
		fmt.Fprintf(&b, "  %s\n", said)
	}
	for _, one := range r.Filed {
		how := "commented on"
		if one.New {
			how = "filed, held:"
		}
		fmt.Fprintf(&b, "  %s %s for %s\n", how, one.ID, one.File)
	}
	if r.Result.GaveUpWaiting {
		fmt.Fprintf(&b, "  %s\n", r.Result.Note)
	}
	if r.LogPath != "" {
		fmt.Fprintf(&b, "  kept    %s and %s\n", r.LogPath, path.Base(r.ResultPath))
	}
	return b.String()
}

func short(sha string) string {
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (s Suite) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Suite) say(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format, args...)
	}
}

// calmed waits, up to the bound, for the host to stop being busy by the rule
// the close-out's gate uses (LoadReading.Busy), and reports the last reading it
// took, how long it waited, and whether the bound ran out with the host still
// busy. A host whose load is not asked for, or cannot be read, is not busy.
func (s Suite) calmed(ctx context.Context) (reading LoadReading, read bool, waited time.Duration, gaveUp bool, err error) {
	poll, bound := s.LoadPoll, s.LoadBound
	if poll <= 0 {
		poll = GateLoadPoll
	}
	if bound <= 0 {
		bound = SuiteCalmBound
	}
	wait := s.LoadWait
	if wait == nil {
		wait = waitFor
	}
	look := func() (LoadReading, bool) {
		if s.Load == nil {
			return LoadReading{}, false
		}
		reading, err := s.Load.Load(ctx)
		return reading, err == nil
	}

	reading, read = look()
	if read && reading.Busy() {
		s.say("  wait    %s is at load %.1f of %d cores: waiting up to %s for it to calm before the suite\n", s.Host, reading.Load, reading.Cores, bound)
	}
	for read && reading.Busy() {
		if waited >= bound {
			return reading, true, waited, true, nil
		}
		if err := wait(ctx, poll); err != nil {
			return reading, true, waited, false, err
		}
		waited += poll
		reading, read = look()
	}
	return reading, read, waited, false, nil
}

// suiteLog is the whole output of a suite, section by section.
type suiteLog struct{ b strings.Builder }

func (l *suiteLog) section(title, output string) {
	fmt.Fprintf(&l.b, "=== %s ===\n%s", title, CapGateLog(output))
	if !strings.HasSuffix(output, "\n") {
		l.b.WriteString("\n")
	}
	l.b.WriteString("\n")
}

// suitePhase is one command line of the whole run: the rig's tests, then its
// changed-specs command with every spec. alone is the command that runs one
// failing file by itself.
type suitePhase struct {
	name, command string
	alone         func(file string) string
}

// cutTree cuts the throwaway worktree a run is made in: detached at the head of
// the rig's origin, named for prefix and the rig. release takes it away again.
func (s Suite) cutTree(ctx context.Context, rig, rigDir, prefix string) (dir string, head RigCommit, release func(), err error) {
	head, err = s.Heads.Resolve(ctx, rigDir, "")
	if err != nil {
		return "", RigCommit{}, nil, fmt.Errorf("reading %s's origin: %w", rig, err)
	}
	dir = WorktreeDir(rigDir, prefix+rig)
	if there, err := s.Trees.Exists(ctx, rigDir, dir, ""); err != nil {
		return "", RigCommit{}, nil, err
	} else if there {
		// Left by a run that was killed: it is named for this rig and this kind of
		// run and nothing else uses it.
		if err := s.Trees.Remove(ctx, rigDir, dir, ""); err != nil {
			return "", RigCommit{}, nil, fmt.Errorf("clearing the worktree an earlier run left at %s: %w", dir, err)
		}
	}
	if err := s.Trees.AddDetached(ctx, rigDir, dir, head.Commit); err != nil {
		return "", RigCommit{}, nil, fmt.Errorf("cutting a worktree of %s at %s: %w", rig, head.Commit, err)
	}
	return dir, head, func() { s.Trees.Remove(context.WithoutCancel(ctx), rigDir, dir, "") }, nil
}

// Run runs rig's whole suite and writes it down. A file that failed is not an
// error: it is a report with Failed set, which the command turns into exit 1.
// An error is a suite that could not be run or kept at all.
func (s Suite) Run(ctx context.Context, rig string) (SuiteReport, error) {
	rigDir, known := s.Rigs[rig]
	if !known {
		return SuiteReport{}, fmt.Errorf("%s is no rig of this host: the config file's [rigs] table does not name it", rig)
	}
	tests := strings.TrimSpace(s.Tests[rig])
	if tests == "" {
		return SuiteReport{}, fmt.Errorf("the rig %s has no [tests] entry in the config file: a whole-suite run needs to be told what the suite is", rig)
	}
	var epic string
	if s.Filer != nil {
		// Refused before anything runs: a run that could not file what it finds
		// is a night's work thrown away.
		var err error
		if epic, err = s.Filer.Epic(ctx, rig); err != nil {
			return SuiteReport{}, err
		}
	}
	phases := []suitePhase{{name: "tests", command: tests, alone: func(file string) string { return tests + " " + shellWord(file) }}}
	if specs := strings.TrimSpace(s.ChangedSpecs[rig]); specs != "" {
		phases = append(phases, suitePhase{
			name:    "changed_specs",
			command: strings.ReplaceAll(specs, SpecsPlaceholder, ""),
			alone:   func(file string) string { return strings.ReplaceAll(specs, SpecsPlaceholder, shellWord(file)) },
		})
	}

	reading, read, waited, gaveUp, err := s.calmed(ctx)
	if err != nil {
		return SuiteReport{}, fmt.Errorf("waiting for %s to calm: %w", s.Host, err)
	}

	dir, head, release, err := s.cutTree(ctx, rig, rigDir, SuiteTreePrefix)
	if err != nil {
		return SuiteReport{}, err
	}
	defer release()

	started := s.now().UTC()
	result := SuiteResult{
		Rig: rig, Host: s.Host, Sha: head.Commit, Branch: head.Branch,
		Started: started.Format(time.RFC3339), WaitedSeconds: waited.Seconds(), GaveUpWaiting: gaveUp,
		Failing: []SuiteFile{},
	}
	if read {
		result.LoadAtStart = &SuiteLoad{Load: reading.Load, Cores: reading.Cores}
	}
	switch {
	case gaveUp:
		result.Note = fmt.Sprintf("%s was still at load %.1f of %d cores after waiting %s, so the suite ran anyway", s.Host, reading.Load, reading.Cores, waited)
		s.say("  wait    %s\n", result.Note)
	case waited > 0:
		result.Note = fmt.Sprintf("%s was busy: waited %s for it to calm before the suite", s.Host, waited)
	}

	log := &suiteLog{}
	fmt.Fprintf(&log.b, "mw suite %s on %s at %s (%s), started %s\n\n", rig, s.Host, head.Commit, head.Branch, result.Started)
	s.say("  run     %s at %s on %s\n", rig, short(head.Commit), head.Branch)
	for _, phase := range phases {
		result.Commands = append(result.Commands, phase.command)
		checked, err := s.Runner.RunLine(ctx, dir, phase.command)
		if err != nil {
			return SuiteReport{}, fmt.Errorf("running `%s` in %s: %w", phase.command, dir, err)
		}
		log.section(phase.command, checked.Output)
		if checked.NotRun {
			return SuiteReport{}, fmt.Errorf("`%s` could not be run in %s, so there is no result to keep: %s", phase.command, dir, checked.Tail(5))
		}
		if checked.Passed {
			continue
		}
		files := FailingFiles(checked.Output)
		if len(files) == 0 {
			said := fmt.Sprintf("`%s` failed and its output names no test file", phase.command)
			if short := FailingTestsShort(checked.Output); short != "" {
				said += " (" + short + ")"
			}
			result.Unnamed = append(result.Unnamed, said)
		}
		for _, failed := range files {
			file := SuiteFile{File: failed.File, Phase: phase.name, Test: failed.Test, Error: failed.Error}
			again := phase.alone(failed.File)
			alone, err := s.Runner.RunLine(ctx, dir, again)
			switch {
			case err != nil:
				file.Alone = "could not be run: " + err.Error()
			case alone.NotRun:
				file.Alone = "could not be run: " + alone.Tail(3)
			default:
				log.section(again, alone.Output)
				file.Alone, file.Flaky = "failed", false
				if alone.Passed {
					file.Alone, file.Flaky = "passed", true
				}
			}
			result.Failing = append(result.Failing, file)
		}
	}
	result.DurationSeconds = math.Round(s.now().UTC().Sub(started).Seconds()*1000) / 1000
	result.Passed = len(result.Failing) == 0 && len(result.Unnamed) == 0

	day := started.Format("2006-01-02")
	report := SuiteReport{Result: result, Failed: !result.Passed}
	written, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return report, fmt.Errorf("writing the result of the suite: %w", err)
	}
	if report.LogPath, err = s.Records.PutSuiteFile(ctx, rig, day+".log", log.b.String()); err != nil {
		return report, fmt.Errorf("keeping the log of the suite: %w", err)
	}
	if report.ResultPath, err = s.Records.PutSuiteFile(ctx, rig, day+".json", string(written)+"\n"); err != nil {
		return report, fmt.Errorf("keeping the result of the suite: %w", err)
	}
	if s.Files != nil {
		said := "all passed"
		if report.Failed {
			said = "failed"
			if n := len(result.Failing); n > 0 {
				said = fmt.Sprintf("%d %s failed", n, plural(n, "file", "files"))
			}
		}
		if _, err := commitWritten(ctx, s.Files, fmt.Sprintf("mw suite %s %s: %s", rig, day, said), []string{report.LogPath, report.ResultPath}); err != nil {
			return report, fmt.Errorf("committing the result of the suite in the vault: %w", err)
		}
	}
	if s.Filer != nil {
		filed, err := s.Filer.File(ctx, epic, report)
		report.Filed = filed
		if err != nil {
			s.say("%s", report.String())
			return report, err
		}
	}
	s.say("%s", report.String())
	return report, nil
}

// SpecRecheck is one spec run alone on a rig's main: whether it passed, at which
// commit, how many tests the run said passed (zero when it did not say), and the
// end of its output.
type SpecRecheck struct {
	Spec   string
	Sha    string
	Passed bool
	Tests  int
	Tail   string
}

// SpecRecheckTail is how many lines of the run a closing comment keeps.
const SpecRecheckTail = 8

var passedCount = regexp.MustCompile(`(\d+) passed`)

// Recheck runs one spec alone on the head of rig's origin, in a worktree of its
// own, with the command the suite runs a file alone with: a *.spec.* file with
// the rig's [changed_specs] command when it has one, any other file with the
// [tests] command and the file added. A spec that fails is an answer; an error
// is a run that could not be made.
func (s Suite) Recheck(ctx context.Context, rig, spec string) (SpecRecheck, error) {
	rigDir, known := s.Rigs[rig]
	if !known {
		return SpecRecheck{}, fmt.Errorf("%s is no rig of this host: the config file's [rigs] table does not name it", rig)
	}
	var command string
	if specs := strings.TrimSpace(s.ChangedSpecs[rig]); specs != "" && strings.Contains(path.Base(spec), ".spec.") {
		command = strings.ReplaceAll(specs, SpecsPlaceholder, shellWord(spec))
	} else if tests := strings.TrimSpace(s.Tests[rig]); tests != "" {
		command = tests + " " + shellWord(spec)
	} else {
		return SpecRecheck{}, fmt.Errorf("the rig %s has no [tests] entry in the config file: there is no command to run %s with", rig, spec)
	}
	dir, head, release, err := s.cutTree(ctx, rig, rigDir, RecheckTreePrefix)
	if err != nil {
		return SpecRecheck{}, err
	}
	defer release()

	checked, err := s.Runner.RunLine(ctx, dir, command)
	if err != nil {
		return SpecRecheck{}, fmt.Errorf("running `%s` in %s: %w", command, dir, err)
	}
	if checked.NotRun {
		return SpecRecheck{}, fmt.Errorf("`%s` could not be run in %s: %s", command, dir, checked.Tail(3))
	}
	found := SpecRecheck{Spec: spec, Sha: head.Commit, Passed: checked.Passed, Tail: checked.Tail(SpecRecheckTail)}
	if all := passedCount.FindAllStringSubmatch(domain.StripANSI(checked.Output), -1); len(all) > 0 {
		found.Tests, _ = strconv.Atoi(all[len(all)-1][1])
	}
	return found, nil
}

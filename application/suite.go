package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
	"strings"
	"time"
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

	head, err := s.Heads.Resolve(ctx, rigDir, "")
	if err != nil {
		return SuiteReport{}, fmt.Errorf("reading %s's origin: %w", rig, err)
	}
	dir := WorktreeDir(rigDir, SuiteTreePrefix+rig)
	if there, err := s.Trees.Exists(ctx, rigDir, dir, ""); err != nil {
		return SuiteReport{}, err
	} else if there {
		// Left by a suite that was killed: it is named for this rig's suite and
		// nothing else uses it.
		if err := s.Trees.Remove(ctx, rigDir, dir, ""); err != nil {
			return SuiteReport{}, fmt.Errorf("clearing the suite worktree an earlier run left at %s: %w", dir, err)
		}
	}
	if err := s.Trees.AddDetached(ctx, rigDir, dir, head.Commit); err != nil {
		return SuiteReport{}, fmt.Errorf("cutting a worktree of %s at %s: %w", rig, head.Commit, err)
	}
	defer s.Trees.Remove(context.WithoutCancel(ctx), rigDir, dir, "")

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
		if _, err := s.Files.Commit(ctx, fmt.Sprintf("mw suite %s %s: %s", rig, day, said), []string{report.LogPath, report.ResultPath}); err != nil {
			return report, fmt.Errorf("committing the result of the suite in the vault: %w", err)
		}
	}
	s.say("%s", report.String())
	return report, nil
}

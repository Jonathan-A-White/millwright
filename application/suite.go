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
	RunLine(ctx context.Context, rig, dir, command string) (Checked, error)
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

	// GateLock, when it is set, is the host's gate lock, the one a close-out's
	// gate and a whole mw check take: the suite takes it for each command line it
	// runs, after the wait for a calm host and not during it, and gives it back
	// between them, so it never runs beside a gate (mw-gq6.914) and a gate waits
	// behind one command line of it, not the whole suite (mw-gq6.917). nil takes
	// none. Only Run takes it: Recheck is run by callers that hold it already
	// (rerunUnderGate).
	GateLock GateLock
	// GateNoticeEvery is how often a suite that waits for the gate lock says
	// again that it is waiting; zero reads CheckGateNoticeEvery.
	GateNoticeEvery time.Duration

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
// Void is a file whose run alone said nothing of it: it could not be run, or it
// failed without running the file's test (a port in use, a build that failed),
// which is the tooling's fault and no bug of the file (mw-gq6.625).
type SuiteFile struct {
	File  string `json:"file"`
	Phase string `json:"phase"`
	Test  string `json:"first_failing_test,omitempty"`
	Error string `json:"first_error,omitempty"`
	Flaky bool   `json:"flaky"`
	Void  bool   `json:"void,omitempty"`
	Alone string `json:"alone"`
}

// SuiteLine is one command line a suite ran and the seconds it ran, not
// counting the wait for the gate lock before it (mw-gq6.920): what the rule that
// a close-out waits behind one command line (mw-gq6.917) is read from.
type SuiteLine struct {
	Command string  `json:"command"`
	Seconds float64 `json:"seconds"`
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
	// GateWaitedSeconds is the part of the duration spent waiting to take the gate
	// lock again between command lines (mw-gq6.917), not running them.
	GateWaitedSeconds float64 `json:"gate_waited_seconds,omitempty"`
	// Lines are the command lines run, in order, each with its seconds. A result
	// kept before mw-gq6.920 has none, and one with none writes no key.
	Lines []SuiteLine `json:"lines,omitempty"`
	// Notes say what was read of the host's load when a command line failed
	// (mw-gq6.1069): that it ran again after the host was far over its cores, or
	// that it was busy and the failure counted at once.
	Notes []string `json:"notes,omitempty"`
	// SkippedForLoad are the failures read with the host far over its cores that
	// the host did not calm for, so they were not run again and are filed as
	// nothing (mw-gq6.1069). A result with any is not passed.
	SkippedForLoad []SuiteSkipped `json:"skipped_for_load,omitempty"`
}

// SuiteSkipped is one failure a suite did not stand behind because the host was
// far over its cores while it ran and never calmed: the command line that failed
// and the file of it, when its output names one (a file's re-run is the file's),
// and the load it was read at.
type SuiteSkipped struct {
	Command string  `json:"command"`
	File    string  `json:"file,omitempty"`
	Load    float64 `json:"load"`
	Cores   int     `json:"cores"`
}

// String says what was skipped, as the summary and the mail list it.
func (k SuiteSkipped) String() string {
	what := k.File
	if what == "" {
		what = "`" + k.Command + "`"
	}
	return fmt.Sprintf("skipped for load  %s  failed at load %.1f of %d cores and the host did not calm: not run again, not filed", what, k.Load, k.Cores)
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
		switch {
		case file.Flaky:
			how = "flaky: passed alone"
		case file.Void:
			how = file.Alone + " (tooling: not filed)"
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
	for _, skipped := range r.Result.SkippedForLoad {
		fmt.Fprintf(&b, "  %s\n", skipped)
	}
	for _, note := range r.Result.Notes {
		fmt.Fprintf(&b, "  note    %s\n", note)
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
// the close-out's gate uses (LoadReading.Busy), saying what it is waiting to run
// before, and reports the last reading it
// took, how long it waited, and whether the bound ran out with the host still
// busy. A host whose load is not asked for, or cannot be read, is not busy.
func (s Suite) calmed(ctx context.Context, before string) (reading LoadReading, read bool, waited time.Duration, gaveUp bool, err error) {
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
		s.say("  wait    %s is at load %.1f of %d cores: waiting up to %s for it to calm before %s\n", s.Host, reading.Load, reading.Cores, bound, before)
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

// takeGateLock takes the host's gate lock for the suite of rig before the command
// line it names, waiting for the gate that holds it and saying so every
// GateNoticeEvery, and returns what gives it back (safe to call twice). A lock
// that cannot be taken ends the suite: before anything ran when ran is empty,
// else after the command lines it names.
func (s Suite) takeGateLock(ctx context.Context, rig, before string, ran []string) (func(), error) {
	if s.GateLock == nil {
		return func() {}, nil
	}
	every := s.GateNoticeEvery
	if every <= 0 {
		every = CheckGateNoticeEvery
	}
	began := time.Now()
	var by string
	quit, done := make(chan struct{}), make(chan struct{})
	noticing := false
	stop := func() {
		if noticing {
			noticing = false
			close(quit)
			<-done
		}
	}
	defer stop()
	turn, err := s.GateLock.Take(ctx, "suite "+rig, func(who string) {
		by = who
		s.say("  wait    %s's gate lock is held by %s: waiting for it before %s\n", s.Host, who, before)
		noticing = true
		go func() {
			defer close(done)
			tick := time.NewTicker(every)
			defer tick.Stop()
			for {
				select {
				case <-quit:
					return
				case <-ctx.Done():
					return
				case <-tick.C:
					holder := by
					if reader, ok := s.GateLock.(gateLockReader); ok {
						if now := reader.Holder(); now != "" {
							holder = now
						}
					}
					s.say("  wait    still waiting for %s's gate lock, held by %s (waited %s so far)\n", s.Host, holder, gateWaited(time.Since(began)))
				}
			}
		}()
	})
	stop()
	if err != nil {
		var why, ended string
		if len(ran) == 0 {
			why = fmt.Sprintf("the suite of %s did not start: %s's gate lock could not be taken after %s", rig, s.Host, gateWaited(time.Since(began)))
			ended = ", and nothing was run"
		} else {
			why = fmt.Sprintf("the suite of %s stopped before %s: %s's gate lock, given back after each command line so that a gate need not wait for the whole suite, could not be taken again after %s", rig, before, s.Host, gateWaited(time.Since(began)))
			ended = fmt.Sprintf(", and only `%s` had been run, so no result was kept", strings.Join(ran, "`, `"))
		}
		if by != "" {
			why += ", it is held by " + by
		}
		return nil, fmt.Errorf("%s%s: %w", why, ended, err)
	}
	if turn.For != "" {
		s.say("  wait    waited %s for %s's gate lock, held by %s\n", gateWaited(turn.Waited), s.Host, turn.For)
	}
	holding := turn.Holding
	return func() {
		if holding != nil {
			holding.Release(context.WithoutCancel(ctx))
			holding = nil
		}
	}, nil
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

// suiteAttempt is one command line as Suite.Run ran it: what the run that stands
// said and its error, and whether the host was far over its cores and never
// calmed, so that the line was not run again and its failure is skipped for the
// load Busy (mw-gq6.1069).
type suiteAttempt struct {
	Checked
	Err     error
	Skipped bool
	Busy    LoadReading
}

// suitePhase is one command line of the whole run: the rig's tests, then its
// changed-specs command with every spec. alone is the command that runs one
// failing file by itself, or why there is none: a re-run that is not alone is
// no re-run (mw-gq6.924).
type suitePhase struct {
	name, command string
	alone         func(file string) (command, void string)
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
	// A file of the [tests] line is run alone by that line's unit runner step and
	// by no other step: the file added to the whole line lands on its last step and
	// runs every file again (Lampas, 2026-11-03: 828 files, four times). The tree
	// is the one the whole run installed in, so the install step is not run again.
	phases := []suitePhase{{name: "tests", command: tests, alone: func(file string) (string, string) {
		step := unitStepFor(tests, file)
		if step == "" {
			return "", "the rig's [tests] command has no unit runner step (npm test, vitest, jest) to run " + file + " alone with, so it was not run again"
		}
		return step, ""
	}}}
	if specs := strings.TrimSpace(s.ChangedSpecs[rig]); specs != "" {
		phases = append(phases, suitePhase{
			name:    "changed_specs",
			command: strings.ReplaceAll(specs, SpecsPlaceholder, ""),
			alone: func(file string) (string, string) {
				return strings.ReplaceAll(specs, SpecsPlaceholder, shellWord(file)), ""
			},
		})
	}

	// The wait for a calm host first and the gate lock after it, so that the wait,
	// which may take up to the bound, keeps no close-out's gate and no Builder's
	// check waiting behind the suite. The load waited on may then be another
	// gate's, which is the lesser cost: a gate is 3 to 25 minutes, and the lock
	// that follows is not ours before it has ended (mw-gq6.917).
	reading, read, waited, gaveUp, err := s.calmed(ctx, "the suite")
	if err != nil {
		return SuiteReport{}, fmt.Errorf("waiting for %s to calm: %w", s.Host, err)
	}

	// The gate lock is held for one command line at a time and given back between
	// them, so that a gate that comes while the suite runs waits for the line in
	// hand and not for the whole suite; it is the next to take the lock, for the
	// suite asks again behind it. A suite that cannot take it again ends with the
	// result unkept, saying what it had run.
	var ran []string
	var gateWaited time.Duration
	giveBack, held := func() {}, false
	defer func() { giveBack() }()
	take := func(before string) error {
		asked := time.Now()
		given, err := s.takeGateLock(ctx, rig, before, ran)
		gateWaited += time.Since(asked)
		if err != nil {
			return err
		}
		giveBack, held = given, true
		return nil
	}
	// turn is the lock for the command line that follows, unless the one taken
	// for the worktree is still held; runLine runs it and gives the lock back.
	turn := func(before string) error {
		if held {
			return nil
		}
		return take(before)
	}
	var lines []SuiteLine
	runLine := func(dir, command string) (Checked, error) {
		held = false
		defer giveBack()
		ran = append(ran, command)
		began := s.now()
		checked, err := s.Runner.RunLine(ctx, rig, dir, command)
		lines = append(lines, SuiteLine{Command: command, Seconds: math.Round(s.now().Sub(began).Seconds()*1000) / 1000})
		return checked, err
	}

	// lastLoad is the last reading of the host's load taken, the load a line is
	// taken to have started at (mw-gq6.1069).
	var lastLoad LoadReading
	if read {
		lastLoad = reading
	}
	if err := take("the suite"); err != nil {
		return SuiteReport{}, err
	}
	dir, head, release, err := s.cutTree(ctx, rig, rigDir, SuiteTreePrefix)
	if err != nil {
		return SuiteReport{}, err
	}
	defer release()

	started := s.now().UTC()
	gateWaited = 0 // what is waited from here on is inside the duration
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
	// attempt runs one command line, and runs it once more when it failed with the
	// host far over its cores (mw-gq6.1069): the load read at once after the
	// failure, or the one before the line, whichever is the busier, is the load it
	// failed at. Far over, it waits for the host to calm by the suite's own rule
	// and runs it again, and the second result stands; a host that never calms
	// ends it skipped, filed as nothing. Busy but not far over, the failure counts
	// at once and the note says the load. what names the line for the waits.
	attempt := func(dir, command, what string) (run suiteAttempt, err error) {
		if err := turn(what); err != nil {
			return suiteAttempt{}, err
		}
		run.Checked, run.Err = runLine(dir, command)
		if run.Err != nil || run.NotRun || run.Passed || s.Load == nil {
			return run, nil
		}
		busy := lastLoad
		if after, err := s.Load.Load(ctx); err == nil {
			lastLoad = after
			if after.Busy() {
				busy = after
			}
		}
		if !busy.FarOver() {
			if busy.Busy() {
				result.Notes = append(result.Notes, fmt.Sprintf("`%s` failed at load %.1f of %d cores, under %.0f times them, so the failure counts and it was not run again", command, busy.Load, busy.Cores, GateFarOver))
			}
			return run, nil
		}
		s.say("  retry   `%s` failed while %s was at load %.1f of %d cores: running it once more when it calms\n", command, s.Host, busy.Load, busy.Cores)
		log.section(command+" (first run, failed at load "+fmt.Sprintf("%.1f of %d", busy.Load, busy.Cores)+")", run.Output)
		calm, _, _, gaveUp, err := s.calmed(ctx, what+" again")
		if err != nil {
			return suiteAttempt{}, fmt.Errorf("waiting for %s to calm: %w", s.Host, err)
		}
		lastLoad = calm
		if gaveUp {
			run.Skipped, run.Busy = true, busy
			return run, nil
		}
		if err := turn(what + " again"); err != nil {
			return suiteAttempt{}, err
		}
		run.Checked, run.Err = runLine(dir, command)
		result.Notes = append(result.Notes, fmt.Sprintf("`%s` ran again after load %.1f of %d (%s was far over its cores when it failed): the second run stands", command, busy.Load, busy.Cores, s.Host))
		return run, nil
	}
	fmt.Fprintf(&log.b, "mw suite %s on %s at %s (%s), started %s\n\n", rig, s.Host, head.Commit, head.Branch, result.Started)
	s.say("  run     %s at %s on %s\n", rig, short(head.Commit), head.Branch)
	for _, phase := range phases {
		result.Commands = append(result.Commands, phase.command)
		whole, err := attempt(dir, phase.command, "the "+strings.ReplaceAll(phase.name, "_", " "))
		if err != nil {
			return SuiteReport{}, err
		}
		checked := whole.Checked
		if whole.Err != nil {
			return SuiteReport{}, fmt.Errorf("running `%s` in %s: %w", phase.command, dir, whole.Err)
		}
		log.section(phase.command, checked.Output)
		if checked.NotRun {
			return SuiteReport{}, fmt.Errorf("`%s` could not be run in %s, so there is no result to keep: %s", phase.command, dir, checked.Tail(5))
		}
		if checked.Passed {
			continue
		}
		files := FailingFiles(checked.Output)
		if whole.Skipped {
			// The host never calmed: the failure is the host's as far as is known, so
			// no file of it is run alone and none is filed.
			if len(files) == 0 {
				result.SkippedForLoad = append(result.SkippedForLoad, SuiteSkipped{Command: phase.command, Load: whole.Busy.Load, Cores: whole.Busy.Cores})
			}
			for _, failed := range files {
				result.SkippedForLoad = append(result.SkippedForLoad, SuiteSkipped{Command: phase.command, File: failed.File, Load: whole.Busy.Load, Cores: whole.Busy.Cores})
			}
			continue
		}
		if len(files) == 0 {
			said := fmt.Sprintf("`%s` failed and its output names no test file", phase.command)
			if short := FailingTestsShort(checked.Output); short != "" {
				said += " (" + short + ")"
			}
			result.Unnamed = append(result.Unnamed, said)
		}
		for _, failed := range files {
			file := SuiteFile{File: failed.File, Phase: phase.name, Test: failed.Test, Error: failed.Error}
			again, noWay := phase.alone(failed.File)
			if noWay != "" {
				file.Alone, file.Void = "recheck void: "+noWay, true
				result.Failing = append(result.Failing, file)
				continue
			}
			attempted, err := attempt(dir, again, "the re-run of "+failed.File)
			if err != nil {
				return SuiteReport{}, err
			}
			alone := attempted.Checked
			switch {
			case attempted.Skipped:
				log.section(again, alone.Output)
				result.SkippedForLoad = append(result.SkippedForLoad, SuiteSkipped{Command: again, File: failed.File, Load: attempted.Busy.Load, Cores: attempted.Busy.Cores})
				continue
			case attempted.Err != nil:
				file.Alone, file.Void = "recheck void: could not be run: "+attempted.Err.Error(), true
			case alone.NotRun:
				file.Alone, file.Void = "recheck void: could not be run: "+alone.Tail(3), true
			default:
				log.section(again, alone.Output)
				file.Alone, file.Flaky = "failed", false
				if n := testFilesRun(alone.Output); n > 1 {
					file.Alone, file.Void = fmt.Sprintf("recheck void: the runner ran %d test files, not %s alone", n, failed.File), true
				} else if alone.Passed {
					file.Alone, file.Flaky = "passed", true
				} else if why := voidRecheck(failed.File, alone); why != "" {
					file.Alone, file.Void = "recheck void: "+why, true
				}
			}
			result.Failing = append(result.Failing, file)
		}
	}
	result.DurationSeconds = math.Round(s.now().UTC().Sub(started).Seconds()*1000) / 1000
	result.GateWaitedSeconds = math.Round(gateWaited.Seconds()*1000) / 1000
	result.Lines = lines
	result.Passed = len(result.Failing) == 0 && len(result.Unnamed) == 0 && len(result.SkippedForLoad) == 0

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

// portInUse is what a run says when the server its tests need cannot start,
// because something already listens where it would: Playwright's webServer, or
// a server's own EADDRINUSE.
var portInUse = regexp.MustCompile(`(?i)is already used|address already in use|EADDRINUSE`)

// testFilesCount is how many test files a run says it ran: vitest's `Test Files
// 1 failed | 827 passed (828)` and jest's `Test Suites: 1 failed, 828 total`.
var testFilesCount = regexp.MustCompile(`Test Files\s.*\((\d+)\)|Test Suites:.*?(\d+) total`)

// testFilesRun is the number of test files output says the run ran, or 0 when it
// does not say. A re-run that says more than one was not run alone.
func testFilesRun(output string) int {
	found := testFilesCount.FindAllStringSubmatch(domain.StripANSI(output), -1)
	if len(found) == 0 {
		return 0
	}
	last := found[len(found)-1]
	n, _ := strconv.Atoi(last[1] + last[2])
	return n
}

// voidRecheck says why a run of file alone that failed says nothing of the file,
// or "" when it is the file failing again: a run whose server could not start,
// or one whose output names no failing test of the file, ran none of it.
func voidRecheck(file string, alone Checked) string {
	output := domain.StripANSI(alone.Output)
	for _, line := range strings.Split(output, "\n") {
		if portInUse.MatchString(line) {
			return "its server could not start: " + strings.TrimSpace(line)
		}
	}
	for _, failed := range FailingFiles(output) {
		if failed.File == file {
			return ""
		}
	}
	return "it failed without running a test of the file: " + strings.TrimSpace(domain.StripANSI(alone.Tail(1)))
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

// installers are the commands that fetch a rig's dependencies into a fresh tree.
var installers = regexp.MustCompile(`^(npm (ci|install)|yarn install|pnpm install)( |$)`)

// installStep is the first step of a rig's tests command when that step installs
// the rig's dependencies (`npm ci ... && npm test`), and empty when it does not.
func installStep(tests string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(tests), " && ")
	if installers.MatchString(first) {
		return first
	}
	return ""
}

// unitRunners are the steps of a tests command that run a rig's unit tests with
// a runner that takes a file to run alone: npm test (the file goes after --),
// pnpm or yarn test, and vitest or jest called directly. `npm run test:coverage`
// and a make target or a wrapper script are none of them.
var unitRunners = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*=\S* +)*(?:(npm (?:run )?test)|pnpm test|yarn test|npx (?:vitest|jest)|vitest|jest)(?: |$)`)

// unitStepFor is the step of a rig's tests command that runs the rig's unit
// tests, with file added so that it runs that one file alone, or "" when the
// command has no such step (mw-gq6.639). The rest of the line, the install
// aside, is the rest of the gate and is no part of a re-run of one file.
func unitStepFor(tests, file string) string {
	for _, step := range strings.Split(tests, " && ") {
		step = strings.TrimSpace(step)
		found := unitRunners.FindStringSubmatch(step)
		if found == nil {
			continue
		}
		if found[1] != "" && !strings.Contains(step, " -- ") {
			step += " --"
		}
		return step + " " + shellWord(file)
	}
	return ""
}

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
		// The tree is cut from main and has no dependencies; the whole gate installs
		// them before it runs the specs, and this re-run has to as well (mw-gq6.638).
		if install := installStep(s.Tests[rig]); install != "" {
			command = install + " && " + command
		}
	} else if tests := strings.TrimSpace(s.Tests[rig]); tests != "" {
		step := unitStepFor(tests, spec)
		if step == "" {
			return SpecRecheck{}, fmt.Errorf("the rig %s has no way to run one unit-test file alone: its [tests] command has no unit runner step (npm test, vitest, jest) to give %s to", rig, spec)
		}
		command = step
		if install := installStep(tests); install != "" {
			command = install + " && " + step
		}
	} else {
		return SpecRecheck{}, fmt.Errorf("the rig %s has no [tests] entry in the config file: there is no command to run %s with", rig, spec)
	}
	dir, head, release, err := s.cutTree(ctx, rig, rigDir, RecheckTreePrefix)
	if err != nil {
		return SpecRecheck{}, err
	}
	defer release()

	checked, err := s.Runner.RunLine(ctx, rig, dir, command)
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

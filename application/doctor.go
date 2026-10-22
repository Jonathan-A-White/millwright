package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultTmpLeftoversBudgetBytes is how many bytes of this factory's own dead
// temp leftovers — a killed bd's dolt spool files, /tmp/bd, a stale
// /tmp/claude-0/<session> dir — mw doctor's tmp-leftovers check holds a host
// to before it clears them, when nothing says otherwise. It is the same
// 200_000_000 infrastructure/config.DefaultDoctorTmpLeftoversBudgetBytes reads
// as.
const DefaultTmpLeftoversBudgetBytes = 200_000_000

// DefaultGoBuildBudgetBytes is how many bytes the tmp-leftovers check lets
// ~/.cache/go-build grow to before `go clean -cache`, when nothing says
// otherwise: 3-5 Builders' go test keep it at several hundred MB in normal
// work, which is not a fault (mw-gq6.478). It is the same 2_000_000_000
// infrastructure/config.DefaultDoctorGoBuildBudgetBytes reads as.
const DefaultGoBuildBudgetBytes = 2_000_000_000

// DoctorFaultExit is the status mw doctor leaves with when this run ends with
// a check faulty and uncured — damped, or its cure failed. It is
// WatchWakeExit's neighbour: the same "somebody should look" signal a timer's
// journal already reads from mw watch.
const DoctorFaultExit = WatchWakeExit

// Verdict is what a DoctorCheck's Probe found: OK, faulty (with a reason a person
// or the log can read), or, when the probe itself could not tell, CannotTell.
type Verdict int

// The three things a Probe may find.
const (
	DoctorOK Verdict = iota
	DoctorFaulty
	DoctorCannotTell
)

// String is the word Verdict is logged and printed as.
func (v Verdict) String() string {
	switch v {
	case DoctorOK:
		return "ok"
	case DoctorFaulty:
		return "faulty"
	case DoctorCannotTell:
		return "cannot-tell"
	default:
		return "unknown"
	}
}

// DoctorCheck is one thing mw doctor knows how to find wrong and put right,
// the same shape for every check: a name, a read-only probe, a cure the
// probe's finding and the damper allow, a damper (how long between cures and
// how many in one fault episode), and a way back a person reads if the cure
// is ever in doubt.
type DoctorCheck interface {
	// Name identifies the check: in the log, on the command line
	// (`mw doctor <name>`) and as the key its episode state is kept under.
	Name() string
	// Probe reports the check's verdict and, for anything but ok, why. It never
	// changes anything: mw doctor's dry run relies on that to be true.
	Probe(ctx context.Context) (Verdict, string)
	// Cure is run only when Probe says faulty and the damper allows it.
	Cure(ctx context.Context) error
	// Damper is the minimum wait between two cures and the most cures one fault
	// episode may spend before mw doctor stops trying and calls it damped. An
	// episode ends when Probe next says ok, or, for a DoctorCureEndsEpisode, with
	// a cure that put the fault right.
	Damper() (wait time.Duration, capPerEpisode int)
	// WayBack is printed by --dry-run and written to the log beside every cure:
	// what a person does if the cure turns out to be wrong.
	WayBack() string
}

// DoctorCoolingQuiet is an optional addition to a DoctorCheck, for a check
// whose cure really does the work and is cheap to repeat: the fault coming
// back inside the damper's minimum wait, after a cure that succeeded, is the
// cure working against a cause that keeps refilling (a cache five Builders
// keep growing), not news. Such a damped tick is logged as damped but leaves
// the run's status alone and writes no note, so it wakes nobody. The cap
// ending an episode, and a cure that failed, are still left faulty.
type DoctorCoolingQuiet interface {
	QuietWhileCooling() bool
}

// DoctorCureEndsEpisode is an optional addition to a DoctorCheck, for a check
// whose fault's cause regrows under normal load (a cache several Builders
// keep growing): a cure that returned no error and, by the check's own
// account, put the fault right ends the episode, so the next growth starts a
// new one with its whole cap rather than spending this one's. The damper's
// wait still holds between two cures. A cure that fails, or one the check
// says put nothing right, is counted against the cap as for any check, so a
// real fault is still damped (mw-gq6.478).
type DoctorCureEndsEpisode interface {
	// CureEndedEpisode is read only after Cure returned no error: whether
	// that cure put the fault right.
	CureEndedEpisode() bool
}

// DoctorChecks is the table mw doctor works down, in order.
type DoctorChecks []DoctorCheck

// DoctorEpisode is what mw doctor remembers of one check between runs, for as
// long as it keeps finding that check faulty: when the fault was first seen,
// how many cures it has spent on it, and when the last one ran. The zero
// value is a check with no fault episode open.
type DoctorEpisode struct {
	FirstFaulty time.Time
	Cures       int
	LastCure    time.Time

	// LastCureFailed is whether the cure that set LastCure returned an error.
	// A check that is quiet while cooling stays loud inside the wait after a
	// failed cure.
	LastCureFailed bool

	// LastOKReason is the reason an ok verdict was last logged with. A check
	// that answers ok with a reason — "n/a: none of these units is installed
	// here", say — is said once, in the log and in a kv write; a later run
	// that answers ok with the same reason costs neither, while nothing has
	// changed. Empty for a check whose last logged ok carried no reason, or
	// whose episode has never been saved.
	LastOKReason string

	// LastCannotTellReason is the same said-once treatment LastOKReason gives
	// an ok with a reason, for cannot-tell: a check that cannot tell for the
	// same reason on every run — a host with no powershell.exe, say — is
	// logged and noted only the first time, or again once the reason
	// changes; a note already written for it is left in place, unrewritten,
	// while the reason repeats. Empty for a check whose last logged
	// cannot-tell carried no reason, or whose episode has never been saved.
	LastCannotTellReason string

	// DampedNoted is whether this episode's note for reaching its cap has been
	// written. The cap ending an episode is noted once, when it is first
	// reached, not on every run after it: a reason that carries a number
	// which moves between runs — a size in bytes, say — would otherwise
	// rewrite the note each time and wake the Millhand again for the same
	// fault. The log line is still appended every run. The episode ending,
	// when the probe next says ok, forgets it, so the next episode notes
	// again.
	DampedNoted bool

	// SeenPaths is a check's own memory of paths it saw last run and is
	// waiting to see again before it says anything: vault-dirty keeps the
	// tracked files outside runs/ it found modified, so a seat's commit
	// landing a few seconds after one run does not wake the Millhand. Empty
	// when the last run saw none.
	SeenPaths []string
}

// DoctorState is where mw doctor keeps each check's episode between runs.
type DoctorState interface {
	// Load reads a check's episode, the zero value if none is kept.
	Load(ctx context.Context, check string) (DoctorEpisode, error)
	// Save replaces what is kept of a check's episode.
	Save(ctx context.Context, check string, episode DoctorEpisode) error
	// Reset forgets a check's episode: its probe found it ok.
	Reset(ctx context.Context, check string) error
}

// DoctorLog is the one place mw doctor writes what it found, one line at a
// time, dated by the caller.
type DoctorLog interface {
	Append(ctx context.Context, line string) error

	// Read is every line the log holds, oldest first; none, and no error, for
	// a log nothing has been written to yet. Notes read a check's own recent
	// lines out of it.
	Read(ctx context.Context) ([]string, error)
}

// DoctorNotePrefix is the prefix every check's own note is kept under.
const DoctorNotePrefix = "doctor."

// DoctorNoteKey is the note kept for one check on one host:
// DoctorNoteKey("laptop", "wifi") == "doctor.laptop.wifi". Two hosts run the
// doctor against the one shared config table, so the host is part of the
// key: without it, one host's check turning ok clears another host's still-
// faulty note of the same check.
func DoctorNoteKey(host, check string) string { return DoctorNotePrefix + host + "." + check }

// ParseDoctorNoteKey is DoctorNoteKey's inverse: the host and check a note's
// key names, and whether it parses at all. A key with no host — the shape
// written before this host-qualified form — does not parse: ok is false, so
// a caller does not mistake a leftover host-less row for any particular
// host's.
func ParseDoctorNoteKey(key string) (host, check string, ok bool) {
	rest := strings.TrimPrefix(key, DoctorNotePrefix)
	if rest == key {
		return "", "", false
	}
	host, check, found := strings.Cut(rest, ".")
	if !found || host == "" || check == "" {
		return "", "", false
	}
	return host, check, true
}

// DoctorNoteLogLines is how many of a check's own most recent log lines its
// note carries, so a person or the Millhand reading the note has the recent
// history without opening the log itself.
const DoctorNoteLogLines = 3

// DoctorNotes is where the doctor writes and clears the note that says a
// check needs a person's attention, and where mw millhand tick later finds
// every note under DoctorNotePrefix and remembers which it has already woken
// the Millhand for. It is TrackerSync's own Note, SetNote and ClearNote,
// narrowed, plus a way to list every note under one prefix without knowing
// the checks' names ahead of time — the same shape SweepNotes is, with that
// one addition. The doctor may be offline: a write or clear that fails is
// logged as note-failed and the run goes on; the next run retries.
type DoctorNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error

	// NotesWithPrefix reports every note whose key has the given prefix, key
	// to value. A nil map, no error, is no notes under that prefix.
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
}

// DoctorFault is what Run returns when this pass ends with a check faulty and
// uncured. Not a plain failure: the line naming it has already been printed
// and logged, and ExitStatus reads it into DoctorFaultExit.
type DoctorFault struct {
	// Line names the check(s) left faulty and uncured this run.
	Line string
}

// Error is the finding, so a caller that prints an error prints it.
func (d *DoctorFault) Error() string { return "left faulty and uncured: " + d.Line }

// DoctorFaults reports whether err is a doctor run that ended with something
// faulty and uncured.
func DoctorFaults(err error) (*DoctorFault, bool) {
	var fault *DoctorFault
	return fault, errors.As(err, &fault)
}

// DoctorResult is what one check's run found.
type DoctorResult struct {
	Check string
	// Verdict is one of: ok, cannot-tell, cured, damped, cure-failed, would-cure.
	Verdict string
	Reason  string
	WayBack string
	// Faulty is whether this check is left faulty and uncured — damped or its
	// cure failed, except a damped tick of a DoctorCoolingQuiet check held
	// only by the wait after a good cure — so this run's exit status should say so.
	Faulty bool
}

// line is how one result is logged, dated by the caller: "<check> <verdict>
// [<reason>][ <wayback>]", the way back only ever beside a cure.
func (r DoctorResult) line() string {
	body := r.Check + " " + r.Verdict
	if r.Reason != "" {
		body += " " + r.Reason
	}
	if r.Verdict == "cured" && r.WayBack != "" {
		body += " " + r.WayBack
	}
	return body
}

// String is how one result reads in the report Run prints.
func (r DoctorResult) String() string {
	switch r.Verdict {
	case "ok":
		if r.Reason != "" {
			return r.Check + ": ok (" + r.Reason + ")"
		}
		return r.Check + ": ok"
	case "cannot-tell":
		return r.Check + ": cannot-tell (" + r.Reason + ")"
	case "cured":
		return r.Check + ": cured (" + r.Reason + ") — way back: " + r.WayBack
	case "damped":
		return r.Check + ": damped (" + r.Reason + ") — way back: " + r.WayBack
	case "cure-failed":
		return r.Check + ": cure-failed (" + r.Reason + ")"
	case "would-cure":
		return r.Check + ": faulty (" + r.Reason + ") — would run its cure; way back: " + r.WayBack
	default:
		return r.Check + ": " + r.Verdict
	}
}

// DoctorReport is what one run of mw doctor found, one result per check.
type DoctorReport struct {
	Results []DoctorResult
}

// Faulty reports whether any check in this report was left faulty and
// uncured.
func (r DoctorReport) Faulty() bool {
	for _, result := range r.Results {
		if result.Faulty {
			return true
		}
	}
	return false
}

// FaultyNames is the checks left faulty and uncured, in the order they ran.
func (r DoctorReport) FaultyNames() []string {
	var names []string
	for _, result := range r.Results {
		if result.Faulty {
			names = append(names, result.Check)
		}
	}
	return names
}

// String is the report as a person reads it, one line per check.
func (r DoctorReport) String() string {
	var b strings.Builder
	for _, result := range r.Results {
		b.WriteString(result.String())
		b.WriteString("\n")
	}
	return b.String()
}

// DamperCounter reads the DAMPERS alarms' counts, for mw doctor to repeat.
// DamperSurvey is the one reader; it reads and writes nothing.
type DamperCounter interface {
	DamperCounts(ctx context.Context) (DampersReport, error)
}

// Doctor runs a table of checks, each its own probe, cure, damper and way
// back, and logs what it found on this host. Nothing in it needs the network
// except a check's own probe or cure; it writes nothing to the tracker and
// sends no mail of its own (a check's cure may: starved-rig mails the Mayor).
type Doctor struct {
	Checks DoctorChecks
	State  DoctorState
	Log    DoctorLog

	// Notes is where a check needing a person's attention is written as a
	// note, and cleared once it is ok again. A nil Notes writes and clears
	// nothing, so a caller with nowhere to keep one still runs.
	Notes DoctorNotes

	// Dampers, when set, is read after the checks for the one line that repeats
	// the DAMPERS counts mw status judges its alarms by. A reading that fails
	// is said on that line and changes nothing else. Nil leaves the line out.
	Dampers DamperCounter

	// Host is this host, part of the key a check's own note is written and
	// cleared under: two hosts share one config table, so without it one
	// host's doctor would clear another's still-faulty note of the same
	// check.
	Host string

	// Now is the clock episodes and log lines are read and dated by. The zero
	// value reads the real one.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer

	// noted is the keys of this host's notes as Run found them: a check that
	// is ok clears its note only when it is there (nil, when the listing
	// failed or there is no Notes, clears every one).
	noted *notedKeys
}

// notedKeys is the set of this host's doctor notes held at the start of a run.
// Each clear is a bd of its own, several connections to the beads server, so a
// run that cleared the note of every ok check was one bd for each of the
// table's checks every five minutes on every host (mw-gq6.608).
type notedKeys struct{ held map[string]bool }

type doctorDryRunKey struct{}

// DoctorDryRun says whether ctx is that of a dry run. Probe runs in a dry run
// too, so a probe that tells the Governor something has ended (a check's way
// back) asks this first and sends nothing.
func DoctorDryRun(ctx context.Context) bool {
	dry, _ := ctx.Value(doctorDryRunKey{}).(bool)
	return dry
}

// Run works every check in the table, or, with name set, only the one it
// names. With dryRun it prints what each faulty check would do — its reason
// and its way back — and changes nothing: no cure runs, no state is written,
// no log line is appended. It leaves with a *DoctorFault, read by ExitStatus
// into DoctorFaultExit, when any check ends the run faulty and uncured.
func (d Doctor) Run(ctx context.Context, name string, dryRun bool) (DoctorReport, error) {
	switch {
	case len(d.Checks) == 0:
		return DoctorReport{}, fmt.Errorf("doctoring: there is nothing to check")
	case d.State == nil:
		return DoctorReport{}, fmt.Errorf("doctoring: there is nowhere to keep what each check remembers")
	case d.Log == nil:
		return DoctorReport{}, fmt.Errorf("doctoring: there is nowhere to log what was found")
	}

	checks, err := d.selected(name)
	if err != nil {
		return DoctorReport{}, err
	}

	if dryRun {
		ctx = context.WithValue(ctx, doctorDryRunKey{}, true)
	} else {
		d.noted = d.readNoted(ctx)
	}
	var report DoctorReport
	for _, check := range checks {
		result, err := d.one(ctx, check, dryRun)
		if err != nil {
			return report, err
		}
		report.Results = append(report.Results, result)
	}

	if d.Out != nil {
		fmt.Fprint(d.Out, report.String())
		if d.Dampers != nil && name == "" {
			if dampers, err := d.Dampers.DamperCounts(ctx); err != nil {
				fmt.Fprintf(d.Out, "dampers: could not be read (%v)\n", err)
			} else {
				fmt.Fprintln(d.Out, dampers.CountsLine())
			}
		}
	}
	if report.Faulty() {
		return report, &DoctorFault{Line: strings.Join(report.FaultyNames(), ", ")}
	}
	return report, nil
}

// selected is the checks Run works: every one in the table, or, named, the
// one whose Name matches.
func (d Doctor) selected(name string) (DoctorChecks, error) {
	if name == "" {
		return d.Checks, nil
	}
	for _, check := range d.Checks {
		if check.Name() == name {
			return DoctorChecks{check}, nil
		}
	}
	return nil, fmt.Errorf("doctoring: no such check: %s (have: %s)", name, strings.Join(d.names(), ", "))
}

// names is every check's name, in table order.
func (d Doctor) names() []string {
	names := make([]string, len(d.Checks))
	for i, check := range d.Checks {
		names[i] = check.Name()
	}
	return names
}

// one runs a single check: its probe and, when it is due, its cure, and logs
// and reports what happened.
func (d Doctor) one(ctx context.Context, check DoctorCheck, dryRun bool) (DoctorResult, error) {
	name := check.Name()
	verdict, reason := check.Probe(ctx)

	switch verdict {
	case DoctorOK:
		return d.ok(ctx, name, reason, dryRun)

	case DoctorCannotTell:
		return d.cannotTell(ctx, name, reason, dryRun)
	}

	return d.faulty(ctx, check, reason, dryRun)
}

// ok is the DoctorOK half of one. A plain ok, with no reason, is logged and
// its note cleared every run, as it always has been: cheap, and a reader of
// the log can trust that a check's silence since its last "ok" line means
// nothing has run since. An ok with a reason — a check explaining why there
// is nothing to do — is said once: it is logged and its note cleared only the
// first time, or again if the reason changes, and the reason said is
// remembered in the check's own episode so a later run with nothing new to
// say costs neither a log line nor a kv write.
func (d Doctor) ok(ctx context.Context, name, reason string, dryRun bool) (DoctorResult, error) {
	result := DoctorResult{Check: name, Verdict: "ok", Reason: reason}
	if dryRun {
		return result, nil
	}

	if reason == "" {
		if err := d.State.Reset(ctx, name); err != nil {
			return DoctorResult{}, fmt.Errorf("resetting %s's doctor state: %w", name, err)
		}
		if err := d.append(ctx, result); err != nil {
			return DoctorResult{}, err
		}
		d.clearNote(ctx, name)
		return result, nil
	}

	episode, err := d.State.Load(ctx, name)
	if err != nil {
		return DoctorResult{}, fmt.Errorf("reading %s's doctor state: %w", name, err)
	}
	if episode.LastOKReason == reason {
		return result, nil
	}

	if err := d.append(ctx, result); err != nil {
		return DoctorResult{}, err
	}
	d.clearNote(ctx, name)
	if err := d.State.Save(ctx, name, DoctorEpisode{LastOKReason: reason}); err != nil {
		return DoctorResult{}, fmt.Errorf("saving %s's doctor state: %w", name, err)
	}
	return result, nil
}

// cannotTell is the DoctorCannotTell half of one, said once the same way an
// ok-with-reason is: logged and noted only the first time, or again once the
// reason changes. A repeat of the same reason costs neither a log line nor a
// kv write, and leaves a note already written for it in place, unrewritten.
func (d Doctor) cannotTell(ctx context.Context, name, reason string, dryRun bool) (DoctorResult, error) {
	result := DoctorResult{Check: name, Verdict: "cannot-tell", Reason: reason}
	if dryRun {
		return result, nil
	}

	episode, err := d.State.Load(ctx, name)
	if err != nil {
		return DoctorResult{}, fmt.Errorf("reading %s's doctor state: %w", name, err)
	}
	if episode.LastCannotTellReason == reason {
		return result, nil
	}

	if err := d.append(ctx, result); err != nil {
		return DoctorResult{}, err
	}
	d.writeNote(ctx, name, result)
	episode.LastCannotTellReason = reason
	if err := d.State.Save(ctx, name, episode); err != nil {
		return DoctorResult{}, fmt.Errorf("saving %s's doctor state: %w", name, err)
	}
	return result, nil
}

// faulty is the DoctorFaulty half of one: decide, from the check's damper and
// what its episode remembers, whether this fault is cured, damped, or, in a
// dry run, would be either.
//
// WayBack is read fresh for each outcome rather than once up front: a damped
// or dry-run report never runs Cure, so it reads whatever WayBack can say
// before any cure; a cured report reads it only after Cure has run, so a
// check whose way back names what the cure actually did — a vault-dirty
// commit's own hash, say — can report that rather than a guess.
func (d Doctor) faulty(ctx context.Context, check DoctorCheck, reason string, dryRun bool) (DoctorResult, error) {
	name := check.Name()

	episode, err := d.State.Load(ctx, name)
	if err != nil {
		return DoctorResult{}, fmt.Errorf("reading %s's doctor state: %w", name, err)
	}
	wait, capPerEpisode := check.Damper()
	capped := capPerEpisode > 0 && episode.Cures >= capPerEpisode
	cooling := !episode.LastCure.IsZero() && d.now().Sub(episode.LastCure) < wait
	damped := capped || cooling

	if damped {
		result := DoctorResult{Check: name, Verdict: "damped", Reason: reason, WayBack: check.WayBack(), Faulty: true}
		// Held only by the minimum wait after a cure that worked: logged, but
		// no one's to look at.
		if quiet, ok := check.(DoctorCoolingQuiet); ok && quiet.QuietWhileCooling() && !capped && !episode.LastCureFailed {
			result.Faulty = false
		}
		if !dryRun {
			if err := d.append(ctx, result); err != nil {
				return DoctorResult{}, err
			}
			// Only the cap ending an episode is a person's to look at: the
			// ordinary cooldown between two cures is expected, and passes on
			// its own.
			if capped && !episode.DampedNoted {
				if d.writeNote(ctx, name, result) {
					episode.DampedNoted = true
					if err := d.State.Save(ctx, name, episode); err != nil {
						return DoctorResult{}, fmt.Errorf("saving %s's doctor state: %w", name, err)
					}
				}
			}
		}
		return result, nil
	}

	if dryRun {
		return DoctorResult{Check: name, Verdict: "would-cure", Reason: reason, WayBack: check.WayBack()}, nil
	}

	cureErr := check.Cure(ctx)
	episode.Cures++
	episode.LastCure = d.now()
	episode.LastCureFailed = cureErr != nil
	if episode.FirstFaulty.IsZero() {
		episode.FirstFaulty = d.now()
	}
	if ends, ok := check.(DoctorCureEndsEpisode); ok && cureErr == nil && ends.CureEndedEpisode() {
		// The fault is put right: its episode ends here, keeping only when
		// this cure ran, for the wait before the next.
		episode.Cures, episode.FirstFaulty, episode.DampedNoted = 0, time.Time{}, false
	}
	if err := d.State.Save(ctx, name, episode); err != nil {
		return DoctorResult{}, fmt.Errorf("saving %s's doctor state: %w", name, err)
	}

	if cureErr != nil {
		result := DoctorResult{Check: name, Verdict: "cure-failed", Reason: cureErr.Error(), Faulty: true}
		if err := d.append(ctx, result); err != nil {
			return DoctorResult{}, err
		}
		// The first failed cure of an episode is not yet a person's to look
		// at: the damper gives it another try first. The second is.
		if episode.Cures >= 2 {
			d.writeNote(ctx, name, result)
		}
		return result, nil
	}

	result := DoctorResult{Check: name, Verdict: "cured", Reason: reason, WayBack: check.WayBack()}
	if err := d.append(ctx, result); err != nil {
		return DoctorResult{}, err
	}
	return result, nil
}

// append writes one result to the log, dated.
func (d Doctor) append(ctx context.Context, result DoctorResult) error {
	dated := d.now().UTC().Format(time.RFC3339) + " " + result.line()
	if err := d.Log.Append(ctx, dated); err != nil {
		return fmt.Errorf("appending to the doctor log: %w", err)
	}
	return nil
}

// writeNote sets a check's own note to this result, dated, with its last
// DoctorNoteLogLines lines from the log beside it. The doctor may be
// offline: a write that fails is logged as note-failed, and the run goes on
// unchanged — the exit status is decided already, by the result's own
// Faulty, not by whether the note got written. The next run retries. It says
// whether the note is written, or there is nowhere to write one.
func (d Doctor) writeNote(ctx context.Context, check string, result DoctorResult) bool {
	if d.Notes == nil {
		return true
	}
	value := d.now().UTC().Format(time.RFC3339) + " " + result.Verdict
	if result.Reason != "" {
		value += " " + result.Reason
	}
	value += " | last log lines: " + strings.Join(d.checkLogLines(ctx, check), " | ")
	if err := d.Notes.SetNote(ctx, DoctorNoteKey(d.Host, check), value); err != nil {
		_ = d.append(ctx, DoctorResult{Check: check, Verdict: "note-failed", Reason: oneLine(err.Error())})
		return false
	}
	if d.noted != nil {
		d.noted.held[DoctorNoteKey(d.Host, check)] = true
	}
	return true
}

// clearNote takes back a check's own note, now that its probe says ok. A
// clear that fails is logged as note-failed the same way a write is.
func (d Doctor) clearNote(ctx context.Context, check string) {
	if d.Notes == nil {
		return
	}
	key := DoctorNoteKey(d.Host, check)
	if d.noted != nil && !d.noted.held[key] {
		return
	}
	if err := d.Notes.ClearNote(ctx, key); err != nil {
		_ = d.append(ctx, DoctorResult{Check: check, Verdict: "note-failed", Reason: oneLine(err.Error())})
		return
	}
	if d.noted != nil {
		delete(d.noted.held, key)
	}
}

// readNoted lists this host's doctor notes in one read, or says nil when there
// is none to list or the listing fails, and every ok check then clears its note
// as it always did.
func (d Doctor) readNoted(ctx context.Context) *notedKeys {
	if d.Notes == nil {
		return nil
	}
	found, err := d.Notes.NotesWithPrefix(ctx, DoctorNoteKey(d.Host, ""))
	if err != nil {
		return nil
	}
	noted := &notedKeys{held: make(map[string]bool, len(found))}
	for key := range found {
		noted.held[key] = true
	}
	return noted
}

// checkLogLines is this check's own last DoctorNoteLogLines lines out of the
// log, oldest first; none of them, on a log Read cannot answer, is not a
// failure — the note is written with what there is.
func (d Doctor) checkLogLines(ctx context.Context, check string) []string {
	lines, err := d.Log.Read(ctx)
	if err != nil {
		return nil
	}
	var matched []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == check {
			matched = append(matched, line)
		}
	}
	if len(matched) > DoctorNoteLogLines {
		matched = matched[len(matched)-DoctorNoteLogLines:]
	}
	return matched
}

// now is the clock episodes and log lines are read and dated by.
func (d Doctor) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// LicenceEntry is one collection of the licence map the postern backend
// prints for `postern rights --json`: whether a licence in it is the cockpit's
// or an app's (App names it; a comma joins more than one) and the routes a key
// holding it opens. Postern decides all of it; mw only reads it.
type LicenceEntry struct {
	Collection string   `json:"collection"`
	Kind       string   `json:"kind"`
	App        string   `json:"app"`
	Routes     []string `json:"routes"`
}

// ParseRightsJSON reads the output of `postern rights --json`:
// {"collections":[{"collection","kind","app","routes"}]}. Text that is not
// that map, or names no collection, or a kind other than cockpit and app, is
// refused: it is an older postern, or not postern, and nothing is guessed.
func ParseRightsJSON(data []byte) ([]LicenceEntry, error) {
	var doc struct {
		Collections []LicenceEntry `json:"collections"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("its output is not a licence map: %w", err)
	}
	if len(doc.Collections) == 0 {
		return nil, fmt.Errorf("its output names no collection")
	}
	for _, entry := range doc.Collections {
		if entry.Collection == "" || (entry.Kind != "cockpit" && entry.Kind != "app") {
			return nil, fmt.Errorf("its output has the collection %q of kind %q, which is neither cockpit nor app", entry.Collection, entry.Kind)
		}
	}
	return doc.Collections, nil
}

// LicenceReport is what mw doctor licences prints: a line per collection,
// then a warning for each thing not as it should be.
type LicenceReport struct {
	Lines    []string
	Warnings []string
}

// Faulty reports whether anything is warned of.
func (r LicenceReport) Faulty() bool { return len(r.Warnings) > 0 }

// String is the report as printed.
func (r LicenceReport) String() string {
	var b strings.Builder
	for _, line := range r.Lines {
		b.WriteString(line + "\n")
	}
	for _, warning := range r.Warnings {
		b.WriteString("WARNING: " + warning + "\n")
	}
	if !r.Faulty() {
		b.WriteString("ok: every licence collection opens what it should\n")
	}
	return b.String()
}

// ReportLicences judges the backend's licence map and changes nothing. It
// warns of what the map can show: a collection listed more than once (the
// backend lists its cockpit collections, then its app collections, so one in
// both is listed twice, as an app's) and a collection with more than one app.
func ReportLicences(entries []LicenceEntry) LicenceReport {
	var report LicenceReport
	var order []string
	first := map[string]LicenceEntry{}
	count := map[string]int{}
	for _, entry := range entries {
		if count[entry.Collection] == 0 {
			order = append(order, entry.Collection)
			first[entry.Collection] = entry
		}
		count[entry.Collection]++
	}
	for _, collection := range order {
		entry := first[collection]
		where := "cockpit"
		if entry.Kind == "app" {
			where = "app " + entry.App
		}
		report.Lines = append(report.Lines, fmt.Sprintf("%s -> %s: a key holding it opens %s", collection, where, strings.Join(entry.Routes, ", ")))
	}
	for _, collection := range order {
		entry := first[collection]
		apps := licenceList(entry.App)
		if count[collection] > 1 {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%s is a cockpit collection (POSTERN_COLLECTIONS) and is mapped to app %s (POSTERN_APPS): a key holding a licence in it, the Governor's cockpit key if he holds one there, is an app key only and loses the cockpit; drop the mapping or the collection",
				collection, strings.Join(apps, ", ")))
		}
		if len(apps) > 1 {
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%s is mapped to two apps (%s) in POSTERN_APPS; one collection is one app's",
				collection, strings.Join(apps, " and ")))
		}
	}
	return report
}

func licenceList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// StaleLandingsAge is how old a landing directory is let grow before mw doctor
// names it: a landing is made and taken away inside one close-out, minutes
// long, so one a day old was left behind (mw-gq6.698).
const StaleLandingsAge = 24 * time.Hour

// StaleLandingsCheck is the mw doctor check that names the throwaway landing
// directories (LandingPrefix) older than StaleLandingsAge, each with its id,
// the part of the name after the prefix. A landing closes its own directory;
// one still here is one a close-out could not take away. It removes nothing:
// git may still hold a record of the worktree, so a person runs `git worktree
// remove --force <dir>` in the rig, and the check says so.
type StaleLandingsCheck struct {
	// Dirs are the directories landings are made in: the home directory and the
	// directory each rig lives in. A directory that is not there, or named
	// twice, is read once or not at all.
	Dirs []string
	// Now is the clock; nil reads time.Now.
	Now func() time.Time
}

var _ DoctorCheck = (*StaleLandingsCheck)(nil)

// Name implements DoctorCheck.
func (c *StaleLandingsCheck) Name() string { return "stale-landings" }

// Probe implements DoctorCheck: cannot-tell, naming the stale directories,
// when there are any; ok otherwise. The reason holds no age, so that the same
// directories said again read the same and are said once.
func (c *StaleLandingsCheck) Probe(context.Context) (Verdict, string) {
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	var stale []string
	seen := map[string]bool{}
	for _, dir := range c.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), LandingPrefix) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil || seen[path] || now.Sub(info.ModTime()) <= StaleLandingsAge {
				continue
			}
			seen[path] = true
			stale = append(stale, fmt.Sprintf("%s (%s)", strings.TrimPrefix(entry.Name(), LandingPrefix), path))
		}
	}
	if len(stale) == 0 {
		return DoctorOK, ""
	}
	sort.Strings(stale)
	return DoctorCannotTell, fmt.Sprintf("%d landing directories older than a day, left by a close-out that could not take them away (id, path): %s; remove one with `git worktree remove --force <path>` in its rig, then `git worktree prune`",
		len(stale), strings.Join(stale, ", "))
}

// Cure implements DoctorCheck. Nothing is cured: Probe never says faulty.
func (c *StaleLandingsCheck) Cure(context.Context) error { return nil }

// Damper implements DoctorCheck.
func (c *StaleLandingsCheck) Damper() (time.Duration, int) { return time.Hour, 1 }

// WayBack implements DoctorCheck.
func (c *StaleLandingsCheck) WayBack() string { return "nothing was changed" }

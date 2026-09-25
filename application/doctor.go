package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

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
	// episode ends when Probe next says ok.
	Damper() (wait time.Duration, capPerEpisode int)
	// WayBack is printed by --dry-run and written to the log beside every cure:
	// what a person does if the cure turns out to be wrong.
	WayBack() string
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

	// LastOKReason is the reason an ok verdict was last logged with. A check
	// that answers ok with a reason — "n/a: none of these units is installed
	// here", say — is said once, in the log and in a kv write; a later run
	// that answers ok with the same reason costs neither, while nothing has
	// changed. Empty for a check whose last logged ok carried no reason, or
	// whose episode has never been saved.
	LastOKReason string
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
	// cure failed — so this run's exit status should say so.
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

// Doctor runs a table of checks, each its own probe, cure, damper and way
// back, and logs what it found on this host. Nothing in it needs the network
// except a check's own probe or cure; it writes nothing to the tracker and
// sends no mail.
type Doctor struct {
	Checks DoctorChecks
	State  DoctorState
	Log    DoctorLog

	// Notes is where a check needing a person's attention is written as a
	// note, and cleared once it is ok again. A nil Notes writes and clears
	// nothing, so a caller with nowhere to keep one still runs.
	Notes DoctorNotes

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
		result := DoctorResult{Check: name, Verdict: "cannot-tell", Reason: reason}
		if !dryRun {
			if err := d.append(ctx, result); err != nil {
				return DoctorResult{}, err
			}
			d.writeNote(ctx, name, result)
		}
		return result, nil
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
		if !dryRun {
			if err := d.append(ctx, result); err != nil {
				return DoctorResult{}, err
			}
			// Only the cap ending an episode is a person's to look at: the
			// ordinary cooldown between two cures is expected, and passes on
			// its own.
			if capped {
				d.writeNote(ctx, name, result)
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
	if episode.FirstFaulty.IsZero() {
		episode.FirstFaulty = d.now()
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
// Faulty, not by whether the note got written. The next run retries.
func (d Doctor) writeNote(ctx context.Context, check string, result DoctorResult) {
	if d.Notes == nil {
		return
	}
	value := d.now().UTC().Format(time.RFC3339) + " " + result.Verdict
	if result.Reason != "" {
		value += " " + result.Reason
	}
	value += " | last log lines: " + strings.Join(d.checkLogLines(ctx, check), " | ")
	if err := d.Notes.SetNote(ctx, DoctorNoteKey(d.Host, check), value); err != nil {
		_ = d.append(ctx, DoctorResult{Check: check, Verdict: "note-failed", Reason: oneLine(err.Error())})
	}
}

// clearNote takes back a check's own note, now that its probe says ok. A
// clear that fails is logged as note-failed the same way a write is.
func (d Doctor) clearNote(ctx context.Context, check string) {
	if d.Notes == nil {
		return
	}
	if err := d.Notes.ClearNote(ctx, DoctorNoteKey(d.Host, check)); err != nil {
		_ = d.append(ctx, DoctorResult{Check: check, Verdict: "note-failed", Reason: oneLine(err.Error())})
	}
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

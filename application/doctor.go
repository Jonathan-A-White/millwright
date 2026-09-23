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
		if !dryRun {
			if err := d.State.Reset(ctx, name); err != nil {
				return DoctorResult{}, fmt.Errorf("resetting %s's doctor state: %w", name, err)
			}
			if err := d.append(ctx, DoctorResult{Check: name, Verdict: "ok"}); err != nil {
				return DoctorResult{}, err
			}
		}
		return DoctorResult{Check: name, Verdict: "ok"}, nil

	case DoctorCannotTell:
		result := DoctorResult{Check: name, Verdict: "cannot-tell", Reason: reason}
		if !dryRun {
			if err := d.append(ctx, result); err != nil {
				return DoctorResult{}, err
			}
		}
		return result, nil
	}

	return d.faulty(ctx, check, reason, dryRun)
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
	damped := (capPerEpisode > 0 && episode.Cures >= capPerEpisode) ||
		(!episode.LastCure.IsZero() && d.now().Sub(episode.LastCure) < wait)

	if damped {
		result := DoctorResult{Check: name, Verdict: "damped", Reason: reason, WayBack: check.WayBack(), Faulty: true}
		if !dryRun {
			if err := d.append(ctx, result); err != nil {
				return DoctorResult{}, err
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

// now is the clock episodes and log lines are read and dated by.
func (d Doctor) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TicksHeading heads the section of mw status that says how this host's timers
// are doing. The report leaves it out when the host keeps no log of either.
const TicksHeading = "TICKS"

// What mw status calls each of the two timers whose runs are counted.
const (
	TicksDispatchLabel = "dispatch"
	TicksMillhandLabel = "Millhand tick"
)

// TicksTimeFormat is how the time of a last good run is shown: UTC, to the
// minute, short enough for a phone.
const TicksTimeFormat = "2006-01-02 15:04Z"

// TicksKey is where a host leaves the counts of its timers' runs for the other
// host to read, beside the note of when it was last level: for the Laptop it is
// host.laptop.ticks.
func TicksKey(host string) string { return "host." + host + ".ticks" }

// TickLogs are the two logs a host's timers keep, as far as this host runs the
// timers: a nil log is a timer that leaves none.
type TickLogs struct {
	Dispatch TickLog
	Millhand TickLog
}

// TickOutcome is what one line of a log says about the run it records.
type TickOutcome int

const (
	// TickUnread is a line that is not a run: nothing dated, or words nobody
	// writes. It is counted as nothing.
	TickUnread TickOutcome = iota
	// TickGood is a run that did what it is for, nothing to do included.
	TickGood
	// TickFailed is a run that ran and failed.
	TickFailed
	// TickFault is a run that could not be made because this host's own network
	// was down. It is not a failure, and is counted apart.
	TickFault
)

// String is the outcome in a phrase.
func (o TickOutcome) String() string {
	switch o {
	case TickGood:
		return "a good run"
	case TickFailed:
		return "a failed run"
	case TickFault:
		return "a local network fault"
	default:
		return "no run"
	}
}

// TickCount is how a timer's runs stand, read from its log: when the last good
// run was, and how many runs since then failed or were local network faults. A
// good run resets both.
type TickCount struct {
	// Known is whether the log holds any run at all. A host that keeps no log
	// has nothing to say, and says nothing.
	Known bool
	// LastGood is the time of the last good run in the log; zero when the log,
	// which keeps only its last lines, holds none.
	LastGood time.Time
	// Failed and Faults are the runs since LastGood, or in the whole log when
	// there is no good one, that failed and that were local network faults.
	Failed, Faults int
}

// CountTicks reads a log's lines, newest first, until the last good run. A log
// that is not there, cannot be read or holds no run is not Known.
func CountTicks(ctx context.Context, log TickLog, outcome func(line string) (time.Time, TickOutcome)) TickCount {
	if log == nil {
		return TickCount{}
	}
	lines, err := log.Read(ctx)
	if err != nil {
		return TickCount{}
	}
	var count TickCount
	for i := len(lines) - 1; i >= 0; i-- {
		at, what := outcome(lines[i])
		switch what {
		case TickUnread:
			continue
		case TickGood:
			count.Known, count.LastGood = true, at
			return count
		case TickFailed:
			count.Failed++
		case TickFault:
			count.Faults++
		}
		count.Known = true
	}
	return count
}

// splitTickLine is a log line's time and the words after it.
func splitTickLine(line string) (at time.Time, words string, ok bool) {
	stamp, words, found := strings.Cut(strings.TrimSpace(line), " ")
	if !found {
		return time.Time{}, "", false
	}
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}, "", false
	}
	return at, words, true
}

// DispatchOutcome reads a line of the dispatch log: what dispatchLogLine writes.
func DispatchOutcome(line string) (time.Time, TickOutcome) {
	at, words, ok := splitTickLine(line)
	switch {
	case !ok:
		return time.Time{}, TickUnread
	case strings.HasPrefix(words, DispatchLogOK):
		return at, TickGood
	case strings.HasPrefix(words, DispatchLogFault):
		return at, TickFault
	case strings.HasPrefix(words, DispatchLogFailed):
		return at, TickFailed
	}
	return time.Time{}, TickUnread
}

// MillhandTickOutcome reads a line of the tick log, the one MillhandTick writes:
// its verdict, and after it the notes of what went wrong on the way, joined by
// "; ".
//
// A tick that could not tell whether the Millhand is needed, that could not look
// for its window or that woke it and failed, failed; so did one whose sync
// failed, though the tick went on and looked here all the same. A tick whose
// watch found this host's own network down is a local network fault. A sync the
// vault blocked, a watch that could not reach the other host and every other
// verdict — quiet, already up, woke the Millhand — are good runs.
func MillhandTickOutcome(line string) (time.Time, TickOutcome) {
	at, words, ok := splitTickLine(line)
	if !ok {
		return time.Time{}, TickUnread
	}
	parts := strings.Split(words, "; ")
	verdict := parts[0]
	switch {
	case strings.HasPrefix(verdict, TickCouldNotLook),
		strings.HasPrefix(verdict, TickCouldNotTell),
		strings.HasPrefix(verdict, TickWakeFailed):
		return at, TickFailed
	}
	for _, note := range parts[1:] {
		if strings.HasPrefix(note, WatchLocalFault+":") || strings.HasPrefix(note, TickLocalNetworkFault) {
			return at, TickFault
		}
	}
	for _, note := range parts[1:] {
		if strings.HasPrefix(note, TickSyncFailed) {
			return at, TickFailed
		}
	}
	return at, TickGood
}

// HostTicks are the counts of both of a host's timers.
type HostTicks struct {
	Dispatch TickCount
	Millhand TickCount
}

// Known reports whether either timer has a run to count.
func (h HostTicks) Known() bool { return h.Dispatch.Known || h.Millhand.Known }

// ReadHostTicks counts the runs of each log.
func ReadHostTicks(ctx context.Context, logs TickLogs) HostTicks {
	return HostTicks{
		Dispatch: CountTicks(ctx, logs.Dispatch, DispatchOutcome),
		Millhand: CountTicks(ctx, logs.Millhand, MillhandTickOutcome),
	}
}

// The words of the note, one part for each timer that has runs.
const (
	ticksNoteDispatch = "dispatch"
	ticksNoteMillhand = "millhand"
	ticksNoteNoGood   = "none"
)

// Note is the counts as the one line a host leaves the other in the tracker's
// key-value store: each timer that has runs, as "dispatch good=<time or none>
// failed=<n> faults=<n>". It is empty when there is nothing to say.
func (h HostTicks) Note() string {
	var parts []string
	for _, timer := range []struct {
		name  string
		count TickCount
	}{{ticksNoteDispatch, h.Dispatch}, {ticksNoteMillhand, h.Millhand}} {
		if !timer.count.Known {
			continue
		}
		good := ticksNoteNoGood
		if !timer.count.LastGood.IsZero() {
			good = timer.count.LastGood.UTC().Format(time.RFC3339)
		}
		parts = append(parts, fmt.Sprintf("%s good=%s failed=%d faults=%d", timer.name, good, timer.count.Failed, timer.count.Faults))
	}
	return strings.Join(parts, "; ")
}

// ParseHostTicks reads a note back. A part that cannot be read is left out, so
// that a note of some future shape is shown as far as it can be, and never as an
// error.
func ParseHostTicks(note string) HostTicks {
	var held HostTicks
	for _, part := range strings.Split(note, ";") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		count, ok := parseTickCount(fields[1:])
		switch {
		case !ok:
		case fields[0] == ticksNoteDispatch:
			held.Dispatch = count
		case fields[0] == ticksNoteMillhand:
			held.Millhand = count
		}
	}
	return held
}

// parseTickCount reads the good=, failed= and faults= of one part of a note.
func parseTickCount(fields []string) (TickCount, bool) {
	count := TickCount{Known: true}
	var good, failed, faults bool
	for _, field := range fields {
		key, value, found := strings.Cut(field, "=")
		if !found {
			return TickCount{}, false
		}
		switch key {
		case "good":
			if value != ticksNoteNoGood {
				at, err := time.Parse(time.RFC3339, value)
				if err != nil {
					return TickCount{}, false
				}
				count.LastGood = at
			}
			good = true
		case "failed", "faults":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return TickCount{}, false
			}
			if key == "failed" {
				count.Failed, failed = n, true
			} else {
				count.Faults, faults = n, true
			}
		}
	}
	return count, good && failed && faults
}

// write is one timer's count as mw status shows it, under the label: the last
// good run on one line, and on the next how many runs since have failed. Local
// network faults are said apart, and only when there were some.
func (c TickCount) write(b *strings.Builder, pad, label string) {
	if !c.Known {
		return
	}
	if c.LastGood.IsZero() {
		clip(b, fmt.Sprintf("%s%s · no good run logged", pad, label))
	} else {
		clip(b, fmt.Sprintf("%s%s · last good %s", pad, label, c.LastGood.UTC().Format(TicksTimeFormat)))
	}
	if c.Faults > 0 {
		faults := "local network faults"
		if c.Faults == 1 {
			faults = "local network fault"
		}
		clip(b, fmt.Sprintf("%s  %d %s, %d failed", pad, c.Faults, faults, c.Failed))
		return
	}
	clip(b, fmt.Sprintf("%s  failed %d in a row", pad, c.Failed))
}

// write is both timers' counts, the dispatch first.
func (h HostTicks) write(b *strings.Builder, pad string) {
	h.Dispatch.write(b, pad, TicksDispatchLabel)
	h.Millhand.write(b, pad, TicksMillhandLabel)
}

// tickResumedPrefix and tickResumingPrefix are what a Millhand tick's own
// line opens with when it announces a resume grace and while it still holds:
// the two words ReadResumeGrace and a tick's own resumeGrace look for in the
// log's newest line, so mw status and the tick agree on what is open from the
// very words the tick wrote.
const (
	tickResumedPrefix  = "resumed after "
	tickResumingPrefix = "resuming: "
)

// TickResumed reports, from a Millhand tick log's newest line, whether now is
// a resume — the host slept through the timer that runs the tick, so the gap
// since that line is more than ResumeGap — and, when it is, the words to
// announce it with. A log with nothing in it yet is never a resume: there is
// nothing to have slept through.
func TickResumed(ctx context.Context, log TickLog, now time.Time) (announce string, resumed bool) {
	if log == nil {
		return "", false
	}
	lines, err := log.Read(ctx)
	if err != nil || len(lines) == 0 {
		return "", false
	}
	when, _, ok := splitTickLine(lines[len(lines)-1])
	if !ok {
		return "", false
	}
	gap := now.Sub(when)
	if gap <= ResumeGap {
		return "", false
	}
	return tickResumedPrefix + gap.Round(time.Second).String(), true
}

// ResumeState is what a Millhand tick log's newest line says about a resume
// grace still running: when it was found, and until when doctor notes and
// the health verdict are deferred on its account. The zero value is no open
// grace.
type ResumeState struct {
	Resumed    time.Time
	GraceUntil time.Time
}

// Open reports whether now still falls inside this grace.
func (r ResumeState) Open(now time.Time) bool {
	return !r.GraceUntil.IsZero() && now.Before(r.GraceUntil)
}

// ReadResumeGrace reads a Millhand tick log's newest line and says whether it
// opened a resume grace that is still running at now — the same words the
// tick itself looks for before deferring, so mw status shows exactly what
// the tick decided by, from the tick log alone: the smaller state to keep
// than a note of its own, since the tick already reads this log every turn
// to know what to write next, and a resume is exactly the kind of thing a
// log already exists to say once and be believed.
//
// Only a line that itself announced or continued a resume counts: an
// ordinary line read again within GraceAfterResume of itself — two ticks
// running close together, in a test or a dry run — is not mistaken for an
// open grace. A local network fault is not read back this way at all: that
// grace is judged fresh from Reach every tick, never remembered between
// them.
func ReadResumeGrace(ctx context.Context, log TickLog, now time.Time) ResumeState {
	if log == nil {
		return ResumeState{}
	}
	lines, err := log.Read(ctx)
	if err != nil || len(lines) == 0 {
		return ResumeState{}
	}
	when, words, ok := splitTickLine(lines[len(lines)-1])
	if !ok || !resumeGraceLine(words) {
		return ResumeState{}
	}
	until := when.Add(GraceAfterResume)
	if !now.Before(until) {
		return ResumeState{}
	}
	return ResumeState{Resumed: when, GraceUntil: until}
}

// resumeGraceLine reports whether a tick's own line opened or is continuing a
// resume grace.
func resumeGraceLine(words string) bool {
	return strings.Contains(words, tickResumedPrefix) || strings.Contains(words, tickResumingPrefix)
}

// MillhandResumeLine is the line mw status shows under the Millhand tick
// while a resume grace still holds, read from the very state the tick itself
// judges by: "" once there is none to show. Its two times are shown to the
// minute alone, with no date: GraceAfterResume is minutes long, so both
// always fall on the one day, and the line stays well inside a phone-width
// report even beside the pad it is shown under.
func MillhandResumeLine(ctx context.Context, log TickLog, now time.Time) string {
	grace := ReadResumeGrace(ctx, log, now)
	if !grace.Open(now) {
		return ""
	}
	const clockFormat = "15:04Z"
	return fmt.Sprintf("resumed %s (grace until %s)",
		grace.Resumed.UTC().Format(clockFormat), grace.GraceUntil.UTC().Format(clockFormat))
}

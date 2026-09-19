package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// WatchWakeExit is the status mw watch leaves with when a wake is called for —
// the watched host is unwell, its health line is stale, or it is down. It is a
// status of its own so that a timer reading nothing but the number can tell
// "wake somebody" from "nobody needs waking" (0) and from a failure (1).
const WatchWakeExit = 6

// The limits of the rule. A health line older than WatchHealthStale is not
// believed; a sync note older than WatchSyncFresh is no sign of life; and a host
// is down only when its second failed check comes WatchDownAfter or more after
// its first, so one dropped connection is never a wake.
const (
	WatchHealthStale = 40 * time.Minute
	WatchSyncFresh   = 30 * time.Minute
	WatchDownAfter   = 3 * time.Minute
)

// What mw watch says it found, the word each line opens with.
const (
	WatchLocalFault     = "local-fault"
	WatchOK             = "ok"
	WatchUnwell         = "unwell"
	WatchStale          = "stale"
	WatchUnreachable    = "unreachable-once"
	WatchDown           = "down"
	WatchNothingToWatch = "nothing to watch"
)

// The signs of life a host that ssh cannot reach may still show.
const (
	SignBlog = "blog"
	SignSync = "sync"
	SignNone = "none"
)

// WatchHealthFile is the file on the watched host that its health line is read
// from, as the remote shell expands it. ssh is run with exactly
// `cat ~/.mw-health` and nothing else.
const WatchHealthFile = "~/.mw-health"

// WatchSettings is what the config's [watch] table says: which host to watch,
// how to reach it and how to tell this host's own network from its.
type WatchSettings struct {
	// SSH is the name ssh knows the watched host by.
	SSH string
	// Host is the watched host's name in beads, whose last sync note is one of
	// the signs of life.
	Host string
	// Outside are the places outside both hosts that only a working network
	// reaches. If none of them answers, the fault is this host's own.
	Outside []string
	// Blog is the URL whose answering over HTTPS is a sign the host is alive.
	Blog string
}

// Empty reports whether there is no [watch] table to act on.
func (s WatchSettings) Empty() bool {
	return s.SSH == "" && s.Host == "" && len(s.Outside) == 0 && s.Blog == ""
}

// WatchMemory is what mw watch keeps between runs on this host: when the
// current run of failed ssh checks began. It is the zero value when ssh last
// answered, or has never failed.
type WatchMemory struct {
	FirstFailure time.Time
}

// WatchProbes is everything mw watch reaches out to, and the little it keeps on
// this host. All of it only reads the world: the one thing it writes is its own
// memory and its own log, on this host.
type WatchProbes interface {
	// Reach reports whether the URL answered at all — any HTTP reply, of any
	// status, and not a refused, timed-out or unresolvable connection.
	Reach(ctx context.Context, url string) bool
	// ReadHealth runs `cat ~/.mw-health` on the watched host over ssh, and returns
	// what it printed. An error means ssh itself failed — no answer, no
	// authentication, a timeout — and so says nothing of the file; a host that
	// answers but has no file returns "" and no error.
	ReadHealth(ctx context.Context, ssh string) (string, error)
	// LoadMemory reads what was saved last: the zero value when nothing was.
	LoadMemory(ctx context.Context) (WatchMemory, error)
	// SaveMemory replaces what was saved.
	SaveMemory(ctx context.Context, memory WatchMemory) error
	// AppendLog adds one line to the log this host keeps of what mw watch found.
	AppendLog(ctx context.Context, line string) error
}

// WatchWake is the outcome of a watch that found somebody needs waking. It is
// not a fault, and a timer should not treat it as one: it carries WatchWakeExit.
// The line naming what was found has already been printed.
type WatchWake struct {
	// Line is what mw watch found, as it printed it.
	Line string
}

// Error is the finding, so a caller that does print an error prints it.
func (w *WatchWake) Error() string { return "a wake is called for: " + w.Line }

// WatchWakes reports whether err is a watch that called for a wake.
func WatchWakes(err error) (*WatchWake, bool) {
	var wake *WatchWake
	return wake, errors.As(err, &wake)
}

// WatchReport is what one run of mw watch found: the one line it prints, and
// whether a wake is called for.
type WatchReport struct {
	Line string
	Wake bool
}

// String is the report as it is printed: the one line.
func (r WatchReport) String() string { return r.Line + "\n" }

// Watch tells a fault of this host's own network from a fault of the host it
// watches, by three cases:
//
//  1. Neither outside place answers: local-fault. Nothing is known of the
//     watched host, nobody is woken and the memory is left as it was.
//  2. An outside place answers and ssh answers: the health line is read. Its
//     leading UTC timestamp older than WatchHealthStale is stale; otherwise its
//     trailing verdict says ok or unwell. Any answer from ssh forgets the failed
//     checks.
//  3. An outside place answers and ssh fails: the signs of life are looked for,
//     and the host is down only when a failed check was remembered from
//     WatchDownAfter ago or more; the first failure is unreachable-once.
//
// It only reads: no ssh command but `cat ~/.mw-health`, no write anywhere but
// this host's own memory and log.
type Watch struct {
	Probes WatchProbes
	// Notes is where the watched host's last sync is read from. A nil Notes
	// leaves the sync sign of life out.
	Notes    TrackerNotes
	Settings WatchSettings

	// Now is the clock the rule is read by. The zero value reads the real one.
	Now func() time.Time

	// Out is where the one line is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run applies the rule once, prints the one line, appends it, dated, to the log
// and, when a wake is called for, returns a *WatchWake so that the command leaves
// with WatchWakeExit.
func (w Watch) Run(ctx context.Context) (WatchReport, error) {
	if w.Probes == nil {
		return WatchReport{}, fmt.Errorf("watching: there is nothing to reach the world with")
	}

	report, err := w.find(ctx)
	if err != nil {
		return report, err
	}

	if w.Out != nil {
		fmt.Fprint(w.Out, report.String())
	}
	dated := w.now().UTC().Format(time.RFC3339) + " " + report.Line
	if err := w.Probes.AppendLog(ctx, dated); err != nil {
		return report, fmt.Errorf("appending to the watch log: %w", err)
	}
	if report.Wake {
		return report, &WatchWake{Line: report.Line}
	}
	return report, nil
}

// find is the rule itself: what was found, and whether it wakes somebody.
func (w Watch) find(ctx context.Context) (WatchReport, error) {
	if w.Settings.Empty() {
		return WatchReport{Line: WatchNothingToWatch}, nil
	}

	if !w.outsideAnswers(ctx) {
		return WatchReport{Line: WatchLocalFault}, nil
	}

	memory, err := w.Probes.LoadMemory(ctx)
	if err != nil {
		return WatchReport{}, fmt.Errorf("reading what the last watch remembered: %w", err)
	}

	health, err := w.Probes.ReadHealth(ctx, w.Settings.SSH)
	if err != nil {
		return w.unreachable(ctx, memory)
	}

	// ssh answered: whatever the line says, the host is there.
	if !memory.FirstFailure.IsZero() {
		if err := w.Probes.SaveMemory(ctx, WatchMemory{}); err != nil {
			return WatchReport{}, fmt.Errorf("forgetting the failed checks: %w", err)
		}
	}
	return w.judge(health), nil
}

// outsideAnswers reports whether any outside place answers, asking no more of
// them than it takes to find one that does.
func (w Watch) outsideAnswers(ctx context.Context) bool {
	for _, url := range w.Settings.Outside {
		if w.Probes.Reach(ctx, url) {
			return true
		}
	}
	return false
}

// judge reads a health line the watched host gave. Only its leading timestamp
// and its trailing verdict are looked at; the rest is the host's own.
//
// A line that cannot be shown to be fresh is stale, and so is no line at all: a
// timestamp that is not a time, or a file that is empty. A fresh line with no
// verdict at all is called unwell, for want of anyone vouching for the host.
func (w Watch) judge(health string) WatchReport {
	line := lastLine(health)
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return WatchReport{Line: WatchStale, Wake: true}
	}
	at, err := time.Parse(time.RFC3339, fields[0])
	if err != nil || w.now().Sub(at) > WatchHealthStale {
		return WatchReport{Line: WatchStale, Wake: true}
	}

	verdict := ""
	for _, field := range fields {
		if rest, found := strings.CutPrefix(field, "verdict="); found {
			verdict = rest
		}
	}
	switch {
	case verdict == "ok":
		return WatchReport{Line: WatchOK}
	case verdict == "":
		return WatchReport{Line: WatchUnwell + " no-verdict", Wake: true}
	}
	reasons := strings.TrimPrefix(strings.TrimPrefix(verdict, WatchUnwell), ":")
	if reasons == "" {
		return WatchReport{Line: WatchUnwell, Wake: true}
	}
	return WatchReport{Line: WatchUnwell + " " + reasons, Wake: true}
}

// unreachable is case 3: ssh failed while the outside answered. The signs of
// life are looked for and named, and the failed check is counted: remembered
// when it is the first, and enough to call the host down when the first was
// WatchDownAfter ago or more.
func (w Watch) unreachable(ctx context.Context, memory WatchMemory) (WatchReport, error) {
	signs := "signs=" + strings.Join(w.signsOfLife(ctx), ",")
	now := w.now()

	if memory.FirstFailure.IsZero() {
		if err := w.Probes.SaveMemory(ctx, WatchMemory{FirstFailure: now}); err != nil {
			return WatchReport{}, fmt.Errorf("remembering the failed check: %w", err)
		}
		return WatchReport{Line: WatchUnreachable + " " + signs}, nil
	}
	if now.Sub(memory.FirstFailure) >= WatchDownAfter {
		return WatchReport{Line: WatchDown + " " + signs, Wake: true}, nil
	}
	return WatchReport{Line: WatchUnreachable + " " + signs}, nil
}

// signsOfLife are the signs a host ssh cannot reach still gives, in a settled
// order, or SignNone when there are none: the blog answering over HTTPS, and a
// last sync note in the local beads fresher than WatchSyncFresh.
//
// A note that cannot be read is no sign: the line is still worth printing, and
// a host is never called down on one sign alone.
func (w Watch) signsOfLife(ctx context.Context) []string {
	var signs []string
	if w.Settings.Blog != "" && w.Probes.Reach(ctx, w.Settings.Blog) {
		signs = append(signs, SignBlog)
	}
	if w.syncedRecently(ctx) {
		signs = append(signs, SignSync)
	}
	if len(signs) == 0 {
		return []string{SignNone}
	}
	return signs
}

// syncedRecently reports whether the watched host recorded a sync within
// WatchSyncFresh, as its last_sync note says, read the way mw status reads it.
func (w Watch) syncedRecently(ctx context.Context) bool {
	if w.Notes == nil || w.Settings.Host == "" {
		return false
	}
	said, err := w.Notes.Note(ctx, LastSyncKey(w.Settings.Host))
	if err != nil {
		return false
	}
	at, err := time.Parse(LastSyncFormat, strings.TrimSpace(said))
	if err != nil {
		return false
	}
	return w.now().Sub(at) < WatchSyncFresh
}

// lastLine is the last line of text that is not blank: the health file holds
// one, and a login banner or a stray newline before or after it is not it.
func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// now is the clock the rule is read by.
func (w Watch) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

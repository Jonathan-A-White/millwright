package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// GristKeepAge is how long the mill keeps what it was sent: a run's photos and
// recordings for this long after the run, and a forwarded grist's pictures for
// this long after the forward. The Governor's word (mw-gq6.692, 'Do A'); the
// small record of a run (the request, the answer, the timing, the ids) is never
// deleted. Changing it is a change to this constant and to docs/grist-tidy.md.
const GristKeepAge = 30 * 24 * time.Hour

// GristTidyLogPrefix starts the words of a tick log line mw grist tidy writes,
// one per act. It starts with TidyLogPrefix, so that the tick's counts and
// resume rules read past it as they do mw tidy's lines.
const GristTidyLogPrefix = TidyLogPrefix + "grist: "

// GristMedia is a run that still holds attachments (photos and recordings).
type GristMedia struct {
	// Name is the run's directory under runs/.
	Name string
	// Txid is the run's grist, for the report.
	Txid string
	// Received is when the mill took the grist up, the run's own time.
	Received time.Time
	Files    int
	Bytes    int64
}

// GristRunMedia is the part of the runs the sweep reads and strips. The real
// adapter is infrastructure/grist's Runs.
type GristRunMedia interface {
	// RunMedia lists the runs that hold attachments, oldest first. A directory
	// that is not a run (no timing.json) is not listed.
	RunMedia(ctx context.Context) ([]GristMedia, error)
	// DropMedia deletes the attachments of the run named, and nothing else of
	// it: its input, scorers, answer, timing and sender stay.
	DropMedia(ctx context.Context, name string) error
}

// GristShelved is one forwarded grist's pictures.
type GristShelved struct {
	// Name is the directory under forwarded/.
	Name string
	// Forwarded is when the pictures were kept.
	Forwarded time.Time
	Files     int
	Bytes     int64
}

// GristForwardShelf is the part of the forwarded pictures the sweep reads and
// deletes. The real adapter is infrastructure/grist's Forwards.
type GristForwardShelf interface {
	// Shelved lists every forwarded grist's directory, oldest first.
	Shelved(ctx context.Context) ([]GristShelved, error)
	// DropShelved deletes the directory named, whole.
	DropShelved(ctx context.Context, name string) error
}

// GristTidyAct is one thing mw grist tidy did, or in a dry run would do.
type GristTidyAct struct {
	// Kind is "media" (a run's attachments) or "forward" (a forwarded grist's
	// directory).
	Kind  string
	Txid  string
	Files int
	Bytes int64
	// Age is how old the run or the forward was.
	Age time.Duration
}

// String is the act as a line of the log and the report.
func (a GristTidyAct) String() string {
	what := "attachments of run"
	if a.Kind == "forward" {
		what = "forwarded pictures of"
	}
	return fmt.Sprintf("%s %s: %s, %s, %d days old", what, a.Txid,
		counted(a.Files, "file", "files"), humanBytes(a.Bytes), int(a.Age/(24*time.Hour)))
}

// GristTidyReport is what one pass did.
type GristTidyReport struct {
	DryRun bool
	Acts   []GristTidyAct
	Notes  []string
}

// Bytes is the size of everything the pass deleted, or would.
func (r GristTidyReport) Bytes() int64 {
	var n int64
	for _, a := range r.Acts {
		n += a.Bytes
	}
	return n
}

// Summary is the report in a phrase, for the tick's line.
func (r GristTidyReport) Summary() string {
	verb := "grist tidy deleted "
	if r.DryRun {
		verb = "grist tidy would delete "
	}
	return verb + counted(len(r.Acts), "item", "items") + " (" + humanBytes(r.Bytes()) + ")"
}

// String is the report as a person reads it.
func (r GristTidyReport) String() string {
	if len(r.Acts) == 0 && len(r.Notes) == 0 {
		return "grist tidy: nothing is old enough to delete\n"
	}
	var b strings.Builder
	verb := "deleted"
	if r.DryRun {
		verb = "would delete"
	}
	fmt.Fprintf(&b, "grist tidy: %s %s, %s\n", verb, counted(len(r.Acts), "item", "items"), humanBytes(r.Bytes()))
	for _, a := range r.Acts {
		fmt.Fprintf(&b, "  %s %s\n", verb, a)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "  ! %s\n", n)
	}
	return b.String()
}

// GristTidy deletes what the mill kept past GristKeepAge: the photos and
// recordings of a run received that long ago (the run's small record stays),
// and the whole directory of pictures of a grist forwarded that long ago.
// Nothing younger is touched. A look or an act that fails is a note and the
// pass goes on; Run returns an error when any did.
type GristTidy struct {
	Runs     GristRunMedia
	Forwards GristForwardShelf

	// Log is the tick log each act adds a line to; nil writes none.
	Log TickLog

	DryRun bool

	// Now is the clock ages are measured against; nil is time.Now.
	Now func() time.Time

	// Out is where the report is printed; nil prints nothing.
	Out io.Writer
}

// Run makes one pass.
func (g GristTidy) Run(ctx context.Context) (GristTidyReport, error) {
	report := GristTidyReport{DryRun: g.DryRun}
	if g.Runs == nil && g.Forwards == nil {
		return report, fmt.Errorf("grist tidy: there is no run store or forward shelf to tidy")
	}
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	var faults []error
	fault := func(err error) {
		faults = append(faults, err)
		report.Notes = append(report.Notes, oneLine(err.Error()))
	}
	if g.Runs != nil {
		media, err := g.Runs.RunMedia(ctx)
		if err != nil {
			fault(fmt.Errorf("reading the runs: %w", err))
		}
		for _, m := range media {
			age := now.Sub(m.Received)
			if age < GristKeepAge {
				continue
			}
			act := GristTidyAct{Kind: "media", Txid: m.Txid, Files: m.Files, Bytes: m.Bytes, Age: age}
			if !g.DryRun {
				if err := g.Runs.DropMedia(ctx, m.Name); err != nil {
					fault(fmt.Errorf("deleting the attachments of run %s: %w", m.Txid, err))
					continue
				}
			}
			g.record(ctx, now, act, &report)
		}
	}
	if g.Forwards != nil {
		shelved, err := g.Forwards.Shelved(ctx)
		if err != nil {
			fault(fmt.Errorf("reading the forwarded pictures: %w", err))
		}
		for _, s := range shelved {
			age := now.Sub(s.Forwarded)
			if age < GristKeepAge {
				continue
			}
			act := GristTidyAct{Kind: "forward", Txid: s.Name, Files: s.Files, Bytes: s.Bytes, Age: age}
			if !g.DryRun {
				if err := g.Forwards.DropShelved(ctx, s.Name); err != nil {
					fault(fmt.Errorf("deleting the forwarded pictures of %s: %w", s.Name, err))
					continue
				}
			}
			g.record(ctx, now, act, &report)
		}
	}
	if g.Out != nil {
		fmt.Fprint(g.Out, report.String())
	}
	return report, errors.Join(faults...)
}

// record adds the act to the report and, when it was done, to the tick log. A
// log that cannot be written is a note: the act is done.
func (g GristTidy) record(ctx context.Context, now time.Time, act GristTidyAct, report *GristTidyReport) {
	report.Acts = append(report.Acts, act)
	if g.Log == nil || g.DryRun {
		return
	}
	line := now.UTC().Format(time.RFC3339) + " " + GristTidyLogPrefix + act.String()
	if err := g.Log.Append(ctx, line); err != nil {
		report.Notes = append(report.Notes, "the tick log could not be written: "+oneLine(err.Error()))
	}
}

// humanBytes is n as KB or MB, one decimal.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

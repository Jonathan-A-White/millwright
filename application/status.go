package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Width is the terminal `mw status` is designed for: a phone screen in a
// terminal app, not a laptop's. No line of a report is wider than this, a
// title however long it runs included.
const Width = 60

// Status reads, for one host, what is running there, what is ready to be
// taken, what is blocked, and today's fuel from a seat's ledger. It is the
// read-only, zero-token twin of dispatch: nothing is claimed, nothing is
// written to a bead, nothing is appended to the ledger, and no session is
// started, sent to or closed.
type Status struct {
	Tracker WorkTracker
	Vault   Vault

	// Host is which of the factory's hosts this report is for.
	Host string
	// Seat is whose ledger today's fuel is summed from — the Builder's, since
	// the Builder is the seat that works every story.
	Seat string

	// Now is the clock "today" is read by, for picking out the ledger's lines
	// dated today. The zero value reads the real one.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// RunningStory is one story this host has claimed, with what a person needs
// to find and judge the session working it.
type RunningStory struct {
	Detail StoryDetail
	// Session is the runner's name for the session: what a person attaches to.
	// It is derived from the story's id, not read from a runner — mw status
	// never asks the runner anything.
	Session string
	// Run is what the tracker has recorded this story's session doing:
	// RunRunning while it works, or the run state left behind once it stopped
	// without closing the story (RunStopped, or the RunStuck a sweep records).
	// Empty reads the same as RunRunning: nobody has recorded anything else.
	Run string
	// FormulaSteps is how many steps of the story's poured formula are still
	// open. Zero means either no formula was poured, or every step is closed.
	FormulaSteps int
}

// Stopped reports whether this story's session is not to be trusted as
// running, whatever its claim says: it was marked stopped or stuck.
func (r RunningStory) Stopped() bool {
	return r.Run == RunStopped || r.Run == RunStuck
}

// FormulaOpen reports whether this story cannot be closed out yet because a
// step of its poured formula is still open.
func (r RunningStory) FormulaOpen() bool { return r.FormulaSteps > 0 }

// StatusReport is what one host is doing right now, as `mw status` reads it.
type StatusReport struct {
	Host    string
	Running []RunningStory
	Ready   []StoryDetail
	Blocked []StoryDetail
	// FuelToday is every token the seat's ledger charged today, summed from
	// the lines the ledger dates today.
	FuelToday int
}

// Run reads the report and prints it. Every call it makes is a read: the
// tracker's RunningStories, StoryState and OpenSteps, ReadyForHost and
// BlockedForHost, and the vault's ReadLedger. Nothing is claimed, nothing is
// poured, nothing is written.
func (s Status) Run(ctx context.Context) (StatusReport, error) {
	report := StatusReport{Host: s.Host}
	switch {
	case s.Tracker == nil:
		return report, fmt.Errorf("reading status: there is no work tracker to read it from")
	case s.Host == "":
		return report, fmt.Errorf("reading status: which host is this? set MW_HOST, or host in the config file")
	case s.Seat == "":
		return report, fmt.Errorf("reading status: whose ledger is today's fuel summed from?")
	}

	running, err := s.Tracker.RunningStories(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is running on %s: %w", s.Host, err)
	}
	for _, detail := range running {
		rs, err := s.running(ctx, detail)
		if err != nil {
			return report, err
		}
		report.Running = append(report.Running, rs)
	}

	ready, err := s.Tracker.ReadyForHost(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is ready on %s: %w", s.Host, err)
	}
	report.Ready = ready

	blocked, err := s.Tracker.BlockedForHost(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is blocked on %s: %w", s.Host, err)
	}
	report.Blocked = blocked

	fuel, err := s.fuelToday(ctx)
	if err != nil {
		return report, fmt.Errorf("summing today's fuel from the %s seat's ledger: %w", s.Seat, err)
	}
	report.FuelToday = fuel

	s.print(report.String())
	return report, nil
}

// running reads what mw status shows about one story this host has claimed:
// what the tracker last recorded its session doing, and whether its poured
// formula, if it has one, still has a step open.
func (s Status) running(ctx context.Context, detail StoryDetail) (RunningStory, error) {
	id := detail.Story.ID
	rs := RunningStory{Detail: detail, Session: SessionName(id)}

	run, err := s.Tracker.StoryState(ctx, id, RunState)
	if err != nil {
		return RunningStory{}, fmt.Errorf("reading the run state of %s: %w", id, err)
	}
	rs.Run = run

	if root := detail.Molecule.RootID; root != "" {
		open, err := s.Tracker.OpenSteps(ctx, root)
		if err != nil {
			return RunningStory{}, fmt.Errorf("reading the open formula steps of %s: %w", id, err)
		}
		rs.FormulaSteps = len(open)
	}
	return rs, nil
}

// fuelToday sums the tokens of the seat's ledger lines dated today. A seat
// with no ledger yet, or a report with no vault to read one from, sums to
// zero rather than failing: there is nothing to add up.
func (s Status) fuelToday(ctx context.Context) (int, error) {
	if s.Vault == nil {
		return 0, nil
	}
	lines, err := s.Vault.ReadLedger(ctx, s.Seat)
	if err != nil {
		return 0, err
	}
	today := s.now().Format(LedgerDate)

	total := 0
	for _, line := range lines {
		row, ok := ParseLedgerRow(line)
		if !ok || row.Date != today {
			continue
		}
		total += row.Tokens
	}
	return total, nil
}

// now is the clock "today" is read by.
func (s Status) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// print writes the report, when there is somewhere to write it.
func (s Status) print(text string) {
	if s.Out == nil {
		return
	}
	fmt.Fprint(s.Out, text)
}

// String is the status report as a person reads it on a phone: no line wider
// than Width, however long a title runs.
func (r StatusReport) String() string {
	var b strings.Builder
	clip(&b, fmt.Sprintf("mw status · %s", r.Host))
	b.WriteString("\n")

	clip(&b, fmt.Sprintf("RUNNING (%d)", len(r.Running)))
	if len(r.Running) == 0 {
		clip(&b, "  nothing running")
	}
	for _, rs := range r.Running {
		rs.write(&b)
	}
	b.WriteString("\n")

	clip(&b, fmt.Sprintf("READY (%d)", len(r.Ready)))
	if len(r.Ready) == 0 {
		clip(&b, "  nothing ready")
	}
	for _, d := range r.Ready {
		writeStory(&b, d, "")
	}
	b.WriteString("\n")

	clip(&b, fmt.Sprintf("BLOCKED (%d)", len(r.Blocked)))
	if len(r.Blocked) == 0 {
		clip(&b, "  nothing blocked")
	}
	for _, d := range r.Blocked {
		note := ""
		if len(d.Needs) > 0 {
			note = "needs " + strings.Join(d.Needs, ", ")
		}
		writeStory(&b, d, note)
	}
	b.WriteString("\n")

	clip(&b, fmt.Sprintf("FUEL today: %s tokens", Thousands(r.FuelToday)))
	b.WriteString("\n")
	return b.String()
}

// write is one running story as the report shows it: the story, its session,
// and whatever a person must not mistake it for — a session recorded stopped
// or stuck rather than running, or a formula step still open that will refuse
// to let it close out.
func (r RunningStory) write(b *strings.Builder) {
	writeStory(b, r.Detail, "session "+r.Session)
	if r.Stopped() {
		clip(b, fmt.Sprintf("    NOT RUNNING: run=%s", r.Run))
	}
	if r.FormulaOpen() {
		clip(b, fmt.Sprintf("    close blocked: %d formula step(s) open", r.FormulaSteps))
	}
}

// writeStory is one story's block: its id and rig, its title, and one more
// line of note when there is something to say. Every line is clipped to
// Width on its own, so a title of any length never widens the report.
func writeStory(b *strings.Builder, d StoryDetail, note string) {
	clip(b, fmt.Sprintf("  %s · %s", d.Story.ID, d.Merged().Rig))
	clip(b, "    "+d.Story.Title)
	if note != "" {
		clip(b, "    "+note)
	}
}

// clip writes one line, cut to Width runes so a phone-width terminal never
// wraps it, with the newline it ends in.
func clip(b *strings.Builder, text string) {
	b.WriteString(clipped(text))
	b.WriteString("\n")
}

// clipped is text cut to at most Width runes, marked with an ellipsis when
// something was cut off so a person knows the line does not say everything.
func clipped(text string) string {
	r := []rune(text)
	if len(r) <= Width {
		return text
	}
	if Width <= 1 {
		return string(r[:Width])
	}
	return string(r[:Width-1]) + "…"
}

package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Width is the terminal `mw status` is designed for: a phone screen in a
// terminal app, not a laptop's. No line of a report is wider than this, a
// title however long it runs included.
const Width = 60

// DefaultHostSilence is how long another host may go without recording a sync
// before mw status calls it asleep and its work stranded, when nothing says
// otherwise. It is the same two hours infrastructure/config.
// DefaultHostSilentHours reads as its default, and for the same reason: a
// host's last_sync note reaches this host only on this host's own next sync, so
// the freshest reading of it can already be a cycle old.
const DefaultHostSilence = 2 * time.Hour

// WaitingHeading is what heads the section for stories the Governor must be
// present for. The report leaves the section out when there are none.
const WaitingHeading = "WAITING FOR THE GOVERNOR"

// RigMemoryHeading is what heads the section naming the rigs whose memory has
// outgrown its budget. The report leaves the section out when none has.
const RigMemoryHeading = "RIG MEMORY"

// DefaultRigMemoryBytes is how large a Builder's memory of one rig may grow
// before mw status says it is due to be pruned, when nothing says otherwise. It
// is the same 8000 infrastructure/config.DefaultRigMemoryBytes reads as.
const DefaultRigMemoryBytes = 8000

// DefaultBeadsBudgetBytes is how large a host's own beads database — .beads
// in full, its auto-commit history and auto-backups included, since both
// have run away in the past — may grow before mw status warns it is past
// budget, when nothing says otherwise.
const DefaultBeadsBudgetBytes = 1_000_000_000

// RepathHint is what a person does about work stranded on a sleeping host: it
// is re-pathed, by hand, to a host that is awake. mw status only ever says
// this; re-pathing a story is the Mayor's act, never a report's.
const RepathHint = "bd update <id> --set-metadata host="

// TrackerNotes is the read-only half of what mw status learns about the
// tracker without writing to it: the notes the factory's hosts leave each
// other in the tracker's key-value store — when each was last level, above
// all — and how large this host's own database is on disk. It is
// deliberately read-only: mw status reads another host's last sync and must
// not be able to write one, not even by mistake.
type TrackerNotes interface {
	// Note reads one value out of the tracker's key-value store, or "" when the
	// key is not there. It is TrackerSync's own Note, narrowed.
	Note(ctx context.Context, key string) (string, error)

	// Size reports how many bytes this host's own beads database occupies on
	// disk — the working database and whatever else bd keeps beside it, its
	// auto-commit history and auto-backups included. It is host-local: no
	// adapter can size another host's disk, so mw status never calls this for
	// a host other than the one it runs on.
	Size(ctx context.Context) (int64, error)
}

// Status reads, for one host, what is running there, what is ready to be
// taken, what waits for the Governor, what is blocked, today's fuel from a
// seat's ledger, and what every other host has in hand and when it last synced. It is the read-only,
// zero-token twin of dispatch: nothing is claimed, nothing is written to a
// bead, no note is left, nothing is appended to the ledger, and no session is
// started, sent to or closed.
type Status struct {
	Tracker WorkTracker
	Vault   Vault
	// Notes is where the other hosts' last sync times are read from. A nil
	// Notes leaves the other-hosts section out altogether rather than calling
	// every host asleep on no evidence.
	Notes TrackerNotes

	// SyncHalt is this host's own mark of a halted sync, read straight rather
	// than through the tracker: the one thing a halted sync cannot carry is
	// the word of its own halt. A nil SyncHalt leaves the local line out.
	SyncHalt SyncHaltMarker

	// Host is which of the factory's hosts this report is for.
	Host string

	// HostSilence is how long another host's recorded sync may be behind
	// before its work is called stranded. Zero reads DefaultHostSilence.
	HostSilence time.Duration
	// RigMemoryBytes is how large the Seat's memory of one rig may be before the
	// report says it is due to be pruned. Zero reads DefaultRigMemoryBytes.
	RigMemoryBytes int
	// BeadsBudgetBytes is how large this host's own beads database may grow
	// before the report warns it is past budget. Zero reads
	// DefaultBeadsBudgetBytes.
	BeadsBudgetBytes int64
	// Seat is whose ledger today's fuel is summed from — the Builder's, since
	// the Builder is the seat that works every story.
	Seat string

	// Ticks are this host's logs of its timers' runs, counted for the TICKS
	// section. A log that is not there, or that no line was ever written to,
	// leaves its timer out; with neither there is no section.
	Ticks TickLogs

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

// HostWork is what one other host has in hand, as this host can see it: when
// that host last recorded itself level, whether that is recent enough to
// believe it is awake, and the stories pathed to it.
//
// The factory has no automatic failover: a host that stops syncing does not
// hand its work back. So this is the whole of the safety net — the work is
// named, the silence is named, and a person re-paths it.
type HostWork struct {
	// Host is the host these stories are pathed to.
	Host string
	// LastSync is when that host recorded itself last level, as its own note
	// says. It is zero when the host has never recorded one, and zero when the
	// note it left cannot be read as a time.
	LastSync time.Time
	// Said is the note exactly as the tracker held it, so that one nobody can
	// read as a time is still shown rather than swallowed. It is "" when the
	// host has never recorded a sync.
	Said string
	// Silent is how long it is since LastSync, and zero when there is no
	// LastSync to measure from.
	Silent time.Duration
	// Halt is what that host's own sync-halted note says, read like every
	// other note of it — which, since the note rides the very sync that is
	// stuck, may say nothing of a halt that has not cleared yet. Nil when it
	// has recorded none.
	Halt *SyncHaltInfo
	// Asleep says this host has been silent for longer than the threshold — or
	// has never synced, or left a note that is not a time. Its stories are
	// stranded: nothing here will move them, and no other host will take them
	// until somebody re-paths them.
	Asleep bool
	// Ticks are the counts of that host's timers as it left them with its last
	// sync: nothing known of a host that has left none.
	Ticks HostTicks
	// Stories are the stories pathed to this host that are ready to be taken or
	// already claimed, in the order the tracker listed them.
	Stories []StoryDetail
}

// NeverSynced reports whether this host has never recorded being level at all.
func (w HostWork) NeverSynced() bool { return w.Said == "" }

// Unreadable reports whether this host left a note that cannot be read as a
// time — the one case where a host is called asleep without a last sync to
// show for it.
func (w HostWork) Unreadable() bool { return w.Said != "" && w.LastSync.IsZero() }

// StatusReport is what one host is doing right now, as `mw status` reads it.
type StatusReport struct {
	Host    string
	Running []RunningStory
	Ready   []StoryDetail
	// Waiting are the beads labelled hitl that are open and not blocked, on any
	// host or none, and the stories labelled hitl this host has claimed: worked
	// with the Governor present, so neither a dispatcher's to take nor a session
	// for Running to show. They are in no other list, most urgent first.
	Waiting []StoryDetail
	Blocked []StoryDetail
	// Others is what every other host named in a story's Path has in hand, one
	// entry per host, in host order.
	Others []HostWork
	// Ticks are how this host's own timers are doing, counted from their logs.
	Ticks HostTicks
	// MillhandResume is the "resumed ... (grace until ...)" line shown under
	// the Millhand tick while its resume grace still holds; "" once it does
	// not.
	MillhandResume string
	// FuelToday is every token the seat's ledger charged today, summed from
	// the lines the ledger dates today.
	FuelToday int
	// RigMemory are the rigs whose memory is larger than RigMemoryBudget, in rig
	// order. It is empty, and the report has no section for it, when none is.
	RigMemory []RigMemorySize
	// RigMemoryBudget is the size a rig's memory was held to.
	RigMemoryBudget int
	// Halt is what this host's own sync-halted mark says, read straight from
	// SyncHalt rather than through the tracker. Nil when nothing is halted.
	Halt *SyncHaltInfo
	// BeadsBytes is how large this host's own beads database is on disk, and
	// BeadsKnown says whether it was measured at all: a report with no Notes
	// to read it from leaves both zero rather than claiming an empty
	// database. BeadsBudgetBytes is the size it was held to.
	BeadsBytes       int64
	BeadsKnown       bool
	BeadsBudgetBytes int64
}

// Run reads the report and prints it. Every call it makes is a read: the
// tracker's WorkInHand, StoryState and OpenSteps, ReadyWithLabel and
// BlockedForHost, two Notes per other host, the two tick logs' Read and the
// vault's ReadLedger and RigMemorySizes. WorkInHand is read once, and this
// host's running and ready stories and the other hosts' work are all narrowed
// from it. Nothing is claimed, nothing is poured, nothing is written.
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

	work, err := s.Tracker.WorkInHand(ctx)
	if err != nil {
		return report, fmt.Errorf("reading what is in hand: %w", err)
	}

	for _, detail := range work.RunningOn(s.Host) {
		if detail.Hitl() {
			// The Mayor works it beside the Governor: there is no session of its
			// own to judge, only a story to be found waiting.
			report.Waiting = append(report.Waiting, detail)
			continue
		}
		rs, err := s.running(ctx, detail)
		if err != nil {
			return report, err
		}
		report.Running = append(report.Running, rs)
	}

	for _, detail := range work.ReadyOn(s.Host) {
		if detail.Hitl() {
			// Listed below with every other bead for the Governor, whichever host
			// its Path names.
			continue
		}
		report.Ready = append(report.Ready, detail)
	}

	// A bead for the Governor may have no Path at all, so no listing keyed by
	// host finds it: ask for the label itself. A hitl story ready on this host
	// comes back here too, and is listed once.
	governors, err := s.Tracker.ReadyWithLabel(ctx, LabelHitl)
	if err != nil {
		return report, fmt.Errorf("reading what waits for the Governor: %w", err)
	}
	report.Waiting = append(report.Waiting, governors...)
	sort.SliceStable(report.Waiting, func(i, j int) bool {
		return report.Waiting[i].Priority < report.Waiting[j].Priority
	})

	blocked, err := s.Tracker.BlockedForHost(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is blocked on %s: %w", s.Host, err)
	}
	report.Blocked = blocked

	others, err := s.elsewhere(ctx, work)
	if err != nil {
		return report, err
	}
	report.Others = others
	report.Ticks = ReadHostTicks(ctx, s.Ticks)
	report.MillhandResume = MillhandResumeLine(ctx, s.Ticks.Millhand, s.now())

	fuel, err := s.fuelToday(ctx)
	if err != nil {
		return report, fmt.Errorf("summing today's fuel from the %s seat's ledger: %w", s.Seat, err)
	}
	report.FuelToday = fuel

	over, err := s.rigMemoryOverBudget(ctx)
	if err != nil {
		return report, fmt.Errorf("sizing the %s seat's memory of its rigs: %w", s.Seat, err)
	}
	report.RigMemory = over
	report.RigMemoryBudget = s.rigMemoryBudget()

	if s.SyncHalt != nil {
		if info, there, err := s.SyncHalt.Read(ctx); err != nil {
			return report, fmt.Errorf("reading whether this host's own sync is halted: %w", err)
		} else if there {
			report.Halt = &info
		}
	}

	if s.Notes != nil {
		size, err := s.Notes.Size(ctx)
		if err != nil {
			return report, fmt.Errorf("sizing this host's own beads database: %w", err)
		}
		report.BeadsBytes = size
		report.BeadsBudgetBytes = s.beadsBudget()
		report.BeadsKnown = true
	}

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

// elsewhere reads what the other hosts hold: the stories pathed to each of
// them, taken from the work already in hand, and when each last recorded itself
// level and how its timers were doing. It costs two notes per host that has
// work, and writes nothing.
//
// A host is called asleep when the last sync it recorded is further back than
// the threshold, when it has never recorded one, or when what it recorded is
// not a time. None of those is an error: a host that cannot say when it was
// last level is exactly the host whose work a person must look at.
func (s Status) elsewhere(ctx context.Context, inHand WorkInHand) ([]HostWork, error) {
	if s.Notes == nil {
		return nil, nil
	}

	byHost := map[string][]StoryDetail{}
	var hosts []string
	for _, detail := range inHand.Elsewhere(s.Host) {
		host := detail.Merged().Host
		if _, seen := byHost[host]; !seen {
			hosts = append(hosts, host)
		}
		byHost[host] = append(byHost[host], detail)
	}
	sort.Strings(hosts)

	now := s.now()
	work := make([]HostWork, 0, len(hosts))
	for _, host := range hosts {
		said, err := s.Notes.Note(ctx, LastSyncKey(host))
		if err != nil {
			return nil, fmt.Errorf("reading when %s last synced: %w", host, err)
		}

		held := HostWork{Host: host, Said: strings.TrimSpace(said), Stories: byHost[host]}
		if held.Said != "" {
			if at, err := time.Parse(LastSyncFormat, held.Said); err == nil {
				held.LastSync = at
				held.Silent = now.Sub(at)
			}
		}
		held.Asleep = held.LastSync.IsZero() || held.Silent > s.hostSilence()

		haltSaid, err := s.Notes.Note(ctx, SyncHaltKey(host))
		if err != nil {
			return nil, fmt.Errorf("reading whether %s's sync is halted: %w", host, err)
		}
		if info, ok := ParseSyncHalt(haltSaid); ok {
			held.Halt = &info
		}

		ticks, err := s.Notes.Note(ctx, TicksKey(host))
		if err != nil {
			return nil, fmt.Errorf("reading how the timers of %s are doing: %w", host, err)
		}
		held.Ticks = ParseHostTicks(ticks)
		work = append(work, held)
	}
	return work, nil
}

// hostSilence is how long another host's last recorded sync may be behind
// before its work is called stranded.
func (s Status) hostSilence() time.Duration {
	if s.HostSilence <= 0 {
		return DefaultHostSilence
	}
	return s.HostSilence
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

// rigMemoryOverBudget is the rigs whose memory is larger than the budget, in
// rig order. A report with no vault to read them from has none.
func (s Status) rigMemoryOverBudget(ctx context.Context) ([]RigMemorySize, error) {
	if s.Vault == nil {
		return nil, nil
	}
	sizes, err := s.Vault.RigMemorySizes(ctx, s.Seat)
	if err != nil {
		return nil, err
	}
	var over []RigMemorySize
	for _, size := range sizes {
		if size.Bytes > s.rigMemoryBudget() {
			over = append(over, size)
		}
	}
	return over, nil
}

// rigMemoryBudget is how large a rig's memory may be before it is called due
// to be pruned.
func (s Status) rigMemoryBudget() int {
	if s.RigMemoryBytes <= 0 {
		return DefaultRigMemoryBytes
	}
	return s.RigMemoryBytes
}

// beadsBudget is how large this host's own beads database may grow before
// the report warns it is past budget.
func (s Status) beadsBudget() int64 {
	if s.BeadsBudgetBytes <= 0 {
		return DefaultBeadsBudgetBytes
	}
	return s.BeadsBudgetBytes
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

	if r.Halt != nil {
		clip(&b, haltLine(r.Host, *r.Halt))
		b.WriteString("\n")
	}

	if r.BeadsKnown {
		clip(&b, beadsLine(r.BeadsBytes, r.BeadsBudgetBytes))
		b.WriteString("\n")
	}

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

	if len(r.Waiting) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", WaitingHeading, len(r.Waiting)))
		for _, d := range r.Waiting {
			writeStory(&b, d, readyOrClaimed(d))
		}
		b.WriteString("\n")
	}

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

	clip(&b, fmt.Sprintf("OTHER HOSTS (%d)", len(r.Others)))
	if len(r.Others) == 0 {
		clip(&b, "  nothing pathed to another host")
	}
	for _, w := range r.Others {
		w.write(&b, r.Host)
	}
	b.WriteString("\n")

	if r.Ticks.Known() {
		clip(&b, TicksHeading)
		r.Ticks.write(&b, "  ")
		if r.MillhandResume != "" {
			clip(&b, "    "+r.MillhandResume)
		}
		b.WriteString("\n")
	}

	if len(r.RigMemory) > 0 {
		clip(&b, RigMemoryHeading)
		for _, size := range r.RigMemory {
			clip(&b, fmt.Sprintf("  %s %d/%d bytes: prune (Mayor)", size.Rig, size.Bytes, r.RigMemoryBudget))
		}
		b.WriteString("\n")
	}

	clip(&b, fmt.Sprintf("FUEL today: %s tokens", Thousands(r.FuelToday)))
	b.WriteString("\n")
	return b.String()
}

// write is one other host's block: the host and how long it has been quiet,
// the sync it last recorded, and the stories pathed to it — marked stranded,
// with the one line that re-paths them, when the host is asleep. here is the
// host the report is for, and so the host a stranded story is re-pathed to.
func (w HostWork) write(b *strings.Builder, here string) {
	clip(b, fmt.Sprintf("  %s · %s", w.Host, w.state()))
	if w.Halt != nil {
		clip(b, "    "+haltLine(w.Host, *w.Halt))
	}
	switch {
	case w.Unreadable():
		clip(b, fmt.Sprintf("    its note says %q, which is not a time", w.Said))
	case !w.LastSync.IsZero():
		clip(b, fmt.Sprintf("    last sync %s (a cycle behind)", w.LastSync.UTC().Format(LastSyncFormat)))
	}
	if w.Asleep {
		clip(b, "    re-path: "+RepathHint+here)
	}
	w.Ticks.write(b, "    ")

	note := ""
	if w.Asleep {
		note = "stranded"
	}
	for _, d := range w.Stories {
		writeStoryIn(b, "    ", d, strings.TrimPrefix(note+" · "+readyOrClaimed(d), " · "))
	}
}

// state is how a host's silence reads at the head of its block.
func (w HostWork) state() string {
	switch {
	case w.NeverSynced():
		return "ASLEEP, never synced"
	case w.Unreadable():
		return "ASLEEP, last sync unreadable"
	case w.Asleep:
		return "ASLEEP, silent " + Clock(w.Silent)
	default:
		return "synced " + Clock(w.Silent) + " ago"
	}
}

// haltLine is the one line a halted sync is worth, wherever it is shown: the
// host, since when, and what bd said.
func haltLine(host string, halt SyncHaltInfo) string {
	return fmt.Sprintf("host %s: sync halted since %s: %s", host, halt.At.UTC().Format(LastSyncFormat), halt.Said)
}

// beadsLine is the one line this host's own beads database is worth: its
// size, and a warning once it is past budget, so the Mayor sees it without
// asking a Clerk to run du.
func beadsLine(bytes, budget int64) string {
	if bytes > budget {
		return fmt.Sprintf("BEADS %s: past the %s budget", formatBytes(bytes), formatBytes(budget))
	}
	return fmt.Sprintf("BEADS %s", formatBytes(bytes))
}

// formatBytes writes a byte count the way a person reads one: whole
// gigabytes once there are any, otherwise whole megabytes, otherwise the
// bytes themselves.
func formatBytes(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fGB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%dMB", n/1_000_000)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// readyOrClaimed says, in one word, whether a story elsewhere is waiting to be
// taken or already in somebody's hands.
func readyOrClaimed(d StoryDetail) string {
	if strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress) {
		return "claimed"
	}
	return "ready"
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
// line of note when there is something to say. A bead with no rig — a ticket
// for the Governor — is its id alone. Every line is clipped to Width on its
// own, so a title of any length never widens the report.
func writeStory(b *strings.Builder, d StoryDetail, note string) {
	writeStoryIn(b, "  ", d, note)
}

// writeStoryIn is writeStory indented under something else — a story listed
// under the host it is pathed to, rather than under a heading.
func writeStoryIn(b *strings.Builder, pad string, d StoryDetail, note string) {
	if rig := d.Merged().Rig; rig != "" {
		clip(b, fmt.Sprintf("%s%s · %s", pad, d.Story.ID, rig))
	} else {
		clip(b, pad+d.Story.ID)
	}
	clip(b, pad+"  "+d.Story.Title)
	if note != "" {
		clip(b, pad+"  "+note)
	}
	// A story started once is the usual story; one started more than once is one
	// that was refused or given back, and is running out of tries.
	if d.Attempts > 1 {
		clip(b, fmt.Sprintf("%s  attempts %d", pad, d.Attempts))
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
func clipped(text string) string { return clippedTo(text, Width) }

// clippedTo is text cut to at most n runes, marked the same way.
func clippedTo(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

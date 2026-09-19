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

// RepathHint is what a person does about work stranded on a sleeping host: it
// is re-pathed, by hand, to a host that is awake. mw status only ever says
// this; re-pathing a story is the Mayor's act, never a report's.
const RepathHint = "bd update <id> --set-metadata host="

// TrackerNotes is the read half of the notes the factory's hosts leave each
// other in the tracker's key-value store — when each was last level, above
// all. It is deliberately the read half alone: mw status reads another host's
// last sync and must not be able to write one, not even by mistake.
type TrackerNotes interface {
	// Note reads one value out of the tracker's key-value store, or "" when the
	// key is not there. It is TrackerSync's own Note, narrowed.
	Note(ctx context.Context, key string) (string, error)
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

	// Host is which of the factory's hosts this report is for.
	Host string

	// HostSilence is how long another host's recorded sync may be behind
	// before its work is called stranded. Zero reads DefaultHostSilence.
	HostSilence time.Duration
	// RigMemoryBytes is how large the Seat's memory of one rig may be before the
	// report says it is due to be pruned. Zero reads DefaultRigMemoryBytes.
	RigMemoryBytes int
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
	// Asleep says this host has been silent for longer than the threshold — or
	// has never synced, or left a note that is not a time. Its stories are
	// stranded: nothing here will move them, and no other host will take them
	// until somebody re-paths them.
	Asleep bool
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
	// Waiting are the stories labelled hitl that are ready or already claimed:
	// worked with the Governor present, so neither a dispatcher's to take nor a
	// session for Running to show. They are in no other list.
	Waiting []StoryDetail
	Blocked []StoryDetail
	// Others is what every other host named in a story's Path has in hand, one
	// entry per host, in host order.
	Others []HostWork
	// FuelToday is every token the seat's ledger charged today, summed from
	// the lines the ledger dates today.
	FuelToday int
	// RigMemory are the rigs whose memory is larger than RigMemoryBudget, in rig
	// order. It is empty, and the report has no section for it, when none is.
	RigMemory []RigMemorySize
	// RigMemoryBudget is the size a rig's memory was held to.
	RigMemoryBudget int
}

// Run reads the report and prints it. Every call it makes is a read: the
// tracker's RunningStories, StoryState and OpenSteps, ReadyForHost,
// BlockedForHost and WorkElsewhere, one Note per other host, and the vault's
// ReadLedger and RigMemorySizes. Nothing is claimed, nothing is poured, nothing
// is written.
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

	ready, err := s.Tracker.ReadyForHost(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is ready on %s: %w", s.Host, err)
	}
	for _, detail := range ready {
		if detail.Hitl() {
			report.Waiting = append(report.Waiting, detail)
			continue
		}
		report.Ready = append(report.Ready, detail)
	}

	blocked, err := s.Tracker.BlockedForHost(ctx, s.Host)
	if err != nil {
		return report, fmt.Errorf("reading what is blocked on %s: %w", s.Host, err)
	}
	report.Blocked = blocked

	others, err := s.elsewhere(ctx)
	if err != nil {
		return report, err
	}
	report.Others = others

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
// them, and when each last recorded itself level. It costs one listing plus
// one note per host that has work, and writes nothing.
//
// A host is called asleep when the last sync it recorded is further back than
// the threshold, when it has never recorded one, or when what it recorded is
// not a time. None of those is an error: a host that cannot say when it was
// last level is exactly the host whose work a person must look at.
func (s Status) elsewhere(ctx context.Context) ([]HostWork, error) {
	if s.Notes == nil {
		return nil, nil
	}
	stories, err := s.Tracker.WorkElsewhere(ctx, s.Host)
	if err != nil {
		return nil, fmt.Errorf("reading what the other hosts hold: %w", err)
	}

	byHost := map[string][]StoryDetail{}
	var hosts []string
	for _, detail := range stories {
		host := detail.Merged().Host
		if host == "" || host == s.Host {
			continue
		}
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
	switch {
	case w.Unreadable():
		clip(b, fmt.Sprintf("    its note says %q, which is not a time", w.Said))
	case !w.LastSync.IsZero():
		clip(b, fmt.Sprintf("    last sync %s (a cycle behind)", w.LastSync.UTC().Format(LastSyncFormat)))
	}
	if w.Asleep {
		clip(b, "    re-path: "+RepathHint+here)
	}

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
// line of note when there is something to say. Every line is clipped to
// Width on its own, so a title of any length never widens the report.
func writeStory(b *strings.Builder, d StoryDetail, note string) {
	writeStoryIn(b, "  ", d, note)
}

// writeStoryIn is writeStory indented under something else — a story listed
// under the host it is pathed to, rather than under a heading.
func writeStoryIn(b *strings.Builder, pad string, d StoryDetail, note string) {
	clip(b, fmt.Sprintf("%s%s · %s", pad, d.Story.ID, d.Merged().Rig))
	clip(b, pad+"  "+d.Story.Title)
	if note != "" {
		clip(b, pad+"  "+note)
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

package application

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
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

// CancelledWindow is how far back mw status shows a cancelled run.
const CancelledWindow = 24 * time.Hour

// WaitingHeading is what heads the section for stories the Governor must be
// present for. The report leaves the section out when there are none.
const WaitingHeading = "WAITING FOR THE GOVERNOR"

// WaitingOnMayorHeading is what heads the section for needs that wait on the
// Mayor: a hands bead with no step filed yet. The report leaves the section
// out when there are none.
const WaitingOnMayorHeading = "WAITING ON THE MAYOR"

// HeldHandsHeading is what heads the section for held beads that keep a hands
// step: the view offers no Run until the bead is open. The report leaves the
// section out when there are none.
const HeldHandsHeading = "HELD, WITH A HANDS STEP"

// HeldHandsReader finds the beads that are held yet keep a hands step not yet
// run: the view offers the Governor a Run only on a workable bead (workable),
// so his phone shows none until the bead is open. MayorReader is the one
// reader.
type HeldHandsReader interface {
	HeldHands(ctx context.Context) ([]StoryDetail, error)
}

// MayorNeeds reads the needs that wait on the Mayor among the beads labelled
// hitl, without writing anything. MayorReader is the one reader.
type MayorNeeds interface {
	MayorNeeds(ctx context.Context, hitl []StoryDetail) ([]PosternViewNeed, error)
}

// EpicsMissingHeading heads the section naming the open epics that do not meet
// what their rig requires of an epic, and EpicsWaivedHeading the one naming the
// epics the Governor waived it for. Each is left out when it has none.
const (
	EpicsMissingHeading = "EPICS MISSING REQUIREMENTS"
	EpicsWaivedHeading  = "EPICS WAIVED"
)

// FinishedHeading is what heads the section naming the open beads that look
// finished: an epic or map whose children are all closed, a grilling or
// research ticket whose epics are all closed. Nothing is closed by it: the
// Mayor decides.
const FinishedHeading = "DONE, STILL OPEN"

// BeadGraph reads every bead the tracker holds, closed ones included, with the
// links each carries, for the DONE, STILL OPEN section. It reads and writes
// nothing.
type BeadGraph interface {
	BeadGraph(ctx context.Context) ([]domain.GraphBead, error)
}

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
// budget, when nothing says otherwise. A host that serves every rig's beads
// raises it with the beads_budget_bytes key of ~/.config/mw/config.toml
// (infrastructure/config.BeadsBudgetBytes).
const DefaultBeadsBudgetBytes = 1_500_000_000

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

	// Mayor is where the needs waiting on the Mayor are read from, for the
	// WAITING ON THE MAYOR section, from the beads labelled hitl the report
	// lists. A nil Mayor leaves the section out.
	Mayor MayorNeeds

	// HeldHands is where the held beads that keep a hands step are read from,
	// for the HELD, WITH A HANDS STEP section. A nil HeldHands leaves it out.
	HeldHands HeldHandsReader

	// Rules is where each rig's requirements of its epics are read from, for
	// the EPICS MISSING REQUIREMENTS and EPICS WAIVED sections. A nil Rules
	// leaves both out.
	Rules EpicRules

	// Graph is where the beads that look finished are found, for the DONE, STILL
	// OPEN section. A nil Graph leaves the section out.
	Graph BeadGraph

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

	// SyncMode is how this host's beads database is synced, config
	// beads_sync: said on the report's BEADS SYNC line, with the last backup
	// on the host that keeps the one database, and read for whether another
	// host's note of its last sync is a cycle behind (remote) or read live
	// out of the one database every host shares. Empty reads
	// BeadsSyncRemote.
	SyncMode BeadsSyncMode

	// Home reads the vault's home file, to turn a SyncMode of BeadsSyncAuto
	// into backup (this host is home) or shared (it is not). Nil, or a home
	// that cannot be told, leaves it auto and unresolved.
	Home HomeFile

	// Ticks are this host's logs of its timers' runs, counted for the TICKS
	// section. A log that is not there, or that no line was ever written to,
	// leaves its timer out; with neither there is no section.
	Ticks TickLogs

	// Events, when set, is where the event follower's sending is read from,
	// for the EVENTS line. Nil leaves the line out.
	Events EventsShipping

	// Log, when set, is read for the IDLE line: how long it has held no
	// event but the jobs' own. IdleAfter is how long that is before the
	// factory is called idle; Harness, when set, counts the harness
	// processes alive, which is none when it is.
	Log       EventLog
	IdleAfter time.Duration
	Harness   HarnessCount

	// Network, when set, is asked for the NETWORK line: metered or not.
	Network NetworkReader

	// VPSNginx, when set, is asked for the VPS NGINX line: whether the VPS's
	// postern_api upstream sends the phone to the home first.
	VPSNginx VPSNginxReader

	// Standby, when set, is asked for the standby line: whether the VPS's backend
	// runs the home's commit.
	Standby StandbyReader

	// Control, when set, is the home's event log, read for the CANCELLED
	// section (the cancel events of the last day) and the PAUSED line. Nil
	// leaves both out.
	Control EventLog

	// Now is the clock "today" is read by, for picking out the ledger's lines
	// dated today. The zero value reads the real one.
	Now func() time.Time

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// HarnessCount counts the processes of the harness alive on this host: what an
// idle factory has none of, since a seat's reaper closes an idle window.
type HarnessCount interface {
	Count(ctx context.Context) (int, error)
}

// EventsShipping says where the follower's sending of events stands.
// EventShip is the one reader.
type EventsShipping interface {
	Status(ctx context.Context) (ShipStatus, error)
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

// Refused reports whether this story's landing was refused (or its attempts
// ran out) and it now waits on the Mayor: run state blocked.
func (r RunningStory) Refused() bool { return r.Run == RunBlocked }

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
	// WaitingOnMayor are the needs the view says wait on the Mayor, oldest
	// first (one with no known age last), and NeedsAt is the clock their age is read against.
	WaitingOnMayor []PosternViewNeed
	// HeldHands are the held beads that keep a hands step, in id order.
	HeldHands []StoryDetail
	NeedsAt   time.Time
	Blocked   []StoryDetail
	// Others is what every other host named in a story's Path has in hand, one
	// entry per host, in host order.
	Others []HostWork
	// EpicShortfalls are the open epics of rigs that require something which do
	// not meet it or were waived, in the order the tracker lists them.
	EpicShortfalls []EpicShortfall
	// Finished are the open beads that look finished, in the order the tracker
	// lists them; FinishedKnown says the tracker was asked, so that "nothing" is
	// told from "not asked".
	Finished      []domain.Finished
	FinishedKnown bool
	// Ticks are how this host's own timers are doing, counted from their logs.
	Ticks HostTicks
	// Events is where the event follower stands; nil when it was not asked
	// or could not be read.
	Events *ShipStatus
	// Idle is when the factory fell idle: the time of the last event that
	// was no job's, once none but jobs' events have come for IdleAfter and
	// no job is in flight. Zero when the factory is not idle, or the log was
	// not asked or could not be read.
	Idle time.Time
	// Harness is the count of harness processes, when HarnessKnown.
	Harness      int
	HarnessKnown bool
	// Network is whether the network is metered; nil when not asked.
	Network *NetworkReading
	// VPSNginx is what the VPS's nginx upstream was found to be; nil when
	// not asked.
	VPSNginx *VPSNginxReading
	// Standby is how the standby's backend compares with the home's; nil when not
	// asked.
	Standby *StandbyReading
	// Cancelled are the runs a cancel event ended in the last day, and Paused
	// the pause-host event this host is under, if any.
	Cancelled []Cancel
	Paused    *HostPause
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

	// SyncMode is how this host's beads are synced. It is said on its own
	// line only when BeadsKnown is: a report with no Notes says nothing of
	// the beads at all.
	SyncMode BeadsSyncMode
	// SyncWhy says how an auto SyncMode was resolved ("auto: home"), or, when
	// SyncMode is still auto, why it could not be.
	SyncWhy string
	// LastBackup is when a backup of the one database last got through, on
	// the host that keeps it, and BackupAge how long ago that was. Both are
	// zero when it never has, and when LastBackupSaid, the note as the
	// tracker held it, cannot be read as a time.
	LastBackup     time.Time
	BackupAge      time.Duration
	LastBackupSaid string
}

// Run reads the report and prints it. Every call it makes is a read: the
// tracker's WorkInHand, StoryState and OpenSteps, ReadyWithLabel and
// BlockedForHost, two Notes per other host, the two tick logs' Read and the
// vault's ReadLedger and RigMemorySizes. WorkInHand is read once, and this
// host's running and ready stories and the other hosts' work are all narrowed
// from it. Nothing is claimed, nothing is poured, nothing is written.
func (s Status) Run(ctx context.Context) (StatusReport, error) {
	report := StatusReport{Host: s.Host, SyncMode: s.syncMode()}
	if report.SyncMode == BeadsSyncAuto {
		resolved, err := ResolveBeadsSync(ctx, s.Home, s.Host, BeadsSyncAuto)
		if unknown, ok := HomeUnknownIn(err); ok {
			report.SyncWhy = "home unknown: " + unknown.Why.Error()
		} else if err != nil {
			report.SyncWhy = "home unknown: " + err.Error()
		} else {
			report.SyncMode, report.SyncWhy = resolved.Mode, resolved.Why
		}
	}
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

	// What waits on the Mayor takes a read or two of its own, so it is read
	// while the rest of the report is, and joined before it is printed.
	var mayor chan mayorRead
	if s.Mayor != nil {
		mayor = make(chan mayorRead, 1)
		hitl := append([]StoryDetail(nil), report.Waiting...)
		go func() {
			needs, err := s.Mayor.MayorNeeds(ctx, hitl)
			mayor <- mayorRead{needs, err}
		}()
	}

	var heldHands chan heldHandsRead
	if s.HeldHands != nil {
		heldHands = make(chan heldHandsRead, 1)
		go func() {
			held, err := s.HeldHands.HeldHands(ctx)
			heldHands <- heldHandsRead{held, err}
		}()
	}

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

	if report.EpicShortfalls, err = s.epicShortfalls(ctx); err != nil {
		return report, err
	}

	if s.Graph != nil {
		graph, err := s.Graph.BeadGraph(ctx)
		if err != nil {
			return report, fmt.Errorf("reading which open beads look finished: %w", err)
		}
		report.Finished, report.FinishedKnown = domain.FinishedStillOpen(graph), true
	}

	fuel, err := s.fuelToday(ctx)
	if err != nil {
		return report, fmt.Errorf("summing today's fuel from the %s seat's ledger: %w", s.Seat, err)
	}
	report.FuelToday = fuel

	if s.Events != nil {
		// A shipper that cannot be read is left out of the report, not a
		// reason to refuse the rest of it.
		if shipping, err := s.Events.Status(ctx); err == nil {
			report.Events = &shipping
		}
	}

	if s.Log != nil {
		report.Idle = s.idleSince(ctx)
	}
	if s.Harness != nil {
		if n, err := s.Harness.Count(ctx); err == nil {
			report.Harness, report.HarnessKnown = n, true
		}
	}
	if s.Network != nil {
		reading := s.Network.Read(ctx)
		report.Network = &reading
	}
	if s.VPSNginx != nil {
		reading := s.VPSNginx.Read(ctx)
		report.VPSNginx = &reading
	}
	if s.Standby != nil {
		reading := s.Standby.Read(ctx)
		report.Standby = &reading
	}
	if s.Control != nil {
		// Like the shipper, a log that cannot be read is left out of the
		// report, not a reason to refuse the rest of it.
		if cancels, err := CancelsSince(ctx, s.Control, s.now().Add(-CancelledWindow)); err == nil {
			running := map[string]bool{}
			for _, rs := range report.Running {
				running[rs.Detail.Story.ID] = true
			}
			for _, cancel := range cancels {
				if !running[cancel.Bead] {
					report.Cancelled = append(report.Cancelled, cancel)
				}
			}
		}
		if pause, paused, err := PausedHost(ctx, s.Control, s.Host); err == nil && paused {
			report.Paused = &pause
		}
	}

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

		if report.SyncMode == BeadsSyncBackup {
			said, err := s.Notes.Note(ctx, LastBackupKey(s.Host))
			if err != nil {
				return report, fmt.Errorf("reading when %s last backed up its beads: %w", s.Host, err)
			}
			report.LastBackupSaid = strings.TrimSpace(said)
			if at, err := time.Parse(LastSyncFormat, report.LastBackupSaid); err == nil {
				report.LastBackup = at
				report.BackupAge = s.now().Sub(at)
			}
		}
	}

	if mayor != nil {
		read := <-mayor
		if read.err != nil {
			return report, fmt.Errorf("reading what waits on the Mayor: %w", read.err)
		}
		report.WaitingOnMayor = read.needs
		sort.SliceStable(report.WaitingOnMayor, func(i, j int) bool {
			a, b := report.WaitingOnMayor[i].Since, report.WaitingOnMayor[j].Since
			return b == "" && a != "" || a != "" && b != "" && a < b
		})
		report.NeedsAt = s.now()
	}

	if heldHands != nil {
		read := <-heldHands
		if read.err != nil {
			return report, fmt.Errorf("reading the held beads that keep hands steps: %w", read.err)
		}
		report.HeldHands = read.held
	}

	if err := report.markGuests(ctx, s.Rules); err != nil {
		return report, err
	}

	s.print(report.String())
	return report, nil
}

// markGuests sets Guest on every story of the report whose rig is a guest repo.
func (r *StatusReport) markGuests(ctx context.Context, rules EpicRules) error {
	var all []*StoryDetail
	for i := range r.Running {
		all = append(all, &r.Running[i].Detail)
	}
	for _, list := range [][]StoryDetail{r.Ready, r.Waiting, r.HeldHands, r.Blocked} {
		for i := range list {
			all = append(all, &list[i])
		}
	}
	for i := range r.Others {
		for j := range r.Others[i].Stories {
			all = append(all, &r.Others[i].Stories[j])
		}
	}
	return markGuests(ctx, rules, all...)
}

// heldHandsRead is what reading the held beads that keep hands steps came back with.
type heldHandsRead struct {
	held []StoryDetail
	err  error
}

// mayorRead is what reading the Mayor's needs came back with.
type mayorRead struct {
	needs []PosternViewNeed
	err   error
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

// syncMode is how this host's beads are synced, remote when nothing says.
func (s Status) syncMode() BeadsSyncMode {
	if s.SyncMode == "" {
		return BeadsSyncRemote
	}
	return s.SyncMode
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
		if r.SyncMode == BeadsSyncBackup {
			clip(&b, backupHaltLine(r.Host, *r.Halt))
		} else {
			clip(&b, haltLine(r.Host, *r.Halt))
		}
		b.WriteString("\n")
	}

	if r.BeadsKnown {
		clip(&b, beadsLine(r.BeadsBytes, r.BeadsBudgetBytes))
		clip(&b, r.syncModeLine())
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

	if r.Paused != nil {
		clip(&b, fmt.Sprintf("PAUSED by %s %s: no story is started", r.Paused.Actor, r.Paused.At.UTC().Format("01-02 15:04Z")))
		b.WriteString("\n")
	}

	if len(r.Cancelled) > 0 {
		clip(&b, fmt.Sprintf("CANCELLED (%d)", len(r.Cancelled)))
		for _, cancel := range r.Cancelled {
			clip(&b, fmt.Sprintf("  %s · cancelled by %s %s", cancel.Bead, cancel.Actor, cancel.At.UTC().Format("01-02 15:04Z")))
		}
		b.WriteString("\n")
	}

	clip(&b, fmt.Sprintf("READY (%d)", len(r.Ready)))
	if len(r.Ready) == 0 {
		clip(&b, "  nothing ready")
	}
	for _, d := range r.Ready {
		note := ""
		if d.Merged().Host == domain.HostAuto {
			note = "host " + domain.HostAuto
		}
		writeStory(&b, d, note)
	}
	b.WriteString("\n")

	if len(r.Waiting) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", WaitingHeading, len(r.Waiting)))
		for _, d := range r.Waiting {
			writeStory(&b, d, readyOrClaimed(d))
		}
		b.WriteString("\n")
	}

	if len(r.WaitingOnMayor) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", WaitingOnMayorHeading, len(r.WaitingOnMayor)))
		for _, need := range r.WaitingOnMayor {
			clip(&b, "  "+need.Bead+" · "+needAge(need, r.NeedsAt))
			clip(&b, "    "+needWhy(need))
		}
		b.WriteString("\n")
	}

	if len(r.HeldHands) > 0 {
		clip(&b, fmt.Sprintf("%s (%d)", HeldHandsHeading, len(r.HeldHands)))
		for _, d := range r.HeldHands {
			writeStory(&b, d, "no Run until it is open")
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
		w.write(&b, r.Host, r.SyncMode.OneDatabase())
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

	r.writeEpicShortfalls(&b)

	if r.FinishedKnown {
		clip(&b, fmt.Sprintf("%s (%d)", FinishedHeading, len(r.Finished)))
		if len(r.Finished) == 0 {
			clip(&b, "  nothing")
		}
		for _, f := range r.Finished {
			clip(&b, "  "+f.ID+" · "+f.Title)
			clip(&b, "    "+f.Why)
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

	if r.Events != nil {
		clip(&b, r.Events.Line())
		b.WriteString("\n")
	}

	if !r.Idle.IsZero() {
		line := "IDLE since " + r.Idle.Format("15:04")
		if r.HarnessKnown {
			line += fmt.Sprintf(" · %d harness processes", r.Harness)
		}
		clip(&b, line)
		b.WriteString("\n")
	} else if r.HarnessKnown {
		clip(&b, fmt.Sprintf("HARNESS %d processes", r.Harness))
		b.WriteString("\n")
	}

	if r.Network != nil {
		clip(&b, r.Network.Line())
		b.WriteString("\n")
	}

	if r.VPSNginx != nil {
		clip(&b, r.VPSNginx.Line())
		b.WriteString("\n")
	}
	if r.Standby != nil {
		clip(&b, r.Standby.Line())
		b.WriteString("\n")
	}

	clip(&b, fmt.Sprintf("FUEL today: %s tokens", Thousands(r.FuelToday)))
	b.WriteString("\n")
	return b.String()
}

// writeEpicShortfalls is the two sections about epics and their rig's
// requirements: the epics still missing something, then the ones the Governor
// waived. Each is left out when there is nothing to put in it.
func (r StatusReport) writeEpicShortfalls(b *strings.Builder) {
	for _, section := range []struct {
		heading string
		waived  bool
	}{{EpicsMissingHeading, false}, {EpicsWaivedHeading, true}} {
		var lines []string
		for _, epic := range r.EpicShortfalls {
			lacks := epic.Missing
			if section.waived {
				lacks = epic.Waived
			}
			if len(lacks) == 0 {
				continue
			}
			var names []string
			for _, lack := range lacks {
				if lack.Section {
					names = append(names, lack.Name+" section")
				} else {
					names = append(names, lack.Name+" story")
				}
			}
			lines = append(lines, "  "+epic.ID+" · "+strings.Join(names, ", "))
		}
		if len(lines) == 0 {
			continue
		}
		clip(b, fmt.Sprintf("%s (%d)", section.heading, len(lines)))
		for _, line := range lines {
			clip(b, line)
		}
		b.WriteString("\n")
	}
}

// needAge is how long a need has waited at now, or that it is not known.
func needAge(need PosternViewNeed, now time.Time) string {
	since, err := time.Parse(time.RFC3339, need.Since)
	if err != nil || since.IsZero() {
		return "age unknown"
	}
	return Clock(now.Sub(since))
}

// needWhy is the one line a need is worth: its kind, and what it waits on or,
// where it names nothing, what it says.
func needWhy(need PosternViewNeed) string {
	if len(need.WaitingOn) > 0 {
		return need.Kind + ": " + strings.Join(need.WaitingOn, ", ")
	}
	return need.Kind + ": " + need.Text
}

// write is one other host's block: the host and how long it has been quiet,
// the sync it last recorded, and the stories pathed to it — marked stranded,
// with the one line that re-paths them, when the host is asleep. here is the
// host the report is for, and so the host a stranded story is re-pathed to.
// live says the note was read straight out of the one database every host
// shares, rather than out of this host's own copy, which is a cycle behind.
func (w HostWork) write(b *strings.Builder, here string, live bool) {
	clip(b, fmt.Sprintf("  %s · %s", w.Host, w.state()))
	if w.Halt != nil {
		clip(b, "    "+haltLine(w.Host, *w.Halt))
	}
	switch {
	case w.Unreadable():
		clip(b, fmt.Sprintf("    its note says %q, which is not a time", w.Said))
	case !w.LastSync.IsZero() && live:
		clip(b, fmt.Sprintf("    last sync %s", w.LastSync.UTC().Format(LastSyncFormat)))
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

// backupHaltLine is haltLine on the host that keeps the one database, where
// the only cycle that can halt is its backup.
func backupHaltLine(host string, halt SyncHaltInfo) string {
	return fmt.Sprintf("host %s: beads backup halted since %s: %s", host, halt.At.UTC().Format(LastSyncFormat), halt.Said)
}

// syncModeLine is the one line how this host's beads are synced is worth,
// and on the host that keeps the one database, when it last backed it up.
func (r StatusReport) syncModeLine() string {
	line := r.syncModeSaid()
	if r.SyncMode == BeadsSyncShared && r.SyncWhy != "" {
		// "boost of desktop" already says the database is kept elsewhere, and
		// both together would not fit a phone.
		return "BEADS SYNC shared · " + r.SyncWhy
	}
	if r.SyncWhy != "" {
		line += " · " + r.SyncWhy
	}
	return line
}

func (r StatusReport) syncModeSaid() string {
	switch r.SyncMode {
	case BeadsSyncBackup:
		switch {
		case r.LastBackupSaid == "":
			return "BEADS SYNC backup · never backed up"
		case r.LastBackup.IsZero():
			return "BEADS SYNC backup · last backup unreadable"
		default:
			return "BEADS SYNC backup · backed up " + Clock(r.BackupAge) + " ago"
		}
	case BeadsSyncShared:
		return "BEADS SYNC shared · kept on another host"
	case BeadsSyncAuto:
		return "BEADS SYNC auto"
	default:
		return "BEADS SYNC remote"
	}
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
// or stuck rather than running, a landing refused and waiting on the Mayor, or
// a formula step still open that will refuse to let it close out.
func (r RunningStory) write(b *strings.Builder) {
	writeStory(b, r.Detail, "session "+r.Session)
	if r.Stopped() {
		clip(b, fmt.Sprintf("    NOT RUNNING: run=%s", r.Run))
	}
	if r.Refused() {
		clip(b, "    refused, waiting on the Mayor (mw retry or a hold)")
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
	if rig := d.Merged().Rig; rig != "" && d.Guest != "" {
		clip(b, fmt.Sprintf("%s%s · %s · guest: %s", pad, d.Story.ID, rig, d.Guest))
	} else if rig != "" {
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

// idleLookback is how many of the log's last events idleSince reads.
const idleLookback = 200

// idleSince is when the factory fell idle, or the zero time when it has not:
// the time of the last event no job wrote, when every event since is a job's,
// the last non-job event is IdleAfter old, and no job has begun a pass it has not
// ended. A log it cannot read says nothing.
func (s Status) idleSince(ctx context.Context) time.Time {
	after := s.IdleAfter
	if after <= 0 {
		return time.Time{}
	}
	head, err := s.Log.Head(ctx)
	if err != nil || head == 0 {
		return time.Time{}
	}
	var from uint64
	if head > idleLookback {
		from = head - idleLookback
	}
	evs, err := s.Log.Since(ctx, from)
	if err != nil || len(evs) == 0 {
		return time.Time{}
	}
	var since time.Time
	state := map[string]string{}
	for _, e := range evs {
		if e.Kind != events.KindJob {
			since = e.Ts
			continue
		}
		state[e.Actor] = e.To
	}
	if since.IsZero() {
		since = evs[0].Ts
	}
	for _, to := range state {
		if to == events.JobScheduled || to == events.JobRunning {
			return time.Time{}
		}
	}
	if s.now().Sub(since) < after {
		return time.Time{}
	}
	return since.In(s.now().Location())
}

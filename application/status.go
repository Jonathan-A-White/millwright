package application

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
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

// OtherWait is a bead waiting on someone outside the factory: who it waits on
// ("" when no asked-of label says) and since when (zero when unknown).
type OtherWait struct {
	Story StoryDetail
	Of    string
	Since time.Time
}

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

// DamperNotes is the read-only half of the tracker's notes the alarms read his
// open cards and unrun hands steps from.
type DamperNotes interface {
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
}

// DamperReader judges the DAMPERS alarms from the work already read, at now.
// DamperSurvey is the one reader; it reads and writes nothing else.
type DamperReader interface {
	Dampers(ctx context.Context, work WorkInHand, now time.Time) (DampersReport, error)
}

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

// LandingsInFlight names the rigs with an `mw land` alive on this host, or ended
// with no follower told how: LandProcesses.Pending.
type LandingsInFlight interface {
	Pending(ctx context.Context) ([]string, error)
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
	// Rigs are the rigs checked out on this host, by name, and Changes reads
	// what a running story's branch in one has changed so far: a ready story
	// that shares a file with a running one is shown waiting, as mw dispatch
	// passes it over (mw-gq6.349). A nil Changes compares the files keys alone.
	Rigs    map[string]string
	Changes BranchChanges
	// TestersPerRig is the [dampers] testers_per_rig key, which mw dispatch
	// enforces: a ready Tester story is shown waiting while its rig runs that
	// many, on any host (mw-gq6.546). Zero reads the default.
	TestersPerRig int
	// RigFiles says whether a path of a story's files key is in its rig's
	// checkout on this host, to name the stories filed into the wrong rig under
	// WrongRigHeading (mw-gq6.428). Nil leaves the section out.
	RigFiles RigFiles
	// MainCommit is the commit of the factory rig's main as this host last
	// fetched it, the one every host's mw is to be level with. Empty when it is
	// not known, and then no host is said to be behind it.
	MainCommit string
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

	// HasGristTestKey says whether this host holds the grist test key, as
	// Dispatch.HasGristTestKey does: a ready story that needs it is shown with
	// the reason mw dispatch passes it by here. Nil says nothing.
	HasGristTestKey func() bool

	// Mayor is where the needs waiting on the Mayor are read from, for the
	// WAITING ON THE MAYOR section, from the beads labelled hitl the report
	// lists. A nil Mayor leaves the section out.
	Mayor MayorNeeds

	// HeldHands is where the held beads that keep a hands step are read from,
	// for the HELD, WITH A HANDS STEP section. A nil HeldHands leaves it out.
	HeldHands HeldHandsReader

	// Icebox is where the ICEBOX line is read from. A nil Icebox leaves it out.
	Icebox interface {
		Icebox(ctx context.Context) (IceboxSummary, error)
	}

	// Rules is where each rig's requirements of its epics are read from, for
	// the EPICS MISSING REQUIREMENTS and EPICS WAIVED sections. A nil Rules
	// leaves both out.
	Rules EpicRules

	// Suites, when set, is where each nightly-suite rig's newest kept result is
	// read from, for the SUITE section; with Rigs and Rules, which say which
	// rigs run it. Nil leaves the section out.
	Suites SuiteHistory
	// SuiteHeads, when set, reads the head of each suite rig's main as its
	// checkout has it, with no fetch, so that a suite is called late only when
	// main has moved since it (mw-t5shinh.67). Nil, or a head that cannot be
	// read, is a head not known to be the result's.
	SuiteHeads FactoryCommits

	// Landings, when set, names the rigs with an `mw land` alive on this host
	// (LandProcesses.Pending), and GateHolder whose gate holds the host's gate
	// lock, "" for nobody: a queued story whose rig's lander is alive is not
	// stuck, and one whose lander waits for another rig's gate says so
	// (mw-gq6.858). Nil says neither.
	Landings   LandingsInFlight
	GateHolder func() string

	// Batches, when set, is where the batch lines of metrics/factory.jsonl are
	// read from, for the last batch of each rig under LAND RUSH; which rigs
	// have a line is told by Rigs and Rules (landing = "batch"). Nil leaves the
	// last batch out of the line.
	Batches BatchHistory

	// Dampers, when set, is where the DAMPERS alarms are judged. Nil leaves the
	// section out, as does a report with no alarm tripped.
	Dampers DamperReader

	// Graph is where the beads that look finished are found, for the DONE, STILL
	// OPEN section. A nil Graph leaves the section out.
	Graph BeadGraph

	// HostSilence is how long another host's recorded sync may be behind
	// before its work is called stranded. Zero reads DefaultHostSilence.
	HostSilence time.Duration
	// Retired are the hosts the Governor retired (retired_hosts): each is one
	// line under OTHER HOSTS, never called asleep, and its work is shown under
	// the line that re-paths it (mw-6ww.163.1).
	Retired []string
	// GiveWayStale is giveway_stale: a host's give-way note not rewritten for twice
	// this is not believed (mw-2891blf.3). Zero reads DefaultGiveWayStale.
	GiveWayStale time.Duration
	// hereGivesWay is this host's own give-way note as the run read it: the
	// stories claimed here are frozen, not stranded or without a session.
	hereGivesWay GiveWayRead
	// Signs is where another host's other signs of life are read, so that a
	// host whose last sync is old but which has committed to the vault, or
	// whose claimed story's lease was renewed, within HostSilence is not called
	// asleep (mw-gq6.523). A nil Signs, or signs that cannot be read, judge a
	// host by its sync and its claims alone.
	Signs HostSigns
	// RigMemoryBytes is how large the Seat's memory of one rig may be before the
	// report says it is due to be pruned. Zero reads DefaultRigMemoryBytes.
	RigMemoryBytes int
	// Errors, when set, is the common-errors register: the classes not yet
	// checked in every rig of their kinds are named under
	// UncheckedErrorsHeading (mw-gq6.420).
	Errors ErrorFiles
	// RigFacts, when set, is where each rig kept as facts has its eval.md run
	// from; a rig whose eval fails is named under RIG MEMORY.
	RigFacts RigFactFiles
	// BeadsBudgetBytes is how large this host's own beads database may grow
	// before the report warns it is past budget. Zero reads
	// DefaultBeadsBudgetBytes.
	BeadsBudgetBytes int64
	// Seat is whose ledger today's fuel is summed from — the Builder's, since
	// the Builder is the seat that works every story.
	Seat string

	// Ideas, when set, is the Governor's Ideas only: while it is on the report
	// opens with one line saying so and how many of his ideas are held (the
	// state file, and the backend when it answers within IdeasOnlyLineWait).
	// Nil leaves the line out.
	Ideas *IdeasOnlyMode

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

	// Sessions, when set, is asked whether a claimed story's session is alive,
	// to name a story in progress past NoSessionNamedAfter with none
	// (mw-gq6.797). Nil leaves that line out.
	Sessions SessionStatuser

	// Panes is this host's terminal, whose windows are read for a seat stopped
	// at the usage limit (AtTheUsageLimitHeading). A nil Panes leaves the
	// section out.
	Panes ReapTerminal

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

	// Turns, when set, reads how many turns each running story's session has
	// taken so far, in its worktree beside its rig's checkout in Rigs, so that
	// one past TurnsNamedPast is named (mw-1ljpo9.6). Nil names none.
	Turns TranscriptTurns
	// MaxTurns is the host's next.max_turns, and MaxTurnsSaid whether its config
	// says it, so that a session's turn cap is read as a Builder's boot reads it:
	// by the story's size (mw-gq6.852). MaxTurns 0 and not said is the default.
	MaxTurns     int
	MaxTurnsSaid bool

	// CloseOuts, when set, is where the close-outs running on this host are
	// read from, so that a story being closed out says so beside it. Nil
	// leaves the line out.
	CloseOuts CloseOutMarks

	// Network, when set, is asked for the NETWORK line: metered or not.
	Network NetworkReader

	// Usage, when set, is where the newest quota reading is read for the USAGE
	// line, with STALE beside it when it is old or from before the week's reset
	// (mw-gq6.855). Nil leaves the line out.
	Usage *UsageReadings

	// HeavyNet names the rigs that install dependencies ([heavy_net]): while the
	// network is metered, a ready story pinned to this host on one of them is
	// named under DAMPERS, since dispatch passes it (mw-gq6.702).
	HeavyNet map[string]bool

	// Cap is how many sessions may run on this host at once, and Load and Room
	// are how its room to start another is judged (mw-t0z3fu.1): the HOST line
	// says how many of the cap are in use and, when the host has no room, why
	// mw dispatch holds a story back. A zero Cap leaves the line out; a nil Load,
	// or a load that cannot be read, says nothing of room.
	Cap int
	// Drained says the host is held at cap 0, drained for a restart: the HOST
	// line says so, since a zero Cap leaves the line out (mw-gq6.1044).
	Drained bool
	// CapWhy names the dispatch window that sets Cap now, like "night window to
	// 06:00 America/New_York"; "" when the cap is the host's own (mw-gq6.429).
	CapWhy string
	Load   HostLoad
	Room   RoomLimits

	// Grist is what the mill is doing on this host (mw-t0z3fu.4): the GRIST line
	// says whether a tutor is in use and how its answers compare with their par,
	// and a tutor answering slowly is a reason the host is held back. Nil leaves
	// the line out.
	Grist GristPulses

	// VPSNginx, when set, is asked for the VPS NGINX line: whether the VPS's
	// postern_api upstream sends the phone to the home first.
	VPSNginx VPSNginxReader

	// Standby, when set, is asked for the standby line: whether the VPS's backend
	// runs the home's commit.
	Standby StandbyReader

	// Cloud, when set, is the cloud's book, read with CloudPlan for the CLOUD
	// section: each box up, its age, and the month's spend against the cap.
	// Nil leaves it out.
	Cloud     CloudBook
	CloudPlan CloudPlan

	// BoxPanes, when set with Cloud, is asked for the session panes each cloud
	// box kept in the last day, so that a box whose harness (Claude Code on Bun)
	// crashed twice or more in a day is named under HARNESS (mw-gq6.1139). Nil
	// leaves the crashes out; the harness version of each box is printed all the
	// same, from the cloud's book.
	BoxPanes BoxPanes

	// Benchmarks, when set, is the book of past close-outs' benchmarks, read for
	// the BENCHMARKS section: each host's usual gate time against its latest,
	// what each host landed at each running count, and how well the par of each
	// kind has held (mw-t0z3fu.2), by Bench's thresholds. Nil leaves it out.
	Benchmarks BenchmarkBook
	Bench      BenchmarkSettings

	// Causes, when set, is read for the CAUSES MISSING section: the stories
	// refused in the last CausesMissingWindow that no cause is recorded for
	// (mw cause). Nil leaves it out.
	Causes TesterStories

	// Smoke, when set, is the record of each app's last grist smoke, read for
	// the GRIST SMOKE section: a failure, what it holds back, and a kind with
	// no example as a warning (mw-gq6.319). Nil leaves it out.
	Smoke GristSmokeRecords

	// Apps is where each app's rig is checked out on this host ([grist-apps]),
	// and Visions reads the VISION.md of each: an app without one is a VISION
	// line. Either nil leaves the lines out.
	Apps    map[string]string
	Visions VisionSource

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
	// UsageLimit is the harness's own words when the session's result says it
	// stopped at the usage limit ("You've hit your weekly limit · resets …"),
	// empty otherwise: a story parked there waits on the limit, not on its
	// work (mw-gq6.433).
	UsageLimit string
	// Turns is how many turns the story's session has taken so far, 0 when it
	// was not read (mw-1ljpo9.6).
	Turns int
	// TurnCap is the most turns the story's session may take, as its Builder was
	// booted with it (mw-gq6.852); 0 for none or not known.
	TurnCap int
	// CloseOut is the close-out running on this host for the story, nil when
	// none is. Now is when it was read, to say how long it has run.
	CloseOut *CloseOutMark
	Now      time.Time
	// Stranded is when the claim's lease ran out with no heartbeat since, zero
	// while it holds or when the story is not one a session is meant to be
	// renewing (refused, queued, handed back): a claim whose session never
	// started or is gone looks exactly so (mw-gq6.576).
	Stranded time.Time
	// NoSession is when the claim began, when the story has been in progress
	// longer than NoSessionNamedAfter with no live session behind it: a dead
	// pane or no window (mw-gq6.797). Zero otherwise, and when the runner was
	// not asked.
	NoSession time.Time
	// Wait is what a queued story waits for, set only for one run=queued
	// (mw-gq6.604).
	Wait *BatchWait
}

// Stopped reports whether this story's session is not to be trusted as
// running, whatever its claim says: it was marked stopped or stuck.
func (r RunningStory) Stopped() bool {
	return r.Run == RunStopped || r.Run == RunStuck
}

// Queued reports whether this story passed its close-out on a rig that lands in
// batches and waits for its batch: run state queued. It is alive, not stopped,
// though its session is gone and its lease has lapsed.
func (r RunningStory) Queued() bool { return r.Run == RunQueued }

// HandedBack reports whether this story's close-out waited the overall most for
// its rig's merge slot and handed it back for its close-out alone: run state
// close-out-again (mw-gq6.467). It has no session until the next dispatch tick
// runs its close-out again.
func (r RunningStory) HandedBack() bool { return r.Run == RunCloseOutAgain }

// Refused reports whether this story's landing was refused (or its attempts
// ran out) and it now waits on the Mayor: run state blocked.
func (r RunningStory) Refused() bool { return r.Run == RunBlocked }

// FormulaOpen reports whether this story cannot be closed out yet because a
// step of its poured formula is still open.
func (r RunningStory) FormulaOpen() bool { return r.FormulaSteps > 0 }

// CloseBlocked reports whether an open formula step is what keeps this story
// from closing out. A story queued for batch landing has passed its close-out
// already, and the step it has open is the landing its batch does, so a wait
// line says where it is and nothing is blocked (mw-gq6.758).
func (r RunningStory) CloseBlocked() bool { return r.FormulaOpen() && !r.Queued() }

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
	// Pins are the stories here a dispatch tick leaves pathed to this host
	// though it cannot take them, because the pin says why it must stay: story
	// id to "held by its pin: <why>" (mw-gq6.1165).
	Pins map[string]string
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
	// Sign is the newest sign of life seen of the host besides its sync note,
	// when it is newer than LastSync, and SignAge how long it is since: a vault
	// commit it made, a claim, a renewed lease. Zero when there is none newer
	// (NewestSign).
	Sign    HostSign
	SignAge time.Duration
	// Halt is what that host's own sync-halted note says, read like every
	// other note of it — which, since the note rides the very sync that is
	// stuck, may say nothing of a halt that has not cleared yet. Nil when it
	// has recorded none.
	Halt *SyncHaltInfo
	// Now is when the report was read, to judge the lease of a claimed story.
	Now time.Time
	// Waits is what each of its stories queued for batch landing waits for,
	// by story id: such a story is never stranded by its lease (mw-gq6.604).
	Waits map[string]BatchWait
	// Runs is the run state of each of its stories in progress, by story id, as
	// the tracker has it: a refused claim is kept waiting on the Mayor and a
	// handed-back one runs its close-out again, and neither is given back by the
	// host's next dispatch tick as a stranded one is (mw-gq6.941). A story whose
	// state could not be read has none.
	Runs map[string]string
	// Retired says the Governor retired this host: it is said once, none of its
	// notes are read, and it is never asleep. Work still pathed to or claimed by
	// it is listed under the line that re-paths it.
	Retired bool
	// Asleep says this host has been silent for longer than the threshold — or
	// has never synced, or left a note that is not a time. Its stories are
	// stranded: nothing here will move them, and no other host will take them
	// until somebody re-paths them.
	Asleep bool
	// GivesWay is that host's give-way note (mw-2891blf.3): when it stands the host's
	// claimed stories are frozen, never stranded, and the host never asleep; a stale
	// one is said and not believed.
	GivesWay GiveWayRead
	// Ticks are the counts of that host's timers as it left them with its last
	// sync: nothing known of a host that has left none.
	Ticks HostTicks
	// Passed is what that host's dispatch last noted of the stories it passed
	// for a reason of its own that holds until the host is changed, by story id
	// (mw-gq6.822): a story pathed to it that it keeps passing is not "ready".
	Passed map[string]HostPassed
	// DoctorSaid is the note that host's last whole doctor run left, exactly as
	// the tracker held it ("" when it has left none); Doctor is that note read.
	// DoctorUnreadable says it holds something that is not a verdict.
	DoctorSaid       string
	Doctor           DoctorVerdict
	DoctorUnreadable bool
	// MwCommit is the short commit that host's mw was built from, as it left it
	// with its last sync, "" when it has left none. BehindMain says main's
	// commit is known and is not that one, or the host noted none: the host
	// works stories on an mw without main's newest fixes (mw-gq6.521).
	MwCommit   string
	BehindMain bool
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
	Host string
	// MainCommit is the factory rig's main as this host last fetched it, "" when
	// not known: what each other host's mw commit is read against.
	MainCommit string
	// IdeasOnly is the line 'Governor: Ideas only until 21:00 UTC (2 ideas
	// held); to-dos still work', "" when the Governor is not in Ideas only.
	IdeasOnly string
	Running   []RunningStory
	// Cap is how many sessions this host may run at once, 0 when it was not
	// told. InUse is how many of Running hold one of them, and NoRoom is why
	// mw dispatch holds a story back here, "" when the host has room or the load
	// could not be read (mw-t0z3fu.1).
	Cap    int
	CapWhy string
	InUse  int
	NoRoom string
	// Drained is the host held at cap 0 (mw-gq6.1044).
	Drained bool
	// Grist is what the mill is doing, zero (Known false) when this host has none.
	Grist GristPulse
	Ready []StoryDetail
	// NoPath is the line for each ready story mw dispatch passes over at its
	// no-path gate, by story id: a key of its path is missing or unknown
	// (mw-jmvq22o). It is said before any wait, because no wait ends it.
	NoPath map[string]string
	// Sharing are the ready stories mw dispatch passes over for a file a
	// running story here shares with them, by story id (mw-gq6.349).
	Sharing map[string]FileShare
	// TesterHolds are the ready Tester stories mw dispatch passes over because
	// their rig already runs testers_per_rig of them, by story id (mw-gq6.546).
	TesterHolds map[string]TesterHold
	// HeldByFiles is the line for each ready story mw dispatch has held for a
	// shared file longer than FilesHeldTooLong, by story id, as the host's note
	// has it (mw-gq6.442): a story starved by a files key wider than it needs
	// shows itself.
	HeldByFiles map[string]string
	// UnderHandsStep is the line for each ready story whose epic keeps a hands
	// step not yet run, by story id: dispatch does not wait on the step, so the
	// child is shown with it beside it (mw-gq6.763).
	UnderHandsStep map[string]string
	// NoGristKey is the line for each ready story mw dispatch passes by on this
	// host for want of a grist test key (gate no-grist-key), by story id: the
	// home, which holds the key, takes it (mw-1knt69p).
	NoGristKey map[string]string
	// NoHostTakes are the ready stories every host that could take them has
	// passed for a reason of the host's own (a rig not checked out there, a
	// metered network), by story id, as the hosts' notes have it (mw-gq6.703).
	NoHostTakes map[string]NoHostTakes
	// HeldByPin are the ready stories pinned to a host that cannot take them
	// (retired, gone, or full for over the re-path limit) which a dispatch tick
	// leaves there because the pin says why it must stay (PinWhy), by story id:
	// "held by its pin: <why>" (mw-gq6.1165).
	HeldByPin map[string]string
	// Waiting are the beads labelled hitl that are open and not blocked, on any
	// host or none, and the stories labelled hitl this host has claimed: worked
	// with the Governor present, so neither a dispatcher's to take nor a session
	// for Running to show. They are in no other list, most urgent first.
	Waiting []StoryDetail
	// HisToDo are the open beads labelled hitl:todo, the items on the Governor's
	// own list, which Waiting leaves out; most urgent first (mw-ev7oyy.11).
	HisToDo []StoryDetail
	// Warnings are things a person should put right, each one line: a bead for
	// the Governor with two hitl:<kind> labels.
	Warnings []string
	// WaitingOnOthers are the open beads labelled waiting:others, longest
	// waiting first: the ones whose Done-when the Mayor is to recheck.
	WaitingOnOthers []OtherWait
	// WaitingOnMayor are the needs the view says wait on the Mayor, oldest
	// first (one with no known age last), and NeedsAt is the clock their age is read against.
	WaitingOnMayor []PosternViewNeed
	// HeldHands are the held beads that keep a hands step, in id order.
	HeldHands []StoryDetail
	// HeldFor are the held-for-<event> labels some bead not yet closed carries,
	// each with those beads, in label order. Empty when no Graph was given.
	HeldFor []domain.HeldFor
	// Dampers are the alarms tripped: too much in progress, work left to age,
	// his review queue too long. Empty when none is or it was not asked.
	Dampers DampersReport
	// Icebox is the ICEBOX line's facts; its Known is false when not read.
	Icebox  IceboxSummary
	NeedsAt time.Time
	// OpenButClaimed are the open stories whose assignee is still a host
	// (mw@<host>): no session runs them and dispatch will not take them, in
	// the order the tracker listed them.
	OpenButClaimed []StoryDetail
	// NoSessionClaims are the in-progress stories NoSessionClaim names: claimed
	// by a dispatch, lease run out, and no host or session to look for them
	// (mw-gq6.877).
	NoSessionClaims []StoryDetail
	// NoSessionRuns is the run state of each of them, by story id: dispatch gives
	// back only a claim whose run state is empty or running (mw-gq6.941).
	NoSessionRuns map[string]string
	Blocked       []StoryDetail
	// HereGivesWay is this host's own give-way note when it stands, or is stale
	// (mw-2891blf.3); the zero value says nothing.
	HereGivesWay GiveWayRead
	// Others is what every other host named in a story's Path has in hand, one
	// entry per host, in host order.
	Others []HostWork
	// EpicShortfalls are the open epics of rigs that require something which do
	// not meet it or were waived, in the order the tracker lists them.
	EpicShortfalls []EpicShortfall
	// NoFilesKey are the open or held stories of rigs that expect a files key
	// (files_expected = true) whose path has none, in the order found.
	NoFilesKey []NoFilesKey
	// WrongRig are the open, held or running stories none of whose files exist
	// in their rig's checkout on this host (mw-gq6.428).
	WrongRig []WrongRig
	// HandedBack are the open stories whose newest word is a Builder's
	// hand-back with no RE-PATHED: after it, in the order the tracker listed
	// them (mw-gq6.418).
	HandedBack []HandedBackStory
	// NoSweep are the open bugs filed after the sweep rule whose acceptance has
	// no Sweep: line that passes, which mw dispatch will not claim.
	NoSweep []StoryDetail
	// NoFormula are the open stories whose path names a formula the vault has not
	// installed: mw dispatch passes them over (formula-not-installed) with one
	// comment on the bead, so the story sits unclaimed unseen (mw-gq6.497).
	NoFormula []StoryDetail
	// UncheckedErrors are the classes of the common-errors register not yet
	// checked in every rig of their kinds, by slug; UncheckedErrorsUnread is
	// why the register could not be read, when it could not.
	UncheckedErrors       []UncheckedClass
	UncheckedErrorsUnread string
	// Suites is how the last whole suite of each rig that runs one nightly went
	// (mw-8w7xb7.3), in rig order.
	Suites []SuiteStatus
	// LandRush are the rigs that land in batches, one line each, in rig order:
	// what is queued, the oldest wait, the last batch and the batches held
	// (plans/0117, Land Rush 6).
	LandRush []LandRushStatus
	// Finished are the open beads that look finished, in the order the tracker
	// lists them; FinishedKnown says the tracker was asked, so that "nothing" is
	// told from "not asked".
	Finished      []domain.Finished
	FinishedKnown bool
	// Ticks are how this host's own timers are doing, counted from their logs.
	Ticks HostTicks
	// RecheckSkips are the re-runs of a suite bug's spec before dispatch that
	// were not made in the last 24 hours, oldest first (mw-gq6.640).
	RecheckSkips []RecheckSkip
	// AtLimit is every window on this host's terminal whose pane is stopped at
	// the harness's usage limit, in the terminal's order.
	AtLimit []WindowAtLimit
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
	// UsageLines are the USAGE line, and the STALE line under it when the
	// newest reading is stale; empty when not asked.
	UsageLines []string
	// VPSNginx is what the VPS's nginx upstream was found to be; nil when
	// not asked.
	VPSNginx *VPSNginxReading
	// Standby is how the standby's backend compares with the home's; nil when not
	// asked.
	Standby *StandbyReading
	// Cloud is the cloud's boxes and the month's spend; nil when not asked or
	// not read.
	Cloud *CloudReading
	// BoxHarness is the HARNESS section: the Claude Code version of each cloud
	// box, and the harness crashes it had in the last day. Empty when the
	// cloud was not read.
	BoxHarness []BoxHarness
	// Benchmarks is what the close-outs measured, and nil when not asked, not
	// read or there is none yet.
	Benchmarks *BenchmarkReport
	// CausesMissing are the stories refused lately with no cause recorded.
	CausesMissing []StoryDetail
	// GristSmoke is the last grist smoke of each app, and nil when not asked or
	// none has been made.
	GristSmoke []GristSmokeRecord
	// NoVision are the [grist-apps] whose checkout has no VISION.md, by name.
	NoVision []string
	// Cancelled are the runs a cancel event ended in the last day, and Paused
	// the pause-host event this host is under, if any.
	Cancelled []Cancel
	Paused    *HostPause
	// HarnessHold is the hold on this host's new claims that a harness stop
	// left in its note and that has not run out; nil when there is none. An
	// expired Claude login is told apart, with the command that clears it
	// (mw-gq6.781).
	HarnessHold *HarnessHold
	// MillhandResume is the "resumed ... (grace until ...)" line shown under
	// the Millhand tick while its resume grace still holds; "" once it does
	// not.
	MillhandResume string
	// FuelToday is every token the Builder's ledger charged today, summed from
	// the lines the ledger dates today. It is the Builder stories' fuel, not the
	// factory's; the printed line says so.
	FuelToday int
	// RigMemory are the rigs whose memory is larger than RigMemoryBudget, in rig
	// order. It is empty, and the report has no section for it, when none is.
	RigMemory []RigMemorySize
	// RigMemoryBudget is the size a rig's memory was held to.
	RigMemoryBudget int
	// RigMemoryUnseen are the rigs kept as facts whose boot render is near full
	// and whose newest facts are search-only, in rig order.
	RigMemoryUnseen []RigMemorySize
	// RigEvals are the rigs whose eval.md does not pass, in rig order.
	RigEvals []RigEvalFailure
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

	// LastGC is when this host last asked its beads database to reclaim disk
	// space, and GCAge how long ago that was, so that a collection starved
	// for days shows on the BEADS line (mw-gq6.449). Both are zero when it
	// never has, and when LastGCSaid, the note as the tracker held it, cannot
	// be read as a time. A host whose beads live on another's does not collect
	// and leaves all three empty.
	LastGC     time.Time
	GCAge      time.Duration
	LastGCSaid string
}

// Run reads the report and prints it. Every call it makes is a read: the
// tracker's WorkInHand, StoryState and OpenSteps, ReadyWithLabel and
// BlockedForHost, four Notes per other host, the two tick logs' Read and the
// vault's ReadLedger and RigMemorySizes. WorkInHand is read once, and this
// host's running and ready stories and the other hosts' work are all narrowed
// from it. Nothing is claimed, nothing is poured, nothing is written.
func (s Status) Run(ctx context.Context) (StatusReport, error) {
	report := StatusReport{Host: s.Host, MainCommit: s.MainCommit, SyncMode: s.syncMode()}
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

	// A tracker that can read every bead at once is read through that one read
	// where it answers: on a Boost each bd call is two seconds, and the blocked
	// stories, the live epics and their children cost one apiece (431 calls,
	// 131 s, mw-gq6.492). The read is started now and waited for only by the
	// reads it answers, so that none of the rest waits on it; a snapshot that
	// cannot be taken leaves those reads to the tracker itself, slower.
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	if snapshotter, ok := s.Tracker.(TrackerSnapshot); ok {
		s.Tracker = snapshotRead{WorkTracker: s.Tracker, taken: later(func() (WorkTracker, error) { return snapshotter.Snapshot(ctx) })}
	}
	// Over a link with a long round trip every read of the tracker is seconds,
	// and the reads below do not depend on one another, so they are started
	// together and joined where the report needs each (mw-gq6.356). A tracker
	// that takes reads side by side makes them cost one wait and not a row. The
	// ones that need no word of what is in hand are started before it is read.
	var ideasRead func() (string, error)
	if s.Ideas != nil {
		ideasRead = later(func() (string, error) { return s.Ideas.Line(ctx), nil })
	}
	governorsRead := later(func() ([]StoryDetail, error) { return s.Tracker.ReadyWithLabel(ctx, LabelHitl) })
	othersWaitRead := later(func() ([]OtherWait, error) { return s.waitingOnOthers(ctx) })
	blockedRead := later(func() ([]StoryDetail, error) { return s.Tracker.BlockedForHost(ctx, s.Host) })
	shortfallsRead := later(func() ([]EpicShortfall, error) { return s.epicShortfalls(ctx) })
	// The reads that stand apart from the rest of the report, each seconds of
	// its own, are started with the others (mw-gq6.492).
	var causesRead func() ([]StoryDetail, error)
	if s.Causes != nil {
		causesRead = later(func() ([]StoryDetail, error) { return causesMissing(ctx, s.Causes, s.Tracker, s.now()) })
	}
	var iceboxRead func() (IceboxSummary, error)
	if s.Icebox != nil {
		iceboxRead = later(func() (IceboxSummary, error) { return s.Icebox.Icebox(ctx) })
	}
	suitesRead := later(func() ([]SuiteStatus, error) { return s.suiteStatuses(ctx), nil })
	var graphRead func() ([]domain.GraphBead, error)
	if s.Graph != nil {
		graphRead = later(func() ([]domain.GraphBead, error) { return s.Graph.BeadGraph(ctx) })
	}

	var smokeRead func() ([]GristSmokeRecord, error)
	if s.Smoke != nil {
		smokeRead = later(func() ([]GristSmokeRecord, error) { return s.Smoke.Records(ctx) })
	}
	var backupRead func() (string, error)
	if s.Notes != nil && report.SyncMode == BeadsSyncBackup {
		backupRead = later(func() (string, error) { return s.Notes.Note(ctx, LastBackupKey(s.Host)) })
	}

	var gcRead func() (string, error)
	if s.Notes != nil && report.SyncMode != BeadsSyncShared {
		gcRead = later(func() (string, error) { return s.Notes.Note(ctx, LastGCKey(s.Host)) })
	}

	work, err := s.Tracker.WorkInHand(ctx)
	if err != nil {
		return report, fmt.Errorf("reading what is in hand: %w", err)
	}
	waits := s.batchWaits(ctx, work)
	// A host that gives way keeps its claims and its sessions are silent: its own
	// stories are frozen, not stranded (mw-2891blf.3).
	s.hereGivesWay = s.giveWays().Of(ctx, s.Host, s.now())
	report.HereGivesWay = s.hereGivesWay
	if s.hereGivesWay.Stands {
		report.HereGivesWay.Stories = len(work.RunningOn(s.Host))
	}
	runningRead := map[string]func() (RunningStory, error){}
	// A story with a live lease runs whatever its status says (mw-gq6.878).
	runningHere := work.RunningOnLive(s.Host, s.now())
	for _, detail := range runningHere {
		if !detail.Hitl() {
			runningRead[detail.Story.ID] = later(func() (RunningStory, error) { return s.running(ctx, detail) })
		}
	}
	away := work.ClaimsAway(s.Host)
	awayRunsRead := later(func() (map[string]string, error) { return s.runStates(ctx, away), nil })
	elsewhereRead := later(func() ([]HostWork, error) {
		runs, _ := awayRunsRead()
		return s.elsewhere(ctx, work, runs)
	})
	var dampersRead func() (DampersReport, error)
	if s.Dampers != nil {
		dampersRead = later(func() (DampersReport, error) { return s.Dampers.Dampers(ctx, work, s.now()) })
	}

	closing := closingOut(ctx, s.CloseOuts)
	for _, detail := range runningHere {
		if detail.Hitl() {
			// The Mayor works it beside the Governor: there is no session of its
			// own to judge, only a story to be found waiting.
			report.Waiting = append(report.Waiting, detail)
			continue
		}
		rs, err := runningRead[detail.Story.ID]()
		if err != nil {
			return report, err
		}
		if mark, ok := closing[detail.Story.ID]; ok {
			rs.CloseOut, rs.Now = &mark, s.now()
		}
		if wait, ok := waits[detail.Story.ID]; ok && rs.Queued() {
			rs.Wait = &wait
		}
		report.Running = append(report.Running, rs)
		if !rs.Refused() && !rs.Queued() {
			// A refused story waits on the Mayor and a queued one for its batch:
			// neither holds a session, so the cap does not count it, as in mw
			// dispatch.
			report.InUse++
		}
	}
	report.Drained = s.Drained
	if s.Cap > 0 {
		report.Cap = s.Cap
		report.CapWhy = s.CapWhy
		report.NoRoom, _ = ReadRoom(ctx, s.Load, s.Room)
		if s.Grist != nil {
			if pulse, err := s.Grist.Pulse(ctx, s.Cap); err == nil {
				report.Grist = pulse
				report.NoRoom = strings.Join(nonEmpty(report.NoRoom, pulse.Slow), "; ")
			}
		}
	}

	var byKind []StoryDetail
	for _, detail := range work.ReadyOn(s.Host) {
		if QueuedForBatch(detail) {
			// Its work is done: LAND RUSH counts it, and dispatch does not claim it.
			continue
		}
		if detail.Hitl() {
			// Listed below with every other bead for the Governor, whichever host
			// its Path names. One marked only hitl:<kind> is not found by the
			// label hitl, so it is carried here.
			if !hasLabel(detail.Labels, LabelHitl) {
				byKind = append(byKind, detail)
			}
			continue
		}
		if detail.IsParent() {
			// A parent's children are the work: it is no ready story to list or
			// count, and a dispatch passes it over by the parent gate
			// (mw-gq6.1092).
			continue
		}
		report.Ready = append(report.Ready, detail)
	}
	awayRuns, _ := awayRunsRead()
	report.NoPath = s.noPath(report.Ready)
	report.Sharing = s.sharing(ctx, report.Running, report.Ready, away, work.Held, awayRuns)
	report.TesterHolds = s.testerHolds(work, report.Ready, s.now())
	report.HeldByFiles = s.heldByFiles(ctx, report.Ready)
	report.UnderHandsStep = s.underHandsStep(ctx, report.Ready)
	report.NoGristKey = s.noGristKey(report.Ready)
	report.NoHostTakes = s.noHostTakes(ctx, report.Ready)
	report.HeldByPin = s.heldByPin(ctx, report.Ready)

	// A bead for the Governor may have no Path at all, so no listing keyed by
	// host finds it: ask for the label itself. A hitl story ready on this host
	// comes back here too, and is listed once.
	governors, err := governorsRead()
	if err != nil {
		return report, fmt.Errorf("reading what waits for the Governor: %w", err)
	}
	report.Waiting = append(report.Waiting, governors...)
	for _, detail := range byKind {
		listed := false
		for _, have := range report.Waiting {
			listed = listed || have.Story.ID == detail.Story.ID
		}
		if !listed {
			report.Waiting = append(report.Waiting, detail)
		}
	}
	waiting := report.Waiting[:0:0]
	for _, detail := range report.Waiting {
		if detail.Todo() {
			report.HisToDo = append(report.HisToDo, detail)
		} else {
			waiting = append(waiting, detail)
		}
	}
	report.Waiting = waiting
	for _, detail := range report.Waiting {
		if kinds := detail.HitlKinds(); len(kinds) > 1 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s has %d hitl kinds: %s", detail.Story.ID, len(kinds), strings.Join(kinds, ", ")))
		}
	}
	sort.SliceStable(report.Waiting, func(i, j int) bool {
		return report.Waiting[i].Priority < report.Waiting[j].Priority
	})
	sort.SliceStable(report.HisToDo, func(i, j int) bool {
		return report.HisToDo[i].Priority < report.HisToDo[j].Priority
	})

	if report.WaitingOnOthers, err = othersWaitRead(); err != nil {
		return report, err
	}

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

	blocked, err := blockedRead()
	if err != nil {
		return report, fmt.Errorf("reading what is blocked on %s: %w", s.Host, err)
	}
	report.Blocked = blocked

	others, err := elsewhereRead()
	if err != nil {
		return report, err
	}
	for i := range others {
		others[i].Waits = waits
	}
	report.Others = others
	report.Ticks = ReadHostTicks(ctx, s.Ticks)
	report.RecheckSkips = ReadRecheckSkips(ctx, s.Ticks.RecheckSkips, s.now())
	report.AtLimit = WindowsAtLimit(ctx, s.Panes, s.now())
	report.MillhandResume = MillhandResumeLine(ctx, s.Ticks.Millhand, s.now())

	if report.EpicShortfalls, err = shortfallsRead(); err != nil {
		return report, err
	}

	var known []StoryDetail
	known = append(known, report.Ready...)
	known = append(known, report.Blocked...)
	for _, other := range report.Others {
		known = append(known, other.Stories...)
	}
	var held []StoryDetail
	if report.NoFilesKey, held, err = s.noFilesKey(ctx, known); err != nil {
		return report, err
	}
	inRigs := append([]StoryDetail(nil), known...)
	for _, running := range report.Running {
		inRigs = append(inRigs, running.Detail)
	}
	report.WrongRig = wrongRigStories(s.Rigs, s.RigFiles, append(inRigs, held...)...)

	report.NoSweep = bugsWithoutASweep(known)
	report.NoFormula = s.uninstalledFormulas(ctx, known)
	if report.HandedBack, err = s.handedBack(ctx, known, report.OpenButClaimed); err != nil {
		return report, err
	}
	report.UncheckedErrors, report.UncheckedErrorsUnread = s.uncheckedErrors(ctx)

	report.Suites, _ = suitesRead()
	report.LandRush = s.landRushStatuses(ctx, work)

	if graphRead != nil {
		graph, err := graphRead()
		if err != nil {
			return report, fmt.Errorf("reading which open beads look finished: %w", err)
		}
		report.Finished, report.FinishedKnown = domain.FinishedStillOpen(graph), true
		report.HeldFor = domain.HeldForLabels(graph)
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
	if s.Usage != nil {
		report.UsageLines = s.usageLines(ctx)
	}
	if s.Cloud != nil {
		if reading, err := ReadCloud(ctx, s.Cloud, s.CloudPlan, s.now()); err == nil {
			report.Cloud = reading
			report.BoxHarness = s.boxHarness(ctx, reading)
		} else {
			s.print(fmt.Sprintf("mw status: the cloud's book could not be read: %v\n", err))
		}
	}
	if s.Benchmarks != nil {
		if past, err := s.Benchmarks.Recent(ctx); err != nil {
			s.print(fmt.Sprintf("mw status: the benchmarks could not be read: %v\n", err))
		} else {
			// A box the book no longer holds has its rows left out; a book that
			// cannot be read leaves every host's (mw-gq6.1160).
			var gone func(string) bool
			if s.Cloud != nil {
				if book, err := s.Cloud.Read(ctx); err == nil {
					gone = DestroyedBox(s.CloudPlan, book)
				}
			}
			report.Benchmarks = ReadBenchmarks(past, s.Bench, gone)
		}
	}
	if causesRead != nil {
		if missing, err := causesRead(); err != nil {
			s.print(fmt.Sprintf("mw status: the refused stories without a cause could not be read: %v\n", err))
		} else {
			report.CausesMissing = missing
		}
	}
	if smokeRead != nil {
		if records, err := smokeRead(); err != nil {
			s.print(fmt.Sprintf("mw status: the grist smoke's record could not be read: %v\n", err))
		} else {
			report.GristSmoke = records
		}
	}
	report.NoVision = s.appsWithoutVision(ctx)
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

	if s.Notes != nil {
		if hold, held := readHarnessHold(ctx, s.Notes, s.Host); held && hold.Until.After(s.now()) {
			report.HarnessHold = &hold
		}
	}

	sizes, err := s.rigMemorySizes(ctx)
	if err != nil {
		return report, fmt.Errorf("sizing the %s seat's memory of its rigs: %w", s.Seat, err)
	}
	report.RigMemory = s.overBudget(sizes)
	report.RigEvals = s.rigEvalFailures(ctx, sizes)
	report.RigMemoryBudget = s.rigMemoryBudget()
	report.RigMemoryUnseen = s.nearFullSearchOnly(sizes)

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

		if backupRead != nil {
			said, err := backupRead()
			if err != nil {
				return report, fmt.Errorf("reading when %s last backed up its beads: %w", s.Host, err)
			}
			report.LastBackupSaid = strings.TrimSpace(said)
			if at, err := time.Parse(LastSyncFormat, report.LastBackupSaid); err == nil {
				report.LastBackup = at
				report.BackupAge = s.now().Sub(at)
			}
		}
		if gcRead != nil {
			said, err := gcRead()
			if err != nil {
				return report, fmt.Errorf("reading when %s last collected its beads: %w", s.Host, err)
			}
			report.LastGCSaid = strings.TrimSpace(said)
			if at, err := time.Parse(LastSyncFormat, report.LastGCSaid); err == nil {
				report.LastGC = at
				report.GCAge = s.now().Sub(at)
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

	if iceboxRead != nil {
		// A line the report can do without costs it only the line.
		if summary, err := iceboxRead(); err != nil {
			s.print("mw status: " + err.Error() + "\n")
		} else {
			report.Icebox = summary
		}
	}

	for _, detail := range work.OpenClaimed {
		if !QueuedForBatch(detail) && !slices.ContainsFunc(runningHere, func(r StoryDetail) bool { return r.Story.ID == detail.Story.ID }) {
			report.OpenButClaimed = append(report.OpenButClaimed, detail)
		}
	}
	report.NoSessionClaims = s.withoutGivingWay(ctx, work.NoSessionClaims(s.now()))
	report.NoSessionRuns = s.runStates(ctx, report.NoSessionClaims)

	if dampersRead != nil {
		// An alarm that cannot be judged costs the report only its section.
		if dampers, err := dampersRead(); err != nil {
			s.print("mw status: the dampers could not be read: " + err.Error() + "\n")
		} else {
			report.Dampers = dampers
		}
	}

	var pinned []string
	if report.Network != nil && report.Network.Metered {
		if alarm, found := MeteredPinned(work, s.Host, s.HeavyNet); found {
			report.Dampers.Alarms = append(report.Dampers.Alarms, alarm)
			pinned = alarm.Beads
		}
	}
	// A story the metered alarm already names, with its cure, is not named twice.
	for _, id := range slices.Sorted(maps.Keys(report.NoHostTakes)) {
		if slices.Contains(pinned, id) {
			continue
		}
		if alarm, found := report.NoHostTakes[id].Alarm(s.now()); found {
			report.Dampers.Alarms = append(report.Dampers.Alarms, alarm)
		}
	}

	for _, hold := range s.refusedHolds(ctx, work, report.Running, away, awayRuns) {
		if alarm, found := hold.Alarm(s.now()); found {
			report.Dampers.Alarms = append(report.Dampers.Alarms, alarm)
		}
	}

	if alarm, found := needsFilesKeyAlarm(report.Ready, report.NoFilesKey); found {
		report.Dampers.Alarms = append(report.Dampers.Alarms, alarm)
	}

	for _, other := range report.Others {
		report.Dampers.Alarms = append(report.Dampers.Alarms, other.DoctorAlarms()...)
	}

	if err := report.markGuests(ctx, s.Rules); err != nil {
		return report, err
	}
	if ideasRead != nil {
		report.IdeasOnly, _ = ideasRead()
	}

	s.print(report.String())
	return report, nil
}

// sharing is each ready story that shares a file with one running here, as mw
// dispatch finds it, by id; nil when none does. A branch whose changes cannot
// be read is said, and holds nothing back.
func (s Status) sharing(ctx context.Context, running []RunningStory, ready, away, held []StoryDetail, awayRuns map[string]string) map[string]FileShare {
	shares := fileShares{changes: s.Changes, rules: s.Rules, rigs: s.Rigs}
	refused := map[string]bool{}
	for _, rs := range running {
		shares.add(rs.Detail)
		refused[rs.Detail.Story.ID] = rs.Refused()
	}
	// What the other hosts hold keeps its files from the stories ready here, as
	// in mw dispatch (mw-gq6.941).
	for _, detail := range away {
		shares.addAway(detail, detail.ClaimedHost())
		refused[detail.Story.ID] = awayRuns[detail.Story.ID] == RunBlocked
	}
	// A held story whose kept branch is ahead of its target keeps its files as
	// well (mw-gq6.881).
	for _, detail := range held {
		if !detail.Iced() && !slices.ContainsFunc(shares.running, func(r StoryDetail) bool { return r.Story.ID == detail.Story.ID }) {
			shares.addKept(detail)
		}
	}
	var sharing map[string]FileShare
	for _, detail := range ready {
		if share, ok := shares.shared(ctx, detail); ok {
			share.Refused = refused[share.With]
			if sharing == nil {
				sharing = map[string]FileShare{}
			}
			sharing[detail.Story.ID] = share
		}
	}
	for _, note := range shares.notes {
		s.print("mw status: " + note + "\n")
	}
	return sharing
}

// runStates is the run state of each of the claimed stories, by id, read in one
// StoriesState where the tracker offers it and a story at a time where it does
// not or that read failed. A story whose state cannot be read is left out: it is
// not called refused on a guess, and the report is not failed for it.
func (s Status) runStates(ctx context.Context, claims []StoryDetail) map[string]string {
	if len(claims) == 0 {
		return nil
	}
	ids := make([]string, len(claims))
	for i, detail := range claims {
		ids[i] = detail.Story.ID
	}
	if batch, offered := s.Tracker.(StoriesState); offered {
		if states, err := batch.StoriesState(ctx, ids, RunState); err == nil {
			return states
		}
	}
	states := map[string]string{}
	for _, id := range ids {
		if run, err := s.Tracker.StoryState(ctx, id, RunState); err == nil {
			states[id] = run
		}
	}
	return states
}

// refusedHolds are the ready stories that share a file with a refused claim, on
// this host or another, as mw dispatch finds it, each with the claim, the
// file and when a dispatch first held it for its files (mw-gq6.941). A branch
// whose changes cannot be read is said, as in sharing, and holds nothing back.
func (s Status) refusedHolds(ctx context.Context, work WorkInHand, running []RunningStory, away []StoryDetail, awayRuns map[string]string) []RefusedHold {
	type claim struct {
		detail StoryDetail
		host   string
		here   bool
	}
	var refused []claim
	for _, rs := range running {
		if rs.Refused() {
			refused = append(refused, claim{rs.Detail, s.Host, true})
		}
	}
	for _, detail := range away {
		if awayRuns[detail.Story.ID] == RunBlocked {
			refused = append(refused, claim{detail, detail.ClaimedHost(), false})
		}
	}
	if len(refused) == 0 {
		return nil
	}
	since := s.filesHeldSince(ctx)
	var holds []RefusedHold
	for _, c := range refused {
		shares := fileShares{changes: s.Changes, rules: s.Rules, rigs: s.Rigs}
		if c.here {
			shares.add(c.detail)
		} else {
			shares.addAway(c.detail, c.host)
		}
		for _, ready := range work.Ready {
			if ready.IsEpic || ready.Hitl() {
				continue
			}
			share, ok := shares.shared(ctx, ready)
			if !ok || share.Unkeyed == NeedsFilesKey {
				continue
			}
			holds = append(holds, RefusedHold{Refused: c.detail, Host: c.host, Ready: ready, File: share.File, Since: since(ready)})
		}
		for _, note := range shares.notes {
			s.print("mw status: " + note + "\n")
		}
	}
	return holds
}

// filesHeldSince is, for a ready story, when a dispatch first held it for its
// files: the note of the host its Path names, or the earliest of any host's for
// a story any host may take; zero when no note names it or the notes cannot be
// read.
func (s Status) filesHeldSince(ctx context.Context) func(StoryDetail) time.Time {
	byHost := map[string]filesHeld{}
	if notes, ok := s.Notes.(DamperNotes); ok {
		if kept, err := notes.NotesWithPrefix(ctx, "host."); err == nil {
			book := readBoxBook(ctx, s.Cloud, s.CloudPlan)
			for key, value := range kept {
				host, found := strings.CutPrefix(key, "host.")
				if host, found = strings.CutSuffix(host, ".files_held"); found && host != "" && !book.older(host, lastSyncOf(kept[LastSyncKey(host)])) {
					byHost[host] = parseFilesHeld(value)
				}
			}
		}
	}
	return func(ready StoryDetail) time.Time {
		pinned := ready.Merged().Host
		var earliest time.Time
		for host, held := range byHost {
			if pinned != "" && pinned != domain.HostAuto && pinned != host {
				continue
			}
			if at, ok := held[ready.Story.ID]; ok && (earliest.IsZero() || at.Before(earliest)) {
				earliest = at
			}
		}
		return earliest
	}
}

// testerHolds is each ready Tester story whose rig runs testers_per_rig Tester
// stories already, on any host, by id; nil when none is.
func (s Status) testerHolds(work WorkInHand, ready []StoryDetail, now time.Time) map[string]TesterHold {
	var running []StoryDetail
	for _, detail := range work.RunningLive(now) {
		if !detail.Hitl() {
			running = append(running, detail)
		}
	}
	var holds map[string]TesterHold
	for _, detail := range ready {
		if hold, ok := testerHold(running, detail, s.TestersPerRig); ok {
			if holds == nil {
				holds = map[string]TesterHold{}
			}
			holds[detail.Story.ID] = hold
		}
	}
	return holds
}

// noGristKey is the line for each ready story this host's dispatch passes by at
// gate no-grist-key, by id; nil when this host holds the key, or is not asked.
func (s Status) noGristKey(ready []StoryDetail) map[string]string {
	if s.HasGristTestKey == nil || s.HasGristTestKey() {
		return nil
	}
	var lines map[string]string
	for _, detail := range ready {
		if detail.IsEpic || !needsGristTestKey(detail) {
			continue
		}
		if lines == nil {
			lines = map[string]string{}
		}
		lines[detail.Story.ID] = fmt.Sprintf("passed by %s: no grist test key (its checks run mw grist eval or smoke, or its files key names grinds/); a host that has one takes it", s.Host)
	}
	return lines
}

// noPath is the line for each ready story whose path is not whole, by id, in
// the words of mw dispatch's no-path gate: the story waits on a key, not on a
// turn, and a wait for a file another story holds would hide that.
func (s Status) noPath(ready []StoryDetail) map[string]string {
	var lines map[string]string
	for _, detail := range ready {
		if detail.IsEpic {
			continue
		}
		if _, err := detail.Path(); err != nil {
			if lines == nil {
				lines = map[string]string{}
			}
			lines[detail.Story.ID] = fmt.Sprintf("passed by %s: it has no path: %s; `mw path set %s <key>=<value>` mends it", s.Host, err, detail.Story.ID)
		}
	}
	return lines
}

// heldByFiles is the line for each ready story the host's dispatch has held for
// a shared file past FilesHeldTooLong, by id; nil when none has been, or there
// are no notes to read.
func (s Status) heldByFiles(ctx context.Context, ready []StoryDetail) map[string]string {
	if s.Notes == nil {
		return nil
	}
	since := readFilesHeld(ctx, s.Notes, s.Host)
	var lines map[string]string
	for _, detail := range ready {
		if line := heldByFiles(since[detail.Story.ID], s.now()); line != "" {
			if lines == nil {
				lines = map[string]string{}
			}
			lines[detail.Story.ID] = line
		}
	}
	return lines
}

// underHandsStep is the line for each ready story whose epic keeps a hands step
// that has not run clean and was not superseded or retired, by story id: the
// child is open to be dispatched while the epic still waits on his hands. Nil
// when none is, or the notes cannot be read by prefix.
func (s Status) underHandsStep(ctx context.Context, ready []StoryDetail) map[string]string {
	notes, ok := s.Notes.(DamperNotes)
	if !ok || len(ready) == 0 {
		return nil
	}
	read, err := notes.NotesWithPrefix(ctx, "hands.")
	if err != nil {
		return nil
	}
	live := map[string][]string{}
	var lines map[string]string
	for _, detail := range ready {
		epic := detail.EpicID
		if epic == "" {
			continue
		}
		ids, seen := live[epic]
		if !seen {
			for _, step := range viewHandsSteps(epic, read) {
				if waitingHandsStep([]PosternViewHandsStep{step}) {
					ids = append(ids, step.ID)
				}
			}
			live[epic] = ids
		}
		if len(ids) == 0 {
			continue
		}
		if lines == nil {
			lines = map[string]string{}
		}
		lines[detail.Story.ID] = fmt.Sprintf("epic %s has a hands step not yet run: %s", epic, strings.Join(ids, ", "))
	}
	return lines
}

// markGuests sets Guest on every story of the report whose rig is a guest repo.
func (r *StatusReport) markGuests(ctx context.Context, rules EpicRules) error {
	var all []*StoryDetail
	for i := range r.Running {
		all = append(all, &r.Running[i].Detail)
	}
	for _, list := range [][]StoryDetail{r.Ready, r.Waiting, r.HeldHands, r.OpenButClaimed, r.NoSessionClaims, r.Blocked} {
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

// snapshotRead is a tracker whose reads of every bead are answered by a
// snapshot of it taken beside the status's other reads, and waited for only here:
// the reads a snapshot answers (LiveEpics, ShowEpics, ShowBeads, BlockedForHost
// and StoriesComments) wait for it and every other read goes straight to the
// tracker. A snapshot that cannot be taken leaves all of them to the tracker.
type snapshotRead struct {
	WorkTracker
	taken func() (WorkTracker, error)
}

// reader is the snapshot when it was taken, else the tracker itself.
func (r snapshotRead) reader() WorkTracker {
	if snapshot, err := r.taken(); err == nil {
		return snapshot
	}
	return r.WorkTracker
}

func (r snapshotRead) LiveEpics(ctx context.Context) ([]string, error) {
	return r.reader().LiveEpics(ctx)
}

func (r snapshotRead) ShowEpics(ctx context.Context, ids []string) ([]EpicDetail, error) {
	return r.reader().ShowEpics(ctx, ids)
}

func (r snapshotRead) ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error) {
	return r.reader().ShowBeads(ctx, ids)
}

func (r snapshotRead) BlockedForHost(ctx context.Context, host string) ([]StoryDetail, error) {
	return r.reader().BlockedForHost(ctx, host)
}

func (r snapshotRead) StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error) {
	return r.reader().StoriesComments(ctx, ids)
}

// later starts f in a goroutine of its own and returns the function that waits
// for what it came back with. It may be called once or many times.
func later[T any](f func() (T, error)) func() (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan struct{})
	var got result
	go func() {
		defer close(done)
		got.value, got.err = f()
	}()
	return func() (T, error) {
		<-done
		return got.value, got.err
	}
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
	rs.UsageLimit = s.usageLimit(ctx, detail)
	rs.Turns = s.turns(ctx, rs)
	rs.TurnCap = s.turnCap(detail)
	if !rs.Refused() && !rs.Queued() && !rs.HandedBack() && !s.hereGivesWay.Stands {
		rs.Stranded = lapsedLease(detail, s.now())
	}
	if rs.NoSession = s.noSession(ctx, rs); !rs.NoSession.IsZero() {
		rs.Now = s.now()
	}
	return rs, nil
}

// giveWays is the reader of the hosts' give-way notes this status judges by.
func (s Status) giveWays() GiveWayReader {
	return GiveWayReader{Notes: s.Notes, Stale: s.GiveWayStale}
}

// withoutGivingWay drops the claims held on a host whose give-way note stands:
// its sessions are frozen, and the lease it renews through the heartbeat is no
// sign that one is missing.
func (s Status) withoutGivingWay(ctx context.Context, claims []StoryDetail) []StoryDetail {
	if len(claims) == 0 {
		return claims
	}
	stands := map[string]bool{}
	var kept []StoryDetail
	for _, d := range claims {
		host := claimedOn(d)
		giving, read := stands[host]
		if !read {
			if host == s.Host {
				giving = s.hereGivesWay.Stands
			} else {
				giving = s.giveWays().Stands(ctx, host, s.now())
			}
			stands[host] = giving
		}
		if !giving {
			kept = append(kept, d)
		}
	}
	return kept
}

// SessionStatuser is the part of a Runner that says whether a session is alive.
type SessionStatuser interface {
	Status(ctx context.Context, name string) (SessionStatus, error)
}

// NoSessionNamedAfter is how long a story may be in progress with no live
// session before mw status names it (mw-gq6.797). No rig keeps a timeout of its
// own, so it is the age at which mw stuck counts a claim with no session a root.
const NoSessionNamedAfter = StuckClaimAge

// noSession is when the claim began, when the story has been in progress longer
// than NoSessionNamedAfter and its session is not running: a dead pane, or no
// window. A queued or handed-back story has no session by design, and a runner
// that cannot say names nothing. Zero otherwise.
func (s Status) noSession(ctx context.Context, rs RunningStory) time.Time {
	detail := rs.Detail
	if s.Sessions == nil || rs.Queued() || rs.HandedBack() || rs.CloseOut != nil || s.hereGivesWay.Stands ||
		!strings.EqualFold(strings.TrimSpace(detail.Status), StatusInProgress) {
		return time.Time{}
	}
	since := detail.ClaimStarted()
	if since.IsZero() || s.now().Sub(since) < NoSessionNamedAfter {
		return time.Time{}
	}
	status, err := s.Sessions.Status(ctx, rs.Session)
	if err != nil || status.Running() {
		return time.Time{}
	}
	return since
}

// lapsedLease is when a claimed story's lease ran out, when it had by now with
// no heartbeat since, and the zero time otherwise: a live session renews its
// lease, so a lapsed one is a claim nothing is working (StaleClaims's own
// definition, mw-gq6.576).
func lapsedLease(detail StoryDetail, now time.Time) time.Time {
	if strings.EqualFold(strings.TrimSpace(detail.Status), StatusInProgress) && !detail.LeaseExpires.IsZero() && now.After(detail.LeaseExpires) {
		return detail.LeaseExpires
	}
	return time.Time{}
}

// turnsNamedPast is the turns past which the report names the session: the turn
// it is told to wrap up at, of its story's cap, else TurnsNamedPast.
func (r RunningStory) turnsNamedPast() int {
	if r.TurnCap > 0 {
		return TurnCapWrapUp(r.TurnCap)
	}
	return TurnsNamedPast
}

// TurnsNamedPast is how many turns a running session may take before mw status
// names it (mw-1ljpo9.6) when its story has no cap to name it by: with a cap it
// is named past the turn it is told to wrap up at (TurnCapWrapUp).
const TurnsNamedPast = 100

// turnCap is the cap of the session working detail on this host: the story's own
// max_turns, else its size's, else the host's next.max_turns (mw-gq6.852).
func (s Status) turnCap(detail StoryDetail) int {
	path := detail.Merged()
	if s.MaxTurnsSaid {
		return path.TurnCapUnder(s.MaxTurns)
	}
	hostCap := s.MaxTurns
	if hostCap == 0 {
		hostCap = DefaultMaxTurns
	}
	return path.TurnCap(hostCap)
}

// turns is how many turns the story's session has taken so far, read from its
// worktree on this host, and 0 when there is no reader, no checkout of its rig
// here or no session that could be running: a stopped, refused, queued or
// handed-back story has none. A turn count that cannot be read is no reason to
// fail the report.
func (s Status) turns(ctx context.Context, rs RunningStory) int {
	rigDir := s.Rigs[rs.Detail.Merged().Rig]
	if s.Turns == nil || rigDir == "" || rs.Stopped() || rs.Refused() || rs.Queued() || rs.HandedBack() {
		return 0
	}
	turns, err := s.Turns.Turns(ctx, WorktreeDir(rigDir, rs.Detail.Story.ID))
	if err != nil {
		return 0
	}
	return turns
}

// usageLimit is the harness's words when the result the story's session left
// for its attempt says the usage limit stopped it, and empty when it does not,
// when there is no result yet, or when it cannot be read: a session still
// running has none, and nothing here is a reason to fail the report.
func (s Status) usageLimit(ctx context.Context, detail StoryDetail) string {
	if s.Vault == nil {
		return ""
	}
	attempt := detail.Attempts
	if attempt < 1 {
		attempt = 1
	}
	if stop, ok := harnessStopOfRun(ctx, s.Vault, detail, attempt, s.now()); ok && stop.Reason == ReasonUsageLimit {
		return stop.Said
	}
	return ""
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
func (s Status) elsewhere(ctx context.Context, inHand WorkInHand, runs map[string]string) ([]HostWork, error) {
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
	// A retired host is listed whether or not it holds work, and none of its
	// notes are read: it is gone, not asleep.
	for _, host := range s.Retired {
		if _, seen := byHost[host]; !seen && host != s.Host && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	sort.Strings(hosts)

	// Each host's three notes are a read apiece, and no note depends on
	// another: all of them are started before the first is waited for.
	now := s.now()
	type noted struct {
		synced, halted, ticks, mw, doctor, passed, givesWay func() (string, error)
	}
	notes := make([]noted, len(hosts))
	for i, host := range hosts {
		if IsRetired(s.Retired, host) {
			continue
		}
		read := func(key string) func() (string, error) {
			return later(func() (string, error) { return s.Notes.Note(ctx, key) })
		}
		notes[i] = noted{read(LastSyncKey(host)), read(SyncHaltKey(host)), read(TicksKey(host)), read(MwCommitKey(host)), read(DoctorVerdictKey(host)), read(HostPassedKey(host)), read(GiveWayKey(host))}
	}

	// A box made again under a destroyed box's name finds that box's last sync
	// note in the vault: a sync from before the new box was made is the old
	// box's, and is read as none (mw-gq6.1145). A book that cannot be read
	// leaves every sync as it was.
	book := readBoxBook(ctx, s.Cloud, s.CloudPlan)

	work := make([]HostWork, 0, len(hosts))
	for i, host := range hosts {
		if IsRetired(s.Retired, host) {
			work = append(work, HostWork{Host: host, Retired: true, Stories: byHost[host], Now: now})
			continue
		}
		said, err := notes[i].synced()
		if err != nil {
			return nil, fmt.Errorf("reading when %s last synced: %w", host, err)
		}

		held := HostWork{Host: host, Said: strings.TrimSpace(said), Stories: byHost[host], Now: now}
		for _, d := range held.Stories {
			if run, read := runs[d.Story.ID]; read && run != "" {
				if held.Runs == nil {
					held.Runs = map[string]string{}
				}
				held.Runs[d.Story.ID] = run
			}
		}
		// Every other note of the host is read against the same sync: the sync
		// writes the ticks and the mw commit beside it, and the notes the
		// host's timers leave come after one.
		syncAt := lastSyncOf(held.Said)
		older := book.older(host, syncAt)
		if held.Said != "" {
			if at, err := time.Parse(LastSyncFormat, held.Said); err == nil {
				if older {
					held.Said = ""
				} else {
					held.LastSync = at
					held.Silent = now.Sub(at)
				}
			}
		}
		// A host is asleep on its sync age only when nothing else of it is newer
		// than the threshold either (mw-gq6.523): a Boost syncs on its dispatch
		// ticks, and may land and push between them.
		if sign := NewestSign(ctx, s.Signs, host, byHost[host]); sign.At.After(held.LastSync) {
			held.Sign, held.SignAge = sign, max(now.Sub(sign.At), 0)
		}
		held.Asleep = held.LastSync.IsZero() ||
			held.Silent > s.hostSilence() && (held.Sign.At.IsZero() || held.SignAge > s.hostSilence())

		haltSaid, err := notes[i].halted()
		if err != nil {
			return nil, fmt.Errorf("reading whether %s's sync is halted: %w", host, err)
		}
		if info, ok := ParseSyncHalt(haltSaid); ok && !older {
			held.Halt = &info
		}

		ticks, err := notes[i].ticks()
		if err != nil {
			return nil, fmt.Errorf("reading how the timers of %s are doing: %w", host, err)
		}
		if !older {
			held.Ticks = ParseHostTicks(ticks)
		}

		commit, err := notes[i].mw()
		if err != nil {
			return nil, fmt.Errorf("reading which mw %s runs: %w", host, err)
		}
		if !older {
			held.MwCommit = strings.TrimSpace(commit)
		}
		held.BehindMain = behindMain(held.MwCommit, s.MainCommit)

		doctor, err := notes[i].doctor()
		if err != nil {
			return nil, fmt.Errorf("reading what the doctor of %s found: %w", host, err)
		}
		if older {
			doctor = ""
		}
		if held.DoctorSaid = strings.TrimSpace(doctor); held.DoctorSaid != "" {
			var ok bool
			if held.Doctor, ok = ParseDoctorVerdict(held.DoctorSaid); !ok {
				held.DoctorUnreadable = true
			}
		}
		passed, err := notes[i].passed()
		if err != nil {
			return nil, fmt.Errorf("reading what %s passed for a reason of its own: %w", host, err)
		}
		held.Passed = map[string]HostPassed{}
		for _, p := range ParseHostPassed(passed) {
			if hostPassedGates[p.Gate] && !older {
				held.Passed[p.Story] = p
			}
		}
		// A host that gives way keeps its claims and its heartbeats while its
		// sessions are silent: its stories are frozen, not stranded, and the host
		// is not asleep. A note that cannot be read is no note (mw-2891blf.3).
		if said, err := notes[i].givesWay(); err == nil && !older {
			held.GivesWay = readGiveWay(said, now, s.GiveWayStale)
			if held.GivesWay.Stands {
				held.Asleep = false
				for _, d := range held.Stories {
					if strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress) {
						held.GivesWay.Stories++
					}
				}
			}
		}
		work = append(work, held)
	}
	for i := range work {
		work[i].Pins = s.pinsOf(ctx, work[i])
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
// usageLines are the USAGE line: the newest quota reading and when the week
// resets, and under it STALE with the reason when the reading is stale. A
// readings note that cannot be read is said on the line, not left out.
func (s Status) usageLines(ctx context.Context) []string {
	all, err := s.Usage.All(ctx)
	switch {
	case err != nil:
		return []string{"USAGE unreadable: " + err.Error()}
	case len(all) == 0:
		return []string{"USAGE no reading yet: mw usage read"}
	}
	last := all[len(all)-1]
	lines := []string{fmt.Sprintf("USAGE %.1f%% read %s · resets %s", last.Percent, last.At.UTC().Format("01-02 15:04Z"), last.Resets.UTC().Format("01-02 15:04Z"))}
	if why := last.Stale(s.now()); why != "" {
		lines = append(lines, "  STALE: "+why)
	}
	return lines
}

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

// rigMemorySizes is how large each rig's memory is, in rig order. A report
// with no vault to read them from has none.
func (s Status) rigMemorySizes(ctx context.Context) ([]RigMemorySize, error) {
	if s.Vault == nil {
		return nil, nil
	}
	return s.Vault.RigMemorySizes(ctx, s.Seat)
}

// overBudget is the rigs whose memory is larger than the budget.
func (s Status) overBudget(sizes []RigMemorySize) []RigMemorySize {
	var over []RigMemorySize
	for _, size := range sizes {
		if size.Bytes > s.rigMemoryBudget() {
			over = append(over, size)
		}
	}
	return over
}

// nearFullSearchOnly is the rigs kept as facts whose render is within
// NearFullPercent of the budget (or past it) while their newest facts are
// search-only: a Builder never read those facts at boot, and nothing else says so.
func (s Status) nearFullSearchOnly(sizes []RigMemorySize) []RigMemorySize {
	var unseen []RigMemorySize
	for _, size := range sizes {
		if size.Facts && size.SearchOnlyNewest > 0 && size.Bytes*100 >= s.rigMemoryBudget()*NearFullPercent {
			unseen = append(unseen, size)
		}
	}
	return unseen
}

// rigEvalFailures are the rigs kept as facts whose eval.md does not pass, in
// rig order. A rig with no eval.md, and a report with no RigFacts to run one
// against, have none. An eval.md that cannot be read is a failure with its
// problem.
func (s Status) rigEvalFailures(ctx context.Context, sizes []RigMemorySize) []RigEvalFailure {
	if s.RigFacts == nil {
		return nil
	}
	memory := Memory{Files: s.RigFacts, Seat: s.Seat}
	var failing []RigEvalFailure
	for _, size := range sizes {
		if !size.Facts {
			continue
		}
		report, found, err := memory.runEval(ctx, size.Rig)
		switch {
		case err != nil && !found:
			failing = append(failing, RigEvalFailure{Rig: size.Rig, Problem: "cannot run eval.md: " + err.Error()})
		case err != nil:
			failing = append(failing, RigEvalFailure{Rig: size.Rig, Problem: err.Error()})
		case found && report.Failed > 0:
			failing = append(failing, RigEvalFailure{Rig: size.Rig, Failed: report.Failed, Total: report.Total})
		}
	}
	return failing
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

// idleSince is when the factory fell idle, or the zero time when it has not:
// the time of the last event no job wrote, when every event since is a job's,
// the last non-job event is IdleAfter old, and no job has begun a pass it has not
// ended. It reads the newest events the log keeps of each job (NewestOf), however
// far back they lie. A log it cannot read says nothing.
func (s Status) idleSince(ctx context.Context) time.Time {
	after := s.IdleAfter
	if after <= 0 {
		return time.Time{}
	}
	newest, err := NewestOf(ctx, s.Log)
	if err != nil || newest.Seq == 0 {
		return time.Time{}
	}
	since := newest.Other.Ts
	if newest.Other.Seq == 0 {
		since = newest.First.Ts
	}
	for _, j := range newest.Jobs {
		if j.Last.To == events.JobScheduled || j.Last.To == events.JobRunning {
			return time.Time{}
		}
	}
	if s.now().Sub(since) < after {
		return time.Time{}
	}
	return since.In(s.now().Location())
}

// appsWithoutVision names, in order, each app of [grist-apps] whose checkout
// holds no VISION.md. A checkout whose file cannot be read is not named: the
// mail to the Mayor says that, and status only says what is plainly missing.
func (s Status) appsWithoutVision(ctx context.Context) []string {
	if s.Visions == nil {
		return nil
	}
	var missing []string
	for app, dir := range s.Apps {
		if _, found, err := s.Visions.Read(ctx, dir); err == nil && !found {
			missing = append(missing, app)
		}
	}
	sort.Strings(missing)
	return missing
}

// waitingOnOthers lists the open beads labelled waiting:others, longest
// waiting first, each with who it waits on and since when its note says. It
// reads and writes nothing else.
func (s Status) waitingOnOthers(ctx context.Context) ([]OtherWait, error) {
	beads, err := s.Tracker.ReadyWithLabel(ctx, LabelWaitingOthers)
	if err != nil {
		return nil, fmt.Errorf("reading what waits on others: %w", err)
	}
	// One note a bead, all asked for before the first is waited on.
	noted := make([]func() (string, error), len(beads))
	for i, d := range beads {
		if s.Notes != nil {
			noted[i] = later(func() (string, error) { return s.Notes.Note(ctx, AskWaitingKey(d.Story.ID)) })
		}
	}
	var waits []OtherWait
	for i, d := range beads {
		note := ""
		if noted[i] != nil {
			if note, err = noted[i](); err != nil {
				return nil, fmt.Errorf("reading when %s began waiting: %w", d.Story.ID, err)
			}
		}
		waits = append(waits, OtherWait{Story: d, Of: d.AskedOf(), Since: askWaitingSince(note, d)})
	}
	sort.SliceStable(waits, func(i, j int) bool {
		a, b := waits[i].Since, waits[j].Since
		if a.IsZero() != b.IsZero() {
			return !a.IsZero()
		}
		return a.Before(b)
	})
	return waits, nil
}

// bugsWithoutASweep picks, from the open stories this status has read, those
// mw dispatch refuses for want of a Sweep: line, once each.
func bugsWithoutASweep(known []StoryDetail) []StoryDetail {
	var found []StoryDetail
	seen := map[string]bool{}
	for _, d := range known {
		if d.IsEpic || d.Status == StatusClosed || seen[d.Story.ID] {
			continue
		}
		seen[d.Story.ID] = true
		if domain.OwesASweep(d.Type, d.Story.Title, d.Labels, d.Created) && !domain.SweepOfStory(d.Acceptance, d.Description).Passes() {
			found = append(found, d)
		}
	}
	return found
}

// uninstalledFormulas picks, from the open stories this status has read, those
// whose formula the tracker has not installed, once each. A tracker that cannot
// say what is installed names none: that is not a story's fault.
func (s Status) uninstalledFormulas(ctx context.Context, known []StoryDetail) []StoryDetail {
	var found []StoryDetail
	var installed map[string]bool
	seen := map[string]bool{}
	for _, d := range known {
		formula := d.Merged().Formula
		if d.IsEpic || d.Status == StatusClosed || formula == "" || seen[d.Story.ID] {
			continue
		}
		seen[d.Story.ID] = true
		if installed == nil {
			names, err := s.Tracker.Formulas(ctx)
			if err != nil {
				return nil
			}
			installed = make(map[string]bool, len(names))
			for _, name := range names {
				installed[name] = true
			}
		}
		if !installed[formula] {
			found = append(found, d)
		}
	}
	return found
}

// handedBack lists the open stories among the ones the report knows whose
// newest word is a Builder's hand-back (mw-gq6.418), in the order they were
// listed. The comments are read together, and only for stories that carry any.
func (s Status) handedBack(ctx context.Context, known, claimed []StoryDetail) ([]HandedBackStory, error) {
	var candidates []StoryDetail
	var ids []string
	seen := map[string]bool{}
	for _, list := range [][]StoryDetail{known, claimed} {
		for _, d := range list {
			if d.IsEpic || d.Status == StatusClosed || d.CommentCount == 0 || seen[d.Story.ID] {
				continue
			}
			seen[d.Story.ID] = true
			candidates = append(candidates, d)
			ids = append(ids, d.Story.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	said, err := s.Tracker.StoriesComments(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("reading which stories were handed back: %w", err)
	}
	var found []HandedBackStory
	for _, d := range candidates {
		if at, handedBack := HandedBack(said[d.Story.ID]); handedBack {
			found = append(found, HandedBackStory{Detail: d, At: at})
		}
	}
	return found, nil
}

// uncheckedErrors are the classes of the common-errors register not yet
// checked in every rig of their kinds. A register that cannot be read is said,
// and fails nothing else of the status; a class file that is not whole is left
// out, as mw errors list names it.
func (s Status) uncheckedErrors(ctx context.Context) ([]UncheckedClass, string) {
	if s.Errors == nil {
		return nil, ""
	}
	files, kindsText, err := s.Errors.ReadErrorRegister(ctx, s.Seat)
	if err != nil {
		return nil, err.Error()
	}
	kinds, err := ParseRigKinds(kindsText)
	if err != nil {
		return nil, ErrorKindsFile + ": " + err.Error()
	}
	classes, _ := LoadErrorClasses(files)
	return UncheckedClasses(classes, kinds), ""
}

// HarnessCrashDay is how far back a box's panes are read for the harness's
// crashes, and BoxHarnessCrashesNamed how many of them in that time name the
// box: one crash is the harness's bad luck, two on a box say the box is
// suspect (mw-gq6.1139).
const (
	HarnessCrashDay          = 24 * time.Hour
	BoxHarnessCrashesNamed   = 2
	harnessCrashBannerPrefix = "Bun v"
)

// BoxPane is one session pane a box kept: Window names it (the story it ran),
// At is when it was last active and Text what it showed.
type BoxPane struct {
	Window string
	At     time.Time
	Text   string
}

// BoxPanes is what mw status asks of a cloud box to count its harness crashes:
// the session panes it kept at or after since. The adapter reaches the box over
// ssh; an error is a box that could not be read, not a box with none.
type BoxPanes interface {
	Panes(ctx context.Context, box string, since time.Time) ([]BoxPane, error)
}

// isHarnessCrash reports whether a pane shows the harness crashing: the Bun
// runtime's banner ("Bun v1.4.4 (...) Linux x64") together with its crash
// words. A segfault alone is not it (any program can), nor is a pane that only
// mentions a Bun version.
func isHarnessCrash(text string) bool {
	if !strings.Contains(text, harnessCrashBannerPrefix) {
		return false
	}
	low := strings.ToLower(text)
	return strings.Contains(low, "segmentation fault") || strings.Contains(low, "panic(") || strings.Contains(low, "has crashed")
}

// BoxHarness is one cloud box's line of HARNESS: the Claude Code version the
// cloud's book says it was brought level to ("" when it never was), the panes
// of the harness's crashes in the last day, and why its panes could not be
// read when they could not.
type BoxHarness struct {
	Box      string
	Version  string
	Crashes  []BoxPane
	ReadFail string
}

// boxHarness reads HARNESS for the boxes of the cloud's reading. A box still
// being made or restored runs no session and is not reached.
func (s Status) boxHarness(ctx context.Context, reading *CloudReading) []BoxHarness {
	var out []BoxHarness
	since := reading.Now.Add(-HarnessCrashDay)
	for _, box := range reading.Boxes {
		if box.Making || box.Restoring {
			continue
		}
		line := BoxHarness{Box: box.Name, Version: box.Harness}
		if s.BoxPanes != nil {
			panes, err := s.BoxPanes.Panes(ctx, box.Name, since)
			if err != nil {
				line.ReadFail = oneLine(err.Error())
			}
			for _, pane := range panes {
				if !pane.At.Before(since) && isHarnessCrash(pane.Text) {
					line.Crashes = append(line.Crashes, pane)
				}
			}
		}
		out = append(out, line)
	}
	return out
}

package application

import (
	"context"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The statuses a story is in, in the tracker's own words. A story is held back
// from every dispatcher (StatusHeld), released to them (StatusOpen), claimed by
// a session (StatusInProgress), or finished (StatusClosed). "Held" is what the
// factory calls what beads stores as "deferred": the tracker's word for work
// that is filed but not yet approved.
const (
	StatusOpen       = "open"
	StatusHeld       = "deferred"
	StatusInProgress = "in_progress"
	StatusClosed     = "closed"
)

// LabelHitl is the label on a story that is worked with the Governor present, a
// human in the loop: the Mayor claims it and does it beside them, so no
// dispatcher ever starts a session for it. It says who a story is for, which a
// Path's host cannot: hosts are always named, never "any" or "the Governor".
const LabelHitl = "hitl"

// DefaultPriority is the priority a story has when nobody set one, and what a
// tracker that reports none is taken to mean. Priorities run from 0, the most
// urgent, to 4, as beads has them.
const DefaultPriority = 2

// StoryDetail is everything the work tracker knows about one story: the story
// itself with the Path overrides it carries, and the default Path of the epic
// it belongs to. The Path the story is actually worked by is the two overlaid,
// which is what Path reports.
type StoryDetail struct {
	Story    domain.Story
	Defaults domain.Path
	EpicID   string
	Status   string
	Assignee string
	// Labels are the tags the story carries in the tracker, as the tracker spells
	// them; empty when it has none or the tracker was not asked.
	Labels      []string
	Description string
	Acceptance  string
	// Needs is the ids of the stories this one waits on, as far as the listing
	// it came from said. It is empty when the tracker was not asked for the
	// story's dependencies. A listing that knows which of them are finished —
	// BlockedForHost does — leaves the finished ones out: a story shown as
	// waiting on work that is done reads as blocked when it is not.
	Needs []string
	// EstimateMinutes is the Mayor's estimate in minutes; zero when unset.
	EstimateMinutes int
	// Priority is how urgent the story is, 0 (most urgent) to 4. It is what a
	// dispatch starts stories by, so a tracker that does not say leaves it at
	// DefaultPriority rather than at zero, which would be the most urgent of all.
	Priority int
	// Created is when the story was filed; zero when the tracker did not say.
	Created time.Time
	// Started is when the story was claimed, as the tracker recorded it; zero
	// when it was never claimed or the tracker did not say.
	Started time.Time
	// IsEpic is true when this is not a story but an epic filed under an epic:
	// a listing of an epic's children returns it among them, and it is not work
	// a session takes. A tracker that does not say leaves it false.
	IsEpic bool
	// Attempts is how many times a session was started for this story, as its
	// AttemptsField metadata says; zero when it never was. Exhausted says mw
	// dispatch has already told the Mayor the story used them all up.
	Attempts  int
	Exhausted bool
	// Molecule is the formula poured for this story, empty until it has been
	// poured. It is filled in by whoever pours it, not by reading the story.
	Molecule Molecule
	// LeaseExpires is when the tracker's own lease on this story's claim runs
	// out unless a heartbeat renews it first; zero when the story is not
	// claimed or the tracker does not say. A claim found past this with its
	// session's tmux pane dead is a dispatch's strongest sign that the session
	// ended without mw next ever hearing about it (mw-gq6.106).
	LeaseExpires time.Time
}

// WorkInHand is what the tracker has in hand across every host: the stories
// ready to be taken and the stories already claimed, before either is narrowed
// to a host. The tracker reads it once; each section of a report narrows it
// with ReadyOn, RunningOn or Elsewhere.
type WorkInHand struct {
	// Ready are the stories open, unclaimed and unblocked, on any host or none.
	Ready []StoryDetail
	// Running are the stories claimed and not yet finished, on any host or none.
	Running []StoryDetail
}

// ReadyOn is the ready stories whose Path names this host. A story whose Path
// names no host is not offered to any host.
func (w WorkInHand) ReadyOn(host string) []StoryDetail { return pathedTo(w.Ready, host) }

// RunningOn is the claimed stories whose Path names this host.
func (w WorkInHand) RunningOn(host string) []StoryDetail { return pathedTo(w.Running, host) }

// Elsewhere is every story, ready and then claimed, whose Path names some host
// other than this one. A story whose Path names no host is nobody's.
func (w WorkInHand) Elsewhere(host string) []StoryDetail {
	var away []StoryDetail
	for _, list := range [][]StoryDetail{w.Ready, w.Running} {
		for _, detail := range list {
			if on := detail.Merged().Host; on != "" && on != host {
				away = append(away, detail)
			}
		}
	}
	return away
}

// pathedTo narrows stories to the ones whose Path names the host.
func pathedTo(stories []StoryDetail, host string) []StoryDetail {
	var on []StoryDetail
	for _, detail := range stories {
		if merged := detail.Merged().Host; merged != "" && merged == host {
			on = append(on, detail)
		}
	}
	return on
}

// Molecule is a story's formula poured into beads: the root bead the steps hang
// from, and the steps in the order they are worked. The zero Molecule is a
// formula that has not been poured.
type Molecule struct {
	// Formula is the formula that was poured.
	Formula string
	// RootID is the bead the step beads are children of.
	RootID string
	Steps  []FormulaStep
}

// Poured reports whether this molecule holds steps a session can work.
func (m Molecule) Poured() bool { return m.RootID != "" && len(m.Steps) > 0 }

// FormulaStep is one step of a poured formula, as the bead that holds it: the
// id the session closes when the step is done, and what the step says.
type FormulaStep struct {
	ID          string
	Title       string
	Description string
}

// Comment is one comment left on a story: who left it, when, and what it says,
// as written.
type Comment struct {
	Author  string
	Created time.Time
	Text    string
}

// Held reports whether the tracker is holding this story back from every
// dispatcher: filed, complete, and waiting on somebody to approve it.
func (d StoryDetail) Held() bool {
	return strings.EqualFold(strings.TrimSpace(d.Status), StatusHeld)
}

// Closed reports whether this story is finished.
func (d StoryDetail) Closed() bool {
	return strings.EqualFold(strings.TrimSpace(d.Status), StatusClosed)
}

// Hitl reports whether this story is worked with the Governor present, which
// no dispatcher may take and which is not a session for a host's cap to count.
func (d StoryDetail) Hitl() bool {
	for _, label := range d.Labels {
		if strings.EqualFold(strings.TrimSpace(label), LabelHitl) {
			return true
		}
	}
	return false
}

// Merged is the epic's defaults overlaid with the story's own overrides,
// whether or not what comes out is a complete Path. Use it to read one field
// of a story that may not be fully planned yet; use Path before working it.
func (d StoryDetail) Merged() domain.Path {
	return d.Defaults.Overlay(d.Story.Overrides)
}

// Path is the Path this story is worked by, or the reason it has none.
func (d StoryDetail) Path() (domain.Path, error) {
	return d.Story.PathFrom(d.Defaults)
}

// EpicDetail is an epic as the tracker holds it now: what it is called, the
// default Path its stories inherit, and every story filed under it — whatever
// state each is in, closed ones included, because an epic's tree that leaves
// out the work already done is not this epic's tree.
type EpicDetail struct {
	ID    string
	Title string
	// Status is what the tracker says the epic is, in its own words; empty when
	// the tracker did not say.
	Status string
	// Priority is how urgent the epic is, 0 (most urgent) to 4, DefaultPriority
	// when the tracker did not say.
	Priority int
	Defaults domain.Path
	// Stories are in the order they were filed, each with the epic's defaults
	// overlaid and with Needs filled in. They are every direct child of the
	// epic: one that is itself an epic is among them with IsEpic set, and its
	// own children are not read.
	Stories []StoryDetail
}

// NewEpic is an epic about to be filed: what it delivers, how it will be known
// to be delivered, and the default Path its stories inherit. A Priority of zero
// leaves the tracker's own default standing.
type NewEpic struct {
	Title           string
	Description     string
	SuccessCriteria string
	Priority        int
	Defaults        domain.Path
}

// NewStory is a story about to be filed under an epic. Overrides are the fields
// of the epic's default Path this story departs from, and are stored as the
// story's metadata; Needs are the ids of the stories that must be finished
// before it can be worked. A Priority or an EstimateMinutes of zero leaves the
// tracker's own default standing.
type NewStory struct {
	EpicID          string
	Title           string
	Description     string
	Acceptance      string
	Priority        int
	EstimateMinutes int
	Overrides       domain.Path
	Needs           []string
}

// WorkTracker is the port the factory reads and writes stories through. One
// adapter talks to beads; an in-memory one stands in for it in tests.
//
// Implementations serialise their own calls: the beads database takes a
// single-writer lock, so no two of these may be in flight at once.
type WorkTracker interface {
	// CreateEpic files an epic and reports the id the tracker gave it.
	CreateEpic(ctx context.Context, epic NewEpic) (string, error)

	// CreateStory files one story under an epic and reports the id the tracker
	// gave it. It is always filed held: a story nobody has approved must never
	// be dispatchable, not even for the moment between filing and reading the
	// plan back. ReleaseStory is the only way out of that.
	//
	// Every story it waits on must be filed already, so that Needs can name
	// them by id — file a plan in the order Plan.Order gives.
	CreateStory(ctx context.Context, story NewStory) (string, error)

	// ReleaseStory releases a held story: it may be worked as soon as whatever
	// it waits on is done. Releasing a story that is not held changes nothing.
	ReleaseStory(ctx context.Context, id string) error

	// ShowStory reports one story with its epic's default Path overlaid.
	ShowStory(ctx context.Context, id string) (StoryDetail, error)

	// ShowEpic reports an epic and every story filed under it, in the order
	// they were filed, each with the epic's defaults overlaid and with what it
	// waits on. An id that names no epic is an error, and reading is all it
	// does: nothing is written.
	ShowEpic(ctx context.Context, id string) (EpicDetail, error)

	// ReadyStories lists the stories of an epic that can be started on a host
	// right now: open, unclaimed, unblocked, and whose Path names that host.
	ReadyStories(ctx context.Context, epicID, host string) ([]StoryDetail, error)

	// ReadyForHost lists every story in the tracker that can be started on a
	// host right now, whatever epic it belongs to — what a dispatcher may take.
	// Each story comes back with its own epic's defaults overlaid, and a story
	// whose Path names no host is not listed for any host.
	ReadyForHost(ctx context.Context, host string) ([]StoryDetail, error)

	// RunningStories lists the stories claimed on a host and not yet finished:
	// what that host already has in flight, and so what a concurrency cap
	// counts. A story claimed on another host is not listed.
	RunningStories(ctx context.Context, host string) ([]StoryDetail, error)

	// BlockedForHost lists every open, unclaimed story in the tracker that
	// waits on a dependency not yet finished, on a host — what `mw status`
	// shows under "blocked" beside what ReadyForHost shows under "ready". Each
	// story comes back with its own epic's defaults overlaid, with its Needs
	// narrowed to the stories it is still waiting for, and a story whose Path
	// names no host is not listed for any host.
	BlockedForHost(ctx context.Context, host string) ([]StoryDetail, error)

	// ReadyWithLabel lists every bead carrying a label that is open and not
	// blocked, whatever it is pathed to — or whether it has a Path at all: the
	// tickets the Mayors file under the map for the Governor have no rig and no
	// host, and no host-keyed listing above would ever show them. A bead that is
	// held, claimed, closed or waiting on an open bead is not listed, and nor is
	// an epic. Each comes back with its own epic's defaults overlaid, when it
	// has an epic. It reads and writes nothing.
	ReadyWithLabel(ctx context.Context, label string) ([]StoryDetail, error)

	// WorkInHand lists every story that is ready to be taken or already claimed,
	// on every host, in one read: the two listings ReadyForHost and
	// RunningStories make, unnarrowed, so that a caller wanting what one host and
	// the others have in hand asks the tracker once, not once per section. Each
	// story comes back with its own epic's defaults overlaid; a story whose Path
	// names no host is listed, and WorkInHand's own narrowing leaves it out of
	// every host. It reads and writes nothing.
	WorkInHand(ctx context.Context) (WorkInHand, error)

	// ClaimStory takes a story: it becomes assigned and in progress, and stops
	// being ready. Claiming a story already claimed by this actor is harmless.
	ClaimStory(ctx context.Context, id string) error

	// ReleaseClaim gives a claim back: the story is unassigned and open again,
	// and any dispatcher may take it. It is how a dispatch that failed after
	// claiming leaves the story exactly as ready as it found it.
	ReleaseClaim(ctx context.Context, id string) error

	// SetStoryState records one dimension of a story's operational state — what
	// it is doing right now, as against what it is — with the reason it changed.
	// A dispatched story is run=running.
	SetStoryState(ctx context.Context, id, dimension, value, reason string) error

	// Formulas names the formulas installed where this tracker can pour them. A
	// formula a story names but the tracker does not have cannot be poured.
	Formulas(ctx context.Context) ([]string, error)

	// PourFormula pours a formula into step beads for one story and reports the
	// molecule it made: the root bead and the steps in the order they are
	// worked. Pouring twice makes two molecules, so pour only when the story has
	// no open molecule to work (see OpenMolecule).
	PourFormula(ctx context.Context, formula, storyID, title string) (Molecule, error)

	// StoryState reads back one dimension of a story's operational state, or ""
	// when that dimension has never been set.
	StoryState(ctx context.Context, id, dimension string) (string, error)

	// OpenMolecule reads back a molecule a story recorded: the root bead and the
	// steps of it not closed yet, in the order they are worked. It is the zero
	// Molecule when the root is closed or is not in the tracker at all — beads
	// are never deleted, but a root can be missing from this host's copy — and
	// an error only when the tracker could not say which. It reads and writes
	// nothing, and leaves Formula empty: the tracker does not know it.
	OpenMolecule(ctx context.Context, rootID string) (Molecule, error)

	// OpenSteps lists the steps of a poured formula that are not closed yet:
	// what the session working the story did not finish. A molecule whose steps
	// are all closed comes back empty, and so does one that was never poured.
	OpenSteps(ctx context.Context, moleculeID string) ([]FormulaStep, error)

	// SetStoryMetadata writes metadata fields onto a story, leaving the fields
	// it does not name alone. Path fields are metadata like any other.
	SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error

	// CommentOnStory appends one comment to a story.
	CommentOnStory(ctx context.Context, id, text string) error

	// StoryComments lists the comments left on a story, oldest first. It reads
	// and writes nothing, and a story with no comments has an empty list.
	StoryComments(ctx context.Context, id string) ([]Comment, error)

	// CloseStory closes a story with the reason it was closed for.
	CloseStory(ctx context.Context, id, reason string) error

	// StaleClaims lists claimed stories untouched for at least days days — the
	// sessions that went away without closing or handing back. days must be at
	// least 1. The stories come back without an epic's defaults overlaid.
	StaleClaims(ctx context.Context, days int) ([]StoryDetail, error)
}

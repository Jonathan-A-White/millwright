package application

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
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

// LabelHitlKindPrefix starts the label that says what kind of need a hitl story
// is: hitl:hands, hitl:decision, hitl:review or hitl:verify. A story is marked
// with hitl plus exactly one of them; a bare hitl is a hands need.
const LabelHitlKindPrefix = "hitl:"

// LabelHitlTodo marks a bead as an item on one of the Governor's own lists
// (errands, checks at a desk), filed as a hitl bead so no dispatcher takes it.
// It is not a need: the view's Needs you leaves it out, and the phone lists it
// under its epic until he closes it.
const LabelHitlTodo = LabelHitlKindPrefix + "todo"

// hitlKinds are the kinds a hitl:<kind> label may name, which are also the
// kinds of need the Governor's view lists for them.
var hitlKinds = []string{PosternNeedHands, PosternNeedDecision, PosternNeedReview, PosternNeedVerify}

// hitlKindsOf is the kinds the labels name with hitl:<kind>, in label order and
// each once. A hitl:<other> label names no kind.
func hitlKindsOf(labels []string) []string {
	var kinds []string
	for _, label := range labels {
		label = strings.ToLower(strings.TrimSpace(label))
		kind, ok := strings.CutPrefix(label, LabelHitlKindPrefix)
		if !ok || !slices.Contains(hitlKinds, kind) || slices.Contains(kinds, kind) {
			continue
		}
		kinds = append(kinds, kind)
	}
	return kinds
}

// hasHitlLabel reports whether the labels mark a story as worked with the
// Governor present: hitl, hitl:todo, or hitl:<kind> for a kind of need.
func hasHitlLabel(labels []string) bool {
	return hasLabel(labels, LabelHitl) || hasLabel(labels, LabelHitlTodo) || len(hitlKindsOf(labels)) > 0
}

// isTodo reports whether the labels make a bead an item on one of his own
// lists, which makes no need of any kind.
func isTodo(labels []string) bool {
	return hasLabel(labels, LabelHitlTodo)
}

// hitlNeedKind is the kind of need the labels make of a hitl story: the first
// hitl:<kind> label's kind, and hands when it has none.
func hitlNeedKind(labels []string) string {
	if kinds := hitlKindsOf(labels); len(kinds) > 0 {
		return kinds[0]
	}
	return PosternNeedHands
}

// DefaultPriority is the priority a story has when nobody set one, and what a
// tracker that reports none is taken to mean. Priorities run from 0, the most
// urgent, to 4, as beads has them.
const DefaultPriority = 2

// MostUrgentPriority and LeastUrgentPriority bound a priority, as beads has
// them: 0 is the most urgent, 4 the least.
const (
	MostUrgentPriority  = 0
	LeastUrgentPriority = 4
)

// ValidPriority reports why priority is not one beads holds, or nil.
func ValidPriority(priority int) error {
	if priority < MostUrgentPriority || priority > LeastUrgentPriority {
		return fmt.Errorf("priority %d is not one the tracker holds: priorities run %d (most urgent) to %d", priority, MostUrgentPriority, LeastUrgentPriority)
	}
	return nil
}

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
	// Guest is the owner of the story's rig when that rig is a guest repo, as
	// mw status and mw brief mark it; the tracker never sets it.
	Guest string
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
	// ClaimedAt is when the latest claim of the story was made, as a dispatch
	// recorded it (ClaimedAtField); zero when none did. Started is bd's and
	// stays at the first claim ever, so ClaimStarted is what to measure a run
	// from.
	ClaimedAt time.Time
	// SentBackAt is when mw next last sent the story's branch back to a fresh
	// session, as SentBackAtField says; zero when it never did.
	SentBackAt time.Time
	// QueuedAt is when mw next queued the story for batch landing, as
	// QueuedAtField says; zero when it never did.
	QueuedAt time.Time
	// Updated is when the tracker last recorded a change to the story; zero
	// when the tracker did not say.
	Updated time.Time
	// ClosedAt is when the story was closed, as the tracker recorded it; zero
	// when it is not closed or the tracker did not say.
	ClosedAt time.Time
	// CloseReason is why the story was closed, as the tracker has it; empty when
	// it is not closed or the tracker did not say.
	CloseReason string
	// IsEpic is true when this is not a story but an epic filed under an epic:
	// a listing of an epic's children returns it among them, and it is not work
	// a session takes. A tracker that does not say leaves it false.
	IsEpic bool
	// Type is the tracker's own word for what kind of bead this is — epic,
	// task, bug, feature, chore — as it spells it; empty when it did not say.
	Type string
	// Attempts is how many times a session was started for this story, as its
	// AttemptsField metadata says; zero when it never was. Exhausted says mw
	// dispatch has already told the Mayor the story used them all up.
	Attempts  int
	Exhausted bool
	// AttemptsRefunded is how many of those attempts a dead-pane reclaim has
	// given back, as the AttemptsRefundedField metadata says; zero when none.
	AttemptsRefunded int
	// RebaseSends is how many times mw next has sent this story's branch back
	// to a fresh session to rebase, and RebaseBase the target branch's head
	// the last of them was made against, as the RebaseSendsField and
	// RebaseBaseField metadata say; zero and empty when it never was.
	RebaseSends int
	RebaseBase  string
	// FilesChanged are the files the story's branch has changed so far, as mw
	// next recorded them (FilesChangedField); empty when none were.
	FilesChanged []string
	// ConflictFiles are the files the story's branch last conflicted in when a
	// landing merged it (ConflictFilesField); empty when it never did.
	ConflictFiles []string
	// SmokeExamples are the failing grist smoke examples the story names as its
	// cause, as the SmokeExamplesField metadata says (<app>/<kind>/<example>);
	// empty when it names none.
	SmokeExamples []string
	// Cause, CauseWhy and CauseFix are what mw cause recorded of why the story
	// failed, as the CauseField, CauseWhyField and CauseFixField metadata say:
	// the class (one of CauseClasses), the reason, and the bead that mends it;
	// empty when never named.
	Cause    string
	CauseWhy string
	CauseFix string
	// AttemptCauses is the class mw cause named for each attempt, by attempt,
	// as the AttemptCausesField metadata says; nil when none was named.
	AttemptCauses map[int]string
	// Molecule is the formula poured for this story, empty until it has been
	// poured. It is filled in by whoever pours it, not by reading the story.
	Molecule Molecule
	// Continues is what the boot tells the session of the branch it starts on
	// when an earlier attempt's branch was handed to it (mw-1ljpo9.7): that it
	// continues that branch, or why it starts from the target after all. It is
	// filled in by the dispatch that cuts the worktree, not by reading the
	// story; empty when the attempt starts from the target as any first does.
	Continues string
	// LeaseExpires is when the tracker's own lease on this story's claim runs
	// out unless a heartbeat renews it first; zero when the story is not
	// claimed or the tracker does not say. A claim found past this with its
	// session's tmux pane dead is a dispatch's strongest sign that the session
	// ended without mw next ever hearing about it (mw-gq6.106).
	LeaseExpires time.Time
	// CommentCount is how many comments the story carries, as the listing it
	// came from said; zero when the tracker did not say. A story reporting
	// zero can carry no marker written as a comment, so a reader looking for
	// one needs no further call to find that out (mw-tfne4.24).
	CommentCount int
	// TodoRank is where a hitl:todo card stands among those of its list, as the
	// TodoRankField metadata says; empty when it has none (InTodoOrder).
	TodoRank string
	// TodoDue is when a hitl:todo card is due, as the TodoDueField metadata says
	// (RFC 3339), and TodoReminded the TodoDue the Governor was last reminded
	// of it for (TodoRemindedField); empty when none.
	TodoDue      string
	TodoReminded string
	// TodoImportant is whether the card is starred, as the TodoImportantField
	// metadata says ("true").
	TodoImportant bool
	// TodoSize is how big a hitl:todo card is, as the TodoSizeField metadata says
	// (one of TodoSizes), and TodoThen the line filed as a card of its own when
	// this one is closed (TodoThenField); empty when none.
	TodoSize string
	TodoThen string
	// TodoRow is the row mw todo rank last put a hitl:todo card in, as the
	// TodoRowField metadata says (one of TodoRows), and TodoRowWhy its one line
	// on why (TodoRowWhyField); TodoSlipped is the due time it last found the
	// starred card slipped past (TodoSlippedField). Each is empty when none.
	TodoRow     string
	TodoRowWhy  string
	TodoSlipped string
	// TodoSlippedTold is the TodoSlipped due time the Governor was last told of
	// in the card's thread (TodoSlippedToldField); empty when none.
	TodoSlippedTold string
}

// RunningEverywhere is a tracker that reads what is in flight on this host and
// on the others in one read of its in-progress list, where RunningStories and
// RunningElsewhere each read it: a dispatch tick asks for it, when the tracker
// offers it, instead of both (mw-gq6.492). here is what RunningStories lists
// for host and away what RunningElsewhere lists.
type RunningEverywhere interface {
	RunningEverywhere(ctx context.Context, host string) (here, away []StoryDetail, err error)
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
	// OpenClaimed are the stories whose status is open yet whose assignee is
	// still a host's claim (mw@<host>): neither ready, which asks for the
	// unassigned, nor running, which asks for in progress (mw-gq6.417).
	OpenClaimed []StoryDetail
	// Held are the stories deferred (held back by the Mayor or the Governor),
	// on any host or none; none of them is ready. Only the kept connection
	// fills it: the fallback's bd calls are counted (a slow link pays seconds
	// for each), so it leaves Held empty.
	Held []StoryDetail
}

// Queued is every story queued for its rig's batch (RunQueued) that the batch
// takes, whether the tracker lists it in progress, open and claimed, or open
// and unclaimed: a hold and un-hold while it ran, or an open-steps give-back,
// leaves a queued story open, and a batch that read only the in-progress list
// never landed it (mw-bo813m2). A held or closed story is not in it.
func (w WorkInHand) Queued() []StoryDetail {
	var queued []StoryDetail
	for _, list := range [][]StoryDetail{w.Running, w.OpenClaimed, w.Ready} {
		for _, detail := range list {
			if QueuedForBatch(detail) {
				queued = append(queued, detail)
			}
		}
	}
	return queued
}

// QueuedHeld is the stories labelled queued for batch landing that are held
// (deferred): a batch leaves them out, and says so. Only the kept connection
// fills Held, so a tracker read the other way names none.
func (w WorkInHand) QueuedHeld() []StoryDetail {
	var held []StoryDetail
	for _, detail := range w.Held {
		if QueuedForBatch(detail) {
			held = append(held, detail)
		}
	}
	return held
}

// ReadyOn is the ready stories whose Path names this host, or domain.HostAuto,
// which any host may take. A story whose Path names no host is not offered to
// any host.
func (w WorkInHand) ReadyOn(host string) []StoryDetail {
	var on []StoryDetail
	for _, detail := range w.Ready {
		if merged := detail.Merged().Host; merged == domain.HostAuto || merged != "" && merged == host {
			on = append(on, detail)
		}
	}
	return on
}

// RunningOn is the claimed stories whose Path names this host.
func (w WorkInHand) RunningOn(host string) []StoryDetail { return pathedTo(w.Running, host) }

// Elsewhere is every story, ready and then claimed, whose Path names some host
// other than this one. A story whose Path names no host is nobody's, and one
// that names domain.HostAuto is not elsewhere: any host may take it.
func (w WorkInHand) Elsewhere(host string) []StoryDetail {
	var away []StoryDetail
	for _, list := range [][]StoryDetail{w.Ready, w.Running} {
		for _, detail := range list {
			if on := detail.Merged().Host; on != "" && on != host && on != domain.HostAuto {
				away = append(away, detail)
			}
		}
	}
	return away
}

// PathedTo is every story, ready and then claimed, whose Path names this host.
func (w WorkInHand) PathedTo(host string) []StoryDetail {
	return append(pathedTo(w.Ready, host), pathedTo(w.Running, host)...)
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

// ClaimStarted is when the claim the story is held under began: the latest
// claim a dispatch recorded, else bd's started_at, which an older claim by
// another host may have set. Zero when neither is known.
func (d StoryDetail) ClaimStarted() time.Time {
	if !d.ClaimedAt.IsZero() {
		return d.ClaimedAt
	}
	return d.Started
}

// QuietSince is when the story's silence is counted from: the later of its
// latest claim and its latest send-back, which put a fresh session to work on
// it and mailed the Mayor the Sent back line.
func (d StoryDetail) QuietSince() time.Time {
	since := d.ClaimStarted()
	if since.IsZero() {
		return since
	}
	if d.SentBackAt.After(since) {
		return d.SentBackAt
	}
	return since
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
// The Governor's own demo is one (GovernorsDemo), labelled hitl or not.
func (d StoryDetail) Hitl() bool {
	return hasHitlLabel(d.Labels) || d.GovernorsDemo()
}

// GovernorsDemo reports whether this story is labelled demo and names no files
// of its own: his own demo, which closes on his word and which no Builder
// works. A demo a Builder builds writes a file and so has a files key
// (mw-gq6.535).
func (d StoryDetail) GovernorsDemo() bool {
	return governorsDemo(d.Labels, d.Story.Overrides.Files)
}

// governorsDemo is GovernorsDemo for a story's labels and its own files key.
func governorsDemo(labels []string, files string) bool {
	return hasLabel(labels, LabelDemo) && strings.TrimSpace(files) == ""
}

// demoNoHitlLine is the warning for a story labelled demo that has neither hitl nor
// a files key, which mw file and mw path set print: it reads as the Governor's
// own demo, and is held from every dispatcher.
func demoNoHitlLine(id string) string {
	return fmt.Sprintf("%s is labelled demo with no files key and no hitl label: it is held as the Governor's own demo, so no Builder works it (a Builder-built demo names its files: mw path set %s files=...)", id, id)
}

// Todo reports whether this bead is an item on one of the Governor's own
// lists (hitl:todo), which is not a need of his.
func (d StoryDetail) Todo() bool {
	return isTodo(d.Labels)
}

// HitlKind is the kind of need this hitl story is: hands, decision, review or
// verify. A story with no hitl:<kind> label is hands.
func (d StoryDetail) HitlKind() string {
	return hitlNeedKind(d.Labels)
}

// HitlKinds are the kinds its hitl:<kind> labels name, more than one of which
// is a mistake `mw status` warns of.
func (d StoryDetail) HitlKinds() []string {
	return hitlKindsOf(d.Labels)
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

// ErrNotAnEpic is what ShowEpic and ShowEpics wrap when the id they were given
// is a bead that is not an epic, so that a caller who can take a story as well
// tells that refusal from any other.
var ErrNotAnEpic = errors.New("not an epic")

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
	// Bead is the epic's own bead, as a listing of its parent's children
	// would report it: the parent it is filed under (EpicID, empty for a
	// root), its labels, times, description and comment count — what a reader
	// showing the epic as one bead among the rest needs (mw postern view).
	// Its own default Path is Defaults above; Bead.Story.Overrides is the same
	// metadata read as overrides.
	Bead StoryDetail
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
//
// A story with no epic is only filed when Standalone says so: a bead of the
// factory's own making, such as a backend swap for a landing whose story had no
// epic, has no parent to be filed under.
type NewStory struct {
	EpicID string
	// Standalone files the story with no parent, and is how an empty EpicID is
	// told from one that was forgotten.
	Standalone      bool
	Title           string
	Description     string
	Acceptance      string
	Priority        int
	EstimateMinutes int
	Overrides       domain.Path
	Needs           []string
	// Labels are tags the story is filed with.
	Labels []string
}

// StoryEdit is a change to a bead's own words: the title and the description
// it is to have. A nil field is left as it is.
type StoryEdit struct {
	Title       *string
	Description *string
}

// BeadPage is one bead as ShowBeadPage reads it.
type BeadPage struct {
	// Bead is the bead as ShowBeads reports it.
	Bead StoryDetail
	// Comments are the bead's, oldest first, as StoryComments lists them.
	Comments []Comment
	// Children are the direct children of the bead, whatever its type (a task
	// can be a parent too), in the order they were filed, as ShowEpic lists its
	// Stories; nil when it has none.
	Children []StoryDetail
	// Parent is the bead's parent read as an epic, only its Defaults and
	// Stories filled in; nil when the bead has none, or its parent cannot be
	// read as an epic.
	Parent *EpicDetail
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

	// ShowEpics is ShowEpic for several epics at once, in the order ids names
	// them: a caller reading many epics — a project view across all of them,
	// say — asks the tracker to spend as few calls as it can on their own
	// fields (title, status, priority) rather than one per epic, though each
	// epic's children may still cost their own call underneath. An id that
	// names no epic is an error, just as ShowEpic's is.
	ShowEpics(ctx context.Context, ids []string) ([]EpicDetail, error)

	// ShowBeads reads several beads at once, whatever each is — story, epic
	// or anything else the tracker holds — in the order ids names them, in as
	// few calls as the tracker allows: what a reader that must look at a
	// handful of beads outside the epics it already read (the parents of a
	// live epic, a blocker in another tree) asks instead of ShowStory once
	// each. Each bead's Needs are narrowed to the unfinished, and a story's
	// Defaults are its parent's when the tracker hands them over in the same
	// read, empty otherwise. An id the tracker does not know is left out
	// rather than failing the whole read. It reads and writes nothing.
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)

	// ShowBeadPage reads one bead with everything a page of its own shows, in as
	// few calls as the tracker allows — three for a bead with a parent: the bead as ShowBeads reads it, its comments as
	// StoryComments does, its children when it is an epic, and its parent's
	// (see BeadPage). False, with nothing else, when the tracker does not know
	// the bead. It reads and writes nothing.
	ShowBeadPage(ctx context.Context, id string) (BeadPage, bool, error)

	// LiveEpics lists the ids of every epic that is open or in progress, in
	// the order the tracker files them — what a project view means by "live":
	// a held or closed epic has nothing left for it to say. It reads and
	// writes nothing.
	LiveEpics(ctx context.Context) ([]string, error)

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

	// RunningElsewhere lists the stories claimed on any host but the one named
	// and not yet finished: what the other hosts hold, whose files a dispatcher
	// keeps from the stories it has ready (mw-gq6.389). They are not what a cap
	// counts.
	RunningElsewhere(ctx context.Context, host string) ([]StoryDetail, error)

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

	// ClaimStory takes a story: it becomes assigned and in progress, stops
	// being ready, and carries a lease the tracker keeps (see LeaseExpires)
	// that a heartbeat renews. Claiming a story already claimed by this actor
	// is harmless. The claim is conditional: a story another assignee already
	// holds is not taken from them — it fails with a *ClaimHeldError, and
	// nothing is written. A run state (RunStates) an earlier attempt left on the
	// story is cleared by the claim, so a fresh claim is never read as refused
	// before its session has started.
	ClaimStory(ctx context.Context, id string) error

	// HeartbeatClaim pushes the lease on a claim this actor holds forward by
	// the tracker's lease length. It fails, writing nothing, when the story is
	// no longer this actor's claim — reclaimed, released or closed — which is
	// how whatever is heartbeating learns to stop.
	HeartbeatClaim(ctx context.Context, id string) error

	// ReclaimStory gives back a claim whose lease has expired with no heartbeat
	// since: the story is unassigned and open again, the same as ReleaseClaim
	// leaves it. It reports whether it did. A claim whose lease still holds,
	// and a claim that carries no lease at all, are left exactly as they were
	// and reported not reclaimed.
	ReclaimStory(ctx context.Context, id string) (bool, error)

	// ReleaseClaim gives a claim back: the story is unassigned and open again,
	// and any dispatcher may take it. It is how a dispatch that failed after
	// claiming leaves the story exactly as ready as it found it. It is
	// conditional on this actor's own name: a claim another actor holds is
	// left exactly as it was, and reported as a failure, writing nothing.
	ReleaseClaim(ctx context.Context, id string) error

	// ReleaseClaimHeldBy is ReleaseClaim for a claim another actor holds: it is
	// conditional on holder, the actor the story is assigned to, rather than on
	// this actor. It is how a retry on one host gives back a claim a dispatch on
	// another host made, whose actor is that host's. An empty holder asks for
	// ReleaseClaim.
	ReleaseClaimHeldBy(ctx context.Context, id, holder string) error

	// HoldClaimHeldBy is ReleaseClaimHeldBy that leaves the story held
	// (StatusHeld) rather than open, in the one write: no dispatcher can take
	// the story between the give-back and the hold (mw-gq6.597). It is
	// conditional on holder the same way.
	HoldClaimHeldBy(ctx context.Context, id, holder string) error

	// ClearRunState takes every run:* label (RunStates) and the run state they
	// record off a story, in one write, as a claim does. A give-back keeps
	// the label its caller set (a cancel's run:cancelled, a landed demo's
	// run:landed), so a retry, which starts the story afresh, clears it itself:
	// a run:queued left on a story given back is read by dispatch as a story
	// waiting for its rig's batch, and attempt 2 never starts (mw-gq6.823).
	ClearRunState(ctx context.Context, id string) error

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
	// no open molecule to work (see OpenMolecule). A story that is a bug (see
	// domain.IsBugStory) is poured as one: the steps that ask for the
	// regression test, the class and the sweep are made in place of the plain
	// red and closing steps, by a formula that has them.
	PourFormula(ctx context.Context, formula, storyID, title string, bug bool) (Molecule, error)

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

	// EditStory changes a bead's title, its description, or both, leaving what
	// the edit names no value for as it is. A blank title is refused before the
	// tracker is asked. It is the Governor's todo_edit tap (section 13).
	EditStory(ctx context.Context, id string, edit StoryEdit) error

	// MoveStory files a bead under another parent, taking it from the one it
	// has. It is the Governor's todo_move tap to another list (section 13).
	MoveStory(ctx context.Context, id, parent string) error

	// SetStoryPriority sets a bead's priority, 0 (most urgent) to 4; anything
	// outside that is refused before the tracker is asked. It is the
	// Governor's priority tap (postern's docs/protocol.md section 13).
	SetStoryPriority(ctx context.Context, id string, priority int) error

	// HoldStory holds a story back from every dispatcher, as it was when it
	// was filed (StatusHeld), until ReleaseStory lets it through again. Whether
	// the story is open and unclaimed is the caller's to check first.
	HoldStory(ctx context.Context, id string) error

	// AddLabel puts label on a bead, leaving its other labels as they are; a
	// bead already carrying it is left as it is.
	AddLabel(ctx context.Context, id, label string) error

	// RemoveLabel takes label off a bead, leaving its other labels as they are;
	// a bead not carrying it is left as it is.
	RemoveLabel(ctx context.Context, id, label string) error

	// AddBlocker makes blocker block blocked: blocked waits on blocker, as if
	// it had been filed with it in Needs. A blocker blocked already has is left
	// as it is.
	AddBlocker(ctx context.Context, blocked, blocker string) error

	// CommentOnStory appends one comment to a story.
	CommentOnStory(ctx context.Context, id, text string) error

	// StoryComments lists the comments left on a story, oldest first. It reads
	// and writes nothing, and a story with no comments has an empty list.
	StoryComments(ctx context.Context, id string) ([]Comment, error)

	// StoriesComments is StoryComments for several stories at once, keyed by
	// id: a caller that would otherwise read many stories' comments back to
	// back asks the tracker to spend one call on all of them instead of one
	// each. A story with no comments has an empty list, same as
	// StoryComments, and an id the tracker does not recognise is simply
	// absent from the map rather than failing the whole call.
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)

	// CloseStory closes a story with the reason it was closed for. A story in
	// progress whose claim another actor holds is refused with a
	// *CloseHeldError, nothing written: only a claim's holder closes it.
	CloseStory(ctx context.Context, id, reason string) error

	// TakeOverClaim makes this actor the holder of a claim holder has, by
	// force, so that a story landed here can be closed here under this
	// actor's own name rather than under the other host's (mw-gq6.475). A
	// story holder no longer holds is left as it is, and reported as a
	// failure. It is for a story whose work is done: a claim with a session
	// behind it is never taken.
	TakeOverClaim(ctx context.Context, id, holder string) error

	// StaleClaims lists the claimed stories whose lease had expired by now
	// with no heartbeat since — the sessions that went away without closing or
	// handing back, which ReclaimStory may give back. A claim that carries no
	// lease is not listed: nothing says when its holder was last heard from.
	// Each story comes back with its own epic's defaults overlaid. It reads
	// and writes nothing.
	StaleClaims(ctx context.Context, now time.Time) ([]StoryDetail, error)
}

// ClaimHeldError is a claim refused because another assignee already holds
// the story: the conditional claim failing cleanly, with nothing written.
type ClaimHeldError struct {
	ID     string
	Holder string
}

func (e *ClaimHeldError) Error() string {
	return fmt.Sprintf("%s is already claimed by %s, so it was not taken", e.ID, e.Holder)
}

// claimHeld matches the words of a ClaimHeldError, for a refusal that reached
// a caller as its text and not its type.
var claimHeld = regexp.MustCompile(`(\S+) is already claimed by (\S+), so it was not taken`)

// ClaimHeldBy reports whether err is a claim refused because another actor
// holds the story, and who. It reads the typed error first and the words of one
// after it, so that a refusal that crossed a boundary which kept only its text
// (a log line, a flattened error) is still known for what it was.
func ClaimHeldBy(err error) (holder string, held bool) {
	var typed *ClaimHeldError
	if errors.As(err, &typed) {
		return typed.Holder, true
	}
	if err == nil {
		return "", false
	}
	if found := claimHeld.FindStringSubmatch(err.Error()); found != nil {
		return found[2], true
	}
	return "", false
}

// CloseHeldError is a close refused because another actor holds the story's
// claim: beads lets only the claim's holder close a story in progress, so a
// story another host claimed is refused to this one (mw-gq6.475). Actor is
// the name this tracker acts under, and Said what the tracker said, when it
// said anything.
type CloseHeldError struct {
	ID     string
	Holder string
	Actor  string
	Said   string
}

func (e *CloseHeldError) Error() string {
	if e.Said != "" {
		return e.Said
	}
	return fmt.Sprintf("cannot close %s: assignee is %q, actor is %q", e.ID, e.Holder, e.Actor)
}

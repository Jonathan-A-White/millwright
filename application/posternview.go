package application

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Jonathan-A-White/millwright/domain"
)

// PosternViewVersion is the live view's version, postern's docs/protocol.md
// §11: the "v" every view and every bead detail (§12) carries.
const PosternViewVersion = 2

// PosternViewRecentWindow is how recently a closed bead must have closed to
// be in the view at all: postern's docs/protocol.md §11. An epic counts the
// direct children that closed before it in its done_earlier instead.
const PosternViewRecentWindow = 7 * 24 * time.Hour

// PosternViewVerifyWindow is how recently a story must have closed, with no
// VERIFIED comment, to be a verify need: §11's table, the same day
// PosternSnapshotWindow gives a landing.
const PosternViewVerifyWindow = PosternSnapshotWindow

// PosternViewStaleApprove is how long a live epic's oldest held story may wait
// for the Governor's word before its approve need becomes a stale one: §11.
const PosternViewStaleApprove = 7 * 24 * time.Hour

// PosternViewStaleHands is how long a hands bead may wait for his hands before
// its hands need becomes a stale one: §11.
const PosternViewStaleHands = 3 * 24 * time.Hour

// PosternViewSummaryLimit is the most runes of a bead's description its
// summary carries, as plain text: §11.
const PosternViewSummaryLimit = 280

// PosternViewHostStale is how old a host's last sync may be before it is an
// alarm: §11's table, the same twenty minutes the quiet alarm allows
// (DefaultNudgeSyncStale).
const PosternViewHostStale = DefaultNudgeSyncStale

// The kinds of need the view lists, §11's table.
const (
	PosternNeedQuestion = "question"
	PosternNeedApprove  = "approve"
	PosternNeedVerify   = "verify"
	PosternNeedDemo     = "demo"
	PosternNeedHands    = "hands"
	PosternNeedStale    = "stale"
	PosternNeedAlarm    = "alarm"
)

// Who a need waits on, its waits_for: the Governor, the Mayor or the factory.
const (
	PosternWaitsYou     = "you"
	PosternWaitsMayor   = "mayor"
	PosternWaitsFactory = "factory"
)

// LabelDemo is the label on a bead the Governor is to be shown working: a
// demo need while it is open.
const LabelDemo = "demo"

// posternNeedRank orders needs of different kinds that tie on blocks and
// since, so the view comes out the same every run.
var posternNeedRank = map[string]int{
	PosternNeedAlarm: 0, PosternNeedQuestion: 1, PosternNeedApprove: 2,
	PosternNeedHands: 3, PosternNeedVerify: 4, PosternNeedStale: 5, PosternNeedDemo: 6,
}

// PosternViewDoc is the live view's plaintext, postern's docs/protocol.md §11.
type PosternViewDoc struct {
	V         int               `json:"v"`
	WrittenAt string            `json:"written_at"`
	Host      string            `json:"host"`
	Hosts     []PosternViewHost `json:"hosts"`
	Needs     []PosternViewNeed `json:"needs"`
	Beads     []PosternViewBead `json:"beads"`
}

// PosternViewHost is one host and when it last recorded itself level.
type PosternViewHost struct {
	Name     string `json:"name"`
	LastSync string `json:"last_sync"`
}

// PosternViewNeed is one thing waiting on the Governor, §11's needs.
type PosternViewNeed struct {
	Kind        string   `json:"kind"`
	Bead        string   `json:"bead"`
	Epic        string   `json:"epic"`
	Title       string   `json:"title"`
	Since       string   `json:"since"`
	Text        string   `json:"text"`
	Recommended string   `json:"recommended"`
	Options     []string `json:"options"`
	Blocks      int      `json:"blocks"`
	// WaitsFor says who the need waits on: PosternWaitsYou, PosternWaitsMayor
	// or PosternWaitsFactory. Only a need waiting on him is one he can act on.
	WaitsFor string `json:"waits_for"`
	// Steps are a hands need's steps (§17), absent on any other need.
	Steps []PosternViewHandsStep `json:"steps,omitempty"`
	// NotReady marks a hands or demo need that cannot be acted on yet, and
	// WaitingOn says what it waits on: the titles of its open blockers, and
	// for hands with no steps, the Mayor to write them. A card so marked
	// offers neither Approve nor Done. Both are absent on a ready need.
	NotReady  bool     `json:"not_ready,omitempty"`
	WaitingOn []string `json:"waiting_on,omitempty"`
}

// PosternViewHandsStep is one hands step as a hands need carries it: the
// step, the sha256 of its canonical bytes that his approval binds, and —
// once it has run — how.
type PosternViewHandsStep struct {
	domain.HandsStep
	SHA256 string    `json:"sha256"`
	Ran    *HandsRan `json:"ran,omitempty"`
}

// PosternViewPath is a bead's merged Path as §11 writes it: every field, empty
// where the tracker does not know it.
type PosternViewPath struct {
	Rig     string `json:"rig"`
	Branch  string `json:"branch"`
	Host    string `json:"host"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	Formula string `json:"formula"`
	Harness string `json:"harness"`
}

// PosternViewBead is one bead of the view, §11's beads.
type PosternViewBead struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Type        string           `json:"type"`
	Status      string           `json:"status"`
	Priority    int              `json:"priority"`
	Parent      string           `json:"parent"`
	Labels      []string         `json:"labels"`
	Assignee    string           `json:"assignee"`
	Waits       []string         `json:"waits"`
	Created     string           `json:"created"`
	Updated     string           `json:"updated"`
	Started     string           `json:"started"`
	Closed      string           `json:"closed"`
	Path        *PosternViewPath `json:"path,omitempty"`
	Attempts    int              `json:"attempts"`
	Summary     string           `json:"summary"`
	Comments    int              `json:"comments"`
	DoneEarlier int              `json:"done_earlier"`
}

// PosternView builds the live view of the factory, postern's docs/protocol.md
// §11 — every live epic, every bead under one at any depth, every live bead's
// parent chain, and everything waiting on the Governor — and, once built,
// seals it to the Governor's key and writes it where the postern backend
// serves it (GET /api/view). It replaces PosternSnapshot's ten-minute brief
// with one cheap enough to run every half minute on the factory's own host:
//
//   - LiveEpics, once;
//   - ShowEpics, once for every live epic — one call for their own fields and
//     one per epic for its children, the one call per epic the tracker needs;
//   - NotesWithPrefix, once, for every question, every host's last sync and
//     the landed memory at the same time;
//   - StoriesComments, at most once, only for an open question whose note does
//     not say what it asked and a landing the memory cannot answer;
//   - ShowEpics again only for a child epic that is not live (held, or closed
//     within the week), and ShowBeads only for a parent or a blocker that no
//     epic read already holds — neither in the usual run.
//
// Building writes nothing but the landed memory note, and that only when it
// changed.
type PosternView struct {
	Tracker WorkTracker
	Notes   PosternNotes
	Cipher  Cipher
	// File is where the sealed view is written, atomically: config
	// postern_view_path. The snapshot's own port serves, since both are one
	// sealed file written whole.
	File PosternSnapshotFile

	// GovernorKey is who the view is sealed to: config postern_governor_key.
	// Required only by Run, not Build.
	GovernorKey string
	// Host is this host's own name, the view's "host".
	Host string

	// Now is the clock written_at is stamped with and every window is
	// measured against. The zero value reads the real one.
	Now func() time.Time

	// Out is where Run says what it wrote. A nil Out prints nothing.
	Out io.Writer
	// Err is where a failure to write the landed memory is said; the view is
	// built regardless. A nil Err says nothing.
	Err io.Writer
}

// Run builds the view, seals it to GovernorKey and writes it through File,
// reporting the view it wrote. What writing needs is checked before the
// tracker is read.
func (v PosternView) Run(ctx context.Context) (PosternViewDoc, error) {
	if err := v.checkWritable(); err != nil {
		return PosternViewDoc{}, err
	}
	doc, err := v.Build(ctx)
	if err != nil {
		return PosternViewDoc{}, err
	}
	sealed, plain, zipped, err := SealPosternDoc(v.Cipher, v.GovernorKey, doc)
	if err != nil {
		return PosternViewDoc{}, err
	}
	if err := v.File.Write(ctx, []byte(sealed)); err != nil {
		return PosternViewDoc{}, err
	}
	if v.Out != nil {
		fmt.Fprintf(v.Out, "wrote the view of %d bead(s) and %d need(s), %d plaintext byte(s), %d gzipped, to %s\n",
			len(doc.Beads), len(doc.Needs), plain, zipped, v.File.Path())
	}
	return doc, nil
}

// checkWritable reports whether Run has what writing needs, without touching
// the tracker.
func (v PosternView) checkWritable() error {
	switch {
	case v.Cipher == nil:
		return fmt.Errorf("mw postern view: no cipher is configured")
	case v.File == nil:
		return fmt.Errorf("mw postern view: nowhere to write the view")
	case strings.TrimSpace(v.GovernorKey) == "":
		return fmt.Errorf("mw postern view: postern_governor_key is not set, so there is no one to seal it to")
	}
	return nil
}

// viewEntry is one bead as the view holds it while it is built.
type viewEntry struct {
	detail StoryDetail
	// path is what the view writes as the bead's path: a story's merged Path,
	// always; an epic's own defaults, nil when it has none.
	path *PosternViewPath
	// underLive says the bead is a direct child of a live epic: a story
	// closed within the day there is a verify candidate, the same one
	// PosternSnapshot calls landed, so the two share one memory.
	underLive bool
	// doneEarlier counts the direct children of an epic whose children were
	// read that closed before PosternViewRecentWindow.
	doneEarlier int
	// ancestor says the bead is in the view only as a parent in a live
	// bead's chain, whatever its status.
	ancestor bool
	waits    []string
}

// viewBuild is the view while it is built.
type viewBuild struct {
	now     time.Time
	entries map[string]*viewEntry
	order   []string
	live    map[string]bool
	epics   map[string]EpicDetail
	// outside is every bead read only to learn whether a bead in the view
	// still waits on it.
	outside map[string]StoryDetail
	// dependents is waits turned round: the unfinished beads waiting on each
	// bead, filled in by computeWaits for blocks to walk.
	dependents map[string][]string
}

func (b *viewBuild) add(e *viewEntry) {
	if _, seen := b.entries[e.detail.Story.ID]; seen {
		return
	}
	b.entries[e.detail.Story.ID] = e
	b.order = append(b.order, e.detail.Story.ID)
}

// Build reads the tracker and reports the view, without sealing or writing
// it: what --json prints for inspection.
func (v PosternView) Build(ctx context.Context) (PosternViewDoc, error) {
	if v.Tracker == nil {
		return PosternViewDoc{}, fmt.Errorf("mw postern view: no work tracker is configured")
	}
	if v.Notes == nil {
		return PosternViewDoc{}, fmt.Errorf("mw postern view: nowhere to read the notes from")
	}
	b := &viewBuild{
		now: v.now(), entries: map[string]*viewEntry{},
		live: map[string]bool{}, epics: map[string]EpicDetail{}, outside: map[string]StoryDetail{},
	}

	// Every note at once: bd keeps them in one table and lists it whole, so
	// one read serves the questions, the hosts and the landed memory.
	notes, err := v.Notes.NotesWithPrefix(ctx, "")
	if err != nil {
		return PosternViewDoc{}, fmt.Errorf("reading the notes: %w", err)
	}
	memory := parseLandedMemory(notes[PosternSnapshotMemoryKey])

	ids, err := v.Tracker.LiveEpics(ctx)
	if err != nil {
		return PosternViewDoc{}, fmt.Errorf("listing the live epics: %w", err)
	}
	live, err := v.Tracker.ShowEpics(ctx, ids)
	if err != nil {
		return PosternViewDoc{}, fmt.Errorf("reading the live epics: %w", err)
	}
	for _, epic := range live {
		b.live[epic.ID] = true
	}
	for _, epic := range live {
		b.add(&viewEntry{detail: epicBeadOf(epic), path: viewPathOrNil(epic.Defaults)})
	}
	if err := v.readTrees(ctx, b, live); err != nil {
		return PosternViewDoc{}, err
	}
	if err := v.readOutside(ctx, b); err != nil {
		return PosternViewDoc{}, err
	}
	b.computeWaits()

	needs, newMemory, err := v.needs(ctx, b, notes, memory)
	if err != nil {
		return PosternViewDoc{}, err
	}
	if err := saveLandedMemory(ctx, v.Notes, memory, newMemory); err != nil {
		sayLandedMemoryFailed(v.Err, err)
	}

	hosts, hostNeeds := v.hosts(b, notes)
	needs = append(needs, hostNeeds...)
	sortPosternNeeds(needs)

	doc := PosternViewDoc{
		V: PosternViewVersion, WrittenAt: b.now.UTC().Format(time.RFC3339), Host: v.Host,
		Hosts: hosts, Needs: needs, Beads: make([]PosternViewBead, 0, len(b.order)),
	}
	for _, id := range b.order {
		e := b.entries[id]
		if !e.ancestor && !b.recent(e.detail) {
			continue
		}
		doc.Beads = append(doc.Beads, viewBeadOf(e))
	}
	return doc, nil
}

// epicBeadOf is a read epic as one bead of the view: its own bead, with the
// title, status and priority the epic reading reports.
func epicBeadOf(epic EpicDetail) StoryDetail {
	bead := epic.Bead
	bead.Story.ID = epic.ID
	bead.Story.Title = epic.Title
	bead.Status = epic.Status
	bead.Priority = epic.Priority
	bead.IsEpic = true
	return bead
}

// readTrees adds the children of every epic in epics, and then of every child
// epic that is not live but is still in the view (held, or closed within the
// week) — read level by level in one ShowEpics each — so that every bead
// under a live epic is in the view at any depth. A live child epic needs no
// second read: it is among the live epics already.
func (v PosternView) readTrees(ctx context.Context, b *viewBuild, epics []EpicDetail) error {
	for len(epics) > 0 {
		var next []string
		for _, epic := range epics {
			b.epics[epic.ID] = epic
			parent := b.entries[epic.ID]
			for _, child := range epic.Stories {
				if child.Closed() && !b.recent(child) && parent != nil {
					parent.doneEarlier++
				}
				if b.live[child.Story.ID] {
					continue
				}
				entry := &viewEntry{detail: child, underLive: b.live[epic.ID]}
				if child.IsEpic {
					entry.path = viewPathOrNil(child.Story.Overrides)
					if b.recent(child) {
						if _, read := b.epics[child.Story.ID]; !read {
							next = append(next, child.Story.ID)
						}
					}
				} else {
					entry.path = viewPathOf(child.Merged())
				}
				b.add(entry)
			}
		}
		if len(next) == 0 {
			return nil
		}
		read, err := v.Tracker.ShowEpics(ctx, next)
		if err != nil {
			return fmt.Errorf("reading the child epics %s: %w", strings.Join(next, ", "), err)
		}
		epics = read
	}
	return nil
}

// readOutside reads, in one ShowBeads a round, whatever the tree names but
// does not hold: the parent of a bead in the view that is not itself in the
// view (so every live bead's chain reaches its root), and a bead one in the
// view waits on in another tree (so its waits say whether it is finished).
// A parent read this way joins the view as an ancestor; a blocker does not.
// The usual run reads nothing here.
func (v PosternView) readOutside(ctx context.Context, b *viewBuild) error {
	pending := append([]string(nil), b.order...)
	asked := map[string]bool{}
	for len(pending) > 0 {
		var ask []string
		parentOf := map[string]bool{}
		for _, id := range pending {
			e := b.entries[id]
			if !e.ancestor && !b.recent(e.detail) {
				continue
			}
			if p := e.detail.EpicID; p != "" && b.entries[p] == nil && !asked[p] {
				asked[p] = true
				parentOf[p] = true
				ask = append(ask, p)
			}
			if e.detail.Closed() {
				continue
			}
			for _, need := range e.detail.Needs {
				if b.entries[need] == nil && !asked[need] {
					asked[need] = true
					ask = append(ask, need)
				}
			}
		}
		if len(ask) == 0 {
			return nil
		}
		found, err := v.Tracker.ShowBeads(ctx, ask)
		if err != nil {
			return fmt.Errorf("reading %s, named by the view's beads: %w", strings.Join(ask, ", "), err)
		}
		pending = nil
		for _, detail := range found {
			id := detail.Story.ID
			b.outside[id] = detail
			if !parentOf[id] {
				continue
			}
			path := viewPathOf(detail.Merged())
			if detail.IsEpic {
				path = viewPathOrNil(detail.Story.Overrides)
			}
			b.add(&viewEntry{detail: detail, path: path, ancestor: true})
			pending = append(pending, id)
		}
	}
	return nil
}

// computeWaits fills in every unfinished bead's waits: the beads it waits on
// that the view, or a read of the beads outside it, knows to be unfinished.
// A need the tracker has no record of at all is not a wait.
func (b *viewBuild) computeWaits() {
	b.dependents = map[string][]string{}
	for _, id := range b.order {
		e := b.entries[id]
		e.waits = []string{}
		if e.detail.Closed() {
			continue
		}
		for _, need := range e.detail.Needs {
			if blocker, ok := b.entries[need]; ok {
				if !blocker.detail.Closed() {
					e.waits = append(e.waits, need)
				}
				continue
			}
			if blocker, ok := b.outside[need]; ok && !blocker.Closed() {
				e.waits = append(e.waits, need)
			}
		}
		for _, w := range e.waits {
			b.dependents[w] = append(b.dependents[w], id)
		}
	}
}

// recent reports whether detail belongs in the view by its status: anything
// not closed, and anything closed within PosternViewRecentWindow.
func (b *viewBuild) recent(detail StoryDetail) bool {
	if !detail.Closed() {
		return true
	}
	return !detail.ClosedAt.IsZero() && b.now.Sub(detail.ClosedAt) <= PosternViewRecentWindow
}

// inView reports whether the bead id is one the view writes.
func (b *viewBuild) inView(id string) bool {
	e, ok := b.entries[id]
	return ok && (e.ancestor || b.recent(e.detail))
}

// blocks counts the distinct unfinished beads in the view that wait on any
// of ids, directly or through others; with self, ids themselves count too
// (an epic's held stories are what its approval holds up).
func (b *viewBuild) blocks(self bool, ids ...string) int {
	seen := map[string]bool{}
	queue := append([]string(nil), ids...)
	if self {
		for _, id := range ids {
			seen[id] = true
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, d := range b.dependents[id] {
			if !seen[d] {
				seen[d] = true
				queue = append(queue, d)
			}
		}
	}
	return len(seen)
}

// needs lists every need a bead in the view makes, §11's table but the host
// alarms: questions, approvals, verifications, demos, hands and a story out
// of attempts. The comments it needs — an open question whose note does not
// say what it asked, a landing the memory cannot answer — are read in one
// StoriesComments; newMemory is the landed memory as this run leaves it.
func (v PosternView) needs(ctx context.Context, b *viewBuild, notes map[string]string, memory map[string]posternSnapshotMemoryEntry) ([]PosternViewNeed, map[string]posternSnapshotMemoryEntry, error) {
	var questions, verifies []*viewEntry
	var needComments []string
	for _, id := range b.order {
		e := b.entries[id]
		if !b.inView(id) || e.detail.Closed() {
			continue
		}
		note := notes[PosternQuestionKey(id)]
		if strings.TrimSpace(note) == "" {
			continue
		}
		questions = append(questions, e)
		if _, ok := questionFromNote(note); !ok && e.detail.CommentCount > 0 {
			needComments = append(needComments, id)
		}
	}
	for _, id := range b.order {
		e := b.entries[id]
		d := e.detail
		if !e.underLive || d.IsEpic || !d.Closed() || d.ClosedAt.IsZero() || b.now.Sub(d.ClosedAt) > PosternViewVerifyWindow {
			continue
		}
		// A closed hands bead is his own doing, a step he approved and ran or a
		// bead he closed: only a landing asks him to verify.
		if hasLabel(d.Labels, LabelHitl) {
			continue
		}
		verifies = append(verifies, e)
		if d.CommentCount == 0 {
			continue
		}
		if mem, known := memory[id]; known && mem.Count == d.CommentCount {
			continue
		}
		needComments = append(needComments, id)
	}

	// An approve or hands need older than its window is read as a stale one,
	// which quotes its bead's newest comment, so that comment is read here
	// too.
	var waiting []waitingNeed
	for _, id := range b.order {
		e := b.entries[id]
		if !b.inView(id) || b.kept(e, notes) {
			continue
		}
		d := e.detail
		if b.live[id] {
			if need, held, ok := b.approve(e); ok {
				waiting = append(waiting, waitingNeed{need: need, entry: e, window: PosternViewStaleApprove, fact: heldFact(held)})
			}
		}
		if workable(d) && hasLabel(d.Labels, LabelHitl) {
			steps := viewHandsSteps(id, notes)
			// A demo bead is labelled hitl only to keep it from dispatch; it
			// is his demo card alone unless it also carries a hands step.
			demoOnly := hasLabel(d.Labels, LabelDemo) && len(steps) == 0
			if !demoOnly && !allStepsRanClean(steps) {
				need := b.need(PosternNeedHands, e, firstKnown(d.Created, d.Updated), viewSummary(d.Description))
				need.Steps = steps
				waiting = append(waiting, waitingNeed{need: need, entry: e, window: PosternViewStaleHands, fact: handsFact(steps)})
				// A hands bead with no steps may hold his instructions in a
				// BY HAND comment, so its comments are read.
				if len(steps) == 0 && d.CommentCount > 0 {
					needComments = append(needComments, id)
				}
			}
		}
	}
	for _, w := range waiting {
		if w.stale(b.now) && w.entry.detail.CommentCount > 0 {
			needComments = append(needComments, w.entry.detail.Story.ID)
		}
	}

	comments := map[string][]Comment{}
	needComments = uniqueStrings(needComments)
	if len(needComments) > 0 {
		read, err := v.Tracker.StoriesComments(ctx, needComments)
		if err != nil {
			return nil, nil, fmt.Errorf("reading the comments of the view's questions and landings: %w", err)
		}
		comments = read
	}

	needs := []PosternViewNeed{}
	for _, e := range questions {
		facts, ok := questionFromNote(notes[PosternQuestionKey(e.detail.Story.ID)])
		if !ok {
			facts, _ = questionFromComments(comments[e.detail.Story.ID])
		}
		need := b.need(PosternNeedQuestion, e, facts.AskedAt, facts.Text)
		need.Recommended = facts.Recommended
		if facts.Options != nil {
			need.Options = facts.Options
		}
		needs = append(needs, need)
	}

	newMemory := map[string]posternSnapshotMemoryEntry{}
	for _, e := range verifies {
		if _, unverified := landedVerdict(e.detail, comments, memory, newMemory); !unverified {
			continue
		}
		howTo := landedHowTo(newMemory[e.detail.Story.ID])
		need := b.need(PosternNeedVerify, e, e.detail.ClosedAt, verifyText(e.detail, howTo))
		if howTo == "" {
			need.WaitsFor = PosternWaitsMayor
			need.NotReady = true
			need.WaitingOn = []string{viewMayorChecksLanding}
		} else {
			need.Options = []string{"Verified"}
		}
		needs = append(needs, need)
	}

	for i := range waiting {
		w := &waiting[i]
		if w.need.Kind == PosternNeedHands && !w.stale(b.now) {
			steps := w.need.Steps
			byHand := ""
			if len(steps) == 0 {
				byHand = byHandInstructions(comments[w.entry.detail.Story.ID])
			}
			b.markNotReady(&w.need, w.entry, len(steps) == 0 && byHand == "")
			if byHand != "" && !w.need.NotReady {
				w.need.Text = byHand
			}
		}
		if w.stale(b.now) {
			needs = append(needs, w.staleNeed(b, comments[w.entry.detail.Story.ID]))
			continue
		}
		needs = append(needs, w.need)
	}

	for _, id := range b.order {
		e := b.entries[id]
		if !b.inView(id) || !workable(e.detail) {
			continue
		}
		d := e.detail
		if hasLabel(d.Labels, LabelDemo) {
			need := b.need(PosternNeedDemo, e, firstKnown(d.Created, d.Updated), viewSummary(d.Description))
			b.markNotReady(&need, e, false)
			needs = append(needs, need)
		}
		if !d.IsEpic && d.Exhausted {
			alarm := b.need(PosternNeedAlarm, e, firstKnown(d.Updated, d.Created),
				fmt.Sprintf("used all %d attempts", d.Attempts))
			alarm.WaitsFor = PosternWaitsMayor
			needs = append(needs, alarm)
		}
	}
	return needs, newMemory, nil
}

// viewLandingCheckedMarker is the words the Mayor's comment on a landing he
// has checked opens with.
const viewLandingCheckedMarker = "Landing checked"

// viewCheckLimit is the most runes of the Mayor's check a verify need quotes.
const viewCheckLimit = 200

// viewMayorChecksLanding is what a landing with no HOW TO CHECK IT waits on.
const viewMayorChecksLanding = "the Mayor to check the landing"

// verifyText is the words of a verify need on the landed story d: howTo, the
// steps its closing comment gives the Governor, when it has any; else that the
// Mayor has to check the landing first, and when the need clears by itself.
func verifyText(d StoryDetail, howTo string) string {
	if howTo != "" {
		return howTo
	}
	return fmt.Sprintf("Landed %s: %s. Waiting on %s; clears by itself %s.",
		viewClock(d.ClosedAt), strings.TrimRight(d.Story.Title, ". "), viewMayorChecksLanding,
		viewClock(d.ClosedAt.Add(PosternViewVerifyWindow)))
}

// landedHowTo is the HOW TO CHECK IT of the landed story mem remembers: the
// section itself, or for a memory written before it was kept, the section as
// far as the remembered comments hold it; "" when there is none.
func landedHowTo(mem posternSnapshotMemoryEntry) string {
	if mem.HowTo != "" {
		return mem.HowTo
	}
	comments := make([]Comment, 0, len(mem.Comments))
	for i := len(mem.Comments) - 1; i >= 0; i-- {
		comments = append(comments, Comment{Text: mem.Comments[i].Text})
	}
	return howToCheck(comments)
}

// viewHowToMarker is the words of the section of a closing comment that tells
// the Governor how to check what landed.
const viewHowToMarker = "HOW TO CHECK IT"

// viewHowToLimit is the most runes of that section a verify need carries.
const viewHowToLimit = 1500

// viewForTheGovernor is the words a HOW TO CHECK IT heading may go on with.
var viewForTheGovernor = regexp.MustCompile(`(?i)^[ \t]*,?[ \t]*for the governor`)

// howToCheck is the section after HOW TO CHECK IT in the newest of comments
// (oldest first) that carries those words, up to the next heading or
// viewHowToLimit runes; "" when no comment does or the words are all it says.
// The heading may go on ", for the Governor:" before the steps start. Markdown
// image links stay in it, so a published screenshot shows on the card.
func howToCheck(comments []Comment) string {
	for i := len(comments) - 1; i >= 0; i-- {
		_, after, found := strings.Cut(comments[i].Text, viewHowToMarker)
		if !found {
			continue
		}
		after = viewForTheGovernor.ReplaceAllString(after, "")
		after = strings.TrimLeft(after, " \t\r\n:;,.-\u2013\u2014")
		var kept []string
		for j, line := range strings.Split(after, "\n") {
			if j > 0 && isHeadingLine(line) {
				break
			}
			kept = append(kept, line)
		}
		section := strings.TrimSpace(strings.Join(kept, "\n"))
		if r := []rune(section); len(r) > viewHowToLimit {
			section = strings.TrimSpace(string(r[:viewHowToLimit])) + "…"
		}
		if section != "" {
			return section
		}
	}
	return ""
}

// isHeadingLine reports whether line opens a new section of a comment: a
// Markdown heading, the "For the rig memory:" line a closing comment ends
// with, or a line of capital letters alone.
func isHeadingLine(line string) bool {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "For the rig memory") {
		return true
	}
	letters := 0
	for _, r := range line {
		switch {
		case unicode.IsLower(r), unicode.IsDigit(r):
			return false
		case unicode.IsUpper(r):
			letters++
		}
	}
	return letters >= 4
}

// viewClock is t as a phone card writes a moment, in UTC.
func viewClock(t time.Time) string { return t.UTC().Format("2 Jan 15:04 UTC") }

// landingCheck is the first sentence after "Landing checked" in the newest of
// comments (oldest first) that carries those words, without its closing
// full stop and cut to viewCheckLimit runes; "" when no comment carries them
// or the words are all it says.
func landingCheck(comments []Comment) string {
	for i := len(comments) - 1; i >= 0; i-- {
		_, after, found := strings.Cut(comments[i].Text, viewLandingCheckedMarker)
		if !found {
			continue
		}
		after = strings.TrimLeft(after, " \t\r\n:;,.-\u2013\u2014")
		end := len(after)
		for j := 0; j < len(after); j++ {
			if after[j] == '\n' || (after[j] == '.' && (j+1 == len(after) || strings.ContainsRune(" \t\r\n", rune(after[j+1])))) {
				end = j
				break
			}
		}
		sentence := strings.Join(strings.Fields(after[:end]), " ")
		if len([]rune(sentence)) > viewCheckLimit {
			sentence = string([]rune(sentence)[:viewCheckLimit]) + "…"
		}
		return sentence
	}
	return ""
}

// approve is the approve need of a live epic with stories held for the
// Governor's word, if it has any: bead and epic are the epic itself, since
// is when the oldest of them was filed, and it blocks the held stories and
// everything waiting on them.
func (b *viewBuild) approve(e *viewEntry) (PosternViewNeed, int, bool) {
	epic, read := b.epics[e.detail.Story.ID]
	if !read {
		return PosternViewNeed{}, 0, false
	}
	var held []string
	var since time.Time
	for _, child := range epic.Stories {
		if child.IsEpic || !child.Held() {
			continue
		}
		held = append(held, child.Story.ID)
		if !child.Created.IsZero() && (since.IsZero() || child.Created.Before(since)) {
			since = child.Created
		}
	}
	if len(held) == 0 {
		return PosternViewNeed{}, 0, false
	}
	if since.IsZero() {
		since = firstKnown(e.detail.Updated, e.detail.Created)
	}
	text := fmt.Sprintf("%d held stories wait for your word", len(held))
	if len(held) == 1 {
		text = "1 held story waits for your word"
	}
	need := b.need(PosternNeedApprove, e, since, text)
	need.Options = []string{"Release"}
	need.Blocks = b.blocks(true, held...)
	return need, len(held), true
}

// waitingNeed is an approve or hands need on entry, with the window it may
// wait in before it is a stale one and the fact of what it waits on that the
// stale need states.
type waitingNeed struct {
	need   PosternViewNeed
	entry  *viewEntry
	window time.Duration
	fact   string
}

// since is when the need began to wait, the zero time when unknown.
func (w waitingNeed) since() time.Time {
	at, _ := time.Parse(time.RFC3339, w.need.Since)
	return at
}

// stale reports whether the need has waited past its window at now.
func (w waitingNeed) stale(now time.Time) bool {
	since := w.since()
	return !since.IsZero() && now.Sub(since) > w.window
}

// staleNeed is the one stale need that stands in for w's approve or hands
// need: since is when it went stale, and the text only states facts — what it
// was, its title, what it waits on, its age and the newest of comments (oldest
// first).
func (w waitingNeed) staleNeed(b *viewBuild, comments []Comment) PosternViewNeed {
	d := w.entry.detail
	newest := "no comments"
	if len(comments) > 0 {
		last := comments[len(comments)-1]
		text := strings.Join(strings.Fields(last.Text), " ")
		if r := []rune(text); len(r) > PosternViewStaleQuote {
			text = string(r[:PosternViewStaleQuote])
		}
		newest = "newest comment: " + text
		if !last.Created.IsZero() {
			newest = "newest comment " + last.Created.UTC().Format("2 Jan 2006") + ": " + text
		}
	}
	since := w.since()
	need := b.need(PosternNeedStale, w.entry, since.Add(w.window),
		fmt.Sprintf("%s: %s; %s; waiting %s; %s", w.need.Kind, d.Story.Title, w.fact, viewAge(b.now.Sub(since)), newest))
	need.Options = []string{"Keep", "Close"}
	need.Blocks = w.need.Blocks
	return need
}

// PosternViewStaleQuote is the most runes of a bead's newest comment a stale
// need quotes: §11.
const PosternViewStaleQuote = 200

// viewAge is d as whole days, the unit a stale need waits in.
func viewAge(d time.Duration) string {
	days := int(d / (24 * time.Hour))
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// heldFact is what an approve need waits on, n held stories.
func heldFact(n int) string {
	if n == 1 {
		return "1 held story"
	}
	return fmt.Sprintf("%d held stories", n)
}

// handsFact is what a hands need waits on: the steps that have not run.
func handsFact(steps []PosternViewHandsStep) string {
	var notRun []string
	for _, step := range steps {
		if step.Ran == nil {
			notRun = append(notRun, step.ID)
		}
	}
	switch {
	case len(steps) == 0:
		return "no step filed"
	case len(notRun) == 0:
		return "every step run"
	case len(notRun) == 1:
		return "step " + notRun[0] + " not run"
	}
	return "steps " + strings.Join(notRun, ", ") + " not run"
}

// kept reports whether the Governor has kept e's bead past now: its
// postern.keep note holds a time still ahead, which hides the stale, approve
// and hands needs it would make. A note that is not a time keeps nothing.
func (b *viewBuild) kept(e *viewEntry, notes map[string]string) bool {
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(notes[PosternKeepKey(e.detail.Story.ID)]))
	return err == nil && until.After(b.now)
}

// need is a need of kind on e's bead: its epic (the bead itself when it is an
// epic, else its parent), its title, since, text and what it blocks.
func (b *viewBuild) need(kind string, e *viewEntry, since time.Time, text string) PosternViewNeed {
	d := e.detail
	epic := d.EpicID
	if d.IsEpic {
		epic = d.Story.ID
	}
	return PosternViewNeed{
		Kind: kind, Bead: d.Story.ID, Epic: epic, Title: d.Story.Title,
		Since: formatOrEmpty(since), Text: text, Options: []string{},
		Blocks: b.blocks(false, d.Story.ID), WaitsFor: PosternWaitsYou,
	}
}

// viewByHandMarker is the words a comment opens with when it holds the
// Governor's own instructions for a hands bead that has no steps.
const viewByHandMarker = "BY HAND"

// byHandInstructions is what the newest of comments (oldest first) that opens
// with BY HAND says after those words, without the colon or dash that follows
// them; "" when no comment does or it says nothing more.
func byHandInstructions(comments []Comment) string {
	for i := len(comments) - 1; i >= 0; i-- {
		text := strings.TrimSpace(comments[i].Text)
		if len(text) < len(viewByHandMarker) || !strings.EqualFold(text[:len(viewByHandMarker)], viewByHandMarker) {
			continue
		}
		text = strings.Trim(text[len(viewByHandMarker):], " \t\r\n:-–—")
		if text != "" {
			return text
		}
	}
	return ""
}

// uniqueStrings is ids without repeats, in first-seen order.
func uniqueStrings(ids []string) []string {
	seen := map[string]bool{}
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// viewMayorWritesSteps is what a hands need with no steps yet waits on.
const viewMayorWritesSteps = "the Mayor to write the steps"

// markNotReady marks need, a hands or demo need on e, not ready when e waits
// on an open bead or, with noSteps, has no hands steps yet: WaitingOn names the
// blockers' titles (their ids when a title is empty), then the Mayor writing
// the steps, and the text says so instead of the bead's summary. It waits for
// the factory while a blocker is open, else for the Mayor.
func (b *viewBuild) markNotReady(need *PosternViewNeed, e *viewEntry, noSteps bool) {
	var waiting []string
	for _, w := range e.waits {
		blocker := b.outside[w]
		if in, ok := b.entries[w]; ok {
			blocker = in.detail
		}
		title := strings.TrimSpace(blocker.Story.Title)
		if title == "" {
			title = w
		}
		waiting = append(waiting, title)
	}
	if noSteps {
		waiting = append(waiting, viewMayorWritesSteps)
	}
	if len(waiting) == 0 {
		return
	}
	need.NotReady = true
	need.WaitsFor = PosternWaitsFactory
	if len(e.waits) == 0 {
		need.WaitsFor = PosternWaitsMayor
	}
	need.WaitingOn = waiting
	need.Text = "Not ready yet: waiting on " + strings.Join(waiting, ", ")
}

// hosts lists every host that has recorded a last sync, and this host, in
// name order, and the alarm need of each whose last sync is older than
// PosternViewHostStale while work is pathed to it — in progress, or open
// and waiting on nothing. A host with nothing pathed to it strands nothing,
// and a host asleep with no work is not an alarm; the same rule the quiet
// alarm keeps (Nudge).
func (v PosternView) hosts(b *viewBuild, notes map[string]string) ([]PosternViewHost, []PosternViewNeed) {
	lastSync := map[string]string{}
	if v.Host != "" {
		lastSync[v.Host] = ""
	}
	for key, value := range notes {
		name, ok := strings.CutPrefix(key, "host.")
		if !ok {
			continue
		}
		if name, ok = strings.CutSuffix(name, ".last_sync"); ok && name != "" {
			lastSync[name] = strings.TrimSpace(value)
		}
	}
	names := make([]string, 0, len(lastSync))
	for name := range lastSync {
		names = append(names, name)
	}
	sort.Strings(names)

	working := map[string]bool{}
	for _, id := range b.order {
		e := b.entries[id]
		d := e.detail
		if d.IsEpic || !b.inView(id) {
			continue
		}
		inProgress := strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress)
		frontier := strings.EqualFold(strings.TrimSpace(d.Status), StatusOpen) && len(e.waits) == 0
		if host := d.Merged().Host; host != "" && (inProgress || frontier) {
			working[host] = true
		}
	}

	hosts := make([]PosternViewHost, 0, len(names))
	var alarms []PosternViewNeed
	for _, name := range names {
		at, err := time.Parse(LastSyncFormat, lastSync[name])
		if err != nil {
			hosts = append(hosts, PosternViewHost{Name: name, LastSync: ""})
			continue
		}
		hosts = append(hosts, PosternViewHost{Name: name, LastSync: formatOrEmpty(at)})
		silent := b.now.Sub(at)
		if silent <= PosternViewHostStale || !working[name] {
			continue
		}
		alarms = append(alarms, PosternViewNeed{
			Kind: PosternNeedAlarm, Title: fmt.Sprintf("%s has not synced for %d min", name, minutes(silent)),
			Since: formatOrEmpty(at), Text: fmt.Sprintf("%s last synced %s, with work pathed to it", name, formatOrEmpty(at)),
			Options: []string{}, WaitsFor: PosternWaitsFactory,
		})
	}
	return hosts, alarms
}

// sortPosternNeeds orders needs most blocking first, then oldest — a need
// whose since is unknown after every one whose since is known — then by kind
// and bead, so the view comes out the same every run.
func sortPosternNeeds(needs []PosternViewNeed) {
	sort.SliceStable(needs, func(i, j int) bool {
		a, c := needs[i], needs[j]
		if a.Blocks != c.Blocks {
			return a.Blocks > c.Blocks
		}
		if a.Since != c.Since {
			if a.Since == "" || c.Since == "" {
				return c.Since == ""
			}
			return a.Since < c.Since
		}
		if posternNeedRank[a.Kind] != posternNeedRank[c.Kind] {
			return posternNeedRank[a.Kind] < posternNeedRank[c.Kind]
		}
		return a.Bead < c.Bead
	})
}

// viewBeadOf is e as §11 writes a bead.
func viewBeadOf(e *viewEntry) PosternViewBead {
	d := e.detail
	kind := strings.TrimSpace(d.Type)
	if kind == "" {
		kind = "task"
		if d.IsEpic {
			kind = "epic"
		}
	}
	labels := append([]string{}, d.Labels...)
	return PosternViewBead{
		ID: d.Story.ID, Title: d.Story.Title, Type: kind, Status: d.Status, Priority: d.Priority,
		Parent: d.EpicID, Labels: labels, Assignee: d.Assignee, Waits: e.waits,
		Created: formatOrEmpty(d.Created), Updated: formatOrEmpty(d.Updated),
		Started: formatOrEmpty(d.Started), Closed: formatOrEmpty(d.ClosedAt),
		Path: e.path, Attempts: d.Attempts, Summary: viewSummary(d.Description),
		Comments: d.CommentCount, DoneEarlier: e.doneEarlier,
	}
}

// viewHandsSteps is bead's hands steps from notes, each with its hash and
// its last run: nil for a bead with none, or with a note that does not read
// as steps.
func viewHandsSteps(bead string, notes map[string]string) []PosternViewHandsStep {
	records, err := parseHandsSteps(notes[HandsStepsKey(bead)])
	if err != nil || len(records) == 0 {
		return nil
	}
	steps := make([]PosternViewHandsStep, 0, len(records))
	for _, record := range records {
		step := PosternViewHandsStep{HandsStep: record.HandsStep, SHA256: domain.HandsSHA256(bead, record.HandsStep)}
		if ran, ok := parseHandsRan(notes[HandsRanKey(bead, record.ID)]); ok {
			step.Ran = &ran
		}
		steps = append(steps, step)
	}
	return steps
}

// allStepsRanClean reports whether steps is not empty and every one has a
// last run with exit 0: the bead then waits on the Mayor's acceptance check,
// not on the Governor's hands. A failed step, one not yet run, or no steps at
// all leaves the hands need standing.
func allStepsRanClean(steps []PosternViewHandsStep) bool {
	if len(steps) == 0 {
		return false
	}
	for _, step := range steps {
		if step.Ran == nil || step.Ran.Exit != 0 {
			return false
		}
	}
	return true
}

// workable reports whether a bead is open or in progress: neither held nor
// finished, so a demo or a pair of hands it asks for is asked now.
func workable(d StoryDetail) bool {
	status := strings.ToLower(strings.TrimSpace(d.Status))
	return status == StatusOpen || status == StatusInProgress
}

// hasLabel reports whether labels carries label, as the tracker spells it or
// in any other case.
func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if strings.EqualFold(strings.TrimSpace(l), label) {
			return true
		}
	}
	return false
}

// firstKnown is the first of times that is not the zero time, or the zero
// time.
func firstKnown(times ...time.Time) time.Time {
	for _, t := range times {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// viewPathOf is path as §11 writes it: every field, empty where unknown.
func viewPathOf(path domain.Path) *PosternViewPath {
	return &PosternViewPath{
		Rig: path.Rig, Branch: path.Branch, Host: path.Host, Model: string(path.Model),
		Effort: string(path.Effort), Formula: path.Formula, Harness: string(path.Harness),
	}
}

// viewPathOrNil is an epic's defaults as §11 writes them: absent when it has
// none.
func viewPathOrNil(path domain.Path) *PosternViewPath {
	if path == (domain.Path{}) {
		return nil
	}
	return viewPathOf(path)
}

// Markdown a summary drops, being plain text: a link keeps its words, an
// image its alt text; emphasis, code ticks, heading and quote marks, and
// list bullets go.
var (
	markdownImage    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	markdownLink     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markdownLineMark = regexp.MustCompile(`(?m)^\s*(?:#{1,6}\s+|>\s?|[-*+]\s+|\d+[.)]\s+)`)
	markdownEmphasis = regexp.MustCompile("(\\*\\*|__|\\*|`)")
	whitespaceRun    = regexp.MustCompile(`\s+`)
)

// viewSummary is description as §11's summary: plain text, its whitespace
// run together, cut to PosternViewSummaryLimit runes with a trailing marker
// when cut. The full description comes from §12.
func viewSummary(description string) string {
	text := markdownImage.ReplaceAllString(description, "$1")
	text = markdownLink.ReplaceAllString(text, "$1")
	text = markdownLineMark.ReplaceAllString(text, "")
	text = markdownEmphasis.ReplaceAllString(text, "")
	text = strings.TrimSpace(whitespaceRun.ReplaceAllString(text, " "))
	return clippedTo(text, PosternViewSummaryLimit)
}

// now is the clock Build and Run use.
func (v PosternView) now() time.Time {
	if v.Now == nil {
		return time.Now()
	}
	return v.Now()
}

// SealPosternDoc is doc as postern's docs/protocol.md §11 and §12 carry it:
// its JSON, gzipped (RFC 1952), encrypted with BRC-78 to toPubKey, base64 —
// base64(EncryptedMessage.encrypt(gzip(utf8(JSON)), mayorKey, governorKey)).
// It reports the sealed text and the JSON's and the gzip's sizes, for a line
// a person reads.
func SealPosternDoc(cipher Cipher, toPubKey string, doc any) (sealed string, plainBytes, gzipBytes int, err error) {
	plain, err := json.Marshal(doc)
	if err != nil {
		return "", 0, 0, fmt.Errorf("building the JSON: %w", err)
	}
	var zipped bytes.Buffer
	gz := gzip.NewWriter(&zipped)
	if _, err := gz.Write(plain); err != nil {
		return "", 0, 0, fmt.Errorf("gzipping the JSON: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", 0, 0, fmt.Errorf("gzipping the JSON: %w", err)
	}
	sealed, err = cipher.EncryptBytes(toPubKey, zipped.Bytes())
	if err != nil {
		return "", 0, 0, err
	}
	return sealed, len(plain), zipped.Len(), nil
}

// OpenPosternDoc reads sealed back as its reader would, §11: decrypt it with
// privKey, inflate it when it starts with the gzip magic 1f 8b (read it as
// JSON as it stands otherwise), and decode the JSON into into.
func OpenPosternDoc(cipher Cipher, privKey, sealed string, into any) error {
	text, _, err := cipher.Decrypt(privKey, strings.TrimSpace(sealed))
	if err != nil {
		return err
	}
	raw := []byte(text)
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("inflating: %w", err)
		}
		if raw, err = io.ReadAll(gz); err != nil {
			return fmt.Errorf("inflating: %w", err)
		}
	}
	return json.Unmarshal(raw, into)
}

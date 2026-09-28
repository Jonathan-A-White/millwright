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
	PosternNeedAlarm    = "alarm"
)

// LabelDemo is the label on a bead the Governor is to be shown working: a
// demo need while it is open.
const LabelDemo = "demo"

// posternNeedRank orders needs of different kinds that tie on blocks and
// since, so the view comes out the same every run.
var posternNeedRank = map[string]int{
	PosternNeedAlarm: 0, PosternNeedQuestion: 1, PosternNeedApprove: 2,
	PosternNeedHands: 3, PosternNeedVerify: 4, PosternNeedDemo: 5,
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
		return PosternViewDoc{}, err
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
		verifies = append(verifies, e)
		if d.CommentCount == 0 {
			continue
		}
		if mem, known := memory[id]; known && mem.Count == d.CommentCount {
			continue
		}
		needComments = append(needComments, id)
	}

	comments := map[string][]Comment{}
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
		need := b.need(PosternNeedVerify, e, e.detail.ClosedAt, viewSummary(e.detail.Description))
		need.Options = []string{"Verified"}
		needs = append(needs, need)
	}

	for _, id := range b.order {
		e := b.entries[id]
		if !b.inView(id) {
			continue
		}
		d := e.detail
		if b.live[id] {
			if need, ok := b.approve(e); ok {
				needs = append(needs, need)
			}
		}
		if !workable(d) {
			continue
		}
		since := firstKnown(d.Created, d.Updated)
		if hasLabel(d.Labels, LabelDemo) {
			needs = append(needs, b.need(PosternNeedDemo, e, since, viewSummary(d.Description)))
		}
		if hasLabel(d.Labels, LabelHitl) {
			needs = append(needs, b.need(PosternNeedHands, e, since, viewSummary(d.Description)))
		}
		if !d.IsEpic && d.Exhausted {
			needs = append(needs, b.need(PosternNeedAlarm, e, firstKnown(d.Updated, d.Created),
				fmt.Sprintf("used all %d attempts", d.Attempts)))
		}
	}
	return needs, newMemory, nil
}

// approve is the approve need of a live epic with stories held for the
// Governor's word, if it has any: bead and epic are the epic itself, since
// is when the oldest of them was filed, and it blocks the held stories and
// everything waiting on them.
func (b *viewBuild) approve(e *viewEntry) (PosternViewNeed, bool) {
	epic, read := b.epics[e.detail.Story.ID]
	if !read {
		return PosternViewNeed{}, false
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
		return PosternViewNeed{}, false
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
	return need, true
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
		Blocks: b.blocks(false, d.Story.ID),
	}
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
			Options: []string{},
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

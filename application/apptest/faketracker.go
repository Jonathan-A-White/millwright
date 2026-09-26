// Package apptest holds in-memory stand-ins for the application's ports, so
// that a use case can be tested without a beads database, a git worktree or a
// harness. It is test support that ships in the module: any package's tests
// may import it.
package apptest

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// Statuses a story in the fake tracker can be in. They are the tracker's own
// names, which are beads' own names.
const (
	StatusOpen       = application.StatusOpen
	StatusInProgress = application.StatusInProgress
	StatusDeferred   = application.StatusHeld
	StatusClosed     = application.StatusClosed
)

// Actor is the assignee the fake records when a story is claimed.
const Actor = "fake"

// LeaseTTL is how long a claim's lease runs in the fake, from the claim or the
// last heartbeat: five minutes, as bd 1.3.0 grants it and does not let be set.
const LeaseTTL = 5 * time.Minute

// FakeTracker is an in-memory application.WorkTracker. Stories are listed in
// the order they were added, so a test can assert on the whole list.
//
// A story it holds is ready when it is open (neither held nor claimed nor
// closed), under the epic asked about, pointed at the host asked about, and
// every story it waits on is closed — which is what beads does too.
type FakeTracker struct {
	mu sync.Mutex

	defaults map[string]domain.Path // epic id -> its default path
	titles   map[string]string      // epic id -> what it is called
	epicSays map[string]epicFacts   // epic id -> its status and priority, when set
	epicSaid map[string][]string    // epic id -> the comments on it, oldest first
	stories  map[string]*fakeStory
	order    []string
	epics    []string

	formulas    map[string][]application.FormulaStep // formula name -> its steps
	molecules   []application.Molecule
	poured      map[string]string // story id -> the molecule poured for it
	closedSteps map[string]bool   // step id -> closed by the session working it
	closedRoots map[string]bool   // molecule root id -> closed

	// refused is the stories CloseStory turns down, by the reason it gives.
	refused map[string]string

	// writes counts the calls that changed something, so that a test can say a
	// reading wrote nothing.
	writes int

	notes map[string]string
	// published is the notes as the last sync that got through left them: what
	// the other host reads on its next sync. A note set after that sync is in
	// notes and not yet here.
	published map[string]string
	syncs     int
	// gcs counts the calls to GC, so a test can say whether one happened.
	gcs int
	// repacks counts the calls to GC that a real Gateway would also have
	// repacked the Dolt git-remote-cache on, mirroring gcs: the fake reclaims
	// nothing on disk, but a test still wants to say that a due GC is the one
	// call that carries the repack too, and a GC that never ran carries
	// neither.
	repacks int
	// packChecks counts every successful call to Sync, mirroring the check a
	// real Gateway now also makes after every `bd sync`: whether a
	// git-remote-cache has grown past the repack threshold. Unlike repacks,
	// this runs on every sync, the daily GC's own cadence or not — the fake
	// holds no cache to crowd, but a test still wants to say the question was
	// asked.
	packChecks int
	// GCErr, when set, is what GC reports instead of collecting.
	GCErr error
	// size is what Size reports.
	size int64
	// asked is the dispatch-facing calls in the order they were made, so that a
	// test can say the hosts were levelled before anything was claimed.
	asked []string

	// commentReads counts each call to StoryComments, by story id, so that a
	// test can say a story's comments were never read.
	commentReads map[string]int

	// showEpicsCalls counts each call to ShowEpics, and storiesCommentsCalls
	// each call to StoriesComments, so that a test can say a batch of epics or
	// stories was read in one tracker call rather than one per epic or story.
	showEpicsCalls       int
	storiesCommentsCalls int

	// failing is the methods FailOn makes fail, by the error each gives.
	failing map[string]error

	// Clock is what a claim's lease is counted from. A nil Clock reads the
	// real one.
	Clock func() time.Time

	// Err, when set, is returned by every method instead of doing the work.
	Err error
	// SyncErr, when set, is what Sync reports instead of synchronising. Use
	// SyncExits to make it the halt a beads exit code stands for.
	SyncErr error
	// syncErrOnce, when set, is what the very next Sync call reports, after
	// which it clears itself; set with SyncExitsOnce. It is checked before
	// SyncErr, so a test can chain "fails once, then fails a different way"
	// by setting both.
	syncErrOnce error
}

// epicFacts is what DescribeEpic gave an epic beyond its title.
type epicFacts struct {
	status   string
	priority int
}

// fakeStory is one story as the fake remembers it.
type fakeStory struct {
	detail      application.StoryDetail
	metadata    map[string]string
	states      map[string]string
	reasons     map[string]string // dimension -> why it was last set
	needs       []string
	comments    []string
	closeReason string
}

// NewFakeTracker returns an empty fake work tracker.
func NewFakeTracker() *FakeTracker {
	return &FakeTracker{
		defaults:  map[string]domain.Path{},
		titles:    map[string]string{},
		epicSays:  map[string]epicFacts{},
		epicSaid:  map[string][]string{},
		stories:   map[string]*fakeStory{},
		formulas:  map[string][]application.FormulaStep{},
		poured:    map[string]string{},
		notes:     map[string]string{},
		published: map[string]string{},
	}
}

// AddEpic records an epic and the default Path its stories inherit.
func (f *FakeTracker) AddEpic(id string, defaults domain.Path) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, seen := f.defaults[id]; !seen {
		f.epics = append(f.epics, id)
	}
	f.defaults[id] = defaults
}

// DescribeEpic gives an epic the title, status and priority a reading of it
// reports. An epic never described reads as untitled, open and at
// application.DefaultPriority.
func (f *FakeTracker) DescribeEpic(id, title, status string, priority int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.titles[id] = title
	f.epicSays[id] = epicFacts{status: status, priority: priority}
}

// AddEpicComment records a comment already left on an epic, after those before
// it. It is a fixture, not a write: Writes does not count it.
func (f *FakeTracker) AddEpicComment(id, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.epicSaid[id] = append(f.epicSaid[id], text)
}

// AddStory records an open, unclaimed story under an epic. Its Overrides are
// the fields of the epic's default Path it departs from.
func (f *FakeTracker) AddStory(epicID string, story domain.Story) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, seen := f.stories[story.ID]; !seen {
		f.order = append(f.order, story.ID)
	}
	f.stories[story.ID] = &fakeStory{
		detail: application.StoryDetail{
			Story:    story,
			Defaults: f.defaults[epicID],
			EpicID:   epicID,
			Status:   StatusOpen,
			Priority: application.DefaultPriority,
		},
		metadata: map[string]string{},
	}
}

// AddChildEpic records an epic filed under another epic, as a listing of the
// parent's children returns it: among its stories, marked as an epic. It can be
// shown in its own right, with the defaults of its parent.
func (f *FakeTracker) AddChildEpic(parentID, id, title string) {
	f.AddStory(parentID, domain.Story{ID: id, Title: title})
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stories[id].detail.IsEpic = true
	if _, seen := f.defaults[id]; !seen {
		f.epics = append(f.epics, id)
	}
	f.defaults[id] = f.defaults[parentID]
	f.titles[id] = title
}

// SetPriority sets the priority of a story the fake holds, 0 being the most
// urgent. A story added without one has application.DefaultPriority.
func (f *FakeTracker) SetPriority(id string, priority int) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Priority = priority
		return nil
	})
}

// SetStatus sets the status of a story the fake holds, whatever it was: how a
// fixture makes a story held, in progress or closed without walking it there.
func (f *FakeTracker) SetStatus(id, status string) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Status = status
		return nil
	})
}

// SetCreated sets when a story the fake holds was filed. A story added without
// one has no creation time, which a dispatch reads as no age to order it by.
func (f *FakeTracker) SetCreated(id string, created time.Time) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Created = created
		return nil
	})
}

// SetStarted sets when a story the fake holds was claimed. ClaimStory leaves it
// alone: a scenario that wants a claim from long ago says so here.
func (f *FakeTracker) SetStarted(id string, started time.Time) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Started = started
		return nil
	})
}

// SetLeaseExpires sets when a story the fake holds's claim lease expires. A
// story added without one has no lease, which a dispatch never reads as
// expired (mw-gq6.106).
func (f *FakeTracker) SetLeaseExpires(id string, expires time.Time) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.LeaseExpires = expires
		return nil
	})
}

// SetUpdated sets when a story the fake holds was last changed. A story added
// without one has no updated time.
func (f *FakeTracker) SetUpdated(id string, updated time.Time) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Updated = updated
		return nil
	})
}

// SetClosedAt sets when a story the fake holds was closed. CloseStory leaves
// it alone: a scenario that wants a landing from days ago says so here.
func (f *FakeTracker) SetClosedAt(id string, closed time.Time) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.ClosedAt = closed
		return nil
	})
}

// SetLabels sets the labels of a story the fake holds, replacing any it had. A
// story added without any has none.
func (f *FakeTracker) SetLabels(id string, labels ...string) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Labels = append([]string(nil), labels...)
		return nil
	})
}

// CreateEpic implements application.WorkTracker. The fake mints ids the way
// beads does — f-1 for an epic, f-1.1 for its first story — so that a test can
// read a tree without knowing them in advance.
func (f *FakeTracker) CreateEpic(_ context.Context, epic application.NewEpic) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if epic.Title == "" {
		return "", fmt.Errorf("an epic needs a title")
	}
	f.writes++
	id := fmt.Sprintf("f-%d", len(f.epics)+1)
	f.epics = append(f.epics, id)
	f.defaults[id] = epic.Defaults
	f.titles[id] = epic.Title
	return id, nil
}

// CreateStory implements application.WorkTracker. The story is filed held, and
// stays that way until it is released.
func (f *FakeTracker) CreateStory(_ context.Context, story application.NewStory) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	switch {
	case story.Title == "":
		return "", fmt.Errorf("a story needs a title")
	case story.EpicID == "":
		return "", fmt.Errorf("a story needs an epic to be filed under")
	}
	for _, need := range story.Needs {
		if _, filed := f.stories[need]; !filed {
			return "", fmt.Errorf("story %q waits on %q, which is not filed", story.Title, need)
		}
	}

	f.writes++
	under := 0
	for _, id := range f.order {
		if f.stories[id].detail.EpicID == story.EpicID {
			under++
		}
	}
	id := fmt.Sprintf("%s.%d", story.EpicID, under+1)

	f.order = append(f.order, id)
	f.stories[id] = &fakeStory{
		detail: application.StoryDetail{
			Story: domain.Story{
				ID:        id,
				Title:     story.Title,
				Overrides: story.Overrides,
			},
			Defaults:        f.defaults[story.EpicID],
			EpicID:          story.EpicID,
			Status:          StatusDeferred,
			Priority:        priorityOr(story.Priority),
			Description:     story.Description,
			Acceptance:      story.Acceptance,
			EstimateMinutes: story.EstimateMinutes,
		},
		metadata: story.Overrides.Metadata(),
		needs:    append([]string(nil), story.Needs...),
	}
	return id, nil
}

// ReleaseStory implements application.WorkTracker.
func (f *FakeTracker) ReleaseStory(_ context.Context, id string) error {
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Status == StatusDeferred {
			s.detail.Status = StatusOpen
		}
		return nil
	})
}

// Epics reports the ids of the epics filed, in the order they were filed.
func (f *FakeTracker) Epics() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.epics...)
}

// Stories reports the ids of every story the fake holds, in the order they
// arrived — so that a test can say that nothing at all was written.
func (f *FakeTracker) Stories() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
}

// ClaimAs claims a story for another assignee than the fake's own Actor, with
// a lease until the time given: the claim another host's dispatcher made.
func (f *FakeTracker) ClaimAs(id, holder string, until time.Time) error {
	return f.write(id, func(s *fakeStory) error {
		s.detail.Assignee = holder
		s.detail.Status = StatusInProgress
		s.detail.LeaseExpires = until
		return nil
	})
}

// Needs sets the ids of the stories a story waits on, as if they had been
// named when it was filed. It is for a fixture that adds a story with
// AddStory and gives it a dependency afterwards, rather than filing it
// through CreateStory.
func (f *FakeTracker) Needs(id string, needs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stories[id]; ok {
		s.needs = append([]string(nil), needs...)
	}
}

// Comments reports the comments left on a story, oldest first.
func (f *FakeTracker) Comments(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.stories[id]
	if !ok {
		return nil
	}
	return append([]string(nil), s.comments...)
}

// CommentReads reports how many times StoryComments was called for id, so
// that a test can say a story's comments were never read.
func (f *FakeTracker) CommentReads(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commentReads[id]
}

// Writes reports how many calls changed something — a story or epic filed, a
// story written to, a note set or cleared — so that a test can say that a
// reading wrote nothing.
func (f *FakeTracker) Writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// StoryComments implements application.WorkTracker. The fake keeps only what a
// comment said, so each comes back from the actor, without a time.
func (f *FakeTracker) StoryComments(_ context.Context, id string) ([]application.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.commentReads == nil {
		f.commentReads = map[string]int{}
	}
	f.commentReads[id]++
	if f.Err != nil {
		return nil, f.Err
	}
	texts := f.epicSaid[id]
	if s, ok := f.stories[id]; ok {
		texts = s.comments
	} else if _, epic := f.defaults[id]; !epic {
		return nil, fmt.Errorf("no story %q", id)
	}
	comments := make([]application.Comment, 0, len(texts))
	for _, text := range texts {
		comments = append(comments, application.Comment{Author: Actor, Text: text})
	}
	return comments, nil
}

// StoriesComments implements application.WorkTracker: StoryComments for each
// id, keyed by id. Each call still goes through StoryComments, so
// CommentReads keeps counting them the same way a test written against the
// single-story call already does; the one call to StoriesComments itself is
// counted separately, by StoriesCommentsCalls.
func (f *FakeTracker) StoriesComments(ctx context.Context, ids []string) (map[string][]application.Comment, error) {
	f.mu.Lock()
	f.storiesCommentsCalls++
	f.mu.Unlock()
	out := make(map[string][]application.Comment, len(ids))
	for _, id := range ids {
		comments, err := f.StoryComments(ctx, id)
		if err != nil {
			return nil, err
		}
		out[id] = comments
	}
	return out, nil
}

// CloseReason reports why a story was closed, or "" if it is still open.
func (f *FakeTracker) CloseReason(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stories[id]; ok {
		return s.closeReason
	}
	return ""
}

// Metadata reports the metadata fields written onto a story.
func (f *FakeTracker) Metadata(id string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	if s, ok := f.stories[id]; ok {
		for k, v := range s.metadata {
			out[k] = v
		}
	}
	return out
}

// ShowStory implements application.WorkTracker.
func (f *FakeTracker) ShowStory(_ context.Context, id string) (application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.StoryDetail{}, f.Err
	}
	s, ok := f.stories[id]
	if !ok {
		return application.StoryDetail{}, fmt.Errorf("no story %q", id)
	}
	detail := s.detail
	detail.CommentCount = len(s.comments)
	return detail, nil
}

// ShowEpic implements application.WorkTracker. The stories come back in the
// order they were filed, each carrying what it waits on.
func (f *FakeTracker) ShowEpic(_ context.Context, id string) (application.EpicDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return application.EpicDetail{}, f.Err
	}
	if story, filed := f.stories[id]; filed && !story.detail.IsEpic {
		return application.EpicDetail{}, fmt.Errorf("%s is a story, not an epic", id)
	}
	if _, filed := f.defaults[id]; !filed {
		return application.EpicDetail{}, fmt.Errorf("no epic %q", id)
	}

	epic := application.EpicDetail{ID: id, Title: f.titles[id], Defaults: f.defaults[id],
		Status: StatusOpen, Priority: application.DefaultPriority}
	if says, described := f.epicSays[id]; described {
		epic.Status, epic.Priority = says.status, says.priority
	}
	for _, storyID := range f.order {
		s := f.stories[storyID]
		if s.detail.EpicID != id {
			continue
		}
		detail := s.detail
		detail.Needs = append([]string(nil), s.needs...)
		detail.CommentCount = len(s.comments)
		epic.Stories = append(epic.Stories, detail)
	}
	return epic, nil
}

// ShowEpics implements application.WorkTracker: ShowEpic for each id, in
// order. The fake holds nothing that costs more calls to read together, so it
// simply asks ShowEpic once per id; it counts the one call to ShowEpics
// itself, so a test can say a caller asked for a batch of epics in one
// tracker call rather than one per epic.
func (f *FakeTracker) ShowEpics(ctx context.Context, ids []string) ([]application.EpicDetail, error) {
	f.mu.Lock()
	f.showEpicsCalls++
	f.mu.Unlock()
	out := make([]application.EpicDetail, 0, len(ids))
	for _, id := range ids {
		epic, err := f.ShowEpic(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, epic)
	}
	return out, nil
}

// ShowEpicsCalls reports how many times ShowEpics was called, so that a test
// can say several epics were read in one batch rather than one call each.
func (f *FakeTracker) ShowEpicsCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.showEpicsCalls
}

// StoriesCommentsCalls reports how many times StoriesComments was called, so
// that a test can say several stories' comments were read in one batch
// rather than one call each.
func (f *FakeTracker) StoriesCommentsCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.storiesCommentsCalls
}

// LiveEpics implements application.WorkTracker: the epics added (AddEpic or
// AddChildEpic) or created (CreateEpic), in that order, narrowed to the ones
// open or in progress. An epic never described (DescribeEpic) is open, like
// ShowEpic's own default.
func (f *FakeTracker) LiveEpics(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var live []string
	for _, id := range f.epics {
		status := StatusOpen
		if says, described := f.epicSays[id]; described && says.status != "" {
			status = says.status
		}
		if strings.EqualFold(status, StatusOpen) || strings.EqualFold(status, StatusInProgress) {
			live = append(live, id)
		}
	}
	return live, nil
}

// AddFormula installs a formula in the fake, with the steps pouring it makes.
func (f *FakeTracker) AddFormula(name string, steps ...application.FormulaStep) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.formulas[name] = append([]application.FormulaStep(nil), steps...)
}

// Formulas implements application.WorkTracker.
func (f *FakeTracker) Formulas(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	names := make([]string, 0, len(f.formulas))
	for name := range f.formulas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// PourFormula implements application.WorkTracker. The step beads are named
// after the molecule they belong to, as beads' own are, so that a test can tell
// two pourings of one formula apart.
func (f *FakeTracker) PourFormula(_ context.Context, formula, storyID, title string) (application.Molecule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "PourFormula")
	if f.Err != nil {
		return application.Molecule{}, f.Err
	}
	steps, installed := f.formulas[formula]
	if !installed {
		return application.Molecule{}, fmt.Errorf("no formula %q is installed", formula)
	}

	root := fmt.Sprintf("f-mol-%d", len(f.molecules)+1)
	poured := application.Molecule{Formula: formula, RootID: root}
	for i, step := range steps {
		poured.Steps = append(poured.Steps, application.FormulaStep{
			ID:          fmt.Sprintf("%s.%d", root, i+1),
			Title:       strings.NewReplacer("{{story}}", storyID, "{{title}}", title).Replace(step.Title),
			Description: strings.NewReplacer("{{story}}", storyID, "{{title}}", title).Replace(step.Description),
		})
	}
	f.molecules = append(f.molecules, poured)
	f.poured[storyID] = root
	return poured, nil
}

// Poured reports the molecule poured for a story, and whether one was.
func (f *FakeTracker) Poured(storyID string) (application.Molecule, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	root, ok := f.poured[storyID]
	if !ok {
		return application.Molecule{}, false
	}
	for _, molecule := range f.molecules {
		if molecule.RootID == root {
			return molecule, true
		}
	}
	return application.Molecule{}, false
}

// Molecules reports how many formulas have been poured, so that a test can say
// that nothing was.
func (f *FakeTracker) Molecules() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.molecules)
}

// Asked reports the dispatch-facing calls the fake was made, in order: Sync,
// RunningStories, ReadyForHost, WorkInHand, ClaimStory, ReleaseClaim,
// OpenMolecule, PourFormula, SetStoryState and GC. It is how a test says what was done before
// what.
func (f *FakeTracker) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// ReadyForHost implements application.WorkTracker. Unlike ReadyWithLabel and
// BlockedForHost it does not narrow to what is unblocked: bd's own ready set
// is what decides that in the real tracker, and what it decides is not always
// caught up with a dependency filed moments before (mw-gq6.93) — so a story is
// offered here open, unassigned and pathed to the host, whatever it still
// waits on, with every one of its needs carried on it for Dispatch's own guard
// to read back and judge for itself.
func (f *FakeTracker) ReadyForHost(_ context.Context, host string) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "ReadyForHost")
	if f.Err != nil {
		return nil, f.Err
	}
	if host == "" {
		return nil, fmt.Errorf("which host are the ready stories for?")
	}
	var ready []application.StoryDetail
	for _, id := range f.order {
		s := f.stories[id]
		switch {
		case s.detail.Status != StatusOpen,
			s.detail.Assignee != "",
			s.detail.Merged().Host != host:
			continue
		}
		detail := s.detail
		detail.Needs = append([]string(nil), s.needs...)
		ready = append(ready, detail)
	}
	return ready, nil
}

// RunningStories implements application.WorkTracker.
func (f *FakeTracker) RunningStories(_ context.Context, host string) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "RunningStories")
	if f.Err != nil {
		return nil, f.Err
	}
	var running []application.StoryDetail
	for _, id := range f.order {
		s := f.stories[id]
		if s.detail.Status == StatusInProgress && s.detail.Merged().Host == host {
			running = append(running, s.detail)
		}
	}
	return running, nil
}

// BlockedForHost implements application.WorkTracker.
func (f *FakeTracker) BlockedForHost(_ context.Context, host string) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	if host == "" {
		return nil, fmt.Errorf("which host are the blocked stories for?")
	}
	var blocked []application.StoryDetail
	for _, id := range f.order {
		s := f.stories[id]
		switch {
		case s.detail.Status != StatusOpen,
			s.detail.Assignee != "",
			s.detail.Merged().Host != host,
			!f.waiting(s):
			continue
		}
		detail := s.detail
		// Only what it is still waiting for: a story it waits on that is
		// already finished is no longer a wait, which is what the real tracker
		// says too when it is asked about one story at a time.
		for _, need := range s.needs {
			if blocker, filed := f.stories[need]; !filed || blocker.detail.Status != StatusClosed {
				detail.Needs = append(detail.Needs, need)
			}
		}
		blocked = append(blocked, detail)
	}
	return blocked, nil
}

// ReadyWithLabel implements application.WorkTracker: open, unblocked and
// carrying the label, whatever host — or none — the Path names.
func (f *FakeTracker) ReadyWithLabel(_ context.Context, label string) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "ReadyWithLabel")
	if f.Err != nil {
		return nil, f.Err
	}
	if label == "" {
		return nil, fmt.Errorf("which label are the ready stories carrying?")
	}
	var ready []application.StoryDetail
	for _, id := range f.order {
		s := f.stories[id]
		if s.detail.Status != StatusOpen || f.waiting(s) || !carries(s.detail.Labels, label) {
			continue
		}
		ready = append(ready, s.detail)
	}
	return ready, nil
}

// carries reports whether a story's labels include the one asked for.
func carries(labels []string, label string) bool {
	for _, have := range labels {
		if strings.EqualFold(strings.TrimSpace(have), label) {
			return true
		}
	}
	return false
}

// WorkInHand implements application.WorkTracker: every story ready to be taken
// or claimed and unfinished, whatever host — or none — its Path names.
func (f *FakeTracker) WorkInHand(_ context.Context) (application.WorkInHand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "WorkInHand")
	if f.Err != nil {
		return application.WorkInHand{}, f.Err
	}
	var work application.WorkInHand
	for _, id := range f.order {
		s := f.stories[id]
		switch {
		case s.detail.Status == StatusInProgress:
			work.Running = append(work.Running, s.detail)
		case s.detail.Status == StatusOpen && s.detail.Assignee == "" && !f.waiting(s):
			work.Ready = append(work.Ready, s.detail)
		}
	}
	return work, nil
}

// ReleaseClaim implements application.WorkTracker.
func (f *FakeTracker) ReleaseClaim(_ context.Context, id string) error {
	f.note("ReleaseClaim")
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Status == StatusInProgress {
			s.detail.Status = StatusOpen
		}
		s.detail.Assignee = ""
		s.detail.LeaseExpires = time.Time{}
		return nil
	})
}

// SetStoryState implements application.WorkTracker.
func (f *FakeTracker) SetStoryState(_ context.Context, id, dimension, value, reason string) error {
	f.note("SetStoryState")
	if dimension == "" {
		return fmt.Errorf("a state needs a dimension")
	}
	f.mu.Lock()
	failing := f.failing["SetStoryState"]
	f.mu.Unlock()
	if failing != nil {
		return failing
	}
	return f.write(id, func(s *fakeStory) error {
		if s.states == nil {
			s.states = map[string]string{}
		}
		s.states[dimension] = value
		if s.reasons == nil {
			s.reasons = map[string]string{}
		}
		s.reasons[dimension] = reason
		return nil
	})
}

// StateReason reports why one dimension of a story's operational state was last
// set, or "" when it never was.
func (f *FakeTracker) StateReason(id, dimension string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stories[id]; ok {
		return s.reasons[dimension]
	}
	return ""
}

// State reports one dimension of a story's operational state, or "" when it has
// never been set.
func (f *FakeTracker) State(id, dimension string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.stories[id]; ok {
		return s.states[dimension]
	}
	return ""
}

// note records one dispatch-facing call under the lock.
func (f *FakeTracker) note(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, call)
}

// ReadyStories implements application.WorkTracker.
func (f *FakeTracker) ReadyStories(_ context.Context, epicID, host string) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var ready []application.StoryDetail
	for _, id := range f.order {
		s := f.stories[id]
		switch {
		case s.detail.EpicID != epicID,
			s.detail.Status != StatusOpen,
			s.detail.Assignee != "",
			s.detail.Merged().Host != host,
			f.waiting(s):
			continue
		}
		ready = append(ready, s.detail)
	}
	return ready, nil
}

// ClaimStory implements application.WorkTracker.
func (f *FakeTracker) ClaimStory(_ context.Context, id string) error {
	f.note("ClaimStory")
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Assignee != "" && s.detail.Assignee != Actor {
			return &application.ClaimHeldError{ID: id, Holder: s.detail.Assignee}
		}
		s.detail.Assignee = Actor
		s.detail.Status = StatusInProgress
		s.detail.LeaseExpires = f.now().Add(LeaseTTL)
		return nil
	})
}

// SetStoryMetadata implements application.WorkTracker.
func (f *FakeTracker) SetStoryMetadata(_ context.Context, id string, fields map[string]string) error {
	return f.write(id, func(s *fakeStory) error {
		for k, v := range fields {
			s.metadata[k] = v
			// The molecule a story carries is read back off the story, as beads
			// reads it off the bead's metadata.
			if k == application.MoleculeField {
				s.detail.Molecule.RootID = v
			}
			// So are the attempts, and the mark that the Mayor was told of them.
			if k == application.AttemptsField {
				s.detail.Attempts, _ = strconv.Atoi(v)
			}
			if k == application.AttemptsExhaustedField {
				s.detail.Exhausted = v != ""
			}
			// Path fields are metadata: keep the story's overrides in step.
			_ = s.detail.Story.Overrides.Set(k, v)
		}
		return nil
	})
}

// StoryState implements application.WorkTracker.
func (f *FakeTracker) StoryState(_ context.Context, id, dimension string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if s, ok := f.stories[id]; ok {
		return s.states[dimension], nil
	}
	return "", fmt.Errorf("no story %q", id)
}

// CloseStep closes one step of a poured molecule, as a session does as it works
// its formula.
func (f *FakeTracker) CloseStep(stepID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closedSteps == nil {
		f.closedSteps = map[string]bool{}
	}
	f.closedSteps[stepID] = true
}

// FailOn makes one method, named as Asked names it, fail with err instead of
// doing its work, and leaves every other method alone. A nil err lets it work
// again. Only OpenMolecule and SetStoryState look at it so far.
func (f *FakeTracker) FailOn(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing == nil {
		f.failing = map[string]error{}
	}
	if err == nil {
		delete(f.failing, method)
		return
	}
	f.failing[method] = err
}

// CloseMolecule closes the root bead of a poured molecule, as whoever finishes
// with a story does. Its steps are left as they were.
func (f *FakeTracker) CloseMolecule(rootID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closedRoots == nil {
		f.closedRoots = map[string]bool{}
	}
	f.closedRoots[rootID] = true
}

// OpenMolecule implements application.WorkTracker.
func (f *FakeTracker) OpenMolecule(_ context.Context, rootID string) (application.Molecule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "OpenMolecule")
	if f.Err != nil {
		return application.Molecule{}, f.Err
	}
	if err := f.failing["OpenMolecule"]; err != nil {
		return application.Molecule{}, err
	}
	for _, molecule := range f.molecules {
		if molecule.RootID != rootID || f.closedRoots[rootID] {
			continue
		}
		open := application.Molecule{RootID: rootID}
		for _, step := range molecule.Steps {
			if !f.closedSteps[step.ID] {
				open.Steps = append(open.Steps, step)
			}
		}
		return open, nil
	}
	return application.Molecule{}, nil
}

// OpenSteps implements application.WorkTracker.
func (f *FakeTracker) OpenSteps(_ context.Context, moleculeID string) ([]application.FormulaStep, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var open []application.FormulaStep
	for _, molecule := range f.molecules {
		if molecule.RootID != moleculeID {
			continue
		}
		for _, step := range molecule.Steps {
			if !f.closedSteps[step.ID] {
				open = append(open, step)
			}
		}
	}
	return open, nil
}

// CommentOnStory implements application.WorkTracker.
func (f *FakeTracker) CommentOnStory(_ context.Context, id, text string) error {
	return f.write(id, func(s *fakeStory) error {
		s.comments = append(s.comments, text)
		return nil
	})
}

// RefuseToClose makes CloseStory turn a story down, saying why — the way beads
// turns down a close by an actor that is not the story's assignee. An empty why
// lets closes through again, so that a test can walk a story from a close that
// was refused to the run that closes it.
func (f *FakeTracker) RefuseToClose(id, why string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refused == nil {
		f.refused = map[string]string{}
	}
	if why == "" {
		delete(f.refused, id)
		return
	}
	f.refused[id] = why
}

// CloseStory implements application.WorkTracker.
func (f *FakeTracker) CloseStory(_ context.Context, id, reason string) error {
	return f.write(id, func(s *fakeStory) error {
		if why := f.refused[id]; why != "" {
			return fmt.Errorf("%s", why)
		}
		s.detail.Status = StatusClosed
		s.closeReason = reason
		return nil
	})
}

// StaleClaims implements application.WorkTracker.
func (f *FakeTracker) StaleClaims(_ context.Context, now time.Time) ([]application.StoryDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	var stale []application.StoryDetail
	for _, id := range f.order {
		if s := f.stories[id]; lapsed(s.detail, now) {
			stale = append(stale, s.detail)
		}
	}
	return stale, nil
}

// HeartbeatClaim implements application.WorkTracker.
func (f *FakeTracker) HeartbeatClaim(_ context.Context, id string) error {
	return f.write(id, func(s *fakeStory) error {
		if s.detail.Status != StatusInProgress || s.detail.Assignee != Actor {
			return fmt.Errorf("heartbeat %s: it is %s and held by %q, not a claim of %s", id, s.detail.Status, s.detail.Assignee, Actor)
		}
		s.detail.LeaseExpires = f.now().Add(LeaseTTL)
		return nil
	})
}

// ReclaimStory implements application.WorkTracker.
// A lease that still holds is left alone and counts as no write.
func (f *FakeTracker) ReclaimStory(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return false, f.Err
	}
	s, ok := f.stories[id]
	if !ok {
		return false, fmt.Errorf("no story %q", id)
	}
	if !lapsed(s.detail, f.now()) {
		return false, nil
	}
	s.detail.Status = StatusOpen
	s.detail.Assignee = ""
	s.detail.LeaseExpires = time.Time{}
	f.writes++
	return true, nil
}

// lapsed reports whether a story is claimed under a lease that had run out by
// now. A claim with no lease has not lapsed: nothing says when it would.
func lapsed(detail application.StoryDetail, now time.Time) bool {
	return detail.Status == StatusInProgress && !detail.LeaseExpires.IsZero() && now.After(detail.LeaseExpires)
}

// now is the fake's clock: Clock when set, the real one otherwise.
func (f *FakeTracker) now() time.Time {
	if f.Clock != nil {
		return f.Clock()
	}
	return time.Now()
}

// Sync implements application.TrackerSync. Nothing is synchronised: the fake
// counts the cycle and reports SyncErr, so that a use case can be walked
// through a halted sync without a database or a remote. A cycle that gets
// through publishes the notes as they stand; a halted one publishes nothing.
func (f *FakeTracker) Sync(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "Sync")
	if f.Err != nil {
		return f.Err
	}
	f.syncs++
	if f.syncErrOnce != nil {
		err := f.syncErrOnce
		f.syncErrOnce = nil
		return err
	}
	if f.SyncErr != nil {
		return f.SyncErr
	}
	f.published = make(map[string]string, len(f.notes))
	for key, value := range f.notes {
		f.published[key] = value
	}
	f.packChecks++
	return nil
}

// PackChecks reports how many successful synchronisation cycles checked the
// git-remote-cache pack counts, so that a test can say every sync asks the
// question, whether or not it was also the daily GC's turn.
func (f *FakeTracker) PackChecks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.packChecks
}

// Note implements application.TrackerSync.
func (f *FakeTracker) Note(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	return f.notes[key], nil
}

// SetNote implements application.TrackerSync.
func (f *FakeTracker) SetNote(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if key == "" {
		return fmt.Errorf("a note needs a key")
	}
	f.writes++
	f.notes[key] = value
	return nil
}

// NotesWithPrefix implements application.DoctorNotes.
func (f *FakeTracker) NotesWithPrefix(_ context.Context, prefix string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	found := map[string]string{}
	for key, value := range f.notes {
		if strings.HasPrefix(key, prefix) {
			found[key] = value
		}
	}
	return found, nil
}

// PublishedNote reads a note as the other host would on its next sync: as it
// stood when the last sync that got through pushed it. "" when that sync did
// not carry the key, or none has got through.
func (f *FakeTracker) PublishedNote(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.published[key]
}

// ClearNote implements application.TrackerSync.
func (f *FakeTracker) ClearNote(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	if key == "" {
		return fmt.Errorf("a note needs a key")
	}
	f.writes++
	delete(f.notes, key)
	return nil
}

// SyncExits makes the next Sync halt the way a beads exit code says it did.
// Code 0 clears it.
func (f *FakeTracker) SyncExits(code int, said string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if code == 0 {
		f.SyncErr = nil
		return
	}
	f.SyncErr = &application.SyncHalt{Code: code, Said: said}
}

// SyncExitsOnce makes only the very next Sync call halt the way a beads exit
// code says it did; the call after that reports SyncErr (or nothing, when
// SyncErr is unset) as if the tracker had come right on its own. It is how a
// test stands in for a conflict that clears on retry, or for one that does not
// but says something different the second time.
func (f *FakeTracker) SyncExitsOnce(code int, said string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncErrOnce = &application.SyncHalt{Code: code, Said: said}
}

// Syncs reports how many synchronisation cycles were asked for, so that a test
// can say a halt was not retried.
func (f *FakeTracker) Syncs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.syncs
}

// GC implements application.TrackerSync. Nothing is actually collected: the
// fake counts the call, so a test can say whether mw sync asked for one — and
// counts it again as a repack, since the real Gateway this stands in for
// repacks the git-remote-cache in the same call.
func (f *FakeTracker) GC(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, "GC")
	if f.Err != nil {
		return f.Err
	}
	if f.GCErr != nil {
		return f.GCErr
	}
	f.gcs++
	f.repacks++
	return nil
}

// GCs reports how many times GC was asked to collect, so that a test can say
// a cadence held it back.
func (f *FakeTracker) GCs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gcs
}

// Repacked reports how many of those collections also carried the
// git-remote-cache repack, so that a test can say a due GC carries it and a
// GC held back by its cadence carries neither.
func (f *FakeTracker) Repacked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.repacks
}

// SetSize makes Size report bytes, as if the beads database were that large
// on disk.
func (f *FakeTracker) SetSize(bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.size = bytes
}

// Size implements application.TrackerNotes.
func (f *FakeTracker) Size(_ context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return 0, f.Err
	}
	return f.size, nil
}

// waiting reports whether this story still waits on another that is not
// finished. The lock is held by the caller.
func (f *FakeTracker) waiting(s *fakeStory) bool {
	for _, need := range s.needs {
		blocker, filed := f.stories[need]
		if !filed || blocker.detail.Status != StatusClosed {
			return true
		}
	}
	return false
}

// write applies a change to one story under the lock.
func (f *FakeTracker) write(id string, change func(*fakeStory) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	s, ok := f.stories[id]
	if !ok {
		return fmt.Errorf("no story %q", id)
	}
	if err := change(s); err != nil {
		return err
	}
	f.writes++
	return nil
}

// IDs reports the ids of the stories in a listing, in order — a convenience
// for tests that assert on what came back.
func IDs(details []application.StoryDetail) []string {
	ids := make([]string, 0, len(details))
	for _, d := range details {
		ids = append(ids, d.Story.ID)
	}
	return ids
}

// SortedIDs is IDs, sorted.
func SortedIDs(details []application.StoryDetail) []string {
	ids := IDs(details)
	sort.Strings(ids)
	return ids
}

// FakeTracker satisfies the ports the beads gateway stands behind.
var (
	_ application.WorkTracker  = (*FakeTracker)(nil)
	_ application.TrackerSync  = (*FakeTracker)(nil)
	_ application.TrackerNotes = (*FakeTracker)(nil)
	_ application.DoctorNotes  = (*FakeTracker)(nil)
)

// priorityOr is a priority a story was filed with, or the default when it was
// filed with none: zero means "unset" in a NewStory, as it does for the tracker.
func priorityOr(priority int) int {
	if priority == 0 {
		return application.DefaultPriority
	}
	return priority
}

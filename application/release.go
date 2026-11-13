package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// OverHoldMarker opens the comment Release writes on a story it releases over a
// hold on the Governor's word. A hold comment older than the newest one of these
// on the story no longer holds it.
const OverHoldMarker = "RELEASED OVER HOLD:"

// Release approves a plan that was filed earlier. `mw file` holds every story
// it files, because the Governor is usually not at the machine when the Mayor
// files a plan; this is how he says yes afterwards, without filing the plan a
// second time.
//
// It reads the epic back out of the tracker, prints the same tree the filing
// printed, and then releases the stories that are still held. Nothing else is
// touched: a story somebody has already taken, or already finished, or that was
// released before, is left exactly as it was found. So releasing an epic twice
// does what releasing it once did, and nothing more.
//
// A held epic is opened with the stories it releases; the phone's view takes
// only the children of epics that are open or in progress.
//
// The tree is printed before anything is released, and shows the epic as it was
// found — what was held at the moment the release was asked for. What changed
// is the line under it.
type Release struct {
	Tracker WorkTracker

	// Rules is where a rig's file is read from, to warn of a story that has no
	// files key on a rig that expects one. A nil Rules warns of none.
	Rules EpicRules

	// Rigs are the rigs checked out on this host and RigFiles says whether a
	// path is in one, to warn of a story whose files are in none of its rig
	// (mw-gq6.428). Either nil warns of none.
	Rigs     map[string]string
	RigFiles RigFiles

	// OverHold is why a story held on the Governor's word is released anyway
	// (mw-gq6.481). Empty releases none of them: the release is refused, naming
	// the comment. It is written on each story it lets through.
	OverHold string

	// Out is where the tree is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run releases the held stories of one already-filed epic and reports the epic
// as it was found. An epic the tracker does not have is a refusal, and nothing
// is written: the reading comes first, and it is the whole of the reading.
//
// An id that is a story and not an epic releases that one story, when it is
// held (see story).
func (r Release) Run(ctx context.Context, epicID string) (FiledPlan, error) {
	if r.Tracker == nil {
		return FiledPlan{}, fmt.Errorf("releasing a plan: there is no work tracker to release it in")
	}
	if strings.TrimSpace(epicID) == "" {
		return FiledPlan{}, fmt.Errorf("releasing a plan: which epic?")
	}

	epic, err := r.Tracker.ShowEpic(ctx, epicID)
	if errors.Is(err, ErrNotAnEpic) {
		return r.story(ctx, epicID)
	}
	if err != nil {
		return FiledPlan{}, fmt.Errorf("reading the epic %s: %w", epicID, err)
	}
	found := filedFrom(epic)
	r.print(found.Tree())
	// A warning only: the stories are released all the same.
	warnNoFilesKey(ctx, r.Out, r.Rules, epic.Stories...)
	warnWrongRig(r.Out, r.Rigs, r.RigFiles, epic.Stories...)

	held := found.held()
	iced := found.iced()
	refs := make([]heldRef, 0, len(held))
	for _, story := range held {
		refs = append(refs, heldRef{ID: story.ID, Parent: epic.ID})
	}
	if err := r.checkWordHolds(ctx, refs); err != nil {
		return found, err
	}
	for _, story := range held {
		if err := r.Tracker.ReleaseStory(ctx, story.ID); err != nil {
			return found, fmt.Errorf("releasing story %s of %s: %w", story.ID, epic.ID, err)
		}
	}
	// A held epic hides its stories from the phone's view, released or not, so
	// it is opened with the stories it releases. An epic that is open, in
	// progress or closed is left as it is, and so is one with nothing to release.
	opened := len(held) > 0 && strings.EqualFold(strings.TrimSpace(epic.Status), StatusHeld)
	if opened {
		if err := r.Tracker.ReleaseStory(ctx, epic.ID); err != nil {
			return found, fmt.Errorf("opening the epic %s after releasing its stories: %w", epic.ID, err)
		}
	}
	found.Released = true
	r.print(found.released(len(held)))
	if len(iced) > 0 {
		r.print(fmt.Sprintf("Left iced: %s. Thaw them with mw thaw first.\n", strings.Join(iced, ", ")))
	}
	if opened {
		r.print(fmt.Sprintf("Opened the epic %s too: it was held.\n", epic.ID))
	}
	return found, nil
}

// story releases one story named by its own id: it prints the story's entry of
// the tree, warns of a missing files key, releases it and says so. A story that
// is not held is left as it is and said. A story whose Path names no rig or no
// target branch is refused before anything is printed or written.
func (r Release) story(ctx context.Context, id string) (FiledPlan, error) {
	detail, err := r.Tracker.ShowStory(ctx, id)
	if err != nil {
		return FiledPlan{}, fmt.Errorf("reading the story %s: %w", id, err)
	}
	if detail.Iced() {
		r.print(fmt.Sprintf("%s is iced: nothing released. Thaw it with mw thaw %s first.\n", id, id))
		return FiledPlan{EpicID: detail.EpicID, Defaults: detail.Defaults}, nil
	}
	path := detail.Merged()
	if strings.TrimSpace(path.Rig) == "" || strings.TrimSpace(path.Branch) == "" {
		return FiledPlan{}, fmt.Errorf("releasing the story %s: it has no path (no rig or no target branch)", id)
	}
	found := FiledPlan{EpicID: detail.EpicID, Defaults: detail.Defaults, Stories: []FiledStory{{
		ID:              detail.Story.ID,
		Title:           detail.Story.Title,
		Path:            path,
		EstimateMinutes: detail.EstimateMinutes,
		Needs:           detail.Needs,
		State:           StateOf(detail.Status),
		Iced:            detail.Iced(),
	}}}
	if !detail.Held() {
		r.print(fmt.Sprintf("%s is %s, not held: nothing released\n", id, found.Stories[0].state()))
		return found, nil
	}
	r.print(found.Stories[0].tree())
	// A warning only: the story is released all the same.
	warnNoFilesKey(ctx, r.Out, r.Rules, detail)
	warnWrongRig(r.Out, r.Rigs, r.RigFiles, detail)
	if err := r.checkWordHolds(ctx, []heldRef{{ID: id, Parent: detail.EpicID}}); err != nil {
		return found, err
	}
	if err := r.Tracker.ReleaseStory(ctx, id); err != nil {
		return found, fmt.Errorf("releasing story %s: %w", id, err)
	}
	found.Released = true
	r.print(fmt.Sprintf("released 1: %s\n", id))
	return found, nil
}

// heldRef is a held story to be released and the bead it hangs from.
type heldRef struct {
	ID, Parent string
}

var governorsWord = regexp.MustCompile(`(?i)\b(governor|his word)\b`)

// holdsOnHisWord reports whether a comment is a hold on the Governor's word: one
// that opens with HeldCommentPrefix, the way the factory writes a real hold, and
// names the Governor or his word. The words held and Governor elsewhere in its
// prose are not one: a filing note that says "the Governor to confirm" or quotes
// him is no hold on his word (mw-gq6.720).
func holdsOnHisWord(text string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), HeldCommentPrefix) &&
		governorsWord.MatchString(text)
}

// wordHold is the newest comment on a story, or on its parent, that holds it on
// the Governor's word: one that opens HELD: and names the Governor or his word,
// and that no OverHoldMarker comment on the story is newer than. A
// comment of the story's own holds it; a comment on the parent holds it only
// when it names the story's id, for an epic's comment about other stories is
// not a hold on this one (mw-gq6.566). ok is false when there is none; on is
// the id of the bead the comment sits on.
func wordHold(id string, own, parent []Comment, parentID string) (hold Comment, on string, ok bool) {
	var marked Comment
	hasMark := false
	for _, c := range own {
		if strings.HasPrefix(strings.TrimSpace(c.Text), OverHoldMarker) {
			marked, hasMark = c, true
		}
	}
	scan := func(comments []Comment, bead string, sameList bool) {
		for i, c := range comments {
			if !holdsOnHisWord(c.Text) {
				continue
			}
			if !sameList && !namesID(c.Text, id) {
				continue
			}
			if hasMark {
				if !c.Created.IsZero() && !marked.Created.IsZero() {
					if !marked.Created.Before(c.Created) {
						continue
					}
				} else if sameList && i < indexOfComment(comments, marked) {
					continue
				}
			}
			if !ok || c.Created.After(hold.Created) || c.Created.Equal(hold.Created) {
				hold, on, ok = c, bead, true
			}
		}
	}
	scan(own, id, true)
	scan(parent, parentID, false)
	return hold, on, ok
}

// namesID reports whether text names the bead id as a whole: no letter, digit,
// hyphen, underscore or dot runs into it before, and nothing runs on after it,
// so f-1.1 is not found in f-1.17 or in f-1.1.2, but is in "f-1.1." ending a
// sentence.
func namesID(text, id string) bool {
	if id == "" {
		return false
	}
	idChar := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_'
	}
	for from := 0; ; {
		i := strings.Index(text[from:], id)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(id)
		from = start + 1
		if start > 0 && (idChar(text[start-1]) || text[start-1] == '.') {
			continue
		}
		if end < len(text) {
			if idChar(text[end]) {
				continue
			}
			if text[end] == '.' && end+1 < len(text) && idChar(text[end+1]) {
				continue
			}
		}
		return true
	}
}

func indexOfComment(comments []Comment, want Comment) int {
	for i, c := range comments {
		if c == want {
			return i
		}
	}
	return -1
}

// checkWordHolds refuses the release of any of these held stories that a comment
// of its own, or one of its parent's that names it, holds on the Governor's word,
// naming the bead the comment sits on and the comment,
// unless OverHold says why not; nothing has been released when it refuses. With
// OverHold it writes the reason on each such story.
func (r Release) checkWordHolds(ctx context.Context, refs []heldRef) error {
	if len(refs) == 0 {
		return nil
	}
	var ids []string
	seen := map[string]bool{}
	for _, ref := range refs {
		for _, id := range []string{ref.ID, ref.Parent} {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	comments, err := r.Tracker.StoriesComments(ctx, ids)
	if err != nil {
		return fmt.Errorf("reading the comments of %s to look for a hold on the Governor's word: %w",
			strings.Join(ids, ", "), err)
	}
	var refusals []string
	var flagged []string
	for _, ref := range refs {
		hold, on, ok := wordHold(ref.ID, comments[ref.ID], comments[ref.Parent], ref.Parent)
		if !ok {
			continue
		}
		flagged = append(flagged, ref.ID)
		refusals = append(refusals, fmt.Sprintf("%s is held on the Governor's word, by this comment on %s by %s%s: %q",
			ref.ID, on, hold.Author, holdTime(hold), strings.TrimSpace(hold.Text)))
	}
	if len(flagged) == 0 {
		return nil
	}
	if strings.TrimSpace(r.OverHold) == "" {
		return fmt.Errorf("releasing refused, nothing released: %s\nRelease anyway with --over-hold \"<why>\"",
			strings.Join(refusals, "\n"))
	}
	for _, id := range flagged {
		note := fmt.Sprintf("%s %s", OverHoldMarker, strings.TrimSpace(r.OverHold))
		if err := r.Tracker.CommentOnStory(ctx, id, note); err != nil {
			return fmt.Errorf("writing the reason for releasing %s over its hold: %w", id, err)
		}
	}
	return nil
}

func holdTime(c Comment) string {
	if c.Created.IsZero() {
		return ""
	}
	return " at " + c.Created.UTC().Format("2006-01-02 15:04Z")
}

// print writes one block of the report, when there is somewhere to write it.
func (r Release) print(block string) {
	if r.Out == nil {
		return
	}
	fmt.Fprint(r.Out, block)
}

// filedFrom is an epic read back out of the tracker, as the same FiledPlan the
// filing of a plan reports — so that one tree serves both. A story's Needs are
// narrowed to what it is still waiting for: a story it waits on that is already
// finished is no longer a wait, and a tree that said otherwise would read as
// blocked work that is not blocked.
func filedFrom(epic EpicDetail) FiledPlan {
	finished := make(map[string]bool, len(epic.Stories))
	for _, story := range epic.Stories {
		finished[story.Story.ID] = story.Closed()
	}

	filed := FiledPlan{EpicID: epic.ID, Title: epic.Title, Defaults: epic.Defaults}
	for _, story := range epic.Stories {
		// A child that is itself an epic is not a story of this plan: it is
		// named as an epic, and it is never released or counted as work.
		if story.IsEpic {
			filed.Epics = append(filed.Epics, FiledEpic{
				ID: story.Story.ID, Title: story.Story.Title, State: StateOf(story.Status),
			})
			continue
		}
		waits := make([]string, 0, len(story.Needs))
		for _, need := range story.Needs {
			// A need the epic does not hold is one this reading cannot vouch
			// for, so it counts as a wait: the tracker is the one that decides
			// what is ready, and it knows about that story even if we do not.
			if !finished[need] {
				waits = append(waits, need)
			}
		}
		filed.Stories = append(filed.Stories, FiledStory{
			ID:              story.Story.ID,
			Title:           story.Story.Title,
			Path:            story.Merged(),
			EstimateMinutes: story.EstimateMinutes,
			Needs:           waits,
			State:           StateOf(story.Status),
			Iced:            story.Iced(),
		})
	}
	return filed
}

// held is the stories of this plan the tracker is still holding back.
func (p FiledPlan) held() []FiledStory {
	var holding []FiledStory
	for _, story := range p.Stories {
		if story.state() == StateHeld && !story.Iced {
			holding = append(holding, story)
		}
	}
	return holding
}

// iced is the ids of the held stories of this plan that are in the icebox,
// which a release leaves held.
func (p FiledPlan) iced() []string {
	var ids []string
	for _, story := range p.Stories {
		if story.state() == StateHeld && story.Iced {
			ids = append(ids, story.ID)
		}
	}
	return ids
}

// ready is the ids of the stories a dispatcher can take once the held ones have
// been released: the ones that are not claimed and not finished, and that wait
// on nothing still to be done. It is what the tracker's own rule comes to, said
// here so that a person is told what the release made takeable.
func (p FiledPlan) ready() []string {
	var takeable []string
	for _, story := range p.Stories {
		switch story.state() {
		case StateHeld, StateOpen:
			if len(story.Needs) == 0 && !story.Iced {
				takeable = append(takeable, story.ID)
			}
		}
	}
	return takeable
}

// released is what a person is told once the held stories have been let
// through: how many were released, or that there were none, and what can be
// dispatched now.
func (p FiledPlan) released(count int) string {
	var did string
	switch {
	case len(p.Stories) == 0:
		return fmt.Sprintf("\nThe epic %s has no stories: there is nothing to release.\n", p.EpicID)
	case count == 0:
		did = fmt.Sprintf("Nothing to release: none of the %d stories of %s is held.",
			len(p.Stories), p.EpicID)
	case count == 1:
		did = fmt.Sprintf("Released the 1 held story of %s.", p.EpicID)
	default:
		did = fmt.Sprintf("Released the %d held stories of %s.", count, p.EpicID)
	}

	ready := p.ready()
	if len(ready) == 0 {
		return did + " Nothing is ready: every story left waits on another, or is already taken.\n"
	}
	return fmt.Sprintf("%s Ready now: %s.\n", did, strings.Join(ready, ", "))
}

// The pace of a paced release (mw-u2w5klk, "The model stories and the pace"): at
// most ReleasePacedMax stories of a list running or open at once, and none
// released while the week's usage reads over ReleasePacedUsageLine percent. They
// are the epic's numbers, kept here and nowhere else; they are no config keys.
const (
	ReleasePacedMax       = 3
	ReleasePacedUsageLine = 60.0
)

// LabelModelStory marks a story that is a model's judgement (a pass over a batch
// of chapters, say): it is released at the pace above, whoever releases it.
const LabelModelStory = "model-story"

// ReleasePaced un-holds the next stories of a list, in the order the list gives,
// at the pace of the epic that filed them (ReleasePacedMax, ReleasePacedUsageLine),
// so that nobody reads the usage and counts what runs by hand at each sitting.
// A story of the list that is open or running counts against the pace and is
// left as it is; one that is closed, not in the tracker, iced, or held on the
// Governor's word is skipped and named. It says plainly why it released none.
type ReleasePaced struct {
	Tracker WorkTracker
	// Readings are the usage readings mw usage read keeps.
	Readings UsageReadings
	Now      func() time.Time
	// DryRun releases nothing and writes nothing, and prints what it would.
	DryRun bool
	Out    io.Writer
}

// Run releases the next stories of ids the pace allows and reports their ids.
func (r ReleasePaced) Run(ctx context.Context, ids []string) ([]string, error) {
	if r.Tracker == nil || r.Readings.Notes == nil {
		return nil, fmt.Errorf("releasing a list: there is no work tracker to release it in")
	}
	ids = listIDs(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("releasing a list: the list has no story ids")
	}
	now := time.Now() //mw:realclock the default clock, taken before an injected Now replaces it
	if r.Now != nil {
		now = r.Now()
	}
	now = now.UTC()

	details, err := r.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("reading the stories of the list: %w", err)
	}
	byID := make(map[string]StoryDetail, len(details))
	for _, detail := range details {
		byID[detail.Story.ID] = detail
	}
	var active int
	var held, closed, missing, iced []string
	for _, id := range ids {
		detail, found := byID[id]
		switch {
		case !found:
			missing = append(missing, id)
		case detail.Closed():
			closed = append(closed, id)
		case detail.Held() && detail.Iced():
			iced = append(iced, id)
		case detail.Held():
			held = append(held, id)
		default:
			// Open or in progress: running or about to be, so it counts.
			active++
		}
	}

	// The usage speaks first, before the skipped are named; the count after.
	pace, err := checkPace(ctx, r.Readings, now, active, "the list")
	if err != nil {
		return nil, err
	}
	if pace.OnUsage {
		r.print(pace.Refusal() + "\n")
		return nil, nil
	}
	r.printSkipped("closed, skipped", closed)
	r.printSkipped("not in the tracker, skipped", missing)
	r.printSkipped("iced, skipped (mw thaw first)", iced)
	if pace.Why != "" {
		r.print(pace.Refusal() + "\n")
		return nil, nil
	}
	reading, room := pace.Reading, pace.Room

	switch {
	case len(held) == 0:
		r.print("no story of the list is held: nothing released\n")
		return nil, nil
	}

	chosen, err := r.choose(ctx, held, room)
	if err != nil {
		return nil, err
	}
	if len(chosen) == 0 {
		r.print("nothing released\n")
		return nil, nil
	}
	if r.DryRun {
		r.print(fmt.Sprintf("would release %d: %s (usage %.1f%% of the week, %d of the list running or open)\n",
			len(chosen), strings.Join(chosen, ", "), reading.Percent, active))
		return nil, nil
	}
	note := fmt.Sprintf("RELEASED by mw release-paced: usage %.1f%% of the week (read %s, the week resets %s), not over %.0f%%; "+
		"%d of the list running or open, so room for %d (at most %d at once)",
		reading.Percent, reading.At.UTC().Format(time.RFC3339), reading.Resets.UTC().Format(time.RFC3339),
		ReleasePacedUsageLine, active, room, ReleasePacedMax)
	var released []string
	for _, id := range chosen {
		if err := r.Tracker.ReleaseStory(ctx, id); err != nil {
			return released, fmt.Errorf("releasing story %s: %w", id, err)
		}
		released = append(released, id)
		if err := r.Tracker.CommentOnStory(ctx, id, note); err != nil {
			return released, fmt.Errorf("%s was released; writing its comment failed: %w", id, err)
		}
	}
	r.print(fmt.Sprintf("released %d: %s (usage %.1f%% of the week, %d of the list running or open)\n",
		len(released), strings.Join(released, ", "), reading.Percent, active))
	return released, nil
}

// Pace is the verdict of the pace of a paced release on a count of what already
// runs or is open: the usage reading it read, the room left under
// ReleasePacedMax, and why it lets nothing go when it does not.
type Pace struct {
	Reading UsageReading
	// Room is how many more may be released at once; zero when Why is set.
	Room int
	// Why is empty when a release may go on, else the reason none may.
	Why string
	// OnUsage is whether Why is about the usage (missing, stale or over the
	// line), rather than the count.
	OnUsage bool
	// hint is what to do about the reason, as the paced release says it.
	hint string
}

// Refusal is the reason as mw release-paced prints it.
func (p Pace) Refusal() string { return p.Why + ": nothing released" + p.hint }

// Held is the reason as mw path set prints it after "held: <id>: ".
func (p Pace) Held() string { return p.Why + p.hint }

// checkPace is the one pace check of a paced release, used by ReleasePaced.Run
// and by mw path set (whyStayHeld) for a story labelled model-story: the newest
// usage reading must be current and not over ReleasePacedUsageLine, and fewer
// than ReleasePacedMax stories may already be running or open (active, of what,
// as the count says it). A reason is no error; a reading that cannot be read is.
func checkPace(ctx context.Context, readings UsageReadings, now time.Time, active int, of string) (Pace, error) {
	const unreported = "no usage was reported (mw usage read, or mw usage set <percent>)"
	if readings.Notes == nil {
		return Pace{OnUsage: true, Why: unreported}, nil
	}
	all, err := readings.All(ctx)
	if err != nil {
		return Pace{}, fmt.Errorf("reading the usage: %w", err)
	}
	if len(all) == 0 {
		return Pace{OnUsage: true, Why: unreported}, nil
	}
	reading := all[len(all)-1]
	if why := reading.Stale(now); why != "" {
		return Pace{Reading: reading, OnUsage: true, hint: "; run mw usage read",
			Why: fmt.Sprintf("usage %.1f%% is STALE (%s)", reading.Percent, why)}, nil
	}
	if reading.Percent > ReleasePacedUsageLine {
		return Pace{Reading: reading, OnUsage: true,
			Why: fmt.Sprintf("usage %.1f%% of the week (read %s) is over %.0f%%",
				reading.Percent, reading.At.UTC().Format(time.RFC3339), ReleasePacedUsageLine)}, nil
	}
	room := ReleasePacedMax - active
	if room <= 0 {
		return Pace{Reading: reading,
			Why: fmt.Sprintf("%d of %s running or open, at most %d at once", active, of, ReleasePacedMax)}, nil
	}
	return Pace{Reading: reading, Room: room}, nil
}

// choose is the first room of the held stories, in order, that no comment holds
// on the Governor's word; one that does is skipped and named. It reads comments
// only for as many stories as it needs.
func (r ReleasePaced) choose(ctx context.Context, held []string, room int) ([]string, error) {
	var chosen []string
	for from := 0; from < len(held) && len(chosen) < room; {
		to := min(from+room-len(chosen), len(held))
		batch := held[from:to]
		from = to
		comments, err := r.Tracker.StoriesComments(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("reading the comments of %s to look for a hold on the Governor's word: %w",
				strings.Join(batch, ", "), err)
		}
		for _, id := range batch {
			if _, _, word := wordHold(id, comments[id], nil, ""); word {
				r.print(fmt.Sprintf("%s is held on the Governor's word, skipped (mw release --over-hold \"<why>\" %s to release it)\n", id, id))
				continue
			}
			chosen = append(chosen, id)
		}
	}
	return chosen, nil
}

func (r ReleasePaced) printSkipped(what string, ids []string) {
	if len(ids) > 0 {
		r.print(fmt.Sprintf("%s: %s\n", what, strings.Join(ids, ", ")))
	}
}

func (r ReleasePaced) print(line string) {
	if r.Out != nil {
		fmt.Fprint(r.Out, line)
	}
}

// listIDs is the ids of a list in order, each once, with blank ones left out.
func listIDs(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

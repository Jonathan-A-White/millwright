package application

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// HeldCommentPrefix opens a comment that is the written reason a bead is held:
// 'HELD: waits for his word on the colour'. A held bead with such a comment, or
// one of the labels in HoldReasonLabels, is held on purpose and mw stuck leaves
// it alone.
const HeldCommentPrefix = "HELD:"

// IsHoldComment is whether a comment is the written reason a bead is held: its
// first word is HELD, in any case, followed by a colon or a space, so that both
// 'HELD: waits' and the hand-written 'HELD by the Mayor 921: waits' count, and
// 'HELDOVER' or a comment with HELD elsewhere in it does not (mw-gq6.1379). It
// is the one reader of a hold's reason: nothing else matches the opening by a
// prefix of its own.
func IsHoldComment(text string) bool {
	rest := strings.TrimSpace(text)
	if len(rest) <= len("HELD") || !strings.EqualFold(rest[:len("HELD")], "HELD") {
		return false
	}
	switch rest[len("HELD")] {
	case ':', ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

// StuckClaimAge is how long a claim may have had no session behind it (its
// lease run out with no heartbeat since) before mw stuck counts it a root.
const StuckClaimAge = 6 * time.Hour

// HoldReasonLabels are the labels that say why a bead is held. A label starting
// LabelHitlKindPrefix counts as LabelHitl.
func HoldReasonLabels() []string {
	return []string{"grilling", LabelHitl, LabelDemo, LabelIcebox}
}

// stuckNoReason is the Why of a held root with no reason written.
const stuckNoReason = "held, no reason written"

// StuckRoot is a held bead that live beads wait behind, and so cannot move
// until it does.
type StuckRoot struct {
	ID string
	// Why is the kind of hold: iced, held with no reason, or a claim with no
	// session.
	Why string
	// Behind are the live beads that wait on it, directly or through other live
	// beads, in id order.
	Behind []string
}

// StuckReport is every held root that live beads wait behind.
type StuckReport struct {
	Roots []StuckRoot
	// PastReason are the held stories whose written reason names only beads that
	// have closed, grouped by epic: held with nothing left to wait for.
	PastReason []StuckEpicHolds
}

// StuckPastReason is a held story whose written reason (the newest comment
// IsHoldComment accepts) names beads that have all closed.
type StuckPastReason struct {
	ID string
	// Closed are the beads its reason names, all closed, in id order.
	Closed []string
}

// StuckEpicHolds are the StuckPastReason stories of one epic, in id order. Epic
// is empty for stories with no parent.
type StuckEpicHolds struct {
	Epic  string
	Holds []StuckPastReason
}

// PastReasonCount is how many held stories are past their reason.
func (r StuckReport) PastReasonCount() int {
	n := 0
	for _, group := range r.PastReason {
		n += len(group.Holds)
	}
	return n
}

// pastReasonText is the part of mw stuck's output about holds past their reason:
// the count, then each epic with its stories and the closed beads they name.
func (r StuckReport) pastReasonText() string {
	n := r.PastReasonCount()
	if n == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "past their reason: %d held %s name only beads that have closed; mw release puts one back\n", n, plural(n, "story", "stories"))
	for _, group := range r.PastReason {
		name := group.Epic
		if name == "" {
			name = "no epic"
		}
		fmt.Fprintf(&b, "%s (%d)\n", name, len(group.Holds))
		for _, hold := range group.Holds {
			fmt.Fprintf(&b, "  %s: closed %s\n", hold.ID, strings.Join(hold.Closed, " "))
		}
	}
	return b.String()
}

// LiveBehind is how many distinct live beads wait behind some root.
func (r StuckReport) LiveBehind() int {
	seen := map[string]bool{}
	for _, root := range r.Roots {
		for _, id := range root.Behind {
			seen[id] = true
		}
	}
	return len(seen)
}

// Summary is the count on one line: 'N live beads behind M held roots'.
func (r StuckReport) Summary() string {
	n := r.LiveBehind()
	return fmt.Sprintf("%d live %s behind %d held %s", n, plural(n, "bead", "beads"), len(r.Roots), plural(len(r.Roots), "root", "roots"))
}

// String is what mw stuck prints: the count, then a line for each root with the
// live beads behind it.
func (r StuckReport) String() string {
	var b strings.Builder
	if len(r.Roots) == 0 {
		if r.PastReasonCount() == 0 {
			return "nothing is stuck: no live bead waits behind a held bead that cannot move.\n"
		}
		b.WriteString("no live bead waits behind a held bead that cannot move.\n")
	} else {
		b.WriteString("stuck: " + r.Summary() + "\n")
	}
	for _, root := range r.Roots {
		fmt.Fprintf(&b, "%s (%s): %d behind: %s\n", root.ID, root.Why, len(root.Behind), strings.Join(root.Behind, " "))
	}
	b.WriteString(r.pastReasonText())
	return b.String()
}

// StuckClaims lists the claims whose lease ran out by now: the tracker's
// StaleClaims, narrowed.
type StuckClaims interface {
	StaleClaims(ctx context.Context, now time.Time) ([]StoryDetail, error)
}

// StuckComments reads the comments of several beads at once: the tracker's
// StoriesComments, narrowed.
type StuckComments interface {
	StoriesComments(ctx context.Context, ids []string) (map[string][]Comment, error)
}

// StuckFinder finds the stuck roots: StuckSurvey.
type StuckFinder interface {
	Find(ctx context.Context) (StuckReport, error)
}

// StuckSurvey finds the stuck roots: held beads that live beads wait behind and
// that, as they are, will never move.
//
//   - An iced bead (label icebox).
//   - A held (deferred) bead every blocker of which is closed and which has no
//     reason to be held: none of the labels in HoldReasonLabels, and no comment
//     that IsHoldComment accepts.
//   - A claimed bead whose lease ran out more than StuckClaimAge ago, with no
//     heartbeat since: a claim with no session. A claim bd shows no lease for is
//     not listed, as StaleClaims does not list it. One queued for its rig's
//     batch landing (run:queued) is listed as that, not as a claim with no
//     session.
//
// Apart from the roots, it lists the held stories past their reason
// (StuckReport.PastReason): held, no blocker unfinished, no label that gives a
// reason, whose newest hold comment names beads in the graph that have all
// closed. A story whose reason names one still open, or names no bead, is not;
// nor is one whose hold also waits for a date still ahead or the Governor's word.
//
// A root is listed only when some live bead (open or in progress, not an epic)
// waits behind it over 'blocks' links, through live beads only. It reads and
// writes nothing.
type StuckSurvey struct {
	Graph    BeadGraph
	Comments StuckComments
	Claims   StuckClaims
	// GiveWay reads the hosts' give-way notes: a claim held on a host whose note
	// stands is frozen, not a claim with no session (mw-2891blf.3).
	GiveWay GiveWayReader
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Find reads the beads and reports the roots, in id order.
func (s StuckSurvey) Find(ctx context.Context) (StuckReport, error) {
	now := s.now()
	beads, err := s.Graph.BeadGraph(ctx)
	if err != nil {
		return StuckReport{}, fmt.Errorf("reading the beads for the stuck check: %w", err)
	}
	byID := make(map[string]domain.GraphBead, len(beads))
	dependents := map[string][]string{}
	for _, b := range beads {
		byID[b.ID] = b
		for _, blocker := range blockedOn(b) {
			dependents[blocker] = append(dependents[blocker], b.ID)
		}
	}
	behind := func(root string) []string { return liveBehind(byID, dependents, root) }

	var roots []StuckRoot
	var maybe []StuckRoot // held with no label that gives a reason: comments decide
	var unlabelled []string
	for _, b := range beads {
		if b.Type == "epic" || strings.Contains(b.ID, "-mol-") || !strings.EqualFold(b.Status, StatusHeld) {
			continue
		}
		switch {
		case slices.Contains(b.Labels, LabelIcebox):
			if live := behind(b.ID); len(live) > 0 {
				roots = append(roots, StuckRoot{ID: b.ID, Why: "iced", Behind: live})
			}
		case hasHoldLabel(b.Labels) || waitsOnUnfinished(byID, b):
		default:
			unlabelled = append(unlabelled, b.ID)
			if live := behind(b.ID); len(live) > 0 {
				maybe = append(maybe, StuckRoot{ID: b.ID, Why: stuckNoReason, Behind: live})
			}
		}
	}
	var pastReason []StuckEpicHolds
	if len(unlabelled) > 0 {
		said, err := s.Comments.StoriesComments(ctx, unlabelled)
		if err != nil {
			return StuckReport{}, fmt.Errorf("reading the comments of the held beads: %w", err)
		}
		for _, root := range maybe {
			if !holdsAReason(said[root.ID]) {
				roots = append(roots, root)
			}
		}
		pastReason = pastReasonHolds(byID, unlabelled, said, now)
	}
	if s.Claims != nil {
		stale, err := s.Claims.StaleClaims(ctx, now.Add(-StuckClaimAge))
		if err != nil {
			return StuckReport{}, fmt.Errorf("reading the claims with no session: %w", err)
		}
		for _, claim := range stale {
			if s.GiveWay.Stands(ctx, claimedOn(claim), now) {
				continue
			}
			if live := behind(claim.Story.ID); len(live) > 0 {
				roots = append(roots, StuckRoot{ID: claim.Story.ID, Why: staleClaimWhy(claim, now), Behind: live})
			}
		}
	}
	sort.Slice(roots, func(a, b int) bool { return roots[a].ID < roots[b].ID })
	return StuckReport{Roots: roots, PastReason: pastReason}, nil
}

// staleClaimWhy is the reason a stale claim is a root. A story queued for its
// batch (run:queued) has no session by design: its Builder finished and it
// waits on its rig's batch landing, so it is named for that, with how long it
// has been queued when the tracker said (mw-gq6.1425). Any other is a claim
// with no session.
func staleClaimWhy(claim StoryDetail, now time.Time) string {
	if !QueuedForBatch(claim) {
		return "claimed, no session for over 6 hours"
	}
	rig := "its rig"
	if name := claim.Merged().Rig; name != "" {
		rig = name
	}
	why := "queued for " + rig + "'s batch landing"
	if !claim.QueuedAt.IsZero() {
		why += " for over " + elapsed(now.Sub(claim.QueuedAt))
	}
	return why
}

func hasHoldLabel(labels []string) bool {
	return slices.ContainsFunc(labels, func(l string) bool {
		l = strings.ToLower(l)
		return slices.Contains(HoldReasonLabels(), l) || strings.HasPrefix(l, LabelHitlKindPrefix)
	})
}

// waitsOnUnfinished is whether a bead still waits on a blocker not closed: it
// is held behind that, not stuck.
func waitsOnUnfinished(byID map[string]domain.GraphBead, b domain.GraphBead) bool {
	for _, blocker := range blockedOn(b) {
		if other, ok := byID[blocker]; ok && !strings.EqualFold(other.Status, StatusClosed) {
			return true
		}
	}
	return false
}

// beadRef is a bead id written in a comment.
var beadRef = regexp.MustCompile(`\bmw-[a-z0-9]+(?:\.[0-9]+)*\b`)

// pastReasonHolds are the held stories among ids whose newest hold comment
// names beads of the graph that have all closed (and at least one), grouped by
// epic: the epics in id order, those with no parent last. A hold whose words
// still stand beyond the beads (holdStillStands) is not past its reason.
func pastReasonHolds(byID map[string]domain.GraphBead, ids []string, said map[string][]Comment, now time.Time) []StuckEpicHolds {
	byEpic := map[string][]StuckPastReason{}
	for _, id := range ids {
		reason := newestHoldComment(said[id])
		if reason == "" || holdStillStands(said[id], now) {
			continue
		}
		var closed []string
		allClosed := true
		for _, ref := range beadRef.FindAllString(reason, -1) {
			named, ok := byID[ref]
			if !ok || ref == id || slices.Contains(closed, ref) {
				continue
			}
			if !strings.EqualFold(named.Status, StatusClosed) {
				allClosed = false
				break
			}
			closed = append(closed, ref)
		}
		if !allClosed || len(closed) == 0 {
			continue
		}
		sort.Strings(closed)
		epic := ""
		for _, e := range byID[id].Edges {
			if e.Kind == domain.EdgeParentChild {
				epic = e.To
				break
			}
		}
		byEpic[epic] = append(byEpic[epic], StuckPastReason{ID: id, Closed: closed})
	}
	var groups []StuckEpicHolds
	for epic, holds := range byEpic {
		sort.Slice(holds, func(a, b int) bool { return holds[a].ID < holds[b].ID })
		groups = append(groups, StuckEpicHolds{Epic: epic, Holds: holds})
	}
	sort.Slice(groups, func(a, b int) bool {
		if (groups[a].Epic == "") != (groups[b].Epic == "") {
			return groups[b].Epic == ""
		}
		return groups[a].Epic < groups[b].Epic
	})
	return groups
}

// isoDate is a date written YYYY-MM-DD in a hold's words.
var isoDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)

// waitsOnGovernor matches the phrasing hold comments use for a wait on the
// Governor: 'HELD for the Governor', 'the Governor's design choice', 'his word',
// 'his answer', and a question put to him ('QUESTION for ...').
var waitsOnGovernor = regexp.MustCompile(`(?i)\bfor the governor\b|\bon the governor\b|\bthe governor's (word|answer|call|choice|decision|design choice)\b|\bhis (word|answer|call|choice|decision|design choice|question)\b|^\s*question for\b`)

// holdStillStands is whether the newest hold comment, or a comment after it,
// says the hold waits on more than the beads it names: a date (YYYY-MM-DD) that
// is still ahead of now, or the Governor's word. A date is ahead when its day
// has not begun (UTC); the day itself is past.
func holdStillStands(comments []Comment, now time.Time) bool {
	last := -1
	for n := len(comments) - 1; n >= 0; n-- {
		if IsHoldComment(comments[n].Text) {
			last = n
			break
		}
	}
	if last < 0 {
		return false
	}
	for _, c := range comments[last:] {
		if waitsOnGovernor.MatchString(c.Text) {
			return true
		}
		for _, day := range isoDate.FindAllString(c.Text, -1) {
			if at, err := time.Parse("2006-01-02", day); err == nil && at.After(now) {
				return true
			}
		}
	}
	return false
}

// newestHoldComment is the text of the last comment IsHoldComment accepts, the
// written reason in force; empty when there is none.
func newestHoldComment(comments []Comment) string {
	for n := len(comments) - 1; n >= 0; n-- {
		if IsHoldComment(comments[n].Text) {
			return comments[n].Text
		}
	}
	return ""
}

func holdsAReason(comments []Comment) bool {
	return slices.ContainsFunc(comments, func(c Comment) bool {
		return IsHoldComment(c.Text)
	})
}

// stuckLive is whether a bead is waiting to move: open or in progress, and not
// an epic.
func stuckLive(b domain.GraphBead) bool {
	return b.Type != "epic" && (strings.EqualFold(b.Status, StatusOpen) || strings.EqualFold(b.Status, StatusInProgress))
}

// liveBehind are the live beads that wait on root, directly or through other
// live beads, in id order.
func liveBehind(byID map[string]domain.GraphBead, dependents map[string][]string, root string) []string {
	seen := map[string]bool{root: true}
	var found []string
	queue := []string{root}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, next := range dependents[at] {
			if seen[next] || !stuckLive(byID[next]) {
				continue
			}
			seen[next] = true
			found = append(found, next)
			queue = append(queue, next)
		}
	}
	sort.Strings(found)
	return found
}

// OpenRefusedAge is how long an open story may carry a run state that keeps mw
// dispatch from taking it before it is named, in DAMPERS and in the one mail to
// the Mayor: a hold that nothing else says (mw-gq6.1026, mw-gq6.1166).
const OpenRefusedAge = 30 * time.Minute

// OpenRefused is an open story a run state keeps from being dispatched, left so
// for longer than OpenRefusedAge: run:blocked after a refused close-out or an
// exhausted attempt count, run:stopped or run:stuck after a session was lost,
// run:landed when only its close was lost; also a claim a refused close-out
// kept (in progress, run:blocked). It holds no claim a dispatch tick gives
// back, so it waits for the one command that goes on.
type OpenRefused struct {
	Story StoryDetail
	// Run is the run state it carries: blocked, stopped, stuck or landed.
	Run string
	// Idle is how long since the tracker last recorded a change to it.
	Idle time.Duration
}

// Command is the one command that goes on with it: mw next closes a story that
// landed, mw retry frees any other.
func (o OpenRefused) Command() string {
	if o.Run == RunLanded {
		return "mw next " + o.Story.Story.ID
	}
	return "mw retry " + o.Story.Story.ID
}

// Alarm is the DAMPERS line for the story: its run state, its age and Command.
func (o OpenRefused) Alarm() DamperAlarm { return o.alarm("") }

// AlarmDown is Alarm for a claim whose holding host does not answer, down being
// what mw status says of it: any run state but landed is freed from elsewhere
// by force and re-pathed, as a retry run on that host cannot be (mw-gq6.1515).
func (o OpenRefused) AlarmDown(down string) DamperAlarm { return o.alarm(down) }

func (o OpenRefused) alarm(down string) DamperAlarm {
	command := o.Command()
	if down != "" {
		if o.Run != RunLanded {
			command = fromElsewhereCure(o.Story.Story.ID)
		}
		command = down + ", so from elsewhere: " + command
	}
	return DamperAlarm{
		Principle: DamperOpenRefused,
		Text: fmt.Sprintf("open with %s:%s for %s and no dispatch tick takes it; to go on: %s",
			RunState, o.Run, elapsed(o.Idle), command),
		Beads:    []string{o.Story.Story.ID},
		Identity: DamperOpenRefused + ":" + o.Story.Story.ID,
	}
}

// OpenRefusedStories are the stories ready or open and claimed (neither is
// running), and the claims a refused close-out kept (in progress, run:blocked),
// that carry a run state mw dispatch passes over, last changed more than
// OpenRefusedAge before now, oldest first. A parent (an epic, or a bead with
// children), a story of his own (hitl) and a story whose age the tracker did not
// say are left out.
func OpenRefusedStories(work WorkInHand, now time.Time) []OpenRefused {
	var found []OpenRefused
	seen := map[string]bool{}
	for _, list := range [][]StoryDetail{work.Ready, work.OpenClaimed, work.Running} {
		for _, d := range list {
			if seen[d.Story.ID] || d.IsParent() || d.Hitl() {
				continue
			}
			// A claim a refused close-out kept is in progress (run:blocked): no
			// session works it, and only a retry frees it (mw-gq6.1166). Any other
			// run state on a story in progress is a session still working.
			inProgress := strings.EqualFold(d.Status, StatusInProgress)
			if !inProgress && !strings.EqualFold(d.Status, StatusOpen) {
				continue
			}
			seen[d.Story.ID] = true
			run := ""
			for _, state := range []string{RunBlocked, RunStopped, RunStuck, RunLanded} {
				if hasLabel(d.Labels, RunState+":"+state) {
					run = state
					break
				}
			}
			if inProgress && run != RunBlocked {
				run = ""
			}
			// A harness stop mw dispatch is to give back, its time not yet come, is
			// waiting as it was told to, not refused (mw-gq6.1382).
			if harnessStopPending(d.Labels, now) {
				continue
			}
			touched := firstKnown(d.Updated, d.Created)
			if run == "" || touched.IsZero() || now.Sub(touched) <= OpenRefusedAge {
				continue
			}
			found = append(found, OpenRefused{Story: d, Run: run, Idle: now.Sub(touched)})
		}
	}
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].Idle != found[b].Idle {
			return found[a].Idle > found[b].Idle
		}
		return found[a].Story.Story.ID < found[b].Story.Story.ID
	})
	return found
}

func (x StuckSurvey) now() time.Time {
	if x.Now != nil {
		return x.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}

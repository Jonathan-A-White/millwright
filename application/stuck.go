package application

import (
	"context"
	"fmt"
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

// StuckClaimAge is how long a claim may have had no session behind it (its
// lease run out with no heartbeat since) before mw stuck counts it a root.
const StuckClaimAge = 6 * time.Hour

// HoldReasonLabels are the labels that say why a bead is held. A label starting
// LabelHitlKindPrefix counts as LabelHitl.
func HoldReasonLabels() []string {
	return []string{"grilling", LabelHitl, LabelDemo, LabelIcebox}
}

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
	if len(r.Roots) == 0 {
		return "nothing is stuck: no live bead waits behind a held bead that cannot move.\n"
	}
	var b strings.Builder
	b.WriteString("stuck: " + r.Summary() + "\n")
	for _, root := range r.Roots {
		fmt.Fprintf(&b, "%s (%s): %d behind: %s\n", root.ID, root.Why, len(root.Behind), strings.Join(root.Behind, " "))
	}
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
//     opening HeldCommentPrefix.
//   - A claimed bead whose lease ran out more than StuckClaimAge ago, with no
//     heartbeat since: a claim with no session. A claim bd shows no lease for is
//     not listed, as StaleClaims does not list it.
//
// A root is listed only when some live bead (open or in progress, not an epic)
// waits behind it over 'blocks' links, through live beads only. It reads and
// writes nothing.
type StuckSurvey struct {
	Graph    BeadGraph
	Comments StuckComments
	Claims   StuckClaims
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Find reads the beads and reports the roots, in id order.
func (s StuckSurvey) Find(ctx context.Context) (StuckReport, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
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
			if live := behind(b.ID); len(live) > 0 {
				maybe = append(maybe, StuckRoot{ID: b.ID, Why: "held, no reason written", Behind: live})
			}
		}
	}
	if len(maybe) > 0 {
		ids := make([]string, len(maybe))
		for n, root := range maybe {
			ids[n] = root.ID
		}
		said, err := s.Comments.StoriesComments(ctx, ids)
		if err != nil {
			return StuckReport{}, fmt.Errorf("reading the comments of the held beads: %w", err)
		}
		for _, root := range maybe {
			if !holdsAReason(said[root.ID]) {
				roots = append(roots, root)
			}
		}
	}
	if s.Claims != nil {
		stale, err := s.Claims.StaleClaims(ctx, now.Add(-StuckClaimAge))
		if err != nil {
			return StuckReport{}, fmt.Errorf("reading the claims with no session: %w", err)
		}
		for _, claim := range stale {
			if live := behind(claim.Story.ID); len(live) > 0 {
				roots = append(roots, StuckRoot{ID: claim.Story.ID, Why: "claimed, no session for over 6 hours", Behind: live})
			}
		}
	}
	sort.Slice(roots, func(a, b int) bool { return roots[a].ID < roots[b].ID })
	return StuckReport{Roots: roots}, nil
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

func holdsAReason(comments []Comment) bool {
	return slices.ContainsFunc(comments, func(c Comment) bool {
		return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(c.Text)), HeldCommentPrefix)
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

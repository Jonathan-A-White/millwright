package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// GroomWindow is the week a groom reads: the landings of the last seven days
// are the week's, and the fix budget is worked out from them.
const GroomWindow = 7 * 24 * time.Hour

// GroomIceAge is how long an auto-found bead may stay held before the groom
// names it a candidate to ice.
const GroomIceAge = 21 * 24 * time.Hour

// LabelHisReport marks a bead that is the Governor's own report: the groom never
// offers it to be iced, as it never offers a bead labelled HisPriorityLabel.
const LabelHisReport = "his-report"

// GroomSource is what mw groom reads: every story with its times and path, the
// comments of held ones, and the graph of every bead with its links. It has no
// write method.
type GroomSource interface {
	TesterStories
	BeadGraph
}

// PacedList is a list of story ids released at a pace (mw release-paced): the
// list file's name and its ids in release order.
type PacedList struct {
	Name string   `json:"name"`
	IDs  []string `json:"-"`
}

// PacedLists finds the paced lists the vault keeps.
type PacedLists interface {
	PacedLists(ctx context.Context) ([]PacedList, error)
}

// Groom is `mw groom <rig>`: the weekly grooming list of one rig, from one read
// of the tracker. It prints the rig's FEATURES against its target and floor, its
// fix budget, the epics that are done and still open or wait on a demo alone,
// the held beads with live beads behind them and no reason, the features whose
// root blocker is a bug, the auto-found beads held long enough to ice, the paced
// lists with nothing open, and the fold groups with found-at-close beads (by the
// directory of their files key) and [taste] ones in. It runs no model and
// writes nothing (mw-6ww.119.10.7).
type Groom struct {
	Source GroomSource
	Rig    string
	// Rules gives the rig's feature_share and feature_floor; nil reads neither.
	Rules EpicRules
	// Lists finds the paced lists; nil lists none.
	Lists PacedLists
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// JSON prints the report as JSON in place of text.
	JSON bool
	Out  io.Writer
}

// GroomBead is a bead a list names.
type GroomBead struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Why is what the list says of it, when it says more than its heading does.
	Why string `json:"why,omitempty"`
}

// GroomFeatures is the rig's landings of the week by lane (the FEATURES line of
// mw report), against its target and floor.
type GroomFeatures struct {
	FeatureShare
	// Target is the rig's feature_share as a percent; 0 when the rig keeps none.
	Target int `json:"target"`
	// Floor is the rig's feature_floor; 0 when it keeps none.
	Floor int `json:"floor"`
	// Ready is how many feature-lane stories below P1 are ready now.
	Ready int `json:"ready"`
}

// GroomBudget is the week's fix budget: (1 - share) of the week's non-express
// landings, against the fixes landed, running and ready.
type GroomBudget struct {
	Share float64 `json:"share"`
	// Budget is nil when the rig keeps no feature_share.
	Budget     *int `json:"budget"`
	NonExpress int  `json:"non_express_landings"`
	Landed     int  `json:"landed"`
	Running    int  `json:"running"`
	Ready      int  `json:"ready"`
	Total      int  `json:"total"`
}

// GroomEpics are the epics whose work is over, and still open.
type GroomEpics struct {
	// DoneStillOpen are the open epics every child of which is closed.
	DoneStillOpen []GroomBead `json:"done_still_open"`
	// OnlyDemo are the open epics whose only unfinished children are demos.
	OnlyDemo []GroomBead `json:"only_demo"`
}

// GroomHeld is a held bead with no written reason that live beads wait behind.
type GroomHeld struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Behind int    `json:"behind"`
}

// GroomBlocked is a feature that waits, through other beads or at once, on a bug.
type GroomBlocked struct {
	Feature      string `json:"feature"`
	FeatureTitle string `json:"feature_title"`
	Bug          string `json:"bug"`
	BugTitle     string `json:"bug_title"`
}

// GroomPaced is a paced list with nothing open: none of its stories is open or
// in progress.
type GroomPaced struct {
	Name    string `json:"name"`
	Held    int    `json:"held"`
	Iced    int    `json:"iced"`
	Closed  int    `json:"closed"`
	Missing int    `json:"missing"`
}

// GroomReport is what mw groom found: eight lists, each present when empty.
type GroomReport struct {
	Features        GroomFeatures  `json:"features"`
	FixBudget       GroomBudget    `json:"fix_budget"`
	Epics           GroomEpics     `json:"epics"`
	HeldNoReason    []GroomHeld    `json:"held_no_reason"`
	BlockedFeatures []GroomBlocked `json:"blocked_features"`
	IceCandidates   []GroomBead    `json:"ice_candidates"`
	PacedLists      []GroomPaced   `json:"paced_lists"`
	FoldGroups      []FoldGroup    `json:"fold_groups"`
}

// Run reads the tracker once and prints the lists.
func (g Groom) Run(ctx context.Context) (GroomReport, error) {
	report := GroomReport{
		Epics:        GroomEpics{DoneStillOpen: []GroomBead{}, OnlyDemo: []GroomBead{}},
		HeldNoReason: []GroomHeld{}, BlockedFeatures: []GroomBlocked{}, IceCandidates: []GroomBead{},
		PacedLists: []GroomPaced{}, FoldGroups: []FoldGroup{},
	}
	if g.Source == nil {
		return report, fmt.Errorf("grooming %s: there is no work tracker to read", g.Rig)
	}
	now := time.Now() //mw:realclock the default of the injected Now, used where none is set
	if g.Now != nil {
		now = g.Now()
	}
	stories, err := g.Source.StoriesSince(ctx, time.Time{})
	if err != nil {
		return report, fmt.Errorf("grooming %s: reading the stories: %w", g.Rig, err)
	}
	graph, err := g.Source.BeadGraph(ctx)
	if err != nil {
		return report, fmt.Errorf("grooming %s: reading the beads: %w", g.Rig, err)
	}
	var share float64
	if g.Rules != nil {
		required, err := g.Rules.EpicRequirements(ctx, g.Rig)
		if err != nil {
			return report, fmt.Errorf("grooming %s: reading the rig's file: %w", g.Rig, err)
		}
		share = required.FeatureShare
		report.Features.Floor = required.FeatureFloor
	}
	w := groomWorld{rig: g.Rig, now: now, stories: map[string]StoryDetail{}, graph: map[string]domain.GraphBead{}}
	for _, d := range stories {
		w.stories[d.Story.ID] = d
	}
	for _, b := range graph {
		w.graph[b.ID] = b
	}
	w.waited = laneWaits(graph)

	w.landings(&report, share)
	if report.Epics, err = w.epics(graph); err != nil {
		return report, err
	}
	if report.HeldNoReason, err = w.heldNoReason(ctx, g.Source, graph); err != nil {
		return report, err
	}
	report.BlockedFeatures = w.blockedFeatures(stories)
	report.IceCandidates = w.iceCandidates(stories)
	if g.Lists != nil {
		lists, err := g.Lists.PacedLists(ctx)
		if err != nil {
			return report, fmt.Errorf("grooming %s: reading the paced lists: %w", g.Rig, err)
		}
		report.PacedLists = w.pacedLists(lists)
	}
	_, report.FoldGroups = foldGroups(stories, g.Rig, DefaultFoldMin, true)

	if g.Out != nil {
		if g.JSON {
			body, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				return report, err
			}
			fmt.Fprintf(g.Out, "%s\n", body)
		} else {
			report.print(g.Out, g.Rig, share)
		}
	}
	return report, nil
}

// groomWorld is what the groom reads, indexed.
type groomWorld struct {
	rig     string
	now     time.Time
	stories map[string]StoryDetail
	graph   map[string]domain.GraphBead
	// waited are the stories a feature waits on, directly or through others.
	waited map[string]bool
}

func (w groomWorld) ofRig(d StoryDetail) bool { return d.Merged().Rig == w.rig }

// landed is whether a closed story of the rig is a landing of the week: closed
// inside the window, not a Tester story, a formula step, a superseded or a
// never-released bead (ClosedStory.landed).
func (w groomWorld) landed(d StoryDetail) bool {
	if !d.Closed() || !w.ofRig(d) || d.IsParent() || IsTesterStory(d) || d.ClosedAt.IsZero() ||
		d.ClosedAt.Before(w.now.Add(-GroomWindow)) || d.ClosedAt.After(w.now) {
		return false
	}
	kind := d.Type
	if kind == "" {
		kind = "task"
	}
	return ClosedStory{ID: d.Story.ID, Title: d.Story.Title, Type: kind, CloseReason: d.CloseReason}.landed()
}

// unfinished is whether a bead still waits on its blocker.
func (w groomWorld) unfinished(id string) bool {
	b, known := w.graph[id]
	return known && !strings.EqualFold(b.Status, StatusClosed)
}

// ready is whether a story is open, unclaimed and waits on nothing unfinished.
func (w groomWorld) ready(d StoryDetail) bool {
	if !strings.EqualFold(strings.TrimSpace(d.Status), StatusOpen) || strings.TrimSpace(d.Assignee) != "" {
		return false
	}
	for _, blocker := range blockedOn(w.graph[d.Story.ID]) {
		if w.unfinished(blocker) {
			return false
		}
	}
	return true
}

func (w groomWorld) landings(report *GroomReport, share float64) {
	var f FeatureShare
	f.Rig = w.rig
	var running, ready, readyFeatures int
	for _, d := range w.stories {
		if w.landed(d) {
			f.Landed++
			switch kind := StoryKind(d); {
			case d.Priority <= ExpressPriority:
				f.Express++
			case IsFeatureKind(kind):
				f.Features++
				if kind == KindContent {
					f.Content++
				}
			default:
				f.Fixes++
			}
			continue
		}
		if d.Closed() || !w.ofRig(d) || !shareEligible(d) || IsTesterStory(d) {
			continue
		}
		feature := inFeatureLane(d, w.waited)
		switch {
		case strings.EqualFold(strings.TrimSpace(d.Status), StatusInProgress):
			if !feature {
				running++
			}
		case w.ready(d):
			if feature {
				readyFeatures++
			} else {
				ready++
			}
		}
	}
	f.Percent = int(percentOf(f.Features, f.Landed) + 0.5)
	report.Features.FeatureShare = f
	report.Features.Target = int(math.Round(share * 100))
	report.Features.Ready = readyFeatures

	nonExpress := f.Landed - f.Express
	b := GroomBudget{Share: share, NonExpress: nonExpress, Landed: f.Fixes, Running: running, Ready: ready, Total: f.Fixes + running + ready}
	if share > 0 {
		budget := int(math.Round((1 - share) * float64(nonExpress)))
		b.Budget = &budget
	}
	report.FixBudget = b
}

// epicRig is the rig an epic belongs to: the rig of its stories, found through
// its children (an epic of epics through theirs); "" when none says.
func epicRig(id string, children map[string][]string, stories map[string]StoryDetail, seen map[string]bool) []string {
	if seen[id] {
		return nil
	}
	seen[id] = true
	var rigs []string
	for _, child := range children[id] {
		if d, ok := stories[child]; ok {
			rigs = append(rigs, d.Merged().Rig)
			continue
		}
		rigs = append(rigs, epicRig(child, children, stories, seen)...)
	}
	return rigs
}

// epics lists the rig's open epics whose children are all closed, and those
// whose only unfinished children are demos.
func (w groomWorld) epics(graph []domain.GraphBead) (GroomEpics, error) {
	out := GroomEpics{DoneStillOpen: []GroomBead{}, OnlyDemo: []GroomBead{}}
	children := map[string][]string{}
	for _, b := range graph {
		for _, edge := range b.Edges {
			if edge.Kind == domain.EdgeParentChild {
				children[edge.To] = append(children[edge.To], b.ID)
			}
		}
	}
	for _, b := range graph {
		if b.Type != "epic" || !b.IsOpen() || len(children[b.ID]) == 0 {
			continue
		}
		if !slices.Contains(epicRig(b.ID, children, w.stories, map[string]bool{}), w.rig) {
			continue
		}
		var unfinished, demos int
		for _, child := range children[b.ID] {
			c, known := w.graph[child]
			if !known || strings.EqualFold(c.Status, StatusClosed) {
				continue
			}
			unfinished++
			if hasLabel(c.Labels, LabelDemo) {
				demos++
			}
		}
		switch {
		case unfinished == 0:
			out.DoneStillOpen = append(out.DoneStillOpen, GroomBead{ID: b.ID, Title: b.Title,
				Why: fmt.Sprintf("all %d children closed", len(children[b.ID]))})
		case demos == unfinished:
			out.OnlyDemo = append(out.OnlyDemo, GroomBead{ID: b.ID, Title: b.Title,
				Why: fmt.Sprintf("%d of %d children left, %s", unfinished, len(children[b.ID]), plural(demos, "a demo", "all demos"))})
		}
	}
	return out, nil
}

// heldNoReason is what mw stuck calls a held root with no reason written, of
// this rig.
func (w groomWorld) heldNoReason(ctx context.Context, source GroomSource, graph []domain.GraphBead) ([]GroomHeld, error) {
	held := []GroomHeld{}
	stuck, err := StuckSurvey{Graph: groomGraph(graph), Comments: source}.Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("grooming %s: the held beads: %w", w.rig, err)
	}
	for _, root := range stuck.Roots {
		d, known := w.stories[root.ID]
		if !known || !w.ofRig(d) || root.Why != stuckNoReason {
			continue
		}
		held = append(held, GroomHeld{ID: root.ID, Title: d.Story.Title, Behind: len(root.Behind)})
	}
	return held, nil
}

// groomGraph is the graph the groom already read, offered as a BeadGraph so the
// stuck survey reads no more.
type groomGraph []domain.GraphBead

func (g groomGraph) BeadGraph(context.Context) ([]domain.GraphBead, error) { return g, nil }

// blockedFeatures are the open features of the rig that wait, through the
// beads between, on a bug at the root of the wait: a bead nothing unfinished
// is before it.
func (w groomWorld) blockedFeatures(stories []StoryDetail) []GroomBlocked {
	out := []GroomBlocked{}
	for _, d := range stories {
		if d.Closed() || !w.ofRig(d) || d.IsParent() || !IsFeatureKind(StoryKind(d)) {
			continue
		}
		seen := map[string]bool{d.Story.ID: true}
		queue := []string{d.Story.ID}
		var bugs []string
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			var waitsOn []string
			for _, blocker := range blockedOn(w.graph[at]) {
				if w.unfinished(blocker) {
					waitsOn = append(waitsOn, blocker)
				}
			}
			if len(waitsOn) == 0 && at != d.Story.ID {
				if root, ok := w.stories[at]; ok && StoryKind(root) == KindBug {
					bugs = append(bugs, at)
				}
			}
			for _, blocker := range waitsOn {
				if !seen[blocker] {
					seen[blocker] = true
					queue = append(queue, blocker)
				}
			}
		}
		sort.Strings(bugs)
		for _, bug := range bugs {
			out = append(out, GroomBlocked{Feature: d.Story.ID, FeatureTitle: d.Story.Title, Bug: bug, BugTitle: w.stories[bug].Story.Title})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Feature != out[j].Feature {
			return out[i].Feature < out[j].Feature
		}
		return out[i].Bug < out[j].Bug
	})
	return out
}

// autoFound is whether a bead was filed by a machine: a close-out's finding, a
// Tester's finding (a bug with a class label) or a [taste] story, not by the
// Governor and not by the Mayor's hand.
func autoFound(d StoryDetail) bool {
	if hasLabel(d.Labels, LabelFoundAtClose) || StoryKind(d) == KindTaste {
		return true
	}
	for _, label := range d.Labels {
		if strings.HasPrefix(label, ClassLabelPrefix) {
			return true
		}
	}
	return false
}

// iceCandidates are the rig's auto-found beads held for GroomIceAge or more: not
// iced, not the Governor's (his-report, his-priority, hitl), and not one a
// feature waits on.
func (w groomWorld) iceCandidates(stories []StoryDetail) []GroomBead {
	out := []GroomBead{}
	for _, d := range stories {
		if !d.Held() || d.Iced() || !w.ofRig(d) || !autoFound(d) || d.Hitl() || d.Created.IsZero() ||
			w.now.Sub(d.Created) < GroomIceAge || w.waited[d.Story.ID] ||
			hasLabel(d.Labels, LabelHisReport) || hasLabel(d.Labels, HisPriorityLabel) {
			continue
		}
		out = append(out, GroomBead{ID: d.Story.ID, Title: d.Story.Title, Why: "held " + elapsed(w.now.Sub(d.Created))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// pacedLists are the lists with a story of the rig and none open or in progress.
func (w groomWorld) pacedLists(lists []PacedList) []GroomPaced {
	out := []GroomPaced{}
	for _, list := range lists {
		var p GroomPaced
		p.Name = list.Name
		open, mine := 0, false
		for _, id := range listIDs(list.IDs) {
			d, known := w.stories[id]
			switch {
			case !known:
				p.Missing++
			case d.Closed():
				p.Closed++
				mine = mine || w.ofRig(d)
			case d.Held() && d.Iced():
				p.Iced++
				mine = mine || w.ofRig(d)
			case d.Held():
				p.Held++
				mine = mine || w.ofRig(d)
			default:
				open++
				mine = mine || w.ofRig(d)
			}
		}
		if mine && open == 0 {
			out = append(out, p)
		}
	}
	return out
}

// foldsWhenWide is whether the groom's fold counts a story that mw fold leaves
// out: a close-out's finding, or a [taste] story.
func foldsWhenWide(d StoryDetail) bool {
	return hasLabel(d.Labels, LabelFoundAtClose) || StoryKind(d) == KindTaste
}

// groomFoldKey is the key the groom groups a bug by: its class:<slug> label, else
// for a found-at-close bead the directory of its files key, else the screen its
// title names, else empty.
func groomFoldKey(d StoryDetail, rig string) string {
	for _, label := range d.Labels {
		if strings.HasPrefix(label, ClassLabelPrefix) && strings.TrimSpace(strings.TrimPrefix(label, ClassLabelPrefix)) != "" {
			return label
		}
	}
	if hasLabel(d.Labels, LabelFoundAtClose) {
		if dir := filesDir(d.Merged().Files); dir != "" {
			return groomDirPrefix + dir
		}
	}
	return foldKey(d, rig)
}

const groomDirPrefix = "dir:"

// filesDir is the directory of the first path or glob of a files key: 'application'
// for 'application/a.go,application/a_test.go', and the entry itself when it
// ends in a slash; '.' for a file in the root; "" for no files.
func filesDir(files string) string {
	first, _, _ := strings.Cut(files, ",")
	first = strings.TrimSpace(first)
	switch {
	case first == "":
		return ""
	case strings.HasSuffix(first, "/"):
		return strings.TrimRight(first, "/")
	}
	return path.Dir(first)
}

// print writes the report as text, a heading and its lines for each list.
func (r GroomReport) print(out io.Writer, rig string, share float64) {
	f := r.Features
	fmt.Fprintf(out, "groom %s: the last %d days\n\n", rig, int(GroomWindow/(24*time.Hour)))
	fmt.Fprintln(out, f.Line())
	if share > 0 {
		fmt.Fprintf(out, "  target %d%%: %s\n", f.Target, underOrOn(f.Percent, f.Target))
	} else {
		fmt.Fprintln(out, "  no feature_share in the rig's file: no target")
	}
	if f.Floor > 0 {
		fmt.Fprintf(out, "  feature floor %d: %d ready%s\n", f.Floor, f.Ready, map[bool]string{true: ", under it: carve or release a feature before a fix"}[f.Ready < f.Floor])
	} else {
		fmt.Fprintln(out, "  no feature_floor in the rig's file")
	}
	b := r.FixBudget
	if b.Budget != nil {
		fmt.Fprintf(out, "%s fixes this week: landed %d + running %d + ready %d = %d of budget %d%s\n",
			rig, b.Landed, b.Running, b.Ready, b.Total, *b.Budget, map[bool]string{true: " (over: release none)"}[b.Total > *b.Budget])
	} else {
		fmt.Fprintf(out, "%s fixes this week: landed %d + running %d + ready %d = %d; no feature_share, so no budget\n", rig, b.Landed, b.Running, b.Ready, b.Total)
	}

	section := func(heading string, n int) bool {
		fmt.Fprintf(out, "\n%s (%d)\n", heading, n)
		return n > 0
	}
	section("EPICS done, still open", len(r.Epics.DoneStillOpen))
	for _, e := range r.Epics.DoneStillOpen {
		fmt.Fprintf(out, "  %s  %s: %s\n", e.ID, e.Title, e.Why)
	}
	section("EPICS waiting only on a demo", len(r.Epics.OnlyDemo))
	for _, e := range r.Epics.OnlyDemo {
		fmt.Fprintf(out, "  %s  %s: %s\n", e.ID, e.Title, e.Why)
	}
	section("HELD with live beads behind and no HELD: reason", len(r.HeldNoReason))
	for _, h := range r.HeldNoReason {
		fmt.Fprintf(out, "  %s  %s: %d behind\n", h.ID, h.Title, h.Behind)
	}
	section("FEATURES waiting on a bug", len(r.BlockedFeatures))
	for _, bl := range r.BlockedFeatures {
		fmt.Fprintf(out, "  %s  %s: waits on bug %s  %s\n", bl.Feature, bl.FeatureTitle, bl.Bug, bl.BugTitle)
	}
	section(fmt.Sprintf("ICE candidates, auto-found and held %d days or more", int(GroomIceAge/(24*time.Hour))), len(r.IceCandidates))
	for _, c := range r.IceCandidates {
		fmt.Fprintf(out, "  %s  %s: %s\n", c.ID, c.Title, c.Why)
	}
	section("PACED lists with nothing open", len(r.PacedLists))
	for _, p := range r.PacedLists {
		fmt.Fprintf(out, "  %s: %d held, %d iced, %d closed, %d not found\n", p.Name, p.Held, p.Iced, p.Closed, p.Missing)
	}
	section(fmt.Sprintf("FOLD groups of %d or more", DefaultFoldMin), len(r.FoldGroups))
	for _, g := range r.FoldGroups {
		fmt.Fprintf(out, "  %s (%d)\n", g.Key, len(g.Bugs))
		for _, bug := range g.Bugs {
			mark := ""
			if bug.InProgress {
				mark = "  [in progress: do not fold]"
			}
			fmt.Fprintf(out, "    %s  %s%s\n", bug.ID, bug.Title, mark)
		}
	}
}

func underOrOn(percent, target int) string {
	if percent >= target {
		return fmt.Sprintf("FEATURES %d%%, on it", percent)
	}
	return fmt.Sprintf("FEATURES %d%%, %d points under", percent, target-percent)
}

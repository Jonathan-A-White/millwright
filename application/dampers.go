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

// DamperLimits are the thresholds the DAMPERS section trips on, each a config
// key of the [dampers] table; a zero field reads its default.
type DamperLimits struct {
	// ReadyP1 is how many ready P1 stories there may be before the WIP alarm.
	ReadyP1 int
	// P1UntouchedDays is how many days a P1 story may go unchanged.
	P1UntouchedDays int
	// DemoWaitingDays is how many days a landed demo may wait for him.
	DemoWaitingDays int
	// GrillingDays is how many days a grilling may stay open with no card.
	GrillingDays int
	// TestersPerRig is how many Tester stories may run on one rig at once.
	TestersPerRig int
	// ReviewQueue is how many open cards, open demos and unrun hands steps he
	// may have waiting.
	ReviewQueue int
}

// The defaults of DamperLimits.
const (
	DefaultDamperReadyP1         = 10
	DefaultDamperP1UntouchedDays = 5
	DefaultDamperDemoWaitingDays = 7
	DefaultDamperGrillingDays    = 7
	DefaultDamperTestersPerRig   = 1
	DefaultDamperReviewQueue     = 8
)

// DamperFacts is everything the alarms are judged from, all read already.
type DamperFacts struct {
	Now time.Time
	// Work is the stories ready and running on every host.
	Work WorkInHand
	// Graph is every bead the tracker holds, read for its open grillings.
	Graph []domain.GraphBead
	// Notes are the tracker's notes, read for his open cards and unrun hands steps.
	Notes map[string]string
	// Starved are the rigs the hosts' dispatches note they hold back whole.
	Starved []StarvedRig
	// Stuck are the held roots that live beads wait behind (mw stuck).
	Stuck StuckReport
}

// DamperAlarm is one tripped alarm: the principle in a word (WIP, ageing,
// review), what is wrong, and the beads, all of them.
type DamperAlarm struct {
	Principle string
	Text      string
	Beads     []string
}

// DampersReport is what the alarms found, with the counts mw doctor repeats.
type DampersReport struct {
	Alarms []DamperAlarm
	Counts DamperCounts
	Limits DamperLimits
}

// DamperCounts are the numbers behind the alarms.
type DamperCounts struct {
	ReadyP1       int
	P1Untouched   int
	DemosWaiting  int
	GrillingsBare int
	TesterRigs    int
	ReviewQueue   int
	StarvedRigs   int
	// StuckLive and StuckRoots are the live beads behind held roots and the roots.
	StuckLive  int
	StuckRoots int
	// Inversions are the beads below the priority of what waits on them.
	Inversions int
}

// DampersHeading heads the section naming the alarms that are tripped: the
// factory's work in progress and ageing, and the review queue it is building
// for him. The report leaves the section out when none is.
const DampersHeading = "DAMPERS"

// The principle each alarm names, in a word.
const (
	DamperWIP    = "WIP"
	DamperAgeing = "ageing"
	DamperReview = "review"
	// DamperStuck is the alarm for live beads behind a held root that cannot move.
	DamperStuck = "stuck"
	// DamperInversion is the alarm for a bead below the priority of what waits on it.
	DamperInversion = "inversion"
	// DamperMetered is the alarm for ready stories pinned to this host, on a rig
	// that installs dependencies, while the network is metered: dispatch passes
	// them, and only host=auto lets another host take them (mw-gq6.702).
	DamperMetered = "metered"
)

// damperNamed is how many beads an alarm's line names before it counts the rest.
const damperNamed = 5

func (l DamperLimits) orDefaults() DamperLimits {
	for _, one := range []struct {
		have *int
		want int
	}{
		{&l.ReadyP1, DefaultDamperReadyP1}, {&l.P1UntouchedDays, DefaultDamperP1UntouchedDays},
		{&l.DemoWaitingDays, DefaultDamperDemoWaitingDays}, {&l.GrillingDays, DefaultDamperGrillingDays},
		{&l.TestersPerRig, DefaultDamperTestersPerRig}, {&l.ReviewQueue, DefaultDamperReviewQueue},
	} {
		if *one.have <= 0 {
			*one.have = one.want
		}
	}
	return l
}

// Line is the alarm as one report line: the principle, what is wrong, and up
// to five beads, then how many more.
func (a DamperAlarm) Line() string {
	line := a.Principle + " · " + a.Text
	if a.Principle == DamperStuck || a.Principle == DamperInversion || a.Principle == DamperMetered || a.Principle == DamperNoHost {
		line = a.Principle + ": " + a.Text
	}
	if len(a.Beads) == 0 {
		return line
	}
	named := a.Beads
	// Every story the metered alarm names is one the Mayor repaths, and no other
	// command lists them, so it is never cut.
	if len(named) > damperNamed && a.Principle != DamperMetered {
		named = named[:damperNamed]
	}
	line += ": " + strings.Join(named, " ")
	if rest := len(a.Beads) - len(named); rest > 0 {
		line += fmt.Sprintf(" +%d (all: %s)", rest, a.fullCommand())
	}
	return line
}

// fullCommand is the command that prints every bead of the alarm: mw stuck for
// the held roots, mw stuck --dampers for the rest.
func (a DamperAlarm) fullCommand() string {
	if a.Principle == DamperStuck {
		return "mw stuck"
	}
	return "mw stuck --dampers"
}

// FullList is every alarm with every bead it names, one a line under the
// alarm's own text, for mw stuck --dampers: the whole list that Line cuts at
// damperNamed. With no alarm tripped it is a single line saying so.
func (r DampersReport) FullList() string {
	if len(r.Alarms) == 0 {
		return "no damper alarm\n"
	}
	var out strings.Builder
	for _, alarm := range r.Alarms {
		head := alarm.Principle + " · " + alarm.Text
		if alarm.Principle == DamperStuck || alarm.Principle == DamperInversion {
			head = alarm.Principle + ": " + alarm.Text
		}
		out.WriteString(head + "\n")
		for _, bead := range alarm.Beads {
			out.WriteString("  " + bead + "\n")
		}
	}
	return out.String()
}

// MeteredPinned is the alarm for the ready stories pinned to host whose rig is
// in heavy: while the network is metered mw dispatch on that host passes them
// all (gate metered-network), and a pin to a home host lets no other take them.
// It is the zero alarm, with no beads, when none is. Epics and hands beads are
// never dispatched, so they are left out (mw-gq6.702).
func MeteredPinned(work WorkInHand, host string, heavy map[string]bool) (DamperAlarm, bool) {
	var stuck []string
	for _, d := range work.Ready {
		merged := d.Merged()
		if d.IsEpic || d.Hitl() || merged.Host == "" || merged.Host == domain.HostAuto || merged.Host != host || !heavy[merged.Rig] {
			continue
		}
		stuck = append(stuck, d.Story.ID)
	}
	if len(stuck) == 0 {
		return DamperAlarm{}, false
	}
	return DamperAlarm{
		Principle: DamperMetered,
		Text:      fmt.Sprintf("pinned to %s, metered, its rig installs dependencies: set host=auto", host),
		Beads:     stuck,
	}, true
}

// Write writes the DAMPERS section, with a blank line after it, or nothing
// when no alarm is tripped.
func (r DampersReport) Write(b *strings.Builder) {
	if len(r.Alarms) == 0 {
		return
	}
	clip(b, DampersHeading)
	for _, alarm := range r.Alarms {
		wrapInto(b, "  ", alarm.Line())
	}
	b.WriteString("\n")
}

// CountsLine is the counts against their limits on one line, for mw doctor.
func (r DampersReport) CountsLine() string {
	l, c := r.Limits.orDefaults(), r.Counts
	return fmt.Sprintf("dampers: ready P1 %d (limit %d), P1 untouched %d days or more %d, demos waiting %d days or more %d, grillings with no card %d, rigs with over %d Tester running %d, his review queue %d (limit %d)",
		c.ReadyP1, l.ReadyP1, l.P1UntouchedDays, c.P1Untouched, l.DemoWaitingDays, c.DemosWaiting, c.GrillingsBare, l.TestersPerRig, c.TesterRigs, c.ReviewQueue, l.ReviewQueue)
}

// ReadDampers judges the facts against the limits.
func ReadDampers(facts DamperFacts, limits DamperLimits) DampersReport {
	limits = limits.orDefaults()
	report := DampersReport{Limits: limits}
	days := func(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

	live := liveStories(facts.Work)

	var readyP1, untouched, demos, oldDemos []string
	for _, d := range facts.Work.Ready {
		if d.Priority == 1 && !d.IsEpic && !d.Hitl() {
			readyP1 = append(readyP1, d.Story.ID)
		}
	}
	for _, d := range live {
		if d.IsEpic || !workable(d) {
			continue
		}
		if touched := firstKnown(d.Updated, d.Created); d.Priority == 1 && !touched.IsZero() && facts.Now.Sub(touched) > days(limits.P1UntouchedDays) {
			untouched = append(untouched, d.Story.ID)
		}
	}
	for _, d := range QueuedDemos(facts.Work) {
		demos = append(demos, d.Story.ID)
		if filed := firstKnown(d.Created, d.Updated); !filed.IsZero() && facts.Now.Sub(filed) > days(limits.DemoWaitingDays) {
			oldDemos = append(oldDemos, d.Story.ID)
		}
	}

	cards, _, hands := reviewQueue(facts)
	var bare []string
	byID := map[string]domain.GraphBead{}
	for _, b := range facts.Graph {
		byID[b.ID] = b
	}
	for _, b := range facts.Graph {
		if !b.IsGrilling() || !b.IsOpen() || b.Created.IsZero() || facts.Now.Sub(b.Created) <= days(limits.GrillingDays) {
			continue
		}
		if !slices.Contains(cards, b.ID) && !hasHitlCardLinked(b, facts.Graph, byID) {
			bare = append(bare, b.ID)
		}
	}

	testers := map[string][]string{}
	for _, d := range facts.Work.Running {
		if IsTesterStory(d) {
			rig := d.Merged().Rig
			testers[rig] = append(testers[rig], d.Story.ID)
		}
	}
	var rigs []string
	for rig, ids := range testers {
		if len(ids) > limits.TestersPerRig {
			rigs = append(rigs, rig)
		}
	}
	sort.Strings(rigs)

	queue := append(append(slices.Clone(cards), demos...), hands...)

	report.Counts = DamperCounts{
		ReadyP1: len(readyP1), P1Untouched: len(untouched), DemosWaiting: len(oldDemos),
		GrillingsBare: len(bare), TesterRigs: len(rigs), ReviewQueue: len(queue),
	}
	add := func(principle, text string, beads []string) {
		report.Alarms = append(report.Alarms, DamperAlarm{Principle: principle, Text: text, Beads: beads})
	}
	// A rig with nothing running is the loudest: it comes first.
	starved := StarvedAlarms(facts.Starved, facts.Work, facts.Now)
	report.Alarms = append(report.Alarms, starved...)
	report.Counts.StarvedRigs = len(starved)
	if live := facts.Stuck.LiveBehind(); live > 0 {
		roots := make([]string, len(facts.Stuck.Roots))
		for n, root := range facts.Stuck.Roots {
			roots[n] = root.ID
		}
		report.Counts.StuckLive, report.Counts.StuckRoots = live, len(roots)
		add(DamperStuck, facts.Stuck.Summary(), roots)
	}
	if inverted := InversionsOf(facts.Graph); len(inverted) > 0 {
		report.Counts.Inversions = len(inverted)
		add(DamperInversion, InversionSummary(inverted), nil)
	}
	if len(readyP1) > limits.ReadyP1 {
		add(DamperWIP, fmt.Sprintf("%d ready P1 stories (limit %d)", len(readyP1), limits.ReadyP1), readyP1)
	}
	if len(untouched) > 0 {
		add(DamperAgeing, fmt.Sprintf("%d P1 untouched over %d days", len(untouched), limits.P1UntouchedDays), untouched)
	}
	if len(oldDemos) > 0 {
		add(DamperAgeing, fmt.Sprintf("%d demos waiting over %d days", len(oldDemos), limits.DemoWaitingDays), oldDemos)
	}
	if len(bare) > 0 {
		add(DamperAgeing, fmt.Sprintf("%d grillings open over %d days with no card", len(bare), limits.GrillingDays), bare)
	}
	for _, rig := range rigs {
		add(DamperWIP, fmt.Sprintf("%d Tester stories running on %s (limit %d)", len(testers[rig]), rig, limits.TestersPerRig), testers[rig])
	}
	if len(queue) > limits.ReviewQueue {
		add(DamperReview, fmt.Sprintf("his review queue is %d: %d cards, %d demos, %d hands steps (limit %d)",
			len(queue), len(cards), len(demos), len(hands), limits.ReviewQueue), uniqueStrings(queue))
	}
	return report
}

// TesterHold is why a ready Tester story is held: its rig already runs as many
// Tester stories as testers_per_rig allows (mw-gq6.546).
type TesterHold struct {
	Rig     string
	Running int
	Limit   int
}

// String is the hold as mw dispatch and mw status say it.
func (h TesterHold) String() string {
	return fmt.Sprintf("waiting: %s already runs %d Tester stories (testers_per_rig %d)", h.Rig, h.Running, h.Limit)
}

// testerHold says whether the ready story is a Tester story whose rig has limit
// Tester stories (zero reads the default) in running already, on any host.
func testerHold(running []StoryDetail, ready StoryDetail, limit int) (TesterHold, bool) {
	if !IsTesterStory(ready) {
		return TesterHold{}, false
	}
	limit = DamperLimits{TestersPerRig: limit}.orDefaults().TestersPerRig
	rig := ready.Merged().Rig
	count := 0
	for _, d := range running {
		if d.Story.ID != ready.Story.ID && IsTesterStory(d) && d.Merged().Rig == rig {
			count++
		}
	}
	return TesterHold{Rig: rig, Running: count, Limit: limit}, count >= limit
}

// hasHitlCardLinked reports whether an open bead for the Governor is linked to
// b, in either direction: the card a grilling's question was put on.
func hasHitlCardLinked(b domain.GraphBead, all []domain.GraphBead, byID map[string]domain.GraphBead) bool {
	card := func(other domain.GraphBead) bool {
		return other.IsOpen() && slices.ContainsFunc(other.Labels, func(l string) bool {
			return strings.EqualFold(l, LabelHitl) || strings.HasPrefix(strings.ToLower(l), LabelHitlKindPrefix)
		})
	}
	for _, edge := range b.Edges {
		if other, ok := byID[edge.To]; ok && card(other) {
			return true
		}
	}
	for _, other := range all {
		for _, edge := range other.Edges {
			if edge.To == b.ID && card(other) {
				return true
			}
		}
	}
	return false
}

// liveStories are the stories ready, running and claimed, in that order.
func liveStories(work WorkInHand) []StoryDetail {
	var live []StoryDetail
	live = append(live, work.Ready...)
	live = append(live, work.Running...)
	return append(live, work.OpenClaimed...)
}

// isQueuedDemo is whether d is a demo of his review queue: a workable story
// that is not an epic and carries the demo label.
func isQueuedDemo(d StoryDetail) bool {
	return !d.IsEpic && workable(d) && hasLabel(d.Labels, LabelDemo)
}

// QueuedDemos are the demos of his review queue among the stories ready,
// running and claimed: the one place the open demos are found, which the
// DAMPERS alarms, the sleep pass's needs-you.txt and mw verify match all read.
func QueuedDemos(work WorkInHand) []StoryDetail {
	var demos []StoryDetail
	for _, d := range liveStories(work) {
		if isQueuedDemo(d) {
			demos = append(demos, d)
		}
	}
	return demos
}

// reviewQueue is his review queue as mw stuck --dampers counts it, by bead: the
// cards still open, the live demos and the hands steps unrun, one entry for each
// item. The sleep pass's needs-you.txt reads it through here, so that it and his
// phone count the same things.
func reviewQueue(facts DamperFacts) (cards, demos, hands []string) {
	closed := closedBeads(facts.Graph)
	for _, d := range QueuedDemos(facts.Work) {
		demos = append(demos, d.Story.ID)
	}
	return damperCards(facts.Notes, closed), demos, damperHands(facts.Notes, closed)
}

// closedBeads are the ids of the beads the graph shows closed. A note outlives
// its bead, so the review queue leaves out what it finds there; a bead the graph
// does not hold is not in the set and is counted.
func closedBeads(graph []domain.GraphBead) map[string]bool {
	closed := map[string]bool{}
	for _, b := range graph {
		if strings.EqualFold(b.Status, StatusClosed) {
			closed[b.ID] = true
		}
	}
	return closed
}

// damperCards are the beads, not closed, with a question still marked open, from
// the postern.question.* notes, in key order.
func damperCards(notes map[string]string, closed map[string]bool) []string {
	var beads []string
	for _, key := range sortedKeys(notes) {
		if bead, ok := strings.CutPrefix(key, PosternQuestionKey("")); ok && !closed[bead] && strings.TrimSpace(notes[key]) != "" {
			beads = append(beads, bead)
		}
	}
	return beads
}

// damperHands are the beads, not closed, of the hands steps no run was recorded
// for, one entry for each step, in key order: the steps mw hands list prints.
func damperHands(notes map[string]string, closed map[string]bool) []string {
	var beads []string
	for _, key := range sortedKeys(notes) {
		bead, ok := strings.CutPrefix(key, HandsStepsKey(""))
		if !ok || closed[bead] {
			continue
		}
		if records, err := parseHandsSteps(notes[key]); err != nil || len(records) == 0 {
			continue
		}
		for _, step := range viewHandsSteps(bead, notes) {
			if step.Ran == nil && step.SupersededBy == "" && step.Retired == "" {
				beads = append(beads, bead)
			}
		}
	}
	return beads
}

// DamperSurvey reads the facts the alarms are judged from out of the tracker:
// the one reader mw status and mw doctor share.
type DamperSurvey struct {
	Tracker WorkTracker
	Notes   DamperNotes
	// Graph, when set, is read for the open grillings; nil judges none.
	Graph  BeadGraph
	Limits DamperLimits
	// Stuck, when set, finds the held roots live beads wait behind; nil finds
	// none. A stuck check that cannot be read costs the section only its stuck
	// line.
	Stuck StuckFinder
}

// Dampers reads the alarms from work already read.
func (s DamperSurvey) Dampers(ctx context.Context, work WorkInHand, now time.Time) (DampersReport, error) {
	facts := DamperFacts{Now: now, Work: work, Notes: map[string]string{}}
	if s.Notes != nil {
		for _, prefix := range []string{PosternQuestionKey(""), HandsStepsKey("")} {
			kept, err := s.Notes.NotesWithPrefix(ctx, prefix)
			if err != nil {
				return DampersReport{}, fmt.Errorf("reading the notes under %s: %w", prefix, err)
			}
			for key, value := range kept {
				facts.Notes[key] = value
			}
		}
	}
	starved, err := s.readStarved(ctx)
	if err != nil {
		return DampersReport{}, err
	}
	facts.Starved = starved
	if s.Graph != nil {
		graph, err := s.Graph.BeadGraph(ctx)
		if err != nil {
			return DampersReport{}, fmt.Errorf("reading the grillings: %w", err)
		}
		facts.Graph = graph
	}
	if s.Stuck != nil {
		if stuck, err := s.Stuck.Find(ctx); err == nil {
			facts.Stuck = stuck
		}
	}
	return ReadDampers(facts, s.Limits), nil
}

// DamperCounts reads the work itself and then the alarms, for mw doctor.
func (s DamperSurvey) DamperCounts(ctx context.Context) (DampersReport, error) {
	work, err := s.Tracker.WorkInHand(ctx)
	if err != nil {
		return DampersReport{}, fmt.Errorf("reading what is in hand: %w", err)
	}
	return s.Dampers(ctx, work, time.Now())
}

// Inversion is a bead waited on by a bead of higher priority: it goes later in
// the dispatch order than what it holds up.
type Inversion struct {
	ID string
	// Priority is the bead's own, 0 being the most urgent.
	Priority int
	// Raise is the highest priority among the live beads that wait on it,
	// directly or through live beads: the priority it would be raised to.
	Raise int
	// WaitedOnBy is the highest-priority live bead that waits on it, directly
	// or through live beads (the lowest id among equals).
	WaitedOnBy string
	// WaiterPriority is that bead's priority.
	WaiterPriority int
	// His is whether the bead carries the his-priority label: it is named, no
	// raise is proposed, and the Mayor questions it only on a card.
	His bool
}

// Text is the inversion as a line says it: 'mw-a.2 P2→P1', or 'mw-a.3 P3
// (his)' for a bead of his.
func (i Inversion) Text() string {
	if i.His {
		return fmt.Sprintf("%s P%d (his)", i.ID, i.Priority)
	}
	return fmt.Sprintf("%s P%d→P%d", i.ID, i.Priority, i.Raise)
}

// Line is the inversion as mw stuck --inversions prints it: Text, then the
// bead that waits on it, 'mw-a.2 P2→P1 waited on by mw-a.1 (P1)'.
func (i Inversion) Line() string {
	return fmt.Sprintf("%s waited on by %s (P%d)", i.Text(), i.WaitedOnBy, i.WaiterPriority)
}

// InversionLister lists every priority inversion for mw stuck --inversions:
// the whole list that the DAMPERS line cuts at inversionsNamed. It reads the
// beads and writes nothing.
type InversionLister struct {
	Graph BeadGraph
}

// List is one line for each inversion in id order, or a single line saying
// there is none.
func (l InversionLister) List(ctx context.Context) (string, error) {
	beads, err := l.Graph.BeadGraph(ctx)
	if err != nil {
		return "", fmt.Errorf("reading the beads for the inversions: %w", err)
	}
	found := InversionsOf(beads)
	if len(found) == 0 {
		return "no priority inversion\n", nil
	}
	var out strings.Builder
	for _, one := range found {
		out.WriteString(one.Line() + "\n")
	}
	return out.String(), nil
}

// inversionsNamed is how many inversions the DAMPERS line names before it
// counts the rest; the sleep pass's list names them all.
const inversionsNamed = 8

// InversionSummary is the inversions on one line for the DAMPERS section: the
// count, then the first inversionsNamed.
func InversionSummary(found []Inversion) string {
	named := found
	if len(named) > inversionsNamed {
		named = named[:inversionsNamed]
	}
	texts := make([]string, len(named))
	for n, one := range named {
		texts[n] = one.Text()
	}
	line := fmt.Sprintf("%d %s below what waits on %s: %s", len(found), plural(len(found), "bead", "beads"), plural(len(found), "it", "them"), strings.Join(texts, ", "))
	if rest := len(found) - len(named); rest > 0 {
		line += fmt.Sprintf(" +%d (all: mw stuck --inversions)", rest)
	}
	return line
}

// InversionsOf finds each open or held bead (not an epic) whose priority is
// below the highest priority among the live beads waiting on it, in id order.
// The beads that wait are found by mw stuck's walk (liveBehind): through live
// beads only, so a held bead in the middle of a chain cuts it. It reads nothing
// and proposes only: no priority is changed.
func InversionsOf(beads []domain.GraphBead) []Inversion {
	byID := make(map[string]domain.GraphBead, len(beads))
	dependents := map[string][]string{}
	for _, b := range beads {
		byID[b.ID] = b
		for _, blocker := range blockedOn(b) {
			dependents[blocker] = append(dependents[blocker], b.ID)
		}
	}
	var found []Inversion
	for _, b := range beads {
		open, held := strings.EqualFold(b.Status, StatusOpen), strings.EqualFold(b.Status, StatusHeld)
		if b.Type == "epic" || strings.Contains(b.ID, "-mol-") || !(open || held) || len(dependents[b.ID]) == 0 {
			continue
		}
		raise, waiter := b.Priority, ""
		for _, waiting := range liveBehind(byID, dependents, b.ID) {
			if byID[waiting].Priority < raise {
				raise, waiter = byID[waiting].Priority, waiting
			}
		}
		if raise < b.Priority {
			found = append(found, Inversion{ID: b.ID, Priority: b.Priority, Raise: raise, WaitedOnBy: waiter, WaiterPriority: raise, His: HasHisPriorityLabel(b.Labels)})
		}
	}
	sort.Slice(found, func(a, b int) bool { return found[a].ID < found[b].ID })
	return found
}

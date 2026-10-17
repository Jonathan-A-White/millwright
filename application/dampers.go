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
	if len(a.Beads) == 0 {
		return line
	}
	named := a.Beads
	if len(named) > damperNamed {
		named = named[:damperNamed]
	}
	line += ": " + strings.Join(named, " ")
	if rest := len(a.Beads) - len(named); rest > 0 {
		line += fmt.Sprintf(" +%d", rest)
	}
	return line
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

	var live []StoryDetail
	live = append(live, facts.Work.Ready...)
	live = append(live, facts.Work.Running...)
	live = append(live, facts.Work.OpenClaimed...)

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
		if hasLabel(d.Labels, LabelDemo) {
			demos = append(demos, d.Story.ID)
			if filed := firstKnown(d.Created, d.Updated); !filed.IsZero() && facts.Now.Sub(filed) > days(limits.DemoWaitingDays) {
				oldDemos = append(oldDemos, d.Story.ID)
			}
		}
	}

	cards := damperCards(facts.Notes)
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

	hands := damperHands(facts.Notes)
	queue := append(append(slices.Clone(cards), demos...), hands...)

	report.Counts = DamperCounts{
		ReadyP1: len(readyP1), P1Untouched: len(untouched), DemosWaiting: len(oldDemos),
		GrillingsBare: len(bare), TesterRigs: len(rigs), ReviewQueue: len(queue),
	}
	add := func(principle, text string, beads []string) {
		report.Alarms = append(report.Alarms, DamperAlarm{Principle: principle, Text: text, Beads: beads})
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

// damperCards are the beads with a question still marked open, from the
// postern.question.* notes, in key order.
func damperCards(notes map[string]string) []string {
	var beads []string
	for _, key := range sortedKeys(notes) {
		if bead, ok := strings.CutPrefix(key, PosternQuestionKey("")); ok && strings.TrimSpace(notes[key]) != "" {
			beads = append(beads, bead)
		}
	}
	return beads
}

// damperHands are the beads of the hands steps no run was recorded for, one
// entry for each step, in key order.
func damperHands(notes map[string]string) []string {
	var beads []string
	for _, key := range sortedKeys(notes) {
		bead, ok := strings.CutPrefix(key, HandsStepsKey(""))
		if !ok {
			continue
		}
		if records, err := parseHandsSteps(notes[key]); err != nil || len(records) == 0 {
			continue
		}
		for _, step := range viewHandsSteps(bead, notes) {
			if step.Ran == nil && step.SupersededBy == "" {
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
	if s.Graph != nil {
		graph, err := s.Graph.BeadGraph(ctx)
		if err != nil {
			return DampersReport{}, fmt.Errorf("reading the grillings: %w", err)
		}
		facts.Graph = graph
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

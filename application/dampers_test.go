package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func graphBead(id string, priority int, status string, labels []string, needs ...string) domain.GraphBead {
	return domain.GraphBead{ID: id, Title: "Bead " + id, Status: status, Type: "task", Priority: priority, Labels: labels, Edges: blocks(needs...)}
}

func dampersOf(graph []domain.GraphBead) string {
	facts := aCleanDamperFixture()
	facts.Graph = append(facts.Graph, graph...)
	var b strings.Builder
	application.ReadDampers(facts, application.DamperLimits{}).Write(&b)
	return strings.Join(strings.Fields(b.String()), " ") // the section wraps its lines
}

func TestDampersNameADependencyBelowWhatWaitsOnIt(t *testing.T) {
	// A P1 waits on a P2 which waits on a P3: both lower beads are named, each raised to P1.
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "open", nil, "mw-i.2"),
		graphBead("mw-i.2", 2, "open", nil, "mw-i.3"),
		graphBead("mw-i.3", 3, "open", nil),
	})
	if !strings.Contains(got, "inversion") {
		t.Fatalf("DAMPERS has no inversion line:\n%s", got)
	}
	for _, want := range []string{"mw-i.2 P2→P1", "mw-i.3 P3→P1"} {
		if !strings.Contains(got, want) {
			t.Errorf("DAMPERS = %q, want it to hold %q", got, want)
		}
	}
	if strings.Contains(got, "mw-i.1 ") {
		t.Errorf("the P1 at the top is named: %q", got)
	}
}

func TestDampersSayNothingOfAChainWithNoInversion(t *testing.T) {
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 3, "open", nil, "mw-i.2"),
		graphBead("mw-i.2", 2, "open", nil, "mw-i.3"),
		graphBead("mw-i.3", 1, "open", nil),
		graphBead("mw-i.4", 2, "open", nil, "mw-i.5"),
		graphBead("mw-i.5", 2, "open", nil),
	})
	if strings.Contains(got, "inversion") {
		t.Errorf("an inversion line for a chain with none:\n%s", got)
	}
}

func TestDampersCountOnlyBeadsThatCanStillWait(t *testing.T) {
	// A closed P1 waiting on a P3 holds nothing up; an epic's priority is no one's.
	epic := graphBead("mw-i.5", 1, "open", nil, "mw-i.6")
	epic.Type = "epic"
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "closed", nil, "mw-i.2"),
		graphBead("mw-i.2", 3, "open", nil),
		epic,
		graphBead("mw-i.6", 3, "open", nil),
	})
	if strings.Contains(got, "inversion") {
		t.Errorf("an inversion line with no live bead waiting:\n%s", got)
	}
}

func TestDampersNameAHisPriorityBeadAsHisAndProposeNoRaise(t *testing.T) {
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "open", nil, "mw-i.2", "mw-i.3"),
		graphBead("mw-i.2", 3, "deferred", []string{application.HisPriorityLabel}),
		graphBead("mw-i.3", 2, "open", nil),
	})
	if !strings.Contains(got, "mw-i.2 P3 (his)") {
		t.Errorf("DAMPERS = %q, want the his-priority bead named as his", got)
	}
	if strings.Contains(got, "mw-i.2 P3→") {
		t.Errorf("a raise is proposed for his bead: %q", got)
	}
	if !strings.Contains(got, "mw-i.3 P2→P1") {
		t.Errorf("DAMPERS = %q, want the other bead raised", got)
	}
}

func TestTheInversionWalkIsTheOneMwStuckUses(t *testing.T) {
	// A bead held in the middle of a chain cuts it, as it does for mw stuck.
	got := dampersOf([]domain.GraphBead{
		graphBead("mw-i.1", 1, "open", nil, "mw-i.2"),
		graphBead("mw-i.2", 2, "deferred", nil, "mw-i.3"),
		graphBead("mw-i.3", 3, "open", nil),
	})
	if strings.Contains(got, "mw-i.3 P3") {
		t.Errorf("a bead behind a held one is named for a P1 above the hold: %q", got)
	}
	if !strings.Contains(got, "mw-i.2 P2→P1") {
		t.Errorf("DAMPERS = %q, want the held P2 named", got)
	}
}

func TestInversionListNamesEveryInversionOnALineWithWhatWaitsOnIt(t *testing.T) {
	// Twelve P3 beads each under its own P1; one of them is his.
	graph := &apptest.FakeBeadGraph{}
	for n := 1; n <= 12; n++ {
		low, top := fmt.Sprintf("mw-i.%02d", n), fmt.Sprintf("mw-t.%02d", n)
		var labels []string
		if n == 5 {
			labels = []string{application.HisPriorityLabel}
		}
		graph.Add(low, "low", "open", "task", labels)
		graph.SetPriority(low, 3)
		graph.Add(top, "top", "open", "task", nil, blocks(low)...)
		graph.SetPriority(top, 1)
	}
	got, err := application.InversionLister{Graph: graph}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("%d lines, want 12:\n%s", len(lines), got)
	}
	for n, line := range lines {
		id := fmt.Sprintf("mw-i.%02d", n+1)
		top := fmt.Sprintf("mw-t.%02d", n+1)
		if !strings.HasPrefix(line, id+" ") || !strings.Contains(line, "P3") || !strings.Contains(line, top) {
			t.Errorf("line %d = %q, want %s, its priority and %s", n+1, line, id, top)
		}
		if n+1 == 5 {
			if !strings.Contains(line, "(his)") || strings.Contains(line, "→") {
				t.Errorf("his line = %q, want (his) and no raise", line)
			}
		} else if !strings.Contains(line, "P3→P1") || strings.Contains(line, "(his)") {
			t.Errorf("line = %q, want P3→P1 and no (his)", line)
		}
	}
}

func TestInversionListNamesTheHighestPriorityBeadThatWaitsEvenThroughAnother(t *testing.T) {
	graph := &apptest.FakeBeadGraph{}
	graph.Add("mw-i.1", "top", "open", "task", nil, blocks("mw-i.2")...)
	graph.SetPriority("mw-i.1", 0)
	graph.Add("mw-i.2", "mid", "open", "task", nil, blocks("mw-i.3")...)
	graph.SetPriority("mw-i.2", 2)
	graph.Add("mw-i.3", "low", "open", "task", nil)
	graph.SetPriority("mw-i.3", 4)
	got, err := application.InversionLister{Graph: graph}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "mw-i.3 P4→P0 waited on by mw-i.1") {
		t.Errorf("list = %q, want mw-i.3 raised to P0 for mw-i.1", got)
	}
}

func TestInversionListSaysSoWhenThereIsNone(t *testing.T) {
	graph := &apptest.FakeBeadGraph{}
	graph.Add("mw-i.1", "a", "open", "task", nil, blocks("mw-i.2")...)
	graph.Add("mw-i.2", "b", "open", "task", nil)
	got, err := application.InversionLister{Graph: graph}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "no priority inversion\n" {
		t.Errorf("list = %q, want the single line saying none", got)
	}
}

func TestInversionListReportsAGraphItCannotRead(t *testing.T) {
	graph := &apptest.FakeBeadGraph{Err: errors.New("no server")}
	if _, err := (application.InversionLister{Graph: graph}).List(context.Background()); err == nil || !strings.Contains(err.Error(), "no server") {
		t.Errorf("err = %v, want the read error", err)
	}
}

func TestDampersInversionLineEndsItsCutListWithAPointerToMwStuckInversions(t *testing.T) {
	var graph []domain.GraphBead
	for n := 1; n <= 10; n++ {
		graph = append(graph,
			graphBead(fmt.Sprintf("mw-i.%02d", n), 3, "open", nil),
			graphBead(fmt.Sprintf("mw-t.%02d", n), 1, "open", nil, fmt.Sprintf("mw-i.%02d", n)))
	}
	got := dampersOf(graph)
	if !strings.Contains(got, "+2 (all: mw stuck --inversions)") {
		t.Errorf("DAMPERS = %q, want the cut ending with a pointer to mw stuck --inversions", got)
	}
	if few := dampersOf(graph[:4]); strings.Contains(few, "mw stuck --inversions") {
		t.Errorf("a list not cut points to the command anyway: %q", few)
	}
}

// reviewFixture is the clean fixture with a hands step not run on each of the
// beads, a card on each of the cards, and the graph holding each bead as given.
func reviewFixture(t *testing.T, statuses map[string]string, steps, cards []string) application.DamperFacts {
	t.Helper()
	facts := aCleanDamperFixture()
	for _, bead := range steps {
		raw, _ := json.Marshal([]application.HandsStepRecord{{HandsStep: domain.HandsStep{ID: "a", Host: "desktop", As: "user", Run: "true"}}})
		facts.Notes[application.HandsStepsKey(bead)] = string(raw)
	}
	for _, bead := range cards {
		facts.Notes[application.PosternQuestionKey(bead)] = `{"q":"which?"}`
	}
	for bead, status := range statuses {
		facts.Graph = append(facts.Graph, domain.GraphBead{ID: bead, Title: "Bead " + bead, Status: status, Type: "task"})
	}
	return facts
}

func TestReviewQueueCountsAHandsStepOnlyWhileItsBeadIsOpen(t *testing.T) {
	facts := reviewFixture(t, map[string]string{"mw-h.closed": "closed", "mw-h.open": "open"}, []string{"mw-h.closed", "mw-h.open"}, nil)
	report := application.ReadDampers(facts, application.DamperLimits{ReviewQueue: 1})
	// the clean fixture holds 2 (a card and a demo); the open bead's step makes 3
	if report.Counts.ReviewQueue != 3 {
		t.Errorf("ReviewQueue = %d, want 3: the closed bead's step is not counted", report.Counts.ReviewQueue)
	}
	var review application.DamperAlarm
	for _, alarm := range report.Alarms {
		if alarm.Principle == application.DamperReview {
			review = alarm
		}
	}
	if slices.Contains(review.Beads, "mw-h.closed") || !slices.Contains(review.Beads, "mw-h.open") {
		t.Errorf("review alarm names %v, want mw-h.open and not mw-h.closed", review.Beads)
	}
	if !strings.Contains(review.Text, "1 hands steps") {
		t.Errorf("review alarm says %q, want 1 hands steps", review.Text)
	}
}

func TestReviewQueueCountsACardOnlyWhileItsBeadIsOpen(t *testing.T) {
	facts := reviewFixture(t, map[string]string{"mw-c.closed": "closed", "mw-c.open": "in_progress"}, nil, []string{"mw-c.closed", "mw-c.open"})
	report := application.ReadDampers(facts, application.DamperLimits{ReviewQueue: 1})
	if report.Counts.ReviewQueue != 3 {
		t.Errorf("ReviewQueue = %d, want 3: the closed bead's card is not counted", report.Counts.ReviewQueue)
	}
}

func TestReviewQueueCountsABeadTheGraphDoesNotHold(t *testing.T) {
	facts := reviewFixture(t, nil, []string{"mw-h.unknown"}, nil)
	if got := application.ReadDampers(facts, application.DamperLimits{}).Counts.ReviewQueue; got != 3 {
		t.Errorf("ReviewQueue = %d, want 3: a bead the graph does not show is counted", got)
	}
}

func TestDamperAlarmHintNamesACommandThatPrintsTheWholeList(t *testing.T) {
	var cards []string
	for n := 1; n <= 12; n++ {
		cards = append(cards, fmt.Sprintf("mw-q.%02d", n))
	}
	facts := reviewFixture(t, nil, nil, cards)
	report := application.ReadDampers(facts, application.DamperLimits{})
	var b strings.Builder
	report.Write(&b)
	got := strings.Join(strings.Fields(b.String()), " ")
	if !strings.Contains(got, "(all: mw stuck --dampers)") || strings.Contains(got, "mw stuck --inversions") {
		t.Errorf("DAMPERS = %q, want the cut ending with a pointer to mw stuck --dampers", got)
	}
	full := report.FullList()
	for _, bead := range cards {
		if !strings.Contains(full, bead+"\n") {
			t.Errorf("FullList lacks %s:\n%s", bead, full)
		}
	}
	if !strings.Contains(full, "review") {
		t.Errorf("FullList does not name the alarm:\n%s", full)
	}
	if got := (application.DampersReport{}).FullList(); !strings.Contains(got, "no damper alarm") {
		t.Errorf("FullList with none = %q, want it to say so", got)
	}
}

// needsCountsFile is the counts file the view writes at written, for a doc
// holding n verify needs and one each of a few other kinds waiting on him.
func needsCountsFile(t *testing.T, written time.Time, verify int) []byte {
	t.Helper()
	doc := application.PosternViewDoc{}
	add := func(kind string, n int) {
		for i := 0; i < n; i++ {
			doc.Needs = append(doc.Needs, application.PosternViewNeed{Kind: kind, WaitsFor: application.PosternWaitsYou})
		}
	}
	add(application.PosternNeedVerify, verify)
	add(application.PosternNeedStale, 2)
	add(application.PosternNeedApprove, 3)
	add(application.PosternNeedDemo, 4)
	add(application.PosternNeedQuestion, 5)
	add(application.PosternNeedHands, 6)
	raw, err := application.PosternNeedsCountsJSON(doc, written)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func needsYouLine(report application.DampersReport) (string, bool) {
	for _, alarm := range report.Alarms {
		if alarm.Principle == application.DamperNeedsYou {
			return alarm.Line(), true
		}
	}
	return "", false
}

func TestDampersRingWhenHisLineIsOverTheLimit(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.NeedsCounts = needsCountsFile(t, facts.Now.Add(-5*time.Minute), 10) // 30 in all
	report := application.ReadDampers(facts, application.DamperLimits{})
	got, ok := needsYouLine(report)
	want := "needs you · his line is 30: 10 verify, 2 stale, 3 release, 4 demos, 5 questions, 6 hands (limit 25)"
	if !ok || got != want {
		t.Errorf("line = %q (%v), want %q", got, ok, want)
	}
	if report.Counts.NeedsYou != 30 {
		t.Errorf("NeedsYou = %d, want 30", report.Counts.NeedsYou)
	}
	if !strings.Contains(report.FullList(), want) {
		t.Errorf("mw stuck --dampers does not list it:\n%s", report.FullList())
	}
}

func TestDampersAreQuietAtOrUnderTheNeedsYouLimit(t *testing.T) {
	for _, verify := range []int{4, 5} { // 25 is the limit, 24 is under it
		facts := aCleanDamperFixture()
		facts.NeedsCounts = needsCountsFile(t, facts.Now.Add(-time.Minute), verify)
		// 20 other needs; verify 4 -> 24, verify 5 -> 25
		report := application.ReadDampers(facts, application.DamperLimits{})
		if line, ok := needsYouLine(report); ok {
			t.Errorf("verify %d: rang %q at or under the limit", verify, line)
		}
	}
}

func TestDampersNeedsYouLimitIsTheKnob(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.NeedsCounts = needsCountsFile(t, facts.Now.Add(-time.Minute), 0) // 20
	if _, ok := needsYouLine(application.ReadDampers(facts, application.DamperLimits{NeedsYou: 19})); !ok {
		t.Error("a limit of 19 did not ring at 20")
	}
	if line, ok := needsYouLine(application.ReadDampers(facts, application.DamperLimits{NeedsYou: 20})); ok {
		t.Errorf("a limit of 20 rang at 20: %q", line)
	}
}

func TestDampersSayTheNeedsCountIsStaleInsteadOfANumber(t *testing.T) {
	facts := aCleanDamperFixture()
	written := facts.Now.Add(-31 * time.Minute)
	facts.NeedsCounts = needsCountsFile(t, written, 20) // over the limit, but old
	got, ok := needsYouLine(application.ReadDampers(facts, application.DamperLimits{}))
	if !ok || !strings.HasPrefix(got, "needs you · count is stale (written ") || !strings.Contains(got, written.UTC().Format("15:04")) {
		t.Errorf("line = %q (%v), want the stale-count line naming when it was written", got, ok)
	}
	if strings.Contains(got, "his line is") {
		t.Errorf("a stale count still gave a number: %q", got)
	}
	// exactly 30 minutes old is not yet stale
	facts.NeedsCounts = needsCountsFile(t, facts.Now.Add(-30*time.Minute), 20)
	if got, _ := needsYouLine(application.ReadDampers(facts, application.DamperLimits{})); strings.Contains(got, "count is stale") {
		t.Errorf("a count 30 minutes old is called stale: %q", got)
	}
}

func TestDampersSayNothingOfNeedsWhenThereIsNoCountsFile(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.NeedsCounts = nil
	if line, ok := needsYouLine(application.ReadDampers(facts, application.DamperLimits{})); ok {
		t.Errorf("a missing counts file rang %q", line)
	}
}

func TestDampersNameACountsFileThatCannotBeRead(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.NeedsCounts = []byte("not json")
	got, ok := needsYouLine(application.ReadDampers(facts, application.DamperLimits{}))
	if !ok || !strings.Contains(got, "count is stale") {
		t.Errorf("line = %q (%v), want a count that cannot be trusted said as stale", got, ok)
	}
}

// proposalComment is a PROPOSED comment of mw verify match, written at when.
func proposalComment(txid string, when time.Time) application.Comment {
	return application.Comment{
		Created: when,
		Text:    "PROPOSED VERIFIED by the Governor's use, from " + txid + " (2026-11-06 17:00Z): the green tick after Save (confidence 0.97, opus, run r-9)",
	}
}

func proposalDampers(proposals []application.DamperProposal) string {
	facts := aCleanDamperFixture()
	facts.Proposals = proposals
	var b strings.Builder
	application.ReadDampers(facts, application.DamperLimits{}).Write(&b)
	return strings.Join(strings.Fields(b.String()), " ")
}

func TestDampersNameAProposalNobodyConfirmedInTwoDays(t *testing.T) {
	comments := map[string][]application.Comment{
		"mw-old.1":  {proposalComment("direct:aaa", statusNow.Add(-3*24*time.Hour))},
		"mw-new.1":  {proposalComment("direct:bbb", statusNow.Add(-24*time.Hour))},
		"mw-done.1": {proposalComment("direct:ccc", statusNow.Add(-3*24*time.Hour)), {Created: statusNow.Add(-24 * time.Hour), Text: "VERIFIED by the Governor's use, inferred by the Mayor from direct:ccc: ok"}},
		"mw-rej.1":  {proposalComment("direct:ddd", statusNow.Add(-3*24*time.Hour)), {Created: statusNow.Add(-24 * time.Hour), Text: "REJECTED PROPOSED from direct:ddd: wrong picture"}},
	}
	got := proposalDampers(application.ProposalsOf(comments))
	want := "proposals · 1 waiting over 2 days: mw-old.1 (mw verify list)"
	if !strings.Contains(got, want) {
		t.Errorf("DAMPERS = %q, want it to hold %q", got, want)
	}
	for _, not := range []string{"mw-new.1", "mw-done.1", "mw-rej.1"} {
		if strings.Contains(got, not) {
			t.Errorf("DAMPERS names %s: %q", not, got)
		}
	}
}

func TestDampersSayNothingOfAProposalYoungerThanTwoDays(t *testing.T) {
	got := proposalDampers(application.ProposalsOf(map[string][]application.Comment{
		"mw-new.1": {proposalComment("direct:bbb", statusNow.Add(-24*time.Hour))},
	}))
	if strings.Contains(got, "proposals") {
		t.Errorf("a proposal a day old rings: %q", got)
	}
}

func TestDampersCountEachBeadOnceWhateverItsProposals(t *testing.T) {
	got := proposalDampers(application.ProposalsOf(map[string][]application.Comment{
		"mw-two.1": {proposalComment("direct:aaa", statusNow.Add(-4*24*time.Hour)), proposalComment("direct:bbb", statusNow.Add(-3*24*time.Hour))},
	}))
	if !strings.Contains(got, "proposals · 1 waiting over 2 days: mw-two.1 (mw verify list)") {
		t.Errorf("DAMPERS = %q", got)
	}
}

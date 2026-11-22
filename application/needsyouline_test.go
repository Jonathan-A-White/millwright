package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// aBadgeFile is the counts file of a view written at written: 10 landings to
// look at, 2 stale, 3 releases, 4 demos, 5 questions and 6 hands steps wait on
// him (30), and 7 To do items are open.
func aBadgeFile(t *testing.T, written time.Time) []byte {
	t.Helper()
	doc := application.PosternViewDoc{}
	add := func(kind string, n int) {
		for i := 0; i < n; i++ {
			doc.Needs = append(doc.Needs, application.PosternViewNeed{Kind: kind, WaitsFor: application.PosternWaitsYou})
		}
	}
	add(application.PosternNeedVerify, 10)
	add(application.PosternNeedStale, 2)
	add(application.PosternNeedApprove, 3)
	add(application.PosternNeedDemo, 4)
	add(application.PosternNeedQuestion, 5)
	add(application.PosternNeedHands, 6)
	for i := 0; i < 7; i++ {
		doc.Beads = append(doc.Beads, application.PosternViewBead{Status: "open", Type: "task", Labels: []string{application.LabelHitl, application.LabelHitlTodo}})
	}
	doc.Beads = append(doc.Beads,
		application.PosternViewBead{Status: "closed", Type: "task", Labels: []string{application.LabelHitlTodo}},
		application.PosternViewBead{Status: "open", Type: "epic", Labels: []string{application.LabelHitlTodo}})
	raw, err := application.PosternNeedsCountsJSON(doc, written)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

const wantBadgeLine = "his badge 30: 5 cards, 4 demos, 6 hands steps, 10 landings waiting for his look, 5 other; To do items 7 (own tab, not in the badge's count)"

func TestHisTimeNeedsYouCountSplitsTheBadgeByKindBesideTheReviewQueue(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.NeedsCounts = aBadgeFile(t, facts.Now.Add(-time.Minute))
	report := application.ReadDampers(facts, application.DamperLimits{})
	if got := report.Counts.NeedsYouSplit.Line(); got != wantBadgeLine {
		t.Errorf("badge line = %q, want %q", got, wantBadgeLine)
	}
	if got := report.Counts.NeedsYouSplit.Total; got != report.Counts.NeedsYou {
		t.Errorf("the split's total %d is not the number the needs-you alarm counts, %d", got, report.Counts.NeedsYou)
	}

	var out bytes.Buffer
	result, err := application.FactoryReport{
		Tracker: apptest.NewFakeTracker(), Files: vault.New(t.TempDir()), Seat: "builder",
		Benchmarks: &apptest.FakeBenchmarks{}, Review: reportCounter{report}, Out: &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := result.String()
	queue := strings.Index(text, "  review queue ")
	badge := strings.Index(text, "  his badge 30: 5 cards")
	demos := strings.Index(text, "  demos waiting")
	if queue < 0 || badge < queue || demos < badge {
		t.Errorf("HIS TIME does not print the badge's split between the review queue and the demos waiting:\n%s", text)
	}
	if !strings.Contains(strings.Join(strings.Fields(text), " "), wantBadgeLine) {
		t.Errorf("HIS TIME lacks %q:\n%s", wantBadgeLine, text)
	}
	for _, line := range strings.Split(text, "\n") {
		if len([]rune(line)) > application.ReportWidth {
			t.Errorf("a line is wider than %d columns: %q", application.ReportWidth, line)
		}
	}
}

type reportCounter struct{ report application.DampersReport }

func (c reportCounter) DamperCounts(context.Context) (application.DampersReport, error) {
	return c.report, nil
}

func TestHisTimeNeedsYouCountSaysWhyWhenThereIsNoCurrentCount(t *testing.T) {
	facts := aCleanDamperFixture()
	for name, raw := range map[string][]byte{
		"missing": nil,
		"stale":   aBadgeFile(t, facts.Now.Add(-time.Hour)),
		"garbage": []byte("{"),
	} {
		facts.NeedsCounts = raw
		got := application.ReadDampers(facts, application.DamperLimits{}).Counts.NeedsYouSplit.Line()
		if !strings.HasPrefix(got, "his badge: no count (") || strings.Contains(got, "cards") {
			t.Errorf("%s: line = %q, want it to say there is no count", name, got)
		}
	}
}

// The limit of 8 is judged on the review queue as before, whatever the badge
// says: 9 open cards ring it with a quiet badge, and 3 with a loud one do not.
func TestTheReviewLimitIsStillJudgedOnTheReviewQueueNotTheBadge(t *testing.T) {
	if application.DefaultDamperReviewQueue != 8 {
		t.Fatalf("DefaultDamperReviewQueue = %d, want 8", application.DefaultDamperReviewQueue)
	}
	var nine, three []string
	for n := 1; n <= 9; n++ {
		nine = append(nine, "mw-q."+string(rune('a'+n)))
		if n <= 3 {
			three = append(three, nine[n-1])
		}
	}
	reviewRings := func(cards []string, counts []byte) (bool, int) {
		facts := reviewFixture(t, nil, nil, cards)
		facts.NeedsCounts = counts
		report := application.ReadDampers(facts, application.DamperLimits{})
		for _, a := range report.Alarms {
			if a.Principle == application.DamperReview {
				return true, report.Counts.ReviewQueue
			}
		}
		return false, report.Counts.ReviewQueue
	}
	// The fixture's own demo is in the queue with the cards: 9 + 2 over 8, 3 + 2 under.
	loud := aBadgeFile(t, statusNow.Add(-time.Minute))
	for name, counts := range map[string][]byte{"no counts file": nil, "a badge of 30": loud} {
		if rings, size := reviewRings(nine, counts); !rings || size != 11 {
			t.Errorf("%s: 9 cards: rings %v, queue %d, want it to ring on 11", name, rings, size)
		}
		if rings, size := reviewRings(three, counts); rings || size != 5 {
			t.Errorf("%s: 3 cards: rings %v, queue %d, want it quiet on 5", name, rings, size)
		}
	}
}

func TestStuckDampersQueueLinesPrintTheBadgeBesideTheReviewQueue(t *testing.T) {
	facts := aCleanDamperFixture()
	facts.NeedsCounts = aBadgeFile(t, facts.Now.Add(-time.Minute))
	got := application.ReadDampers(facts, application.DamperLimits{}).QueueLines()
	want := "his review queue 2 (limit 8)\n" + wantBadgeLine + "\n"
	if got != want {
		t.Errorf("QueueLines = %q, want %q", got, want)
	}
}

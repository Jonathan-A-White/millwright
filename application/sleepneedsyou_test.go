package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// needsYouFixture is a review queue of three: a card on a grilling he parked, a
// hands step on a bead whose replacement is closed, and a demo that is live.
func needsYouFixture() application.NeedsYouFacts {
	live := damperStory("mw-lv.1", 2, 2*24*time.Hour, application.LabelDemo, application.LabelHitl)
	live.Description = "Open the app and look at the new screen."
	return application.NeedsYouFacts{
		DamperFacts: application.DamperFacts{
			Now:  statusNow,
			Work: application.WorkInHand{Ready: []application.StoryDetail{live}},
			Graph: []domain.GraphBead{
				{ID: "mw-fn", Title: "Map: the queue", Status: "deferred", Type: "epic"},
				{ID: "mw-fn.13", Title: "grilling: which queue first?", Status: "deferred", Created: statusNow.Add(-9 * 24 * time.Hour),
					Edges: []domain.BeadEdge{{To: "mw-fn", Kind: domain.EdgeParentChild}}},
				{ID: "mw-5g.6", Title: "Install the new unit", Status: "open", Created: statusNow.Add(-12 * 24 * time.Hour)},
				{ID: "mw-5g.9", Title: "Install it another way", Status: "closed"},
				{ID: "mw-lv.1", Title: "story mw-lv.1", Status: "open"},
			},
			Notes: map[string]string{
				application.PosternQuestionKey("mw-fn.13"): `{"txid":"direct:q1","q":"Which queue first, A or B?","rec":"A","asked":"2026-09-09T10:00:00Z"}`,
				application.HandsStepsKey("mw-5g.6"):       `[{"id":"s1","host":"vps","as":"root","run":"install-unit","way_back":"remove-unit","added_at":"2026-09-06T10:00:00Z"}]`,
			},
		},
		Comments: map[string][]application.Comment{
			"mw-5g.6": {
				{Author: "mayor", Created: statusNow.Add(-5 * 24 * time.Hour), Text: "first comment"},
				{Author: "mayor", Created: statusNow.Add(-3 * 24 * time.Hour), Text: "The install was overtaken by mw-5g.9, which landed."},
			},
		},
	}
}

func TestTheNeedsYouPartHoldsEachItemOfTheQueueAndNamesTheEvidenceOfTheStaleOnes(t *testing.T) {
	text := needsYouFixture().Text(application.SleepBudgetOf(application.SleepNeedsYouFile))
	for _, want := range []string{
		"3 items: 1 card, 1 demo, 1 hands step",
		"mw-fn.13", "grilling: which queue first?", "Which queue first, A or B?",
		"parked", "mw-fn (deferred)",
		"mw-5g.6", "mw-5g.9 is closed", "overtaken by mw-5g.9", "install-unit",
		"mw-lv.1", "Open the app and look at the new screen.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("needs-you.txt lacks %q:\n%s", want, text)
		}
	}
	stale, rest, ok := strings.Cut(text, "OTHER ITEMS")
	if !ok {
		t.Fatalf("needs-you.txt has no OTHER ITEMS section:\n%s", text)
	}
	for _, id := range []string{"mw-fn.13", "mw-5g.6"} {
		if !strings.Contains(stale, id) || strings.Contains(rest, id) {
			t.Errorf("%s is not among the stale candidates only:\n%s", id, text)
		}
	}
	if strings.Contains(stale, "mw-lv.1") || !strings.Contains(rest, "mw-lv.1") {
		t.Errorf("the live demo is among the stale candidates:\n%s", text)
	}
}

func TestTheNeedsYouQueueIsTheOneMwStuckDampersCounts(t *testing.T) {
	facts := needsYouFixture()
	report := application.ReadDampers(facts.DamperFacts, application.DamperLimits{ReviewQueue: 1})
	if report.Counts.ReviewQueue != facts.QueueSize() {
		t.Errorf("mw stuck --dampers counts %d, needs-you.txt %d", report.Counts.ReviewQueue, facts.QueueSize())
	}
	// A bead the graph shows closed is out of both.
	facts.Graph[3].Status = "closed"
	facts.Graph[2].Status = "closed"
	if got := facts.QueueSize(); got != 2 {
		t.Errorf("a closed bead's hands step is still in the queue: %d", got)
	}
}

func TestADemoNamingAVersionIsShownWithTheRigsCurrentOne(t *testing.T) {
	facts := needsYouFixture()
	facts.Work.Ready[0].Description = "Works as before in v0.1.5: look at the screen."
	facts.Versions = map[string]string{"millwright": "0.1.305"}
	text := facts.Text(application.SleepBudgetOf(application.SleepNeedsYouFile))
	if !strings.Contains(text, "names v0.1.5, millwright is at 0.1.305") {
		t.Errorf("the version is not shown:\n%s", text)
	}
}

func TestTheNeedsYouPartIsCutToItsBudgetKeepingTheStaleCandidates(t *testing.T) {
	facts := needsYouFixture()
	for n := 0; n < 200; n++ {
		d := damperStory(fmt.Sprintf("mw-d.%d", n), 2, time.Duration(n+1)*time.Hour, application.LabelDemo, application.LabelHitl)
		d.Description = strings.Repeat("look at the screen. ", 40)
		facts.Work.Ready = append(facts.Work.Ready, d)
	}
	budget := application.SleepBudgetOf(application.SleepNeedsYouFile)
	text := facts.Text(budget)
	if len(text) > budget {
		t.Errorf("needs-you.txt is %d bytes, over its budget of %d", len(text), budget)
	}
	for _, want := range []string{"mw-fn.13", "mw-5g.6", "mw cut this part"} {
		if !strings.Contains(text, want) {
			t.Errorf("a cut needs-you.txt lacks %q", want)
		}
	}
}

// ideasOnlyFixture is a live Ideas-only card of two items, the first of which
// the Mayor has filed a bead for.
func ideasOnlyFixture() application.NeedsYouFacts {
	facts := needsYouFixture()
	facts.Cards = []domain.CardRecord{{
		Txid: "direct:card1", Title: application.IdeasOnlyCardTitle(2), At: "2026-10-19T09:00:00Z",
		Items: []domain.CardRecordItem{
			{N: 1, Text: "1. add a nightly sweep of Needs you (general, 11:58 UTC)"},
			{N: 2, Text: "2. paint the shed (general, 12:01 UTC)"},
		},
	}}
	facts.Released = []application.IdeasOnlyHeld{
		{Txid: "direct:755f88ac2fb710167b4a71d5f69e577faba3b7988b9475fc4aee269d07fb8c3a", Channel: "general", Text: "add a nightly sweep of Needs you"},
		{Txid: "direct:99999999aaaaaaaa", Channel: "general", Text: "paint the shed"},
	}
	facts.Evidence = func() []application.NeedsYouText {
		return []application.NeedsYouText{
			{Source: "mw-gq6.674", Text: "HIS WORDS (Postern general, txid direct:755f88ac2fb710167b4a71d5f69e577faba3b7988b9475fc4aee269d07fb8c3a): sweep."},
		}
	}
	return facts
}

func TestAnIdeasOnlyCardsHandledIdeaIsAStaleCandidateAndTheUnhandledOneIsNot(t *testing.T) {
	text := ideasOnlyFixture().Text(application.SleepBudgetOf(application.SleepNeedsYouFile))
	stale, _, _ := strings.Cut(text, "OTHER ITEMS")
	for _, want := range []string{
		"add a nightly sweep of Needs you", "mw-gq6.674", "mw card update direct:card1 --tick 1",
	} {
		if !strings.Contains(stale, want) {
			t.Errorf("the stale candidates lack %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "paint the shed") {
		t.Errorf("an idea nobody handled is listed:\n%s", text)
	}
}

func TestAnIdeasOnlyItemAlreadyTickedIsLeftOut(t *testing.T) {
	facts := ideasOnlyFixture()
	facts.Cards = append(facts.Cards, domain.CardRecord{Txid: "direct:upd1", Re: "direct:card1", At: "2026-10-20T09:00:00Z", Tick: []int{1}})
	text := facts.Text(application.SleepBudgetOf(application.SleepNeedsYouFile))
	if strings.Contains(text, "add a nightly sweep") {
		t.Errorf("a ticked item is listed:\n%s", text)
	}
}

func TestTheNeedsYouPartIsAPartOfTheBundleWhenTheGathererCanReadIt(t *testing.T) {
	g := gatherFixture()
	g.NeedsYou = func(context.Context) (application.NeedsYouFacts, error) { return needsYouFixture(), nil }
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	part := partsByFile(bundle)[application.SleepNeedsYouFile]
	if part.File == "" || part.Budget != application.SleepBudgetOf(application.SleepNeedsYouFile) || !strings.Contains(part.Text, "mw-5g.9 is closed") {
		t.Errorf("the bundle's needs-you part = %+v", part)
	}
	if len(part.Text) > part.Budget {
		t.Errorf("needs-you.txt is %d bytes, over its budget of %d", len(part.Text), part.Budget)
	}
}

func TestANeedsYouPartThatCannotBeReadIsSaidSoAndThePassGoesOn(t *testing.T) {
	g := gatherFixture()
	g.NeedsYou = func(context.Context) (application.NeedsYouFacts, error) {
		return application.NeedsYouFacts{}, fmt.Errorf("the tracker is away")
	}
	bundle, err := g.Gather(context.Background(), application.SleepWindow{Date: "2026-10-16"})
	if err != nil {
		t.Fatal(err)
	}
	if got := partsByFile(bundle)[application.SleepNeedsYouFile].Text; !strings.Contains(got, "could not be read: the tracker is away") {
		t.Errorf("needs-you.txt = %q", got)
	}
}

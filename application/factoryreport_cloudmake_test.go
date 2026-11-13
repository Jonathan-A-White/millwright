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

// slowMakingSurvey is a DamperSurvey whose fake book holds a box made for 56
// minutes against the default 30 minute limit, as mw report builds it.
func slowMakingSurvey() application.DamperSurvey {
	tracker := apptest.NewFakeTracker()
	up := time.Now().Add(-56 * time.Minute)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: up, Making: true, Vultr: "active locked"}}}}
	return application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book}
}

// The cloud make alarm shows in mw report and in the phone summary mw report
// send writes, in the words mw status uses (mw-gq6.1128).
func TestAReportPrintsTheCloudMakeAlarmOfASlowMakingBox(t *testing.T) {
	var out bytes.Buffer
	result, err := application.FactoryReport{
		Tracker: apptest.NewFakeTracker(), Files: vault.New(t.TempDir()), Seat: "builder",
		Benchmarks: &apptest.FakeBenchmarks{}, Review: slowMakingSurvey(), Out: &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"mw report": result.String(), "mw report send": result.Summary()} {
		if !strings.Contains(text, "cloud make: cloud1 has been made for") || !strings.Contains(text, "Vultr says active locked") {
			t.Errorf("%s does not print the cloud make alarm:\n%s", name, text)
		}
		for _, line := range strings.Split(text, "\n") {
			if len([]rune(line)) > application.ReportWidth {
				t.Errorf("%s: a line is wider than %d columns: %q", name, application.ReportWidth, line)
			}
		}
	}
	if !strings.Contains(out.String(), "cloud make: cloud1") {
		t.Errorf("the report written to Out does not print the alarm:\n%s", out.String())
	}
}

// A report with no slow make says nothing of one.
func TestAReportWithNoSlowMakePrintsNoCloudMakeAlarm(t *testing.T) {
	survey := slowMakingSurvey()
	survey.Cloud = &apptest.FakeCloudBook{}
	result, err := application.FactoryReport{
		Tracker: apptest.NewFakeTracker(), Files: vault.New(t.TempDir()), Seat: "builder",
		Benchmarks: &apptest.FakeBenchmarks{}, Review: survey, Out: &bytes.Buffer{},
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if text := result.String() + result.Summary(); strings.Contains(text, "cloud make") {
		t.Errorf("expected no cloud make line, got:\n%s", text)
	}
}

// The review hold that counts his queue before a card goes out says the cloud
// make alarm too, on its error stream, and still counts the queue.
func TestTheReviewHoldSaysTheCloudMakeAlarmWhenItCountsTheQueue(t *testing.T) {
	var errs bytes.Buffer
	hold := application.ReviewHold{Queue: slowMakingSurvey(), Err: &errs}
	verdict := hold.Judge(context.Background(), application.PosternSendRequest{
		Class: "decision-needed", Bead: "mw-a", Text: "Which?", Options: []string{"A", "B"},
	})
	if verdict.Over {
		t.Errorf("expected an empty queue to be under the limit, got %+v", verdict)
	}
	if !strings.Contains(errs.String(), "cloud make: cloud1 has been made for") {
		t.Errorf("the hold does not say the cloud make alarm:\n%s", errs.String())
	}
}

// The review hold reads the age of a make off its own injected clock, not the
// host's (mw-gq6.1220): a box made 56 minutes before the hold's Now is slow
// whatever the host's time is.
func TestTheReviewHoldReadsTheAgeOfAMakeOffItsInjectedClock(t *testing.T) {
	base := time.Date(2031, 3, 4, 10, 0, 0, 0, time.UTC)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2031-03",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: base, Making: true, Vultr: "active locked"}}}}
	survey := application.DamperSurvey{Tracker: apptest.NewFakeTracker(), Notes: apptest.NewFakeTracker(), Cloud: book}
	req := application.PosternSendRequest{Class: "decision-needed", Bead: "mw-a", Text: "Which?", Options: []string{"A", "B"}}

	var slow bytes.Buffer
	application.ReviewHold{Queue: survey, Err: &slow, Now: func() time.Time { return base.Add(56 * time.Minute) }}.Judge(context.Background(), req)
	if !strings.Contains(slow.String(), "cloud make: cloud1 has been made for") {
		t.Errorf("a make 56 minutes old by the hold's clock is not said:\n%s", slow.String())
	}

	var fresh bytes.Buffer
	application.ReviewHold{Queue: survey, Err: &fresh, Now: func() time.Time { return base.Add(5 * time.Minute) }}.Judge(context.Background(), req)
	if strings.Contains(fresh.String(), "cloud make") {
		t.Errorf("a make 5 minutes old by the hold's clock is said:\n%s", fresh.String())
	}
}

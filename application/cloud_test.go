package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

var cloudPlan = application.CloudPlan{MaxBoxes: 2, MonthlyCapUSD: 75, HourlyUSD: 0.132, Idle: 30 * time.Minute, BoxCap: 2}

func TestANewMonthCountsOnlyItsOwnHours(t *testing.T) {
	up := time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Hours: 500, Boxes: []application.CloudBox{{Name: "cloud1", Up: up}}}}
	report, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// October's 500 hours are gone; the box up since October is counted
	// from midnight: two hours begun.
	if book.State.Month != "2026-11" || book.State.Hours != 0 || report.SpentUSD != 2*0.132 {
		t.Errorf("expected November to count two hours, got %+v, $%.3f", book.State, report.SpentUSD)
	}
}

// stuckCloud destroys nothing.
type stuckCloud struct{ apptest.FakeCloud }

func (*stuckCloud) Down(context.Context, string) error { return errors.New("terraform destroy failed") }

func TestABoxThatCannotBeDestroyedStaysInTheBookAndFailsTheCheck(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	up := now.Add(-2 * time.Hour)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up}}}}
	log := &apptest.FakeEventLog{}
	_, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: &stuckCloud{}, Book: book, Log: log, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "destroying cloud1") {
		t.Errorf("expected the check to fail destroying cloud1, got %v", err)
	}
	if len(book.State.Boxes) != 1 {
		t.Errorf("expected the box still in the book, got %+v", book.State.Boxes)
	}
	evs, _ := log.Since(context.Background(), 0)
	if len(evs) != 1 || !strings.Contains(evs[0].Detail, "failed to destroy cloud1") || evs[0].Actor != "cloud@desktop" {
		t.Errorf("expected one cloud event saying so, got %+v", evs)
	}
}

func TestStatusShowsTheCloudOnlyWhenItHasABook(t *testing.T) {
	now := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Hours: 10,
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-90 * time.Minute)}},
		Moves: []application.CloudMove{{At: now.Add(-90 * time.Minute), What: "up cloud1: 3 stories wait for any host, 0 sessions free"}}}}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder",
		Cloud: book, CloudPlan: cloudPlan, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := report.String()
	for _, want := range []string{"CLOUD: $1.58 spent of $75.00 this month, 1 of 2 boxes", "cloud1  up 1h30m, working", "10-09 12:30Z up cloud1"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
	report, err = application.Status{Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder"}.Run(context.Background())
	if err != nil || strings.Contains(report.String(), "CLOUD") {
		t.Errorf("expected no CLOUD section without a book: %v\n%s", err, report.String())
	}
}

// pathedStories files an epic with four stories on the tracker: open and
// deferred ones pathed to cloud1, a closed one pathed to it, and an open one
// pathed to cloud2.
func pathedStories(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-c", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	for _, s := range []struct{ id, host, status string }{
		{"mw-c.1", "cloud1", apptest.StatusOpen},
		{"mw-c.2", "cloud1", apptest.StatusDeferred},
		{"mw-c.3", "cloud2", apptest.StatusOpen},
		{"mw-c.4", "cloud1", apptest.StatusClosed},
	} {
		tracker.AddStory("mw-c", domain.Story{ID: s.id, Title: "story " + s.id})
		if err := tracker.SetStoryMetadata(context.Background(), s.id, map[string]string{"host": s.host}); err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetStatus(s.id, s.status); err != nil {
			t.Fatal(err)
		}
	}
	return tracker
}

// hostOf is the host a story's path names now.
func hostOf(t *testing.T, tracker *apptest.FakeTracker, id string) string {
	t.Helper()
	d, err := tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d.Merged().Host
}

func TestABoxDestroyedByTheCheckRepathsItsOpenAndDeferredStoriesToAuto(t *testing.T) {
	now := time.Date(2026, 10, 17, 22, 0, 0, 0, time.UTC)
	up := now.Add(-2 * time.Hour)
	tracker := pathedStories(t)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up}, {Name: "cloud2", Up: now.Add(-time.Hour)}}}}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"mw-c.1": domain.HostAuto, "mw-c.2": domain.HostAuto, "mw-c.3": "cloud2", "mw-c.4": "cloud1"} {
		if got := hostOf(t, tracker, id); got != want {
			t.Errorf("expected %s on host %s after cloud1 was destroyed, got %s", id, want, got)
		}
	}
	text := report.String()
	for _, want := range []string{"mw-c.1", "mw-c.2", "host=auto"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in the report:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "re-pathed mw-c.1"); n != 1 {
		t.Errorf("expected the move said once, said %d times:\n%s", n, text)
	}
	if strings.Contains(text, "mw-c.3") || strings.Contains(text, "mw-c.4") {
		t.Errorf("expected the report to leave out the stories not moved:\n%s", text)
	}
}

func TestMwCloudDownDestroysTheBoxAndRepathsItsStories(t *testing.T) {
	now := time.Date(2026, 10, 17, 22, 0, 0, 0, time.UTC)
	tracker := pathedStories(t)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-time.Hour)}}}}
	provider := &apptest.FakeCloud{}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: provider, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Down(context.Background(), "cloud1")
	if err != nil {
		t.Fatal(err)
	}
	if len(book.State.Boxes) != 0 {
		t.Errorf("expected cloud1 out of the book, got %+v", book.State.Boxes)
	}
	if got := hostOf(t, tracker, "mw-c.1"); got != domain.HostAuto {
		t.Errorf("expected mw-c.1 on host auto, got %s", got)
	}
	if got := hostOf(t, tracker, "mw-c.3"); got != "cloud2" {
		t.Errorf("expected mw-c.3 left on cloud2, got %s", got)
	}
	if !strings.Contains(report, "mw-c.1") || !strings.Contains(report, "mw-c.2") {
		t.Errorf("expected the report to name the stories moved:\n%s", report)
	}
}

func TestMwCloudDownRefusesAHostThatIsNotABox(t *testing.T) {
	tracker := pathedStories(t)
	_, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: &apptest.FakeCloudBook{}, Host: "desktop",
		Plan: cloudPlan,
	}.Down(context.Background(), "desktop")
	if err == nil {
		t.Error("expected mw cloud down desktop to be refused: desktop is no cloud box")
	}
}

func TestABoxThatCannotBeDestroyedKeepsItsStoriesPathedToIt(t *testing.T) {
	now := time.Date(2026, 10, 17, 22, 0, 0, 0, time.UTC)
	up := now.Add(-2 * time.Hour)
	tracker := pathedStories(t)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up}}}}
	_, _ = application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &stuckCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if got := hostOf(t, tracker, "mw-c.1"); got != "cloud1" {
		t.Errorf("expected mw-c.1 still on cloud1 while the box stands, got %s", got)
	}
}

// goneBoxStories files an epic with open, deferred and claimed stories pathed
// to cloud1, and open ones pathed to laptop, desktop, a named Boost and cloud2.
func goneBoxStories(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-g", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	for _, s := range []struct{ id, host, status string }{
		{"mw-g.1", "cloud1", apptest.StatusOpen},
		{"mw-g.2", "cloud1", apptest.StatusDeferred},
		{"mw-g.3", "cloud1", apptest.StatusInProgress},
		{"mw-g.4", "laptop", apptest.StatusOpen},
		{"mw-g.5", "desktop", apptest.StatusOpen},
		{"mw-g.6", "boost-tower", apptest.StatusOpen},
		{"mw-g.7", "cloud2", apptest.StatusOpen},
	} {
		tracker.AddStory("mw-g", domain.Story{ID: s.id, Title: "story " + s.id})
		if err := tracker.SetStoryMetadata(context.Background(), s.id, map[string]string{"host": s.host}); err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetStatus(s.id, s.status); err != nil {
			t.Fatal(err)
		}
	}
	return tracker
}

func TestAStoryPathedToAGoneBoxIsRepathedByALaterCheck(t *testing.T) {
	now := time.Date(2026, 10, 26, 3, 12, 0, 0, time.UTC)
	tracker := goneBoxStories(t)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10"}}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "laptop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"mw-g.1": domain.HostAuto, "mw-g.2": domain.HostAuto, "mw-g.3": "cloud1", "mw-g.4": "laptop",
		"mw-g.5": "desktop", "mw-g.6": "boost-tower", "mw-g.7": domain.HostAuto,
	} {
		if got := hostOf(t, tracker, id); got != want {
			t.Errorf("expected %s on host %s, got %s", id, want, got)
		}
	}
	text := report.String()
	if !strings.Contains(text, "re-pathed mw-g.1, mw-g.2 from cloud1 to host=auto") {
		t.Errorf("expected the move for cloud1 in the report:\n%s", text)
	}
	if !strings.Contains(text, "re-pathed mw-g.7 from cloud2 to host=auto") {
		t.Errorf("expected the move for cloud2 in the report:\n%s", text)
	}
	if strings.Contains(text, "mw-g.4") || strings.Contains(text, "mw-g.5") || strings.Contains(text, "mw-g.6") {
		t.Errorf("expected stories on hosts that are no cloud box left out:\n%s", text)
	}
}

func TestAStoryPathedToABoxThatIsUpIsLeftAlone(t *testing.T) {
	now := time.Date(2026, 10, 26, 3, 12, 0, 0, time.UTC)
	tracker := goneBoxStories(t)
	up := now.Add(-10 * time.Minute)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up}}}}
	_, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "laptop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"mw-g.1": "cloud1", "mw-g.2": "cloud1", "mw-g.7": domain.HostAuto} {
		if got := hostOf(t, tracker, id); got != want {
			t.Errorf("expected %s on host %s, got %s", id, want, got)
		}
	}
}

// queuedOnBox files an epic with three stories claimed on cloud1: mw-q.1 is
// queued for its batch (its session gone by design), mw-q.2 is running, and
// mw-q.3 is claimed with its session ended and the Mayor to look at it
// (run:blocked). A fourth, mw-q.4, is open and pathed to cloud1.
func queuedOnBox(t *testing.T) *apptest.FakeTracker {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-q", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	for _, id := range []string{"mw-q.1", "mw-q.2", "mw-q.3", "mw-q.4"} {
		tracker.AddStory("mw-q", domain.Story{ID: id, Title: "story " + id})
		if err := tracker.SetStoryMetadata(ctx, id, map[string]string{"host": "cloud1"}); err != nil {
			t.Fatal(err)
		}
	}
	for id, run := range map[string]string{"mw-q.1": application.RunQueued, "mw-q.2": application.RunRunning, "mw-q.3": application.RunBlocked} {
		if err := tracker.ClaimAs(id, "mw@cloud1", time.Time{}); err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetStoryState(ctx, id, application.RunState, run, "the test"); err != nil {
			t.Fatal(err)
		}
	}
	return tracker
}

// claimOf is the assignee and the status a story has now.
func claimOf(t *testing.T, tracker *apptest.FakeTracker, id string) (assignee, status string) {
	t.Helper()
	d, err := tracker.ShowStory(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d.Assignee, d.Status
}

func TestABoxWhoseOnlyClaimIsQueuedForABatchGoesIdleAndIsDestroyed(t *testing.T) {
	now := time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC)
	tracker := queuedOnBox(t)
	for _, id := range []string{"mw-q.2", "mw-q.3"} {
		if err := tracker.ReleaseClaimHeldBy(context.Background(), id, "mw@cloud1"); err != nil {
			t.Fatal(err)
		}
	}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-30 * time.Minute)}}}}
	provider := &apptest.FakeCloud{}
	check := func(at time.Time) {
		t.Helper()
		_, err := application.CloudCheck{
			Work: tracker, Stories: tracker, Provider: provider, Book: book, Host: "desktop",
			Plan: cloudPlan, Now: func() time.Time { return at },
		}.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	check(now)
	if len(book.State.Boxes) != 1 || book.State.Boxes[0].IdleSince.IsZero() {
		t.Fatalf("expected cloud1 marked idle: its only claim waits for a batch and runs no session, got %+v", book.State.Boxes)
	}
	check(now.Add(cloudPlan.Idle))
	if len(book.State.Boxes) != 0 {
		t.Errorf("expected cloud1 destroyed after %s idle, got %+v", cloudPlan.Idle, book.State.Boxes)
	}
}

func TestABoxWithARunningSessionBesideAQueuedClaimStaysBusy(t *testing.T) {
	now := time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC)
	tracker := queuedOnBox(t)
	idleSince := now.Add(-time.Hour)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-2 * time.Hour), IdleSince: idleSince}}}}
	_, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(book.State.Boxes) != 1 || !book.State.Boxes[0].IdleSince.IsZero() {
		t.Errorf("expected cloud1 busy and up while mw-q.2 runs on it, got %+v", book.State.Boxes)
	}
}

// checkHandedBack asserts what destroying cloud1 did to the stories of
// queuedOnBox, and that the text says so.
func checkHandedBack(t *testing.T, tracker *apptest.FakeTracker, text string) {
	t.Helper()
	assignee, status := claimOf(t, tracker, "mw-q.1")
	if assignee != "" || status != apptest.StatusOpen {
		t.Errorf("expected the queued mw-q.1 handed back (open, unassigned), got %q %q", assignee, status)
	}
	if got := hostOf(t, tracker, "mw-q.1"); got != domain.HostAuto {
		t.Errorf("expected mw-q.1 re-pathed to host auto, got %s", got)
	}
	if got := tracker.State("mw-q.1", application.RunState); got != application.RunQueued {
		t.Errorf("expected mw-q.1 still %s=%s, so that its batch lands it and nobody works it again, got %q", application.RunState, application.RunQueued, got)
	}
	for _, id := range []string{"mw-q.2", "mw-q.3"} {
		if assignee, _ := claimOf(t, tracker, id); assignee != "mw@cloud1" {
			t.Errorf("expected %s left to its claim on cloud1, got %q", id, assignee)
		}
		if got := hostOf(t, tracker, id); got != "cloud1" {
			t.Errorf("expected %s left on cloud1, got %s", id, got)
		}
	}
	if got := hostOf(t, tracker, "mw-q.4"); got != domain.HostAuto {
		t.Errorf("expected the open mw-q.4 re-pathed as before, got %s", got)
	}
	if !strings.Contains(text, "mw-q.1") || !strings.Contains(text, "queued") || strings.Contains(text, "mw-q.2") || strings.Contains(text, "mw-q.3") {
		t.Errorf("expected the output to name the queued mw-q.1 handed back, and not the stories left:\n%s", text)
	}
}

func TestMwCloudDownHandsBackAClaimQueuedForABatch(t *testing.T) {
	now := time.Date(2026, 10, 31, 2, 5, 0, 0, time.UTC)
	tracker := queuedOnBox(t)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}}}
	text, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Down(context.Background(), "cloud1")
	if err != nil {
		t.Fatal(err)
	}
	checkHandedBack(t, tracker, text)
}

func TestTheChecksDestroyHandsBackAClaimQueuedForABatch(t *testing.T) {
	now := time.Date(2026, 10, 31, 2, 5, 0, 0, time.UTC)
	tracker := queuedOnBox(t)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Hours: 600, Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}}}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(book.State.Boxes) != 0 {
		t.Fatalf("expected the month's cap to destroy cloud1, got %+v", book.State.Boxes)
	}
	checkHandedBack(t, tracker, report.String())
}

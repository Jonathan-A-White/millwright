package application_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
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
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up, Checked: up}}}}
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
// queuedOnBox, and that the text says so: every claim the box held is given
// back (mw-gq6.1127), whatever became of its session, and re-pathed to
// host=auto. The queued one keeps its run state, so that its batch lands it;
// the others lose theirs, so that a fresh dispatch is not confused by it.
func checkHandedBack(t *testing.T, tracker *apptest.FakeTracker, text string) {
	t.Helper()
	for _, id := range []string{"mw-q.1", "mw-q.2", "mw-q.3"} {
		assignee, status := claimOf(t, tracker, id)
		if assignee != "" || status != apptest.StatusOpen {
			t.Errorf("expected %s handed back (open, unassigned), got %q %q", id, assignee, status)
		}
		if got := hostOf(t, tracker, id); got != domain.HostAuto {
			t.Errorf("expected %s re-pathed to host auto, got %s", id, got)
		}
		if !strings.Contains(text, id) {
			t.Errorf("expected the output to name %s as handed back:\n%s", id, text)
		}
	}
	if got := tracker.State("mw-q.1", application.RunState); got != application.RunQueued {
		t.Errorf("expected mw-q.1 still %s=%s, so that its batch lands it and nobody works it again, got %q", application.RunState, application.RunQueued, got)
	}
	for _, id := range []string{"mw-q.2", "mw-q.3"} {
		if got := tracker.State(id, application.RunState); got != "" {
			t.Errorf("expected the run state of %s cleared with its claim, got %q", id, got)
		}
	}
	if got := hostOf(t, tracker, "mw-q.4"); got != domain.HostAuto {
		t.Errorf("expected the open mw-q.4 re-pathed as before, got %s", got)
	}
}

// A claim on another box is not the destroyed box's to give back.
func TestMwCloudDownLeavesAClaimOnAnotherBoxAlone(t *testing.T) {
	now := time.Date(2026, 11, 12, 18, 50, 0, 0, time.UTC)
	tracker := queuedOnBox(t)
	tracker.AddStory("mw-q", domain.Story{ID: "mw-q.5", Title: "story mw-q.5"})
	if err := tracker.SetStoryMetadata(context.Background(), "mw-q.5", map[string]string{"host": "cloud2"}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ClaimAs("mw-q.5", "mw@cloud2", time.Time{}); err != nil {
		t.Fatal(err)
	}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}}}
	if _, err := (application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}).Down(context.Background(), "cloud1"); err != nil {
		t.Fatal(err)
	}
	if assignee, _ := claimOf(t, tracker, "mw-q.5"); assignee != "mw@cloud2" {
		t.Errorf("expected cloud2's claim left alone, got %q", assignee)
	}
	if got := hostOf(t, tracker, "mw-q.5"); got != "cloud2" {
		t.Errorf("expected mw-q.5 left on cloud2, got %s", got)
	}
}

// aClaimedOnCloud1 files mw-b.1, in progress and claimed by holder on cloud1,
// its run state running, and mw-b.2, claimed by mw@cloud1 the same way.
func aClaimedOnCloud1(t *testing.T, holder string) *apptest.FakeTracker {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-b", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	for id, who := range map[string]string{"mw-b.1": holder, "mw-b.2": "mw@cloud1"} {
		tracker.AddStory("mw-b", domain.Story{ID: id, Title: "story " + id})
		if err := tracker.SetStoryMetadata(ctx, id, map[string]string{"host": "cloud1"}); err != nil {
			t.Fatal(err)
		}
		if err := tracker.ClaimAs(id, who, time.Time{}); err != nil {
			t.Fatal(err)
		}
		if err := tracker.SetStoryState(ctx, id, application.RunState, application.RunRunning, "the test"); err != nil {
			t.Fatal(err)
		}
	}
	return tracker
}

// mw cloud down gives back a claim a Builder session holds as builder@<box>
// as well as the dispatch's mw@<box>, names the lost attempt tooling (mw
// cause), and says on the story that the box was destroyed (mw-gq6.1144).
func TestMwCloudDownHandsBackEveryClaimOnTheBoxNamesTheCauseToolingAndSaysSoOnTheStory(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 14, 0, 0, time.UTC)
	tracker := aClaimedOnCloud1(t, "builder@cloud1")
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}}}
	text, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Down(context.Background(), "cloud1")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"mw-b.1", "mw-b.2"} {
		if assignee, status := claimOf(t, tracker, id); assignee != "" || status != apptest.StatusOpen {
			t.Errorf("expected %s handed back (open, unassigned), got %q %q", id, assignee, status)
		}
		if got := hostOf(t, tracker, id); got != domain.HostAuto {
			t.Errorf("expected %s re-pathed to host auto, got %s", id, got)
		}
		if !strings.Contains(text, id) {
			t.Errorf("expected the output to name %s as handed back:\n%s", id, text)
		}
		if got := tracker.Metadata(id)[application.CauseField]; got != application.CauseTooling {
			t.Errorf("expected the attempt of %s named %s (mw cause), got %q", id, application.CauseTooling, got)
		}
		said := strings.Join(tracker.Comments(id), "\n")
		if !strings.Contains(said, "cloud1") || !strings.Contains(said, "destroyed") || !strings.Contains(said, "mw cloud down") {
			t.Errorf("expected a comment on %s saying cloud1 was destroyed by mw cloud down, got %q", id, said)
		}
	}
}

// --force looks at nothing on the box, and the comment on the story says its
// work was not saved.
func TestMwCloudDownForceSaysOnTheStoryThatItsWorkWasNotSaved(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 14, 0, 0, time.UTC)
	tracker := aClaimedOnCloud1(t, "builder@cloud1")
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}}}
	if _, err := (application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}).DownForce(context.Background(), "cloud1"); err != nil {
		t.Fatal(err)
	}
	said := strings.Join(tracker.Comments("mw-b.1"), "\n")
	if !strings.Contains(said, "not saved") {
		t.Errorf("expected the comment to say the work was not saved, got %q", said)
	}
	if got := tracker.Metadata("mw-b.1")[application.CauseField]; got != application.CauseTooling {
		t.Errorf("expected the attempt named %s, got %q", application.CauseTooling, got)
	}
}

// The check's own destroy of an idle box saves a kept worktree and the comment
// on its story says what was saved.
func TestTheChecksOwnDestroySaysOnTheStoryWhatWasSavedFromTheBox(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	tracker := aClaimedOnCloud1(t, "builder@cloud1")
	tracker.AddStory("mw-b", domain.Story{ID: "mw-5r3p30.754", Title: "the kept one"})
	if err := tracker.ClaimAs("mw-5r3p30.754", "builder@cloud1", time.Time{}); err != nil {
		t.Fatal(err)
	}
	r.check.Work, r.check.Stories = tracker, tracker
	if _, err := r.check.Down(context.Background(), "cloud1"); err != nil {
		t.Fatal(err)
	}
	said := strings.Join(tracker.Comments("mw-5r3p30.754"), "\n")
	if !strings.Contains(said, application.BundleRecord("mw-5r3p30.754", 1)) {
		t.Errorf("expected the comment to name the bundle saved, got %q", said)
	}
	if other := strings.Join(tracker.Comments("mw-b.1"), "\n"); strings.Contains(other, application.BundleRecord("mw-5r3p30.754", 1)) || !strings.Contains(other, "destroyed") {
		t.Errorf("expected the other story's comment to say the box was destroyed, got %q", other)
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

// A box restored from the snapshot carries its units: the timer is hourly,
// and a box has no event follower to spring its dispatch (mw-gq6.1004).
func paceCheck(t *testing.T, cloud *apptest.FakeCloud, box application.CloudBox) (application.CloudReport, *apptest.FakeCloudBook) {
	t.Helper()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	box.Up, box.Made, box.Dispatched = now.Add(-30*time.Minute), now.Add(-30*time.Minute), now.Add(-20*time.Minute)
	box.Cap = 2
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
	report, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: cloud, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report, book
}

func TestABoxOnTheSnapshotsHourlyTimerIsSetToTheFiveMinutePaceAndTheReportSaysSo(t *testing.T) {
	cloud := &apptest.FakeCloud{Paces: map[string]string{"cloud1": "hourly"}}
	report, book := paceCheck(t, cloud, application.CloudBox{Name: "cloud1"})
	if got := cloud.Paces["cloud1"]; got != application.CloudPaceFive {
		t.Errorf("expected the box's timer set to OnCalendar %s, got %q", application.CloudPaceFive, got)
	}
	if got := book.State.Boxes[0].Pace; got != application.CloudPaceFive {
		t.Errorf("expected the book to keep the pace %s, got %q", application.CloudPaceFive, got)
	}
	if text := report.String(); !strings.Contains(text, "cloud1: dispatch timer every 5 minutes") {
		t.Errorf("expected the report to name the pace, got:\n%s", text)
	}
}

func TestABoxAlreadyAtTheFiveMinutePaceIsNotReachedAgain(t *testing.T) {
	cloud := &apptest.FakeCloud{}
	report, _ := paceCheck(t, cloud, application.CloudBox{Name: "cloud1", Pace: application.CloudPaceFive})
	if calls := cloud.PaceCalls(); len(calls) != 0 {
		t.Errorf("expected no ssh to a box known to be at the pace, got %v", calls)
	}
	if text := report.String(); !strings.Contains(text, "cloud1: dispatch timer every 5 minutes") {
		t.Errorf("expected the report to name the pace, got:\n%s", text)
	}
}

func TestABoxWhoseTimerStaysHourlyIsSaidHourlyInTheReport(t *testing.T) {
	cloud := &apptest.FakeCloud{Paces: map[string]string{"cloud1": "hourly"}, PaceStuck: map[string]bool{"cloud1": true}}
	report, book := paceCheck(t, cloud, application.CloudBox{Name: "cloud1"})
	if text := report.String(); !strings.Contains(text, "cloud1: dispatch timer hourly") {
		t.Errorf("expected the report to say the timer is hourly, got:\n%s", text)
	}
	if got := book.State.Boxes[0].Pace; got != "hourly" {
		t.Errorf("expected the book to keep the pace it is at, hourly, got %q", got)
	}
}

func TestABoxThatCannotBeReachedForItsPaceIsSaidSoAndTriedAgain(t *testing.T) {
	cloud := &apptest.FakeCloud{Unreachable: map[string]bool{"cloud1": true}}
	report, book := paceCheck(t, cloud, application.CloudBox{Name: "cloud1"})
	if text := report.String(); !strings.Contains(text, "cloud1: dispatch timer pace not known") {
		t.Errorf("expected the report to say the pace is not known, got:\n%s", text)
	}
	if got := book.State.Boxes[0].PaceFault; got == "" {
		t.Errorf("expected the book to keep why the pace is not known")
	}
	// The next check asks again.
	paceCheck(t, cloud, book.State.Boxes[0])
	if calls := cloud.PaceCalls(); len(calls) != 2 {
		t.Errorf("expected the box tried at each check, got %v", calls)
	}
}

// harnessCheck is paceCheck with the home's Claude Code at version.
func harnessCheck(t *testing.T, cloud *apptest.FakeCloud, version string, box application.CloudBox) (application.CloudReport, *apptest.FakeCloudBook) {
	t.Helper()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	box.Up, box.Made, box.Dispatched = now.Add(-30*time.Minute), now.Add(-30*time.Minute), now.Add(-20*time.Minute)
	box.Cap = 2
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
	report, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: cloud, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
		HarnessVersion: func(context.Context) (string, error) { return version, nil },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report, book
}

func TestABoxBehindTheHomesHarnessIsBroughtLevelBeforeItsTimerIsSetAndTheMovesHoldBothVersions(t *testing.T) {
	cloud := &apptest.FakeCloud{Harnesses: map[string]string{"cloud1": "2.1.300"}}
	report, book := harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1"})
	if got := cloud.HarnessCalls(); len(got) != 1 || got[0] != "cloud1 2.1.322" {
		t.Errorf("expected exactly one install asked, of 2.1.322, got %v", got)
	}
	if got, want := cloud.Order(), []string{"harness cloud1", "pace cloud1"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("expected the harness brought level before the timer is set, got %v", got)
	}
	if got := cloud.Harnesses["cloud1"]; got != "2.1.322" {
		t.Errorf("expected the box at 2.1.322, got %q", got)
	}
	want := "cloud1: claude 2.1.300 to 2.1.322 (the home's)"
	if !slices.ContainsFunc(report.Moves, func(m string) bool { return strings.Contains(m, want) }) {
		t.Errorf("expected a move %q, got %q", want, report.Moves)
	}
	if !slices.ContainsFunc(book.State.Moves, func(m application.CloudMove) bool { return strings.Contains(m.What, want) }) {
		t.Errorf("expected the book to keep the move %q, got %+v", want, book.State.Moves)
	}
}

func TestABoxWhoseHarnessInstallFailsGoesOnAndItsTimerIsSetAndOneLineSaysWhy(t *testing.T) {
	cloud := &apptest.FakeCloud{
		Harnesses:    map[string]string{"cloud1": "2.1.300"},
		HarnessStuck: map[string]string{"cloud1": "npm install failed: npm error network timeout"},
	}
	report, book := harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1"})
	if got := book.State.Boxes[0].Pace; got != application.CloudPaceFive {
		t.Errorf("expected the timer set all the same, got pace %q", got)
	}
	want := "cloud1: claude stays at 2.1.300, not 2.1.322: npm install failed: npm error network timeout"
	n := 0
	for _, m := range report.Moves {
		if strings.Contains(m, "claude stays") {
			n++
			if !strings.Contains(m, want) {
				t.Errorf("expected the line %q, got %q", want, m)
			}
		}
	}
	if n != 1 {
		t.Errorf("expected one line saying the harness stays, got %d in %q", n, report.Moves)
	}
}

func TestABoxAlreadyLevelWithTheHomesHarnessRunsNoInstallAndSaysNothingMoved(t *testing.T) {
	cloud := &apptest.FakeCloud{Harnesses: map[string]string{"cloud1": "2.1.322"}}
	report, _ := harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1"})
	if got := cloud.Harnesses["cloud1"]; got != "2.1.322" {
		t.Errorf("expected the box left at 2.1.322, got %q", got)
	}
	for _, m := range report.Moves {
		if strings.Contains(m, "claude") {
			t.Errorf("expected no move about the harness on a level box, got %q", m)
		}
	}
}

func TestABoxLevelAtThePaceIsNotAskedItsHarnessAgainAndNoHomeVersionAsksNothing(t *testing.T) {
	cloud := &apptest.FakeCloud{}
	harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1", Pace: application.CloudPaceFive, Harness: "2.1.322"})
	if got := cloud.HarnessCalls(); len(got) != 0 {
		t.Errorf("expected a box already brought up and level left alone, got %v", got)
	}
	harnessCheck(t, cloud, "", application.CloudBox{Name: "cloud2"})
	if got := cloud.HarnessCalls(); len(got) != 0 {
		t.Errorf("expected a home that cannot say its version to ask nothing, got %v", got)
	}
	harnessCheck(t, cloud, "", application.CloudBox{Name: "cloud3", Pace: application.CloudPaceFive})
	if got := cloud.HarnessCalls(); len(got) != 0 {
		t.Errorf("expected a box at the pace and a home with no version to ask nothing, got %v", got)
	}
}

func TestACloudBoxAtTheFiveMinutePaceIsBroughtLevelWhenTheHomesHarnessMoves(t *testing.T) {
	cloud := &apptest.FakeCloud{Harnesses: map[string]string{"cloud1": "2.1.320"}}
	report, book := harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1", Pace: application.CloudPaceFive, Harness: "2.1.320"})
	if got := cloud.HarnessCalls(); len(got) != 1 || got[0] != "cloud1 2.1.322" {
		t.Errorf("expected exactly one install asked, of 2.1.322, got %v", got)
	}
	if got := cloud.PaceCalls(); len(got) != 0 {
		t.Errorf("expected the timer of a box at the pace left alone, got %v", got)
	}
	want := "cloud1: claude 2.1.320 to 2.1.322 (the home's)"
	if !slices.ContainsFunc(report.Moves, func(m string) bool { return strings.Contains(m, want) }) {
		t.Errorf("expected a move %q, got %q", want, report.Moves)
	}
	if got := book.State.Boxes[0].Harness; got != "2.1.322" {
		t.Errorf("expected the book to keep the box at 2.1.322, got %q", got)
	}
	if len(book.Whys) == 0 {
		t.Errorf("expected the book written")
	}
}

func TestACloudBoxAtThePaceWithNoKeptHarnessIsBroughtLevel(t *testing.T) {
	cloud := &apptest.FakeCloud{Harnesses: map[string]string{"cloud1": "2.1.300"}}
	_, book := harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1", Pace: application.CloudPaceFive})
	if got := cloud.HarnessCalls(); len(got) != 1 {
		t.Errorf("expected one install asked, got %v", got)
	}
	if got := book.State.Boxes[0].Harness; got != "2.1.322" {
		t.Errorf("expected the book to keep 2.1.322, got %q", got)
	}
}

func TestALevelBoxIsNotReachedAgain(t *testing.T) {
	cloud := &apptest.FakeCloud{}
	harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1", Pace: application.CloudPaceFive, Harness: "2.1.322"})
	if got := cloud.HarnessCalls(); len(got) != 0 {
		t.Errorf("expected no SetHarness to a level box, got %v", got)
	}
	if got := cloud.PaceCalls(); len(got) != 0 {
		t.Errorf("expected no SetPace to a level box, got %v", got)
	}
}

func TestABoxWhoseHarnessStaysAtThePaceIsSaidOnceAndTriedAgain(t *testing.T) {
	cloud := &apptest.FakeCloud{
		Harnesses:    map[string]string{"cloud1": "2.1.300"},
		HarnessStuck: map[string]string{"cloud1": "npm install failed"},
	}
	_, book := harnessCheck(t, cloud, "2.1.322", application.CloudBox{Name: "cloud1", Pace: application.CloudPaceFive, Harness: "2.1.300"})
	if got := book.State.Boxes[0].Harness; got != "2.1.300" {
		t.Errorf("expected the kept version left at 2.1.300, got %q", got)
	}
	report, _ := harnessCheck(t, cloud, "2.1.322", book.State.Boxes[0])
	if got := cloud.HarnessCalls(); len(got) != 2 {
		t.Errorf("expected the install tried at each pass, got %v", got)
	}
	for _, m := range report.Moves {
		if strings.Contains(m, "claude stays") {
			t.Errorf("expected the same failure not a move twice, got %q", m)
		}
	}
}

func TestTheHomesHarnessIsReadOncePerPass(t *testing.T) {
	cloud := &apptest.FakeCloud{}
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	var boxes []application.CloudBox
	for _, name := range []string{"cloud1", "cloud2", "cloud3"} {
		boxes = append(boxes, application.CloudBox{Name: name, Pace: application.CloudPaceFive, Harness: "2.1.322", Cap: 2,
			Up: now.Add(-30 * time.Minute), Made: now.Add(-30 * time.Minute), Dispatched: now.Add(-20 * time.Minute)})
	}
	for _, home := range []string{"2.1.322", ""} {
		reads := 0
		book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: append([]application.CloudBox(nil), boxes...)}}
		report, err := application.CloudCheck{
			Work: apptest.NewFakeTracker(), Provider: cloud, Book: book, Host: "desktop",
			Plan: cloudPlan, Now: func() time.Time { return now },
			HarnessVersion: func(context.Context) (string, error) { reads++; return home, nil },
		}.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if reads > 1 {
			t.Errorf("home %q: expected the home's version read at most once, read %d times", home, reads)
		}
		if home == "" && strings.Count(report.String(), "version is not known") > 1 {
			t.Errorf("expected the unknown version said once, got:\n%s", report.String())
		}
	}
	if got := cloud.HarnessCalls(); len(got) != 0 {
		t.Errorf("expected no box brought level, got %v", got)
	}
}

// denyCheck is paceCheck with the home's [session] deny rules as given.
func denyCheck(t *testing.T, cloud *apptest.FakeCloud, home []string, box application.CloudBox) (application.CloudReport, *apptest.FakeCloudBook) {
	t.Helper()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	box.Up, box.Made, box.Dispatched = now.Add(-30*time.Minute), now.Add(-30*time.Minute), now.Add(-20*time.Minute)
	box.Cap, box.Pace = 2, application.CloudPaceFive
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
	report, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: cloud, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
		SessionDeny: func(context.Context) ([]string, error) { return home, nil },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report, book
}

var homeDeny = []string{"Bash(mw config set *)", "Bash(scp *)", "Read(~/.ssh/**)"}

func TestABoxWhoseSessionDenyDiffersFromTheHomesIsBroughtLevelOnceAndItIsAMove(t *testing.T) {
	cloud := &apptest.FakeCloud{SessionDenies: map[string]string{"cloud1": `["Bash(scp *)"]`}}
	report, book := denyCheck(t, cloud, homeDeny, application.CloudBox{Name: "cloud1"})
	if got := cloud.SessionDenyCalls(); len(got) != 1 || got[0] != "cloud1 "+domain.SessionDenyLiteral(homeDeny) {
		t.Errorf("expected exactly one set, of the home's list, got %v", got)
	}
	if got := cloud.SessionDenies["cloud1"]; got != domain.SessionDenyLiteral(homeDeny) {
		t.Errorf("expected the box to hold the home's list, got %q", got)
	}
	want := "cloud1: session.deny set to the home's 3 rules"
	if !slices.ContainsFunc(report.Moves, func(m string) bool { return strings.Contains(m, want) }) {
		t.Errorf("expected a move %q, got %q", want, report.Moves)
	}
	if !slices.ContainsFunc(book.State.Moves, func(m application.CloudMove) bool { return strings.Contains(m.What, want) }) {
		t.Errorf("expected the book to keep the move %q, got %+v", want, book.State.Moves)
	}
	if got := book.State.Boxes[0].SessionDeny; got != domain.SessionDenyLiteral(homeDeny) {
		t.Errorf("expected the book to keep the list the box was brought to, got %q", got)
	}
	// The next pass, with the book kept, does not reach the box again.
	denyCheck(t, cloud, homeDeny, book.State.Boxes[0])
	if got := cloud.SessionDenyCalls(); len(got) != 1 {
		t.Errorf("expected the box set once, got %v", got)
	}
}

func TestABoxLevelWithTheHomesSessionDenyGetsNoSshAndNoMove(t *testing.T) {
	cloud := &apptest.FakeCloud{}
	report, _ := denyCheck(t, cloud, homeDeny, application.CloudBox{Name: "cloud1", SessionDeny: domain.SessionDenyLiteral(homeDeny)})
	if got := cloud.SessionDenyCalls(); len(got) != 0 {
		t.Errorf("expected no set to a level box, got %v", got)
	}
	for _, m := range report.Moves {
		if strings.Contains(m, "session.deny") {
			t.Errorf("expected no move for a level box, got %q", m)
		}
	}
}

func TestABoxThatAlreadyHoldsTheHomesSessionDenyIsRecordedLevelWithNoMove(t *testing.T) {
	cloud := &apptest.FakeCloud{SessionDenies: map[string]string{"cloud1": domain.SessionDenyLiteral(homeDeny)}}
	report, book := denyCheck(t, cloud, homeDeny, application.CloudBox{Name: "cloud1"})
	if got := book.State.Boxes[0].SessionDeny; got != domain.SessionDenyLiteral(homeDeny) {
		t.Errorf("expected the box recorded level, got %q", got)
	}
	for _, m := range report.Moves {
		if strings.Contains(m, "session.deny") {
			t.Errorf("expected no move for a box already level, got %q", m)
		}
	}
}

func TestABoxThatCannotBeReachedForItsSessionDenyIsTriedAgainAndNothingIsRecorded(t *testing.T) {
	cloud := &apptest.FakeCloud{Unreachable: map[string]bool{"cloud1": true}}
	_, book := denyCheck(t, cloud, homeDeny, application.CloudBox{Name: "cloud1"})
	if got := book.State.Boxes[0].SessionDeny; got != "" {
		t.Errorf("expected nothing recorded for a box not reached, got %q", got)
	}
	denyCheck(t, cloud, homeDeny, book.State.Boxes[0])
	if got := cloud.SessionDenyCalls(); len(got) != 2 {
		t.Errorf("expected the box tried at each pass, got %v", got)
	}
}

func TestAHomeWithNoSessionDenyOrOneThatCannotBeReadSetsNoBox(t *testing.T) {
	for name, home := range map[string]func(context.Context) ([]string, error){
		"unset":      func(context.Context) ([]string, error) { return nil, nil },
		"unreadable": func(context.Context) ([]string, error) { return nil, errors.New("config.toml does not parse") },
	} {
		cloud := &apptest.FakeCloud{}
		now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
		box := application.CloudBox{Name: "cloud1", Pace: application.CloudPaceFive, Cap: 2, Up: now.Add(-30 * time.Minute), Made: now.Add(-30 * time.Minute), Dispatched: now.Add(-20 * time.Minute)}
		book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
		if _, err := (application.CloudCheck{
			Work: apptest.NewFakeTracker(), Provider: cloud, Book: book, Host: "desktop",
			Plan: cloudPlan, Now: func() time.Time { return now }, SessionDeny: home,
		}).Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := cloud.SessionDenyCalls(); len(got) != 0 {
			t.Errorf("%s: expected no box set, got %v", name, got)
		}
	}
}

func TestHarnessBehindComparesVersionsAsNumbers(t *testing.T) {
	for _, c := range []struct {
		have, want string
		behind     bool
	}{
		{"2.1.300", "2.1.322", true}, {"2.1.322", "2.1.322", false}, {"2.1.330", "2.1.322", false},
		{"2.1.99", "2.1.322", true}, {"2.1.1000", "2.1.322", false}, {"2.2", "2.1.322", false}, {"2.1", "2.1.1", true},
	} {
		if got := application.HarnessBehind(c.have, c.want); got != c.behind {
			t.Errorf("HarnessBehind(%q, %q) = %v, want %v", c.have, c.want, got, c.behind)
		}
	}
}

// fakeBoxWork is the worktrees a cloud box keeps, as the home asks over ssh: it
// writes a bundle into the fake vault as the adapter writes one into the real
// one, and notes whether the provider had destroyed the box by then.
type fakeBoxWork struct {
	vault    *fakeVault
	provider *apptest.FakeCloud
	trees    map[string][]application.CloudKeptWorktree
	diff     string

	listErr, bundleErr, verifyErr, diffErr error

	bundled, verified []string
	downBeforeBundle  bool
}

func (f *fakeBoxWork) Worktrees(_ context.Context, box string) ([]application.CloudKeptWorktree, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.trees[box], nil
}

func (f *fakeBoxWork) Bundle(_ context.Context, box string, wt application.CloudKeptWorktree, dest string) error {
	if slices.Contains(f.provider.Calls(), "down "+box) {
		f.downBeforeBundle = true
	}
	if f.bundleErr != nil {
		return f.bundleErr
	}
	name := dest[strings.LastIndex(dest, "/")+1:]
	f.vault.written[wt.Story+"/"+name] = "bundle of " + wt.Branch
	f.bundled = append(f.bundled, wt.Story+"/"+name)
	return nil
}

func (f *fakeBoxWork) VerifyBundle(_ context.Context, _ string, wt application.CloudKeptWorktree, dest string) error {
	f.verified = append(f.verified, dest)
	return f.verifyErr
}

func (f *fakeBoxWork) UncommittedDiff(context.Context, string, application.CloudKeptWorktree) (string, error) {
	return f.diff, f.diffErr
}

// aBoxWithWork is a check of cloud1, idle since long before, whose box keeps
// the worktrees given, wired to fakes that save into a fake vault.
type boxWorkRig struct {
	check    application.CloudCheck
	book     *apptest.FakeCloudBook
	provider *apptest.FakeCloud
	box      *fakeBoxWork
	vault    *fakeVault
	files    *apptest.FakeVaultFiles
	log      *apptest.FakeEventLog
}

func aBoxWithWork(trees ...application.CloudKeptWorktree) *boxWorkRig {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	up := now.Add(-2 * time.Hour)
	r := &boxWorkRig{
		book:     &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up}}}},
		provider: &apptest.FakeCloud{},
		vault:    newFakeVault(),
		files:    &apptest.FakeVaultFiles{},
		log:      &apptest.FakeEventLog{},
	}
	r.box = &fakeBoxWork{vault: r.vault, provider: r.provider, trees: map[string][]application.CloudKeptWorktree{"cloud1": trees}}
	r.check = application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: r.provider, Book: r.book, Log: r.log, Host: "desktop",
		Box: r.box, Vault: r.vault, Files: r.files,
		Plan: cloudPlan, Now: func() time.Time { return now },
	}
	return r
}

var aheadTree = application.CloudKeptWorktree{
	Story: "mw-5r3p30.754", Branch: "mw/mw-5r3p30.754", Dir: "/home/mw/.mw-worktrees/mw-5r3p30.754", Head: "abc123", Ahead: 3,
}

func (r *boxWorkRig) moves(t *testing.T) string {
	t.Helper()
	var all []string
	for _, m := range r.book.State.Moves {
		all = append(all, m.What)
	}
	return strings.Join(all, "\n")
}

func TestAnIdleBoxHasItsKeptBranchBundledAndVerifiedBeforeItIsDestroyed(t *testing.T) {
	tree := aheadTree
	tree.Uncommitted = []string{"application/x.go"}
	r := aBoxWithWork(tree)
	r.box.diff = "diff --git a/application/x.go b/application/x.go\n"
	if _, err := r.check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.box.downBeforeBundle {
		t.Error("the box was destroyed before its branch was bundled")
	}
	if !slices.Contains(r.provider.Calls(), "down cloud1") {
		t.Fatalf("expected cloud1 destroyed once its work was saved, calls %v", r.provider.Calls())
	}
	if want := []string{"mw-5r3p30.754/" + application.BundleFileName(1)}; !slices.Equal(r.box.bundled, want) {
		t.Errorf("expected the bundle %v, got %v", want, r.box.bundled)
	}
	if len(r.box.verified) != 1 || !strings.HasSuffix(r.box.verified[0], "/"+application.BundleFileName(1)) {
		t.Errorf("expected the bundle verified, got %v", r.box.verified)
	}
	if got := r.vault.written["mw-5r3p30.754/"+application.PatchFileName(1)]; got != r.box.diff {
		t.Errorf("expected the uncommitted diff kept as %s, got %q", application.PatchFileName(1), got)
	}
	commits := r.files.Commits()
	if len(commits) != 1 || !slices.Equal(commits[0].Paths, []string{application.BundleRecord("mw-5r3p30.754", 1), application.PatchRecord("mw-5r3p30.754", 1)}) {
		t.Errorf("expected one vault commit of the bundle and the patch, got %+v", commits)
	}
	if _, _, pushes := r.files.Moves(); pushes == 0 {
		t.Error("expected the vault commit pushed")
	}
	if !strings.Contains(r.moves(t), "saved mw-5r3p30.754 from cloud1") {
		t.Errorf("expected a move saying what was saved:\n%s", r.moves(t))
	}
}

func TestABoxWhoseBundleFailsIsNotDestroyedAndTheReportNamesTheStory(t *testing.T) {
	for name, set := range map[string]func(*fakeBoxWork){
		"the bundle fails":           func(b *fakeBoxWork) { b.bundleErr = errors.New("git bundle: fatal: Refusing to create empty bundle") },
		"the bundle will not verify": func(b *fakeBoxWork) { b.verifyErr = errors.New("the bundle requires this ref: deadbeef") },
	} {
		t.Run(name, func(t *testing.T) {
			r := aBoxWithWork(aheadTree)
			set(r.box)
			_, err := r.check.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), "mw-5r3p30.754") || !strings.Contains(err.Error(), "cloud1") {
				t.Errorf("expected the check to fail naming cloud1 and the story, got %v", err)
			}
			if slices.Contains(r.provider.Calls(), "down cloud1") {
				t.Errorf("expected the box not destroyed, calls %v", r.provider.Calls())
			}
			if len(r.book.State.Boxes) != 1 {
				t.Errorf("expected the box still in the book, got %+v", r.book.State.Boxes)
			}
			if got := r.moves(t); !strings.Contains(got, "not destroying cloud1") || !strings.Contains(got, "mw-5r3p30.754") || !strings.Contains(got, "stays up") {
				t.Errorf("expected a move naming the story and why the box stays, got:\n%s", got)
			}
			if len(r.files.Commits()) != 0 {
				t.Errorf("expected nothing committed to the vault, got %+v", r.files.Commits())
			}
		})
	}
}

func TestABoxThatCannotBeReachedForItsWorktreesIsNotDestroyed(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	r.box.listErr = errors.New("ssh: connect to host 10.88.0.8 port 22: Connection timed out")
	_, err := r.check.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cloud1") || !strings.Contains(err.Error(), "Connection timed out") {
		t.Errorf("expected the check to fail saying why, got %v", err)
	}
	if slices.Contains(r.provider.Calls(), "down cloud1") || len(r.book.State.Boxes) != 1 {
		t.Errorf("expected the box kept, calls %v, book %+v", r.provider.Calls(), r.book.State.Boxes)
	}
}

func TestARefusedTeardownIsAMoveOnceAndNotAtEveryCheck(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	r.box.bundleErr = errors.New("disk full")
	now := r.check.Now()
	for i := range 3 {
		// The idle time in the line grows at each check.
		r.check.Now = func() time.Time { return now.Add(time.Duration(i) * time.Minute) }
		if _, err := r.check.Run(context.Background()); err == nil {
			t.Fatal("expected each check to fail")
		}
	}
	if n := strings.Count(r.moves(t), "not destroying cloud1"); n != 1 {
		t.Errorf("expected the refusal a move once, got %d times:\n%s", n, r.moves(t))
	}
}

func TestMwCloudDownSavesTheKeptWorktreesFirstAndRefusesWhenItCannot(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	text, err := r.check.Down(context.Background(), "cloud1")
	if err != nil {
		t.Fatal(err)
	}
	if r.box.downBeforeBundle || len(r.box.bundled) != 1 || !slices.Contains(r.provider.Calls(), "down cloud1") {
		t.Errorf("expected the work bundled and then the box destroyed, bundled %v, calls %v", r.box.bundled, r.provider.Calls())
	}
	if !strings.Contains(text, "saved mw-5r3p30.754") || !strings.Contains(text, "down cloud1") {
		t.Errorf("expected the report to say both, got:\n%s", text)
	}

	r = aBoxWithWork(aheadTree)
	r.box.bundleErr = errors.New("disk full")
	text, err = r.check.Down(context.Background(), "cloud1")
	if err == nil || !strings.Contains(err.Error(), "mw-5r3p30.754") || slices.Contains(r.provider.Calls(), "down cloud1") {
		t.Errorf("expected mw cloud down refused naming the story, got %v, calls %v", err, r.provider.Calls())
	}
	if !strings.Contains(text, "not destroying cloud1") || len(r.book.State.Boxes) != 1 {
		t.Errorf("expected the line and the box kept, got %q, %+v", text, r.book.State.Boxes)
	}
}

func TestABoxWithNothingToSaveIsDestroyedAsBefore(t *testing.T) {
	clean := aheadTree
	clean.Ahead = 0
	for name, trees := range map[string][]application.CloudKeptWorktree{"no worktree": nil, "a worktree with nothing ahead and nothing uncommitted": {clean}} {
		t.Run(name, func(t *testing.T) {
			r := aBoxWithWork(trees...)
			if _, err := r.check.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(r.provider.Calls(), "down cloud1") || len(r.box.bundled) != 0 || len(r.files.Commits()) != 0 {
				t.Errorf("expected the box destroyed with nothing bundled, calls %v, bundled %v", r.provider.Calls(), r.box.bundled)
			}
		})
	}
}

func TestAStoryBundledBeforeKeepsEveryEarlierAttemptsFile(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	r.vault.written["mw-5r3p30.754/"+application.BundleFileName(1)] = "the first attempt's bundle"
	r.vault.written["mw-5r3p30.754/"+application.BundleFileName(2)] = "the second attempt's bundle"
	if _, err := r.check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"mw-5r3p30.754/" + application.BundleFileName(3)}; !slices.Equal(r.box.bundled, want) {
		t.Errorf("expected the new bundle beside the two earlier ones as %v, got %v", want, r.box.bundled)
	}
	if r.vault.written["mw-5r3p30.754/"+application.BundleFileName(1)] != "the first attempt's bundle" {
		t.Error("expected the first attempt's bundle untouched")
	}
}

func TestABoxWhoseWorkIsSavedIsNotBundledAgainWhenTheDestroyingFailsAndIsTriedAgain(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	r.check.Provider = &stuckCloud{}
	for range 2 {
		_, _ = r.check.Run(context.Background())
	}
	if len(r.box.bundled) != 1 {
		t.Errorf("expected the branch bundled once, got %v", r.box.bundled)
	}
}

func TestACloudBoxIsNeverAskedForWorktreesWhenItDidNotComeUp(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	r := aBoxWithWork(aheadTree)
	r.book.State.Boxes = []application.CloudBox{{Name: "cloud1", Up: now.Add(-time.Hour), Making: true}}
	r.box.listErr = errors.New("unreachable")
	if _, err := r.check.Down(context.Background(), "cloud1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.provider.Calls(), "down cloud1") {
		t.Errorf("expected a box still being made destroyed without asking it anything, calls %v", r.provider.Calls())
	}
}

// atTheCap is a box whose month is spent: the cloud check finds one more hour
// of it past the cap.
func atTheCap(trees ...application.CloudKeptWorktree) *boxWorkRig {
	r := aBoxWithWork(trees...)
	r.book.State.Boxes[0].IdleSince = time.Time{}
	r.book.State.Hours = 600
	r.check.Notice = &apptest.FakeCloudNotice{}
	return r
}

func TestAtTheMonthsCapABoxWhoseSalvageFailsIsDestroyedAfterOneTryAndTheMoveSaysWhatWasNotSaved(t *testing.T) {
	r := atTheCap(aheadTree)
	r.box.bundleErr = errors.New("disk full")
	if _, err := r.check.Run(context.Background()); err != nil {
		t.Fatalf("expected the cap to win without a failed check, got %v", err)
	}
	if n := strings.Count(strings.Join(r.provider.Calls(), "\n"), "down cloud1"); n != 1 {
		t.Errorf("expected Provider.Down once after the one salvage try, calls %v", r.provider.Calls())
	}
	if len(r.book.State.Boxes) != 0 {
		t.Errorf("expected the box out of the book, got %+v", r.book.State.Boxes)
	}
	got := r.moves(t)
	for _, want := range []string{"cloud1", "mw-5r3p30.754", "mw/mw-5r3p30.754", "abc123", "not saved", "disk full"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the moves to say %q, got:\n%s", want, got)
		}
	}
	cards := r.check.Notice.(*apptest.FakeCloudNotice).Cards
	if len(cards) != 1 || !strings.Contains(cards[0], "cloud1") || !strings.Contains(cards[0], "mw-5r3p30.754") {
		t.Errorf("expected one alert naming the box and the story, got %v", cards)
	}
}

func TestAtTheMonthsCapABoxThatCannotBeReachedIsDestroyedAnyway(t *testing.T) {
	r := atTheCap(aheadTree)
	r.box.listErr = errors.New("ssh: Connection timed out")
	if _, err := r.check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.provider.Calls(), "down cloud1") {
		t.Errorf("expected the box destroyed at the cap, calls %v", r.provider.Calls())
	}
	if got := r.moves(t); !strings.Contains(got, "Connection timed out") || !strings.Contains(got, "not saved") {
		t.Errorf("expected a move saying why nothing was saved, got:\n%s", got)
	}
}

func TestAtTheMonthsCapABoxWhoseWorkIsSavedIsDestroyedWithNoAlert(t *testing.T) {
	r := atTheCap(aheadTree)
	if _, err := r.check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cards := r.check.Notice.(*apptest.FakeCloudNotice).Cards; len(cards) != 0 {
		t.Errorf("expected no alert when everything was saved, got %v", cards)
	}
}

func TestBelowTheCapARefusedTeardownAlertsTheGovernorOnce(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	notice := &apptest.FakeCloudNotice{}
	r.check.Notice = notice
	r.box.bundleErr = errors.New("disk full")
	now := r.check.Now()
	for i := range 3 {
		r.check.Now = func() time.Time { return now.Add(time.Duration(i) * time.Minute) }
		if _, err := r.check.Run(context.Background()); err == nil {
			t.Fatal("expected each check to fail")
		}
		if i == 0 {
			if len(notice.Cards) != 1 {
				t.Fatalf("expected one alert at the first refusal, got %v", notice.Cards)
			}
			for _, want := range []string{"cloud1", "mw-5r3p30.754", "disk full", "0.13", "mw cloud down --force cloud1"} {
				if !strings.Contains(notice.Cards[0], want) {
					t.Errorf("expected the alert to say %q, got %q", want, notice.Cards[0])
				}
			}
		}
	}
	if len(notice.Cards) != 1 {
		t.Errorf("expected the later refused checks to send none, got %v", notice.Cards)
	}
}

func TestMwCloudDownForceDestroysWithoutSalvageAndSaysTheWorkWasNotSaved(t *testing.T) {
	r := aBoxWithWork(aheadTree)
	r.box.bundleErr = errors.New("disk full")
	r.book.State.Boxes[0].Refused = ": mw-5r3p30.754: disk full"
	text, err := r.check.DownForce(context.Background(), "cloud1")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.provider.Calls(), "down cloud1") {
		t.Errorf("expected Provider.Down, calls %v", r.provider.Calls())
	}
	if len(r.box.bundled) != 0 || r.box.verified != nil {
		t.Errorf("expected no salvage, bundled %v", r.box.bundled)
	}
	if !strings.Contains(text, "not saved") || !strings.Contains(text, "--force") {
		t.Errorf("expected the output to say the work was not saved, got %q", text)
	}
	if got := r.moves(t); !strings.Contains(got, "not saved") {
		t.Errorf("expected the move to say so, got:\n%s", got)
	}
	if len(r.book.State.Boxes) != 0 {
		t.Errorf("expected the box out of the book, got %+v", r.book.State.Boxes)
	}
}

// fullHostsWithAutoWaiting files an epic with n stories claimed on each of the
// given hosts and two stories waiting for any host.
func fullHostsWithAutoWaiting(t *testing.T, now time.Time, running map[string]int) *apptest.FakeTracker {
	t.Helper()
	ctx := context.Background()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-r", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	next := 0
	file := func(host string) string {
		next++
		id := fmt.Sprintf("mw-r.%d", next)
		tracker.AddStory("mw-r", domain.Story{ID: id, Title: "story " + id})
		if err := tracker.SetStoryMetadata(ctx, id, map[string]string{"host": host}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	for host, n := range running {
		for i := 0; i < n; i++ {
			if err := tracker.ClaimAs(file(host), "mw@"+host, now.Add(5*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	file(domain.HostAuto)
	file(domain.HostAuto)
	return tracker
}

// A host named in retired_hosts is no place a session runs, whatever
// [cloud.caps] says of it (mw-gq6.1111): its line must not count as free
// sessions, or no box is made while every live host is full.
func TestARetiredHostsCapCountsNoFreeSessionsSoABoxIsMade(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 55, 0, 0, time.UTC)
	tracker := fullHostsWithAutoWaiting(t, now, map[string]int{"laptop": 2})
	plan := cloudPlan
	plan.HostCaps = map[string]int{"laptop": 2, "desktop": 6}
	plan.Retired = []string{"desktop"}
	provider := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: provider, Book: book, Host: "laptop",
		Plan: plan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Free != 0 {
		t.Errorf("expected no sessions free (laptop full, desktop retired), got %d: %s", report.Free, report)
	}
	if got := provider.TakeBegun(); len(got) != 1 {
		t.Errorf("expected one box begun for the 2 waiting stories, got %v: %s", got, report)
	}
}

// Without the host retired the same line does count, so the test above fails
// for the retired host and not for some other reason.
func TestAHostInTheCapsAndNotRetiredCountsItsFreeSessions(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 55, 0, 0, time.UTC)
	tracker := fullHostsWithAutoWaiting(t, now, map[string]int{"laptop": 2})
	plan := cloudPlan
	plan.HostCaps = map[string]int{"laptop": 2, "desktop": 6}
	provider := &apptest.FakeCloud{}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: provider, Book: &apptest.FakeCloudBook{}, Host: "laptop",
		Plan: plan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Free != 6 {
		t.Errorf("expected desktop's 6 sessions free, got %d: %s", report.Free, report)
	}
	if got := provider.TakeBegun(); len(got) != 0 {
		t.Errorf("expected no box begun while desktop has room, got %v", got)
	}
}

// A box whose make has not finished has taken no story, and mw status must not
// call it working (mw-gq6.1113): on 2026-11-12 four boxes 56 minutes into a
// snapshot restore, Vultr saying "active locked", were each "up 56m, working".
func TestStatusSaysABoxStillBeingMadeIsMakingAndNeverWorking(t *testing.T) {
	now := time.Date(2026, 11, 12, 16, 53, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-56 * time.Minute), Making: true}}}}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder",
		Cloud: book, CloudPlan: cloudPlan, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := report.String()
	if !strings.Contains(text, "cloud1  making 56m") {
		t.Errorf("expected %q in:\n%s", "cloud1  making 56m", text)
	}
	if strings.Contains(text, "working") || strings.Contains(text, "cloud1  up 56m") {
		t.Errorf("expected a box still being made not called up or working, got:\n%s", text)
	}
}

// aMakingBox is a book with cloud1 asked for 56 minutes ago and its make not
// done, Vultr holding the instance locked: what the Governor's boxes were on
// 2026-11-12.
func aMakingBox(now time.Time) (*apptest.FakeCloud, *apptest.FakeCloudBook) {
	provider := &apptest.FakeCloud{Orphans: []application.CloudInstance{{ID: "v1", Label: "cloud1", Status: "active", Server: "locked"}}}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-56 * time.Minute), Making: true}}}}
	return provider, book
}

func TestTheCheckKeepsWhatVultrSaysOfABoxBeingMadeAndStatusShowsIt(t *testing.T) {
	now := time.Date(2026, 11, 12, 16, 53, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	provider, book := aMakingBox(now)
	if _, err := (application.CloudCheck{Work: tracker, Provider: provider, Book: book, Host: "laptop",
		Plan: cloudPlan, Now: func() time.Time { return now }}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := book.State.Boxes[0].Vultr; got != "active locked" {
		t.Errorf("expected the book to keep Vultr's word %q, got %q", "active locked", got)
	}
	if slices.Contains(provider.Calls(), "down cloud1") || !book.State.Boxes[0].Making {
		t.Errorf("expected the box still being made, calls %v, book %+v", provider.Calls(), book.State.Boxes)
	}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder",
		Cloud: book, CloudPlan: cloudPlan, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := report.String()
	for _, want := range []string{"cloud1  making 56m, Vultr active locked", "1 of 2 boxes"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "working") {
		t.Errorf("expected no box called working:\n%s", text)
	}
}

func TestAMakeThatRunsPastTheLimitIsOneCloudEventAndADampersLineNamingTheBoxAndVultrsStatus(t *testing.T) {
	now := time.Date(2026, 11, 12, 16, 53, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	provider, book := aMakingBox(now)
	log := &apptest.FakeEventLog{}
	check := application.CloudCheck{Work: tracker, Provider: provider, Book: book, Host: "laptop", Log: log,
		Plan: cloudPlan, Now: func() time.Time { return now }}
	for range 2 {
		if _, err := check.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	slow := 0
	for _, e := range cloudEventsOf(log) {
		if strings.Contains(e.Detail, "cloud1: the make begun at 15:57Z has not finished after 56m") && strings.Contains(e.Detail, "Vultr says active locked") {
			slow++
		}
	}
	if slow != 1 {
		t.Errorf("expected one cloud event for the slow make, got %d in %+v", slow, cloudEventsOf(log))
	}
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book, CloudPlan: cloudPlan}
	damp, err := survey.Dampers(context.Background(), application.WorkInHand{}, now)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, a := range damp.Alarms {
		lines = append(lines, a.Line())
	}
	if text := strings.Join(lines, "\n"); !strings.Contains(text, "cloud1") || !strings.Contains(text, "Vultr says active locked") {
		t.Errorf("expected a DAMPERS line naming cloud1 and Vultr's status, got %q", text)
	}
}

func TestAMakeUnderTheLimitIsNeitherACloudEventNorADampersLine(t *testing.T) {
	now := time.Date(2026, 11, 12, 16, 10, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	provider, book := aMakingBox(now)
	book.State.Boxes[0].Up = now.Add(-10 * time.Minute)
	log := &apptest.FakeEventLog{}
	if _, err := (application.CloudCheck{Work: tracker, Provider: provider, Book: book, Host: "laptop", Log: log,
		Plan: cloudPlan, Now: func() time.Time { return now }}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(cloudEventsOf(log)); n != 0 {
		t.Errorf("expected no cloud event for a make 10 minutes old, got %+v", cloudEventsOf(log))
	}
	damp, err := application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book, CloudPlan: cloudPlan}.Dampers(context.Background(), application.WorkInHand{}, now)
	for _, a := range damp.Alarms {
		if a.Principle == application.DamperCloudMake {
			t.Errorf("expected no cloud make alarm, got %+v (%v)", a, err)
		}
	}
}

// A box still being made counts no session free, and yet no second box is made
// for the story the first already serves.
func TestABoxStillBeingMadeCountsNoSessionFreeAndNoSecondBoxIsMade(t *testing.T) {
	now := time.Date(2026, 11, 12, 16, 10, 0, 0, time.UTC)
	tracker := fullHostsWithAutoWaiting(t, now, map[string]int{"laptop": 2})
	plan := cloudPlan
	plan.HostCaps = map[string]int{"laptop": 2}
	provider, book := aMakingBox(now)
	report, err := application.CloudCheck{Work: tracker, Stories: tracker, Provider: provider, Book: book, Host: "laptop",
		Plan: plan, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Free != 0 || report.Coming != 2 {
		t.Errorf("expected no session free and 2 coming, got free %d coming %d: %s", report.Free, report.Coming, report)
	}
	if got := provider.TakeBegun(); len(got) != 0 {
		t.Errorf("expected no second box begun while the first is made, got %v", got)
	}
}

func cloudEventsOf(log *apptest.FakeEventLog) []events.Event {
	got, _ := log.Since(context.Background(), 0)
	return got
}

// mw-gq6.1127: the quiet alarm cried "cloud1 last synced 25 min ago" for the
// old cloud1, destroyed, while a new cloud1 was being made under the name.

func quietAlarmOf(t *testing.T, book application.CloudBook) []application.NudgeClause {
	t.Helper()
	tracker := aHostSyncedLongAgoHoldingAClaim(t)
	syncedAt(t, tracker, "cloud2", 25*time.Minute)
	return nudgeReport(t, tracker, application.Nudge{
		Down:      application.CloudBoxesDown{Book: book, Plan: cloudPlan},
		CloudBook: book, CloudPlan: cloudPlan,
	})
}

func TestTheQuietAlarmIsNotRaisedForABoxTheCloudBookMarksMaking(t *testing.T) {
	made := statusNow.Add(-5 * time.Minute)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud2", Up: made, Making: true, Vultr: "active locked"}}}}
	for _, c := range quietAlarmOf(t, book) {
		if c.Key == "host:cloud2" {
			t.Errorf("expected no quiet alarm for a box being made, got %+v", c)
		}
	}
}

func TestTheQuietAlarmIsNotRaisedForABoxTheCloudBookDoesNotHold(t *testing.T) {
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11"}}
	for _, c := range quietAlarmOf(t, book) {
		if c.Key == "host:cloud2" {
			t.Errorf("expected no quiet alarm for a box that is down, got %+v", c)
		}
	}
}

func TestTheQuietAlarmIsStillRaisedForABoxUpAndSilent(t *testing.T) {
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud2", Up: statusNow.Add(-5 * time.Hour), Checked: statusNow.Add(-4 * time.Hour)}}}}
	found := false
	for _, c := range quietAlarmOf(t, book) {
		found = found || c.Key == "host:cloud2"
	}
	if !found {
		t.Errorf("expected the quiet alarm for a box that is up and silent")
	}
}

func TestAMakingBoxPast15MinutesHasItsOwnAlarmNamingVultrsStatusAndTheStep(t *testing.T) {
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud2", Up: statusNow.Add(-16 * time.Minute), Making: true, Vultr: "active locked"}}}}
	clauses := quietAlarmOf(t, book)
	var got string
	for _, c := range clauses {
		if c.Key == "cloud:cloud2" {
			got = c.Text
		}
	}
	for _, want := range []string{"cloud2", "16m", "Vultr says active locked", "mw cloud down cloud2"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected the making alarm to say %q, got %q in %+v", want, got, clauses)
		}
	}
	// And it is the DAMPERS line too.
	book.State.Boxes[0].Up = statusNow.Add(-14 * time.Minute)
	for _, c := range quietAlarmOf(t, book) {
		if c.Key == "cloud:cloud2" {
			t.Errorf("expected no making alarm at 14 minutes, got %+v", c)
		}
	}
}

func TestTheDampersSaysAMakeOf16MinutesAndTheStepToEndIt(t *testing.T) {
	now := time.Date(2026, 11, 12, 19, 10, 0, 0, time.UTC)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-16 * time.Minute), Making: true, Vultr: "active locked"}}}}
	tracker := apptest.NewFakeTracker()
	damp, err := application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book, CloudPlan: cloudPlan}.Dampers(context.Background(), application.WorkInHand{}, now)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, a := range damp.Alarms {
		lines = append(lines, a.Line())
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "Vultr says active locked") || !strings.Contains(text, "mw cloud down cloud1") {
		t.Errorf("expected a DAMPERS line with Vultr's status and the step, got %q", text)
	}
}

// A box destroyed is not made again under its name for 24 hours: its notes
// (last sync, ticks, mw commit) would otherwise be read as the new box's.
func TestANewBoxNeverTakesTheNameOfABoxDownedInTheLast24Hours(t *testing.T) {
	now := time.Date(2026, 11, 12, 18, 50, 0, 0, time.UTC)
	tracker := fullHostsWithAutoWaiting(t, now, map[string]int{"laptop": 2})
	plan := cloudPlan
	plan.HostCaps = map[string]int{"laptop": 2}
	provider := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}}}
	at := now
	check := application.CloudCheck{Work: tracker, Stories: tracker, Provider: provider, Book: book, Host: "laptop",
		Plan: plan, Now: func() time.Time { return at }}
	if _, err := check.Down(context.Background(), "cloud1"); err != nil {
		t.Fatal(err)
	}
	at = now.Add(2 * time.Minute)
	if _, err := check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := provider.TakeBegun(); len(got) != 1 || got[0] != "cloud2" {
		t.Fatalf("expected cloud2 begun, the name cloud1 being a box downed 2 minutes ago, got %v", got)
	}
	// 24 hours on, the name is free again.
	book.State.Boxes = nil
	at = now.Add(24*time.Hour + time.Minute)
	if _, err := check.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := provider.TakeBegun(); len(got) != 1 || got[0] != "cloud1" {
		t.Errorf("expected cloud1 made again after 24 hours, got %v", got)
	}
}

// A box Vultr reports locked past 30 minutes, making or down, is named with
// its instance id and the step: open a Vultr ticket.
func TestABoxVultrHoldsLockedPast30MinutesIsNamedWithItsInstanceIdAndTheStep(t *testing.T) {
	for name, box := range map[string]application.CloudBox{
		"making": {Name: "cloud1", Making: true},
		"down":   {Name: "cloud1", Refused: ": Vultr would not delete (locked)"},
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC)
			box.Up = now.Add(-5 * time.Minute)
			provider := &apptest.FakeCloud{Orphans: []application.CloudInstance{{ID: "vultr-9f3", Label: "cloud1", Status: "active", Server: "locked"}}}
			book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: []application.CloudBox{box}}}
			tracker := apptest.NewFakeTracker()
			at := now
			// An idle box is destroyed after the plan's Idle; this one is kept to be read.
			plan := cloudPlan
			plan.Idle = 10 * time.Hour
			check := application.CloudCheck{Work: tracker, Provider: provider, Book: book, Host: "laptop",
				Plan: plan, Now: func() time.Time { return at }}
			survey := application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book, CloudPlan: cloudPlan}
			alarms := func() string {
				damp, err := survey.Dampers(context.Background(), application.WorkInHand{}, at)
				if err != nil {
					t.Fatal(err)
				}
				var lines []string
				for _, a := range damp.Alarms {
					if a.Principle == application.DamperCloudMake {
						lines = append(lines, a.Line())
					}
				}
				return strings.Join(lines, "\n")
			}
			if _, err := check.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if text := alarms(); strings.Contains(text, "vultr-9f3") {
				t.Errorf("expected no ticket line the minute the lock is first seen:\n%s", text)
			}
			at = now.Add(31 * time.Minute)
			if _, err := check.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			text := alarms()
			for _, want := range []string{"cloud1", "locked", "vultr-9f3", "open a Vultr ticket"} {
				if !strings.Contains(text, want) {
					t.Errorf("expected the alarm to say %q, got:\n%s", want, text)
				}
			}
		})
	}
}

// A box stuck making is not a box to make a second beside.
func TestNoNewBoxIsMadeWhileOneIsStuckMaking(t *testing.T) {
	now := time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC)
	tracker := fullHostsWithAutoWaiting(t, now, map[string]int{"laptop": 2})
	plan := cloudPlan
	plan.HostCaps = map[string]int{"laptop": 2}
	provider, book := aMakingBox(now)
	book.State.Boxes[0].Cap = 1
	check := func(up time.Duration) []string {
		book.State.Boxes[0].Up = now.Add(-up)
		if _, err := (application.CloudCheck{Work: tracker, Stories: tracker, Provider: provider, Book: book, Host: "laptop",
			Plan: plan, Now: func() time.Time { return now }}).Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		return provider.TakeBegun()
	}
	if got := check(56 * time.Minute); len(got) != 0 {
		t.Errorf("expected no box begun beside one stuck making for 56 minutes, got %v", got)
	}
	book.State.Boxes = book.State.Boxes[:1]
	if got := check(5 * time.Minute); len(got) != 1 {
		t.Errorf("expected a second box begun beside one made for 5 minutes (the demand outruns its cap), got %v", got)
	}
}

// feeLimitedCheck runs the check on full hosts with stories waiting, the book
// holding Vultr's fee-limit refusal at feeAt and the given downs, and reports
// the boxes it began (mw-gq6.1163).
func feeLimitedCheck(t *testing.T, now, feeAt time.Time, downed map[string]time.Time) (begun []string, report application.CloudReport) {
	t.Helper()
	tracker := fullHostsWithAutoWaiting(t, now, map[string]int{"laptop": 2})
	provider := &apptest.FakeCloud{}
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", FeeLimitAt: feeAt, Downed: downed}}
	report, err := application.CloudCheck{
		Work: tracker, Stories: tracker, Provider: provider, Book: book, Host: "laptop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return provider.TakeBegun(), report
}

func TestNoBoxIsTriedForAnHourAfterVultrsFeeLimitRefusal(t *testing.T) {
	now := time.Date(2026, 11, 12, 23, 42, 0, 0, time.UTC)
	for _, ago := range []time.Duration{time.Minute, 11 * time.Minute, 22 * time.Minute, 59 * time.Minute} {
		if begun, report := feeLimitedCheck(t, now, now.Add(-ago), nil); len(begun) != 0 {
			t.Errorf("fee limit %s ago: expected no box tried, got %v: %s", ago, begun, report)
		}
	}
}

func TestABoxIsTriedAgainAnHourAfterVultrsFeeLimitRefusal(t *testing.T) {
	now := time.Date(2026, 11, 12, 23, 42, 0, 0, time.UTC)
	if begun, report := feeLimitedCheck(t, now, now.Add(-time.Hour), nil); len(begun) != 1 {
		t.Errorf("expected one box tried an hour after the refusal, got %v: %s", begun, report)
	}
}

func TestABoxDownedAfterTheFeeLimitRefusalEndsTheWaitSinceItFreesRoom(t *testing.T) {
	now := time.Date(2026, 11, 12, 23, 42, 0, 0, time.UTC)
	feeAt := now.Add(-30 * time.Minute)
	if begun, report := feeLimitedCheck(t, now, feeAt, map[string]time.Time{"cloud2": now.Add(-5 * time.Minute)}); len(begun) != 1 {
		t.Errorf("expected one box tried after a box was downed, got %v: %s", begun, report)
	}
	// A box downed before the refusal freed nothing that the refusal had not seen.
	if begun, report := feeLimitedCheck(t, now, feeAt, map[string]time.Time{"cloud2": now.Add(-2 * time.Hour)}); len(begun) != 0 {
		t.Errorf("expected no box tried for a down before the refusal, got %v: %s", begun, report)
	}
}

func TestStatusNamesTheFeeLimitTheWayToRaiseItAndWhenABoxIsTriedAgain(t *testing.T) {
	now := time.Date(2026, 11, 12, 23, 42, 0, 0, time.UTC)
	feeAt := time.Date(2026, 11, 12, 23, 20, 0, 0, time.UTC)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", FeeLimitAt: feeAt}}
	report, err := application.Status{Tracker: apptest.NewFakeTracker(), Notes: apptest.NewFakeTracker(), Host: "desktop", Seat: "builder",
		Cloud: book, CloudPlan: cloudPlan, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Vultr fee limit reached since 11-12 23:20Z", "my.vultr.com", "no box is tried before 00:20Z"} {
		if !strings.Contains(report.String(), want) {
			t.Errorf("expected status to say %q, got:\n%s", want, report.String())
		}
	}
}

package application_test

import (
	"context"
	"errors"
	"slices"
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
	for range 3 {
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

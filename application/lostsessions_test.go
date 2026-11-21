package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-6ww.185.10: a Boost that answers again ends the sessions of the stories
// the home gave away while it was down, and leaves the worktrees to the sweep.

func lostSessionsWorld(t *testing.T) (*apptest.FakeTracker, *fakeHostSessions) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-ls", domain.Path{Rig: "millwright", Branch: "main", Host: "auto"})
	for _, id := range []string{"mw-ls.1", "mw-ls.2"} {
		tracker.AddStory("mw-ls", domain.Story{ID: id, Title: "story " + id})
	}
	return tracker, &fakeHostSessions{live: []application.HostSession{
		{Name: application.SessionName("mw-ls.1")},
		{Name: application.SessionName("mw-ls.2")},
		{Name: "whisper-server"},
	}}
}

func TestAReturningHostEndsItsSessionOfAStoryNowHeldElsewhereAndLeavesOneStillItsOwn(t *testing.T) {
	tracker, sessions := lostSessionsWorld(t)
	until := time.Now().Add(time.Hour)
	if err := tracker.ClaimAs("mw-ls.1", "mw@vps", until); err != nil {
		t.Fatal(err)
	}
	if err := tracker.ClaimAs("mw-ls.2", "mw@desktop", until); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	report, err := application.EndLostSessions{Tracker: tracker, Sessions: sessions, Host: "desktop", Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sessions.closed, ","); got != application.SessionName("mw-ls.1") {
		t.Errorf("expected only the session of mw-ls.1 closed, got %q", got)
	}
	if want := "ended: mw-ls.1 is now held by vps"; !strings.Contains(out.String(), want) {
		t.Errorf("expected %q printed, got %q", want, out.String())
	}
	if strings.Contains(out.String(), "mw-ls.2") || len(report.Ended) != 1 || report.Ended[0].StoryID != "mw-ls.1" {
		t.Errorf("expected only mw-ls.1 ended, got %+v and %q", report, out.String())
	}
}

func TestAReturningHostLeavesASessionOfAStoryNoOneHoldsOrThatIsClosed(t *testing.T) {
	tracker, sessions := lostSessionsWorld(t)
	if err := tracker.CloseStory(context.Background(), "mw-ls.2", "landed"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if _, err := (application.EndLostSessions{Tracker: tracker, Sessions: sessions, Host: "desktop", Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sessions.closed) != 0 || out.Len() != 0 {
		t.Errorf("expected nothing ended, got %v and %q", sessions.closed, out.String())
	}
}

func TestAReturningHostThatCannotListItsSessionsEndsNothingAndSaysSo(t *testing.T) {
	tracker, sessions := lostSessionsWorld(t)
	sessions.listErr = errors.New("no server running")

	report, err := application.EndLostSessions{Tracker: tracker, Sessions: sessions, Host: "desktop"}.Run(context.Background())
	if err != nil || len(report.Ended) != 0 || len(report.Notes) != 1 {
		t.Errorf("expected a note and nothing ended, got %+v, %v", report, err)
	}
}

func TestAReturningHostThatCannotCloseASessionSaysSoAndGoesOn(t *testing.T) {
	tracker, sessions := lostSessionsWorld(t)
	until := time.Now().Add(time.Hour)
	for _, id := range []string{"mw-ls.1", "mw-ls.2"} {
		if err := tracker.ClaimAs(id, "mw@vps", until); err != nil {
			t.Fatal(err)
		}
	}
	sessions.closeFail = map[string]error{application.SessionName("mw-ls.1"): errors.New("tmux died")}

	report, err := application.EndLostSessions{Tracker: tracker, Sessions: sessions, Host: "desktop"}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Ended) != 1 || report.Ended[0].StoryID != "mw-ls.2" || len(report.Notes) != 1 {
		t.Errorf("expected mw-ls.2 ended and a note for mw-ls.1, got %+v", report)
	}
}

func TestADryRunEndsNothing(t *testing.T) {
	tracker, sessions := lostSessionsWorld(t)
	if err := tracker.ClaimAs("mw-ls.1", "mw@vps", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	report, err := application.EndLostSessions{Tracker: tracker, Sessions: sessions, Host: "desktop", DryRun: true}.Run(context.Background())
	if err != nil || len(sessions.closed) != 0 || len(report.Ended) != 1 {
		t.Errorf("expected the session named and not closed, got %+v, %v, closed %v", report, err, sessions.closed)
	}
}

package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// fakeHostSessions is the host's tmux sessions as a sweep sees them.
type fakeHostSessions struct {
	live      []application.HostSession
	listErr   error
	closed    []string
	closeFail map[string]error
}

func (f *fakeHostSessions) Sessions(context.Context) ([]application.HostSession, error) {
	return f.live, f.listErr
}

func (f *fakeHostSessions) Close(_ context.Context, name string) error {
	if err := f.closeFail[name]; err != nil {
		return err
	}
	f.closed = append(f.closed, name)
	return nil
}

func sweepSessionsFor(t *testing.T, sessions *fakeHostSessions, stories map[string]string) (application.SweepReport, string) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-swp", domain.Path{Rig: "millwright", Branch: "main", Host: "vps"})
	for id, status := range stories {
		tracker.AddStory("mw-swp", domain.Story{ID: id, Title: "story " + id})
		switch status {
		case "closed":
			if err := tracker.CloseStory(context.Background(), id, "landed"); err != nil {
				t.Fatal(err)
			}
		case "running":
			if err := tracker.ClaimStory(context.Background(), id); err != nil {
				t.Fatal(err)
			}
		}
	}
	var out bytes.Buffer
	report, err := application.Sweep{Tracker: tracker, Host: "vps", Sessions: sessions, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	return report, out.String()
}

func TestASweepClosesTheIdleSessionOfAClosedStory(t *testing.T) {
	sessions := &fakeHostSessions{live: []application.HostSession{{Name: "mw-swp_1", Idle: true}}}
	report, out := sweepSessionsFor(t, sessions, map[string]string{"mw-swp.1": "closed"})
	if len(sessions.closed) != 1 || sessions.closed[0] != "mw-swp_1" {
		t.Fatalf("closed %v, want mw-swp_1", sessions.closed)
	}
	if len(report.SessionsClosed) != 1 || !strings.Contains(out, "mw-swp_1") {
		t.Errorf("the report should name it: %+v\n%s", report.SessionsClosed, out)
	}
}

func TestASweepClosesTheSentBackSessionOfAClosedStory(t *testing.T) {
	sessions := &fakeHostSessions{live: []application.HostSession{{Name: "mw-swp_1-sent-back", Idle: true}}}
	sweepSessionsFor(t, sessions, map[string]string{"mw-swp.1": "closed"})
	if len(sessions.closed) != 1 {
		t.Fatalf("closed %v, want the sent-back session", sessions.closed)
	}
}

func TestASweepKeepsTheSessionOfAnOpenOrARunningStory(t *testing.T) {
	for _, status := range []string{"open", "running"} {
		t.Run(status, func(t *testing.T) {
			sessions := &fakeHostSessions{live: []application.HostSession{{Name: "mw-swp_2", Idle: true}}}
			sweepSessionsFor(t, sessions, map[string]string{"mw-swp.2": status})
			if len(sessions.closed) != 0 {
				t.Errorf("closed %v of a %s story", sessions.closed, status)
			}
		})
	}
}

func TestASweepKeepsTheSessionOfAClosedStoryWhosePaneIsAtWork(t *testing.T) {
	sessions := &fakeHostSessions{live: []application.HostSession{{Name: "mw-swp_3", Idle: false}}}
	report, out := sweepSessionsFor(t, sessions, map[string]string{"mw-swp.3": "closed"})
	if len(sessions.closed) != 0 {
		t.Fatalf("closed %v, want the busy pane kept", sessions.closed)
	}
	if len(report.SessionsKept) != 1 || !strings.Contains(out, "mw-swp_3") {
		t.Errorf("the report should say it kept it: %+v\n%s", report.SessionsKept, out)
	}
}

func TestASweepNeverTouchesASessionThatIsNoStorys(t *testing.T) {
	sessions := &fakeHostSessions{live: []application.HostSession{
		{Name: "mw-seats", Idle: true},
		{Name: "whisper-server", Idle: true},
		{Name: "mw-tester_1", Idle: true},
		{Name: "mw-swp_9", Idle: true}, // a story the tracker does not know
		{Name: "mw-swp", Idle: true},   // an epic's name
	}}
	sweepSessionsFor(t, sessions, map[string]string{"mw-swp.1": "closed"})
	if len(sessions.closed) != 0 {
		t.Errorf("closed %v, want nothing", sessions.closed)
	}
}

func TestASweepOfAHostWithNoSessionsSaysSoAndSucceeds(t *testing.T) {
	report, out := sweepSessionsFor(t, &fakeHostSessions{}, nil)
	if report.SessionsSeen != 0 || !strings.Contains(out, "sessions: none") {
		t.Errorf("want a line saying there are no sessions, got\n%s", out)
	}
}

func TestASweepNotesASessionItCouldNotListOrClose(t *testing.T) {
	report, _ := sweepSessionsFor(t, &fakeHostSessions{listErr: errors.New("tmux broke")}, nil)
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "tmux broke") {
		t.Errorf("want the listing failure noted, got %v", report.Notes)
	}

	sessions := &fakeHostSessions{
		live:      []application.HostSession{{Name: "mw-swp_1", Idle: true}, {Name: "mw-swp_2", Idle: true}},
		closeFail: map[string]error{"mw-swp_1": errors.New("busy")},
	}
	report, _ = sweepSessionsFor(t, sessions, map[string]string{"mw-swp.1": "closed", "mw-swp.2": "closed"})
	if len(report.Notes) != 1 || len(sessions.closed) != 1 || sessions.closed[0] != "mw-swp_2" {
		t.Errorf("one failure should not stop the rest: notes %v closed %v", report.Notes, sessions.closed)
	}
}

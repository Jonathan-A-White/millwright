package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.886: a pause or a resume on a loaded host, where tmux is too slow to say
// whether a session is running, does not read that as the session being gone.

func aPausedHostWithASlowTmux(t *testing.T, running bool) (application.FleetHostPause, *slowTmuxRunner) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-e", domain.Path{Rig: "rig", Branch: "main", Host: "laptop"})
	tracker.AddStory("mw-e", domain.Story{ID: "mw-e.1", Title: "A story"})
	if err := tracker.ClaimAs("mw-e.1", "mw@laptop", time.Now().Add(apptest.LeaseTTL)); err != nil {
		t.Fatal(err)
	}
	runner := &slowTmuxRunner{FakeRunner: apptest.NewFakeRunner(), slow: true}
	if running {
		mustDo(t, runner.FakeRunner.Start(context.Background(), application.SessionSpec{Name: application.SessionName("mw-e.1"), Command: []string{"claude"}}))
	}
	return application.FleetHostPause{
		Host: "laptop", Notes: tracker, Stories: tracker, Runner: runner, Git: apptest.NewFakePauseGit(),
		Rigs: map[string]string{"rig": t.TempDir()},
	}, runner
}

func TestAPauseTypesTheLineToASessionTmuxIsTooSlowToAnswerAbout(t *testing.T) {
	host, runner := aPausedHostWithASlowTmux(t, true)

	paused, err := host.Pause(context.Background(), application.HostPauseOrder{Reason: "limit"})
	if err != nil {
		t.Fatal(err)
	}
	if typed := runner.Input(application.SessionName("mw-e.1")); len(typed) != 1 || !strings.HasPrefix(typed[0], "PAUSE") {
		t.Errorf("typed %q, want the PAUSE line", typed)
	}
	if len(paused.Stories) != 1 || !paused.Stories[0].Alive || !strings.Contains(paused.Stories[0].Note, "could not be asked about") {
		t.Errorf("expected the story recorded alive with the note that tmux was not asked, got %+v", paused.Stories)
	}
}

func TestAPauseRecordsASessionItCannotAskAboutOrTypeToAsNotKnown(t *testing.T) {
	host, _ := aPausedHostWithASlowTmux(t, false)

	paused, err := host.Pause(context.Background(), application.HostPauseOrder{Reason: "limit"})
	if err != nil {
		t.Fatal(err)
	}
	if len(paused.Stories) != 1 {
		t.Fatalf("expected the story recorded, got %+v", paused.Stories)
	}
	if note := paused.Stories[0].Note; !strings.Contains(note, "could not be asked about") || !strings.Contains(note, "not known whether it was running") {
		t.Errorf("expected the story recorded as one whose session is not known, got note %q", note)
	}
}

func TestAResumeTypesTheLineToASessionTmuxIsTooSlowToAnswerAbout(t *testing.T) {
	host, runner := aPausedHostWithASlowTmux(t, true)

	resumed, err := host.Resume(context.Background(), application.HostResumeOrder{Stories: []application.PausedStory{
		{Story: "mw-e.1", Host: "laptop", Window: application.SessionName("mw-e.1")}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Gone) != 0 || len(resumed.Told) != 1 {
		t.Errorf("expected the story told RESUME and not Gone, got told %v gone %v", resumed.Told, resumed.Gone)
	}
	if typed := runner.Input(application.SessionName("mw-e.1")); len(typed) != 1 || !strings.HasPrefix(typed[0], "RESUME") {
		t.Errorf("typed %q, want the RESUME line", typed)
	}
}

func TestAResumeNeverListsAsGoneASessionItCannotAskAboutOrTypeTo(t *testing.T) {
	host, _ := aPausedHostWithASlowTmux(t, false)

	resumed, err := host.Resume(context.Background(), application.HostResumeOrder{Stories: []application.PausedStory{
		{Story: "mw-e.1", Host: "laptop", Window: application.SessionName("mw-e.1")}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Gone) != 0 || len(resumed.Told) != 0 {
		t.Errorf("expected the story neither told nor Gone, got told %v gone %v", resumed.Told, resumed.Gone)
	}
	if len(resumed.Notes) != 1 || !strings.Contains(resumed.Notes[0], "could not be asked about") || !strings.Contains(resumed.Notes[0], "mw-e.1") {
		t.Errorf("expected a note naming the story and that its session could not be asked about, got %q", resumed.Notes)
	}
}

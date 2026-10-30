package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// resumeOfDownedBox resumes a pause whose cloud1 box was brought down, holding
// the given stories, whose retry cannot ask cloud1 anything.
func resumeOfDownedBox(t *testing.T, stories ...application.PausedStory) (*apptest.FakePauseRetry, string, error) {
	t.Helper()
	book := apptest.NewFakePauseBook()
	const dir = "plans/pause-2026-10-30"
	if err := book.Write(context.Background(), dir, application.PausedSet{
		PausedAt: time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC),
		Hosts:    []application.PausedSetHost{{Host: "cloud1", Cloud: true}},
		Boxes:    []string{"cloud1"},
		Stories:  stories,
	}); err != nil {
		t.Fatal(err)
	}
	if err := book.SetCurrent(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	retry := &apptest.FakePauseRetry{Unaskable: map[string]bool{}}
	for _, ps := range stories {
		retry.Unaskable[ps.Story] = true
	}
	var out bytes.Buffer
	err := application.Resume{
		Fleet: apptest.NewFakePauseFleet(), Book: book, Retry: retry, Host: "laptop", Out: &out,
	}.Run(context.Background(), false)
	return retry, out.String(), err
}

func TestResumeGivesBackAPushedStoryOfADownedBoxWithoutBeingAskedToForce(t *testing.T) {
	retry, out, err := resumeOfDownedBox(t,
		application.PausedStory{Story: "mw-aaa.1", Host: "cloud1", Branch: "mw/mw-aaa.1", SHA: "abc1234", Pushed: true, Ended: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(retry.Retried) != 1 || retry.Retried[0] != "mw-aaa.1" {
		t.Fatalf("retried %v, want mw-aaa.1 retried; output:\n%s", retry.Retried, out)
	}
	if !strings.Contains(out, "branch pushed at the pause, claim given back") {
		t.Fatalf("the output does not say the branch was pushed at the pause:\n%s", out)
	}
}

func TestResumeStillNeedsForceForAStoryNotRecordedAsPushed(t *testing.T) {
	retry, out, err := resumeOfDownedBox(t,
		application.PausedStory{Story: "mw-bbb.2", Host: "cloud1", Branch: "mw/mw-bbb.2", Pushed: false, Ended: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(retry.Retried) != 0 {
		t.Fatalf("retried %v, want none: the branch was not pushed; output:\n%s", retry.Retried, out)
	}
	if !strings.Contains(out, "add --force") || strings.Contains(out, "branch pushed at the pause") {
		t.Fatalf("the output should show the refusal, and not claim a push:\n%s", out)
	}
}

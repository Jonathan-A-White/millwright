package application_test

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aTagPublishingLanding is a landing whose range holds the tags it is told
// to publish, and that remembers what it was asked.
type aTagPublishingLanding struct {
	aMovedTargetLanding
	answer application.PublishedTags
	base   string
	named  []string
}

func (l *aTagPublishingLanding) PublishTags(_ context.Context, _, _, base string, named []string) (application.PublishedTags, error) {
	l.base, l.named = base, named
	return l.answer, nil
}

func aLandingWithTags(t *testing.T, acceptance string, answer application.PublishedTags) (application.NextReport, *aTagPublishingLanding, *apptest.FakeMailbox) {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "A story"})
	if err := tracker.SetAcceptance("mw-x.1", acceptance); err != nil {
		t.Fatal(err)
	}
	vault := newFakeVault()
	vault.written["mw-x.1/"+application.ResultFileNameForAttempt(1)] = `{"subtype":"success"}`
	lands := &aTagPublishingLanding{aMovedTargetLanding: aMovedTargetLanding{fakeRetryLanding{AheadCount: 1, BehindCount: 1}}, answer: answer}
	mailbox := apptest.NewFakeMailbox()
	report, err := application.Next{
		Tracker: tracker, Vault: vault, Worktrees: lands, Landing: lands,
		Checks: &scriptedChecks{passes: []bool{true, true}}, Slot: aSlot{}, Mailbox: mailbox,
		Seat: "builder", Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": "/rigs/millwright"},
		Out:  io.Discard, Err: io.Discard,
	}.Run(context.Background(), "mw-x.1")
	if err != nil || !report.Landed {
		t.Fatalf("expected the story landed, got err %v and %+v", err, report)
	}
	return report, lands, mailbox
}

func TestACLandingNamesTheTagsItPushed(t *testing.T) {
	report, lands, _ := aLandingWithTags(t, "1. v0.3.0 is tagged.", application.PublishedTags{Pushed: []string{"v0.3.0"}})
	if lands.base != "fedcba9876543210" || !reflect.DeepEqual(lands.named, []string{"v0.3.0"}) {
		t.Errorf("expected the tags asked for from the commit the merge began at, naming v0.3.0, got %q %v", lands.base, lands.named)
	}
	if !strings.Contains(report.String(), "tag     v0.3.0 pushed to origin") {
		t.Errorf("expected the report to name v0.3.0, got:\n%s", report)
	}
}

func TestALandingSaysATagTheOriginHasElsewhereWasNotMoved(t *testing.T) {
	report, _, _ := aLandingWithTags(t, "", application.PublishedTags{Elsewhere: []string{"v0.2.0"}})
	if !strings.Contains(report.String(), "v0.2.0 on origin points elsewhere") {
		t.Errorf("expected the report to say v0.2.0 points elsewhere, got:\n%s", report)
	}
}

func TestALandedMailWarnsOfAnACTagNotOnOrigin(t *testing.T) {
	_, lands, mailbox := aLandingWithTags(t, "1. The release v1.2.3 is tagged and v1.2.3 installs.", application.PublishedTags{Missing: []string{"v1.2.3"}})
	if !reflect.DeepEqual(lands.named, []string{"v1.2.3"}) {
		t.Errorf("expected v1.2.3 asked about once, got %v", lands.named)
	}
	inbox, err := mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for _, m := range inbox {
		body += m.Body
	}
	if !strings.Contains(body, "WARN: tag v1.2.3 named in the AC is not on origin") {
		t.Errorf("expected the Landed mail to warn, got:\n%s", body)
	}
}

func TestACVersionTagsAreTheVersionsTheCriteriaName(t *testing.T) {
	got := application.ACVersionTags("1. tag v0.2.0 and v1.0.0.\n2. v0.2.0 again; version 3.4.5, v1.2 and xv9.9.9 are not tags")
	if want := []string{"v0.2.0", "v1.0.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

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

// mw-gq6.1129: a hold through mw bd refuses a story a Builder has claimed,
// unless --over-claim says why.

func claimedBd(id string) *apptest.FakeBd {
	bd := apptest.NewFakeBd(id)
	bd.Claim(id, "mw@cloud2", time.Now().Add(time.Hour))
	return bd
}

func TestAHoldOfAClaimedStoryIsRefusedNamingTheClaimerAndTheFlag(t *testing.T) {
	for _, status := range []string{"deferred", "blocked"} {
		t.Run(status, func(t *testing.T) {
			bd := claimedBd("mw-x.1")
			run := runBdRetry(t, bd, "-C", "/vault", "--actor", "root", "update", "mw-x.1", "--status", status)
			if run.exit == 0 {
				t.Fatalf("a hold of a claimed story exited 0: %q", run.errs.String())
			}
			for _, said := range []string{"mw-x.1", "mw@cloud2", "--over-claim"} {
				if !strings.Contains(run.errs.String(), said) {
					t.Errorf("the refusal should say %q, it said %q", said, run.errs.String())
				}
			}
			if got := bd.Status("mw-x.1"); got != "in_progress" {
				t.Errorf("the story is %s, want it unchanged", got)
			}
			for _, call := range bd.Calls() {
				if strings.Contains(strings.Join(call, " "), "update") {
					t.Errorf("bd update was run: %v", call)
				}
			}
		})
	}
}

func TestAHoldWithEqualsStatusAndShortFlagIsRefusedToo(t *testing.T) {
	for _, args := range [][]string{
		{"update", "mw-x.1", "--status=deferred"},
		{"update", "-s", "blocked", "mw-x.1"},
		{"update", "--title", "t", "mw-x.1", "--status", "deferred"},
	} {
		bd := claimedBd("mw-x.1")
		if run := runBdRetry(t, bd, args...); run.exit == 0 {
			t.Errorf("%v exited 0", args)
		}
	}
}

func TestAHoldWithOverClaimHoldsAndWritesTheWhyOnTheBead(t *testing.T) {
	bd := claimedBd("mw-x.1")
	run := runBdRetry(t, bd, "update", "mw-x.1", "--status", "deferred", "--over-claim", "amending the criteria")
	if run.exit != 0 {
		t.Fatalf("exit %d: %q", run.exit, run.errs.String())
	}
	if got := bd.Status("mw-x.1"); got != "deferred" {
		t.Errorf("the story is %s, want deferred", got)
	}
	comments := bd.Comments("mw-x.1")
	if len(comments) != 1 || !strings.Contains(comments[0], "amending the criteria") || !strings.Contains(comments[0], "mw@cloud2") {
		t.Errorf("want one comment with the why and the claimer, got %q", comments)
	}
	for _, call := range bd.Calls() {
		if strings.Contains(strings.Join(call, " "), "--over-claim") {
			t.Errorf("bd was given mw bd's own flag: %v", call)
		}
	}
}

func TestOverClaimWithEqualsFormAndWithNoReason(t *testing.T) {
	bd := claimedBd("mw-x.1")
	if run := runBdRetry(t, bd, "update", "mw-x.1", "--over-claim=why not", "--status=deferred"); run.exit != 0 || bd.Status("mw-x.1") != "deferred" {
		t.Errorf("exit %d, status %s: %q", run.exit, bd.Status("mw-x.1"), run.errs.String())
	}
	bd = claimedBd("mw-x.1")
	if run := runBdRetry(t, bd, "update", "mw-x.1", "--status", "deferred", "--over-claim", "  "); run.exit == 0 || bd.Status("mw-x.1") != "in_progress" {
		t.Errorf("a blank reason should be refused, exit %d, status %s", run.exit, bd.Status("mw-x.1"))
	}
}

func TestAHoldOfAnUnclaimedStoryAndEveryOtherCallBehaveAsBefore(t *testing.T) {
	bd := apptest.NewFakeBd("mw-x.1", "mw-x.2")
	if run := runBdRetry(t, bd, "update", "mw-x.1", "--status", "deferred"); run.exit != 0 || bd.Status("mw-x.1") != "deferred" {
		t.Errorf("hold of an open story: exit %d, status %s, %q", run.exit, bd.Status("mw-x.1"), run.errs.String())
	}
	if got := len(bd.Calls()); got < 1 {
		t.Fatalf("no calls")
	}
	// A claimed story may be moved to other statuses, and updated for other things.
	bd = claimedBd("mw-x.1")
	for _, args := range [][]string{
		{"update", "mw-x.1", "--status", "open"},
		{"update", "mw-x.1", "--title", "deferred"},
		{"show", "mw-x.1", "--json"},
		{"comments", "add", "mw-x.1", "hello"},
	} {
		if run := runBdRetry(t, bd, args...); run.exit != 0 {
			t.Errorf("%v exited %d: %q", args, run.exit, run.errs.String())
		}
	}
	if got := bd.Status("mw-x.1"); got != "open" {
		t.Errorf("the status update did not run, status %s", got)
	}
}

func TestAHoldOfAStoryWhoseClaimLapsedStillRefusesInProgress(t *testing.T) {
	bd := apptest.NewFakeBd("mw-x.1")
	bd.Claim("mw-x.1", "mw@laptop", time.Now().Add(-time.Hour))
	if run := runBdRetry(t, bd, "update", "mw-x.1", "--status", "deferred"); run.exit == 0 {
		t.Errorf("an in-progress story with a lapsed lease was held")
	}
}

func TestAHoldIsNotBlockedWhenTheStoryCannotBeRead(t *testing.T) {
	bd := apptest.NewFakeBd("mw-x.1")
	bd.FailReads(apptest.BdFault{Said: "something odd"})
	if run := runBdRetry(t, bd, "update", "mw-x.1", "--status", "deferred"); run.exit != 0 || bd.Status("mw-x.1") != "deferred" {
		t.Errorf("exit %d, status %s", run.exit, bd.Status("mw-x.1"))
	}
}

// The same flag on mw path set: a re-path of a claimed story.

func TestPathSetOfAClaimedStoryNamesTheFlagAndWithItWritesTheWhy(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	if err := tracker.ClaimAs("mw-gq6.2", "mw@laptop", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err := application.PathSet{Tracker: tracker}.Run(context.Background(), "mw-gq6.2", "", []string{"model=sonnet"})
	if err == nil || !strings.Contains(err.Error(), "in_progress") || !strings.Contains(err.Error(), "--over-claim") || !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("want a refusal naming the status and --over-claim, got %v", err)
	}
	if detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2"); detail.Story.Overrides.Model != "" {
		t.Errorf("the path changed: %+v", detail.Story.Overrides)
	}

	if _, err := (application.PathSet{Tracker: tracker, OverClaim: "the Builder is on the wrong model"}).Run(context.Background(), "mw-gq6.2", "", []string{"model=sonnet"}); err != nil {
		t.Fatalf("with --over-claim: %v", err)
	}
	detail, _ := tracker.ShowStory(context.Background(), "mw-gq6.2")
	if detail.Story.Overrides.Model != domain.ModelSonnet {
		t.Errorf("the path was not set: %+v", detail.Story.Overrides)
	}
	comments := tracker.Comments("mw-gq6.2")
	if !strings.Contains(strings.Join(comments, "\n"), "wrong model") {
		t.Errorf("the why is not on the story: %q", comments)
	}
}

func TestPathSetOfAClosedStoryIsRefusedEvenOverClaim(t *testing.T) {
	tracker := aVaultWithTwoFormulas()
	if err := tracker.SetStatus("mw-gq6.2", apptest.StatusClosed); err != nil {
		t.Fatal(err)
	}
	_, err := application.PathSet{Tracker: tracker, OverClaim: "why"}.Run(context.Background(), "mw-gq6.2", "", []string{"model=sonnet"})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("want a refusal, got %v", err)
	}
}

package application_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// ctxChecks keeps the context its whole-tests run was given.
type ctxChecks struct{ ctx context.Context }

func (c *ctxChecks) Run(ctx context.Context, _, _ string) (application.Checked, error) {
	c.ctx = ctx
	return application.Checked{Command: "make test", Passed: true, Output: "ok\n"}, nil
}

// mw check runs the rig's tests under a context that names the story, so the
// test-runner ticket the run takes says whose it is (mw-gq6.1491).
func TestACheckRunsTheTestsUnderAContextThatNamesTheStory(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-x", domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: "sonnet", Effort: "high", Formula: "tdd-feature", Host: "laptop"})
	tracker.AddStory("mw-x", domain.Story{ID: "mw-x.1", Title: "One"})
	rigDir := filepath.Join(t.TempDir(), "rigs", "millwright")
	if err := os.MkdirAll(rigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checks := &ctxChecks{}
	_, err := application.Check{
		Tracker: tracker, Landing: &fakeRetryLanding{AheadCount: 1}, Checks: checks,
		Slot: aSlot{}, Records: &mapRecords{},
		Host: "laptop", Remote: "origin",
		Rigs: map[string]string{"millwright": rigDir},
		Out:  io.Discard,
	}.Run(context.Background(), "mw-x.1")
	if err != nil {
		t.Fatal(err)
	}
	if checks.ctx == nil {
		t.Fatal("the rig's tests were not run")
	}
	if got, want := application.TestRunHolder(checks.ctx, "tests of millwright"), "tests of millwright for mw-x.1"; got != want {
		t.Errorf("expected the ticket %q, got %q", want, got)
	}
}

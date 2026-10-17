package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// aLandingConflictedIn is a branch that does not merge into its target, and
// says which two files it conflicted in.
type aLandingConflictedIn struct{ fakeRetryLanding }

func (*aLandingConflictedIn) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{}, &application.MergeConflict{
		Files: []string{"package.json", "CHANGELOG.md"},
		Said:  "the merge has conflicts: CONFLICT (content): Merge conflict in package.json",
	}
}

func TestAMergeConflictNamesItsFilesWhetherTheStoryIsSentBackOrStopped(t *testing.T) {
	for name, sendBack := range map[string]bool{"sent back": true, "stopped": false} {
		t.Run(name, func(t *testing.T) {
			id := "mw-x.7"
			next := aCloseOutOn("laptop", id, aSlot{}, aCrossSlotOn(apptest.NewFakeTracker(), "laptop", new(atomic.Int32)), &aLandingConflictedIn{fakeRetryLanding{AheadCount: 1}}, &scriptedChecks{passes: []bool{true}})
			if !sendBack {
				next.Runner = nil
			}
			report, err := next.Run(context.Background(), id)
			if err != nil && sendBack {
				t.Fatalf("expected the close-out to end, got %v", err)
			}
			if report.Reason != application.ReasonMergeConflict || (report.SentBack != "") != sendBack {
				t.Fatalf("expected a merge conflict, sent back %v, got %+v", sendBack, report)
			}
			tracker := next.Tracker.(*apptest.FakeTracker)
			if got := tracker.Metadata(id)[application.ConflictFilesField]; got != "package.json,CHANGELOG.md" {
				t.Errorf("expected the files in the story's metadata, got %q", got)
			}
			const line = "Conflicted files: package.json, CHANGELOG.md"
			var found bool
			for _, c := range tracker.Comments(id) {
				found = found || strings.Contains(c, line)
			}
			if !found {
				t.Errorf("expected a comment holding %q, got %q", line, tracker.Comments(id))
			}
		})
	}
}

func TestAConflictErrorKeepsItsFilesThroughWrappingAndStillIsAMergeConflict(t *testing.T) {
	err := fmt.Errorf("main does not merge: %w", &application.MergeConflict{Files: []string{"a", "b"}, Said: "the merge has conflicts"})
	if !application.Conflicted(err) {
		t.Fatal("expected the error to be a merge conflict")
	}
	if got := strings.Join(application.ConflictFiles(err), " "); got != "a b" {
		t.Errorf("got the files %q", got)
	}
	if files := application.ConflictFiles(errors.New("push refused")); files != nil {
		t.Errorf("expected no files from another error, got %q", files)
	}
	if files := application.ConflictFiles(application.ErrMergeConflict); files != nil {
		t.Errorf("expected no files from a bare conflict, got %q", files)
	}
}

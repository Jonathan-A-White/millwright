package application

import (
	"context"
	"testing"
)

func TestAReadyTesterStoryWithNoFilesKeyWaitsForNoRunningStory(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	shares.add(openStory("mw-t.1", "feature", "a.go"))
	shares.add(openStory("mw-t.2", "feature", ""))
	if got, waits := shares.shared(context.Background(), openStory("mw-t.3", "tester", "")); waits {
		t.Fatalf("a Tester story claims no files, yet it waits: %+v", got)
	}
}

func TestARunningTesterStoryWithNoFilesKeyHoldsBackNoReadyStory(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	shares.add(openStory("mw-t.1", "tester", ""))
	if got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", "a.go")); waits {
		t.Fatalf("a running Tester story claims no files, yet it holds back a keyed story: %+v", got)
	}
	// It needs a files key of its own, but not for the Tester story.
	if got, waits := shares.shared(context.Background(), openStory("mw-t.3", "feature", "")); waits && got.Unkeyed != NeedsFilesKey {
		t.Fatalf("a running Tester story holds back an unkeyed feature story: %+v", got)
	}
}

func TestAReadyFeatureStoryWithNoFilesKeyNeedsOneAndWaitsForNoStory(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", ""))
	if !waits || got.Unkeyed != NeedsFilesKey || got.With != "mw-t.2" {
		t.Fatalf("expected the unkeyed feature story to need a files key, got %+v, %v", got, waits)
	}
	if want := "needs files key: mw path set mw-t.2 files=<csv>"; got.String() != want {
		t.Errorf("expected %q, got %q", want, got.String())
	}
}

func TestARunningStoryWithNoFilesKeyHoldsBackOnlyWhatItsBranchChanged(t *testing.T) {
	shares := &fileShares{
		rules:   filesRules(),
		changes: branchChangesStub{"src/a.go"},
		rigs:    map[string]string{"lampas": "/rig"},
	}
	shares.add(openStory("mw-t.1", "feature", ""))
	if got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", "src/b.go")); waits {
		t.Fatalf("a story naming other files waits for the unkeyed one: %+v", got)
	}
	got, waits := shares.shared(context.Background(), openStory("mw-t.3", "feature", "src/a.go"))
	if !waits || got.File != "src/a.go" || got.With != "mw-t.1" {
		t.Fatalf("expected the story naming src/a.go to wait for mw-t.1, got %+v, %v", got, waits)
	}
}

// branchChangesStub says every branch has changed the files it holds.
type branchChangesStub []string

func (b branchChangesStub) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return b, nil
}

func (b branchChangesStub) AddedFiles(context.Context, string, string, string) ([]AddedFile, error) {
	return nil, nil
}

func (b branchChangesStub) DiffLines(context.Context, string, string, string, string) ([]DiffLine, error) {
	return nil, nil
}

func TestATesterStoryThatNamesFilesIsComparedAsBefore(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	shares.add(openStory("mw-t.1", "feature", "a.go"))
	got, waits := shares.shared(context.Background(), openStory("mw-t.2", "tester", "a.go"))
	if !waits || got.File != "a.go" {
		t.Fatalf("expected the keyed Tester story to wait for a.go, got %+v, %v", got, waits)
	}
}

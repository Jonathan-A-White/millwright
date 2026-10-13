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
	if got, waits := shares.shared(context.Background(), openStory("mw-t.3", "feature", "")); waits {
		t.Fatalf("a running Tester story holds back an unkeyed feature story: %+v", got)
	}
}

func TestAReadyFeatureStoryWithNoFilesKeyStillWaits(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	shares.add(openStory("mw-t.1", "feature", "a.go"))
	got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", ""))
	if !waits || got.Unkeyed != ReadyUnkeyed || got.With != "mw-t.1" {
		t.Fatalf("expected the unkeyed feature story to wait for mw-t.1, got %+v, %v", got, waits)
	}
}

func TestATesterStoryThatNamesFilesIsComparedAsBefore(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	shares.add(openStory("mw-t.1", "feature", "a.go"))
	got, waits := shares.shared(context.Background(), openStory("mw-t.2", "tester", "a.go"))
	if !waits || got.File != "a.go" {
		t.Fatalf("expected the keyed Tester story to wait for a.go, got %+v, %v", got, waits)
	}
}

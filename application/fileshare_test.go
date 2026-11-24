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

// A claim is made before dispatch writes the host over "auto", so for that
// moment another host's story names no host and its claim alone says where it
// runs (mw-gq6.1015): the files it names are still kept from a ready story.
func TestAStoryAnotherHostClaimedWhileItsPathNamesAutoKeepsItsFiles(t *testing.T) {
	running := openStory("mw-t.1", "feature", "src/ui/List.tsx")
	running.Status, running.Assignee = "in_progress", "mw@cloud2"
	running.Defaults.Host = "auto"
	if on := running.ClaimedAwayFrom("cloud1"); on != "cloud2" {
		t.Fatalf("expected the claim to place the story on cloud2, got %q", on)
	}
	if on := running.ClaimedAwayFrom("cloud2"); on != "" {
		t.Fatalf("a story claimed by this host is not away, got %q", on)
	}
	shares := &fileShares{rules: filesRules()}
	shares.addAway(running, running.ClaimedHost())
	got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", "src/a.go,src/ui/List.tsx"))
	if !waits || got.File != "src/ui/List.tsx" || got.With != "mw-t.1" || got.Host != "cloud2" {
		t.Fatalf("expected the story to wait for mw-t.1 on cloud2, got %+v, %v", got, waits)
	}
}

func TestAStoryWithNeitherAHostNorAClaimIsWorkedOnNoHost(t *testing.T) {
	story := openStory("mw-t.1", "feature", "a.go")
	story.Defaults.Host = "auto"
	if on := story.ClaimedHost(); on != "" {
		t.Fatalf("expected no host, got %q", on)
	}
	story.Assignee = "someone"
	if on := story.ClaimedHost(); on != "" {
		t.Fatalf("a claim not made by mw names no host, got %q", on)
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

// A held story keeps the work its kept branch carries, so it keeps its files
// from a ready story while that branch is ahead of its target (mw-gq6.881).
func TestAHeldStoryWithItsBranchAheadKeepsItsFiles(t *testing.T) {
	shares := &fileShares{
		rules:   filesRules(),
		changes: branchChangesStub{"docs/verse-view.md"},
		rigs:    map[string]string{"lampas": "/rig"},
	}
	shares.addKept(openStory("mw-t.1", "feature", "src/a.go"))
	got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", "docs/verse-view.md"))
	if !waits || !got.Kept || got.File != "docs/verse-view.md" || got.With != "mw-t.1" {
		t.Fatalf("expected the story to wait for the held mw-t.1, got %+v, %v", got, waits)
	}
	if want := "waiting: shares docs/verse-view.md with mw-t.1; mw-t.1 is held, its kept branch carries work"; got.String() != want {
		t.Errorf("expected %q, got %q", want, got.String())
	}
	if got, waits := shares.shared(context.Background(), openStory("mw-t.3", "feature", "src/b.go")); waits {
		t.Fatalf("a story naming no file the held one has changed or names waits: %+v", got)
	}
}

func TestAHeldStoryWithItsBranchNotAheadHoldsNothingBack(t *testing.T) {
	shares := &fileShares{
		rules:   filesRules(),
		changes: branchChangesStub{},
		rigs:    map[string]string{"lampas": "/rig"},
	}
	shares.addKept(openStory("mw-t.1", "feature", "src/a.go"))
	if got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", "src/a.go")); waits {
		t.Fatalf("a held story with nothing on its branch holds back a story: %+v", got)
	}
}

// The deadlock of lampas, 2026-11-24 (mw-gq6.1705): the red-main carrier mw-y1k16cd
// was held on a file a story queued for landing names, and that story's landing
// was held on the carrier.
func redMainCarrier(id, files string) StoryDetail {
	carrier := openStory(id, "bugfix", files)
	carrier.Story.Title = "[bug] main is red: appearance.spec.ts:207"
	carrier.Labels = []string{LabelBug}
	return carrier
}

func queuedStory(id, files string) StoryDetail {
	queued := openStory(id, "feature", files)
	queued.Status = "in_progress"
	queued.Labels = []string{RunState + ":" + RunQueued}
	return queued
}

func TestAReadyRedMainCarrierIsNotHeldOnAFileAQueuedStoryNames(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	shares.add(queuedStory("mw-t.1", "tests/e2e/appearance.spec.ts"))
	if got, waits := shares.shared(context.Background(), redMainCarrier("mw-t.2", "tests/e2e/appearance.spec.ts")); waits {
		t.Fatalf("the carrier waits for a story queued on the carrier itself: %+v", got)
	}
	// A story queued on another host is read the same.
	away := &fileShares{rules: filesRules()}
	away.addAway(queuedStory("mw-t.1", "tests/e2e/appearance.spec.ts"), "desktop")
	if got, waits := away.shared(context.Background(), redMainCarrier("mw-t.2", "tests/e2e/appearance.spec.ts")); waits {
		t.Fatalf("the carrier waits for a story queued on desktop: %+v", got)
	}
}

func TestAReadyRedMainCarrierStillWaitsForAStoryABuilderIsRunning(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	running := openStory("mw-t.1", "feature", "tests/e2e/appearance.spec.ts")
	running.Status = "in_progress"
	shares.add(running)
	got, waits := shares.shared(context.Background(), redMainCarrier("mw-t.2", "tests/e2e/appearance.spec.ts"))
	if !waits || got.With != "mw-t.1" || got.File != "tests/e2e/appearance.spec.ts" {
		t.Fatalf("expected the carrier to wait for the running mw-t.1, got %+v, %v", got, waits)
	}
}

func TestAReadyStoryThatIsNoCarrierIsStillHeldOnAFileAQueuedStoryNames(t *testing.T) {
	shares := &fileShares{rules: filesRules()}
	shares.add(queuedStory("mw-t.1", "tests/e2e/appearance.spec.ts"))
	got, waits := shares.shared(context.Background(), openStory("mw-t.2", "feature", "tests/e2e/appearance.spec.ts"))
	if !waits || got.With != "mw-t.1" {
		t.Fatalf("expected an ordinary story to wait for the queued mw-t.1, got %+v, %v", got, waits)
	}
}

func TestACarrierIsNotHeldOnAStoryHeldOnItsRedOrOnAnotherTarget(t *testing.T) {
	onRed := openStory("mw-t.1", "feature", "a.go")
	onRed.Status = "in_progress"
	onRed.Labels = []string{RunState + ":" + RunCloseOutAgain, MainRedState + ":mw-t.2"}
	shares := &fileShares{rules: filesRules()}
	shares.add(onRed)
	if got, waits := shares.shared(context.Background(), redMainCarrier("mw-t.2", "a.go")); waits {
		t.Fatalf("the carrier waits for the story held on it: %+v", got)
	}
	// A queued story landing on another branch does not wait on this carrier.
	other := queuedStory("mw-t.3", "a.go")
	other.Defaults.Branch = "release"
	shares = &fileShares{rules: filesRules()}
	shares.add(other)
	if _, waits := shares.shared(context.Background(), redMainCarrier("mw-t.4", "a.go")); !waits {
		t.Fatal("the carrier does not wait for a story queued for another branch")
	}
}

func TestACarrierIsRecognisedByTheTitleMwNextAndMwLandFileItUnder(t *testing.T) {
	for title, want := range map[string]bool{
		"[bug] main is red: TestA, TestB":                true,
		"[bug] host gap: cloud2: go fails TestA":         true,
		"[bug] mw dispatch holds a carrier on files":     false,
		"main is red: TestA":                             false,
		"[feature] show that main is red: on the screen": false,
	} {
		story := openStory("mw-t.1", "bugfix", "a.go")
		story.Story.Title = title
		if got := IsRedMainCarrier(story); got != want {
			t.Errorf("IsRedMainCarrier(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestAReadyCarrierIsNotHeldOrFoldedForAFileAQueuedStoryNamesInTheCloudDemand(t *testing.T) {
	ready := []StoryDetail{redMainCarrier("mw-t.2", "a.go")}
	ready[0].Defaults.Host = "auto"
	waiting, held, folded := readyDemand(context.Background(), []StoryDetail{queuedStory("mw-t.1", "a.go")}, ready)
	if waiting != 1 || held != 0 || folded != 0 {
		t.Fatalf("expected the carrier to wait for a box, got waiting %d, held %d, folded %d", waiting, held, folded)
	}
}

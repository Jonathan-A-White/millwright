package rig_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aTaggedLanding lands a branch of one commit on main at the origin, the tags
// given made in the rig before the landing is pushed, and reports what
// PublishTags says of it, with the landing still open for the caller's check.
func aTaggedLanding(t *testing.T, here string, named []string, tag func(branchTip string)) (published []string, elsewhere, missing []string) {
	t.Helper()
	ctx := context.Background()
	worktrees := rig.New()
	aBranchAppending(t, here, "mw/story", "README.md", "the story")
	tag(run(t, here, "git", "rev-parse", "mw/story"))
	run(t, here, "git", "fetch", "-q", "origin")
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	defer worktrees.CloseLanding(ctx, here, dir)
	landed, err := worktrees.Merge(ctx, dir, "mw/story")
	if err != nil {
		t.Fatalf("merging: %v", err)
	}
	if err := worktrees.Push(ctx, dir, "origin", "main"); err != nil {
		t.Fatalf("pushing: %v", err)
	}
	got, err := worktrees.PublishTags(ctx, dir, "origin", landed.Before, named)
	if err != nil {
		t.Fatalf("publishing the tags: %v", err)
	}
	if len(got.Failed) > 0 {
		t.Fatalf("a tag failed: %v", got.Failed)
	}
	return got.Pushed, got.Elsewhere, got.Missing
}

func remoteTagSha(t *testing.T, here, tag string) string {
	t.Helper()
	line := run(t, here, "git", "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if fields := strings.Fields(line); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func TestALandingPushesATagOnACommitOfItsRange(t *testing.T) {
	here, _ := aRig(t)
	var tip string
	pushed, elsewhere, _ := aTaggedLanding(t, here, nil, func(sha string) {
		tip = sha
		run(t, here, "git", "tag", "v0.3.0", sha)
	})
	if !reflect.DeepEqual(pushed, []string{"v0.3.0"}) || len(elsewhere) != 0 {
		t.Fatalf("expected v0.3.0 pushed, got %v and %v", pushed, elsewhere)
	}
	if got := remoteTagSha(t, here, "v0.3.0"); got != tip {
		t.Errorf("expected v0.3.0 on the remote at %s, got %q", tip, got)
	}
}

func TestALandingPushesAnAnnotatedTagAtTheCommitItNames(t *testing.T) {
	here, _ := aRig(t)
	pushed, _, _ := aTaggedLanding(t, here, nil, func(sha string) {
		run(t, here, "git", "tag", "-a", "-m", "release", "v0.3.1", sha)
	})
	if !reflect.DeepEqual(pushed, []string{"v0.3.1"}) {
		t.Fatalf("expected v0.3.1 pushed, got %v", pushed)
	}
}

func TestALandingLeavesATagOutsideItsRangeAlone(t *testing.T) {
	here, _ := aRig(t)
	pushed, _, _ := aTaggedLanding(t, here, nil, func(string) {
		run(t, here, "git", "tag", "v0.1.0", "main")
	})
	if len(pushed) != 0 {
		t.Fatalf("expected nothing pushed, got %v", pushed)
	}
	if got := remoteTagSha(t, here, "v0.1.0"); got != "" {
		t.Errorf("expected v0.1.0 not on the remote, got %q", got)
	}
}

func TestALandingNeitherMovesNorForcesATagTheRemoteHasElsewhere(t *testing.T) {
	here, _ := aRig(t)
	main := run(t, here, "git", "rev-parse", "main")
	run(t, here, "git", "tag", "v0.2.0", main)
	run(t, here, "git", "push", "-q", "origin", "refs/tags/v0.2.0")
	pushed, elsewhere, _ := aTaggedLanding(t, here, nil, func(sha string) {
		run(t, here, "git", "tag", "-f", "v0.2.0", sha)
	})
	if len(pushed) != 0 || !reflect.DeepEqual(elsewhere, []string{"v0.2.0"}) {
		t.Fatalf("expected v0.2.0 reported elsewhere, got %v and %v", pushed, elsewhere)
	}
	if got := remoteTagSha(t, here, "v0.2.0"); got != main {
		t.Errorf("expected v0.2.0 left at %s on the remote, got %q", main, got)
	}
}

func TestATagTheCriteriaNameThatIsNowhereIsMissing(t *testing.T) {
	here, _ := aRig(t)
	_, _, missing := aTaggedLanding(t, here, []string{"v1.2.3"}, func(string) {})
	if !reflect.DeepEqual(missing, []string{"v1.2.3"}) {
		t.Fatalf("expected v1.2.3 missing, got %v", missing)
	}
}

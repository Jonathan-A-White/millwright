package application_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestTestNamesOfNamesTheTopLevelTestsThatFailed(t *testing.T) {
	cases := map[string]struct {
		output string
		want   []string
	}{
		"go test, a subtest and its parent once": {
			"--- FAIL: TestScorerScoresTheFixtureClips (21.93s)\n    --- FAIL: TestScorerScoresTheFixtureClips/clip_1 (1.00s)\n--- FAIL: TestOther (0.00s)\nFAIL\tgithub.com/x/contrib\t64.042s\n",
			[]string{"TestScorerScoresTheFixtureClips", "TestOther"},
		},
		"vitest": {"FAIL  src/a.test.ts > does a thing\n", []string{"src/a.test.ts > does a thing"}},
		"none":   {"ok  \tgithub.com/x/y\t1.2s\n", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := application.TestNamesOf(c.output); !reflect.DeepEqual(got, c.want) {
				t.Errorf("TestNamesOf = %q, want %q", got, c.want)
			}
		})
	}
}

// The gates of mw-gq6.858 and mw-gq6.859 failed the mail-notify tests of
// contrib/ though neither story touched it (mw-gq6.861): a close-out does not
// put such a failure down to the story.
func TestPackagesOutsideTheStoryAreThoseThatHoldNoChangedFile(t *testing.T) {
	failed := "--- FAIL: TestALineThatNeverGoes (62.07s)\nFAIL\n" +
		"FAIL\tgithub.com/x/millwright/contrib\t93.086s\n" +
		"FAIL\tgithub.com/x/millwright/infrastructure/beads\t1.0s\n"
	cases := map[string]struct {
		changed []string
		want    []string
	}{
		"the story changes other packages": {
			[]string{"application/next.go", "docs/a.md", "mw-gq6.858.md"},
			[]string{"github.com/x/millwright/contrib", "github.com/x/millwright/infrastructure/beads"},
		},
		"a failing package holds a changed file": {[]string{"application/next.go", "contrib/mail-notify"}, nil},
		"a package of the same name deeper down is not it": {
			[]string{"infrastructure/beads/sub/x.go"},
			[]string{"github.com/x/millwright/contrib", "github.com/x/millwright/infrastructure/beads"},
		},
		"a Go file at the root is read by every package": {[]string{"main.go"}, nil},
		"go.mod is read by every package":                {[]string{"go.mod"}, nil},
		"the story changed nothing":                      {nil, []string{"github.com/x/millwright/contrib", "github.com/x/millwright/infrastructure/beads"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := application.PackagesOutsideTheStory(failed, c.changed); !reflect.DeepEqual(got, c.want) {
				t.Errorf("PackagesOutsideTheStory = %q, want %q", got, c.want)
			}
		})
	}
}

func TestNoPackageIsOutsideTheStoryWhenTheOutputNamesNone(t *testing.T) {
	if got := application.PackagesOutsideTheStory("--- FAIL: TestX (0.01s)\n", []string{"application/next.go"}); got != nil {
		t.Errorf("PackagesOutsideTheStory = %q, want none: with no package named the failure may be the story's", got)
	}
}

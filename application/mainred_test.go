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

package application_test

import (
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestTimingEnvIsNothingWithoutGlobs(t *testing.T) {
	if got := application.TimingEnv("skip", nil); len(got) != 0 {
		t.Errorf("TimingEnv with no globs = %v, want empty", got)
	}
}

func TestTimingEnvNamesTheModeAndTheCommaJoinedGlobs(t *testing.T) {
	got := application.TimingEnv("skip", []string{"tests/timing/**", "**/*.timing.test.ts"})
	want := map[string]string{"MW_TIMING": "skip", "MW_TIMING_TESTS": "tests/timing/**,**/*.timing.test.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TimingEnv = %v, want %v", got, want)
	}
}

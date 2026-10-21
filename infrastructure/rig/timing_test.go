package rig_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestTheTaggedTestsOfACommitAreReadWithTheirCoversLines(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "tests/timing/sw.test.ts", "// covers: src/sw/**, src/retry.ts\nimport x\n")
	write(t, here, "tests/timing/plain.test.ts", "import y\n")
	write(t, here, "tests/unit/a.test.ts", "// covers: src/a.ts\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Timing tests")
	commit := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD"))

	got, err := rig.New().Tagged(context.Background(), here, commit, []string{"tests/timing/**"})
	if err != nil {
		t.Fatal(err)
	}
	want := []application.TaggedTest{
		{File: "tests/timing/plain.test.ts"},
		{File: "tests/timing/sw.test.ts", Covers: []string{"src/sw/**", "src/retry.ts"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tagged = %+v, want %+v", got, want)
	}
}

func TestTheFilesChangedBetweenTwoCommitsAreListed(t *testing.T) {
	here, _ := aRig(t)
	first := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD"))
	write(t, here, "src/sw/a.ts", "a\n")
	write(t, here, "README.md", "# changed\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Change")
	second := strings.TrimSpace(run(t, here, "git", "rev-parse", "HEAD"))

	got, err := rig.New().ChangedBetween(context.Background(), here, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"README.md", "src/sw/a.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("changed = %v, want %v", got, want)
	}
	if _, err := rig.New().ChangedBetween(context.Background(), here, "0000000000000000000000000000000000000000", second); err == nil {
		t.Errorf("a commit that is not there was no error")
	}
}

func TestATimingRunsEnvironmentTakesThePlaceOfTheSkipEveryOtherRunGets(t *testing.T) {
	dir := t.TempDir()
	checks := rig.NewChecks(rig.WithCommand(`echo "timing=$MW_TIMING tests=$MW_TIMING_TESTS"`), rig.WithTimingTests(map[string][]string{"lampas": {"tests/timing/**"}}))

	skipped, err := checks.RunLine(context.Background(), "lampas", dir, `echo "timing=$MW_TIMING tests=$MW_TIMING_TESTS"`)
	if err != nil || !strings.Contains(skipped.Output, "timing=skip tests=tests/timing/**") {
		t.Fatalf("a gate run got %q, %v; want MW_TIMING=skip", skipped.Output, err)
	}
	only, err := checks.RunTiming(context.Background(), "lampas", dir, `echo "timing=$MW_TIMING tests=$MW_TIMING_TESTS"`, application.TimingEnv(application.TimingOnly, []string{"tests/timing/**", "x/**"}))
	if err != nil || !strings.Contains(only.Output, "timing=only tests=tests/timing/**,x/**") {
		t.Errorf("a timing run got %q, %v; want MW_TIMING=only and the globs", only.Output, err)
	}
}

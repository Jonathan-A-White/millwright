package rig_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// specsRepo is a repository with a main branch holding one old spec and a
// branch cut from it whose commits write the files given.
func specsRepo(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, body string) {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("tests/e2e/old.spec.ts", "old\n")
	git("add", "-A")
	git("commit", "-qm", "main")
	git("checkout", "-q", "-b", "story")
	for _, name := range files {
		write(name, "story\n")
	}
	git("add", "-A")
	git("commit", "-qm", "story")
	return dir
}

func TestOnlyTheSpecsTheBranchChangedAreGivenToTheRigsCommand(t *testing.T) {
	dir := specsRepo(t, "tests/e2e/talk-fit.spec.ts", "tests/e2e/old.spec.ts", "tests/e2e/shared/helper.ts", "tests/unit/a.spec.ts", "src/x.ts")
	log := filepath.Join(t.TempDir(), "log")
	checks := rig.NewChecks(rig.WithSpecCommands(map[string]string{"lampas": "printf '%s\\n' {specs} > " + log}))

	run, err := checks.RunSpecs(context.Background(), "lampas", dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tests/e2e/old.spec.ts", "tests/e2e/talk-fit.spec.ts"}
	if !run.Passed || !reflect.DeepEqual(run.Specs, want) {
		t.Fatalf("expected a passing run on %v, got %+v", want, run)
	}
	if got, _ := os.ReadFile(log); string(got) != strings.Join(want, "\n")+"\n" {
		t.Errorf("expected the command to be given the specs, got %q", got)
	}
}

func TestNothingIsRunWhenTheRigHasNoCommandOrTheBranchChangedNoSpec(t *testing.T) {
	withSpec := specsRepo(t, "tests/e2e/talk-fit.spec.ts")
	noSpec := specsRepo(t, "src/x.ts")
	marker := filepath.Join(t.TempDir(), "ran")
	with := rig.NewChecks(rig.WithSpecCommands(map[string]string{"lampas": "touch " + marker + " # {specs}"}))

	for name, run := range map[string]func() (int, error){
		"no command": func() (int, error) {
			r, err := rig.NewChecks().RunSpecs(context.Background(), "lampas", withSpec, "main")
			return len(r.Specs), err
		},
		"another rig": func() (int, error) {
			r, err := with.RunSpecs(context.Background(), "millwright", withSpec, "main")
			return len(r.Specs), err
		},
		"no spec changed": func() (int, error) {
			r, err := with.RunSpecs(context.Background(), "lampas", noSpec, "main")
			return len(r.Specs), err
		},
	} {
		if n, err := run(); err != nil || n != 0 {
			t.Errorf("%s: expected nothing to be run, got %d specs, error %v", name, n, err)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("expected the command never to have run")
	}
}

func TestAFailingSpecRunIsAnAnswerAndAMissingProgramIsNotARedSpec(t *testing.T) {
	dir := specsRepo(t, "tests/e2e/talk-fit.spec.ts")

	failing := rig.NewChecks(rig.WithSpecCommands(map[string]string{"lampas": "echo 'FAIL {specs}'; exit 1"}))
	run, err := failing.RunSpecs(context.Background(), "lampas", dir, "main")
	if err != nil || run.Passed || run.NotRun || !strings.Contains(run.Output, "FAIL tests/e2e/talk-fit.spec.ts") {
		t.Errorf("expected a failed run that names the spec, got %+v (error: %v)", run, err)
	}

	missing := rig.NewChecks(rig.WithSpecCommands(map[string]string{"lampas": "no-such-program-here {specs}"}))
	run, err = missing.RunSpecs(context.Background(), "lampas", dir, "main")
	if err != nil || run.Passed || !run.NotRun {
		t.Errorf("expected a run that did not start, got %+v (error: %v)", run, err)
	}
}

func TestSpecNamesAreQuotedForTheShell(t *testing.T) {
	dir := specsRepo(t, "tests/e2e/it's here.spec.ts")
	log := filepath.Join(t.TempDir(), "log")
	checks := rig.NewChecks(rig.WithSpecCommands(map[string]string{"lampas": "printf '%s\\n' {specs} > " + log}))

	if _, err := checks.RunSpecs(context.Background(), "lampas", dir, "main"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(log); string(got) != "tests/e2e/it's here.spec.ts\n" {
		t.Errorf("expected the name to survive the shell whole, got %q", got)
	}
}

func TestAChangedTimingSpecIsLeftOutOfTheRigsChangedSpecsCommand(t *testing.T) {
	timing := map[string][]string{"lampas": {"tests/e2e/turn-pages-trace.spec.ts"}}
	command := func(log string) map[string]string {
		return map[string]string{"lampas": "printf '%s\\n' {specs} > " + log}
	}

	t.Run("only a timing spec changed: nothing is run and the spec is named as left out", func(t *testing.T) {
		dir := specsRepo(t, "tests/e2e/turn-pages-trace.spec.ts")
		log := filepath.Join(t.TempDir(), "log")
		checks := rig.NewChecks(rig.WithSpecCommands(command(log)), rig.WithTimingTests(timing))

		run, err := checks.RunSpecs(context.Background(), "lampas", dir, "main")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(log); err == nil {
			t.Error("expected the changed-specs command never to run on a timing spec")
		}
		if len(run.Specs) != 0 || run.Command != "" {
			t.Errorf("expected no run, got %+v", run)
		}
		if want := []string{"tests/e2e/turn-pages-trace.spec.ts"}; !reflect.DeepEqual(run.LeftOut, want) {
			t.Errorf("LeftOut = %v, want %v", run.LeftOut, want)
		}
	})

	t.Run("a timing spec and an ordinary one changed: the command gets the ordinary one", func(t *testing.T) {
		dir := specsRepo(t, "tests/e2e/turn-pages-trace.spec.ts", "tests/e2e/talk-fit.spec.ts")
		log := filepath.Join(t.TempDir(), "log")
		checks := rig.NewChecks(rig.WithSpecCommands(command(log)), rig.WithTimingTests(timing))

		run, err := checks.RunSpecs(context.Background(), "lampas", dir, "main")
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"tests/e2e/talk-fit.spec.ts"}; !run.Passed || !reflect.DeepEqual(run.Specs, want) {
			t.Errorf("expected a passing run on %v, got %+v", want, run)
		}
		if got, _ := os.ReadFile(log); string(got) != "tests/e2e/talk-fit.spec.ts\n" {
			t.Errorf("expected the command to be given the ordinary spec only, got %q", got)
		}
		if want := []string{"tests/e2e/turn-pages-trace.spec.ts"}; !reflect.DeepEqual(run.LeftOut, want) {
			t.Errorf("LeftOut = %v, want %v", run.LeftOut, want)
		}
	})

	t.Run("a rig with no timing_tests runs every changed spec, as before", func(t *testing.T) {
		dir := specsRepo(t, "tests/e2e/turn-pages-trace.spec.ts", "tests/e2e/talk-fit.spec.ts")
		log := filepath.Join(t.TempDir(), "log")
		checks := rig.NewChecks(rig.WithSpecCommands(command(log)), rig.WithTimingTests(map[string][]string{"other": {"tests/e2e/*.spec.ts"}}))

		run, err := checks.RunSpecs(context.Background(), "lampas", dir, "main")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"tests/e2e/talk-fit.spec.ts", "tests/e2e/turn-pages-trace.spec.ts"}
		if !reflect.DeepEqual(run.Specs, want) || len(run.LeftOut) != 0 {
			t.Errorf("expected every changed spec, got %+v", run)
		}
		if got, _ := os.ReadFile(log); string(got) != strings.Join(want, "\n")+"\n" {
			t.Errorf("expected the command as before, got %q", got)
		}
	})
}

func TestAChangedTimingFileIsLeftOutOfTheRelatedTestsLine(t *testing.T) {
	dir := specsRepo(t, "tests/e2e/turn-pages-trace.spec.ts", "src/x.ts")
	log := filepath.Join(t.TempDir(), "log")
	checks := rig.NewChecks(
		rig.WithRelatedCommands(map[string]string{"lampas": "printf '%s\\n' {files} > " + log}),
		rig.WithTimingTests(map[string][]string{"lampas": {"tests/e2e/turn-pages-trace.spec.ts"}}))

	run, err := checks.RunRelated(context.Background(), "lampas", dir, application.RelatedSpan{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"src/x.ts"}; !run.Ran || !reflect.DeepEqual(run.Files, want) {
		t.Errorf("expected the line run on %v, got %+v", want, run)
	}
	if got, _ := os.ReadFile(log); string(got) != "src/x.ts\n" {
		t.Errorf("expected the line to be given src/x.ts only, got %q", got)
	}

	only := specsRepo(t, "tests/e2e/turn-pages-trace.spec.ts")
	run, err = checks.RunRelated(context.Background(), "lampas", only, application.RelatedSpan{Base: "main"})
	if err != nil || run.Ran || !run.NoFiles {
		t.Errorf("expected nothing to run on a branch that changed only a timing test, got %+v (error: %v)", run, err)
	}
}

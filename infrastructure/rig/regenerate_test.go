package rig_test

// A rig's regenerated_files (mw-gq6.1666): a landing merge that conflicts only
// in generated files takes the target's side of each, runs the generator on the
// merged tree and commits; any other conflict, or a generator that fails or
// leaves a conflict marker, is the whole merge undone. Real git, a stand-in for
// the rig's generator.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

const generatedTable = "# GENERATED: do not edit\nch1 a\nch2 b\nch3 c\n"

// twoStoriesRegenerating opens a rig whose files each hold a table, cuts mw/one
// and mw/two from it, each changing the second row of every file to its own,
// and lands mw/one on main at the origin: so that merging mw/two conflicts in
// each of files.
func twoStoriesRegenerating(t *testing.T, files ...string) string {
	t.Helper()
	here, _ := aRig(t)
	for _, file := range files {
		write(t, here, file, generatedTable)
	}
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The tables open")
	run(t, here, "git", "push", "-q", "origin", "main")
	for _, story := range []string{"one", "two"} {
		run(t, here, "git", "checkout", "-q", "-b", "mw/"+story, "main")
		for _, file := range files {
			write(t, here, file, strings.Replace(generatedTable, "ch2 b", "ch2 from "+story, 1))
		}
		run(t, here, "git", "commit", "-qam", "Story "+story)
	}
	run(t, here, "git", "checkout", "-q", "main")
	if err := landWith(t, rig.New(), here, "mw/one", nil); err != nil {
		t.Fatalf("landing the first story: %v", err)
	}
	return here
}

// aGenerator is a stand-in for the rig's generator: it records the commands it
// is run by, in which directory, and rewrites each file it is told to as the
// merged tree would have it.
type aGenerator struct {
	ran   []string
	dirs  []string
	write map[string]string
	said  string
	err   error
}

func (g *aGenerator) run(_ context.Context, dir, command string) (string, error) {
	g.ran, g.dirs = append(g.ran, command), append(g.dirs, dir)
	for file, text := range g.write {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
			return "", err
		}
	}
	return g.said, g.err
}

// mergeRuled merges branch into a fresh landing at origin/main by rules, and
// reports what it said, and the landing's own state afterwards.
func mergeRuled(t *testing.T, here, branch string, worktrees *rig.Worktrees, rules domain.MergeRules) (landed application.Landed, left string, err error) {
	t.Helper()
	ctx := context.Background()
	run(t, here, "git", "fetch", "-q", "origin")
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatalf("opening the landing: %v", err)
	}
	defer func() {
		if err := worktrees.CloseLanding(ctx, here, dir); err != nil {
			t.Errorf("closing the landing: %v", err)
		}
	}()
	landed, err = worktrees.MergeRuled(ctx, dir, branch, rules)
	left = run(t, dir, "git", "status", "--porcelain")
	return landed, left, err
}

func TestAMergeConflictingOnlyInAGeneratedFileRunsItsGeneratorAndCommits(t *testing.T) {
	here := twoStoriesRegenerating(t, "docs/gen.txt")
	generator := &aGenerator{write: map[string]string{"docs/gen.txt": "# GENERATED\nch1 a\nch2 regenerated\nch3 c\n"}}
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/*.txt"}, Run: "npm run gen"}}}

	landed, left, err := mergeRuled(t, here, "mw/two", rig.New(rig.WithRegenerator(generator.run)), rules)
	if err != nil {
		t.Fatalf("expected the merge settled by the generator, got %v", err)
	}
	if want := []application.Regeneration{{Path: "docs/gen.txt", Run: "npm run gen"}}; fmt.Sprint(landed.Regenerated) != fmt.Sprint(want) {
		t.Errorf("regenerated = %v, want %v", landed.Regenerated, want)
	}
	if got := landed.Regenerated[0].String(); got != "regenerated docs/gen.txt by npm run gen" {
		t.Errorf("the line reads %q", got)
	}
	if len(generator.ran) != 1 || generator.ran[0] != "npm run gen" {
		t.Errorf("expected the generator run once, got %v", generator.ran)
	}
	if got := run(t, here, "git", "show", landed.Commit+":docs/gen.txt"); !strings.Contains(got, "ch2 regenerated") || strings.Contains(got, "<<<<<<<") {
		t.Errorf("expected the merge commit to hold what the generator wrote, got %q", got)
	}
	if strings.TrimSpace(left) != "" {
		t.Errorf("expected nothing left uncommitted in the landing, got %q", left)
	}
	if landed.FastForward || landed.Commit == "" || landed.Commit == landed.Before {
		t.Errorf("expected a merge commit of its own, got %+v", landed)
	}
}

func TestAGeneratorIsRunOnceForEveryFileOfItsEntryThatConflicted(t *testing.T) {
	here := twoStoriesRegenerating(t, "docs/gen.txt", "docs/exo.txt")
	generator := &aGenerator{write: map[string]string{"docs/gen.txt": generatedTable, "docs/exo.txt": generatedTable}}
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/*.txt"}, Run: "npm run gen"}}}

	landed, _, err := mergeRuled(t, here, "mw/two", rig.New(rig.WithRegenerator(generator.run)), rules)
	if err != nil {
		t.Fatalf("expected the merge settled by the generator, got %v", err)
	}
	if len(generator.ran) != 1 || len(landed.Regenerated) != 2 {
		t.Errorf("expected one run and two files named, got %v and %v", generator.ran, landed.Regenerated)
	}
	if len(generator.dirs) == 1 && !strings.Contains(generator.dirs[0], ".mw-landing-") {
		t.Errorf("expected the generator run in the landing worktree, got %s", generator.dirs[0])
	}
}

func TestAGeneratedFileAndAUnionFileTogetherAreBothSettled(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "docs/gen.txt", generatedTable)
	write(t, here, "ATTRIBUTION.md", "# attributions\n\nkept\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Open")
	run(t, here, "git", "push", "-q", "origin", "main")
	for _, story := range []string{"one", "two"} {
		run(t, here, "git", "checkout", "-q", "-b", "mw/"+story, "main")
		write(t, here, "docs/gen.txt", strings.Replace(generatedTable, "ch2 b", "ch2 from "+story, 1))
		write(t, here, "ATTRIBUTION.md", "# attributions\n\nkept\nadded by "+story+"\n")
		run(t, here, "git", "commit", "-qam", "Story "+story)
	}
	run(t, here, "git", "checkout", "-q", "main")
	rules := domain.MergeRules{Union: []string{"ATTRIBUTION.md"}, Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "make gen"}}}
	generator := &aGenerator{write: map[string]string{"docs/gen.txt": generatedTable}}
	worktrees := rig.New(rig.WithRegenerator(generator.run))
	if _, _, err := mergeRuled(t, here, "mw/one", worktrees, domain.MergeRules{}); err != nil {
		t.Fatal(err)
	}
	run(t, here, "git", "push", "-q", "origin", "mw/one:main")

	landed, _, err := mergeRuled(t, here, "mw/two", worktrees, rules)
	if err != nil {
		t.Fatalf("expected both settled, got %v", err)
	}
	if len(landed.Unioned) != 1 || len(landed.Regenerated) != 1 || len(generator.ran) != 1 {
		t.Errorf("unioned %v, regenerated %v, ran %v", landed.Unioned, landed.Regenerated, generator.ran)
	}
}

func TestAConflictInAnUnlistedFileSetsTheMergeAsideAsBefore(t *testing.T) {
	here := twoStoriesRegenerating(t, "docs/gen.txt", "src/code.txt")
	generator := &aGenerator{write: map[string]string{"docs/gen.txt": generatedTable}}
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "npm run gen"}}}

	_, left, err := mergeRuled(t, here, "mw/two", rig.New(rig.WithRegenerator(generator.run)), rules)
	if !application.Conflicted(err) {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if files := application.ConflictFiles(err); len(files) != 2 {
		t.Errorf("expected both files named as in conflict, got %v", files)
	}
	if len(generator.ran) != 0 {
		t.Errorf("expected no generator run for a merge that is set aside, got %v", generator.ran)
	}
	if strings.TrimSpace(left) != "" {
		t.Errorf("expected the landing as it was found, got %q", left)
	}
}

func TestAGeneratorThatFailsOrLeavesAMarkerSetsTheMergeAsideSayingWhy(t *testing.T) {
	for name, tc := range map[string]struct {
		generator aGenerator
		want      []string
	}{
		"exits non-zero": {
			aGenerator{said: "line1\nline2\nTypeError: no such book\n", err: errors.New("exit status 2")},
			[]string{"npm run gen", "failed", "exit status 2", "TypeError: no such book", "CONFLICT"},
		},
		"leaves a marker": {
			aGenerator{write: map[string]string{"docs/gen.txt": "ch1 a\n<<<<<<< ours\nch2 x\n"}},
			[]string{"docs/gen.txt", "conflict marker", "CONFLICT"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			here := twoStoriesRegenerating(t, "docs/gen.txt")
			generator := tc.generator
			rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "npm run gen"}}}

			_, left, err := mergeRuled(t, here, "mw/two", rig.New(rig.WithRegenerator(generator.run)), rules)
			if !application.Conflicted(err) {
				t.Fatalf("expected a conflict, got %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the refusal to say %q, got %v", want, err)
				}
			}
			if strings.TrimSpace(left) != "" {
				t.Errorf("expected the merge undone, got %q", left)
			}
		})
	}
}

func TestARealShellRunsTheGeneratorInTheLandingWorktree(t *testing.T) {
	here := twoStoriesRegenerating(t, "docs/gen.txt")
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "printf 'made by sh\\n' > docs/gen.txt"}}}

	landed, _, err := mergeRuled(t, here, "mw/two", rig.New(), rules)
	if err != nil {
		t.Fatalf("expected the merge settled, got %v", err)
	}
	if len(landed.Regenerated) != 1 {
		t.Fatalf("regenerated = %v", landed.Regenerated)
	}
	if got := run(t, here, "git", "show", landed.Commit+":docs/gen.txt"); got != "made by sh" {
		t.Errorf("the merge holds %q", got)
	}
}

package rig_test

// A batch's landing worktree is cut bare: a rig's regenerated_files run there
// finds no node_modules and none of the rig's gitignored inputs, so `tsx` was
// "not found" (exit 127) and every conflict in a generated file was handed back
// (mw-gq6.1672). Real git, real sh, a rig that keeps both beside its own tree.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// aNodeRigRegenerating is twoStoriesRegenerating for a rig that has a
// package-lock.json and ignores node_modules and data/private, both of which
// its checkout holds: node_modules/.bin/gen writes the file it is given, and
// data/private/input.txt is what the generator reads.
func aNodeRigRegenerating(t *testing.T) string {
	t.Helper()
	here, _ := aRig(t)
	write(t, here, "package-lock.json", "{}\n")
	write(t, here, ".gitignore", "node_modules/\ndata/private/\n")
	write(t, here, "docs/gen.txt", generatedTable)
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "A node rig")
	run(t, here, "git", "push", "-q", "origin", "main")
	for _, story := range []string{"one", "two"} {
		run(t, here, "git", "checkout", "-q", "-b", "mw/"+story, "main")
		write(t, here, "docs/gen.txt", strings.Replace(generatedTable, "ch2 b", "ch2 from "+story, 1))
		run(t, here, "git", "commit", "-qam", "Story "+story)
	}
	run(t, here, "git", "checkout", "-q", "main")
	if err := landWith(t, rig.New(), here, "mw/one", nil); err != nil {
		t.Fatalf("landing the first story: %v", err)
	}
	run(t, here, "git", "push", "-q", "origin", "mw/one:main")

	write(t, here, "node_modules/.bin/gen", "#!/bin/sh\nprintf '# GENERATED\\nch1 a\\n' > \"$1\"\ncat data/private/input.txt >> \"$1\"\n")
	if err := os.Chmod(filepath.Join(here, "node_modules/.bin/gen"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, here, "data/private/input.txt", "ch2 regenerated\nch3 c\n")
	return here
}

func TestAGeneratorOfANodeRigFindsNodeModulesAndTheRigsIgnoredInputsInTheLanding(t *testing.T) {
	here := aNodeRigRegenerating(t)
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "node_modules/.bin/gen docs/gen.txt"}}}

	landed, left, err := mergeRuled(t, here, "mw/two", rig.New(), rules)
	if err != nil {
		t.Fatalf("expected the merge settled by a generator that finds node_modules and its input, got %v", err)
	}
	if got := landed.Regenerated[0].String(); got != "regenerated docs/gen.txt by node_modules/.bin/gen docs/gen.txt" {
		t.Errorf("the line reads %q", got)
	}
	if got := run(t, here, "git", "show", landed.Commit+":docs/gen.txt"); !strings.Contains(got, "ch2 regenerated") {
		t.Errorf("expected the merge commit to hold what the generator wrote, got %q", got)
	}
	if strings.TrimSpace(left) != "" {
		t.Errorf("expected nothing left uncommitted in the landing, got %q", left)
	}
}

func TestTheLinksTheGeneratorWasGivenAreTakenAwayWhenItEnds(t *testing.T) {
	here := aNodeRigRegenerating(t)
	ctx := context.Background()
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "node_modules/.bin/gen docs/gen.txt"}}}
	worktrees := rig.New()
	run(t, here, "git", "fetch", "-q", "origin")
	dir, err := worktrees.OpenLanding(ctx, here, "origin/main")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worktrees.CloseLanding(ctx, here, dir) }()
	if _, err := worktrees.MergeRuled(ctx, dir, "mw/two", rules); err != nil {
		t.Fatalf("expected the merge settled, got %v", err)
	}
	// CommitLeftovers and KeepLeftovers run git add -A in a landing: a link left
	// behind would be committed into the rig.
	for _, name := range []string{"node_modules", "data/private"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			t.Errorf("expected %s taken away from the landing again", name)
		}
	}
}

func TestAGeneratorWithNoNodeModulesAnywhereFailsAsBeforeAndTheMergeIsUndone(t *testing.T) {
	here := aNodeRigRegenerating(t)
	if err := os.RemoveAll(filepath.Join(here, "node_modules")); err != nil {
		t.Fatal(err)
	}
	// A rig with no lockfile is left alone: no install is tried.
	run(t, here, "git", "fetch", "-q", "origin")
	run(t, here, "git", "reset", "-q", "--hard", "origin/main")
	run(t, here, "git", "rm", "-q", "package-lock.json")
	run(t, here, "git", "commit", "-qm", "No lockfile")
	run(t, here, "git", "push", "-q", "origin", "main")
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "node_modules/.bin/gen docs/gen.txt"}}}

	_, left, err := mergeRuled(t, here, "mw/two", rig.New(), rules)
	if err == nil || !strings.Contains(err.Error(), "CONFLICT (regenerated): docs/gen.txt") {
		t.Fatalf("expected the generator's failure named on a CONFLICT line, got %v", err)
	}
	if strings.TrimSpace(left) != "" {
		t.Errorf("expected the merge undone, got %q", left)
	}
}

// aFakeNpm is an npm that, run as `npm ci`, installs the rig's one binary in the
// directory it is run in, and says so in a file there.
func aFakeNpm(t *testing.T) string {
	t.Helper()
	npm := filepath.Join(t.TempDir(), "npm")
	script := "#!/bin/sh\n[ \"$1\" = ci ] || exit 2\nmkdir -p node_modules/.bin\n" +
		"printf '#!/bin/sh\\nprintf \"# GENERATED\\\\nch1 a\\\\n\" > \"$1\"\\ncat data/private/input.txt >> \"$1\"\\n' > node_modules/.bin/gen\n" +
		"chmod +x node_modules/.bin/gen\n"
	if err := os.WriteFile(npm, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return npm
}

func TestAGeneratorOfANodeRigWhoseCheckoutHasNoNodeModulesGetsNpmCiInTheLanding(t *testing.T) {
	here := aNodeRigRegenerating(t)
	if err := os.RemoveAll(filepath.Join(here, "node_modules")); err != nil {
		t.Fatal(err)
	}
	rules := domain.MergeRules{Regenerated: []domain.RegeneratedFiles{{Paths: []string{"docs/gen.txt"}, Run: "node_modules/.bin/gen docs/gen.txt"}}}

	landed, _, err := mergeRuled(t, here, "mw/two", rig.New(rig.WithNpm(aFakeNpm(t))), rules)
	if err != nil {
		t.Fatalf("expected npm ci to give the landing its node_modules, got %v", err)
	}
	if got := run(t, here, "git", "show", landed.Commit+":docs/gen.txt"); !strings.Contains(got, "ch2 regenerated") {
		t.Errorf("expected the merge commit to hold what the generator wrote, got %q", got)
	}
}

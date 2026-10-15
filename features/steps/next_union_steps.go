package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of a landing that conflicts only in files the rig lists to be
// merged by union (union_merge_files, mw-gq6.424). They hang off nextContext:
// the other host's change is made in c.seed, the story's in its worktree.

// registerNextUnionSteps registers the union-merge steps of features/next.feature.
func registerNextUnionSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's file in the vault lists "([^"]*)" to be merged by union$`, c.theRigListsAUnionFile)
	ctx.Given(`^the other host landed the file "([^"]*)" holding "([^"]*)" on "([^"]*)"$`, c.theOtherHostLandedAFile)
	ctx.Given(`^"([^"]*)" wrote the file "([^"]*)" holding "([^"]*)"$`, c.theStoryWroteAFile)
	ctx.Then(`^the file "([^"]*)" on "([^"]*)" at the rig's origin holds the lines "([^"]*)" and "([^"]*)"$`, c.theOriginFileHoldsBothLines)
}

func (c *nextContext) theRigListsAUnionFile(file string) error {
	return c.theRigFileHoldsTheLine(fmt.Sprintf("%s = [%q]", vault.UnionMergeFilesKey, file))
}

func (c *nextContext) theOtherHostLandedAFile(file, line, branch string) error {
	if err := writeLine(c.seed, file, line); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", "The other host writes " + file}, {"push", "-q", "origin", branch},
	} {
		if err := gitRun(c.seed, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theStoryWroteAFile(id, file, line string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := writeLine(dir, file, line); err != nil {
		return err
	}
	return commitIn(dir, id+" writes "+file)
}

func writeLine(dir, file, line string) error {
	path := filepath.Join(dir, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(line+"\n"), 0o644)
}

func (c *nextContext) theOriginFileHoldsBothLines(file, branch, first, second string) error {
	said, err := gitSay(c.origin(), "show", branch+":"+file)
	if err != nil {
		return err
	}
	for _, want := range []string{first, second} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("expected %s on %s at the origin to hold %q, got %q", file, branch, want, said)
		}
	}
	if strings.Contains(said, "<<<<<<<") {
		return fmt.Errorf("expected no conflict markers in %s on %s, got %q", file, branch, said)
	}
	return nil
}

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

// The steps of the version a landing raises (rigs/<rig>.toml's version_files).
// They hang off nextContext: the rig, its origin and the other host's clone are
// the ones mwClosesOut lands against, and the rig's file is read from the vault
// it hands to mw next.

// registerNextVersionSteps registers the version steps of features/next.feature.
func registerNextVersionSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's main holds a package\.json and a package-lock\.json at version "([^"]*)"$`, c.theRigsMainHoldsPackages)
	ctx.Given(`^the rig's file in the vault names the version files "([^"]*)" and "([^"]*)"$`, c.theRigNamesVersionFiles)
	ctx.Given(`^the work of "([^"]*)" is rebased onto that main$`, c.theWorkIsRebasedOntoMain)
	ctx.Given(`^the story "([^"]*)" raised the version to "([^"]*)" itself$`, c.theStoryRaisedTheVersion)
	ctx.Given(`^the other host lands a story and its version bump the moment mw first tries to push$`, c.theOtherHostRacesWithABump)

	ctx.Then(`^"([^"]*)" at the rig's origin holds version "([^"]*)" in "([^"]*)" and "([^"]*)"$`, c.theOriginHoldsVersion)
	ctx.Then(`^the tip of "([^"]*)" at the rig's origin is the commit "([^"]*)"$`, c.theTipIsTheCommit)
	ctx.Then(`^"([^"]*)" on "([^"]*)" differs from its old self only in its two version fields$`, c.theLockDiffersOnlyInVersions)
	ctx.Then(`^no commit on "([^"]*)" at the rig's origin says "([^"]*)" except the story's own$`, c.noVersionCommitOn)
	ctx.Then(`^"([^"]*)" at the rig's origin has exactly (\d+) commits saying "([^"]*)"$`, c.theOriginHasCommitsSaying)
	ctx.Then(`^that mail's body does not say "([^"]*)"$`, c.thatMailsBodyDoesNotSay)
}

// packageJSON and packageLock are the two files as npm writes them: two-space
// JSON, a trailing newline, a dependency whose version must never be touched.
func packageJSON(version string) string {
	return "{\n  \"name\": \"fixture\",\n  \"version\": \"" + version + "\",\n  \"dependencies\": {\n    \"left-pad\": \"^1.3.0\"\n  }\n}\n"
}

func packageLock(version string) string {
	return "{\n  \"name\": \"fixture\",\n  \"version\": \"" + version + "\",\n  \"lockfileVersion\": 3,\n  \"requires\": true,\n  \"packages\": {\n" +
		"    \"\": {\n      \"name\": \"fixture\",\n      \"version\": \"" + version + "\",\n      \"dependencies\": {\n        \"left-pad\": \"^1.3.0\"\n      }\n    },\n" +
		"    \"node_modules/left-pad\": {\n      \"version\": \"1.3.0\"\n    }\n  }\n}\n"
}

// commitIn commits whatever is changed in dir under message.
func commitIn(dir, message string) error {
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", message}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theRigsMainHoldsPackages(version string) error {
	for file, text := range map[string]string{"package.json": packageJSON(version), "package-lock.json": packageLock(version)} {
		if err := os.WriteFile(filepath.Join(c.seed, file), []byte(text), 0o644); err != nil {
			return err
		}
	}
	if err := commitIn(c.seed, "The rig opens at version "+version); err != nil {
		return err
	}
	return gitRun(c.seed, "git", "push", "-q", "origin", "main")
}

func (c *nextContext) theRigNamesVersionFiles(first, second string) error {
	dir := filepath.Join(c.vault, application.RigsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line := fmt.Sprintf("version_files = [%q, %q]\n", first, second)
	return os.WriteFile(filepath.Join(dir, c.rigKey()+vault.RigFileExt), []byte(line), 0o644)
}

// theWorkIsRebasedOntoMain moves the story's branch onto the main the origin now
// has, so that it lands as a fast-forward and the files are the story's base.
func (c *nextContext) theWorkIsRebasedOntoMain(id string) error {
	dir := application.WorktreeDir(c.rig, id)
	for _, args := range [][]string{{"fetch", "-q", "origin"}, {"rebase", "-q", "origin/main"}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *nextContext) theStoryRaisedTheVersion(id, version string) error {
	dir := application.WorktreeDir(c.rig, id)
	for file, text := range map[string]string{"package.json": packageJSON(version), "package-lock.json": packageLock(version)} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return commitIn(dir, "Version "+version+" ("+id+")")
}

// theOtherHostRacesWithABump is a racer that lands a story of its own with the
// version bump a landing makes, so that mw's first push is refused and its next
// landing starts from a base that already holds 0.1.1.
func (c *nextContext) theOtherHostRacesWithABump() error {
	script := fmt.Sprintf(`#!/bin/sh
cd %s || exit 1
printf 'the other host was here\n' > %s
git add -A
git commit -qm "The other host lands its own work"
sed -i 's/"version": "0.1.0"/"version": "0.1.1"/' package.json package-lock.json
git add -A
git commit -qm "Version 0.1.1 (mw-other.1)"
git push -q origin main
`, c.seed, nextOtherFile)
	return os.WriteFile(c.racer, []byte(script), 0o755)
}

func (c *nextContext) theOriginHoldsVersion(branch, version, first, second string) error {
	for _, file := range []string{first, second} {
		text, err := gitShow(c.origin(), branch, file)
		if err != nil {
			return err
		}
		if want := `"version": "` + version + `"`; !strings.Contains(text, want) || strings.Contains(text, "node_modules/left-pad") && !strings.Contains(text, `"version": "1.3.0"`) {
			return fmt.Errorf("expected %s on %s to hold %s and, in a lock file, the dependency's 1.3.0, got:\n%s", file, branch, want, text)
		}
		for _, other := range []string{"0.1.0", "0.1.1", "0.1.2", "0.2.0", "0.2.1"} {
			if other != version && strings.Contains(text, `"version": "`+other+`"`) {
				return fmt.Errorf("expected %s on %s to hold only version %s, but it holds %s:\n%s", file, branch, version, other, text)
			}
		}
	}
	return nil
}

// gitShow reads a file as a branch of the origin has it, verbatim: gitSay trims.
func gitShow(origin, branch, file string) (string, error) {
	out, err := gitSay(origin, "show", branch+":"+file)
	return out + "\n", err
}

func (c *nextContext) theTipIsTheCommit(branch, subject string) error {
	got, err := gitSay(c.origin(), "log", "-1", "--format=%s", branch)
	if err != nil {
		return err
	}
	if got != subject {
		return fmt.Errorf("expected the tip of %s to be %q, got %q", branch, subject, got)
	}
	return nil
}

func (c *nextContext) theLockDiffersOnlyInVersions(file, branch string) error {
	got, err := gitShow(c.origin(), branch, file)
	if err != nil {
		return err
	}
	was, err := gitShow(c.origin(), branch+"~1", file)
	if err != nil {
		return err
	}
	if want := strings.Replace(was, `"version": "0.1.0"`, `"version": "0.1.1"`, 2); got != want {
		return fmt.Errorf("expected %s to differ from the old one only in its two version fields, got:\n%s\nwant:\n%s", file, got, want)
	}
	return nil
}

func (c *nextContext) noVersionCommitOn(branch, word string) error {
	subjects, err := gitSay(c.origin(), "log", "--format=%s", branch)
	if err != nil {
		return err
	}
	for _, subject := range strings.Split(subjects, "\n") {
		// The story's own commit, which raised the minor, is not mw's bump.
		if strings.HasPrefix(subject, word) && !strings.HasPrefix(subject, "Version 0.2.0 (") {
			return fmt.Errorf("expected no %q commit on %s, found %q", word, branch, subject)
		}
	}
	return nil
}

func (c *nextContext) theOriginHasCommitsSaying(branch string, want int, subject string) error {
	subjects, err := gitSay(c.origin(), "log", "--format=%s", branch)
	if err != nil {
		return err
	}
	got := 0
	for _, line := range strings.Split(subjects, "\n") {
		if line == subject {
			got++
		}
	}
	if got != want {
		return fmt.Errorf("expected %d commit(s) saying %q on %s, found %d in:\n%s", want, subject, branch, got, subjects)
	}
	return nil
}

func (c *nextContext) thatMailsBodyDoesNotSay(text string) error {
	sent, err := c.mailSent()
	if err != nil {
		return err
	}
	for _, message := range sent {
		if strings.Contains(message.Body, text) {
			return fmt.Errorf("expected the mail not to say %q, got:\n%s", text, message.Body)
		}
	}
	return nil
}

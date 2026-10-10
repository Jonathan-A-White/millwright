package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of the changelog a landing writes (rigs/<rig>.toml's
// changelog_files). They hang off nextContext, beside the version steps: the
// entry goes in the commit that raises the version.

// registerNextChangelogSteps registers the changelog steps of features/next.feature.
func registerNextChangelogSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's file in the vault also names the changelog files "([^"]*)" and "([^"]*)"$`, c.theRigNamesChangelogFiles)
	ctx.Given(`^the rig's main already holds a changelog with the entry "([^"]*)" saying "([^"]*)"$`, c.theRigsMainHoldsAChangelog)
	ctx.Given(`^the story "([^"]*)" seeded a changelog entry "([^"]*)" saying "([^"]*)"$`, c.theStorySeededAnEntry)
	ctx.Given(`^the story "([^"]*)" added the version files at "([^"]*)"$`, c.theStoryAddedVersionFiles)
	ctx.Given(`^the story "([^"]*)" is titled "([^"]*)"$`, c.theStoryIsTitled)
	ctx.Given(`^the story "([^"]*)" is labelled "([^"]*)"$`, c.theStoryIsLabelled)

	ctx.Then(`^the tip of "([^"]*)" at the rig's origin changed "([^"]*)" and "([^"]*)"$`, c.theTipChanged)
	ctx.Then(`^the tip of "([^"]*)" at the rig's origin changed no changelog file$`, c.theTipChangedNoChangelog)
	ctx.Then(`^"([^"]*)" at the top of "([^"]*)" at the rig's origin is the entry "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)", "([^"]*)"$`, c.theTopEntryIs)
	ctx.Then(`^"([^"]*)" at the rig's origin lists the versions "([^"]*)" then "([^"]*)"$`, c.theChangelogListsVersions)
	ctx.Then(`^"([^"]*)" at the rig's origin lists exactly (\d+) entry$`, c.theChangelogListsEntries)
	ctx.Then(`^"([^"]*)" on "([^"]*)" at the rig's origin reads:$`, c.theFileReads)
	ctx.Then(`^"([^"]*)" and "([^"]*)" are absent from "([^"]*)" at the rig's origin$`, c.theFilesAreAbsent)
}

func (c *nextContext) theRigNamesChangelogFiles(first, second string) error {
	path := filepath.Join(c.vault, application.RigsDir, c.rigKey()+vault.RigFileExt)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "changelog_files = [%q, %q]\n", first, second)
	return err
}

func (c *nextContext) theRigsMainHoldsAChangelog(version, text string) error {
	if err := os.MkdirAll(filepath.Join(c.seed, "public"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"public/changelog.json": fmt.Sprintf("[\n  {\n    \"version\": %q,\n    \"date\": \"2026-09-01\",\n    \"story\": \"mw-old.1\",\n    \"kind\": \"new\",\n    \"text\": %q\n  }\n]\n", version, text),
		"CHANGELOG.md":          fmt.Sprintf("# What's new\n\n## %s\n_2026-09-01_\n- New: %s\n", version, text),
	}
	for file, body := range files {
		if err := os.WriteFile(filepath.Join(c.seed, filepath.FromSlash(file)), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := commitIn(c.seed, "The rig keeps a changelog"); err != nil {
		return err
	}
	return gitRun(c.seed, "git", "push", "-q", "origin", "main")
}

func (c *nextContext) theStoryIsTitled(id, title string) error {
	return c.tracker.SetTitle(id, title)
}

func (c *nextContext) theStoryIsLabelled(id, label string) error {
	detail, err := c.tracker.ShowStory(context.Background(), id)
	if err != nil {
		return err
	}
	return c.tracker.SetLabels(id, append(detail.Labels, label)...)
}

func (c *nextContext) filesChangedByTip(branch string) ([]string, error) {
	out, err := gitSay(c.origin(), "show", "--name-only", "--format=", branch)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

func (c *nextContext) theTipChanged(branch, first, second string) error {
	changed, err := c.filesChangedByTip(branch)
	if err != nil {
		return err
	}
	for _, want := range []string{first, second} {
		found := false
		for _, got := range changed {
			found = found || got == want
		}
		if !found {
			return fmt.Errorf("expected the tip of %s to change %s, it changed %v", branch, want, changed)
		}
	}
	return nil
}

func (c *nextContext) theTipChangedNoChangelog(branch string) error {
	changed, err := c.filesChangedByTip(branch)
	if err != nil {
		return err
	}
	for _, got := range changed {
		if got == "public/changelog.json" || got == "CHANGELOG.md" {
			return fmt.Errorf("expected the tip of %s to change no changelog file, it changed %v", branch, changed)
		}
	}
	return nil
}

type changelogEntry struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Story   string `json:"story"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
}

func (c *nextContext) changelogAt(file, branch string) ([]changelogEntry, error) {
	text, err := gitShow(c.origin(), branch, file)
	if err != nil {
		return nil, err
	}
	var entries []changelogEntry
	if err := json.Unmarshal([]byte(text), &entries); err != nil {
		return nil, fmt.Errorf("%s on %s is not a JSON array of entries: %w\n%s", file, branch, err, text)
	}
	return entries, nil
}

func (c *nextContext) theTopEntryIs(file, branch, version, date, story, kind, text string) error {
	entries, err := c.changelogAt(file, branch)
	if err != nil {
		return err
	}
	want := changelogEntry{Version: version, Date: date, Story: story, Kind: kind, Text: text}
	if len(entries) == 0 || entries[0] != want {
		return fmt.Errorf("expected the first entry of %s on %s to be %+v, got %+v", file, branch, want, entries)
	}
	return nil
}

func (c *nextContext) theChangelogListsVersions(file, first, second string) error {
	entries, err := c.changelogAt(file, "main")
	if err != nil {
		return err
	}
	if len(entries) != 2 || entries[0].Version != first || entries[1].Version != second {
		return fmt.Errorf("expected %s to list %s then %s, got %+v", file, first, second, entries)
	}
	return nil
}

func (c *nextContext) theFileReads(file, branch string, want *godog.DocString) error {
	got, err := gitShow(c.origin(), branch, file)
	if err != nil {
		return err
	}
	if wanted := strings.TrimSpace(want.Content) + "\n"; got != wanted {
		return fmt.Errorf("expected %s on %s to read:\n%s\ngot:\n%s", file, branch, wanted, got)
	}
	return nil
}

func (c *nextContext) theFilesAreAbsent(first, second, branch string) error {
	for _, file := range []string{first, second} {
		if text, err := gitShow(c.origin(), branch, file); err == nil {
			return fmt.Errorf("expected %s to be absent from %s, it holds:\n%s", file, branch, text)
		}
	}
	return nil
}

// theStorySeededAnEntry is a Builder who wrote its own story's note into both
// changelog files, in its branch, under a version of its choosing.
func (c *nextContext) theStorySeededAnEntry(id, version, text string) error {
	dir := application.WorktreeDir(c.rig, id)
	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"public/changelog.json": fmt.Sprintf("[\n  {\n    \"version\": %q,\n    \"date\": \"2026-09-17\",\n    \"story\": %q,\n    \"kind\": \"new\",\n    \"text\": %q\n  }\n]\n", version, id, text),
		"CHANGELOG.md":          fmt.Sprintf("# What's new\n\n## %s\n_2026-09-17_\n- New: %s\n", version, text),
	}
	for file, body := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(file)), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return commitIn(dir, "Seed the changelog ("+id+")")
}

// theStoryAddedVersionFiles is a rig's first landing: the version files are
// not on main at all, and the story's branch brings them.
func (c *nextContext) theStoryAddedVersionFiles(id, version string) error {
	dir := application.WorktreeDir(c.rig, id)
	for file, text := range map[string]string{"package.json": packageJSON(version), "package-lock.json": packageLock(version)} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return commitIn(dir, "Add the package files ("+id+")")
}

func (c *nextContext) theChangelogListsEntries(file string, want int) error {
	entries, err := c.changelogAt(file, "main")
	if err != nil {
		return err
	}
	if len(entries) != want {
		return fmt.Errorf("expected %s to hold %d entry, got %d: %+v", file, want, len(entries), entries)
	}
	return nil
}

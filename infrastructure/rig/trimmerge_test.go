package rig_test

// Every file mw merged by union — CLAUDE.md by UnionNotes' rule, and what a
// rig lists in union_merge_files — is merged by mw's trim-safe driver instead,
// which mw carries itself: union keeps both sides of a conflicting hunk, so a
// story that trims lines got them back whenever main had appended meanwhile
// (mw-gq6.499). These rigs are lampas-like: no driver script in the checkout.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

const (
	notesBase    = "# notes\n\nkept note\nold note one\nold note two\nold note three\n"
	notesTrimmed = "# notes\n\nkept note\n"
	notesAdded   = "main's new note"
)

// aTrimAgainstAnAppend commits file as notesBase on main, cuts mw/trim from
// it taking the three old notes out, then lands on main a line appended to it:
// the two sides change neighbouring lines, so git's own merge conflicts.
func aTrimAgainstAnAppend(t *testing.T, worktrees *rig.Worktrees, file string) string {
	t.Helper()
	here, _ := aRig(t)
	if _, err := os.Stat(filepath.Join(here, "scripts")); err == nil {
		t.Fatal("expected a rig with no scripts of its own")
	}
	write(t, here, file, notesBase)
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", file+" opens")
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/trim", "main")
	write(t, here, file, notesTrimmed)
	run(t, here, "git", "commit", "-qam", "Trim "+file)
	run(t, here, "git", "checkout", "-q", "main")
	aBranchAppending(t, here, "mw/add", file, notesAdded)
	if err := landWith(t, worktrees, here, "mw/add", nil); err != nil {
		t.Fatalf("landing the appended line: %v", err)
	}
	return here
}

// landWith is landUnion with worktrees, its error alone, the landing pushed.
func landWith(t *testing.T, worktrees *rig.Worktrees, here, branch string, union []string) error {
	t.Helper()
	_, _, err := landUnionWith(t, worktrees, here, branch, union)
	return err
}

// landUnionWith is landUnion by the given worktrees.
func landUnionWith(t *testing.T, worktrees *rig.Worktrees, here, branch string, union []string) (application.Landed, string, error) {
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
	landed, err := worktrees.MergeUnion(ctx, dir, branch, union)
	left := run(t, dir, "git", "status", "--porcelain")
	if err != nil {
		return landed, left, err
	}
	return landed, left, worktrees.Push(ctx, dir, "origin", "main")
}

// TestATrimLandsWithMainsAppendedLineAndWithoutTheTrimmedOnes: CLAUDE.md (by
// UnionNotes' rule) and docs/overview.md (listed, as lampas lists it, with no
// attribute at all) each land the story's trim and main's appended line, and
// none of the three lines the trim took out; no git config names the driver.
func TestATrimLandsWithMainsAppendedLineAndWithoutTheTrimmedOnes(t *testing.T) {
	for _, tc := range []struct {
		file    string
		unioned []string
	}{
		{"CLAUDE.md", nil}, // git's merge settles it by the driver UnionNotes names
		{"docs/overview.md", []string{"docs/overview.md"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			worktrees := rig.New()
			here := aTrimAgainstAnAppend(t, worktrees, tc.file)

			landed, left, err := landUnionWith(t, worktrees, here, "mw/trim", []string{"CLAUDE.md", "docs/*.md"})
			if err != nil {
				t.Fatalf("landing the trim: %v (left %q)", err, left)
			}
			if !slices.Equal(landed.Unioned, tc.unioned) {
				t.Errorf("expected Unioned %q, got %q", tc.unioned, landed.Unioned)
			}
			run(t, here, "git", "fetch", "-q", "origin")
			got := run(t, here, "git", "show", "origin/main:"+tc.file) + "\n"
			if want := notesTrimmed + notesAdded + "\n"; got != want {
				t.Errorf("expected the trim with main's line, got:\n%s", got)
			}
			for _, gone := range []string{"old note one", "old note two", "old note three"} {
				if strings.Contains(got, gone) {
					t.Errorf("expected %q to stay trimmed, got:\n%s", gone, got)
				}
			}
			if said, err := exec.Command("git", "-C", here, "config", "--get-regexp", `^merge\.`).CombinedOutput(); err == nil {
				t.Errorf("expected no merge driver in any git config, got %s", said)
			}
		})
	}
}

// infoAttributes is the path of the info/attributes the rig at dir shares
// with all its worktrees.
func infoAttributes(t *testing.T, dir string) string {
	t.Helper()
	path := strings.TrimSpace(run(t, dir, "git", "rev-parse", "--git-path", "info/attributes"))
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return path
}

// TestUnionNotesRewritesTheUnionRuleIntoTheTrimSafeOne: a rig set up by an
// older mw holds "CLAUDE.md merge=union"; UnionNotes leaves one line naming
// the trim-safe driver in its place, and every other line as it was.
func TestUnionNotesRewritesTheUnionRuleIntoTheTrimSafeOne(t *testing.T) {
	for name, before := range map[string]string{
		"none":         "",
		"union":        "*.png binary\nCLAUDE.md merge=union\n",
		"both":         "CLAUDE.md merge=union\n*.png binary\nCLAUDE.md merge=trimsafe\n",
		"already safe": "*.png binary\nCLAUDE.md merge=trimsafe",
	} {
		t.Run(name, func(t *testing.T) {
			here, _ := aRig(t)
			path := infoAttributes(t, here)
			if before != "" {
				write(t, filepath.Dir(path), filepath.Base(path), before)
			}
			worktrees := rig.New()
			for range 2 {
				if err := worktrees.UnionNotes(context.Background(), here); err != nil {
					t.Fatalf("UnionNotes: %v", err)
				}
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(body), "CLAUDE.md merge=trimsafe\n"); n != 1 {
				t.Errorf("expected the trim-safe rule exactly once, found %d in:\n%s", n, body)
			}
			if strings.Contains(string(body), "merge=union") {
				t.Errorf("expected no union rule left, got:\n%s", body)
			}
			if strings.Contains(before, "*.png") && !strings.Contains(string(body), "*.png binary\n") {
				t.Errorf("expected the other line kept, got:\n%s", body)
			}
			if attr := run(t, here, "git", "check-attr", "merge", "--", "CLAUDE.md"); strings.TrimSpace(attr) != "CLAUDE.md: merge: trimsafe" {
				t.Errorf("expected git to read CLAUDE.md as merge=trimsafe, got %q", attr)
			}
		})
	}
}

// TestATrimTheDriverCannotSettleIsSentBackNotMergedByUnion: with a driver that
// fails, the trim is a conflict sent back that names the driver, for CLAUDE.md
// and a listed file alike, never union's both sides of the hunk.
func TestATrimTheDriverCannotSettleIsSentBackNotMergedByUnion(t *testing.T) {
	for _, file := range []string{"CLAUDE.md", "docs/overview.md"} {
		t.Run(file, func(t *testing.T) {
			here := aTrimAgainstAnAppend(t, rig.New(), file)
			failing := rig.New(rig.WithMergeScript("#!/bin/sh\nexit 1\n"))

			landed, left, err := landUnionWith(t, failing, here, "mw/trim", []string{"CLAUDE.md", "docs/*.md"})
			if !application.Conflicted(err) {
				t.Fatalf("expected a conflict, got %v", err)
			}
			if !strings.Contains(err.Error(), "trim-safe driver, merge=trimsafe") {
				t.Errorf("expected the refusal to name the trim-safe driver, got %v", err)
			}
			if len(landed.Unioned) != 0 || left != "" {
				t.Errorf("expected nothing merged and the landing left clean, got %q and %q", landed.Unioned, left)
			}
			run(t, here, "git", "fetch", "-q", "origin")
			if got := run(t, here, "git", "show", "origin/main:"+file) + "\n"; got != notesBase+notesAdded+"\n" {
				t.Errorf("expected main as it was, got:\n%s", got)
			}
		})
	}
}

// runTrimDriver runs mw's trim-safe driver on the three versions as git would
// (base, ours, theirs written to files, ours overwritten), and returns what it
// left in ours and its exit status.
func runTrimDriver(t *testing.T, base, ours, theirs string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "driver.sh", millwright.TrimMergeScript)
	write(t, dir, "base", base)
	write(t, dir, "ours", ours)
	write(t, dir, "theirs", theirs)
	cmd := exec.Command("sh", "driver.sh", "base", "ours", "theirs")
	cmd.Dir = dir
	status := 0
	if out, err := cmd.CombinedOutput(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running the driver: %v\n%s", err, out)
		}
		status = exit.ExitCode()
	}
	left, err := os.ReadFile(filepath.Join(dir, "ours"))
	if err != nil {
		t.Fatal(err)
	}
	return string(left), status
}

// TestALineBothSidesEditedIsAConflictNotKeptTwice: the base has a prose line P
// in a hunk git cannot merge; ours replaces it with P1 and theirs with P2. The
// driver keeping both made two contradicting versions of one paragraph (Lampas
// ATTRIBUTION.md, batch 292), so it refuses, and writes neither into ours.
func TestALineBothSidesEditedIsAConflictNotKeptTwice(t *testing.T) {
	const (
		p  = "The text is public domain, edited from the 2000 transcription."
		p1 = "The text is public domain, edited from the 2000 transcription by ours."
		p2 = "The text is public domain, edited from the 2001 transcription by theirs."
	)
	frame := func(line string) string { return "# About\n\nintro\n\n" + line + "\n\nend\n" }
	got, status := runTrimDriver(t, frame(p), frame(p1), frame(p2))
	if status == 0 {
		t.Errorf("expected a conflict (non-zero exit), the driver settled it as:\n%s", got)
	}
	if strings.Contains(got, p1) && strings.Contains(got, p2) {
		t.Errorf("expected the two edits of one line not both written, got:\n%s", got)
	}
}

// TestALineBothSidesAddedAtTwoPlacesIsAConflict: the same new line added at
// two spots, which git merges cleanly into two copies of it, is refused.
func TestALineBothSidesAddedAtTwoPlacesIsAConflict(t *testing.T) {
	const toc = "<!-- toc: none -->"
	base := "# About\n\nintro\n\nmiddle\n\nlast one\nold tail\n"
	ours := "# About\n\n" + toc + "\n\nintro\n\nmiddle\n\nlast one\nold tail\n"
	theirs := "# About\n\nintro\n\nmiddle\n\n" + toc + "\n\nlast one\nnew tail\n"
	got, status := runTrimDriver(t, base, ours, theirs)
	if status == 0 {
		t.Errorf("expected a conflict, the driver settled it as:\n%s", got)
	}
}

// TestWhatOneSideAloneChangedStillMerges: lines only one side added, a row both
// sides edited (ours kept) and a line both sides added alike stay settled.
func TestWhatOneSideAloneChangedStillMerges(t *testing.T) {
	base := "# t\n\n| Port | Cell |\n| --- | --- |\n| `A` | one |\n| `B` | two |\n"
	ours := base + "| `C` | ours |\n"
	theirs := "# t\n\n| Port | Cell |\n| --- | --- |\n| `A` | one |\n| `B` | two |\n| `D` | theirs |\n"
	got, status := runTrimDriver(t, base, ours, theirs)
	if status != 0 || !strings.Contains(got, "| `C` | ours |\n") || !strings.Contains(got, "| `D` | theirs |\n") {
		t.Errorf("expected both rows added by one side each, exit 0, got %d:\n%s", status, got)
	}
	sameAdded := "# t\n\nintro\n\nsame new line\n"
	got, status = runTrimDriver(t, "# t\n\nintro\n", sameAdded, sameAdded)
	if status != 0 || got != sameAdded {
		t.Errorf("expected a line both sides added alike once, exit 0, got %d:\n%s", status, got)
	}
}

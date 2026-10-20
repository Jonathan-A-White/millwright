package rig_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestLandingOfFindsTheCommitMwNextMadeForAStory(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "a.txt", "a")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Version 0.1.86 (mw-x.1)")
	want := run(t, here, "git", "rev-parse", "HEAD")
	write(t, here, "b.txt", "b")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "What's new 0.2.0 (mw-x.22)")
	wantOwn := run(t, here, "git", "rev-parse", "HEAD")
	write(t, here, "c.txt", "c")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Talks about Version 9.9.9 (mw-x.3) mid-line")

	w := rig.New()
	got, ok, err := w.LandingOf(context.Background(), here, "main", "mw-x.1")
	if err != nil || !ok || got.Version != "0.1.86" || got.Commit != want {
		t.Errorf("LandingOf mw-x.1 = %+v, %v, %v; want 0.1.86 at %s", got, ok, err, want)
	}
	got, ok, err = w.LandingOf(context.Background(), here, "main", "mw-x.22")
	if err != nil || !ok || got.Version != "0.2.0" || got.Commit != wantOwn {
		t.Errorf("LandingOf mw-x.22 = %+v, %v, %v; want 0.2.0 at %s", got, ok, err, wantOwn)
	}
	// "." in a bead id is no wildcard, and a subject must be only the commit's own.
	for _, story := range []string{"mw-x.3", "mw-xx1", "mw-x.2"} {
		if got, ok, err := w.LandingOf(context.Background(), here, "main", story); err != nil || ok {
			t.Errorf("LandingOf %s = %+v, %v, %v; want none", story, got, ok, err)
		}
	}
}

func TestLandingOfFindsTheVersionCommitOfABatch(t *testing.T) {
	here, _ := aRig(t)
	write(t, here, "a.txt", "a")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Version 0.1.373 (batch 128 of lampas)")
	want := run(t, here, "git", "rev-parse", "HEAD")

	w := rig.New()
	got, ok, err := w.LandingOf(context.Background(), here, "main", "batch 128 of lampas")
	if err != nil || !ok || got.Version != "0.1.373" || got.Commit != want {
		t.Errorf("LandingOf batch 128 = %+v, %v, %v; want 0.1.373 at %s", got, ok, err, want)
	}
	for _, label := range []string{"batch 12 of lampas", "batch 128 of cairn"} {
		if got, ok, err := w.LandingOf(context.Background(), here, "main", label); err != nil || ok {
			t.Errorf("LandingOf %s = %+v, %v, %v; want none", label, got, ok, err)
		}
	}
}

func TestLandingOfOnABranchTheRigLacksIsNoLanding(t *testing.T) {
	here, _ := aRig(t)
	if got, ok, err := rig.New().LandingOf(context.Background(), here, "nope", "mw-x.1"); err != nil || ok {
		t.Errorf("LandingOf on a missing branch = %+v, %v, %v; want none and no error", got, ok, err)
	}
}

func TestStoryCommitOfFindsTheCommitOfAStoryInARigWithNoVersion(t *testing.T) {
	here, _ := aRig(t)
	commit := func(file, subject string) string {
		write(t, here, file, file)
		run(t, here, "git", "add", "-A")
		run(t, here, "git", "commit", "-qm", subject)
		return run(t, here, "git", "rev-parse", "HEAD")
	}
	commit("a.txt", "Do the first thing (mw-x.1)")
	wantNewest := commit("b.txt", "Do the rest of the first thing (mw-x.1)")
	commit("c.txt", "Mentions (mw-x.3) mid-line, and ends otherwise")
	run(t, here, "git", "checkout", "-q", "-b", "mw/mw-x.2")
	commit("d.txt", "Work with no story id in its subject")
	run(t, here, "git", "checkout", "-q", "main")
	commit("e.txt", "Main moves on")
	run(t, here, "git", "merge", "-q", "--no-ff", "-m", "Merge branch 'mw/mw-x.2' into HEAD", "mw/mw-x.2")
	wantMerge := run(t, here, "git", "rev-parse", "HEAD")

	w := rig.New()
	got, ok, err := w.StoryCommitOf(context.Background(), here, "main", "mw-x.1")
	if err != nil || !ok || got != wantNewest {
		t.Errorf("StoryCommitOf mw-x.1 = %q, %v, %v; want its newest commit %s", got, ok, err, wantNewest)
	}
	got, ok, err = w.StoryCommitOf(context.Background(), here, "main", "mw-x.2")
	if err != nil || !ok || got != wantMerge {
		t.Errorf("StoryCommitOf mw-x.2 = %q, %v, %v; want its merge %s", got, ok, err, wantMerge)
	}
	// "." in a bead id is no wildcard, and a subject must end with the story.
	for _, story := range []string{"mw-x.3", "mw-xx1", "mw-x.", "mw-x.22"} {
		if got, ok, err := w.StoryCommitOf(context.Background(), here, "main", story); err != nil || ok {
			t.Errorf("StoryCommitOf %s = %q, %v, %v; want none", story, got, ok, err)
		}
	}
	if got, ok, err := w.StoryCommitOf(context.Background(), here, "nope", "mw-x.1"); err != nil || ok {
		t.Errorf("StoryCommitOf on a missing branch = %q, %v, %v; want none and no error", got, ok, err)
	}
}

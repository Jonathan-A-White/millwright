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

func TestLandingOfOnABranchTheRigLacksIsNoLanding(t *testing.T) {
	here, _ := aRig(t)
	if got, ok, err := rig.New().LandingOf(context.Background(), here, "nope", "mw-x.1"); err != nil || ok {
		t.Errorf("LandingOf on a missing branch = %+v, %v, %v; want none and no error", got, ok, err)
	}
}

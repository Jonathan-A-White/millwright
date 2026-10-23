package rig_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestDiffLinesAreTheLinesTheBranchAddsAndRemovesInFilesOfThatNameAtAnyDepth(t *testing.T) {
	here := aRigWithAServer(t)
	write(t, here, "package-lock.json", "a\nold-ssh git+ssh://x\nb\n")
	write(t, here, "web/package-lock.json", "w\n")
	write(t, here, "notes.txt", "unrelated\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Lockfiles")
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	write(t, here, "package-lock.json", "a\nb\nnew-ssh git+ssh://y\n")
	write(t, here, "web/package-lock.json", "w\nx\n")
	write(t, here, "notes.txt", "unrelated\nchanged\n")
	write(t, here, "deep/er/package-lock.json", "added git+ssh://z\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The story's change")

	got, err := rig.New().DiffLines(context.Background(), here, "main", "mw/story", "package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	want := []application.DiffLine{
		{Path: "deep/er/package-lock.json", Text: "added git+ssh://z", Added: true},
		{Path: "package-lock.json", Text: "old-ssh git+ssh://x"},
		{Path: "package-lock.json", Text: "new-ssh git+ssh://y", Added: true},
		{Path: "web/package-lock.json", Text: "x", Added: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestADiffOfABranchThatIsNotThereIsEmpty(t *testing.T) {
	here := aRigWithAServer(t)
	got, err := rig.New().DiffLines(context.Background(), here, "main", "mw/worked-elsewhere", "package-lock.json")
	if err != nil || len(got) != 0 {
		t.Errorf("expected no lines and no error, got %+v, %v", got, err)
	}
}

package rig_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

func TestAddedLinesAreOnlyTheLinesTheBranchAddsEachWithItsNumberInTheFile(t *testing.T) {
	here := aRigWithAServer(t)
	write(t, here, "a.go", "one\ntwo\nthree\nfour\n")
	write(t, here, "gone.txt", "bye\n")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "Base")
	run(t, here, "git", "push", "-q", "origin", "main")
	run(t, here, "git", "checkout", "-q", "-b", "mw/story")
	// Line 2 is replaced, a line is added after line 3, one removed file, one new file.
	write(t, here, "a.go", "one\nTWO\nthree\nnew-after-three\nfour\n")
	write(t, here, "b/new.go", "first\nsecond\n")
	run(t, here, "git", "rm", "-q", "gone.txt")
	run(t, here, "git", "add", "-A")
	run(t, here, "git", "commit", "-qm", "The story's change")

	got, err := rig.New().AddedLines(context.Background(), here, "main", "mw/story")
	if err != nil {
		t.Fatal(err)
	}
	want := []application.AddedLine{
		{Path: "a.go", Number: 2, Text: "TWO"},
		{Path: "a.go", Number: 4, Text: "new-after-three"},
		{Path: "b/new.go", Number: 1, Text: "first"},
		{Path: "b/new.go", Number: 2, Text: "second"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %+v, got %+v", want, got)
	}
}

func TestAddedLinesOfABranchThatIsNotThereAreNone(t *testing.T) {
	here := aRigWithAServer(t)
	got, err := rig.New().AddedLines(context.Background(), here, "main", "mw/worked-elsewhere")
	if err != nil || len(got) != 0 {
		t.Errorf("expected no lines and no error, got %+v, %v", got, err)
	}
}

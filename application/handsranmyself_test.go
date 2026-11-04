package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// countingView is a live-view publisher that counts its runs.
type countingView struct{ runs int }

func (v *countingView) Run(context.Context) (application.PosternViewDoc, error) {
	v.runs++
	return application.PosternViewDoc{}, nil
}

func (f *runFixture) say(t *testing.T, txid, bead, text string) {
	t.Helper()
	threaded, err := json.Marshal(application.PosternThreadedMessage{Thread: application.PosternThread{Bead: bead}, Text: text})
	mustDo(t, err)
	f.message(t, releaseTapGovernorKey, txid, string(threaded))
}

// The Governor ran a hands step himself and said so in the bead's thread
// (mw-6ww.140, 2026-10-24): the step is retired, the live view is published and
// the bead carries a comment, rather than the step staying 'not run'.
func TestTheGovernorsWordThatHeRanAStepHimselfRetiresIt(t *testing.T) {
	ctx := context.Background()
	f := newRunFixture(t)
	f.say(t, "tx-ran", "mw-e.3", "I ran step echo myself.")
	view := &countingView{}
	inbox := f.inbox()
	inbox.View = view

	if _, err := inbox.Apply(ctx); err != nil {
		t.Fatalf("applying: %v", err)
	}

	note, _ := f.tracker.Note(ctx, application.HandsRetiredKey("mw-e.3", "echo"))
	if note != "run by the Governor himself" {
		t.Fatalf("expected the step retired as run by the Governor himself, got %q", note)
	}
	if view.runs != 1 {
		t.Errorf("expected the live view published once, got %d", view.runs)
	}
	var said, retired bool
	for _, c := range f.tracker.Comments("mw-e.3") {
		said = said || strings.Contains(c, "I ran step echo myself.")
		retired = retired || (strings.Contains(c, "RETIRED") && strings.Contains(c, "run by the Governor himself"))
	}
	if !said || !retired {
		t.Errorf("expected his word and the retirement on the bead, got %q", f.tracker.Comments("mw-e.3"))
	}
	if jobs := f.runner.Jobs(); len(jobs) != 0 {
		t.Errorf("expected nothing run, got %+v", jobs)
	}
	var out bytes.Buffer
	if _, err := (application.HandsList{Notes: f.tracker, Out: &out}).Run(ctx, "mw-e.3"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "echo on desktop as user  sha256") || !strings.Contains(out.String(), "retired: run by the Governor himself") {
		t.Errorf("expected mw hands list to show the step done, got\n%s", out.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "echo ") && strings.Contains(line, "not run") {
			t.Errorf("expected the echo step not 'not run', got %q", line)
		}
	}
}

// Words that are not that sentence, a step the bead does not have, a step
// already retired, and a message from someone else leave the steps as they are;
// the message is still a comment as ever.
func TestTheWordThatHeRanAStepChangesNothingElse(t *testing.T) {
	ctx := context.Background()
	f := newRunFixture(t)
	f.say(t, "tx-1", "mw-e.3", "I ran step echo myself, and it printed hello")
	f.say(t, "tx-2", "mw-e.3", "I ran step nowhere-near myself.")
	f.message(t, "02aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "tx-3",
		`{"thread":{"bead":"mw-e.3"},"text":"I ran step linger myself."}`)
	view := &countingView{}
	inbox := f.inbox()
	inbox.View = view

	if _, err := inbox.Apply(ctx); err != nil {
		t.Fatalf("applying: %v", err)
	}

	for _, id := range []string{"echo", "linger", "nowhere"} {
		if note, _ := f.tracker.Note(ctx, application.HandsRetiredKey("mw-e.3", id)); note != "" {
			t.Errorf("expected step %s left alone, retired as %q", id, note)
		}
	}
	if view.runs != 0 {
		t.Errorf("expected no view published, got %d", view.runs)
	}
	if got := f.tracker.Comments("mw-e.3"); len(got) != 2 {
		t.Errorf("expected his two words as comments, got %q", got)
	}
}

// Said twice, or said of a step already retired, it retires nothing again and
// keeps the first reason.
func TestTheWordThatHeRanAStepKeepsAnEarlierRetirement(t *testing.T) {
	ctx := context.Background()
	f := newRunFixture(t)
	mustDo(t, f.tracker.SetNote(ctx, application.HandsRetiredKey("mw-e.3", "echo"), "done by hand at the Laptop"))
	f.say(t, "tx-ran", "mw-e.3", "I ran step echo myself.")

	if _, err := f.inbox().Apply(ctx); err != nil {
		t.Fatalf("applying: %v", err)
	}

	if note, _ := f.tracker.Note(ctx, application.HandsRetiredKey("mw-e.3", "echo")); note != "done by hand at the Laptop" {
		t.Errorf("expected the first reason kept, got %q", note)
	}
	if got := f.tracker.Comments("mw-e.3"); len(got) != 1 {
		t.Errorf("expected only his word on the bead, got %q", got)
	}
}

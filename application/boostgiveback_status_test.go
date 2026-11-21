package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-6ww.185.10: mw status names the give-back of a Boost that stopped answering,
// in place of the STRANDED line that sends the Mayor to mw retry --force.

func keepGaveBack(t *testing.T, tracker interface {
	SetNote(ctx context.Context, key, value string) error
}, gave application.GaveBack) {
	t.Helper()
	if err := tracker.SetNote(context.Background(), application.GaveBackKey(gave.Host), gave.String()); err != nil {
		t.Fatal(err)
	}
}

func TestStatusSaysAStoryTheHomeGaveBackWasGivenBackAndNotStranded(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	storyOn(t, tracker, "mw-gq6.61", "A story given back", "auto")
	syncedAt(t, tracker, "desktop", 17*time.Hour)
	keepGaveBack(t, tracker, application.GaveBack{Host: "desktop", At: statusNow.Add(-30 * time.Minute), After: 30 * time.Minute, Gave: []string{"mw-gq6.61"}})

	printed := flat(otherHostStatus(t, tracker).String())
	const want = "given back at 09-18 11:30Z: desktop did not answer for 30 min"
	if !strings.Contains(printed, want) {
		t.Errorf("expected %q, got:\n%s", want, printed)
	}
	if strings.Contains(printed, "STRANDED") {
		t.Errorf("expected no STRANDED line for a story given back, got:\n%s", printed)
	}
}

func TestStatusNamesTheGiveBackOnAClaimItLeftOnAHostThatDoesNotAnswer(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.60", "desktop", "src/a.ts", false)
	syncedAt(t, tracker, "desktop", 17*time.Hour)
	keepGaveBack(t, tracker, application.GaveBack{Host: "desktop", At: statusNow.Add(-30 * time.Minute), After: 30 * time.Minute,
		Left: map[string]string{"mw-gq6.60": "pinned by its host_why: needs the age key"}})

	block := flat(section(otherHostStatus(t, tracker)))
	for _, want := range []string{"desktop does not answer", "the home's give-back at 09-18 11:30Z left it: pinned by its host_why: needs the age key"} {
		if !strings.Contains(block, want) {
			t.Errorf("expected %q, got:\n%s", want, block)
		}
	}
}

func TestADownHostsDamperLineNamesTheGiveBackWhenItHasRun(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	aClaimOn(t, tracker, "mw-gq6.1445", "desktop", "src/a.ts", true)
	if err := tracker.SetUpdated("mw-gq6.1445", statusNow.Add(-17*time.Hour)); err != nil {
		t.Fatal(err)
	}
	syncedAt(t, tracker, "desktop", 17*time.Hour)
	keepGaveBack(t, tracker, application.GaveBack{Host: "desktop", At: statusNow.Add(-30 * time.Minute), After: 30 * time.Minute,
		Left: map[string]string{"mw-gq6.1445": "pinned by its host_why: needs the age key"}})

	var open string
	for _, l := range damperLines(statusWithDampers(t, tracker), "mw-gq6.1445") {
		if strings.Contains(l, "refused open") {
			open = flat(l)
		}
	}
	for _, want := range []string{"desktop does not answer", "give-back ran at 09-18 11:30Z"} {
		if !strings.Contains(open, want) {
			t.Errorf("expected %q in the damper line, got %q", want, open)
		}
	}
}

func TestAGaveBackNoteThatCannotBeReadSaysNothing(t *testing.T) {
	gave := application.ParseGaveBack("not json")
	if !gave.At.IsZero() || gave.Line("mw-gq6.1") != "" {
		t.Errorf("expected nothing from an unreadable note, got %+v", gave)
	}
}

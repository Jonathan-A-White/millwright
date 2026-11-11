package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestDispatchKeepsWhenAStoryWasFirstPassedForARigNotCheckedOut(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory = 3, tracker
	dispatch.Rigs = map[string]string{"elsewhere": "/rigs/elsewhere"}
	first := time.Date(2026, 10, 22, 8, 40, 0, 0, time.UTC)
	clock := first
	dispatch.Now = func() time.Time { return clock }
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	key := application.HostPassedKey("vps")
	kept, _ := tracker.Note(ctx, key)
	passed := application.ParseHostPassed(kept)
	if len(passed) != 1 || passed[0].Story != "mw-gq6.1" || passed[0].Gate != "rig-not-checked-out" ||
		!passed[0].Since.Equal(first) || !strings.Contains(passed[0].Why, "not checked out on vps") {
		t.Fatalf("expected the note %s to hold mw-gq6.1 first passed at %s, got %q", key, first, kept)
	}

	// Passed again an hour on, it was passed since the first time, and the note
	// is not written again.
	clock = first.Add(time.Hour)
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching again: %v", err)
	}
	if again, _ := tracker.Note(ctx, key); again != kept {
		t.Errorf("expected the note to keep %q, got %q", kept, again)
	}
	if writes := tracker.NoteWrites(key); writes != 1 {
		t.Errorf("expected the note written once, got %d writes", writes)
	}

	// The rig checked out, the story is started and the note is taken off.
	dispatch.Rigs = map[string]string{"millwright": "/rigs/millwright"}
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching a third time: %v", err)
	}
	if after, _ := tracker.Note(ctx, key); after != "" {
		t.Errorf("expected the note cleared, got %q", after)
	}
}

func TestDispatchKeepsNoNoteOfAStoryAnotherGatePassedOver(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory = 1, tracker
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.2", Title: "A second"})

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if writes := tracker.NoteWrites(application.HostPassedKey("vps")); writes != 0 {
		t.Errorf("expected a story passed for the cap to leave no note, got %d writes", writes)
	}
}

func TestADryRunKeepsNoNoteOfWhatThisHostPassed(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.Memory, dispatch.DryRun = 3, tracker, true
	dispatch.Rigs = map[string]string{}
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	if _, err := dispatch.Run(context.Background()); err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if writes := tracker.NoteWrites(application.HostPassedKey("vps")); writes != 0 {
		t.Errorf("expected a dry run to write no note, got %d writes", writes)
	}
}

func TestDispatchKeepsTheNoteOfAStoryPassedForWantOfAGristTestKey(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker := aGristKeyFactory(t, false)
	dispatch.Memory = tracker
	if err := tracker.SetAcceptance("mw-gq6.1", "1. `mw grist eval lampas` passes."); err != nil {
		t.Fatal(err)
	}
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	passed := passedFor(report, "mw-gq6.1")
	if passed == nil {
		t.Fatalf("expected mw-gq6.1 passed, got %+v", report.Passed)
	}
	kept, _ := tracker.Note(ctx, application.HostPassedKey("vps"))
	notes := application.ParseHostPassed(kept)
	if len(notes) != 1 || notes[0].Story != "mw-gq6.1" || notes[0].Gate != "no-grist-key" || notes[0].Why != passed.Why {
		t.Fatalf("expected the note to hold mw-gq6.1 with the dispatch's own reason %q, got %q", passed.Why, kept)
	}
}

// mw-6ww.163.5: a host the Governor retired (retired_hosts) is no host that
// could take a ready story, so its silence or its stale pass note is not
// counted when mw status asks whether every host passed a story.

func passedNote(t *testing.T, tracker interface {
	SetNote(context.Context, string, string) error
}, host, id string) {
	t.Helper()
	line := application.FormatHostPassed([]application.HostPassed{{
		Story: id, Since: statusNow.Add(-40 * time.Minute), Gate: "metered-network", Why: "the network is metered"}})
	if err := tracker.SetNote(context.Background(), application.HostPassedKey(host), line); err != nil {
		t.Fatal(err)
	}
}

func passedHosts(report application.StatusReport, id string) []string {
	var hosts []string
	for _, p := range report.NoHostTakes[id].Passed {
		hosts = append(hosts, p.Host)
	}
	return hosts
}

func TestStatusDoesNotCountARetiredHostAsOneThatCouldTakeAReadyStory(t *testing.T) {
	for _, c := range []struct {
		name    string
		retired []string
		noteOf  []string
		want    []string
	}{
		{"retired desktop with no note", []string{"desktop"}, []string{"laptop"}, []string{"laptop"}},
		{"retired desktop with a stale note", []string{"desktop"}, []string{"laptop", "desktop"}, []string{"laptop"}},
		{"nothing retired, desktop has not passed", nil, []string{"laptop"}, nil},
		{"nothing retired, desktop passed too", nil, []string{"laptop", "desktop"}, []string{"desktop", "laptop"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			tracker := aTrackerPathedToVPS(t)
			storyOn(t, tracker, "mw-gq6.50", "A ready story", "auto")
			for _, host := range c.noteOf {
				passedNote(t, tracker, host, "mw-gq6.50")
			}
			got := passedHosts(retiredStatus(t, tracker, c.retired...), "mw-gq6.50")
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("expected the hosts that passed it to be %v, got %v", c.want, got)
			}
		})
	}
}

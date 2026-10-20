package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// flatClaim is a tracker whose claim is refused with err, as the words of one:
// a refusal that crossed a boundary which kept its text and not its type.
type flatClaim struct {
	application.WorkTracker
	err func(id string) error
}

func (f flatClaim) ClaimStory(_ context.Context, id string) error { return f.err(id) }

// mw-gq6.643: two Boosts racing for one ready story is normal. The one that
// loses did nothing wrong; its line in the dispatch log is ok and
// mw status's "failed N in a row" does not count it.
func TestDispatchLogsALostClaimAsOkWhateverFormTheRefusalTook(t *testing.T) {
	for name, refusal := range map[string]func(id string) error{
		"as the typed error": func(id string) error {
			return &application.ClaimHeldError{ID: id, Holder: "mw@desktop"}
		},
		"as its words only": func(id string) error {
			return errors.New(id + " is already claimed by mw@desktop, so it was not taken")
		},
	} {
		t.Run(name, func(t *testing.T) {
			dispatch, tracker, _, runner, _ := aFactory(t)
			log := &apptest.FakeTickLog{}
			dispatch.Log = log
			tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
			dispatch.Tracker = flatClaim{WorkTracker: tracker, err: refusal}

			if _, err := dispatch.Run(context.Background()); err != nil {
				t.Fatalf("expected a lost claim not to fail the tick, got %v", err)
			}
			lines := log.Lines()
			if len(lines) != 1 || !strings.Contains(lines[0], " "+application.DispatchLogOK) ||
				!strings.Contains(lines[0], "mw@desktop") || strings.Contains(lines[0], application.DispatchLogFailed) {
				t.Fatalf("expected one ok line naming the holder, got %q", lines)
			}
			if count := application.CountTicks(context.Background(), log, application.DispatchOutcome); count.Failed != 0 || !count.Known {
				t.Errorf("expected a good tick, got %+v", count)
			}
			if names := runner.Names(); len(names) != 0 {
				t.Errorf("expected no session started, got %v", names)
			}
		})
	}
}

// Any other refusal of a claim is still a failed tick, and still counts.
func TestDispatchLogsAnyOtherClaimErrorAsFailed(t *testing.T) {
	dispatch, tracker, _, _, _ := aFactory(t)
	log := &apptest.FakeTickLog{}
	dispatch.Log = log
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	dispatch.Tracker = flatClaim{WorkTracker: tracker, err: func(string) error {
		return errors.New("bd update --claim: the database is down")
	}}

	if _, err := dispatch.Run(context.Background()); err == nil {
		t.Fatal("expected the tick to fail")
	}
	lines := log.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], " "+application.DispatchLogFailed) {
		t.Fatalf("expected one failed line, got %q", lines)
	}
	if count := application.CountTicks(context.Background(), log, application.DispatchOutcome); count.Failed != 1 {
		t.Errorf("expected one failed tick counted, got %+v", count)
	}
}

// The two lines cloud1 wrote on 2026-10-20 read as good runs; a line that holds a
// lost claim and another fault is still a failure.
func TestDispatchOutcomeReadsALostClaimLineAsGood(t *testing.T) {
	stamp := "2026-10-20T14:00:47Z "
	for line, want := range map[string]application.TickOutcome{
		stamp + "failed: dispatching on cloud1: mw-06ggew.5: claiming mw-06ggew.5: mw-06ggew.5 is already claimed by mw@desktop, so it was not taken":                                                                        application.TickGood,
		stamp + "failed: dispatching on cloud1: mw-5r3p30.676: claiming mw-5r3p30.676: mw-5r3p30.676 is already claimed by mw@desktop, so it was not taken":                                                                  application.TickGood,
		stamp + "failed: dispatching on cloud1: mw-a.1: claiming mw-a.1: mw-a.1 is already claimed by mw@desktop, so it was not taken; mw-a.2: claiming mw-a.2: mw-a.2 is already claimed by mw@cloud4, so it was not taken": application.TickGood,
		stamp + "failed: dispatching on cloud1: mw-a.1: claiming mw-a.1: mw-a.1 is already claimed by mw@desktop, so it was not taken; mw-a.2: claiming mw-a.2: bd said no":                                                  application.TickFailed,
		stamp + "ok: claim lost to mw@desktop": application.TickGood,
	} {
		if _, got := application.DispatchOutcome(line); got != want {
			t.Errorf("%q: expected %s, got %s", line, want, got)
		}
	}
}

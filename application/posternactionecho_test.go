package application_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// Every one-tap action the inbox applies is echoed as one action_applied event
// whose detail is the tap's txid: the txid a comment names is not enough, as
// Keep, Verified and Close do not write the word.
func TestApplyingATapEmitsOneActionAppliedEventCarryingItsTxid(t *testing.T) {
	cases := []struct {
		action map[string]any
		bead   string
	}{
		{map[string]any{"action": "release", "bead": "mw-e.1"}, "mw-e.1"},
		{map[string]any{"action": "hold", "bead": "mw-e.3"}, "mw-e.3"},
		{map[string]any{"action": "keep", "bead": "mw-e.3"}, "mw-e.3"},
		{map[string]any{"action": "close", "bead": "mw-e.2"}, "mw-e.2"},
		{map[string]any{"action": "verified", "bead": "mw-e.3"}, "mw-e.3"},
	}
	for _, c := range cases {
		name := c.action["action"].(string)
		t.Run(name, func(t *testing.T) {
			f := newApplyFixture(t)
			log := &apptest.FakeEventLog{}
			f.action(t, "tx-"+name, c.action)
			inbox := f.inbox()
			inbox.Events = log
			if _, err := inbox.Apply(context.Background()); err != nil {
				t.Fatalf("applying: %v", err)
			}
			got := log.All()
			if len(got) != 1 {
				t.Fatalf("the log holds %d events, want one: %+v", len(got), got)
			}
			ev := got[0]
			if ev.Kind != events.KindActionApplied || ev.Bead != c.bead || ev.Detail != "tx-"+name || ev.Actor != application.GovernorPosternActor || ev.Lane != events.LaneNormal {
				t.Fatalf("the event is %+v, want a normal-lane action_applied of %s with detail tx-%s", ev, c.bead, name)
			}
		})
	}
}

// A tap that is refused did nothing, so it is echoed by no event; and a
// replayed tap is not applied again, so it is echoed once.
func TestARefusedOrReplayedTapIsEchoedByNoEvent(t *testing.T) {
	f := newApplyFixture(t)
	log := &apptest.FakeEventLog{}
	f.action(t, "tx-refused", map[string]any{"action": "release", "bead": "mw-e.3"}) // open, not held
	f.action(t, "tx-once", map[string]any{"action": "release", "bead": "mw-e.1"})
	inbox := f.inbox()
	inbox.Events = log
	for range 2 {
		if _, err := inbox.Apply(context.Background()); err != nil {
			t.Fatalf("applying: %v", err)
		}
	}
	got := log.All()
	if len(got) != 1 || got[0].Detail != "tx-once" {
		t.Fatalf("the log holds %+v, want the one event of tx-once", got)
	}
}

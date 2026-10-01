package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

func TestSubscribeFileIsRead(t *testing.T) {
	sub, err := application.ParseSubscription("deputy", "# who hears what\nkinds = [\"mail\", \"card-answered\", \"landing\"]  # three\nspring = true\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := strings.Join(sub.Kinds, ","); got != "mail,card_answered,landing" || !sub.Spring || sub.Seat != "deputy" {
		t.Fatalf("got %+v", sub)
	}
}

func TestSubscribeFileRefusesABadKindAndListsTheKinds(t *testing.T) {
	_, err := application.ParseSubscription("mayor", `kinds = ["mail", "carrier-pigeon"]`)
	if err == nil {
		t.Fatal("a bad kind was accepted")
	}
	for _, want := range []string{"seats/mayor/subscribe.toml", `"carrier-pigeon"`, "mail", "card_answered", "landing", "bead_changed", "job"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not say %q", err, want)
		}
	}
}

func TestSubscribeFileRefusesWhatItDoesNotHold(t *testing.T) {
	for name, text := range map[string]string{
		"no kinds":             "spring = true\n",
		"empty kinds":          "kinds = []\n",
		"unknown key":          "kinds = [\"mail\"]\nloud = true\n",
		"a table":              "[kinds]\n",
		"spring is not bool":   "kinds = [\"mail\"]\nspring = maybe\n",
		"spring a seat w/o up": "",
	} {
		seat := "deputy"
		if name == "spring a seat w/o up" {
			seat, text = "mayor", "kinds = [\"mail\"]\nspring = true\n"
		}
		if _, err := application.ParseSubscription(seat, text); err == nil {
			t.Errorf("%s: accepted %q", name, text)
		}
	}
}

func TestSubscriptionMatchesByKindAndMailBox(t *testing.T) {
	sub := application.Subscription{Seat: "deputy", Kinds: []string{"mail", "landing", "message"}}
	cases := []struct {
		name string
		ev   events.Event
		want bool
	}{
		{"mail to the seat", events.Event{Kind: events.KindMail, Detail: "deputy"}, true},
		{"mail to another box", events.Event{Kind: events.KindMail, Detail: "mayor"}, false},
		{"a message", events.Event{Kind: events.KindMessage}, true},
		{"a landing", events.Event{Kind: events.KindBeadChanged, From: events.BeadRunning, To: events.BeadLanded}, true},
		{"a comment on a landed bead", events.Event{Kind: events.KindBeadChanged, From: events.BeadLanded, To: events.BeadLanded}, false},
		{"a claim", events.Event{Kind: events.KindBeadChanged, From: events.BeadOpen, To: events.BeadClaimed}, false},
		{"a job", events.Event{Kind: events.KindJob}, false},
	}
	for _, c := range cases {
		if got := sub.Matches(c.ev); got != c.want {
			t.Errorf("%s: Matches = %v, want %v", c.name, got, c.want)
		}
	}
}

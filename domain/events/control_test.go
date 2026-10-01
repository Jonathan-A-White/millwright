package events_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

func TestControlDetailsRoundTrip(t *testing.T) {
	for _, c := range []events.Control{
		{Word: events.ControlCancel},
		{Word: events.ControlPauseHost, Host: "laptop"},
		{Word: events.ControlResumeHost, Host: "laptop"},
		{Word: events.ControlCap, Host: "desktop", N: 3},
		{Word: events.ControlPriority, N: 0},
	} {
		got, err := events.ParseControl(c.Detail())
		if err != nil || got != c {
			t.Errorf("%q read back as %+v, %v; want %+v", c.Detail(), got, err, c)
		}
	}
}

func TestParseControlRefusesWhatIsNoControlWord(t *testing.T) {
	for detail, why := range map[string]string{
		"":                "says nothing",
		"explode":         "is not one of",
		"cancel now":      "is not one of",
		"pause-host":      "is not one of",
		"pause-host a b":  "is not one of",
		"cap laptop":      "is not one of",
		"cap laptop none": "whole number",
		"cap laptop 0":    "at least 1",
		"priority 5":      "0 (most urgent) to 4",
		"priority urgent": "0 (most urgent) to 4",
		"resume-host":     "is not one of",
	} {
		if _, err := events.ParseControl(detail); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%q: got %v, want an error containing %q", detail, err, why)
		}
	}
}

func TestAControlEventNamesABeadOnlyWhereTheWordIsAboutOne(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ev := func(bead, detail string) events.Event {
		return events.Event{Seq: 1, Ts: at, Kind: events.KindControl, Bead: bead, Detail: detail, Lane: events.LaneNormal}
	}
	for _, good := range []events.Event{
		ev("mw-1", "cancel"), ev("mw-1", "priority 1"), ev("", "pause-host laptop"), ev("", "resume-host laptop"), ev("", "cap laptop 2"),
	} {
		if err := good.Validate(); err != nil {
			t.Errorf("%+v: %v", good, err)
		}
	}
	for bead, detail := range map[string]string{"": "cancel", "mw-1 ": "pause-host laptop", "mw-2": "cap laptop 2"} {
		if err := ev(strings.TrimSpace(bead), detail).Validate(); err == nil {
			t.Errorf("bead %q detail %q: expected a refusal", bead, detail)
		}
	}
	bad := ev("mw-1", "cancel")
	bad.From, bad.To = "a", "b"
	if err := bad.Validate(); err == nil {
		t.Error("a control event with a from and to is no transition")
	}
	if err := ev("mw-1", "stop").Validate(); err == nil {
		t.Error("an unknown word was accepted")
	}
}

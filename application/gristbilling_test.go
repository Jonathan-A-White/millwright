package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// A sender not marked API only is billed to the Governor's subscription, even
// with api credits on, a usable key and no spent mark: a person marked "max",
// a person with no mark or one the mill does not know, a key that is no
// person's, a lookup that failed, and a mill that asks no one (mw-gq6.1518).
func TestASenderNotMarkedAPIOnlyIsBilledToTheSubscription(t *testing.T) {
	failing := application.NewPersonCap(aRegister(func(context.Context, string) (application.Person, bool, error) {
		return application.Person{}, false, errors.New("the postern backend answered 503")
	}), &bytes.Buffer{})
	for name, persons := range map[string]*application.PersonCap{
		"marked max":       markedPersons(smallMillPhone, application.PersonBillingMax),
		"no mark":          markedPersons(smallMillPhone, ""),
		"an unknown mark":  markedPersons(smallMillPhone, "card"),
		"no person":        markedPersons(gristStrangerKey, application.PersonBillingAPI),
		"a failed lookup":  failing,
		"no lookup at all": nil,
	} {
		t.Run(name, func(t *testing.T) {
			good := application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`)}
			mill, _, grinder, _, _, _ := creditMill(t, good)
			mill.Persons = persons
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if keys := grinder.keys(); len(keys) != 1 || keys[0] != "" {
				t.Fatalf("expected the grist ground once with no key, got %q", keys)
			}
			if line := lineOf(t, mill); line.Billing != application.BillingSubscription || line.Status != application.GristAnswered {
				t.Fatalf("expected an answered line billed to subscription, got %+v", line)
			}
		})
	}
}

// A person marked API only is billed to the key whatever api_credits says:
// the switch is the factory's own grinds' (mw-gq6.1518). The line names the
// person.
func TestAnAPIOnlyPersonIsBilledToTheKeyWhateverTheSwitch(t *testing.T) {
	good := application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`)}
	mill, _, grinder, credits, _, _ := creditMill(t, good)
	credits.On = false
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if keys := grinder.keys(); len(keys) != 1 || keys[0] != testAPIKey {
		t.Fatalf("expected one grind on the key, got %q", keys)
	}
	if line := lineOf(t, mill); line.Billing != application.BillingAPI || line.Person != "p-phone" {
		t.Fatalf("expected a line billed to api naming the person p-phone, got %+v", line)
	}
}

// With the credit found spent, an API-only person's grist is not ground, and
// its refusal names the day the credit renews: the next renewal day after
// today, rolling over a month's end, the day itself counted as gone
// (mw-gq6.1518). A mark of 1517's, with no why, is a spent credit too.
func TestAnAPIOnlyPersonOutOfCreditIsToldTheRenewalDay(t *testing.T) {
	for name, tc := range map[string]struct {
		today time.Time
		day   int
		why   string
		want  string
	}{
		"before the renewal day":       {time.Date(2026, 12, 16, 9, 0, 0, 0, time.UTC), 0, application.CreditsSpent, "Dec 17"},
		"on the renewal day":           {time.Date(2026, 12, 17, 9, 0, 0, 0, time.UTC), 0, application.CreditsSpent, "Jan 17"},
		"after it, at the month's end": {time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC), 0, application.CreditsSpent, "Jan 17"},
		"a mark with no why":           {time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), 0, "", "Nov 17"},
		"a renewal day of its own":     {time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), 5, application.CreditsSpent, "Nov 5"},
		"a day February has not":       {time.Date(2027, 2, 2, 9, 0, 0, 0, time.UTC), 31, application.CreditsSpent, "Feb 28"},
	} {
		t.Run(name, func(t *testing.T) {
			mill, backend, grinder, credits, state, _ := creditMill(t, application.SessionResult{})
			mill.Now = func() time.Time { return tc.today }
			credits.Now = mill.Now
			mill.Ceilings.CreditsRenewDay = tc.day
			state.Set(application.GristCreditMark{SpentAt: tc.today.Add(-2 * time.Hour), Why: tc.why})
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(grinder.calls) != 0 {
				t.Fatalf("expected no grind, got %q", grinder.keys())
			}
			want := "The tutor is out of credits until " + tc.want + "; send it again then."
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if answer.Status != application.GristRefused || answer.Reason != want {
				t.Fatalf("expected the grist refused %q, got %+v", want, answer)
			}
			if line := lineOf(t, mill); line.Billing != application.BillingNone || line.Reason != want {
				t.Fatalf("expected a line billed to none refused %q, got %+v", want, line)
			}
		})
	}
}

// An out-of-credits refusal is the mill's want of credit, not the sender's
// use of the day.
func TestAnOutOfCreditsRefusalIsNoUseOfTheDay(t *testing.T) {
	mill, _, _, _, state, _ := creditMill(t, application.SessionResult{})
	state.Set(application.GristCreditMark{SpentAt: creditsNow.Add(-time.Hour), Why: application.CreditsSpent})
	mill.Ceilings.DailyLimit = 1
	if err := mill.State.Append(context.Background(), application.GrindLine{
		Time: creditsNow, Txid: "direct:old", Sender: application.KeyFingerprint(smallMillPhone),
		Status: application.GristRefused, Reason: outOfCreditsNov17,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, _ := mill.State.Lines(context.Background())
	if last := lines[len(lines)-1]; last.Reason != outOfCreditsNov17 {
		t.Fatalf("expected the grist refused out of credits, not for the day's limit, got %+v", last)
	}
}

// A person marked API only may send api_daily_limit grists a day (30 when
// config says nothing), counted over the person's keys; the next is refused
// with the daily cap's words, which say to send it again tomorrow. A person on
// the Max keeps the home's daily_limit (mw-gq6.1518).
func TestAnAPIOnlyPersonHasThirtyGristsADay(t *testing.T) {
	for name, tc := range map[string]struct {
		billing  string
		apiLimit int
		sent     int
		refused  int // the cap the refusal names; 0 is answered
	}{
		"api, 29 sent":            {application.PersonBillingAPI, 0, 29, 0},
		"api, 30 sent":            {application.PersonBillingAPI, 0, 30, 30},
		"api, a limit of its own": {application.PersonBillingAPI, 5, 5, 5},
		"max, 30 sent":            {application.PersonBillingMax, 0, 30, 0},
		"max, 49 sent":            {application.PersonBillingMax, 0, 49, 0},
	} {
		t.Run(name, func(t *testing.T) {
			good := application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`)}
			mill, backend, _, _, _, _ := creditMill(t, good)
			mill.Persons = markedPersons(smallMillPhone, tc.billing)
			mill.Ceilings = application.GristCeilings{DailyLimit: 250, APIDailyLimit: tc.apiLimit}
			for _, line := range sentLines(smallMillPhone, tc.sent) {
				line.Time = creditsNow.Add(-time.Hour)
				if err := mill.State.Append(context.Background(), line); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			answer := openFakeAnswer(t, backend.Delivered()[0])
			if tc.refused > 0 {
				if answer.Status != application.GristRefused || answer.Reason != application.GristReasonDaily(tc.refused) {
					t.Fatalf("expected the daily cap's refusal at %d, got %+v", tc.refused, answer)
				}
				return
			}
			if answer.Status != application.GristAnswered {
				t.Fatalf("expected the grist answered after %d sent, got %+v", tc.sent, answer)
			}
		})
	}
}

// The person lookup's PersonCap keeps the billing mark it was told, so the
// same grist is billed as the backend said.
func TestTheBillingMarkIsKeptWithThePerson(t *testing.T) {
	asked := 0
	persons := application.NewPersonCap(aRegister(func(context.Context, string) (application.Person, bool, error) {
		asked++
		return application.Person{ID: "p1", Billing: "api"}, true, nil
	}), &bytes.Buffer{})
	for range 2 {
		if person, known := persons.Lookup(context.Background(), "k"); !known || person.Billing != application.PersonBillingAPI {
			t.Fatalf("expected the person marked api, got %+v %v", person, known)
		}
	}
	if asked != 1 {
		t.Fatalf("expected one lookup, kept, got %d", asked)
	}
}

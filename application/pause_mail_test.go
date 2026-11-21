package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-kzxfpzw: while usage is exhausted the usage reader asks for the pause every
// half hour; a pause that left a host unreached ran again each time and mailed
// the Mayor the same paused set again.

type pauseMailRig struct {
	book  *apptest.FakePauseBook
	fleet *apptest.FakePauseFleet
	mail  *apptest.FakeMailbox
	now   time.Time
	until time.Time
}

func aPauseWithAHostThatIsDown(t *testing.T) *pauseMailRig {
	t.Helper()
	r := &pauseMailRig{
		book: apptest.NewFakePauseBook(), fleet: apptest.NewFakePauseFleet(), mail: apptest.NewFakeMailbox(),
		now: time.Date(2026, 11, 20, 9, 4, 49, 0, time.UTC),
	}
	r.until = r.now.Add(20 * time.Hour)
	for _, host := range []string{"laptop", "cloud7"} {
		r.fleet.Add(application.FleetHostPause{Host: host, Cap: "3", Notes: apptest.NewFakeTracker(), Stories: apptest.NewFakeTracker(),
			Runner: apptest.NewFakeRunner(), Git: apptest.NewFakePauseGit()})
	}
	r.fleet.Unreached["cloud7"] = true
	return r
}

func (r *pauseMailRig) pause(t *testing.T, after time.Duration) string {
	t.Helper()
	var out bytes.Buffer
	_, err := application.Pause{
		Fleet: r.fleet, Cloud: apptest.NewFakePauseCloud(), Config: &apptest.FakePauseConfig{Value: "4", Set: true}, Book: r.book,
		Mail: r.mail, Host: "laptop", Now: func() time.Time { return r.now.Add(after) }, Out: &out,
	}.Run(context.Background(), application.PauseRequest{Reason: "usage 99.0% reached the reserve", Until: r.until})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func (r *pauseMailRig) mails(t *testing.T) []application.Message {
	t.Helper()
	got, err := r.mail.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestASecondPauseWhileAHostIsStillDownSendsNoMail(t *testing.T) {
	r := aPauseWithAHostThatIsDown(t)
	r.pause(t, 0)
	r.pause(t, 30*time.Minute)
	r.pause(t, time.Hour)

	var paused int
	for _, m := range r.mails(t) {
		if m.Subject == "The factory is paused" {
			paused++
		}
	}
	if paused != 1 || len(r.mails(t)) != 1 {
		t.Errorf("the Mayor got %d 'The factory is paused' mails of %d, want exactly 1", paused, len(r.mails(t)))
	}
	set, _ := r.book.Set(application.PauseDir(r.now))
	var repeated int
	for _, n := range set.Notes {
		if strings.Contains(n, "cloud7") {
			repeated++
		}
	}
	if repeated != 1 {
		t.Errorf("the set holds the note of cloud7 being down %d times, want 1: %q", repeated, set.Notes)
	}
}

func TestAPauseWhoseSetChangedMailsOnlyWhatChanged(t *testing.T) {
	r := aPauseWithAHostThatIsDown(t)
	r.pause(t, 0)
	r.pause(t, 30*time.Minute)

	r.fleet.Unreached["cloud7"] = false
	r.pause(t, time.Hour)
	r.pause(t, 90*time.Minute)

	mails := r.mails(t)
	if len(mails) != 2 {
		t.Fatalf("the Mayor got %d mails, want 2 (the pause, then the host reached)", len(mails))
	}
	body := mails[1].Body
	if !strings.Contains(body, "host cloud7") {
		t.Errorf("the second mail does not name cloud7 as newly reached:\n%s", body)
	}
	if strings.Contains(body, "host laptop") || strings.Contains(body, "night passes") {
		t.Errorf("the second mail repeats what did not change:\n%s", body)
	}
}

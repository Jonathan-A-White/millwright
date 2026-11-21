package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const mirrorUnit = "mw-postern-mirror.service"

// mirrorUnitCycle is what systemd does with the mirror's unit: one run of
// mw postern mirror, and when it fails, one run of mw doctor unit-failed.
type mirrorUnitCycle struct {
	units    *apptest.FakeUnitSystem
	failures *apptest.FakeUnitFailures
	mailbox  *apptest.FakeMailbox
	now      time.Time
}

func newMirrorUnitCycle() *mirrorUnitCycle {
	units := apptest.NewFakeUnitSystem()
	units.SetTimer(mirrorUnit, true)
	return &mirrorUnitCycle{
		units:    units,
		failures: apptest.NewFakeUnitFailures(),
		mailbox:  apptest.NewFakeMailbox(),
		now:      time.Date(2026, 11, 5, 8, 33, 0, 0, time.UTC),
	}
}

// run is one timer tick: the mirror against a boost whose ssh answers askErr,
// then the doctor's unit-failed when the mirror failed. It reports whether the
// unit failed and what the watchdog was sent to.
func (c *mirrorUnitCycle) run(t *testing.T, askErr error) (failed bool, dests []string) {
	t.Helper()
	m := &downBoostMirrorer{askErr: askErr}
	_, err := runDownBoostMirror(t, m)
	c.now = c.now.Add(20 * time.Minute)
	if err != nil {
		c.units.SetJournal(mirrorUnit, "mw postern mirror: "+err.Error())
		_, uerr := application.UnitFailed{
			Units: c.units, Failures: c.failures, Mailbox: c.mailbox, Host: "laptop",
			Now: func() time.Time { return c.now },
		}.Run(context.Background(), mirrorUnit)
		if uerr != nil {
			t.Fatalf("unit-failed: %v", uerr)
		}
	} else {
		c.units.RanClean(mirrorUnit, c.now)
	}
	return err != nil, m.dests
}

func (c *mirrorUnitCycle) mails(t *testing.T) []application.Message {
	t.Helper()
	sent, err := c.mailbox.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	return sent
}

// A Boost outage (2026-11-05 from 08:33Z, the desktop down) mailed the Mayor
// 'unit failed twice in a row: mw-postern-mirror.service' every 20 minutes. The
// mirror now takes a Boost that cannot be reached for down and exits 0, so the
// unit never fails and no mail is sent however long the outage lasts, and the
// watchdog's files go on every run.
func TestAMirrorRunThroughAnOutageOfTheBoostMailsNothing(t *testing.T) {
	for _, text := range []string{
		connectRefused,
		connectTimedOut,
		"ssh desktop: exit status 255: ssh: connect to host 10.88.0.3 port 22: No route to host",
	} {
		c := newMirrorUnitCycle()
		for run := 1; run <= 6; run++ {
			failed, dests := c.run(t, errors.New(text))
			if failed {
				t.Fatalf("%q: run %d of an outage failed the unit", text, run)
			}
			if len(dests) != 1 || dests[0] != "root@vps:/var/lib/postern-watchdog/" {
				t.Fatalf("%q: run %d copied to %v, want only the watchdog", text, run, dests)
			}
		}
		if sent := c.mails(t); len(sent) != 0 {
			t.Fatalf("%q: an outage of the Boost mailed the Mayor %d times: %+v", text, len(sent), sent)
		}
	}
}

// A Boost that is up and fails otherwise still mails, once for each two
// failures, and the watchdog's files go in every run, failed or not.
func TestAMirrorFailingWithTheBoostUpStillMailsAfterTwoFailures(t *testing.T) {
	c := newMirrorUnitCycle()
	askErr := errors.New("ssh desktop: exit status 255: Permission denied (publickey).")
	for run := 1; run <= 2; run++ {
		failed, dests := c.run(t, askErr)
		if !failed {
			t.Fatalf("run %d with the Boost up and refusing the key did not fail", run)
		}
		if len(dests) != 1 {
			t.Fatalf("run %d: the watchdog's files did not go: %v", run, dests)
		}
		want := 0
		if run == 2 {
			want = 1
		}
		if sent := c.mails(t); len(sent) != want {
			t.Fatalf("after run %d the Mayor has %d mails, want %d", run, len(sent), want)
		}
	}
	sent := c.mails(t)
	if want := "unit failed twice in a row: " + mirrorUnit + " on laptop"; sent[0].Subject != want {
		t.Fatalf("subject = %q, want %q", sent[0].Subject, want)
	}
}

// The mail of two failures in a row says which unit, how many times, and the
// command that prints its log; it does not carry the log, which was most of the
// 299 KB the Mayor was sent in 7 days (mw-t5shinh.1).
func TestTheMailOfTwoFailuresCarriesTheCommandForTheLogNotTheLog(t *testing.T) {
	c := newMirrorUnitCycle()
	askErr := errors.New("ssh desktop: exit status 255: Host key verification failed.")
	c.run(t, askErr)
	c.run(t, askErr)
	sent := c.mails(t)
	if len(sent) != 1 {
		t.Fatalf("the Mayor has %d mails, want 1", len(sent))
	}
	body := sent[0].Body
	for _, want := range []string{
		mirrorUnit + " failed 2 times in a row",
		"journalctl --user -u " + mirrorUnit + " -n 40 --no-pager",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the body does not say %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Host key verification failed") {
		t.Errorf("the body carries the journal:\n%s", body)
	}
	if lines := strings.Count(body, "\n") + 1; lines > 8 {
		t.Errorf("the body is %d lines, want a short one:\n%s", lines, body)
	}
}

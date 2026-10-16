package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A tellRig is an EventSpring over the jobs the follower runs, each a stand-in
// that fails with the error its test sets, and a fake mailbox to read what the
// Mayor is told (mw-gq6.531).
type tellRig struct {
	*springRig
	mail *apptest.FakeMailbox
	fail map[string]error
}

func newTellRig(t *testing.T, jobs map[string]application.SpringJob) *tellRig {
	t.Helper()
	r := &tellRig{springRig: newSpringRig(t), mail: apptest.NewFakeMailbox(), fail: map[string]error{}}
	var list []application.SpringJob
	for name, job := range jobs {
		job.Name = name
		job.Every = time.Hour
		job.Reason = application.ClockReason
		job.Run = func(context.Context) error { return r.fail[name] }
		list = append(list, job)
	}
	r.spring = &application.EventSpring{Log: r.log, Host: "laptop", Jobs: list, Mailbox: r.mail, Now: r.spring.Now}
	return r
}

// pass advances the clock past every job's hour, springs, and waits for the
// passes to end.
func (r *tellRig) pass(t *testing.T) {
	t.Helper()
	r.advance(2 * time.Hour)
	r.spin(t)
	r.spring.Wait()
}

func (r *tellRig) mayorMail(t *testing.T) []application.Message {
	t.Helper()
	msgs, err := r.mail.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

func TestSpringADailyJobThatFailsMailsTheMayorOnceWithTheFailure(t *testing.T) {
	for _, name := range []string{application.NightlySuiteJobName, application.TodoRankJobName, application.SleepJobName} {
		t.Run(name, func(t *testing.T) {
			r := newTellRig(t, map[string]application.SpringJob{name: {TellFailure: true}})
			r.spin(t)
			r.fail[name] = errors.New("running the suite of millwright: exit status 1")
			r.pass(t)

			msgs := r.mayorMail(t)
			if len(msgs) != 1 {
				t.Fatalf("the Mayor has %d messages, want 1: %+v", len(msgs), msgs)
			}
			m := msgs[0]
			if m.From != name+"@laptop" || m.Subject != "job failed: "+name+" on laptop" || m.Body != "running the suite of millwright: exit status 1" {
				t.Fatalf("the mail is from %q, subject %q, body %q", m.From, m.Subject, m.Body)
			}
		})
	}
}

func TestSpringADailyJobThatFailsAgainWithinADayMailsNoSecondTime(t *testing.T) {
	r := newTellRig(t, map[string]application.SpringJob{"sleep": {TellFailure: true}})
	r.spin(t)
	r.fail["sleep"] = errors.New("first")
	r.pass(t)
	r.fail["sleep"] = errors.New("second")
	r.pass(t)
	r.pass(t)
	if msgs := r.mayorMail(t); len(msgs) != 1 || msgs[0].Body != "first" {
		t.Fatalf("the Mayor has %+v, want the one mail about the first failure", msgs)
	}

	r.advance(24 * time.Hour)
	r.fail["sleep"] = errors.New("third")
	r.pass(t)
	if msgs := r.mayorMail(t); len(msgs) != 2 {
		t.Fatalf("a day on, the Mayor has %d messages, want 2", len(msgs))
	}
}

func TestSpringAJobThatSucceedsOrThatDoesNotTellMailsNoOne(t *testing.T) {
	jobs := map[string]application.SpringJob{"sleep": {TellFailure: true}}
	for _, name := range []string{"dispatch", "grist-grind", "grist-level", "mail-notify", "millhand-tick"} {
		jobs[name] = application.SpringJob{}
	}
	r := newTellRig(t, jobs)
	r.spin(t)
	for name := range jobs {
		if name != "sleep" {
			r.fail[name] = errors.New("laptop has no room: load 20.9 of 20 cores")
		}
	}
	r.pass(t)
	if msgs := r.mayorMail(t); len(msgs) != 0 {
		t.Fatalf("the Mayor was mailed: %+v", msgs)
	}
}

func TestSpringTheJobsOnAClockOfADayOrLongerTellTheMayorWhenTheyFail(t *testing.T) {
	tell := map[string]application.SpringJob{
		"suite":           (&application.NightlySuite{}).Job(),
		"todo rank":       (&application.TodoNightly{}).Job(),
		"sleep":           (&application.SleepNightly{}).Job(),
		"digest":          (&application.DigestWeekly{}).Job(),
		"contribute-back": (&application.ContributeBack{}).Job(),
	}
	for name, job := range tell {
		if !job.TellFailure {
			t.Errorf("the %s job does not tell the Mayor when it fails", name)
		}
	}
	quiet := map[string]application.SpringJob{
		"dispatch":      application.DispatchJob(time.Hour, nil),
		"millhand-tick": application.MillhandTickJob(time.Hour, nil),
		"grist-level":   application.GristLevelJob(time.Hour, nil),
		"chain-stamp":   application.ChainStampJob(time.Minute, nil),
		"batch-land":    (&application.BatchLandings{}).Job(),
	}
	for name, job := range quiet {
		if job.TellFailure {
			t.Errorf("the %s job tells the Mayor when it fails", name)
		}
	}
}

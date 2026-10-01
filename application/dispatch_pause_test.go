package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

func control(actor, detail string) events.Event {
	return events.Event{Kind: events.KindControl, Actor: actor, Detail: detail}
}

// A pause-host event for this host stops every dispatch pass from claiming or
// starting anything until a resume-host event for it: a story ready all the
// while waits, and the pass says why.
func TestDispatchIsPausedByAPauseHostEventUntilResumed(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	log := &apptest.FakeEventLog{}
	var out bytes.Buffer
	ticks := &apptest.FakeTickLog{}
	dispatch.Events, dispatch.Out, dispatch.Log = log, &out, ticks
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	appendAll(t, log, control("mw@laptop", "pause-host vps"))
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if report.Paused == nil || report.Paused.Actor != "mw@laptop" {
		t.Fatalf("report.Paused = %+v, want the pause by mw@laptop", report.Paused)
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("a paused host started %+v", report.Started)
	}
	if d, _ := tracker.ShowStory(ctx, "mw-gq6.1"); d.Assignee != "" {
		t.Fatalf("a paused host claimed the story for %q", d.Assignee)
	}
	if !strings.Contains(out.String(), "paused") || !strings.Contains(out.String(), "mw@laptop") {
		t.Fatalf("the pass said %q, want it to say it is paused and by whom", out.String())
	}
	if lines := ticks.Lines(); len(lines) != 1 || !strings.Contains(lines[0], " "+application.DispatchLogOK+"paused") {
		t.Fatalf("the dispatch log says %q, want an ok: paused line", lines)
	}

	appendAll(t, log, control("mw@laptop", "resume-host vps"))
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching after the resume: %v", err)
	}
	if report.Paused != nil || len(report.Started) != 1 {
		t.Fatalf("after resume: paused %+v, started %d", report.Paused, len(report.Started))
	}
}

// A pause for another host leaves this one dispatching; a dry run says the
// host is paused too, and writes nothing.
func TestDispatchIgnoresAPauseForAnotherHostAndADryRunReadsOne(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	log := &apptest.FakeEventLog{}
	dispatch.Events = log
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	appendAll(t, log, control("mw@laptop", "pause-host desktop"))
	dry := dispatch
	dry.DryRun = true
	if report, err := dry.Run(ctx); err != nil || report.Paused != nil || len(report.Started) != 1 {
		t.Fatalf("a pause for the desktop stopped the vps: %+v, %v", report, err)
	}

	appendAll(t, log, control("mw@laptop", "pause-host vps"))
	if report, err := dry.Run(ctx); err != nil || report.Paused == nil || len(report.Started) != 0 {
		t.Fatalf("a dry run did not read the pause: %+v, %v", report, err)
	}
}

func TestPausedHostIsTheLatestPauseOrResumeForIt(t *testing.T) {
	ctx := context.Background()
	log := &apptest.FakeEventLog{}
	if _, paused, err := application.PausedHost(ctx, log, "vps"); err != nil || paused {
		t.Fatalf("an empty log paused the host: %v %v", paused, err)
	}
	appendAll(t, log, control("a@x", "pause-host vps"), control("b@x", "cap vps 1"), control("c@x", "pause-host desktop"))
	pause, paused, err := application.PausedHost(ctx, log, "vps")
	if err != nil || !paused || pause.Actor != "a@x" || pause.Seq != 1 {
		t.Fatalf("got %+v %v %v", pause, paused, err)
	}
	appendAll(t, log, control("d@x", "resume-host vps"))
	if _, paused, _ := application.PausedHost(ctx, log, "vps"); paused {
		t.Fatal("still paused after resume-host")
	}
}

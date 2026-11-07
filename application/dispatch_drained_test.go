package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A host held at cap 0 is drained for a restart (mw-gq6.1044): it starts no
// session, but its pass still syncs and closes out, and each ready story is
// passed over under the gate "drained", not the "cap" one.
func TestADispatchAtCapZeroSyncsClosesOutStartsNothingAndPassesReadyStoriesAsDrained(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	var out bytes.Buffer
	ticks := &apptest.FakeTickLog{}
	sync := &stubSync{}
	dispatch.Cap, dispatch.Sync, dispatch.Out, dispatch.Log = 0, sync, &out, ticks
	now := time.Date(2026, 9, 30, 23, 20, 0, 0, time.UTC)
	dispatch.Now = func() time.Time { return now }
	tracker.Clock = func() time.Time { return now }
	dispatch.Landing = &fakeRetryLanding{MergedTip: landedTip}
	claimedStory(t, tracker, "mw-gq6.9", 1)
	if err := tracker.SetLeaseExpires("mw-gq6.9", now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("setting the lease: %v", err)
	}
	runner.Exit(startOldSession(t, runner, "mw-gq6.9"), 1)
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("a dispatch at cap 0 failed: %v", err)
	}
	if sync.runs != 1 {
		t.Fatalf("the sync ran %d times at cap 0, want once", sync.runs)
	}
	if len(report.LandedAlready) != 1 || report.LandedAlready[0].StoryID != "mw-gq6.9" {
		t.Fatalf("the dead pane of mw-gq6.9 was not closed out at cap 0: %+v", report.LandedAlready)
	}
	if len(report.Started) != 0 || len(report.Failed) != 0 {
		t.Fatalf("a drained host started %+v, failed %+v", report.Started, report.Failed)
	}
	if d, _ := tracker.ShowStory(ctx, "mw-gq6.1"); d.Assignee != "" {
		t.Fatalf("a drained host claimed the story for %q", d.Assignee)
	}
	if len(report.Passed) != 1 || report.Passed[0].StoryID != "mw-gq6.1" || report.Passed[0].Gate != "drained" {
		t.Fatalf("the ready story was passed as %+v, want one under the gate drained", report.Passed)
	}
	if lines := ticks.Lines(); len(lines) != 1 || !strings.Contains(lines[0], " "+application.DispatchLogOK+"capped at 0") {
		t.Fatalf("the dispatch log says %q, want an ok: capped at 0 line", lines)
	}
}

// A drained host stays drained while grist is in use: the grist cap is never
// above the host's own, and a host's own of 0 is not lifted to the 1 that
// "never under one" says for a host that runs any.
func TestTheGristCapOfADrainedHostIsZeroToo(t *testing.T) {
	lock := &apptest.FakeGristLock{}
	lock.Hold()
	if pulse := pulseOf(t, roomOf(roomRuns{}, lock), 0); !pulse.Active || pulse.Cap != 0 {
		t.Fatalf("grist in flight on a host at cap 0 gave the cap %d, want 0: %+v", pulse.Cap, pulse)
	}
}

func TestADryRunAtCapZeroSaysTheHostIsDrained(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, _, _ := aFactory(t)
	dispatch.Cap, dispatch.DryRun = 0, true
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("a dry run at cap 0 failed: %v", err)
	}
	printed := report.String()
	if !strings.Contains(printed, "drained") {
		t.Fatalf("the dry run said:\n%s\nwant it to say the host is drained", printed)
	}
	if len(report.Started) != 0 {
		t.Fatalf("a drained dry run would start %+v", report.Started)
	}
}

// mw status says a host held at cap 0 is drained, where a zero cap leaves the
// HOST line out.
func TestStatusSaysAHostAtCapZeroIsDrained(t *testing.T) {
	ctx := context.Background()
	report, err := application.Status{
		Tracker: apptest.NewFakeTracker(), Host: "vps", Seat: "builder", Cap: 0, Drained: true,
		Now: func() time.Time { return statusNow },
	}.Run(ctx)
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if printed := report.String(); !strings.Contains(printed, "host: drained (cap 0)") {
		t.Fatalf("status did not say the host is drained:\n%s", printed)
	}
}

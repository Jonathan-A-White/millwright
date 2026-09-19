package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

var watchNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func aWatch(world *apptest.FakeWatch) application.Watch {
	return application.Watch{
		Probes:   world,
		Settings: application.WatchSettings{SSH: "vps-ssh", Host: "vps", Outside: []string{"https://one.example"}, Blog: "https://blog.example"},
		Now:      func() time.Time { return watchNow },
	}
}

func TestAWakeCalledForLeavesWithSixAndAnythingElseWithItsOwn(t *testing.T) {
	if got := application.ExitStatus(&application.WatchWake{Line: "down signs=none"}); got != 6 {
		t.Errorf("expected a wake to leave with 6, got %d", got)
	}
	if got := application.ExitStatus(errors.New("boom")); got != 1 {
		t.Errorf("expected any other failure to leave with 1, got %d", got)
	}
}

func TestHealthLinesMwWatchCannotVouchForWakeSomebody(t *testing.T) {
	fresh := watchNow.Add(-time.Minute).Format(time.RFC3339)
	for health, want := range map[string]string{
		"":                                      "stale",
		"\n\n":                                  "stale",
		fresh + " load1=1 mayor=alive\n":        "unwell no-verdict",
		fresh + " load1=1 verdict=unwell\n":     "unwell",
		fresh + " load1=1 verdict=unwell:\n":    "unwell",
		"MOTD line\n" + fresh + " verdict=ok\n": "ok",
	} {
		world := apptest.NewFakeWatch()
		world.Answering["https://one.example"] = true
		world.Health = health

		report, _ := aWatch(world).Run(context.Background())
		if report.Line != want {
			t.Errorf("health %q: expected %q, got %q", health, want, report.Line)
		}
	}
}

func TestOutsidePlacesAreAskedOnlyUntilOneAnswers(t *testing.T) {
	world := apptest.NewFakeWatch()
	world.Answering["https://one.example"] = true
	world.Health = watchNow.Format(time.RFC3339) + " verdict=ok\n"

	watch := aWatch(world)
	watch.Settings.Outside = []string{"https://one.example", "https://two.example"}
	if _, err := watch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := world.Reached(); len(got) != 1 {
		t.Errorf("expected one place asked once one had answered, got %v", got)
	}
}

func TestASyncNoteThatCannotBeReadIsNoSignOfLifeAndNoFailure(t *testing.T) {
	world := apptest.NewFakeWatch()
	world.Answering["https://one.example"] = true
	world.SSHFails = true
	tracker := apptest.NewFakeTracker()
	tracker.Err = errors.New("no beads database")

	watch := aWatch(world)
	watch.Notes = tracker
	report, err := watch.Run(context.Background())
	if err != nil || report.Line != "unreachable-once signs=none" {
		t.Errorf("expected the line all the same, got %q, %v", report.Line, err)
	}
}

func TestAMemoryThatCannotBeSavedIsAFailureNotAFinding(t *testing.T) {
	world := apptest.NewFakeWatch()
	world.Answering["https://one.example"] = true
	world.SSHFails = true
	world.SaveErr = errors.New("read-only file system")

	_, err := aWatch(world).Run(context.Background())
	if err == nil || application.ExitStatus(err) != 1 {
		t.Errorf("expected a plain failure, got %v", err)
	}
	if got := world.Log(); len(got) != 0 {
		t.Errorf("expected nothing logged of a run that failed, got %q", got)
	}
}

package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestTheFactoryRigsBuildRestartsTheFollowerAndSaysSo(t *testing.T) {
	units := &apptest.FakeUnitRestarter{}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if len(units.Asked) != 1 || units.Asked[0] != "mw-view-follow.service" {
		t.Fatalf("expected one try-restart of mw-view-follow.service, got %v", units.Asked)
	}
	if want := "restarted mw-view-follow.service on the new build"; len(notes) != 1 || !strings.Contains(notes[0], want) {
		t.Errorf("expected a note saying %q, got %q", want, notes)
	}
	if len(failed) != 0 {
		t.Errorf("expected no failure, got %q", failed)
	}
}

func TestAnotherRigsBuildRestartsNothing(t *testing.T) {
	units := &apptest.FakeUnitRestarter{}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "elsewhere")

	if len(units.Asked) != 0 || len(notes) != 0 || len(failed) != 0 {
		t.Fatalf("expected nothing asked or said, got asked %v, notes %q, failed %q", units.Asked, notes, failed)
	}
}

func TestAUnitThatIsNotRunningIsLeftBeAndNotSaidRestarted(t *testing.T) {
	units := &apptest.FakeUnitRestarter{NotRunning: map[string]bool{"mw-view-follow.service": true}}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if len(notes) != 0 || len(failed) != 0 {
		t.Fatalf("expected nothing said of a unit that is not running, got notes %q, failed %q", notes, failed)
	}
}

func TestARestartThatFailsIsSaidAndCounted(t *testing.T) {
	units := &apptest.FakeUnitRestarter{Fails: map[string]error{"mw-view-follow.service": errors.New("Job failed\nsee journalctl")}}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "millwright")

	want := "mw-view-follow.service could not be restarted on the new build and still runs the old mw: Job failed"
	if len(notes) != 1 || !strings.Contains(notes[0], want) || strings.Contains(notes[0], "journalctl") {
		t.Errorf("expected one note saying %q, got %q", want, notes)
	}
	if len(failed) != 1 || failed[0] != notes[0] {
		t.Errorf("expected the failure listed, got %q", failed)
	}
}

func TestASelfUpdateThatBuildsRestartsTheFollowerAndAFailedBuildRestartsNothing(t *testing.T) {
	tick, _, after, _ := anUpdatingTick(t)
	units := &apptest.FakeUnitRestarter{}
	tick.SelfUpdate.Units = units
	after.ran = application.Ran{Command: "make build", Status: 2, Output: "boom"}

	report, err := tick.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(units.Asked) != 0 || strings.Contains(report.Line, "restarted") {
		t.Fatalf("expected a failed build to restart nothing, asked %v, line %q", units.Asked, report.Line)
	}

	after.ran = application.Ran{Command: "make build"}
	report, err = tick.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(units.Asked) != 1 || !strings.Contains(report.Line, "restarted mw-view-follow.service on the new build") {
		t.Fatalf("expected one restart said in the line, asked %v, line %q", units.Asked, report.Line)
	}
}

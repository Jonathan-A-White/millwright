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

func TestAFactoryBuildReloadsTheUserManagerBeforeItRestartsAndSaysSo(t *testing.T) {
	units := &apptest.FakeUnitRestarter{Stale: []string{"mw-dispatch.service", "mw-millhand-tick.service"}}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if want := []string{"reload", "restart mw-view-follow.service"}; strings.Join(units.Events, ",") != strings.Join(want, ",") {
		t.Fatalf("expected the reload before the restart, got %v", units.Events)
	}
	want := "ran systemctl --user daemon-reload: mw-dispatch.service, mw-millhand-tick.service had changed on disk and are loaded now"
	if len(notes) != 2 || notes[0] != want {
		t.Errorf("expected the reload said first as %q, got %q", want, notes)
	}
	if len(failed) != 0 {
		t.Errorf("expected no failure, got %q", failed)
	}
}

func TestABuildThatChangedNoUnitReloadsNothingAndSaysNothingOfIt(t *testing.T) {
	units := &apptest.FakeUnitRestarter{}

	notes, _ := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if units.Reloaded != 0 {
		t.Errorf("expected no daemon-reload, got %d", units.Reloaded)
	}
	for _, note := range notes {
		if strings.Contains(note, "daemon-reload") {
			t.Errorf("expected no word of a reload, got %q", note)
		}
	}
}

func TestAnotherRigsBuildReloadsNothingEvenWhenAUnitIsStale(t *testing.T) {
	units := &apptest.FakeUnitRestarter{Stale: []string{"mw-dispatch.service"}}

	application.RestartFactoryUnits(context.Background(), units, "elsewhere")

	if units.Reloaded != 0 {
		t.Errorf("expected no daemon-reload for another rig's build, got %d", units.Reloaded)
	}
}

func TestAReloadThatFailsIsSaidAndTheRestartStillRuns(t *testing.T) {
	units := &apptest.FakeUnitRestarter{Stale: []string{"mw-dispatch.service"}, ReloadErr: errors.New("reloading: exit status 1: Access denied\nmore")}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if len(notes) == 0 || !strings.Contains(notes[0], "daemon-reload could not be run") || !strings.Contains(notes[0], "mw-dispatch.service") || !strings.Contains(notes[0], "Access denied") || strings.Contains(notes[0], "more") {
		t.Errorf("expected the failed reload said first, naming the unit, got %q", notes)
	}
	if len(units.Asked) != 1 {
		t.Errorf("expected the follower still restarted, got %v", units.Asked)
	}
	if len(failed) != 0 {
		t.Errorf("expected a failed reload not to be a failed restart, got %q", failed)
	}
}

func TestASelfUpdateThatBuildsReloadsStaleUnitsAndSaysSoInTheLine(t *testing.T) {
	tick, _, after, _ := anUpdatingTick(t)
	units := &apptest.FakeUnitRestarter{Stale: []string{"mw-millhand-tick.service"}}
	tick.SelfUpdate.Units = units
	after.ran = application.Ran{Command: "make build"}

	report, err := tick.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if units.Reloaded != 1 || !strings.Contains(report.Line, "ran systemctl --user daemon-reload: mw-millhand-tick.service") {
		t.Fatalf("expected one reload said in the line, reloaded %d, line %q", units.Reloaded, report.Line)
	}
}

func TestARootHostsChangedSystemUnitIsReloadedBeforeTheRestartAndTheNoteSaysSo(t *testing.T) {
	units := &apptest.FakeUnitRestarter{SystemStale: []string{"mw-seat-tmux.service"}}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if len(failed) != 0 {
		t.Errorf("expected no failure, got %q", failed)
	}
	if units.SystemReloaded != 1 {
		t.Errorf("expected one system daemon-reload, got %d", units.SystemReloaded)
	}
	if len(units.Events) < 2 || units.Events[0] != "reload system" || units.Events[1] != "restart mw-view-follow.service" {
		t.Errorf("expected the system reload before the restart, events were %q", units.Events)
	}
	found := false
	for _, note := range notes {
		found = found || (strings.Contains(note, "ran systemctl daemon-reload") && strings.Contains(note, "mw-seat-tmux.service") && !strings.Contains(note, "--user"))
	}
	if !found {
		t.Errorf("expected a note saying systemctl daemon-reload ran for mw-seat-tmux.service, got %q", notes)
	}
}

func TestNoSystemReloadIsRunOrSaidWhenNoSystemUnitChanged(t *testing.T) {
	units := &apptest.FakeUnitRestarter{}

	notes, _ := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if units.SystemReloaded != 0 {
		t.Errorf("expected no system daemon-reload, got %d", units.SystemReloaded)
	}
	for _, note := range notes {
		if strings.Contains(note, "daemon-reload") {
			t.Errorf("expected no note of a reload, got %q", note)
		}
	}
}

func TestASystemReloadThatFailsIsSaidWithTheUnitsAndTheHandFix(t *testing.T) {
	units := &apptest.FakeUnitRestarter{SystemStale: []string{"mw-doctor.timer"}, SystemReloadErr: errors.New("Access denied\nmore")}

	notes, failed := application.RestartFactoryUnits(context.Background(), units, "millwright")

	if len(failed) != 0 {
		t.Errorf("a failed reload is not a failed restart, got %q", failed)
	}
	found := false
	for _, note := range notes {
		found = found || (strings.Contains(note, "systemctl daemon-reload could not be run") && strings.Contains(note, "mw-doctor.timer") && strings.Contains(note, "Access denied"))
	}
	if !found {
		t.Errorf("expected a note of the failed system reload, got %q", notes)
	}
}

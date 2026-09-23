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

var doctorNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func aDoctor(state *apptest.FakeDoctorState, log *apptest.FakeDoctorLog, now *time.Time, checks ...application.DoctorCheck) application.Doctor {
	return application.Doctor{
		Checks: checks,
		State:  state,
		Log:    log,
		Now:    func() time.Time { return *now },
	}
}

func TestAnOKProbeLogsOKAndCuresNothing(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK}

	report, err := aDoctor(state, log, &now, check).Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if check.Cures() != 0 {
		t.Errorf("expected no cure, got %d", check.Cures())
	}
	if want := "2026-09-23T12:00:00Z widget ok"; !contains(log.Lines(), want) {
		t.Errorf("expected the log to hold %q, got %v", want, log.Lines())
	}
	if application.ExitStatus(err) != 0 {
		t.Errorf("expected exit 0, got %d", application.ExitStatus(err))
	}
	if report.Faulty() {
		t.Errorf("expected the report not to be faulty, got %+v", report)
	}
}

func TestAFaultyProbeCuresOnceAndLeavesWithZero(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: 10 * time.Minute, Cap: 3, Way: "realign it by hand",
	}

	_, err := aDoctor(state, log, &now, check).Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if check.Cures() != 1 {
		t.Errorf("expected one cure, got %d", check.Cures())
	}
	got := log.Lines()
	if len(got) != 1 || !strings.Contains(got[0], "widget cured misaligned") || !strings.Contains(got[0], "realign it by hand") {
		t.Errorf("expected a cured line naming the reason and the way back, got %v", got)
	}
	if application.ExitStatus(err) != 0 {
		t.Errorf("expected exit 0 for a cured fault, got %d", application.ExitStatus(err))
	}
}

func TestTheSameFaultWithinTheDampersWaitIsDampedNotCured(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: 10 * time.Minute, Cap: 3,
	}
	doctor := aDoctor(state, log, &now, check)

	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	now = now.Add(5 * time.Minute)

	_, err := doctor.Run(context.Background(), "", false)
	if check.Cures() != 1 {
		t.Errorf("expected no second cure, got %d cures", check.Cures())
	}
	if got := log.Lines(); len(got) != 2 || !strings.Contains(got[1], "widget damped") {
		t.Errorf("expected the second line to say damped, got %v", got)
	}
	if application.ExitStatus(err) != application.DoctorFaultExit {
		t.Errorf("expected exit %d for a damped fault, got %d", application.DoctorFaultExit, application.ExitStatus(err))
	}
}

func TestACureThatFailsIsLoggedAndLeavesWithTheFaultExit(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: 10 * time.Minute, Cap: 3, CureErr: errors.New("no wrench found"),
	}

	_, err := aDoctor(state, log, &now, check).Run(context.Background(), "", false)
	got := log.Lines()
	if len(got) != 1 || !strings.Contains(got[0], "widget cure-failed no wrench found") {
		t.Errorf("expected a cure-failed line naming the error, got %v", got)
	}
	if application.ExitStatus(err) != application.DoctorFaultExit {
		t.Errorf("expected exit %d, got %d", application.DoctorFaultExit, application.ExitStatus(err))
	}
}

func TestACapPerEpisodeDampsOnceSpentAndResetsWhenTheProbeSaysOK(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: time.Minute, Cap: 2,
	}
	doctor := aDoctor(state, log, &now, check)

	// Two cures, the wait cleared between them.
	for i := 0; i < 2; i++ {
		if _, err := doctor.Run(context.Background(), "", false); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		now = now.Add(time.Hour)
	}
	if check.Cures() != 2 {
		t.Fatalf("expected 2 cures spent, got %d", check.Cures())
	}

	// A third fault is damped, the cap already spent, and the count stays.
	_, err := doctor.Run(context.Background(), "", false)
	if check.Cures() != 2 {
		t.Errorf("expected the cure count to stay at 2, got %d", check.Cures())
	}
	if application.ExitStatus(err) != application.DoctorFaultExit {
		t.Errorf("expected the capped episode to leave with the fault exit, got %d", application.ExitStatus(err))
	}
	if got := state.Episode("widget"); got.Cures != 2 {
		t.Errorf("expected the state to still hold 2 cures, got %+v", got)
	}

	// The probe says ok: the episode resets.
	check.Verdict = application.DoctorOK
	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("ok run: %v", err)
	}
	if got := state.Episode("widget"); got.Cures != 0 {
		t.Errorf("expected the episode reset, got %+v", got)
	}

	// A later fault is cured again, not damped.
	check.Verdict = application.DoctorFaulty
	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("run after reset: %v", err)
	}
	if check.Cures() != 3 {
		t.Errorf("expected a third cure after the reset, got %d", check.Cures())
	}
}

func TestDryRunNamesTheCureAndTheWayBackAndChangesNothing(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: 10 * time.Minute, Cap: 3, Way: "realign it by hand",
	}

	report, err := aDoctor(state, log, &now, check).Run(context.Background(), "", true)
	if err != nil {
		t.Fatalf("running dry: %v", err)
	}
	if check.Cures() != 0 {
		t.Errorf("expected no cure to run, got %d", check.Cures())
	}
	if len(log.Lines()) != 0 {
		t.Errorf("expected the log untouched, got %v", log.Lines())
	}
	if state.Saves() != 0 {
		t.Errorf("expected the state untouched, got %d save(s)", state.Saves())
	}
	if len(report.Results) != 1 {
		t.Fatalf("expected one result, got %+v", report.Results)
	}
	printed := report.String()
	if !strings.Contains(printed, "widget") || !strings.Contains(printed, "misaligned") || !strings.Contains(printed, "realign it by hand") {
		t.Errorf("expected the check, the cure and the way back named, got %q", printed)
	}
}

func TestNamingAMissingCheckIsRefused(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK}

	_, err := aDoctor(state, log, &now, check).Run(context.Background(), "gadget", false)
	if err == nil || !strings.Contains(err.Error(), "gadget") {
		t.Fatalf("expected a refusal naming gadget, got %v", err)
	}
}

func TestNamingOneCheckRunsOnlyThatOne(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	widget := &apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK}
	gadget := &apptest.FakeDoctorCheck{CheckName: "gadget", Verdict: application.DoctorOK}

	report, err := aDoctor(state, log, &now, widget, gadget).Run(context.Background(), "gadget", false)
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if widget.Probes() != 0 {
		t.Errorf("expected widget not to be probed, it was %d time(s)", widget.Probes())
	}
	if gadget.Probes() != 1 {
		t.Errorf("expected gadget probed once, got %d", gadget.Probes())
	}
	if len(report.Results) != 1 || report.Results[0].Check != "gadget" {
		t.Errorf("expected only gadget in the report, got %+v", report.Results)
	}
}

// TestATurnOKOnOneHostDoesNotClearAnotherHostsNote is the regression for
// mw-i80dx.8: two hosts' doctors shared one note key per check, so one host
// turning a check ok cleared the other host's still-faulty note. Each host's
// note must be kept and cleared under its own key.
func TestATurnOKOnOneHostDoesNotClearAnotherHostsNote(t *testing.T) {
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow

	laptopCheck := &apptest.FakeDoctorCheck{
		CheckName: "beads-size", Verdict: application.DoctorFaulty, Reason: "vault over budget",
		Wait: 0, Cap: 1,
	}
	laptop := application.Doctor{
		Checks: application.DoctorChecks{laptopCheck},
		State:  apptest.NewFakeDoctorState(),
		Log:    &apptest.FakeDoctorLog{},
		Notes:  notes,
		Host:   "laptop",
		Now:    func() time.Time { return now },
	}
	// First run cures the fault, spending the cap; the second finds it faulty
	// again with the cap already spent, so it is damped and a note is written.
	if _, err := laptop.Run(context.Background(), "", false); err != nil {
		t.Fatalf("laptop first run: %v", err)
	}
	if _, err := laptop.Run(context.Background(), "", false); application.ExitStatus(err) != application.DoctorFaultExit {
		t.Fatalf("laptop second run: expected the fault exit, got %v", err)
	}

	laptopKey := application.DoctorNoteKey("laptop", "beads-size")
	if _, ok := notes.Get(laptopKey); !ok {
		t.Fatalf("expected %s to be written", laptopKey)
	}

	vpsCheck := &apptest.FakeDoctorCheck{CheckName: "beads-size", Verdict: application.DoctorOK}
	vps := application.Doctor{
		Checks: application.DoctorChecks{vpsCheck},
		State:  apptest.NewFakeDoctorState(),
		Log:    &apptest.FakeDoctorLog{},
		Notes:  notes,
		Host:   "vps",
		Now:    func() time.Time { return now },
	}
	if _, err := vps.Run(context.Background(), "", false); err != nil {
		t.Fatalf("vps run: %v", err)
	}

	if _, ok := notes.Get(laptopKey); !ok {
		t.Errorf("expected the laptop's own note to survive the vps doctor turning ok, got it cleared")
	}
	vpsKey := application.DoctorNoteKey("vps", "beads-size")
	if _, ok := notes.Get(vpsKey); ok {
		t.Errorf("expected no note written for the vps's own ok check, got one")
	}
}

func contains(lines []string, substr string) bool {
	for _, line := range lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

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

func TestAnOKResultWithAReasonPrintsItAndOneWithoutDoesNot(t *testing.T) {
	with := application.DoctorResult{Check: "widget", Verdict: "ok", Reason: "desktop silent 1h00m (threshold 2h)"}
	if got, want := with.String(), "widget: ok (desktop silent 1h00m (threshold 2h))"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	without := application.DoctorResult{Check: "widget", Verdict: "ok"}
	if got, want := without.String(), "widget: ok"; got != want {
		t.Errorf("expected %q, got %q", want, got)
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

func TestAQuietCheckDampedByTheWaitAfterAGoodCureIsLoggedAndLeavesWithZero(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: 10 * time.Minute, Cap: 3, QuietCooling: true,
	}
	doctor := aDoctor(state, log, &now, check)
	doctor.Notes = notes

	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	now = now.Add(5 * time.Minute)

	report, err := doctor.Run(context.Background(), "", false)
	if check.Cures() != 1 {
		t.Errorf("expected no second cure, got %d cures", check.Cures())
	}
	if got := log.Lines(); len(got) != 2 || !strings.Contains(got[1], "widget damped") {
		t.Errorf("expected the second line to say damped, got %v", got)
	}
	if err != nil || application.ExitStatus(err) != 0 {
		t.Errorf("expected exit 0 for a damped wait after a good cure, got %d (%v)", application.ExitStatus(err), err)
	}
	if report.Faulty() {
		t.Errorf("expected the report not to be faulty, got %v", report.FaultyNames())
	}
	if notes.Sets() != 0 {
		t.Errorf("expected no note, got %d sets", notes.Sets())
	}
}

func TestAQuietCheckStillLeavesWithTheFaultExitOnceItsCapIsSpent(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: time.Minute, Cap: 2, QuietCooling: true,
	}
	doctor := aDoctor(state, log, &now, check)
	for i := 0; i < 2; i++ {
		if _, err := doctor.Run(context.Background(), "", false); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		now = now.Add(time.Hour)
	}

	_, err := doctor.Run(context.Background(), "", false)
	if check.Cures() != 2 {
		t.Errorf("expected the cure count to stay at 2, got %d", check.Cures())
	}
	if application.ExitStatus(err) != application.DoctorFaultExit {
		t.Errorf("expected the capped episode to leave with the fault exit, got %d", application.ExitStatus(err))
	}
}

func TestAQuietCheckWhoseCureFailedStillLeavesWithTheFaultExitInsideTheWait(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: 10 * time.Minute, Cap: 3, QuietCooling: true, CureErr: errors.New("no wrench found"),
	}
	doctor := aDoctor(state, log, &now, check)

	if _, err := doctor.Run(context.Background(), "", false); application.ExitStatus(err) != application.DoctorFaultExit {
		t.Fatalf("expected the failed cure to leave with the fault exit, got %d", application.ExitStatus(err))
	}
	now = now.Add(5 * time.Minute)

	_, err := doctor.Run(context.Background(), "", false)
	if check.Cures() != 1 {
		t.Errorf("expected no second cure, got %d cures", check.Cures())
	}
	if application.ExitStatus(err) != application.DoctorFaultExit {
		t.Errorf("expected exit %d for a damped wait after a failed cure, got %d", application.DoctorFaultExit, application.ExitStatus(err))
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

// A cache several Builders regrow past budget every few minutes is cured
// each time it is due: three good cures in one hour do not spend a cap, so
// the fourth growth is cured too, not damped (mw-gq6.478).
func TestACureThatPutTheFaultRightEndsItsEpisodeSoARegrowthIsCuredNotDamped(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "regrown",
		Wait: 5 * time.Minute, Cap: 3, EndsEpisode: true,
	}
	doctor := aDoctor(state, log, &now, check)
	doctor.Notes = notes

	for i := 0; i < 4; i++ {
		report, err := doctor.Run(context.Background(), "", false)
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if got := report.Results[0].Verdict; got != "cured" {
			t.Fatalf("run %d: expected cured, got %s", i, got)
		}
		now = now.Add(15 * time.Minute)
	}
	if check.Cures() != 4 {
		t.Errorf("expected four cures inside the hour, got %d", check.Cures())
	}
	if got := state.Episode("widget"); got.Cures != 0 || got.LastCure.IsZero() {
		t.Errorf("expected each good cure to end the episode but keep its time for the wait, got %+v", got)
	}
	if notes.Sets() != 0 {
		t.Errorf("expected no note, got %d sets", notes.Sets())
	}
}

// The wait between two cures still holds for a check whose good cure ends
// its episode.
func TestACureThatEndsItsEpisodeStillWaitsBeforeTheNext(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "regrown",
		Wait: 5 * time.Minute, Cap: 3, EndsEpisode: true, QuietCooling: true,
	}
	doctor := aDoctor(state, log, &now, check)

	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	now = now.Add(time.Minute)
	report, err := doctor.Run(context.Background(), "", false)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := report.Results[0].Verdict; got != "damped" || check.Cures() != 1 {
		t.Errorf("expected the second run inside the wait damped with one cure spent, got %s and %d cures", got, check.Cures())
	}
}

// A real fault still damps: a check whose cure says it put nothing right, or
// fails, spends its cap and is left faulty, the way it always was.
func TestACheckWhoseCureFixesNothingOrFailsIsStillDampedAtItsCap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cureErr error
	}{
		{"freed nothing", nil},
		{"failed", errors.New("no wrench found")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := apptest.NewFakeDoctorState()
			log := &apptest.FakeDoctorLog{}
			now := doctorNow
			check := &apptest.FakeDoctorCheck{
				CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "regrown",
				Wait: 5 * time.Minute, Cap: 3, QuietCooling: true, CureErr: tc.cureErr,
			}
			doctor := aDoctor(state, log, &now, check)
			for i := 0; i < 3; i++ {
				_, _ = doctor.Run(context.Background(), "", false)
				now = now.Add(15 * time.Minute)
			}

			report, err := doctor.Run(context.Background(), "", false)
			if got := report.Results[0].Verdict; got != "damped" {
				t.Errorf("expected the fourth fault damped, got %s", got)
			}
			if check.Cures() != 3 {
				t.Errorf("expected three cures spent, got %d", check.Cures())
			}
			if application.ExitStatus(err) != application.DoctorFaultExit {
				t.Errorf("expected exit %d, got %d", application.DoctorFaultExit, application.ExitStatus(err))
			}
		})
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

func TestAnOKReasonIsLoggedOnceAndNotRepeatedWhileNothingChanges(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "timers", Verdict: application.DoctorOK,
		Reason: "n/a: none of these units is installed here",
	}
	d := application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  state,
		Log:    log,
		Notes:  notes,
		Host:   "vps",
		Now:    func() time.Time { return now },
	}

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if want := "2026-09-23T12:00:00Z timers ok n/a: none of these units is installed here"; !contains(log.Lines(), want) {
		t.Errorf("expected the log to hold %q, got %v", want, log.Lines())
	}
	if len(log.Lines()) != 1 {
		t.Fatalf("expected one log line after the first run, got %v", log.Lines())
	}
	firstClears := notes.Clears()

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(log.Lines()) != 1 {
		t.Errorf("expected no new log line on the second run, got %v", log.Lines())
	}
	if notes.Clears() != firstClears {
		t.Errorf("expected no new kv write on the second run, clears went from %d to %d", firstClears, notes.Clears())
	}

	// A changed reason is said again.
	check.Reason = "n/a: still none of these units is installed here, differently"
	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if len(log.Lines()) != 2 {
		t.Errorf("expected a new log line once the reason changes, got %v", log.Lines())
	}
}

func TestACannotTellReasonIsLoggedOnceAndNotRepeatedWhileNothingChanges(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "wifi", Verdict: application.DoctorCannotTell,
		Reason: "powershell not found: this host is not Windows-backed",
	}
	d := application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  state,
		Log:    log,
		Notes:  notes,
		Host:   "vps",
		Now:    func() time.Time { return now },
	}

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if want := "2026-09-23T12:00:00Z wifi cannot-tell powershell not found: this host is not Windows-backed"; !contains(log.Lines(), want) {
		t.Errorf("expected the log to hold %q, got %v", want, log.Lines())
	}
	if len(log.Lines()) != 1 {
		t.Fatalf("expected one log line after the first run, got %v", log.Lines())
	}
	if notes.Sets() != 1 {
		t.Fatalf("expected one kv write after the first run, got %d", notes.Sets())
	}
	firstNote, ok := notes.Get(application.DoctorNoteKey("vps", "wifi"))
	if !ok {
		t.Fatalf("expected a note written for wifi")
	}

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if len(log.Lines()) != 1 {
		t.Errorf("expected no new log line while the reason repeats, got %v", log.Lines())
	}
	if notes.Sets() != 1 {
		t.Errorf("expected no new kv write while the reason repeats, sets went from 1 to %d", notes.Sets())
	}
	if got, ok := notes.Get(application.DoctorNoteKey("vps", "wifi")); !ok || got != firstNote {
		t.Errorf("expected the note left in place unrewritten, got %q (was %q)", got, firstNote)
	}

	// A changed reason is said again.
	check.Reason = "powershell not found: some other reason"
	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("fourth run: %v", err)
	}
	if len(log.Lines()) != 2 {
		t.Errorf("expected a new log line once the reason changes, got %v", log.Lines())
	}
	if notes.Sets() != 2 {
		t.Errorf("expected a new kv write once the reason changes, got %d sets", notes.Sets())
	}

	// A run answering ok clears the note.
	check.Verdict = application.DoctorOK
	check.Reason = ""
	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("fifth run: %v", err)
	}
	if _, ok := notes.Get(application.DoctorNoteKey("vps", "wifi")); ok {
		t.Errorf("expected the note cleared once the check answers ok")
	}
}

func TestAPlainOKWithNoReasonIsStillLoggedEveryRun(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	now := doctorNow
	check := &apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK}
	d := aDoctor(state, log, &now, check)

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(log.Lines()) != 2 {
		t.Errorf("expected a plain ok logged every run, got %v", log.Lines())
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

func TestACappedFaultWhoseReasonChangesEveryRunIsNotedOncePerEpisode(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "beads-size", Verdict: application.DoctorFaulty,
		Wait: 0, Cap: 1,
	}
	d := application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  state,
		Log:    log,
		Notes:  notes,
		Host:   "laptop",
		Now:    func() time.Time { return now },
	}
	ctx := context.Background()
	key := application.DoctorNoteKey("laptop", "beads-size")
	dampedLines := func() int {
		n := 0
		for _, line := range log.Lines() {
			if strings.Contains(line, "beads-size damped") {
				n++
			}
		}
		return n
	}

	// The cap is already spent when the three runs begin. Each finds the
	// fault with a reason whose numbers have moved, as a size in bytes does.
	if err := state.Save(ctx, "beads-size", application.DoctorEpisode{Cures: 1, LastCure: now}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	for i := 1; i <= 3; i++ {
		check.Reason = "the vault is " + strings.Repeat("9", i) + " bytes"
		now = now.Add(5 * time.Minute)
		if _, err := d.Run(ctx, "", false); application.ExitStatus(err) != application.DoctorFaultExit {
			t.Fatalf("run %d: expected the fault exit, got %v", i, err)
		}
	}
	if got := dampedLines(); got != 3 {
		t.Errorf("expected three damped log lines, got %d in %v", got, log.Lines())
	}
	if got := notes.Sets(); got != 1 {
		t.Errorf("expected one SetNote across three damped runs, got %d", got)
	}
	if value, _ := notes.Get(key); !strings.Contains(value, "9 bytes") {
		t.Errorf("expected the note to hold the first damped reason, got %q", value)
	}

	// The probe says ok: the episode ends and the note is cleared.
	check.Verdict = application.DoctorOK
	check.Reason = ""
	now = now.Add(5 * time.Minute)
	if _, err := d.Run(ctx, "", false); err != nil {
		t.Fatalf("ok run: %v", err)
	}
	if _, ok := notes.Get(key); ok {
		t.Errorf("expected the note cleared once the probe said ok")
	}

	// The fault returns: cured once, then capped again, and a new episode
	// notes again.
	check.Verdict = application.DoctorFaulty
	for i := 1; i <= 2; i++ {
		check.Reason = "the vault is back at " + strings.Repeat("8", i) + " bytes"
		now = now.Add(5 * time.Minute)
		d.Run(ctx, "", false)
	}
	if got := notes.Sets(); got != 2 {
		t.Errorf("expected a second SetNote for the new episode, got %d in all", got)
	}
	if _, ok := notes.Get(key); !ok {
		t.Errorf("expected the new episode's note to be written")
	}
}

type fakeDamperCounter struct {
	report application.DampersReport
	err    error
}

func (f fakeDamperCounter) DamperCounts(context.Context) (application.DampersReport, error) {
	return f.report, f.err
}

func TestDoctorRepeatsTheDamperCounts(t *testing.T) {
	now := doctorNow
	var out strings.Builder
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now,
		&apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK})
	doctor.Out = &out
	doctor.Dampers = fakeDamperCounter{report: application.ReadDampers(application.DamperFacts{Now: now}, application.DamperLimits{})}
	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("running: %v", err)
	}
	for _, want := range []string{"widget: ok", "dampers: ready P1 0 (limit 10)", "his review queue 0 (limit 8)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected %q in:\n%s", want, out.String())
		}
	}

	out.Reset()
	doctor.Dampers = fakeDamperCounter{err: errors.New("no tracker")}
	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("an unreadable count is no fault, got %v", err)
	}
	if !strings.Contains(out.String(), "dampers: could not be read (no tracker)") {
		t.Errorf("expected the failure said on the line, got:\n%s", out.String())
	}
}

// The dampers are the Mayor's planning signal, not a fault of this host: a
// queue over its limit is said on the dampers line and nothing else, and mw
// doctor still leaves with 0, so a timer's OnFailure= does not mail on it
// (mw-gq6.532).
func TestDampersOverTheirLimitAreNoFaultOfTheHost(t *testing.T) {
	now := doctorNow
	var out strings.Builder
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now,
		&apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK})
	doctor.Out = &out
	report := application.ReadDampers(application.DamperFacts{Now: now}, application.DamperLimits{})
	report.Counts.ReviewQueue, report.Counts.DemosWaiting = 43, 4
	report.Alarms = []application.DamperAlarm{{Principle: "review", Text: "his review queue 43 is over 8", Beads: []string{"mw-1"}}}
	doctor.Dampers = fakeDamperCounter{report: report}

	if _, err := doctor.Run(context.Background(), "", false); err != nil {
		t.Fatalf("expected dampers over their limit to be no fault, got %v (exit %d)", err, application.ExitStatus(err))
	}
	if !strings.Contains(out.String(), "his review queue 43 (limit 8)") {
		t.Errorf("expected the over-limit count still said, got:\n%s", out.String())
	}
}

// Every plain ok clears its check's note on every run, and each clear is a bd
// of its own (several server connections each) on every host every five
// minutes (mw-gq6.608): the notes are listed once, and only one that is there
// is cleared.
func TestARunClearsOnlyTheNotesThatAreThereAndListsThemOnce(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	stale := application.DoctorNoteKey("vps", "wifi")
	if err := notes.SetNote(context.Background(), stale, "stale"); err != nil {
		t.Fatalf("setting a note: %v", err)
	}
	// Another host's note under the same check name is not this host's to clear.
	theirs := application.DoctorNoteKey("laptop", "wifi")
	if err := notes.SetNote(context.Background(), theirs, "theirs"); err != nil {
		t.Fatalf("setting a note: %v", err)
	}
	d := application.Doctor{
		Checks: application.DoctorChecks{
			&apptest.FakeDoctorCheck{CheckName: "wifi", Verdict: application.DoctorOK},
			&apptest.FakeDoctorCheck{CheckName: "tunnel", Verdict: application.DoctorOK},
			&apptest.FakeDoctorCheck{CheckName: "timers", Verdict: application.DoctorOK},
		},
		State: state, Log: log, Notes: notes, Host: "vps",
		Now: func() time.Time { return now },
	}

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("running: %v", err)
	}
	if got := notes.Clears(); got != 1 {
		t.Errorf("expected one clear, of the note that was there, got %d", got)
	}
	if got := notes.Lists(); got != 1 {
		t.Errorf("expected the notes listed once, got %d", got)
	}
	if _, ok := notes.Get(stale); ok {
		t.Errorf("expected the stale note cleared")
	}
	if _, ok := notes.Get(theirs); !ok {
		t.Errorf("expected the other host's note left alone")
	}
}

func TestARunThatCannotListTheNotesClearsEachCheckAsBefore(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	notes.ListErr = errors.New("the server is unreachable")
	now := doctorNow
	d := application.Doctor{
		Checks: application.DoctorChecks{
			&apptest.FakeDoctorCheck{CheckName: "wifi", Verdict: application.DoctorOK},
			&apptest.FakeDoctorCheck{CheckName: "tunnel", Verdict: application.DoctorOK},
		},
		State: state, Log: log, Notes: notes, Host: "vps",
		Now: func() time.Time { return now },
	}

	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("running: %v", err)
	}
	if got := notes.Clears(); got != 2 {
		t.Errorf("expected each check's note cleared when the listing failed, got %d clears", got)
	}
}

func TestANoteWrittenByACheckInARunIsClearedByTheNextRun(t *testing.T) {
	state := apptest.NewFakeDoctorState()
	log := &apptest.FakeDoctorLog{}
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	check := &apptest.FakeDoctorCheck{
		CheckName: "widget", Verdict: application.DoctorFaulty, Reason: "misaligned",
		Wait: time.Minute, Cap: 1,
	}
	d := application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  state, Log: log, Notes: notes, Host: "vps",
		Now: func() time.Time { return now },
	}
	d.Run(context.Background(), "", false)
	d.Run(context.Background(), "", false)
	key := application.DoctorNoteKey("vps", "widget")
	if _, ok := notes.Get(key); !ok {
		t.Fatalf("expected a note for the check left faulty")
	}

	check.Verdict = application.DoctorOK
	if _, err := d.Run(context.Background(), "", false); err != nil {
		t.Fatalf("running: %v", err)
	}
	if _, ok := notes.Get(key); ok {
		t.Errorf("expected the note cleared once the check was ok")
	}
}

func TestAWholeRunLeavesItsVerdictAndSaysItAgainOnlyWhenItChangesOrGrowsOld(t *testing.T) {
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	faulty := &apptest.FakeDoctorCheck{CheckName: "boost-config", Verdict: application.DoctorFaulty, Reason: "tests.millwright differs\nsecond line",
		CureErr: errors.New("no cure"), Cap: 1}
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now, faulty)
	doctor.Verdicts, doctor.Host = notes, "laptop"
	run := func(name string) {
		t.Helper()
		if _, err := doctor.Run(context.Background(), name, false); err != nil {
			if _, fault := application.DoctorFaults(err); !fault {
				t.Fatalf("running: %v", err)
			}
		}
	}

	run("boost-config")
	if notes.Sets() != 0 {
		t.Fatalf("expected a run of one named check to leave no verdict, got %d writes", notes.Sets())
	}

	run("")
	want := "2026-09-23T12:00:00Z faulty boost-config: tests.millwright differs second line"
	if got, _ := notes.Get(application.DoctorVerdictKey("laptop")); got != want {
		t.Fatalf("expected the verdict %q, got %q", want, got)
	}
	now = now.Add(time.Hour)
	run("")
	if notes.Sets() != 1 {
		t.Errorf("expected the same verdict within the refresh time to be left alone, got %d writes", notes.Sets())
	}
	now = now.Add(application.DoctorVerdictRefresh)
	run("")
	if notes.Sets() != 2 {
		t.Errorf("expected the same verdict to be said again after the refresh time, got %d writes", notes.Sets())
	}
	faulty.Verdict = application.DoctorOK
	run("")
	if got, _ := notes.Get(application.DoctorVerdictKey("laptop")); !strings.HasSuffix(got, " ok") || notes.Sets() != 3 {
		t.Errorf("expected a changed verdict to be left at once as ok, got %q after %d writes", got, notes.Sets())
	}
}

func TestADryRunLeavesNoVerdict(t *testing.T) {
	notes := apptest.NewFakeDoctorNotes()
	now := doctorNow
	doctor := aDoctor(apptest.NewFakeDoctorState(), &apptest.FakeDoctorLog{}, &now,
		&apptest.FakeDoctorCheck{CheckName: "widget", Verdict: application.DoctorOK})
	doctor.Verdicts, doctor.Host = notes, "laptop"
	if _, err := doctor.Run(context.Background(), "", true); err != nil {
		t.Fatalf("running: %v", err)
	}
	if notes.Sets() != 0 {
		t.Errorf("expected no verdict from a dry run, got %d writes", notes.Sets())
	}
}

func TestADoctorVerdictReadsBackWhatItSaid(t *testing.T) {
	said := application.DoctorVerdict{At: doctorNow, Faulty: []application.DoctorFinding{{Check: "a", Reason: "x: y || z"}, {Check: "b", Reason: "w"}}}
	if _, ok := application.ParseDoctorVerdict("2026-09-23T12:00:00Z faulty nothing here"); ok {
		t.Errorf("expected a finding with no colon to be unreadable")
	}
	if _, ok := application.ParseDoctorVerdict("yesterday ok"); ok {
		t.Errorf("expected a note with no time to be unreadable")
	}
	back, ok := application.ParseDoctorVerdict(application.DoctorVerdict{At: doctorNow, Faulty: said.Faulty[1:]}.Note())
	if !ok || len(back.Faulty) != 1 || back.Faulty[0].Reason != "w" || !back.At.Equal(doctorNow) {
		t.Errorf("expected the verdict back, got %+v ok=%v", back, ok)
	}
}

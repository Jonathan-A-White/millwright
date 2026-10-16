package doctor_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// staleRig is a vault, a stand-in tmux and a stand-in bin/mayor-up around a
// MayorStale check: the pane's text is a file the test rewrites (a beat), the
// windows tmux was told to kill and the times mayor-up ran are files the stand-ins append to.
type staleRig struct {
	t       *testing.T
	vault   string
	pane    string
	kills   string
	upRuns  string
	upExit  string
	now     time.Time
	alarms  []string
	clears  []clearCall
	alarmEr error
	check   *doctor.MayorStale
	doc     application.Doctor
}

// frozenPane is a pane whose turn is running, as the harness draws it: the
// elapsed clock n is what a live pane would redraw and a frozen one stops at.
func frozenPane(n int) string {
	return fmt.Sprintf("● Working on it...\n\n✻ Pondering… (%ds · esc to interrupt)\n\n❯\u00a0\n", n)
}

// idlePane is a Mayor whose turn has ended, waiting at an empty prompt.
const idlePane = "● Handed off.\n\n──────────────\n❯\u00a0\n──────────────\n  done 11:44 AM\n"

func newStaleRig(t *testing.T) *staleRig {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are shell scripts")
	}
	r := &staleRig{t: t, now: time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC)}
	r.vault = mayorGoneVault(t, "mayor-2026-10-01-156")
	dir := t.TempDir()
	r.pane = filepath.Join(dir, "pane")
	r.kills = filepath.Join(dir, "kills")
	r.upRuns = filepath.Join(dir, "up-runs")
	r.upExit = filepath.Join(dir, "up-exit")
	r.write(r.pane, frozenPane(1))
	r.write(r.upExit, "0")

	tmux := filepath.Join(dir, "tmux-stand-in")
	r.write(tmux, fmt.Sprintf(`#!/bin/sh
case "$1" in
list-windows) printf '@2|mayor-2026-10-01-156\n' ;;
list-panes) printf '0 claude\n' ;;
capture-pane) cat '%s' ;;
kill-window) printf '%%s\n' "$3" >> '%s' ;;
esac
exit 0
`, r.pane, r.kills))
	r.chmod(tmux)

	mayorUp := filepath.Join(r.vault, "bin", "mayor-up")
	r.write(mayorUp, fmt.Sprintf(`#!/bin/sh
echo run >> '%s'
code=$(cat '%s')
[ "$code" = 0 ] && echo @9
exit "$code"
`, r.upRuns, r.upExit))
	r.chmod(mayorUp)

	store := doctor.New(filepath.Join(dir, "state"))
	r.check = &doctor.MayorStale{
		Vault: r.vault, Tmux: tmux, State: store, Limit: 15 * time.Minute,
		Now: func() time.Time { return r.now },
		Alarm: func(_ context.Context, text string) (uint64, error) {
			r.alarms = append(r.alarms, text)
			return uint64(200 + len(r.alarms)), r.alarmEr
		},
		Clear: func(_ context.Context, text string, clears uint64) error {
			r.clears = append(r.clears, clearCall{text, clears})
			return nil
		},
	}
	r.doc = application.Doctor{
		Checks: application.DoctorChecks{r.check}, State: store, Log: store,
		Host: "laptop", Now: func() time.Time { return r.now },
	}
	return r
}

func (r *staleRig) write(path, text string) {
	r.t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatalf("writing %s: %v", path, err)
	}
}

func (r *staleRig) chmod(path string) {
	r.t.Helper()
	if err := os.Chmod(path, 0o755); err != nil {
		r.t.Fatalf("chmod %s: %v", path, err)
	}
}

func (r *staleRig) lines(path string) int {
	data, _ := os.ReadFile(path)
	return len(strings.Fields(string(data)))
}

// run lets minutes go by and runs the doctor's one check, returning its result.
func (r *staleRig) run(minutes int) application.DoctorResult {
	r.t.Helper()
	r.now = r.now.Add(time.Duration(minutes) * time.Minute)
	report, _ := r.doc.Run(context.Background(), "", false)
	if len(report.Results) != 1 {
		r.t.Fatalf("expected one result, got %d", len(report.Results))
	}
	return report.Results[0]
}

func TestAHeldSeatWhosePaneHasNotChangedForTheLimitIsRespawnedOnceWithOneAlarm(t *testing.T) {
	r := newStaleRig(t)

	if got := r.run(0); got.Verdict != "ok" {
		t.Fatalf("the first look only remembers the pane: expected ok, got %+v", got)
	}
	if got := r.run(14); got.Verdict != "ok" {
		t.Fatalf("14 minutes is under the limit: expected ok, got %+v", got)
	}

	got := r.run(2)
	if got.Verdict != "cured" || !strings.Contains(got.Reason, "16 minutes") {
		t.Fatalf("expected cured naming 16 minutes, got %+v", got)
	}
	if n := r.lines(r.kills); n != 1 {
		t.Errorf("expected the stuck window killed once, got %d", n)
	}
	if n := r.lines(r.upRuns); n != 1 {
		t.Errorf("expected mayor-up run once, got %d", n)
	}
	if len(r.alarms) != 1 || !strings.Contains(r.alarms[0], "mayor-2026-10-01-156") || !strings.Contains(r.alarms[0], "@9") {
		t.Fatalf("expected one alarm naming the window and its successor, got %q", r.alarms)
	}

	// Still stale a few minutes on: damped, and neither a second respawn nor a second alarm.
	got = r.run(3)
	if got.Verdict != "damped" || !got.Faulty {
		t.Fatalf("expected damped and faulty, got %+v", got)
	}
	if r.lines(r.kills) != 1 || r.lines(r.upRuns) != 1 || len(r.alarms) != 1 {
		t.Errorf("a damped run must do nothing more: kills %d, mayor-up %d, alarms %d", r.lines(r.kills), r.lines(r.upRuns), len(r.alarms))
	}
}

func TestAFreshBeatClearsItAndTheClockStartsAgain(t *testing.T) {
	r := newStaleRig(t)
	r.run(0)
	r.run(16)
	if len(r.alarms) != 1 {
		t.Fatalf("expected the first episode to alarm once, got %d", len(r.alarms))
	}

	r.write(r.pane, frozenPane(2))
	if got := r.run(1); got.Verdict != "ok" {
		t.Fatalf("a changed pane is a beat: expected ok, got %+v", got)
	}
	if got := r.run(14); got.Verdict != "ok" {
		t.Fatalf("14 minutes after the beat is under the limit: expected ok, got %+v", got)
	}

	// A later stall is a new episode, with its own respawn and its own alarm.
	if got := r.run(2); got.Verdict != "cured" {
		t.Fatalf("expected the new stall cured, got %+v", got)
	}
	if r.lines(r.upRuns) != 2 || len(r.alarms) != 2 {
		t.Errorf("expected a second respawn and a second alarm: mayor-up %d, alarms %d", r.lines(r.upRuns), len(r.alarms))
	}
}

func TestASeatNotHeldRaisesNothing(t *testing.T) {
	cases := map[string]func(r *staleRig){
		"no .mayor-acting": func(r *staleRig) {
			if err := os.Remove(filepath.Join(r.vault, application.ActingFileName("mayor"))); err != nil {
				t.Fatal(err)
			}
		},
		"a window it does not name": func(r *staleRig) {
			r.write(filepath.Join(r.vault, application.ActingFileName("mayor")), "acting in window mayor-2026-10-01-155\n")
		},
		"a host that is not home": func(r *staleRig) {
			r.check.Home = &apptest.FakeHomeFile{Text: "desktop 2026-10-01T10:00:00Z mayor\n"}
			r.check.Host = "laptop"
		},
	}
	for name, hold := range cases {
		t.Run(name, func(t *testing.T) {
			r := newStaleRig(t)
			hold(r)
			r.run(0)
			if got := r.run(60); got.Verdict != "ok" {
				t.Fatalf("expected ok, got %+v", got)
			}
			if r.lines(r.kills) != 0 || r.lines(r.upRuns) != 0 || len(r.alarms) != 0 {
				t.Errorf("a seat not held must raise nothing: kills %d, mayor-up %d, alarms %d", r.lines(r.kills), r.lines(r.upRuns), len(r.alarms))
			}
		})
	}
}

// mw-gq6.281: with no tmux server there is no Mayor window to look at, so
// the check has nothing to judge: ok, not cannot-tell.
func TestNoTmuxServerIsNothingForTheStaleCheckToLookAt(t *testing.T) {
	for _, stderr := range []string{
		"no server running on /tmp/tmux-1000/default",
		"error connecting to /tmp/tmux-1000/default (No such file or directory)",
	} {
		t.Run(stderr, func(t *testing.T) {
			r := newStaleRig(t)
			r.check.Tmux = fakeMayorGoneTmuxFailing(t, stderr)
			r.run(0)
			if got := r.run(60); got.Verdict != "ok" {
				t.Fatalf("expected ok, got %+v", got)
			}
		})
	}
}

func TestAHarnessPromptIsAlarmedWithItsTextAndTheWindowIsLeftAlone(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, "cat /tmp/mayor154/talk.out\nAuto mode classifier requires confirmation for this command.\nDo you want to proceed?\n 1. Yes\n 2. Yes, allow reading from /tmp/mayor154\n 3. No\n")
	r.run(0)
	got := r.run(20)
	if got.Verdict != "cured" {
		t.Fatalf("expected the episode answered, got %+v", got)
	}
	if r.lines(r.kills) != 0 || r.lines(r.upRuns) != 0 {
		t.Errorf("a window waiting on the Governor's answer is not closed: kills %d, mayor-up %d", r.lines(r.kills), r.lines(r.upRuns))
	}
	if len(r.alarms) != 1 || !strings.Contains(r.alarms[0], "Do you want to proceed?") || !strings.Contains(r.alarms[0], "2. Yes, allow reading from /tmp/mayor154") {
		t.Fatalf("expected one alarm carrying the prompt and its options, got %q", r.alarms)
	}
}

func TestAMayorUpThatFailsStillSendsTheOneAlarm(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.upExit, "4")
	r.run(0)
	got := r.run(20)
	if got.Verdict != "cure-failed" {
		t.Fatalf("expected cure-failed, got %+v", got)
	}
	if len(r.alarms) != 1 || !strings.Contains(r.alarms[0], "could not") {
		t.Fatalf("expected one alarm saying no new Mayor could be started, got %q", r.alarms)
	}
}

func TestADryRunReportsAStaleSeatAndChangesNothing(t *testing.T) {
	r := newStaleRig(t)
	r.run(0)
	r.now = r.now.Add(20 * time.Minute)
	report, _ := r.doc.Run(context.Background(), "", true)
	if got := report.Results[0]; got.Verdict != "would-cure" || !strings.Contains(got.Reason, "20 minutes") {
		t.Fatalf("expected would-cure naming 20 minutes, got %+v", got)
	}
	if r.lines(r.kills) != 0 || r.lines(r.upRuns) != 0 || len(r.alarms) != 0 {
		t.Errorf("a dry run changes nothing: kills %d, mayor-up %d, alarms %d", r.lines(r.kills), r.lines(r.upRuns), len(r.alarms))
	}
}

func TestAMayorUpThatStillFindsTheClosedMayorAliveIsTriedAgain(t *testing.T) {
	r := newStaleRig(t)
	r.check.Settle = time.Millisecond
	mayorUp := filepath.Join(r.vault, "bin", "mayor-up")
	r.write(mayorUp, fmt.Sprintf("#!/bin/sh\necho run >> '%s'\n[ $(wc -l < '%s') -lt 2 ] && exit 3\necho @9\n", r.upRuns, r.upRuns))
	r.run(0)
	if got := r.run(20); got.Verdict != "cured" {
		t.Fatalf("expected cured on the second try, got %+v", got)
	}
	if n := r.lines(r.upRuns); n != 2 {
		t.Errorf("expected mayor-up run twice, got %d", n)
	}
	if len(r.alarms) != 1 {
		t.Errorf("expected one alarm, got %d", len(r.alarms))
	}
}

func TestAnIdleMayorAtAnEmptyPromptIsAliveHoweverLongItStands(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, idlePane)
	r.run(0)
	for _, minutes := range []int{30, 60, 600} {
		if got := r.run(minutes); got.Verdict != "ok" {
			t.Fatalf("an empty prompt is a Mayor waiting: expected ok after %d more minutes, got %+v", minutes, got)
		}
	}
	if r.lines(r.kills) != 0 || r.lines(r.upRuns) != 0 || len(r.alarms) != 0 {
		t.Errorf("an idle Mayor raises nothing: kills %d, mayor-up %d, alarms %d", r.lines(r.kills), r.lines(r.upRuns), len(r.alarms))
	}
}

func TestAFrozenTurnIsClosedRespawnedOnceAndAlarmedOnce(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, frozenPane(41))
	r.run(0)
	got := r.run(16)
	if got.Verdict != "cured" {
		t.Fatalf("expected cured, got %+v", got)
	}
	if r.lines(r.kills) != 1 || r.lines(r.upRuns) != 1 || len(r.alarms) != 1 {
		t.Errorf("expected one kill, one respawn, one alarm: kills %d, mayor-up %d, alarms %d", r.lines(r.kills), r.lines(r.upRuns), len(r.alarms))
	}
}

func TestInputLeftUntakenOnThePromptIsStale(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, "● Handed off.\n\n──────────────\n❯ New events for mayor: 1. Run mw next\n──────────────\n  done 11:44 AM\n")
	r.run(0)
	if got := r.run(14); got.Verdict != "ok" {
		t.Fatalf("under the limit: expected ok, got %+v", got)
	}
	got := r.run(2)
	if got.Verdict != "cured" {
		t.Fatalf("expected cured, got %+v", got)
	}
	if r.lines(r.kills) != 1 || r.lines(r.upRuns) != 1 || len(r.alarms) != 1 {
		t.Errorf("expected one kill, one respawn, one alarm: kills %d, mayor-up %d, alarms %d", r.lines(r.kills), r.lines(r.upRuns), len(r.alarms))
	}
}

func TestAnIdleMayorWhoseInputIsTakenIsAliveAgain(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, "❯ nudge text\n")
	r.run(0)
	r.run(10)
	r.write(r.pane, idlePane)
	if got := r.run(20); got.Verdict != "ok" {
		t.Fatalf("the input was taken and the prompt is empty: expected ok, got %+v", got)
	}
}

func TestAFreshBeatAfterAnAlarmClearsTheEmergencyOnce(t *testing.T) {
	r := newStaleRig(t)
	r.run(0)
	r.run(16)
	if len(r.alarms) != 1 {
		t.Fatalf("expected the alarm, got %d", len(r.alarms))
	}
	if len(r.clears) != 0 {
		t.Fatalf("the Mayor is still stale: expected no clear, got %+v", r.clears)
	}

	r.write(r.pane, frozenPane(2))
	r.run(1)
	if len(r.clears) != 1 || r.clears[0].clears != 201 || !strings.Contains(r.clears[0].text, "Mayor") {
		t.Fatalf("expected one clear of 201 naming the Mayor, got %+v", r.clears)
	}
	r.run(1)
	r.run(1)
	if len(r.clears) != 1 {
		t.Errorf("the episode is over: expected the one clear, got %+v", r.clears)
	}
}

func TestAMayorThatNeverAlarmedClearsNothing(t *testing.T) {
	r := newStaleRig(t)
	r.run(0)
	r.write(r.pane, frozenPane(2))
	r.run(1)
	if len(r.clears) != 0 {
		t.Fatalf("expected no clear without an alarm, got %+v", r.clears)
	}
}

func TestADryRunSendsNoClearAndKeepsTheAlarmRecord(t *testing.T) {
	r := newStaleRig(t)
	r.run(0)
	r.run(16)
	r.write(r.pane, frozenPane(2))

	r.now = r.now.Add(time.Minute)
	if _, err := r.doc.Run(context.Background(), "", true); err != nil {
		t.Fatal(err)
	}
	if len(r.clears) != 0 {
		t.Fatalf("a dry run sends nothing, got %+v", r.clears)
	}
	r.run(1)
	if len(r.clears) != 1 || r.clears[0].clears != 201 {
		t.Fatalf("the real run after it still clears 201, got %+v", r.clears)
	}
}

// parkedPane is the pane of a Mayor stopped on the harness's confirmation
// prompt after three refusals in a row (2026-10-11).
const parkedPane = "● Update(config.toml)\n  ⎿  Error: blocked by the classifier\n\n" +
	"Auto mode classifier requires confirmation for this action.\n" +
	"3 consecutive actions were blocked.\n" +
	"Do you want to proceed?\n" +
	" ❯ 1. Yes\n   2. Yes, allow reading from /home/jwhite/.config/mw\n   3. No\n\n  Esc to cancel\n"

func TestAParkedPromptIsAlarmedOnceNamingTheQuestionAndEveryChoice(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, parkedPane)
	r.run(0)
	r.run(20)
	r.run(5)
	r.run(40)
	if len(r.alarms) != 1 {
		t.Fatalf("a prompt that stays is alarmed once, got %d: %q", len(r.alarms), r.alarms)
	}
	for _, want := range []string{"Do you want to proceed?", "1. Yes", "2. Yes, allow reading from /home/jwhite/.config/mw", "3. No"} {
		if !strings.Contains(r.alarms[0], want) {
			t.Errorf("expected the alarm to carry %q, got %q", want, r.alarms[0])
		}
	}
	if strings.Contains(r.alarms[0], "Error: blocked by the classifier") {
		t.Errorf("the alarm names the prompt and its choices, not what scrolled above it: %q", r.alarms[0])
	}
	if r.lines(r.kills) != 0 || r.lines(r.upRuns) != 0 {
		t.Errorf("the window is left alone: kills %d, mayor-up %d", r.lines(r.kills), r.lines(r.upRuns))
	}
}

func TestAPaneAskingForConfirmationIsAPromptWhateverItsQuestion(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, "Auto mode classifier requires confirmation\n 1. Yes\n 2. No\n")
	r.run(0)
	r.run(20)
	if len(r.alarms) != 1 || !strings.Contains(r.alarms[0], "requires confirmation") || !strings.Contains(r.alarms[0], "2. No") {
		t.Fatalf("expected one alarm naming the line and its choices, got %q", r.alarms)
	}
	if r.lines(r.kills) != 0 {
		t.Errorf("the window is left alone, got %d kills", r.lines(r.kills))
	}
}

func TestTheNoticeQuotedInProseIsNotAPrompt(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, "● The harness said it requires confirmation, so I stopped.\n\n"+idlePane)
	r.run(0)
	if got := r.run(60); got.Verdict != "ok" || len(r.alarms) != 0 {
		t.Fatalf("a Mayor idle at an empty prompt is alive: got %+v, alarms %q", got, r.alarms)
	}
}

func TestAPromptThatReturnsAfterTheMayorWorkedAgainIsAlarmedAgain(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, parkedPane)
	r.run(0)
	r.run(20)
	r.write(r.pane, frozenPane(1))
	r.run(5)
	r.write(r.pane, parkedPane)
	r.run(5)
	r.run(20)
	if len(r.alarms) != 2 {
		t.Fatalf("a prompt that comes back after the Mayor worked is a new one: expected 2 alarms, got %q", r.alarms)
	}
}

func TestACureLineSaysWhatTheCureDid(t *testing.T) {
	t.Run("a prompt is left alone, and the line says so", func(t *testing.T) {
		r := newStaleRig(t)
		r.write(r.pane, parkedPane)
		r.run(0)
		got := r.run(20)
		if strings.Contains(got.WayBack, "would") || !strings.Contains(got.WayBack, "left the Mayor's window alone") {
			t.Fatalf("expected a way back saying the window was left alone, got %q", got.WayBack)
		}
	})
	t.Run("a dry run on a prompt says it would leave it alone", func(t *testing.T) {
		r := newStaleRig(t)
		r.write(r.pane, parkedPane)
		r.run(0)
		r.now = r.now.Add(20 * time.Minute)
		report, _ := r.doc.Run(context.Background(), "", true)
		got := report.Results[0]
		if got.Verdict != "would-cure" || strings.Contains(got.WayBack, "would close") || !strings.Contains(got.WayBack, "would leave the Mayor's window alone") {
			t.Fatalf("expected a dry run saying it would leave the window alone, got %+v", got)
		}
		if len(r.alarms) != 0 {
			t.Errorf("a dry run sends no alarm, got %q", r.alarms)
		}
	})
	t.Run("a dry run on a frozen turn would close the window", func(t *testing.T) {
		r := newStaleRig(t)
		r.run(0)
		r.now = r.now.Add(20 * time.Minute)
		report, _ := r.doc.Run(context.Background(), "", true)
		if got := report.Results[0]; !strings.Contains(got.WayBack, "would close the Mayor's window") {
			t.Fatalf("expected a dry run saying it would close the window, got %+v", got)
		}
	})
	t.Run("a real run that closed the window says it did, not that it would", func(t *testing.T) {
		r := newStaleRig(t)
		r.run(0)
		got := r.run(20)
		if got.Verdict != "cured" || strings.Contains(got.WayBack, "would") || !strings.Contains(got.WayBack, "tmux kill-window -t '@9'") {
			t.Fatalf("expected a way back naming the new window, got %+v", got)
		}
	})
}

// limitMenuPane is a Mayor parked on the harness's usage-limit menu, its
// selection arrow at the start of the line as an input line's is.
const limitMenuPane = "● Reading the newest handoff first.\n\n" +
	"  ⎿  You've hit your weekly limit · resets Oct 16, 7am (UTC)\n\n" +
	"What do you want to do?\n\n" +
	"❯ 1. Stop and wait for limit to reset\n" +
	"  2. Switch to usage credits\n"

// A Mayor stopped at the usage limit is mayor-stuck's to page about: a respawn
// would meet the same limit, so mayor-stale leaves the window be (mw-gq6.433).
func TestAMayorParkedOnTheUsageLimitIsLeftToMayorStuck(t *testing.T) {
	r := newStaleRig(t)
	r.write(r.pane, limitMenuPane)
	r.run(0)
	for _, minutes := range []int{16, 30, 60} {
		if got := r.run(minutes); got.Verdict != "ok" {
			t.Fatalf("expected ok for a pane on the usage limit after %d more minutes, got %+v", minutes, got)
		}
	}
	if r.lines(r.kills) != 0 || r.lines(r.upRuns) != 0 || len(r.alarms) != 0 {
		t.Errorf("a Mayor at the usage limit raises nothing here: kills %d, mayor-up %d, alarms %d", r.lines(r.kills), r.lines(r.upRuns), len(r.alarms))
	}
}

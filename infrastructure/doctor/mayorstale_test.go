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
		Alarm: func(_ context.Context, text string) error {
			r.alarms = append(r.alarms, text)
			return r.alarmEr
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

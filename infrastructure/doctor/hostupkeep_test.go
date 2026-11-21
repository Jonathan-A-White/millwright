package doctor_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// closedStories answers ShowBeads from a table of how long ago each story closed
// (negative: the story is still open); a story not in it is not found.
type closedStories struct {
	now time.Time
	ago map[string]time.Duration
	err error
}

func (c closedStories) ShowBeads(_ context.Context, ids []string) ([]application.StoryDetail, error) {
	if c.err != nil {
		return nil, c.err
	}
	var out []application.StoryDetail
	for _, id := range ids {
		ago, ok := c.ago[id]
		if !ok {
			continue
		}
		d := application.StoryDetail{Status: "in_progress"}
		d.Story.ID = id
		if ago >= 0 {
			d.Status = application.StateClosed
			d.ClosedAt = c.now.Add(-ago)
		}
		out = append(out, d)
	}
	return out, nil
}

// upkeepWorld is a host with a state directory, a worktrees directory, and every
// upkeep mark as recent as the test says.
type upkeepWorld struct {
	t      *testing.T
	now    time.Time
	state  string
	trees  string
	ago    map[string]time.Duration
	off    map[string]bool // timers not enabled
	ran    [][]string
	runOut map[string]string
	runErr error
}

func newUpkeepWorld(t *testing.T) *upkeepWorld {
	t.Helper()
	w := &upkeepWorld{
		t:      t,
		now:    time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC),
		state:  t.TempDir(),
		trees:  t.TempDir(),
		ago:    map[string]time.Duration{},
		off:    map[string]bool{},
		runOut: map[string]string{},
	}
	w.mark(doctor.HostUpkeepSweepMark, time.Hour)
	w.mark(doctor.HostUpkeepGoTidyMark, time.Hour)
	return w
}

// mark writes a mark file last touched age ago.
func (w *upkeepWorld) mark(name string, age time.Duration) {
	w.t.Helper()
	path := filepath.Join(w.state, name)
	if err := os.WriteFile(path, []byte("ran\n"), 0o644); err != nil {
		w.t.Fatal(err)
	}
	at := w.now.Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		w.t.Fatal(err)
	}
}

// worktrees makes n worktrees of stories closed age ago.
func (w *upkeepWorld) worktrees(n int, age time.Duration, prefix string) {
	w.t.Helper()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%d", prefix, i)
		dir := filepath.Join(w.trees, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			w.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
			w.t.Fatal(err)
		}
		w.ago[id] = age
	}
}

func (w *upkeepWorld) check() *doctor.HostUpkeep {
	return &doctor.HostUpkeep{
		StateDir:  w.state,
		Worktrees: []string{w.trees},
		Stories:   closedStories{now: w.now, ago: w.ago},
		Now:       func() time.Time { return w.now },
		TimerEnabled: func(_ context.Context, timer string) (bool, error) {
			return !w.off[timer], nil
		},
		Run: func(_ context.Context, args ...string) (string, error) {
			w.ran = append(w.ran, args)
			return w.runOut[strings.Join(args, " ")], w.runErr
		},
	}
}

func TestHostUpkeepIsOkOnALevelHost(t *testing.T) {
	w := newUpkeepWorld(t)
	w.worktrees(20, 3*24*time.Hour, "old") // exactly 20 old worktrees is not over 20
	w.worktrees(30, -1, "live")            // stories not closed never count
	w.worktrees(30, 2*time.Hour, "fresh")  // closed under a day ago: the close-out has them
	verdict, reason := w.check().Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("a level host: %v %q", verdict, reason)
	}
}

func TestHostUpkeepFaultsOnASweepOverTwoDaysOld(t *testing.T) {
	w := newUpkeepWorld(t)
	w.mark(doctor.HostUpkeepSweepMark, 49*time.Hour)
	verdict, reason := w.check().Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "sweep") || !strings.Contains(reason, "2 days") {
		t.Fatalf("a stale sweep: %v %q", verdict, reason)
	}
	if strings.Contains(reason, "go-cache") {
		t.Fatalf("the go-cache tidy is level and must not be named: %q", reason)
	}
}

func TestHostUpkeepFaultsOnAHostThatOnlyEverListed(t *testing.T) {
	w := newUpkeepWorld(t)
	if err := os.Remove(filepath.Join(w.state, doctor.HostUpkeepSweepMark)); err != nil {
		t.Fatal(err)
	}
	// The first timer run only lists, and leaves its own mark.
	if err := os.WriteFile(filepath.Join(w.state, "host-sweep-listed"), []byte("listed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	verdict, reason := w.check().Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "only listed") {
		t.Fatalf("a host that only listed: %v %q", verdict, reason)
	}
}

func TestHostUpkeepFaultsOnASweepThatNeverRan(t *testing.T) {
	w := newUpkeepWorld(t)
	if err := os.Remove(filepath.Join(w.state, doctor.HostUpkeepSweepMark)); err != nil {
		t.Fatal(err)
	}
	verdict, reason := w.check().Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "never") {
		t.Fatalf("a sweep that never ran: %v %q", verdict, reason)
	}
}

func TestHostUpkeepFaultsOnAGoCacheTidyOverTwoDaysOld(t *testing.T) {
	w := newUpkeepWorld(t)
	w.mark(doctor.HostUpkeepGoTidyMark, 50*time.Hour)
	verdict, reason := w.check().Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "go-cache") {
		t.Fatalf("a stale go-cache tidy: %v %q", verdict, reason)
	}
	if err := os.Remove(filepath.Join(w.state, doctor.HostUpkeepGoTidyMark)); err != nil {
		t.Fatal(err)
	}
	verdict, reason = w.check().Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "never") {
		t.Fatalf("a go-cache tidy that never ran: %v %q", verdict, reason)
	}
}

func TestHostUpkeepFaultsOnMoreThanTwentyWorktreesOfOldClosedStories(t *testing.T) {
	w := newUpkeepWorld(t)
	w.worktrees(21, 25*time.Hour, "old")
	w.worktrees(5, -1, "live")
	verdict, reason := w.check().Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "21 worktrees") {
		t.Fatalf("21 old closed worktrees: %v %q", verdict, reason)
	}
}

func TestHostUpkeepNamesATimerNotEnabledAndTheOneCommand(t *testing.T) {
	w := newUpkeepWorld(t)
	w.off["mw-host-tidy.timer"] = true
	verdict, reason := w.check().Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "mw-host-tidy.timer") || strings.Contains(reason, "mw-host-sweep.timer") {
		t.Fatalf("one timer off: %v %q", verdict, reason)
	}
	if !strings.Contains(reason, "sh scripts/install-units.sh --boost --enable") {
		t.Fatalf("the finding does not give the one command: %q", reason)
	}
	w.off["mw-host-sweep.timer"] = true
	_, reason = w.check().Probe(context.Background())
	if !strings.Contains(reason, "mw-host-tidy.timer") || !strings.Contains(reason, "mw-host-sweep.timer") {
		t.Fatalf("both timers off: %q", reason)
	}
}

func TestHostUpkeepSkipsTheTimersOnAHostWithNoUserManager(t *testing.T) {
	w := newUpkeepWorld(t)
	c := w.check()
	c.TimerEnabled = func(context.Context, string) (bool, error) { return false, doctor.ErrNoUserManager }
	if verdict, reason := c.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("no user manager: %v %q", verdict, reason)
	}
}

func TestHostUpkeepCannotTellWhenTheTrackerCannotBeRead(t *testing.T) {
	w := newUpkeepWorld(t)
	w.worktrees(3, 3*24*time.Hour, "old")
	c := w.check()
	c.Stories = closedStories{err: errors.New("bd is down")}
	verdict, reason := c.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "bd is down") {
		t.Fatalf("an unread tracker: %v %q", verdict, reason)
	}
	// A fault already found is still said.
	w.mark(doctor.HostUpkeepSweepMark, 72*time.Hour)
	if verdict, _ = c.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("a stale sweep with an unread tracker: %v", verdict)
	}
}

func TestHostUpkeepCureRunsTheThreeCommandsAndSaysWhatItRemoved(t *testing.T) {
	w := newUpkeepWorld(t)
	w.mark(doctor.HostUpkeepSweepMark, 72*time.Hour)
	w.runOut["host sweep"] = "host sweep: removed 255 items, 134.0 GB\n  removed worktree /x\n"
	w.runOut["host tidy go-cache"] = "go cache /c: removed 900 files, 39000000000 bytes, unused for over 24h0m0s; kept 1 files, 2 bytes\n"
	w.runOut["host tidy tmp"] = "tmp /tmp: removed 4 entries, 100 bytes, untouched for over 24h0m0s; kept 0 entries, 0 bytes\n"
	c := w.check()
	if err := c.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, args := range w.ran {
		got = append(got, strings.Join(args, " "))
	}
	want := "host tidy tmp|host tidy go-cache|host sweep"
	if strings.Join(got, "|") != want {
		t.Fatalf("cure ran %q, want %q", strings.Join(got, "|"), want)
	}
	back := c.WayBack()
	for _, s := range []string{"host sweep: removed 255 items, 134.0 GB", "removed 900 files", "removed 4 entries"} {
		if !strings.Contains(back, s) {
			t.Errorf("the way back does not say %q: %q", s, back)
		}
	}
}

func TestHostUpkeepCureGoesOnPastACommandThatFailsAndSaysSo(t *testing.T) {
	w := newUpkeepWorld(t)
	w.runErr = errors.New("exit status 1")
	c := w.check()
	err := c.Cure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "host sweep") {
		t.Fatalf("a failing cure: %v", err)
	}
	if len(w.ran) != 3 {
		t.Fatalf("a failing command must not stop the others: ran %v", w.ran)
	}
}

func TestHostUpkeepWayBackBeforeAnyCure(t *testing.T) {
	w := newUpkeepWorld(t)
	if back := w.check().WayBack(); !strings.HasPrefix(back, "none") {
		t.Fatalf("way back before a cure: %q", back)
	}
}

func TestHostUpkeepRecordsARun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "mw")
	at := time.Date(2026, 11, 21, 4, 40, 0, 0, time.UTC)
	if err := doctor.RecordHostUpkeepRun(dir, doctor.HostUpkeepSweepMark, at); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, doctor.HostUpkeepSweepMark))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(at) {
		t.Fatalf("mark time %v, want %v", info.ModTime(), at)
	}
	// A second run moves it.
	later := at.Add(24 * time.Hour)
	if err := doctor.RecordHostUpkeepRun(dir, doctor.HostUpkeepSweepMark, later); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Stat(filepath.Join(dir, doctor.HostUpkeepSweepMark))
	if !info.ModTime().Equal(later) {
		t.Fatalf("mark time %v, want %v", info.ModTime(), later)
	}
}

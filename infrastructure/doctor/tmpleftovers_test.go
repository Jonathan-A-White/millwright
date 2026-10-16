package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// tlHost makes a temp dir with tmp/ and proc/ subdirectories standing in for
// a host's own /tmp and /proc. Nothing here reads a real one.
func tlHost(t *testing.T) (tmp, proc string) {
	t.Helper()
	root := t.TempDir()
	tmp = filepath.Join(root, "tmp")
	proc = filepath.Join(root, "proc")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatalf("making %s: %v", tmp, err)
	}
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatalf("making %s: %v", proc, err)
	}
	return tmp, proc
}

// tlMarkLive gives path a live owner: a symlink under a fake pid's fd
// directory, the shape a real /proc gives a process's open files.
func tlMarkLive(t *testing.T, proc, path string) {
	t.Helper()
	fdDir := filepath.Join(proc, "100", "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatalf("making %s: %v", fdDir, err)
	}
	if err := os.Symlink(path, filepath.Join(fdDir, "3")); err != nil {
		t.Fatalf("symlinking %s: %v", path, err)
	}
}

func TestTmpLeftoversProbeIsOKWithNothingThere(t *testing.T) {
	tmp, proc := tlHost(t)
	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, Budget: 100}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected a plain ok, got %s (%q)", verdict, reason)
	}
}

func TestTmpLeftoversProbeIsOKUnderBudgetNamingBytesFound(t *testing.T) {
	tmp, proc := tlHost(t)
	if err := os.WriteFile(filepath.Join(tmp, "nbs-spool-dead"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}
	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, Budget: 1000}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "50") {
		t.Fatalf("expected the reason to name the bytes found, got %q", reason)
	}
}

func TestTmpLeftoversProbeIsFaultyPastBudgetAndCureRemovesOnlyTheDeadOne(t *testing.T) {
	tmp, proc := tlHost(t)
	deadPath := filepath.Join(tmp, "nbs-spool-dead")
	livePath := filepath.Join(tmp, "nbs-spool-live")
	if err := os.WriteFile(deadPath, make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(livePath, make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	tlMarkLive(t, proc, livePath)

	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, Budget: 100}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "200") || !strings.Contains(reason, "100") {
		t.Fatalf("expected the reason to name the dead bytes and the budget, got %q", reason)
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Fatalf("expected the dead spool file to be removed, stat error: %v", err)
	}
	if _, err := os.Stat(livePath); err != nil {
		t.Fatalf("expected the live spool file to remain: %v", err)
	}

	way := check.WayBack()
	if !strings.Contains(way, "none") {
		t.Fatalf("expected the way back to say there is nothing to restore, got %q", way)
	}
}

func TestTmpLeftoversNeverTouchesAnUnrelatedFile(t *testing.T) {
	tmp, proc := tlHost(t)
	if err := os.WriteFile(filepath.Join(tmp, "nbs-spool-dead"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, Budget: 100}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("expected the unrelated file to remain untouched: %v", err)
	}
}

func TestTmpLeftoversDeadClaudeSessionDirIsRemovedAndLiveOneKept(t *testing.T) {
	tmp, proc := tlHost(t)
	deadDir := filepath.Join(tmp, "claude-0", "session-dead")
	liveDir := filepath.Join(tmp, "claude-0", "session-live")
	if err := os.MkdirAll(deadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deadDir, "state.json"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveDir, "state.json"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	tlMarkLive(t, proc, liveDir)

	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, Budget: 100}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if _, err := os.Stat(deadDir); !os.IsNotExist(err) {
		t.Fatalf("expected the stale session dir to be removed, stat error: %v", err)
	}
	if _, err := os.Stat(liveDir); err != nil {
		t.Fatalf("expected the live session dir to remain: %v", err)
	}
}

func TestTmpLeftoversProbeCannotTellWhenProcDirIsMissing(t *testing.T) {
	tmp, proc := tlHost(t)
	if err := os.WriteFile(filepath.Join(tmp, "nbs-spool-dead"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: filepath.Join(proc, "does-not-exist"), Budget: 100}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTmpLeftoversGoBuildCacheIsClearedOnlyPastItsOwnBudget(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	if err := os.MkdirAll(goBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goBuildDir, "blob"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}

	var cleaned int
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		GoClean: func(context.Context) error { cleaned++; return nil },
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty over the go-build budget, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "go-build") {
		t.Fatalf("expected the reason to name the go-build cache, got %q", reason)
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("expected go clean -cache to run once, ran %d times", cleaned)
	}
}

func TestTmpLeftoversGoBuildCacheLiveWithAnOpenFileIsLeftAlone(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	if err := os.MkdirAll(goBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	blobPath := filepath.Join(goBuildDir, "blob")
	if err := os.WriteFile(blobPath, make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	tlMarkLive(t, proc, blobPath)

	var cleaned int
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		GoClean: func(context.Context) error { cleaned++; return nil },
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok while a process has go-build open, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "go-build") || !strings.Contains(reason, "live") {
		t.Fatalf("expected the reason to say go-build is live, got %q", reason)
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("expected go clean -cache never to run while go-build is live, ran %d times", cleaned)
	}
	if _, err := os.Stat(blobPath); err != nil {
		t.Fatalf("expected go-build's contents to remain: %v", err)
	}
}

func TestTmpLeftoversGoBuildCacheLiveWithARunningGoProcessIsLeftAlone(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	if err := os.MkdirAll(goBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goBuildDir, "blob"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	pidDir := filepath.Join(proc, "200")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var cleaned int
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		GoClean: func(context.Context) error { cleaned++; return nil },
	}

	verdict, _ := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok while a go process is running, got %s", verdict)
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("expected go clean -cache never to run while a go process is running, ran %d times", cleaned)
	}
}

func TestTmpLeftoversGoBuildCacheUnderBudgetIsNeverCleaned(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	if err := os.MkdirAll(goBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goBuildDir, "blob"), make([]byte, 50), 0o644); err != nil {
		t.Fatal(err)
	}

	var cleaned int
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 1000,
		GoClean: func(context.Context) error { cleaned++; return nil },
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("expected go clean -cache never to run under budget, ran %d times", cleaned)
	}
}

// tlGoBuildPastBudget is a go-build dir holding 200 bytes, past a go-build budget of
// 100, with nothing running.
func tlGoBuildPastBudget(t *testing.T) string {
	t.Helper()
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	if err := os.MkdirAll(goBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goBuildDir, "blob"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	return goBuildDir
}

// tlStartGoProcess is a go process appearing in the fake /proc.
func tlStartGoProcess(t *testing.T, proc string) {
	t.Helper()
	pidDir := filepath.Join(proc, "200")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte("go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A Builder's go process that starts after the cure has looked, and before
// `go clean -cache` has finished, makes go clean exit 1: that is the cure
// losing a race, not the cure failing (mw-gq6.318).
func TestTmpLeftoversGoCleanFailingWhileAGoProcessIsLiveIsSkippedNotFailed(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := tlGoBuildPastBudget(t)

	var cleaned int
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		GoClean: func(context.Context) error {
			cleaned++
			tlStartGoProcess(t, proc)
			return errors.New("exit status 1")
		},
	}

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("expected a go clean that lost the race to a live go process to be skipped, got %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("expected go clean -cache to have been tried once, ran %d times", cleaned)
	}
	if strings.Contains(check.WayBack(), goBuildDir) {
		t.Fatalf("expected the skipped go-build cache not to be reported as freed, got %q", check.WayBack())
	}
}

func TestTmpLeftoversGoCleanFailingWithNoGoProcessLiveStillFails(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := tlGoBuildPastBudget(t)

	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		GoClean: func(context.Context) error { return errors.New("exit status 1") },
	}

	err := check.Cure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "go clean -cache") {
		t.Fatalf("expected a go clean -cache failure with no go process live, got %v", err)
	}
}

func TestTheTmpLeftoversCheckIsQuietWhileItsGoodCureCools(t *testing.T) {
	var check application.DoctorCheck = doctor.NewTmpLeftovers(t.TempDir())
	quiet, ok := check.(application.DoctorCoolingQuiet)
	if !ok || !quiet.QuietWhileCooling() {
		t.Errorf("expected the tmp-leftovers check to be quiet while its good cure cools")
	}
}

// tlGoBuildOf is a go-build dir holding one sparse file of size bytes:
// sized the way the check sizes it, without the disk to hold it.
func tlGoBuildOf(t *testing.T, dir string, size int64) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(dir, "blob")
	f, err := os.Create(blob)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// go-build has its own budget, 2 GB unless a host says otherwise, apart from
// the 200 MB the dead leftovers are held to: a cache 3-5 Builders keep at
// 600 MB is normal, not a fault (mw-gq6.478).
func TestTmpLeftoversGoBuildHasItsOwnTwoGigabyteBudget(t *testing.T) {
	if application.DefaultGoBuildBudgetBytes != 2_000_000_000 {
		t.Fatalf("expected go-build's default budget to be 2 GB, got %d", application.DefaultGoBuildBudgetBytes)
	}
	for _, tc := range []struct {
		name    string
		size    int64
		verdict application.Verdict
		cleans  int
	}{
		{"600 MB", 600_000_000, application.DoctorOK, 0},
		{"2.5 GB", 2_500_000_000, application.DoctorFaulty, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp, proc := tlHost(t)
			goBuildDir := filepath.Join(t.TempDir(), "go-build")
			tlGoBuildOf(t, goBuildDir, tc.size)

			var cleaned int
			check := &doctor.TmpLeftovers{
				TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir,
				GoClean: func(context.Context) error { cleaned++; return nil },
			}
			if verdict, reason := check.Probe(context.Background()); verdict != tc.verdict {
				t.Fatalf("expected %s, got %s (%s)", tc.verdict, verdict, reason)
			}
			if err := check.Cure(context.Background()); err != nil {
				t.Fatalf("curing: %v", err)
			}
			if cleaned != tc.cleans {
				t.Errorf("expected go clean -cache to run %d times, ran %d", tc.cleans, cleaned)
			}
		})
	}
}

// GoBuildBudget, when set, is go-build's budget, and Budget the dead
// leftovers' only.
func TestTmpLeftoversGoBuildBudgetIsApartFromTheLeftoversBudget(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := tlGoBuildPastBudget(t)
	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, Budget: 100, GoBuildBudget: 1000}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected a 200 byte go-build under its own 1000 byte budget ok, got %s (%s)", verdict, reason)
	}
	check.GoBuildBudget = 150
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty || !strings.Contains(reason, "150") {
		t.Fatalf("expected faulty past go-build's own 150 byte budget, got %s (%s)", verdict, reason)
	}
}

// tlDoctor runs the real check through application.Doctor, on a clock the
// test moves.
func tlDoctor(check application.DoctorCheck, now *time.Time) (application.Doctor, *apptest.FakeDoctorNotes) {
	notes := apptest.NewFakeDoctorNotes()
	return application.Doctor{
		Checks: application.DoctorChecks{check},
		State:  apptest.NewFakeDoctorState(),
		Log:    &apptest.FakeDoctorLog{},
		Notes:  notes,
		Host:   "laptop",
		Now:    func() time.Time { return *now },
	}, notes
}

// Three good cures of a go-build regrown by normal builds in one hour, then
// a fourth growth: cured, not damped, and nobody woken (mw-gq6.478).
func TestTmpLeftoversGoBuildRegrownByNormalBuildsIsNeverLeftDamped(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir,
		GoClean: func(context.Context) error { return os.RemoveAll(goBuildDir) },
	}
	now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	doc, notes := tlDoctor(check, &now)

	for i := 0; i < 4; i++ {
		tlGoBuildOf(t, goBuildDir, 2_500_000_000)
		report, err := doc.Run(context.Background(), "", false)
		if err != nil {
			t.Fatalf("growth %d: %v", i+1, err)
		}
		if got := report.Results[0].Verdict; got != "cured" {
			t.Fatalf("growth %d: expected cured, got %s (%s)", i+1, got, report.Results[0].Reason)
		}
		now = now.Add(15 * time.Minute)
	}
	if notes.Sets() != 0 {
		t.Errorf("expected no note to wake anyone, got %d", notes.Sets())
	}
}

// A real fault still damps: a go clean that fails, or one that frees nothing
// because it keeps losing the race to a live go process, three times, leaves
// the fourth fault damped and the run faulty.
func TestTmpLeftoversGoBuildCureThatFailsOrFreesNothingIsStillDamped(t *testing.T) {
	for _, tc := range []struct {
		name    string
		goClean func(proc string) func(context.Context) error
	}{
		{"fails", func(string) func(context.Context) error {
			return func(context.Context) error { return errors.New("exit status 1") }
		}},
		{"frees nothing", func(proc string) func(context.Context) error {
			return func(context.Context) error {
				pidDir := filepath.Join(proc, "200")
				if err := os.MkdirAll(pidDir, 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte("go\n"), 0o644); err != nil {
					return err
				}
				return errors.New("exit status 1")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp, proc := tlHost(t)
			goBuildDir := filepath.Join(t.TempDir(), "go-build")
			tlGoBuildOf(t, goBuildDir, 2_500_000_000)
			check := &doctor.TmpLeftovers{
				TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoClean: tc.goClean(proc),
			}
			now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
			doc, _ := tlDoctor(check, &now)

			for i := 0; i < 3; i++ {
				// The go process that won the last race is gone by this tick.
				_ = os.RemoveAll(filepath.Join(proc, "200"))
				report, _ := doc.Run(context.Background(), "", false)
				if got := report.Results[0].Verdict; got != "cured" && got != "cure-failed" {
					t.Fatalf("try %d: expected a cure tried, got %s (%s)", i+1, got, report.Results[0].Reason)
				}
				now = now.Add(15 * time.Minute)
			}
			_ = os.RemoveAll(filepath.Join(proc, "200"))
			report, err := doc.Run(context.Background(), "", false)
			if got := report.Results[0].Verdict; got != "damped" {
				t.Errorf("expected the fourth fault damped, got %s (%s)", got, report.Results[0].Reason)
			}
			if application.ExitStatus(err) != application.DoctorFaultExit {
				t.Errorf("expected exit %d, got %d", application.DoctorFaultExit, application.ExitStatus(err))
			}
		})
	}
}

func TestTheTmpLeftoversCheckEndsItsEpisodeOnlyWithACureThatFreedSomething(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := tlGoBuildPastBudget(t)
	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		GoClean: func(context.Context) error { return nil }}
	var ends application.DoctorCureEndsEpisode = check
	if ends.CureEndedEpisode() {
		t.Errorf("expected no episode ended before any cure ran")
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if !ends.CureEndedEpisode() {
		t.Errorf("expected a cure that cleaned go-build to end its episode")
	}
}

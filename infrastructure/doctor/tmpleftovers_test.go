package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
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
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, Budget: 100,
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
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, Budget: 100,
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
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, Budget: 100,
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
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, Budget: 1000,
		GoClean: func(context.Context) error { cleaned++; return nil },
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("curing: %v", err)
	}
	if cleaned != 0 {
		t.Fatalf("expected go clean -cache never to run under budget, ran %d times", cleaned)
	}
}

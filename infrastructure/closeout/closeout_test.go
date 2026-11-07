package closeout

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// aHostWithProcesses is a process table holding the given pids.
func aHostWithProcesses(t *testing.T, pids ...int) string {
	t.Helper()
	proc := t.TempDir()
	for _, pid := range pids {
		if err := os.Mkdir(filepath.Join(proc, strconv.Itoa(pid)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return proc
}

func TestReadOfNothingWrittenIsNoMarks(t *testing.T) {
	marks := &Marks{Dir: filepath.Join(t.TempDir(), "closing-out")}
	got, err := marks.Read(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no marks, got %+v, err %v", got, err)
	}
}

func TestAMarkOfARunningProcessIsReadBackAndClearedAway(t *testing.T) {
	ctx := context.Background()
	marks := &Marks{Dir: filepath.Join(t.TempDir(), "closing-out"), Proc: aHostWithProcesses(t, 4242), Pid: 4242}
	since := time.Date(2026, 10, 9, 19, 22, 0, 0, time.UTC)
	want := application.CloseOutMark{Story: "mw-5r3p30.114", Since: since, Calming: true}

	if err := marks.Write(ctx, want); err != nil {
		t.Fatalf("writing: %v", err)
	}
	got, err := marks.Read(ctx)
	if err != nil || len(got) != 1 || got[0].Story != want.Story || !got[0].Since.Equal(since) || !got[0].Calming {
		t.Fatalf("expected %+v back, got %+v, err %v", want, got, err)
	}

	want.Calming = false
	if err := marks.Write(ctx, want); err != nil {
		t.Fatalf("rewriting: %v", err)
	}
	if got, _ := marks.Read(ctx); len(got) != 1 || got[0].Calming {
		t.Fatalf("expected the mark replaced, got %+v", got)
	}

	if err := marks.Clear(ctx, want.Story); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if got, _ := marks.Read(ctx); len(got) != 0 {
		t.Fatalf("expected none after clearing, got %+v", got)
	}
	if err := marks.Clear(ctx, want.Story); err != nil {
		t.Fatalf("clearing a mark that is not there should be no error: %v", err)
	}
}

func TestAMarkWhoseProcessIsGoneIsNoMark(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "closing-out")
	writer := &Marks{Dir: dir, Pid: 4242}
	if err := writer.Write(ctx, application.CloseOutMark{Story: "mw-x.1", Since: time.Now()}); err != nil {
		t.Fatalf("writing: %v", err)
	}

	reader := &Marks{Dir: dir, Proc: aHostWithProcesses(t, 1)}
	if got, err := reader.Read(ctx); err != nil || len(got) != 0 {
		t.Fatalf("expected a dead close-out's mark to read as none, got %+v, err %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mw-x.1")); !os.IsNotExist(err) {
		t.Fatalf("expected the dead mark removed, stat err %v", err)
	}
}

// A write names its temp file for itself alone: something already sitting at
// the old fixed '<mark>.new' name is no obstacle (mw-gq6.1042).
func TestWriteIsNotBlockedByAnythingAtTheOldFixedTempName(t *testing.T) {
	dir := t.TempDir()
	marks := &Marks{Dir: dir, Proc: aHostWithProcesses(t, 4242), Pid: 4242}
	if err := os.Mkdir(filepath.Join(dir, "mw-1.new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := marks.Write(context.Background(), application.CloseOutMark{Story: "mw-1", Since: time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("writing with the old temp name taken: %v", err)
	}
	got, err := marks.Read(context.Background())
	if err != nil || len(got) != 1 || got[0].Story != "mw-1" {
		t.Fatalf("expected the mark back, got %+v, %v", got, err)
	}
}

// A temp file a killed writer left is not read as a mark.
func TestAWriterKilledHalfWayLeavesNoMarkToRead(t *testing.T) {
	dir := t.TempDir()
	marks := &Marks{Dir: dir}
	if err := os.WriteFile(filepath.Join(dir, "mw-2.123456.new"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := marks.Read(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("expected a leftover temp file to read as no mark, got %+v, %v", got, err)
	}
}

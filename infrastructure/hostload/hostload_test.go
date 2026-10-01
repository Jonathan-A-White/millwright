package hostload_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostload"
)

func TestProcReadsTheFirstFieldAsTheLoad(t *testing.T) {
	file := filepath.Join(t.TempDir(), "loadavg")
	if err := os.WriteFile(file, []byte("3.25 2.10 1.00 2/345 6789\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := hostload.Proc{File: file, Cores: 16}.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Load != 3.25 || got.Cores != 16 || got.Busy() {
		t.Errorf("got %+v", got)
	}
}

func TestProcSaysWhatItCouldNotRead(t *testing.T) {
	file := filepath.Join(t.TempDir(), "loadavg")
	if _, err := (hostload.Proc{File: file}).Load(context.Background()); err == nil {
		t.Error("expected an error for a missing file")
	}
	if err := os.WriteFile(file, []byte("busy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (hostload.Proc{File: file}).Load(context.Background()); err == nil {
		t.Error("expected an error for a file that holds no number")
	}
}

func TestTheRealLoadIsReadOnThisHost(t *testing.T) {
	if _, err := os.Stat("/proc/loadavg"); err != nil {
		t.Skip("no /proc/loadavg here")
	}
	got, err := hostload.Proc{}.Load(context.Background())
	if err != nil || got.Cores < 1 {
		t.Errorf("got %+v, %v", got, err)
	}
}

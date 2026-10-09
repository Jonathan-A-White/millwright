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

func TestProcReadsMemAvailableInMegabytes(t *testing.T) {
	dir := t.TempDir()
	load := filepath.Join(dir, "loadavg")
	mem := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(load, []byte("1.00 1.00 1.00 1/1 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meminfo := "MemTotal:       12582912 kB\nMemFree:          100000 kB\nMemAvailable:    3145728 kB\nBuffers:          1000 kB\n"
	if err := os.WriteFile(mem, []byte(meminfo), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := hostload.Proc{File: load, MemFile: mem, Cores: 4}.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.MemKnown || got.MemAvailableMB != 3072 {
		t.Errorf("got %+v, want 3072 MB known", got)
	}
}

func TestProcLeavesMemoryUnknownWhenItCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	load := filepath.Join(dir, "loadavg")
	if err := os.WriteFile(load, []byte("1.00 1.00 1.00 1/1 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing, err := hostload.Proc{File: load, MemFile: filepath.Join(dir, "none"), Cores: 4}.Load(context.Background())
	if err != nil || missing.MemKnown {
		t.Errorf("a missing meminfo: got %+v, %v; want the load read and the memory unknown", missing, err)
	}
	noLine := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(noLine, []byte("MemTotal: 1 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := hostload.Proc{File: load, MemFile: noLine, Cores: 4}.Load(context.Background())
	if err != nil || got.MemKnown || got.Load != 1 {
		t.Errorf("a meminfo with no MemAvailable: got %+v, %v", got, err)
	}
}

func TestTheRealMemoryIsReadOnThisHost(t *testing.T) {
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skip("no /proc/meminfo here")
	}
	got, err := hostload.Proc{}.Load(context.Background())
	if err != nil || !got.MemKnown || got.MemAvailableMB < 1 {
		t.Errorf("got %+v, %v", got, err)
	}
}

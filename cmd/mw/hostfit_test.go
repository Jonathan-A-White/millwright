package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

func fitGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestHostFitFindsTrackedFilesThatAreEmptyOnDiskAndLeavesTheOnesMeantToBe(t *testing.T) {
	dir := t.TempDir()
	fitGit(t, dir, "init", "-q")
	for _, name := range []string{"a.go", "b.go", "sub/c.go", "keep.go"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A file that is meant to be empty, committed so.
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fitGit(t, dir, "add", ".")
	fitGit(t, dir, "commit", "-q", "-m", "all")
	// The unclean death: three become empty on disk.
	for _, name := range []string{"a.go", "b.go", "sub/c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A file removed from disk is a different matter and is not said.
	if err := os.Remove(filepath.Join(dir, "keep.go")); err != nil {
		t.Fatal(err)
	}
	got, err := emptyTracked(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "b.go", "sub/c.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty tracked files: got %v, want %v", got, want)
	}
}

func TestHostFitReadsMemoryAndSwapTotalsInMegabytes(t *testing.T) {
	got, err := meminfoLimits(strings.NewReader("MemTotal:       12582912 kB\nMemFree: 1 kB\nSwapTotal:       8388608 kB\nSwapFree: 8388608 kB\n"), application.HostFitLimits{Cap: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got.MemTotalMB != 12288 || got.SwapTotalMB != 8192 || got.Cap != 2 {
		t.Fatalf("limits: %+v", got)
	}
}

func TestHostFitSaysTheBeadsEnvFileByNameAndNeverReadsIt(t *testing.T) {
	home := t.TempDir()
	probes := hostFitProbes{home: home}
	name, present, err := probes.BeadsEnv(context.Background())
	if err != nil || present || !strings.HasSuffix(name, "beads.env") {
		t.Fatalf("no file: %q %v %v", name, present, err)
	}
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mode 000: it could not be read, and the answer is still that it is there.
	if err := os.WriteFile(filepath.Join(dir, "beads.env"), []byte("BEADS_DOLT_PASSWORD=secret\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, present, err := probes.BeadsEnv(context.Background()); err != nil || !present {
		t.Fatalf("the file is there: %v %v", present, err)
	}
}

func TestHostFitIsOneCommandOfMwHostAndTakesJSON(t *testing.T) {
	found, _, err := newRootCmd().Find([]string{"host", "fit"})
	if err != nil || found.Name() != "fit" {
		t.Fatalf("mw host fit: %v", err)
	}
	if found.Flags().Lookup("json") == nil {
		t.Fatal("mw host fit has no --json")
	}
}

// [cloud.caps] is the per-host cap table (laptop = 5, desktop = 2): its names
// are hosts, not cloud boxes, so the desktop, the one Boost, is still asked
// (mw-6ww.185.22).
func TestBoostFitAsksTheDesktopWhenCloudCapsNamesIt(t *testing.T) {
	book := func(context.Context) (application.CloudState, error) {
		return application.CloudState{Boxes: []application.CloudBox{{Name: "cloud9"}}}, nil
	}
	reach := map[string]string{"vps": "ssh vps", "desktop": "ssh desktop", "laptop": "ssh laptop", "cloud9": "ssh cloud9"}
	check := doctor.NewBoostFit(&apptest.FakeHomeFile{Text: "laptop 2026-09-29T00:10:00Z mw@laptop"}, "laptop", reach, doctor.New(t.TempDir()))
	check.Retired = []string{"vps"}
	check.Cloud = cloudBoxNamesFrom(book)
	var asked []string
	check.Ask = func(_ context.Context, argv []string) ([]byte, int, error) {
		asked = append(asked, argv[1])
		out, _ := json.Marshal(application.HostFitReport{Commit: "abc1234"})
		return out, 0, nil
	}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK || strings.Contains(reason, "no Boost") {
		t.Fatalf("verdict %v %q", verdict, reason)
	}
	if !reflect.DeepEqual(asked, []string{"desktop"}) {
		t.Fatalf("asked %v, want [desktop]", asked)
	}
}

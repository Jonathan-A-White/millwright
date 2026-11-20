package application

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeTmpDisk struct {
	entries []TmpEntry
	used    int64
	removed []string
}

func (f *fakeTmpDisk) Entries(context.Context, string) ([]TmpEntry, error) { return f.entries, nil }
func (f *fakeTmpDisk) Used(context.Context, string) (int64, error)         { return f.used, nil }
func (f *fakeTmpDisk) Remove(path string) error                            { f.removed = append(f.removed, path); return nil }

// mw-d40q096: /tmp held 254 GB on the Laptop, and the doctor named nothing.
func TestTmpSizeCheckNamesTheDirOverTwentyGBAndCuresByTheTidy(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	disk := &fakeTmpDisk{
		used: 21_500_000_000,
		entries: []TmpEntry{
			{Path: "/tmp/TestOld1/", Size: 900_000_000, Used: now.Add(-48 * time.Hour)},
			{Path: "/tmp/node-compile-cache/a", Size: 5, Used: now.Add(-time.Hour)},
		},
	}
	check := &TmpSizeCheck{Host: "laptop", Dir: "/tmp", Disk: disk, Now: func() time.Time { return now }}
	verdict, why := check.Probe(context.Background())
	if verdict != DoctorFaulty || !strings.Contains(why, "laptop") || !strings.Contains(why, "/tmp") || !strings.Contains(why, "21.5 GB") {
		t.Fatalf("a 21.5 GB /tmp: got %v %q", verdict, why)
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(disk.removed) != 1 || disk.removed[0] != "/tmp/TestOld1/" {
		t.Errorf("the cure removed %v, want only the old entry", disk.removed)
	}
	if !check.CureEndedEpisode() {
		t.Error("a cure that freed files should end the episode")
	}
}

func TestTmpSizeCheckIsOkAtTwentyGB(t *testing.T) {
	disk := &fakeTmpDisk{used: 20_000_000_000}
	check := &TmpSizeCheck{Host: "h", Dir: "/tmp", Disk: disk}
	if verdict, why := check.Probe(context.Background()); verdict != DoctorOK {
		t.Errorf("20 GB is not over 20 GB: %v %q", verdict, why)
	}
}

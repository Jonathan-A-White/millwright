package application

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeGoCache struct {
	entries []GoCacheEntry
	removed []string
}

func (f *fakeGoCache) Entries(context.Context, string) ([]GoCacheEntry, error) { return f.entries, nil }
func (f *fakeGoCache) Remove(path string) error                                { f.removed = append(f.removed, path); return nil }

func TestGoCacheSizeCheckNamesAHostOverTheLimitAndCuresByTheTidy(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	disk := &fakeGoCache{entries: []GoCacheEntry{
		{Path: "ab/old-a", Size: 25_000_000_000, Used: now.Add(-48 * time.Hour)},
		{Path: "cd/new-a", Size: 6_000_000_000, Used: now.Add(-time.Hour)},
	}}
	check := &GoCacheSizeCheck{Host: "cloud2", Dir: "/c", Disk: disk, Now: func() time.Time { return now }}
	verdict, why := check.Probe(context.Background())
	if verdict != DoctorFaulty || !strings.Contains(why, "cloud2") || !strings.Contains(why, "31.0 GB") {
		t.Fatalf("a 31 GB cache: got %v %q", verdict, why)
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(disk.removed) != 1 || disk.removed[0] != "ab/old-a" {
		t.Errorf("the cure removed %v, want only the old file", disk.removed)
	}
	if !check.CureEndedEpisode() {
		t.Error("a cure that freed files should end the episode")
	}
}

func TestGoCacheSizeCheckIsOkAtThirtyGB(t *testing.T) {
	disk := &fakeGoCache{entries: []GoCacheEntry{{Path: "ab/x-a", Size: 30_000_000_000}}}
	check := &GoCacheSizeCheck{Host: "h", Dir: "/c", Disk: disk}
	if verdict, why := check.Probe(context.Background()); verdict != DoctorOK {
		t.Errorf("30 GB is not over 30 GB: %v %q", verdict, why)
	}
}

package vault_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestSnapshotsAreOneJSONLineADayAppendedAndReadBackOldestFirst(t *testing.T) {
	v := vault.New(t.TempDir())
	ctx := context.Background()
	if got, err := v.Snapshots(ctx); err != nil || len(got) != 0 {
		t.Fatalf("a vault with no file read as %v, %v", got, err)
	}
	attempts := 1.25
	for _, s := range []application.MetricSnapshot{
		{Day: "2026-10-19", Landed: 3, Refused: 1, RefusalRate: 25, AttemptsPerLanding: &attempts},
		{Day: "2026-10-20", Landed: 4},
	} {
		if err := v.Append(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := v.Snapshots(ctx)
	if err != nil || len(got) != 2 || got[0].Day != "2026-10-19" || got[1].Day != "2026-10-20" {
		t.Fatalf("read back %+v, %v", got, err)
	}
	if got[0].AttemptsPerLanding == nil || *got[0].AttemptsPerLanding != 1.25 || got[1].AttemptsPerLanding != nil {
		t.Errorf("the metrics a day had were not kept as they were: %+v", got)
	}
	raw, _ := os.ReadFile(v.SnapshotsPath())
	if lines := strings.Count(string(raw), "\n"); lines != 2 {
		t.Errorf("the file holds %d lines, want 2:\n%s", lines, raw)
	}
}

func TestASnapshotAfterAFileWithNoFinalNewlineStartsALine(t *testing.T) {
	v := vault.New(t.TempDir())
	ctx := context.Background()
	if err := v.Append(ctx, application.MetricSnapshot{Day: "2026-10-19"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(v.SnapshotsPath())
	if err := os.WriteFile(v.SnapshotsPath(), []byte(strings.TrimSuffix(string(raw), "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := v.Append(ctx, application.MetricSnapshot{Day: "2026-10-20"}); err != nil {
		t.Fatal(err)
	}
	if got, err := v.Snapshots(ctx); err != nil || len(got) != 2 {
		t.Errorf("read back %+v, %v; want both days", got, err)
	}
}

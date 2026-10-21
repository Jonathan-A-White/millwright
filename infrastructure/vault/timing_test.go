package vault_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestTheTimingHistoryIsTheNewestResultAndTheNewestPassingOne(t *testing.T) {
	v := vault.New(t.TempDir())
	ctx := context.Background()
	if _, kept, err := v.LatestTiming(ctx, "lampas"); err != nil || kept {
		t.Fatalf("an empty vault: kept %v, err %v", kept, err)
	}
	put := func(name, contents string) {
		t.Helper()
		if _, err := v.PutTimingFile(ctx, "lampas", name, contents); err != nil {
			t.Fatal(err)
		}
	}
	put("2026-10-19.json", `{"rig":"lampas","sha":"aaa","passed":true}`)
	put("2026-10-20.json", `{"rig":"lampas","sha":"bbb","passed":false}`)
	put("2026-10-20.log", "the log")

	newest, kept, err := v.LatestTiming(ctx, "lampas")
	if err != nil || !kept || newest.Day != "2026-10-20" || newest.Result.Sha != "bbb" {
		t.Errorf("newest = %+v, %v, %v", newest, kept, err)
	}
	passing, kept, err := v.LatestPassingTiming(ctx, "lampas")
	if err != nil || !kept || passing.Day != "2026-10-19" || passing.Result.Sha != "aaa" {
		t.Errorf("passing = %+v, %v, %v", passing, kept, err)
	}
	if got, _ := v.PutTimingFile(ctx, "lampas", "x.log", ""); got != application.TimingDir+"/lampas/x.log" {
		t.Errorf("path = %q", got)
	}
	if _, err := v.PutTimingFile(ctx, "../lampas", "x.log", ""); err == nil {
		t.Errorf("a rig name with .. was accepted")
	}
}

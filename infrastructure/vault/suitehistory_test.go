package vault_test

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestLatestSuiteIsTheNewestDatedJSONFileOfTheRig(t *testing.T) {
	v := vault.New(t.TempDir())
	ctx := context.Background()
	if _, kept, err := v.LatestSuite(ctx, "lampas"); err != nil || kept {
		t.Fatalf("a rig that never ran has no result, got kept=%v: %v", kept, err)
	}
	for name, contents := range map[string]string{
		"2026-10-11.json": `{"rig":"lampas","sha":"aaa","passed":true}`,
		"2026-10-13.json": `{"rig":"lampas","sha":"bbb","passed":false,"failing_files":[{"file":"src/a.test.ts"}]}`,
		"2026-10-14.log":  "the log of a day whose summary is not written yet",
	} {
		if _, err := v.PutSuiteFile(ctx, "lampas", name, contents); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := v.PutSuiteFile(ctx, "other", "2026-10-15.json", `{"sha":"zzz"}`); err != nil {
		t.Fatal(err)
	}
	got, kept, err := v.LatestSuite(ctx, "lampas")
	if err != nil || !kept {
		t.Fatalf("expected a result, got kept=%v: %v", kept, err)
	}
	if got.Day != "2026-10-13" || got.Result.Sha != "bbb" || got.Result.Passed || len(got.Result.Failing) != 1 {
		t.Fatalf("expected the 13th's result, got %+v", got)
	}
}

func TestLatestSuiteRefusesAFileThatIsNotJSON(t *testing.T) {
	v := vault.New(t.TempDir())
	if _, err := v.PutSuiteFile(context.Background(), "lampas", "2026-10-13.json", "not json"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.LatestSuite(context.Background(), "lampas"); err == nil {
		t.Fatal("expected a refusal")
	}
}

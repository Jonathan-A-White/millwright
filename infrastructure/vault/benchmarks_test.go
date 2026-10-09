package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestBenchmarksAreReadFromEveryResultFileThatHoldsOne(t *testing.T) {
	dir := t.TempDir()
	write := func(story, name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "runs", story), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "runs", story, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("mw-a.1", "result.json", `{"subtype":"success","host":"laptop","rig":"lampas","benchmark_at":"2026-10-09T12:00:00Z","gate_seconds":310,"landed":true,"actual_s":900}`)
	write("mw-a.1", "result-2.json", `{"subtype":"success","host":"desktop","rig":"lampas","benchmark_at":"2026-10-08T12:00:00Z","gate_seconds":60}`)
	write("mw-a.2", "result.json", `{"subtype":"success"}`)
	write("mw-a.3", "boot.md", `gate_seconds`)
	write("mw-a.4", "result.json", `not json "gate_seconds"`)

	got, err := vault.Benchmarks{Vault: vault.New(dir)}.Recent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Host != "desktop" || got[1].Host != "laptop" || got[1].Story != "mw-a.1" || !got[1].Landed || got[1].ActualS != 900 {
		t.Errorf("expected two benchmarks, oldest first, got %+v", got)
	}
	if none, err := (vault.Benchmarks{Vault: vault.New(t.TempDir())}).Recent(context.Background()); err != nil || len(none) != 0 {
		t.Errorf("expected none from a vault with no runs, got %+v, %v", none, err)
	}
}

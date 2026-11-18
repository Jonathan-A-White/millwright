package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw land hands the smoke of a landed batch to `mw land-smoke` in a process of
// its own, and does not wait for it (mw-apvyg8j).
func TestLandSmokeApartBeginsTheSmokeAsAProcessOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	said := filepath.Join(dir, "said")
	self := filepath.Join(dir, "mw")
	script := "#!/bin/sh\necho \"$@\" > " + said + "\n"
	if err := os.WriteFile(self, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	apart := landSmokeApart{self: self, logDir: filepath.Join(dir, "log"), noScope: true}
	err := apart.Begin(context.Background(), application.LandedSmoke{
		Rig: "lampas", Number: 7, Target: "main", Commit: "abc1234", Story: "lp-9", Apps: []string{"tutor", "scribe"},
	})
	if err != nil {
		t.Fatalf("expected the smoke begun, got %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(said)
		if err == nil && strings.HasSuffix(string(raw), "\n") {
			want := "land-smoke lampas --batch 7 --target main --commit abc1234 --story lp-9 --apps tutor,scribe"
			if got := strings.TrimSpace(string(raw)); got != want {
				t.Fatalf("expected the process begun as %q, got %q", want, got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the process was never begun")
		}
		time.Sleep(20 * time.Millisecond) //mw:realclock waiting for a process begun apart to write its file
	}
}

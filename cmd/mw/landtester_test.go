package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

func writeTesterConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, config.File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mw-gq6.962: mw land built its Next without the Tester trial, so the spring
// mw-gq6.961 added to the batch landing never found the trial on and filed
// nothing. The BatchLand mw land runs carries the trial of the config, as the
// Next of mw next does.
func TestTheBatchLandersNextCarriesTheTesterTrialOfTheConfig(t *testing.T) {
	writeTesterConfig(t, "[tester]\nrigs = [\"lampas\"]\nuntil = \"2099-01-01T00:00:00Z\"\n")
	var stderr bytes.Buffer
	dir := t.TempDir()

	batch := batchLandFor(application.Next{}, mwVault(dir, "laptop"), false, false, &stderr)

	if !batch.Next.Tester.On("lampas", time.Now()) {
		t.Fatalf("the batch lander's Next has no Tester trial on for lampas: %+v", batch.Next.Tester)
	}
	if batch.Next.Tester.On("other", time.Now()) {
		t.Errorf("the trial is on for a rig the table does not name")
	}
	if stderr.Len() != 0 {
		t.Errorf("a readable table was said on stderr: %s", stderr.String())
	}
}

// A [tester] table that cannot be read is said on stderr and stops no landing.
func TestAnUnreadableTesterTableIsSaidAndStopsNoBatchLanding(t *testing.T) {
	writeTesterConfig(t, "[tester]\nrigs = [\"lampas\"]\nuntil = \"tomorrow\"\n")
	var stderr bytes.Buffer
	dir := t.TempDir()

	batch := batchLandFor(application.Next{}, mwVault(dir, "laptop"), false, false, &stderr)

	if batch.Next.Tester.On("lampas", time.Now()) {
		t.Error("an unreadable table sprang a trial")
	}
	if !strings.Contains(stderr.String(), "no Tester story will be sprung") || !strings.Contains(stderr.String(), "until") {
		t.Errorf("the unreadable table was not said on stderr: %q", stderr.String())
	}
}

package main

import (
	"strings"
	"testing"
)

// mw grist smoke asks for the app, and with no test key says where one is
// wanted and that it is no key mw makes: only a key the backend holds a licence
// for is of any use.
func TestGristSmokeNeedsTheAppAndATestKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", t.TempDir())
	t.Setenv("MW_HOST", "laptop")
	t.Setenv("MW_GRIST_TEST_KEY_FILE", "")
	t.Setenv("MW_GRIST_KEY_FILE", "")
	t.Setenv("MW_POSTERN_KEY_FILE", "")
	if _, err := runMw(t, "grist", "smoke"); err == nil {
		t.Error("expected mw grist smoke to want the app")
	}
	out, err := runMw(t, "grist", "smoke", "trade-tracker")
	if err == nil || !strings.Contains(err.Error(), "no grist test key at "+home+"/.config/mw/grist-test.key") {
		t.Fatalf("expected the missing test key named, got %v\n%s", err, out)
	}
}

// mw-gq6.339: --json is the smoke alone, what a landing asks of the mw it built:
// it needs no vault, and with no test key says so on stderr, exiting non-zero,
// which the landing reads as a smoke that could not be made.
func TestGristSmokeJSONNeedsOnlyTheTestKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_GRIST_TEST_KEY_FILE", "")
	t.Setenv("MW_GRIST_KEY_FILE", "")
	t.Setenv("MW_POSTERN_KEY_FILE", "")
	out, err := runMw(t, "grist", "smoke", "trade-tracker", "--json")
	if err == nil || !strings.Contains(err.Error(), "no grist test key at "+home+"/.config/mw/grist-test.key") {
		t.Fatalf("expected the missing test key named, got %v\n%s", err, out)
	}
}

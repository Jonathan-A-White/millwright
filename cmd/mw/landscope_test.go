package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw land runs its own application.Next (batchLandFor), so a scope the flags
// name must be wired there too (mw-6ww.119.11.1.2).
func TestTheBatchLandersScopeIsWhatTheFlagsNamed(t *testing.T) {
	scope := application.LandScope{One: true, Only: []string{"lp-1", "lp-2"}, Without: []string{"lp-3"}}
	batch := batchLandFor(application.Next{}, mwVault(t.TempDir(), "laptop"), false, scope, &bytes.Buffer{})
	if !batch.One || !reflect.DeepEqual(batch.Only, scope.Only) || !reflect.DeepEqual(batch.Without, scope.Without) {
		t.Fatalf("expected the scope in the batch lander, got one=%v only=%v without=%v", batch.One, batch.Only, batch.Without)
	}
}

// mw land --help says what --only, --without and --set-aside do.
func TestMwLandHelpSaysHowAStoryIsLeftOutOrSetAside(t *testing.T) {
	var out bytes.Buffer
	cmd := newLandCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--without", "--only", "--set-aside <story> --why", "SET-ASIDE comment", "two\nhalves", "one batch"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("mw land --help does not say %q:\n%s", want, out.String())
		}
	}
}

// --set-aside lands nothing, so it takes no other flag, and --why needs it.
func TestSetAsideTakesOnlyItsWhy(t *testing.T) {
	for name, args := range map[string][]string{
		"why alone":      {"lampas", "--why", "because"},
		"with --only":    {"lampas", "--set-aside", "lp-1", "--why", "x", "--only", "lp-2"},
		"with --dry-run": {"lampas", "--set-aside", "lp-1", "--why", "x", "--dry-run"},
		"with --without": {"lampas", "--set-aside", "lp-1", "--why", "x", "--without", "lp-2"},
		"with --one":     {"lampas", "--set-aside", "lp-1", "--why", "x", "--one"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("MW_VAULT", dir)
			t.Setenv("MW_HOST", "laptop")
			cmd := newLandCmd()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "--why") && !strings.Contains(err.Error(), "--set-aside") {
				t.Fatalf("expected a refusal naming the flags, got %v", err)
			}
		})
	}
}

package application_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A rig's [named_tests] line (the command that runs just the failing tests by
// name) goes to a Boost too, or a failing test is never run alone on main
// there and mw next cannot tell main's red from the story's (mw-gq6.1223).
func TestExportBoostCopiesNamedTests(t *testing.T) {
	var out bytes.Buffer
	target := &memoryConfig{path: "/vault/" + application.BoostConfigFile}
	err := application.ConfigExportBoost{
		Config: &memoryConfig{path: "/home/.config/mw/config.toml", has: true, data: []byte(`[tests]
millwright = "make test"

[named_tests]
postern = "npm test -- --grep {tests}"
lampas = "npx vitest run -t {tests}"
`)},
		Target: target,
		Vault:  &apptest.FakeVaultFiles{},
		Home:   &apptest.FakeHomeFile{Text: "laptop 2026-10-15T17:03:00Z mw@laptop"},
		Host:   "laptop",
		Out:    &out,
	}.Run(endedContext(), false)
	if err != nil {
		t.Fatalf("export-boost: %v\n%s", err, &out)
	}
	text := string(target.data)
	for _, want := range []string{
		"\n[named_tests]\n",
		"lampas = \"npx vitest run -t {tests}\"\n",
		"postern = \"npm test -- --grep {tests}\"\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the exported file lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "lampas = ") + strings.Count(text, "postern = "); n != 2 {
		t.Errorf("want one line for each of the two rigs, got %d:\n%s", n, text)
	}
	if !strings.Contains(out.String(), "[named_tests] lampas: missing") || !strings.Contains(out.String(), "[named_tests] postern: missing") {
		t.Errorf("the export does not say it brought the lines level:\n%s", &out)
	}
}

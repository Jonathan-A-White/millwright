package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestTheFilingDocSaysAMillwrightStoryNamesItsExpect(t *testing.T) {
	raw, err := os.ReadFile("wayfinding-on-beads.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(raw), "## Filing a millwright story")
	if !ok {
		t.Fatal("wayfinding-on-beads.md has no '## Filing a millwright story' section")
	}
	for _, want := range []string{"Expect: <metric> <up|down>", "refusals", "attempts/landing", "bugs/landing",
		"dispatch-to-landing", "tokens/landing", "$/landing", "escaped bugs", "review queue", "demos waiting", "Expect: none", "FACTORY CHANGES", "WORSE", "10%", "metrics/factory.jsonl"} {
		if !strings.Contains(section, want) {
			t.Errorf("the section does not say %q", want)
		}
	}
}

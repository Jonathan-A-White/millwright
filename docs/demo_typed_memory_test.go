package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestTheTypedMemoryDemoGivesFiveNumberedStepsWithTheRealLabels(t *testing.T) {
	raw, err := os.ReadFile("demo-typed-memory.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	if _, demo, ok := strings.Cut(doc, "## The demo"); !ok {
		t.Fatal("the page has no '## The demo' section")
	} else {
		steps := regexp.MustCompile(`(?m)^[0-9]+\. `).FindAllString(demo, -1)
		if len(steps) != 5 {
			t.Errorf("expected five numbered steps under '## The demo', got %d", len(steps))
		}
		if n := strings.Count(demo, "Right looks like:"); n < 5 {
			t.Errorf("expected 'Right looks like:' for each of five steps, got %d", n)
		}
	}

	for _, want := range []string{
		"mw memory list lampas",
		"seats/builder/rigs/lampas/" + application.FactsDir + "/",
		"superseded-by:",
		"supersedes:",
		"reason:",
		"status: current",
		"source:",
		application.RigMemoryHeading,
		"mw status",
		"Looks good",
		"mw-rz2dak.8",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the page does not name %q", want)
		}
	}
}

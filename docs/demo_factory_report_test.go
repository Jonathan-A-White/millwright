package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTheFactoryReportDemoNamesTheLiveStepsAndTheSections(t *testing.T) {
	raw, err := os.ReadFile("demo-factory-report.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	_, demo, ok := strings.Cut(doc, "## The demo")
	if !ok {
		t.Fatal("the page has no '## The demo' section")
	}
	steps := regexp.MustCompile(`(?m)^[0-9]+\. `).FindAllString(demo, -1)
	if len(steps) == 0 || len(steps) > 6 {
		t.Errorf("expected one to six numbered steps under '## The demo', got %d", len(steps))
	}

	order := []string{"QUALITY", "SPEED", "FUEL", "CHEAP WINS"}
	at := -1
	for _, section := range order {
		i := strings.Index(demo, section)
		if i < 0 {
			t.Errorf("the demo does not name the section %q", section)
			continue
		}
		if i < at {
			t.Errorf("the demo names %q before an earlier section of the report", section)
		}
		at = i
	}

	for _, want := range []string{
		"Factory channel",
		"12:00Z",
		"mw report --rig lampas",
		"mw cause",
		"Looks good",
		"mw-2zh33g.6",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("the page does not name %q", want)
		}
	}
}

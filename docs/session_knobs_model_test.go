package docs_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The research note on session knobs names the current Fable model. A line
// that names the superseded 5.1 must say it is history; any other line is out
// of date.
func TestTheSessionKnobsNoteNamesNoSupersededFableAsCurrent(t *testing.T) {
	raw, err := os.ReadFile("research/claude-code-session-knobs.md")
	if err != nil {
		t.Fatal(err)
	}
	old := regexp.MustCompile(`(?i)fable[ -]5\.1|claude-fable-5-1`)
	for i, line := range strings.Split(string(raw), "\n") {
		if old.MatchString(line) && !strings.Contains(strings.ToLower(line), "history") {
			t.Errorf("line %d names the superseded Fable 5.1 without marking it history: %s", i+1, line)
		}
	}
}

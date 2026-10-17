package domain_test

import (
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestASweepLineIsReadFromAcceptance(t *testing.T) {
	cases := []struct {
		name       string
		acceptance string
		want       domain.SweepVerdict
	}{
		{"none at all", "1. It passes.\n2. The gate passes.", domain.SweepMissing},
		{"empty", "", domain.SweepMissing},
		{"a bare line", "Sweep: every route has the header", domain.SweepGiven},
		{"after a number", "1. It passes.\n7. Sweep: every dispatch gate is listed", domain.SweepGiven},
		{"after a numbered bracket", "1) Sweep: grep the class", domain.SweepGiven},
		{"after a bullet", "- Sweep: grep the class", domain.SweepGiven},
		{"in any case", "sweep: grep the class", domain.SweepGiven},
		{"indented", "   SWEEP: grep the class", domain.SweepGiven},
		{"with nothing after it", "3. Sweep:", domain.SweepEmpty},
		{"none with a reason", "4. Sweep: none, because this is the rule itself.", domain.SweepGiven},
		{"none, because without a comma", "Sweep: none because it is a one-off", domain.SweepGiven},
		{"none with no reason", "Sweep: none", domain.SweepNoneWithoutReason},
		{"none with an empty reason", "Sweep: none, because   ", domain.SweepNoneWithoutReason},
		{"none with a stray full stop", "Sweep: none.", domain.SweepNoneWithoutReason},
		{"the word mid-line is not a line", "1. It passes. Sweep: yes", domain.SweepMissing},
		{"a word that starts the same", "Sweeping: everything", domain.SweepMissing},
		{"a later good line beats nothing", "Sweep: none\nSweep: grep the class", domain.SweepGiven},
		{"a numbered line", "5. Sweep: x", domain.SweepGiven},
		{"a dashed line", "- Sweep: x", domain.SweepGiven},
		{"a starred line", "* Sweep: x", domain.SweepGiven},
		{"a plussed line", "+ Sweep: x", domain.SweepGiven},
		{"no sweep here", "No sweep here", domain.SweepMissing},
		// mw-6drexa.1: the Mayor's numbered items ran on in one line (mw-gq6.469).
		{"a numbered item run on in one line", "1. A unit test. 4. Another test. 5. Sweep: every error return path goes through the one notifier. 6. Gate green.", domain.SweepGiven},
		{"a run-on item saying nothing", "4. It passes. 5. Sweep: 6. Gate green.", domain.SweepEmpty},
		{"a run-on none with no reason", "4. It passes. 5. Sweep: none 6. Gate green.", domain.SweepNoneWithoutReason},
		{"a run-on bracketed item", "1) It passes. 2) Sweep: grep the class", domain.SweepGiven},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domain.SweepOf(c.acceptance); got != c.want {
				t.Errorf("SweepOf(%q) = %v, want %v", c.acceptance, got, c.want)
			}
		})
	}
}

func TestOnlyABugFiledAfterTheRuleOwesASweep(t *testing.T) {
	after := domain.SweepRuleFrom.Add(time.Hour)
	before := domain.SweepRuleFrom.Add(-time.Hour)
	if !domain.OwesASweep("bug", "A story", nil, after) {
		t.Error("a bug type filed after the rule owes one")
	}
	if !domain.OwesASweep("task", "[bug] A story", nil, after) {
		t.Error("a [bug] title filed after the rule owes one")
	}
	if !domain.OwesASweep("task", "A story", []string{"suite", "bug"}, after) {
		t.Error("a bug label filed after the rule owes one")
	}
	if domain.OwesASweep("task", "A story", []string{"suite"}, after) {
		t.Error("a story that is not a bug owes none")
	}
	if domain.OwesASweep("bug", "[bug] old", nil, before) {
		t.Error("a bug filed before the rule owes none")
	}
	if domain.OwesASweep("bug", "[bug] undated", nil, time.Time{}) {
		t.Error("a bug the tracker did not date owes none")
	}
}

func TestSweepInTheDescriptionOnlyIsTold(t *testing.T) {
	cases := []struct {
		name                    string
		acceptance, description string
		want                    bool
	}{
		{"in the description only", "1. It passes.", "Acceptance criteria\nSweep: every X is checked", true},
		{"a numbered line in the description", "", "4. Sweep: every X is checked", true},
		{"nowhere", "1. It passes.", "We should sweep.", false},
		{"in the acceptance field", "Sweep: every X", "Sweep: every X is checked", false},
		{"an empty one in the acceptance field", "Sweep:", "Sweep: every X is checked", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domain.SweepOfStory(c.acceptance, c.description) == domain.SweepInDescription; got != c.want {
				t.Errorf("SweepOfStory(%q, %q) is SweepInDescription: %v, want %v", c.acceptance, c.description, got, c.want)
			}
		})
	}
}

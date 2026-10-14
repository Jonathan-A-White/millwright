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

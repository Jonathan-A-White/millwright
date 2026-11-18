package application

import "testing"

// A phrase that only asks for the Governor's confirmation does not make a fact
// his word; one that names or quotes his decision still does (mw-gq6.1468).
func TestNamesTheGovernor(t *testing.T) {
	for name, c := range map[string]struct {
		sentence string
		want     bool
	}{
		"provisional tail":        {"The services rows are one file; PROVISIONAL, the Governor to confirm.", false},
		"provisional wording":     {"Titles are true only once (wording PROVISIONAL, the Governor's to choose)", false},
		"governor to confirm":     {"A theme hit is offered first, Governor to confirm.", false},
		"opens with His":          {"His rule: every story goes test-first.", true},
		"decided by":              {"Decided by the Governor 2026-10-01: a charter changes only on his word.", true},
		"the Governor's word":     {"the Governor's word on mw-x: land in batches.", true},
		"the Governor said":       {"Every story is test-first, the Governor said.", true},
		"his words":               {"A charter changes only on his words.", true},
		"tail and his own phrase": {"Decided by the Governor: land in batches; PROVISIONAL, the Governor to confirm.", true},
		"plain":                   {"The gateway drops a message larger than the cap.", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := namesTheGovernor(RigFact{Sentence: c.sentence}); got != c.want {
				t.Errorf("namesTheGovernor(%q) = %v, want %v", c.sentence, got, c.want)
			}
		})
	}
}

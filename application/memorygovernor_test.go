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
		// A fact about the guard, quoting the phrase as an example (mw-gq6.1501).
		"fact about the guard": {"namesTheGovernor matches 'the Governor' lower-case-t, so a test fixture saying 'The Governor said' is not protected from make-room or boot room-making.", false},
		"quoted double":        {"The guard reads \"Governor's\" and 'his words' as examples of his phrases.", false},
		"quoted then asserted": {"The guard matches 'the Governor'; the Governor's word on mw-x: land in batches.", true},
		"decided by, again":    {"Decided by the Governor 2026-10-01: land in batches.", true},
		"his rule, again":      {"His rule: every story goes test-first.", true},
		"word on mw-x":         {"the Governor's word on mw-x: land in batches.", true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := namesTheGovernor(RigFact{Sentence: c.sentence}); got != c.want {
				t.Errorf("namesTheGovernor(%q) = %v, want %v", c.sentence, got, c.want)
			}
		})
	}
}

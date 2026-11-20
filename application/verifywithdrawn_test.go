package application

import "testing"

// A VERIFIED written in error is withdrawn by a later comment opening
// UNVERIFIED:, and a VERIFIED after that stands again: the newest of the two
// decides (mw-gq6.1540).
func TestTheNewestOfVerifiedAndUnverifiedDecides(t *testing.T) {
	cases := []struct {
		name  string
		texts []string
		want  bool
	}{
		{"none", []string{"HOW TO CHECK IT: 1. Open the app."}, false},
		{"verified", []string{"VERIFIED by the Mayor."}, true},
		{"verified then unverified", []string{"VERIFIED by the Clerk.", "UNVERIFIED: written in error."}, false},
		{"unverified then verified", []string{"UNVERIFIED: written in error.", "VERIFIED by the Governor's use."}, true},
		{"verified, unverified, verified", []string{"VERIFIED", "UNVERIFIED: oops", "VERIFIED again"}, true},
		{"unverified alone", []string{"UNVERIFIED: nobody looked."}, false},
		{"leading space", []string{"VERIFIED", "  \nUNVERIFIED: oops"}, false},
		{"not a word break", []string{"VERIFIED", "UNVERIFIEDLY said"}, true},
	}
	for _, c := range cases {
		var comments []Comment
		for _, text := range c.texts {
			comments = append(comments, Comment{Text: text})
		}
		if got := verifiedIn(comments); got != c.want {
			t.Errorf("%s: verifiedIn = %v, want %v", c.name, got, c.want)
		}
		if got := newestVerdict(comments).Verified; got != c.want {
			t.Errorf("%s: newestVerdict.Verified = %v, want %v", c.name, got, c.want)
		}
	}
}

// An UNVERIFIED does not undo a NOT MET: only a VERIFIED is withdrawn by it.
func TestUnverifiedLeavesANotMetStanding(t *testing.T) {
	comments := []Comment{{Text: NotMetComment("tx1", "2026-11-20 10:00Z", "mw-x.1")}, {Text: "UNVERIFIED: still not."}}
	if got := newestVerdict(comments); !got.NotMet || got.Fix != "mw-x.1" {
		t.Errorf("expected NOT MET to stand, got %+v", got)
	}
}

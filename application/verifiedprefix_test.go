package application

import "testing"

// mw-27f7myz.6: a VERIFIED written with the time or the seat first counts.
func TestCommentMarksVerifiedSkipsATimeOrSeatPrefix(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"VERIFIED by his word", true},
		{"2026-10-30 19:12Z Mayor 680: VERIFIED (landing Clerk, batch 303)", true},
		{"Mayor 799 2026-11-06 22:10Z: VERIFIED. 6 sources present", true},
		{"2026-11-06T22:10Z VERIFIED", true},
		{"Deputy: VERIFIED by running it", true},
		{"Mayor 680, VERIFIED", true},
		{"NOT VERIFIED", false},
		{"2026-11-05 16:24Z This picture shows the strip. Stays NOT VERIFIED.", false},
		{"LANDED f8a6bc7, NOT YET VERIFIED 2026-11-11 13:56Z", false},
		{"2026-11-11 18:04Z the Mayor 855. His word: Publish. VERIFIED waits on his car check.", false},
		{"PROPOSED match", false},
		{"NOT MET", false},
		{"2026-11-06 22:10Z NOT MET", false},
		{"Mayor 680: NOT VERIFIED", false},
	} {
		if got := commentMarksVerified(tc.text); got != tc.want {
			t.Errorf("commentMarksVerified(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

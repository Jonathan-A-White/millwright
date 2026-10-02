package application

import "testing"

func TestPosternAnswerWord(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"A: Release", "release"},
		{"release", "release"},
		{" B: Hold ", "hold"},
		{"1: Release", "release"},
		{"Hold", "hold"},
		{"A: Release both", "release both"},
		{"AB: Release", "ab: release"},
		{"A:Release", "a:release"},
		{"", ""},
	} {
		if got := posternAnswerWord(tc.in); got != tc.want {
			t.Errorf("posternAnswerWord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsReleaseTapAndHoldTapTakeTheLetterPrefix(t *testing.T) {
	for in, want := range map[string]bool{"A: Release": true, "release": true, "A: Release both": false, "B: Hold": false} {
		if got := isReleaseTap(in); got != want {
			t.Errorf("isReleaseTap(%q) = %v, want %v", in, got, want)
		}
	}
	for in, want := range map[string]bool{"B: Hold": true, " hold ": true, "A: Release": false, "Hold on": false} {
		if got := isHoldTap(in); got != want {
			t.Errorf("isHoldTap(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPosternQuestionOfferedWordsTakeTheLetterPrefix(t *testing.T) {
	note := `{"txid":"q","options":["A: Release","B: Hold"]}`
	if !posternQuestionOfferedRelease(note) {
		t.Error("a card offering 'A: Release' offered Release")
	}
	if !posternQuestionOffered(note, "hold") {
		t.Error("a card offering 'B: Hold' offered Hold")
	}
	if posternQuestionOfferedRelease(`{"txid":"q","options":["A: Release both","B: Hold"]}`) {
		t.Error("'A: Release both' is not Release")
	}
}

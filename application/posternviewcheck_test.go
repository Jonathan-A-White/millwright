package application

import (
	"strings"
	"testing"
)

func TestLandingCheckIsTheFirstSentenceAfterTheMarker(t *testing.T) {
	cases := []struct {
		name     string
		comments []string
		want     string
	}{
		{"none", []string{"a word", "another"}, ""},
		{"colon and full stop dropped", []string{"Landing checked: gate green on main. Then more."}, "gate green on main"},
		{"a line ends it", []string{"Landing checked - all good\nsecond line"}, "all good"},
		{"newest carrying the marker wins", []string{"Landing checked: old.", "chatter", "Landing checked: new."}, "new"},
		{"the marker alone says nothing", []string{"Landing checked."}, ""},
		{"a dotted version does not end it", []string{"Landing checked: bd 1.3.0 answers. Done."}, "bd 1.3.0 answers"},
	}
	for _, c := range cases {
		var comments []Comment
		for _, text := range c.comments {
			comments = append(comments, Comment{Text: text})
		}
		if got := landingCheck(comments); got != c.want {
			t.Errorf("%s: expected %q, got %q", c.name, c.want, got)
		}
	}
}

func TestVerifyTextFindsTheCheckInTheMemoryOfAnOlderRun(t *testing.T) {
	mem := posternSnapshotMemoryEntry{Comments: []PosternSnapshotComment{{Text: "newest"}, {Text: "Landing checked: fine."}}}
	if got := verifyText(StoryDetail{}, mem); got == "" || !strings.Contains(got, "Checked by the Mayor: fine.") {
		t.Errorf("expected the check from the remembered comments, got %q", got)
	}
}

package application

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestBeadHoldsJudgesEachState(t *testing.T) {
	open := StoryDetail{Status: StatusOpen}
	held := StoryDetail{Status: StatusHeld}
	dropped := StoryDetail{Status: StatusClosed}
	landed := StoryDetail{Status: StatusClosed, Labels: []string{RunState + ":" + RunLanded}}
	none := func() []Comment { return nil }
	verified := func() []Comment { return []Comment{{Text: "ran it"}, {Text: "VERIFIED by hand"}} }

	for _, c := range []struct {
		name     string
		bead     StoryDetail
		state    string
		comments func() []Comment
		want     bool
	}{
		{"open is open", open, domain.ExpectOpen, none, true},
		{"held is not open", held, domain.ExpectOpen, none, false},
		{"held is held", held, domain.ExpectHeld, none, true},
		{"open is not held", open, domain.ExpectHeld, none, false},
		{"closed is closed", dropped, domain.ExpectClosed, none, true},
		{"closed and dropped is not landed", dropped, domain.ExpectLanded, none, false},
		{"closed with run:landed is landed", landed, domain.ExpectLanded, none, true},
		{"landed without a VERIFIED comment is not verified", landed, domain.ExpectVerified, none, false},
		{"landed with a VERIFIED comment is verified", landed, domain.ExpectVerified, verified, true},
		{"dropped with a VERIFIED comment is not verified", dropped, domain.ExpectVerified, verified, false},
		{"answered is no state of a bead", open, domain.ExpectAnswered, none, false},
	} {
		if got := beadHolds(c.bead, c.state, c.comments); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

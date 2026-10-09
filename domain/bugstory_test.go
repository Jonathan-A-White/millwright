package domain_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

func TestAStoryIsABugByItsTypeOrByTheTagOnItsTitle(t *testing.T) {
	for _, tc := range []struct {
		kind, title string
		bug         bool
	}{
		{"bug", "Mail shows twice", true},
		{"Bug", "Mail shows twice", true},
		{"task", "[bug] Mail shows twice", true},
		{"", "  [BUG] Mail shows twice", true},
		{"feature", "Mail shows twice", false},
		{"", "Mail shows twice", false},
		{"task", "Fix the [bug] tag", false},
		{"chore", "Debug the mail", false},
	} {
		if got := domain.IsBugStory(tc.kind, tc.title); got != tc.bug {
			t.Errorf("IsBugStory(%q, %q) = %v, want %v", tc.kind, tc.title, got, tc.bug)
		}
	}
}

package domain

import "strings"

// BugTitlePrefix is what a story's title starts with when it files a bug.
const BugTitlePrefix = "[bug]"

// IsBugStory says whether a story is a bug: its type in the tracker is bug, or
// its title starts with the [bug] tag, in either case. A bug story is worked
// from a regression test that reproduces the report (see formulas/).
func IsBugStory(storyType, title string) bool {
	if strings.EqualFold(strings.TrimSpace(storyType), "bug") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(title)), BugTitlePrefix)
}

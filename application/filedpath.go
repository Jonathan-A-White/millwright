package application

import "github.com/Jonathan-A-White/millwright/domain"

// The route a bug filed from a finding runs on: Sonnet at high effort, the
// factory's default. The finder's model and effort are about the finder's own
// work, so they are not copied; a dearer model needs its reason written on the
// bug, by the Mayor (mw-gq6.1426, mw-gq6.1640).
const (
	foundBugModel  = domain.ModelSonnet
	foundBugEffort = domain.EffortHigh
)

// FiledPath is the path a bug filed from another story's path (an unmet
// criterion, main's red, a Tester's finding, a found-not-fixed line) starts
// from, and the one place a filed bug's model and effort are set. The source's
// own keys (size, bump, callers_ok, host_why, max_turns) are left out, as
// Path.WithoutSourceKeys leaves them; its rig, branch, harness, host, formula
// and files stay for the filing to change; and the bug's size, when its filer
// knows one, is its own. Its model and effort come from the bug's own size and
// kind, never from the source's: a finder pathed fable at xhigh does not make a
// small bug run there (mw-gq6.1549, mw-gq6.1550, mw-gq6.1551).
//
// Every size and kind of a filed bug is today the default route; the sizes and
// kinds are the arguments so that a dearer route for one of them is a change
// here and nowhere else.
func FiledPath(source domain.Path, size domain.Size, kind string) domain.Path {
	path := source.WithoutSourceKeys()
	path.Size = size
	path.Model, path.Effort = filedModelAndEffort(size, kind)
	return path
}

// filedModelAndEffort is the route a filed bug of this size and kind runs on:
// the default route for each, until a size or kind earns another.
func filedModelAndEffort(_ domain.Size, _ string) (domain.Model, domain.Effort) {
	return foundBugModel, foundBugEffort
}

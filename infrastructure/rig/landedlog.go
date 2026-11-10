package rig

import (
	"context"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	// landedMerge is the merge of a story's branch into a rig's main as a landing
	// makes it: "Merge branch 'mw/<story>' into HEAD".
	landedMerge = regexp.MustCompile(`^Merge branch '[^'/]+/([a-z][a-z0-9]*-[a-z0-9]+(?:\.[0-9]+)*)'`)
	// landedVersion is the commit that raised a rig's version: "Version X.Y.Z (<story>)".
	landedVersion = regexp.MustCompile(`^Version ([0-9][0-9A-Za-z.+-]*) \(`)
)

// LandedBetween implements application.LandedLog: the stories whose branches were
// merged into the checkout at rigDir after from and up to to, oldest first, and
// the newest version a "Version X.Y.Z (<story>)" commit among them raised. An
// empty from reads nothing: the range would be the whole history.
func (w *Worktrees) LandedBetween(ctx context.Context, rigDir, from, to string) (application.LandedRange, error) {
	var landed application.LandedRange
	if from == "" || to == "" || from == to {
		return landed, nil
	}
	out, err := w.git(ctx, rigDir, "log", "--reverse", "--format=%s", from+".."+to)
	if err != nil {
		return landed, err
	}
	seen := map[string]bool{}
	for _, subject := range strings.Split(out, "\n") {
		subject = strings.TrimSpace(subject)
		if m := landedMerge.FindStringSubmatch(subject); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			landed.Stories = append(landed.Stories, m[1])
		}
		if m := landedVersion.FindStringSubmatch(subject); m != nil {
			landed.Version = m[1]
		}
	}
	return landed, nil
}

var _ application.LandedLog = (*Worktrees)(nil)

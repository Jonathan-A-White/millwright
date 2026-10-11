package rig

import (
	"context"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.GristLandings = (*Worktrees)(nil)

// LandingOf implements application.GristLandings: it finds the commit mw next
// made for the story on the rig's branch, "Version <v> (<story>)" or, for a
// story that raised the version itself, "What's new <v> (<story>)", and reads
// the version off its subject. The branch is read as the rig's origin has it
// when it has it, and then as the checkout does; one the rig does not have
// holds no landing.
func (w *Worktrees) LandingOf(ctx context.Context, rigDir, branch, story string) (application.GristLanding, bool, error) {
	for _, ref := range []string{w.remote + "/" + branch, branch} {
		if _, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
			continue
		}
		landing, found, err := w.landingOn(ctx, rigDir, ref, story)
		if err != nil || found {
			return landing, found, err
		}
	}
	return application.GristLanding{}, false, nil
}

// landingOn is LandingOf on one ref.
func (w *Worktrees) landingOn(ctx context.Context, rigDir, ref, story string) (application.GristLanding, bool, error) {
	out, err := w.git(ctx, rigDir, "log", "-E", "--format=%H%x00%s",
		"--grep=^(Version|What's new) [^ ]+ \\("+regexp.QuoteMeta(story)+"\\)$", ref)
	if err != nil {
		return application.GristLanding{}, false, err
	}
	for _, line := range strings.Split(out, "\n") {
		commit, subject, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		fields := strings.Fields(subject)
		// "Version 0.1.86 (id)" or "What's new 0.1.86 (id)": the version is the
		// word before the story's.
		if len(fields) >= 3 && fields[len(fields)-1] == "("+story+")" {
			return application.GristLanding{Version: fields[len(fields)-2], Commit: commit}, true, nil
		}
	}
	return application.GristLanding{}, false, nil
}

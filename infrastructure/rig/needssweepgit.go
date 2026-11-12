package rig

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Worktrees is the git the needs sweep asks of a rig's checkout.
var _ application.SweepGit = (*Worktrees)(nil)

// sweepMainNames are the refs a rig's main may be, origin's first: a checkout is
// fetched, so origin's is the newer where both exist.
func (w *Worktrees) sweepMainNames() []string {
	remote := w.Remote()
	return []string{remote + "/main", "main", remote + "/master", "master"}
}

// MainRefs implements application.SweepGit: the refs of the rig's main that the
// checkout at dir holds, each with the commit it is at.
func (w *Worktrees) MainRefs(ctx context.Context, dir string) ([]application.SweepRef, error) {
	var refs []application.SweepRef
	for _, name := range w.sweepMainNames() {
		out, err := w.git(ctx, dir, "rev-parse", "--verify", "--quiet", name+"^{commit}")
		if err != nil {
			continue
		}
		refs = append(refs, application.SweepRef{Name: name, Sha: strings.TrimSpace(out)})
	}
	return refs, nil
}

// RevertOf implements application.SweepGit: the commit on ref whose message says
// "This reverts commit <sha>" (git revert writes it) for the commit or for a
// merge of the story's branch, whose message names mw/<bead>' ("Merge branch
// 'mw/<bead>'" or "Merge remote-tracking branch 'origin/mw/<bead>'"): a close
// reason names the branch's tip, and it is the merge that is reverted. A revert
// that a later commit reverts, or that a later merge of the branch follows, does
// not stand. The newest standing revert wins; empty when none does.
func (w *Worktrees) RevertOf(ctx context.Context, dir, ref, commit, bead string) (string, error) {
	out, err := w.git(ctx, dir, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("the checkout at %s lacks commit %s: %w", dir, commit, err)
	}
	named := "mw/" + bead + "'"
	carriers := []string{strings.TrimSpace(out)}
	merges, err := w.git(ctx, dir, "log", ref, "--merges", "--format=%H", "-F", "--grep="+named)
	if err != nil {
		return "", err
	}
	carriers = append(carriers, strings.Fields(merges)...)
	args := []string{"log", ref, "--format=%H", "-F"}
	for _, carrier := range carriers {
		args = append(args, "--grep=This reverts commit "+carrier)
	}
	reverts, err := w.git(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	for _, revert := range strings.Fields(reverts) {
		undone, err := w.git(ctx, dir, "log", ref, "--format=%H", "-F", "--grep=This reverts commit "+revert)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(undone) != "" {
			continue
		}
		relanded, err := w.git(ctx, dir, "log", revert+".."+ref, "--merges", "--format=%H", "-F", "--grep="+named)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(relanded) != "" {
			continue
		}
		return revert, nil
	}
	return "", nil
}

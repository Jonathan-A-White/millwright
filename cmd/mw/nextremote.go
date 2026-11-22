package main

import (
	"context"
	"io"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/peekremote"
)

// sshMayorAnswers is application.MayorAnswers over ssh: mw next run on the host a
// story was worked on, reached as mw retry and mw peek reach it ([hands_hosts],
// and each cloud box at its WireGuard address). The reach is read when a word
// of the Mayor's is first sent elsewhere, not for every mw next (mw-gq6.1622).
type sshMayorAnswers struct{ out io.Writer }

var _ application.MayorAnswers = sshMayorAnswers{}

// NextThere implements application.MayorAnswers.
func (s sshMayorAnswers) NextThere(ctx context.Context, host, storyID string, flags []string) (string, error) {
	reach, gaps, err := storyReach(ctx, s.out)
	if err != nil {
		return "", err
	}
	return peekremote.Remote{Reach: reach, Gaps: gaps}.NextThere(ctx, host, storyID, flags)
}

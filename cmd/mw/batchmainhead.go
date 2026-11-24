package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// mainHeadLimit is how long the follower waits for the origin to say a branch's
// head: the follower's pass is not held up longer by an origin that does not answer.
const mainHeadLimit = 30 * time.Second

// originHead is the head of branch on the origin of the checkout at rigDir, read
// with ls-remote and so as the origin has it now, not as the checkout last
// fetched it; empty when the origin has no such branch.
func originHead(ctx context.Context, rigDir, branch string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, mainHeadLimit)
	defer cancel()
	out, err := gitcmd.Command(ctx, "git", rigDir, "ls-remote", application.DefaultRemote, "refs/heads/"+branch).Output()
	if err != nil {
		return "", fmt.Errorf("asking the origin of %s for %s: %w", rigDir, branch, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

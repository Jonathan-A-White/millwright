package doctor

import (
	"context"
	"strings"

	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// RigOrigins reads the origin url of each rig checkout on this host with git
// (mw-gq6.746).
type RigOrigins struct {
	// Rigs maps a rig's name to its checkout's directory.
	Rigs map[string]string
	// Program is the git command; empty reads "git".
	Program string
}

// Origins lists the origin url of each rig that has one; a rig whose checkout
// is missing or has no origin is left out, since it has no scheme to differ.
func (r RigOrigins) Origins(ctx context.Context) (map[string]string, error) {
	program := r.Program
	if program == "" {
		program = "git"
	}
	origins := map[string]string{}
	for rig, dir := range r.Rigs {
		out, err := gitcmd.Command(ctx, program, dir, "remote", "get-url", "origin").Output()
		if err != nil {
			continue
		}
		if url := strings.TrimSpace(string(out)); url != "" {
			origins[rig] = url
		}
	}
	return origins, ctx.Err()
}

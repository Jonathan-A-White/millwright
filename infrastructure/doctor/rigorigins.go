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
// It reads the effective url, "git remote get-url origin", which a host's
// url.<base>.insteadOf has rewritten: that is the way the host reaches the
// origin, and the scheme the check judges (mw-gq6.1017; mw-gq6.1016 read the
// stored url, and flagged every rig the host's rewrite sends over https).
func (r RigOrigins) Origins(ctx context.Context) (map[string]string, error) {
	return r.read(ctx, func(ctx context.Context, program, dir string) ([]byte, error) {
		return gitcmd.Command(ctx, program, dir, "remote", "get-url", "origin").Output()
	})
}

// Rewritten lists, for each rig whose host rewrites its origin, the url the
// checkout stores; a rig whose stored and effective urls are the same is left
// out. The report line names it beside the effective url.
func (r RigOrigins) Rewritten(ctx context.Context) (map[string]string, error) {
	effective, err := r.Origins(ctx)
	if err != nil {
		return nil, err
	}
	stored, err := r.read(ctx, func(ctx context.Context, program, dir string) ([]byte, error) {
		return gitcmd.Command(ctx, program, dir, "config", "--get", "remote.origin.url").Output()
	})
	if err != nil {
		return nil, err
	}
	rewritten := map[string]string{}
	for rig, url := range stored {
		if now, ok := effective[rig]; ok && now != url {
			rewritten[rig] = url
		}
	}
	return rewritten, nil
}

func (r RigOrigins) read(ctx context.Context, ask func(ctx context.Context, program, dir string) ([]byte, error)) (map[string]string, error) {
	program := r.Program
	if program == "" {
		program = "git"
	}
	origins := map[string]string{}
	for rig, dir := range r.Rigs {
		out, err := ask(ctx, program, dir)
		if err != nil {
			continue
		}
		if url := strings.TrimSpace(string(out)); url != "" {
			origins[rig] = url
		}
	}
	return origins, ctx.Err()
}

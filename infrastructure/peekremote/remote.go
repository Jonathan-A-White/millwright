// Package peekremote reaches another host's mw by ssh to peek at a session
// there. It is the adapter behind application.PeekRemote.
package peekremote

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Remote runs `mw peek --here` on the host a story is worked on, and
// `mw retry --worktree-state` to ask what its worktree there holds.
type Remote struct {
	// Reach is how this host reaches each other host: an ssh prefix by host
	// name (`ssh desktop`), the last word of it the host itself — config
	// [hands_hosts].
	Reach map[string]string
	// Gaps are the hosts left out of Reach, each with why: a cloud box whose
	// address could not be read.
	Gaps map[string]string
}

var (
	_ application.PeekRemote     = Remote{}
	_ application.WorktreeRemote = Remote{}
)

// unattendedSSH keeps ssh from stopping to ask a question nobody is there to
// answer.
var unattendedSSH = []string{"-o", "BatchMode=yes"}

// Peek implements application.PeekRemote. The far end is run in a login shell,
// so that mw is on its PATH as it is for the person who logs in.
func (r Remote) Peek(ctx context.Context, host, storyID string) (string, error) {
	return r.run(ctx, host, "mw peek --here "+quote(storyID))
}

// WorktreeState implements application.WorktreeRemote: what the far end prints
// must say where the worktree is, or the host is taken not to have been asked.
func (r Remote) WorktreeState(ctx context.Context, host, storyID string) (application.WorktreeState, error) {
	text, err := r.run(ctx, host, "mw retry --worktree-state "+quote(storyID))
	if err != nil {
		return application.WorktreeState{}, err
	}
	return application.ParseWorktreeState(text)
}

// run runs command on host over its ssh line and returns what it printed.
func (r Remote) run(ctx context.Context, host, command string) (string, error) {
	prefix := strings.Fields(r.Reach[host])
	if len(prefix) < 2 {
		if why := r.Gaps[host]; why != "" {
			return "", fmt.Errorf("no way to reach %s: %s", host, why)
		}
		return "", fmt.Errorf("no way to reach %s: set `%s = \"ssh <alias>\"` under [hands_hosts] in the config file", host, host)
	}
	args := append(append(append([]string{}, prefix[1:len(prefix)-1]...), unattendedSSH...), prefix[len(prefix)-1],
		"bash -lc "+quote(command))
	cmd := exec.CommandContext(ctx, prefix[0], args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(prefix, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// quote makes s one word for a shell.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

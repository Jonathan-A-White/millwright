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

// Remote runs `mw named-here` to run tests by name there, `mw peek --here` on the host a story is worked on, and
// `mw retry --worktree-state` to ask what its worktree there holds, and
// `mw retry` to have it bundle that work and give the claim back.
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
	_ application.NamedElsewhere = Remote{}
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

// RetryThere implements application.WorktreeRemote: it runs mw retry on host, so
// that the host bundles its own worktree into its own vault clone and gives the
// claim back, and returns what that retry printed.
func (r Remote) RetryThere(ctx context.Context, host, storyID string, flags []string) (string, error) {
	command := "mw retry"
	for _, flag := range flags {
		command += " " + quote(flag)
	}
	return r.run(ctx, host, command+" "+quote(storyID))
}

// RunNamedOn implements application.NamedElsewhere: it runs mw named-here on
// host, which makes a landing of ref there and runs the tests by name, and reads
// how they came out from what it printed.
func (r Remote) RunNamedOn(ctx context.Context, host, rig, ref string, tests []string) (application.NamedRun, error) {
	command := "mw named-here " + quote(rig) + " " + quote(ref)
	for _, test := range tests {
		command += " " + quote(test)
	}
	text, err := r.run(ctx, host, command)
	if err != nil {
		return application.NamedRun{}, err
	}
	return application.ParseNamedHere(text)
}

// LandRoom implements application.BatchElsewhere's ask: it runs mw land <rig>
// --room on host, which says whether the rig is checked out there, whose gate
// holds its gate lock and its load, and reads that from what it printed.
func (r Remote) LandRoom(ctx context.Context, host, rig string) (application.BatchRoom, error) {
	text, err := r.run(ctx, host, "mw land "+quote(rig)+" --room")
	if err != nil {
		return application.BatchRoom{}, err
	}
	return application.ParseBatchRoom(host, text)
}

// LandArgv is the command that runs mw land <rig> on host over its ssh line, for
// a landing begun apart from the follower (apartproc.Landings.BeginOn), with
// --one, --only and --without as the scope names them.
func (r Remote) LandArgv(host, rig string, scope application.LandScope) ([]string, error) {
	command := "mw land " + quote(rig)
	if scope.One {
		command += " --one"
	}
	if len(scope.Only) > 0 {
		command += " --only " + quote(strings.Join(scope.Only, ","))
	}
	if len(scope.Without) > 0 {
		command += " --without " + quote(strings.Join(scope.Without, ","))
	}
	return r.argv(host, command)
}

// NightlyArgv is the command that runs the nightly kind of rig on host over its
// ssh line ("suite": mw suite <rig> --file; "timing": mw timing <rig>), for a run
// begun apart from the follower (apartproc.Processes.BeginOn).
func (r Remote) NightlyArgv(host, kind, rig string) ([]string, error) {
	switch kind {
	case application.NightlySuiteKind:
		return r.argv(host, "mw suite "+quote(rig)+" --file")
	case application.NightlyTimingKind:
		return r.argv(host, "mw timing "+quote(rig))
	}
	return nil, fmt.Errorf("no nightly run of the kind %q to run on %s", kind, host)
}

// argv is the ssh command that runs command on host, in a login shell.
func (r Remote) argv(host, command string) ([]string, error) {
	prefix := strings.Fields(r.Reach[host])
	if len(prefix) < 2 {
		if why := r.Gaps[host]; why != "" {
			return nil, fmt.Errorf("no way to reach %s: %s", host, why)
		}
		return nil, fmt.Errorf("no way to reach %s: set `%s = \"ssh <alias>\"` under [hands_hosts] in the config file", host, host)
	}
	args := append(append(append([]string{}, prefix[1:len(prefix)-1]...), unattendedSSH...), prefix[len(prefix)-1],
		"bash -lc "+quote(command))
	return append([]string{prefix[0]}, args...), nil
}

// run runs command on host over its ssh line and returns what it printed.
func (r Remote) run(ctx context.Context, host, command string) (string, error) {
	argv, err := r.argv(host, command)
	if err != nil {
		return "", err
	}
	prefix := strings.Fields(r.Reach[host])
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(prefix, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// quote makes s one word for a shell.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

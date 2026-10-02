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

// Remote runs `mw peek --here` on the host a story is worked on.
type Remote struct {
	// Reach is how this host reaches each other host: an ssh prefix by host
	// name (`ssh desktop`), the last word of it the host itself — config
	// [hands_hosts].
	Reach map[string]string
}

var _ application.PeekRemote = Remote{}

// unattendedSSH keeps ssh from stopping to ask a question nobody is there to
// answer.
var unattendedSSH = []string{"-o", "BatchMode=yes"}

// Peek implements application.PeekRemote. The far end is run in a login shell,
// so that mw is on its PATH as it is for the person who logs in.
func (r Remote) Peek(ctx context.Context, host, storyID string) (string, error) {
	prefix := strings.Fields(r.Reach[host])
	if len(prefix) < 2 {
		return "", fmt.Errorf("no way to reach %s: set `%s = \"ssh <alias>\"` under [hands_hosts] in the config file", host, host)
	}
	args := append(append(append([]string{}, prefix[1:len(prefix)-1]...), unattendedSSH...), prefix[len(prefix)-1],
		"bash -lc "+quote("mw peek --here "+quote(storyID)))
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

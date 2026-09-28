// Package hands runs the steps the Governor approves for his hands
// (postern's docs/protocol.md §17) where they belong — here or over ssh, as
// the host's own user or through mw-hands-root as root — and checks his
// approval with the one check mw-hands-root also makes.
package hands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/handsroot"
)

var (
	_ application.HandsRunner   = Runner{}
	_ application.HandsVerifier = Verifier{}
)

// Verifier is application.HandsVerifier by handsroot.VerifyApproval, so mw
// and mw-hands-root check an approval one way.
type Verifier struct{}

// VerifyApproval implements application.HandsVerifier.
func (Verifier) VerifyApproval(governorKeyHex, sha256Hex string, approvedAt int64, sigDERHex string) error {
	return handsroot.VerifyApproval(governorKeyHex, sha256Hex, approvedAt, sigDERHex)
}

// DefaultRootHelper is where mw-hands-root is installed on every host
// (contrib/install-hands-root): config hands_root_helper says otherwise.
const DefaultRootHelper = "/usr/local/sbin/mw-hands-root"

// OutputKept is how much of a step's output a Runner keeps, the last of it:
// far more than the 4000 characters an outcome carries, and bounded however
// much a step prints.
const OutputKept = 1 << 20

// Runner runs an approved step: a user step as sh -c, a root step by handing
// the request to mw-hands-root through sudo -n on its standard input; on
// another host, both over that host's ssh prefix.
type Runner struct {
	// RootHelper is mw-hands-root's path, the same on every host.
	RootHelper string
	// Shell runs a user step on this host, Sudo a root one.
	Shell, Sudo string
	// UserLimit is how long a user step may run — §17's ten minutes — and
	// RootLimit how long mw waits for the helper, which stops a root step
	// at ten minutes itself.
	UserLimit, RootLimit time.Duration
}

// NewRunner is a Runner handing root steps to rootHelper.
func NewRunner(rootHelper string) Runner {
	return Runner{
		RootHelper: rootHelper, Shell: "/bin/sh", Sudo: "sudo",
		UserLimit: handsroot.RunLimit, RootLimit: handsroot.RunLimit + time.Minute,
	}
}

// Run implements application.HandsRunner. The step runs in a process group
// of its own, so that its limit stops everything it started; one stopped at
// its limit is exit 124, as mw-hands-root and timeout(1) say it.
func (r Runner) Run(ctx context.Context, job application.HandsJob) (application.HandsOutcome, error) {
	argv, stdin, limit, err := r.command(job)
	if err != nil {
		return application.HandsOutcome{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	output := &tail{limit: OutputKept}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second

	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return application.HandsOutcome{Exit: handsroot.TimedOutExit,
			Output: output.String() + fmt.Sprintf("\nmw: gave up on step %s after %s\n", job.Request.ID, limit)}, nil
	}
	var exited *exec.ExitError
	switch {
	case err == nil:
		return application.HandsOutcome{Exit: 0, Output: output.String()}, nil
	case errors.As(err, &exited):
		code := exited.ExitCode()
		if status, ok := exited.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			code = 128 + int(status.Signal())
		}
		return application.HandsOutcome{Exit: code, Output: output.String()}, nil
	default:
		return application.HandsOutcome{}, err
	}
}

// command is what runs job: its argv, its standard input, and its limit.
func (r Runner) command(job application.HandsJob) ([]string, []byte, time.Duration, error) {
	req := job.Request
	remote := len(job.Remote) > 0
	switch req.As {
	case domain.HandsAsUser:
		if remote {
			if logsInAsRoot(job.Remote) {
				return nil, nil, 0, fmt.Errorf("the [hands_hosts] prefix for %s logs in as root, so a user step there would run as root without mw-hands-root's checks: give it a non-root login", req.Host)
			}
			return append(append([]string(nil), job.Remote...), "sh -c "+shellQuote(req.Run)), nil, r.UserLimit, nil
		}
		return []string{r.Shell, "-c", req.Run}, nil, r.UserLimit, nil
	case domain.HandsAsRoot:
		body, err := json.Marshal(req)
		if err != nil {
			return nil, nil, 0, err
		}
		if remote {
			return append(append([]string(nil), job.Remote...), "sudo -n "+shellQuote(r.RootHelper)), body, r.RootLimit, nil
		}
		return []string{r.Sudo, "-n", r.RootHelper}, body, r.RootLimit, nil
	default:
		return nil, nil, 0, fmt.Errorf("a step runs as %s or %s, not %q", domain.HandsAsUser, domain.HandsAsRoot, req.As)
	}
}

// logsInAsRoot reports whether an ssh prefix names root as its login, as
// root@host or -l root: over such a prefix a user step would be a root step
// that no mw-hands-root ever checked.
func logsInAsRoot(prefix []string) bool {
	for i, word := range prefix {
		if strings.HasPrefix(word, "root@") || (word == "-l" && i+1 < len(prefix) && prefix[i+1] == "root") || word == "-lroot" {
			return true
		}
	}
	return false
}

// shellQuote is text as one single-quoted word of sh: whatever it holds, the
// far shell reads it back unchanged.
func shellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

// tail keeps the last limit bytes written to it.
type tail struct {
	limit int
	buf   []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = append([]byte(nil), t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.buf) }

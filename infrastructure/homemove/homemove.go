// Package homemove is the machine mw home move reaches: ssh to the old home, git
// against GitHub, bd, systemctl, the postern backend's /healthz and the vault's
// bin/mayor-up. It is the adapter behind application.HomeMoveHost.
package homemove

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DoltRef is the git ref of GitHub that holds the beads database Dolt pushed.
const DoltRef = "refs/dolt/data"

// fetchWait is how long reading the time of DoltRef may take: one commit, no
// files, so seconds.
const fetchWait = 90 * time.Second

// answerGrace is how long past ssh's own ConnectTimeout the move waits for ssh
// itself to leave: a name lookup or a stalled login is not bound by it.
var answerGrace = 5 * time.Second

// Host is this host as a move sees it.
type Host struct {
	// Vault is the vault's directory: bd, git and bin/mayor-up run in it.
	Vault string
	// Home is the home directory, where the database set aside goes.
	Home string
	// Poll is how often the backend is asked again while the move waits for it;
	// two seconds when zero.
	Poll time.Duration
}

var _ application.HomeMoveHost = Host{}

// OldHomeAnswers implements application.HomeMoveHost: `ssh -o BatchMode=yes -o
// ConnectTimeout=<wait> <prefix> true`. A live sshd is an answer, however it
// answers: the command ran, or it refused this host's key. Only a connection that
// never came up (timed out, no route, no such name, nothing listening) is a host
// that does not answer.
func (Host) OldHomeAnswers(ctx context.Context, ssh []string, wait time.Duration) (bool, error) {
	if len(ssh) == 0 {
		return false, errors.New("no ssh command to reach the old home with")
	}
	args := append([]string{"-o", "BatchMode=yes", "-o", fmt.Sprintf("ConnectTimeout=%d", int(wait.Seconds()))}, ssh[1:]...)
	args = append(args, "true")
	ctx, cancel := context.WithTimeout(ctx, wait+answerGrace)
	defer cancel()

	cmd := exec.CommandContext(ctx, ssh[0], args...)
	// A ProxyCommand's own child can hold ssh's output open after ssh is killed.
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, nil
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return false, fmt.Errorf("running %s: %w", ssh[0], err)
	}
	switch code := exited.ExitCode(); {
	case code == 255:
		said := string(out)
		return strings.Contains(said, "Permission denied") || strings.Contains(said, "Host key verification failed"), nil
	case code < 0:
		return false, nil
	default:
		// The remote command ran and failed: somebody answered.
		return true, nil
	}
}

// BackupTime implements application.HomeMoveHost: the date of the one commit at
// the tip of GitHub's refs/dolt/data. It is fetched with no files (--depth=1
// --filter=blob:none) into a scratch repository, so the backup's hundreds of
// megabytes are never copied and the vault is not touched.
func (h Host) BackupTime(ctx context.Context) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchWait)
	defer cancel()

	url, err := run(ctx, h.Vault, "git", "remote", "get-url", "origin")
	if err != nil {
		return time.Time{}, err
	}
	url = h.absolute(strings.TrimSpace(url))
	scratch, err := os.MkdirTemp("", "mw-home-move-")
	if err != nil {
		return time.Time{}, err
	}
	defer os.RemoveAll(scratch)

	for _, args := range [][]string{
		{"init", "--quiet"},
		{"fetch", "--quiet", "--no-tags", "--depth=1", "--filter=blob:none", url, DoltRef},
	} {
		if _, err := run(ctx, scratch, "git", args...); err != nil {
			return time.Time{}, err
		}
	}
	date, err := run(ctx, scratch, "git", "log", "-1", "--format=%cI", "FETCH_HEAD")
	if err != nil {
		return time.Time{}, err
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(date))
	if err != nil {
		return time.Time{}, fmt.Errorf("the date of GitHub's %s, %q, is not a time: %w", DoltRef, strings.TrimSpace(date), err)
	}
	return at.UTC(), nil
}

// absolute is a remote's url as the scratch repository can use it: a relative
// path is relative to the vault, not to wherever the scratch repository is. Every
// other kind of url (`https://...`, `git@github.com:...`, an absolute path) is
// already the same from anywhere.
func (h Host) absolute(url string) string {
	local := !strings.Contains(url, "://") && !filepath.IsAbs(url)
	if colon := strings.Index(url, ":"); colon >= 0 && (strings.Index(url, "/") < 0 || colon < strings.Index(url, "/")) {
		local = false // scp-like: host:path
	}
	if local {
		return filepath.Join(h.Vault, url)
	}
	return url
}

// SetBeadsAside implements application.HomeMoveHost: a rename, so that nothing is
// copied and nothing is lost, within the same disk as the vault.
func (h Host) SetBeadsAside(_ context.Context, stamp string) (application.AsideMove, error) {
	from := filepath.Join(h.Vault, ".beads", "embeddeddolt")
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return application.AsideMove{}, nil
	} else if err != nil {
		return application.AsideMove{}, err
	}
	to := filepath.Join(h.Home, "beads-embeddeddolt-aside-"+stamp)
	if _, err := os.Lstat(to); err == nil {
		return application.AsideMove{}, fmt.Errorf("%s is already there: nothing was moved", to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return application.AsideMove{}, err
	}
	if err := os.Rename(from, to); err != nil {
		return application.AsideMove{}, fmt.Errorf("moving %s to %s: %w", from, to, err)
	}
	return application.AsideMove{From: from, To: to}, nil
}

// BootstrapBeads implements application.HomeMoveHost.
func (h Host) BootstrapBeads(ctx context.Context) error {
	_, err := run(ctx, h.Vault, "bd", "bootstrap", "--yes")
	return err
}

// RestoreBeadsConfig implements application.HomeMoveHost.
func (h Host) RestoreBeadsConfig(ctx context.Context) error {
	_, err := run(ctx, h.Vault, "git", "checkout", "--", ".beads/config.yaml")
	return err
}

// BeadsCount implements application.HomeMoveHost: `bd count` prints the number.
func (h Host) BeadsCount(ctx context.Context) (int, error) {
	out, err := run(ctx, h.Vault, "bd", "count")
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("bd count printed %q, not a number", strings.TrimSpace(out))
	}
	return count, nil
}

// UnitInstalled implements application.HomeMoveHost: whether `systemctl --user
// list-unit-files` lists the unit. Nothing listed is no unit; systemctl that says
// something on stderr instead (no user bus, say) is an error, not a no.
func (Host) UnitInstalled(ctx context.Context, unit string) (bool, error) {
	name := unit + ".service"
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "list-unit-files", name, "--no-legend", "--no-pager")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if strings.Contains(stdout.String(), name) {
		return true, nil
	}
	if said := strings.TrimSpace(stderr.String()); said != "" {
		return false, fmt.Errorf("systemctl --user list-unit-files %s: %s", name, said)
	}
	var exited *exec.ExitError
	if err != nil && !errors.As(err, &exited) {
		return false, fmt.Errorf("running systemctl: %w", err)
	}
	return false, nil
}

// StartUnit implements application.HomeMoveHost: a unit that is active is left
// alone.
func (Host) StartUnit(ctx context.Context, unit string) (bool, error) {
	active, _ := exec.CommandContext(ctx, "systemctl", "--user", "is-active", unit).Output()
	if strings.TrimSpace(string(active)) == "active" {
		return false, nil
	}
	if _, err := run(ctx, "", "systemctl", "--user", "start", unit); err != nil {
		return false, err
	}
	return true, nil
}

// BackendServing implements application.HomeMoveHost: GET <url>/healthz until it is
// 200 and does not say standby, or wait is up. A backend on a host that is not
// home answers 200 with "standby": true until its next look at `mw home --check`.
func (h Host) BackendServing(ctx context.Context, url string, wait time.Duration) error {
	poll := h.Poll
	if poll <= 0 {
		poll = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	target := strings.TrimRight(url, "/") + "/healthz"
	// last is the latest answer worth saying: a request cut short by the wait
	// running out says nothing more than the answer before it.
	var last error
	for {
		err := askHealthz(ctx, target)
		if err == nil {
			return nil
		}
		if ctx.Err() == nil || last == nil {
			last = err
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(poll):
		}
	}
}

func askHealthz(ctx context.Context, target string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered HTTP %d", target, response.StatusCode)
	}
	var health struct {
		Standby bool   `json:"standby"`
		Home    string `json:"home"`
	}
	if json.Unmarshal(body, &health) == nil && health.Standby {
		return fmt.Errorf("%s answers, but in standby (its home is %q)", target, health.Home)
	}
	return nil
}

// MayorUp implements application.HomeMoveHost: the vault's bin/mayor-up. Exit 0 is
// a Mayor started, its window the last line; exit 3 is a live Mayor already
// there, nothing done. Every other status, 4 (cannot) and 5 (not home) among
// them, is a failure that says what mayor-up said.
func (h Host) MayorUp(ctx context.Context) (bool, string, error) {
	cmd := exec.CommandContext(ctx, filepath.Join(h.Vault, "bin", "mayor-up"))
	cmd.Dir = h.Vault
	// A tmux server that mayor-up starts can keep its output open after it leaves.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if err == nil {
		return true, last, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) && exited.ExitCode() == 3 {
		return false, last, nil
	}
	return false, "", fmt.Errorf("%w: %s", err, strings.TrimSpace(strings.TrimSpace(stdout.String())+"\n"+strings.TrimSpace(stderr.String())))
}

// run runs one program in dir and returns what it printed. A failure says what
// it printed.
func run(ctx context.Context, dir, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s %s: stopped: %w", program, strings.Join(args, " "), ctx.Err())
		}
		said := strings.TrimSpace(stderr.String() + "\n" + stdout.String())
		if said == "" {
			return "", fmt.Errorf("%s %s: %w", program, strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("%s %s: %w: %s", program, strings.Join(args, " "), err, said)
	}
	return stdout.String(), nil
}

package homemove

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.OldHome = Host{}

// mayorPoll is how often a planned move asks the old home whether its Mayor has
// gone, when Host.Poll says nothing.
const mayorPoll = 15 * time.Second

// remoteEnv is what every command run on the old home starts with: an ssh command
// has no login shell, so it puts on PATH where mw and bd live, gives bd the server
// mode the host runs in (the file bin/respawn-mayor reads too) and says where the
// user's systemd bus is.
const remoteEnv = `PATH="$PATH:/usr/local/go/bin:$HOME/.local/go/bin:$HOME/.local/bin"; ` +
	`[ -r "$HOME/.config/mw/beads.env" ] && { set -a; . "$HOME/.config/mw/beads.env"; set +a; }; ` +
	`export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"; `

// mayorGoneScript answers, on the old home, whether its Mayor has handed off: exit
// 0 when .mayor-acting in its vault is empty or no Mayor process runs
// (bin/respawn-mayor's own markers, "-n Mayor (after ..."), 1 when one is still
// there, 2 when it could not tell. It says what it saw on stdout. The `[r]` keeps
// the pgrep pattern from matching the shell that runs this text.
const mayorGoneScript = `V="${MW_VAULT:-$(sed -n 's/^vault *= *"\(.*\)".*/\1/p' "$HOME/.config/mw/config.toml" 2>/dev/null | head -n 1)}"
[ -n "$V" ] || { echo "no vault: neither MW_VAULT nor a vault key in ~/.config/mw/config.toml" >&2; exit 2; }
ACTING="$(cat "$V/.mayor-acting" 2>/dev/null)"
if [ -z "$(printf %s "$ACTING" | tr -d ' \t\r\n')" ]; then echo ".mayor-acting is empty"; exit 0; fi
command -v pgrep >/dev/null 2>&1 || { echo "no pgrep to look for the Mayor process with" >&2; exit 2; }
pgrep -f -- '-n Mayor \(afte[r] ' >/dev/null 2>&1
case $? in
  0) echo ".mayor-acting says: $ACTING; its Mayor process is running"; exit 1 ;;
  1) echo "no Mayor process runs (.mayor-acting still says: $ACTING)"; exit 0 ;;
  *) echo "pgrep could not look for the Mayor process" >&2; exit 2 ;;
esac`

// quote is one shell word, single-quoted, so that a subject or a body means
// itself to the shell on the old home.
func quote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// remote runs script on the host the ssh prefix reaches, in a POSIX sh with the
// old home's PATH, and returns what it printed and its exit status. A status of
// -1 is ssh that could not be run at all, in which case err says why. ssh's own
// failure is 255 and is an err: nothing there ran.
func (Host) remote(ctx context.Context, ssh []string, script string) (stdout string, status int, err error) {
	if len(ssh) == 0 {
		return "", -1, errors.New("no ssh command to reach the old home with")
	}
	args := append([]string{"-o", "BatchMode=yes", "-o", "ServerAliveInterval=15"}, ssh[1:]...)
	args = append(args, "sh -c "+quote(remoteEnv+script))
	cmd := exec.CommandContext(ctx, ssh[0], args...)
	cmd.WaitDelay = time.Second
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	runErr := cmd.Run()
	if runErr == nil {
		return out.String(), 0, nil
	}
	if ctx.Err() != nil {
		return out.String(), -1, fmt.Errorf("%s: stopped: %w", strings.Join(ssh, " "), ctx.Err())
	}
	var exited *exec.ExitError
	if !errors.As(runErr, &exited) {
		return "", -1, fmt.Errorf("running %s: %w", ssh[0], runErr)
	}
	said := strings.TrimSpace(strings.TrimSpace(out.String()) + "\n" + strings.TrimSpace(errs.String()))
	if exited.ExitCode() == 255 {
		return out.String(), 255, fmt.Errorf("%s did not run the command: %s", strings.Join(ssh, " "), said)
	}
	return out.String(), exited.ExitCode(), fmt.Errorf("exit status %d: %s", exited.ExitCode(), said)
}

// ok is remote for a command that has to succeed.
func (h Host) ok(ctx context.Context, ssh []string, script string) (string, error) {
	out, _, err := h.remote(ctx, ssh, script)
	return out, err
}

// MailMayor implements application.OldHome: mw mail send on the old home, so the
// message is filed in the beads database its Mayor reads, signed from.
func (h Host) MailMayor(ctx context.Context, ssh []string, from, subject, body string) error {
	_, err := h.ok(ctx, ssh, "MW_SEAT="+quote(from)+" mw mail send mayor -s "+quote(subject)+" -m "+quote(body))
	return err
}

// MayorGone implements application.OldHome: mayorGoneScript, asked again every
// poll until it says the Mayor has gone or wait is up.
func (h Host) MayorGone(ctx context.Context, ssh []string, wait time.Duration) (bool, string, error) {
	poll := h.Poll
	if poll <= 0 {
		poll = mayorPoll
	}
	deadline := time.Now().Add(wait)
	for {
		out, status, err := h.remote(ctx, ssh, mayorGoneScript)
		said := strings.TrimSpace(out)
		switch {
		case err == nil:
			return true, said, nil
		case status == 1:
			if !time.Now().Add(poll).Before(deadline) {
				return false, said, nil
			}
		default:
			return false, "", err
		}
		select {
		case <-ctx.Done():
			return false, said, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// Sync implements application.OldHome.
func (h Host) Sync(ctx context.Context, ssh []string) error {
	_, err := h.ok(ctx, ssh, "mw sync")
	return err
}

// OldUnitInstalled implements application.OldHome: whether `systemctl --user
// list-unit-files` on the old home lists the unit.
func (h Host) OldUnitInstalled(ctx context.Context, ssh []string, unit string) (bool, error) {
	name := unit + ".service"
	out, err := h.ok(ctx, ssh, "systemctl --user list-unit-files "+quote(name)+" --no-legend --no-pager")
	if err != nil {
		return false, err
	}
	return strings.Contains(out, name), nil
}

// OldStopUnit implements application.OldHome: a unit that is not active is left
// alone.
func (h Host) OldStopUnit(ctx context.Context, ssh []string, unit string) (bool, error) {
	u := quote(unit)
	out, err := h.ok(ctx, ssh, `[ "$(systemctl --user is-active `+u+` 2>/dev/null)" = active ] || { echo not-running; exit 0; }; systemctl --user stop `+u+` && echo stopped`)
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "stopped"), nil
}

// Mirror implements application.OldHome: mw postern mirror on the old home, which
// is still home, copies its data to this host.
func (h Host) Mirror(ctx context.Context, ssh []string) error {
	_, err := h.ok(ctx, ssh, "mw postern mirror")
	return err
}

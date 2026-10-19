// Package member is the adapter behind application.MemberHost: the files, the
// user manager and the Dolt server of one member's own Linux user, on the
// machine mw member init runs on.
package member

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/homemove"
)

// Host implements application.MemberHost. Its zero value runs the systemctl,
// loginctl and bd on the PATH and waits half a minute for a Dolt server.
type Host struct {
	// Systemctl, Loginctl and Bd are the programs run, when not the ones on
	// the PATH: a test gives scripts of its own.
	Systemctl string
	Loginctl  string
	Bd        string

	// Wait is how long a Dolt server is given to answer; Poll how often it is asked.
	Wait time.Duration
	Poll time.Duration
}

var _ application.MemberHost = (*Host)(nil)

// Exists implements application.MemberHost.
func (*Host) Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// ReadFile implements application.MemberHost.
func (*Host) ReadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

// WriteFile implements application.MemberHost: the file is made with O_EXCL, so
// one that is there is never overwritten.
func (*Host) WriteFile(path, text string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return file.Close()
}

// ReplaceFile implements application.MemberHost.
func (*Host) ReplaceFile(path, text string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), info.Mode().Perm())
}

// MakeDir implements application.MemberHost.
func (*Host) MakeDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("making %s: %w", path, err)
	}
	return nil
}

// DoltProgram implements application.MemberHost.
func (*Host) DoltProgram(given string) (string, error) {
	name := given
	if name == "" {
		name = "dolt"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("dolt was not found (%q): install it, or --dolt names the program the unit runs", name)
	}
	return filepath.Abs(path)
}

// PortFree implements application.MemberHost: a port is taken when a dial of
// it connects.
func (*Host) PortFree(ctx context.Context, port int) error {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil
	}
	conn.Close()
	return fmt.Errorf("something is listening on 127.0.0.1:%d", port)
}

// NewPassword implements application.MemberHost: 24 random bytes, in hex.
func (*Host) NewPassword() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func (h *Host) systemctl(ctx context.Context, args ...string) error {
	bin := h.Systemctl
	if bin == "" {
		bin = "systemctl"
	}
	out, err := exec.CommandContext(ctx, bin, append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		said := strings.Join(strings.Fields(string(out)), " ")
		if said != "" {
			return fmt.Errorf("systemctl --user %s: %w: %s", strings.Join(args, " "), err, said)
		}
		return fmt.Errorf("systemctl --user %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// EnableUnit implements application.MemberHost.
func (h *Host) EnableUnit(ctx context.Context, unit string) error {
	if err := h.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	return h.systemctl(ctx, "enable", "--now", unit)
}

// DisableUnit implements application.MemberHost.
func (h *Host) DisableUnit(ctx context.Context, unit string) error {
	return h.systemctl(ctx, "disable", "--now", unit)
}

// ServerAnswers implements application.MemberHost: a dial of 127.0.0.1:port
// every Poll until one connects or Wait is up.
func (h *Host) ServerAnswers(ctx context.Context, port int) error {
	wait, poll := h.Wait, h.Poll
	if wait == 0 {
		wait = 30 * time.Second
	}
	if poll == 0 {
		poll = time.Second
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var last error
	for {
		dialer := net.Dialer{Timeout: 3 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err == nil {
			conn.Close()
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("the Dolt server on %s did not answer within %s: %w: see journalctl --user -u %s", address, wait, last, application.MemberUnit)
		case <-time.After(poll):
		}
	}
}

// CreateBeadsUser implements application.MemberHost.
func (*Host) CreateBeadsUser(ctx context.Context, port int, user, password string) error {
	_, _, err := homemove.CreateBeadsUserOn(ctx, strconv.Itoa(port), user, password)
	return err
}

// InitBeads implements application.MemberHost: `bd init --server --external`
// in the vault, with the connection in bd's environment (this process's own
// BEADS_* variables are dropped, for they may be another server's).
func (h *Host) InitBeads(ctx context.Context, dir, prefix string, port int, user, password string) error {
	bin := h.Bd
	if bin == "" {
		bin = "bd"
	}
	args := []string{"init", "-p", prefix, "--server", "--external",
		"--server-host", "127.0.0.1", "--server-port", strconv.Itoa(port), "--server-user", user,
		"--non-interactive", "--role", "maintainer", "--skip-agents", "--skip-hooks", "-q"}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "BEADS_") && !strings.HasPrefix(kv, "BD_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env,
		"BEADS_DOLT_SERVER_MODE=1", "BEADS_DOLT_SERVER_HOST=127.0.0.1",
		"BEADS_DOLT_SERVER_PORT="+strconv.Itoa(port), "BEADS_DOLT_SERVER_USER="+user,
		"BEADS_DOLT_PASSWORD="+password, "BEADS_DOLT_AUTO_START=0")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(strings.ReplaceAll(out.String(), password, "<password>"))
		if said != "" {
			return fmt.Errorf("%s init in %s: %w: %s", bin, dir, err, said)
		}
		return fmt.Errorf("%s init in %s: %w", bin, dir, err)
	}
	return nil
}

// LingerEnabled implements application.MemberHost: loginctl show-user for the
// current user.
func (h *Host) LingerEnabled(ctx context.Context) (bool, error) {
	bin := h.Loginctl
	if bin == "" {
		bin = "loginctl"
	}
	me, err := user.Current()
	if err != nil {
		return false, err
	}
	out, err := exec.CommandContext(ctx, bin, "show-user", me.Username, "-p", "Linger").Output()
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(out)) {
	case "Linger=yes":
		return true, nil
	case "Linger=no":
		return false, nil
	}
	return false, fmt.Errorf("loginctl said %q", strings.TrimSpace(string(out)))
}

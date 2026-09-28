package handsroot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The installed helper's fixed layout, which contrib/install-hands-root
// writes: the Governor's public key, this host's own name in the factory,
// and the record of every approval already run. mw-hands-root reads these
// and nothing else — no path, key or setting from its environment or its
// arguments.
const (
	InstalledGovernorKeyPath = "/etc/mw-hands/governor.pub"
	InstalledHostPath        = "/etc/mw-hands/host"
	InstalledUsedPath        = "/var/lib/mw-hands/used"
	InstalledShell           = "/bin/sh"
)

// RequestLimit is the most bytes of request the helper reads.
const RequestLimit = 64 << 10

// RunLimit is how long a root step may run before it is stopped: §17's ten
// minutes.
const RunLimit = 10 * time.Minute

// The helper's own exit statuses, beside the step's: a refusal, and a step
// stopped at RunLimit.
const (
	RefusedExit  = 126
	TimedOutExit = 124
)

// Helper runs one root hands step, once, and only once the Governor has
// approved exactly that step with his key within the last fifteen minutes.
// Every field is fixed by Installed in the program root runs; a test lays
// the same files out in a temporary directory and stands its own uid in for
// root's.
type Helper struct {
	// GovernorKeyPath holds the Governor's compressed public key, hex.
	GovernorKeyPath string
	// HostPath holds this host's name in the factory: a step for another
	// host is refused here, so an approval cannot be carried to a host it
	// was not given for.
	HostPath string
	// UsedPath records every approval already run, one a line.
	UsedPath string
	// RootUID must own the key and host files and their directory, and the
	// used record, and be the uid the helper runs as.
	RootUID int
	// Euid is the uid the helper runs as.
	Euid func() int
	// Now is the clock an approval's age is read by.
	Now func() time.Time
	// Shell runs the step, as Shell -c <run>.
	Shell string
	// Timeout is how long the step may run.
	Timeout time.Duration
	// Env is the whole environment the step runs in.
	Env []string
}

// Installed is the helper as mw-hands-root runs it.
func Installed() Helper {
	return Helper{
		GovernorKeyPath: InstalledGovernorKeyPath,
		HostPath:        InstalledHostPath,
		UsedPath:        InstalledUsedPath,
		RootUID:         0,
		Euid:            os.Geteuid,
		Now:             time.Now,
		Shell:           InstalledShell,
		Timeout:         RunLimit,
		Env:             []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8"},
	}
}

// refusal is a request the helper will not run, said on one line.
type refusal struct{ why string }

func (r refusal) Error() string { return r.why }

func refuse(format string, args ...any) error { return refusal{fmt.Sprintf(format, args...)} }

// Run reads one request (domain.HandsRequest, JSON) from stdin, checks it
// all, records its approval as used, and runs its step as Shell -c, its
// output and errors streamed together to stdout; it reports the step's own
// exit status. A refusal is one line on stderr and RefusedExit; a step
// stopped at Timeout is TimedOutExit.
func (h Helper) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) int {
	req, err := h.check(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "mw-hands-root: refused: %s\n", oneLine(err.Error()))
		return RefusedExit
	}
	return h.execute(ctx, req, stdout, stderr)
}

// check is everything Run makes sure of before it runs a thing, the
// approval's use recorded last.
func (h Helper) check(stdin io.Reader) (domain.HandsRequest, error) {
	if euid := h.Euid(); euid != h.RootUID {
		return domain.HandsRequest{}, refuse("mw-hands-root runs as root only (uid %d), not uid %d", h.RootUID, euid)
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, RequestLimit+1))
	if err != nil {
		return domain.HandsRequest{}, refuse("reading the request: %v", err)
	}
	if len(raw) > RequestLimit {
		return domain.HandsRequest{}, refuse("the request is too large: over %d bytes", RequestLimit)
	}
	var req domain.HandsRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return domain.HandsRequest{}, refuse("the request is not a hands request: %v", err)
	}
	step := req.Step()
	if err := domain.ValidateHandsStep(req.Bead, step); err != nil {
		return domain.HandsRequest{}, refuse("%v", err)
	}
	if step.As != domain.HandsAsRoot {
		return domain.HandsRequest{}, refuse("the step %s runs as %s, not root: mw-hands-root runs root steps only", step.ID, step.As)
	}
	governorKey, err := h.readTrusted(h.GovernorKeyPath)
	if err != nil {
		return domain.HandsRequest{}, err
	}
	host, err := h.readTrusted(h.HostPath)
	if err != nil {
		return domain.HandsRequest{}, err
	}
	if step.Host != host {
		return domain.HandsRequest{}, refuse("the step %s is for %s, and this host is %s", step.ID, step.Host, host)
	}
	if sum := domain.HandsSHA256(req.Bead, step); sum != req.SHA256 {
		return domain.HandsRequest{}, refuse("the step changed since the Governor approved it: it hashes to %s, not %s", sum, req.SHA256)
	}
	if err := VerifyApproval(governorKey, req.SHA256, req.ApprovedAt, req.Sig); err != nil {
		return domain.HandsRequest{}, refuse("%v", err)
	}
	if err := domain.CheckHandsApprovalAge(req.ApprovedAt, h.Now()); err != nil {
		return domain.HandsRequest{}, refuse("%v", err)
	}
	if err := h.claim(req.ApprovalID()); err != nil {
		return domain.HandsRequest{}, err
	}
	return req, nil
}

// readTrusted reads a short file only root could have written: a regular
// file, not a link, owned by RootUID and not group- or world-writable, in a
// directory that is the same.
func (h Helper) readTrusted(path string) (string, error) {
	if err := h.trusted(filepath.Dir(path), true); err != nil {
		return "", err
	}
	if err := h.trusted(path, false); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", refuse("reading %s: %v", path, err)
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > 4096 {
		return "", refuse("%s says nothing usable", path)
	}
	return value, nil
}

// trusted reports why path is not one only root could have changed.
func (h Helper) trusted(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return refuse("%s: %v", path, err)
	}
	switch {
	case dir && !info.IsDir():
		return refuse("%s is not a directory", path)
	case !dir && !info.Mode().IsRegular():
		return refuse("%s is not a plain file", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != h.RootUID {
		return refuse("%s is not owned by root", path)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return refuse("%s may be written by others than root (mode %o)", path, info.Mode().Perm())
	}
	return nil
}

// claim records approval in UsedPath as run, refusing one already there. It
// holds the record's lock from reading to writing, so two helpers given one
// approval at once run it once, and records it before the step runs, so a
// helper stopped half way cannot leave it unused.
func (h Helper) claim(approval string) error {
	dir := filepath.Dir(h.UsedPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return refuse("making %s: %v", dir, err)
	}
	if err := h.trusted(dir, true); err != nil {
		return err
	}
	file, err := os.OpenFile(h.UsedPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return refuse("opening %s: %v", h.UsedPath, err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return refuse("locking %s: %v", h.UsedPath, err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if err := h.trusted(h.UsedPath, false); err != nil {
		return err
	}
	if info, err := file.Stat(); err == nil && info.Mode().Perm()&0o077 != 0 {
		return refuse("%s may be read or written by others than root (mode %o)", h.UsedPath, info.Mode().Perm())
	}
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		if strings.TrimSpace(lines.Text()) == approval {
			return refuse("this approval has already run: approve the step again to run it again")
		}
	}
	if err := lines.Err(); err != nil {
		return refuse("reading %s: %v", h.UsedPath, err)
	}
	if _, err := file.WriteString(approval + "\n"); err != nil {
		return refuse("recording the approval in %s: %v", h.UsedPath, err)
	}
	if err := file.Sync(); err != nil {
		return refuse("recording the approval in %s: %v", h.UsedPath, err)
	}
	return nil
}

// execute runs req's step as Shell -c, in a process group of its own so that
// the limit stops everything it started, and reports its exit status.
func (h Helper) execute(ctx context.Context, req domain.HandsRequest, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.Shell, "-c", req.Run)
	cmd.Env = h.Env
	cmd.Dir = "/"
	cmd.Stdout, cmd.Stderr = stdout, stdout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		fmt.Fprintf(stderr, "mw-hands-root: gave up on step %s after %s\n", req.ID, h.Timeout)
		return TimedOutExit
	}
	var exited *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exited) && exited.ExitCode() >= 0:
		return exited.ExitCode()
	default:
		fmt.Fprintf(stderr, "mw-hands-root: step %s did not run to an end: %s\n", req.ID, oneLine(err.Error()))
		return RefusedExit
	}
}

// oneLine is text on one line.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

package application

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultPosternHandBackupDir is where mw postern serve and mw postern nginx
// back up the file they are about to change, when --backup-dir says
// nothing: tidy under the caller's own home (the VPS's /root/tidy when run
// as root, where the hand steps this pair replaces already backed up to).
func DefaultPosternHandBackupDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/root/tidy"
	}
	return filepath.Join(home, "tidy")
}

// PosternHandFile is what mw postern serve and mw postern nginx ask of the
// disk to edit one text file on the VPS in place: read what is there, back
// it up before touching it, write the new text, and make a directory. There
// is nothing here a temp directory cannot stand in for, so the real adapter
// (infrastructure/postern) is what a test points at a temp file — never a
// fake.
type PosternHandFile interface {
	// Read reports path's current text and whether it exists. A path that is
	// not there yet reports "", false, nil.
	Read(ctx context.Context, path string) (text string, exists bool, err error)
	// Backup copies path's current content into dir (made first if it is not
	// there), named so two runs never collide, and reports where it went.
	// Called only when path exists.
	Backup(ctx context.Context, path, dir string) (backupPath string, err error)
	// Write writes text to path, making its directory first if it is not
	// there.
	Write(ctx context.Context, path, text string) error
	// MkdirAll makes dir and any parents it is missing.
	MkdirAll(ctx context.Context, dir string) error
}

// PosternNginxRunner is what mw postern nginx asks of the host to validate
// and reload nginx once its site file is written: `nginx -t` and `systemctl
// reload nginx`, run through this port so a test never touches the real
// nginx.
type PosternNginxRunner interface {
	// Test runs nginx's own config test against the file just written,
	// reporting its combined output and whether it passed.
	Test(ctx context.Context) (output string, ok bool, err error)
	// Reload reloads nginx. Called only once Test has passed.
	Reload(ctx context.Context) (output string, err error)
}

// DefaultPosternAddr is where the postern backend listens when nothing says
// otherwise: this host alone. The host the backend runs on for the factory,
// the desktop, says its WireGuard address instead (10.88.0.3:8787), so that
// the VPS's nginx reaches it over the tunnel.
const DefaultPosternAddr = "127.0.0.1:8787"

// PosternServeRequest is what mw postern serve is asked to set in this
// host's config file and, given EnvFile, in the postern backend's own
// environment file.
type PosternServeRequest struct {
	Backend      string
	SnapshotPath string
	GovernorKey  string
	DryRun       bool

	// EnvFile is the postern backend's environment file — a systemd
	// EnvironmentFile, NAME="value" lines — to write the backend's own
	// POSTERN_ lines into. Empty leaves the backend's environment alone, and
	// none of the fields below is read.
	EnvFile string
	// Addr is where the backend listens, POSTERN_ADDR. Empty reads
	// DefaultPosternAddr.
	Addr string
	// ViewPath is the live view mw postern view writes and the backend
	// serves, POSTERN_VIEW_FILE, a full path. It is also written into this
	// host's config file as postern_view_path, so that the two never
	// disagree.
	ViewPath string
	// Mw is the full path of the mw the backend runs: `<Mw> postern bead`
	// for a bead's detail (POSTERN_BEAD_CMD) and `<Mw> postern inbox
	// --apply` on each message (POSTERN_ON_MESSAGE).
	Mw string
	// MayorKey is the Mayor's postern public key, as hex, the backend
	// answers /api/me with (POSTERN_MAYOR_KEY). GovernorKey is the
	// licence's issuer (POSTERN_ISSUER_KEY): the Governor issues it.
	MayorKey string
}

// posternServeKeys is the order mw postern serve writes or replaces the
// three postern lines in, root-table keys of ~/.config/mw/config.toml.
var posternServeKeys = []string{"postern_backend", "postern_snapshot_path", "postern_governor_key"}

// posternServeViewKey is the fourth, written only beside an environment
// file, which is what names the view.
const posternServeViewKey = "postern_view_path"

// posternEnvKeys is the order mw postern serve writes or replaces the
// backend's own lines in its environment file.
var posternEnvKeys = []string{
	"POSTERN_ADDR", "POSTERN_VIEW_FILE", "POSTERN_BEAD_CMD", "POSTERN_ON_MESSAGE", "POSTERN_MAYOR_KEY", "POSTERN_ISSUER_KEY",
}

// configKeys is the config file's keys this request writes, in order.
func (r PosternServeRequest) configKeys() []string {
	if r.EnvFile == "" {
		return posternServeKeys
	}
	return append(append([]string{}, posternServeKeys...), posternServeViewKey)
}

func (r PosternServeRequest) values() map[string]string {
	values := map[string]string{
		"postern_backend":       r.Backend,
		"postern_snapshot_path": r.SnapshotPath,
		"postern_governor_key":  r.GovernorKey,
	}
	if r.EnvFile != "" {
		values[posternServeViewKey] = r.ViewPath
	}
	return values
}

// envValues is the backend's own lines, by name.
func (r PosternServeRequest) envValues() map[string]string {
	return map[string]string{
		"POSTERN_ADDR":       r.addr(),
		"POSTERN_VIEW_FILE":  r.ViewPath,
		"POSTERN_BEAD_CMD":   r.Mw + " postern bead",
		"POSTERN_ON_MESSAGE": r.Mw + " postern inbox --apply",
		"POSTERN_MAYOR_KEY":  r.MayorKey,
		"POSTERN_ISSUER_KEY": r.GovernorKey,
	}
}

func (r PosternServeRequest) addr() string {
	if strings.TrimSpace(r.Addr) == "" {
		return DefaultPosternAddr
	}
	return r.Addr
}

// validateEnv refuses an environment file request that could not be
// written, or that would leave the backend short of what it needs.
func (r PosternServeRequest) validateEnv() error {
	switch {
	case !filepath.IsAbs(r.EnvFile):
		return fmt.Errorf("mw postern serve: --env-file %q must be a full path", r.EnvFile)
	case strings.TrimSpace(r.ViewPath) == "":
		return fmt.Errorf("mw postern serve: --env-file needs the view's path (--view-path, or postern_view_path)")
	case !filepath.IsAbs(r.ViewPath):
		return fmt.Errorf("mw postern serve: --view-path %q must be a full path", r.ViewPath)
	case strings.TrimSpace(r.Mw) == "":
		return fmt.Errorf("mw postern serve: --env-file needs the mw the backend runs (--mw)")
	case !filepath.IsAbs(r.Mw):
		return fmt.Errorf("mw postern serve: --mw %q must be a full path", r.Mw)
	case strings.TrimSpace(r.MayorKey) == "":
		return fmt.Errorf("mw postern serve: --env-file needs the Mayor's postern public key (--mayor-key, or run mw postern key init on this host first)")
	}
	if _, _, err := net.SplitHostPort(r.addr()); err != nil {
		return fmt.Errorf("mw postern serve: --addr %q is not a host:port: %w", r.addr(), err)
	}
	for name, value := range r.envValues() {
		if strings.ContainsAny(value, "\"\\\n$`") {
			return fmt.Errorf("mw postern serve: %s would be %q, which cannot be written into an environment file", name, value)
		}
	}
	return nil
}

// validate refuses a request that could not be written into a config file,
// before anything is read or touched.
func (r PosternServeRequest) validate() error {
	switch {
	case strings.TrimSpace(r.Backend) == "":
		return fmt.Errorf("mw postern serve: --backend is required")
	case strings.TrimSpace(r.SnapshotPath) == "":
		return fmt.Errorf("mw postern serve: --snapshot-path is required")
	case !filepath.IsAbs(r.SnapshotPath):
		return fmt.Errorf("mw postern serve: --snapshot-path %q must be a full path", r.SnapshotPath)
	case strings.TrimSpace(r.GovernorKey) == "":
		return fmt.Errorf("mw postern serve: --governor-key is required")
	}
	for name, value := range r.values() {
		if strings.ContainsAny(value, "\"\n") {
			return fmt.Errorf("mw postern serve: %s is %q, which cannot be written into a config file", name, value)
		}
	}
	if r.EnvFile != "" {
		return r.validateEnv()
	}
	return nil
}

// PosternServe writes or replaces the three postern lines this host's
// config file needs to serve the postern to the Governor's app —
// postern_backend, postern_snapshot_path and postern_governor_key —
// idempotent, so a re-run with the same values changes nothing, and makes
// the snapshot's own directory. It backs the config file up first, unless
// there is none yet to back up, and --dry-run prints what it would do
// without touching anything.
//
// Given the backend's environment file it does the same there, for the
// backend's own POSTERN_ lines — every other line of the file left exactly
// as it was — writes postern_view_path beside the three, and makes the
// view's directory. The backend reads its environment only when it starts,
// so the report says to restart it.
type PosternServe struct {
	Files      PosternHandFile
	ConfigPath string
	// BackupDir names where the config file is backed up before it is
	// changed. Empty reads DefaultPosternHandBackupDir().
	BackupDir string

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// PosternServeReport is what serve did, or — under --dry-run — would do.
type PosternServeReport struct {
	ConfigPath  string
	BackupPath  string // empty when nothing needed a backup
	SnapshotDir string
	Changed     bool
	DryRun      bool
	NewText     string // what the config file holds, or would hold

	// The backend's environment file, when one was asked for: where it is,
	// its backup, whether it changed and what it holds, or would hold, and
	// the view's directory. EnvFile is empty when none was.
	EnvFile       string
	EnvBackupPath string
	EnvChanged    bool
	EnvNewText    string
	ViewDir       string
}

func (r PosternServeReport) String() string {
	var out strings.Builder
	if r.DryRun {
		if r.Changed {
			fmt.Fprintf(&out, "--dry-run: would write %s:\n\n%s\n", r.ConfigPath, r.NewText)
		} else {
			fmt.Fprintf(&out, "--dry-run: %s already says this; nothing to write.\n", r.ConfigPath)
		}
		fmt.Fprintf(&out, "--dry-run: would make the snapshot directory %s\n", r.SnapshotDir)
		if r.EnvFile != "" {
			if r.EnvChanged {
				fmt.Fprintf(&out, "--dry-run: would write %s:\n\n%s\n", r.EnvFile, r.EnvNewText)
				fmt.Fprintf(&out, "--dry-run: and would say to restart the postern backend, so that it reads %s\n", r.EnvFile)
			} else {
				fmt.Fprintf(&out, "--dry-run: %s already says this; nothing to write.\n", r.EnvFile)
			}
			fmt.Fprintf(&out, "--dry-run: would make the view directory %s\n", r.ViewDir)
		}
		return out.String()
	}
	if !r.Changed {
		fmt.Fprintf(&out, "%s already says this: nothing changed.\n", r.ConfigPath)
	} else {
		if r.BackupPath != "" {
			fmt.Fprintf(&out, "backed up %s to %s\n", r.ConfigPath, r.BackupPath)
		}
		fmt.Fprintf(&out, "wrote %s\n", r.ConfigPath)
	}
	fmt.Fprintf(&out, "the snapshot directory %s is there\n", r.SnapshotDir)
	if r.BackupPath != "" {
		fmt.Fprintf(&out, "the way back: cp %s %s\n", r.BackupPath, r.ConfigPath)
	}
	if r.EnvFile == "" {
		return out.String()
	}
	if !r.EnvChanged {
		fmt.Fprintf(&out, "%s already says this: nothing changed.\n", r.EnvFile)
	} else {
		if r.EnvBackupPath != "" {
			fmt.Fprintf(&out, "backed up %s to %s\n", r.EnvFile, r.EnvBackupPath)
		}
		fmt.Fprintf(&out, "wrote %s\n", r.EnvFile)
	}
	fmt.Fprintf(&out, "the view directory %s is there\n", r.ViewDir)
	if r.EnvBackupPath != "" {
		fmt.Fprintf(&out, "the way back: cp %s %s\n", r.EnvBackupPath, r.EnvFile)
	}
	if r.EnvChanged {
		fmt.Fprintf(&out, "restart the postern backend now, so that it reads %s\n", r.EnvFile)
	}
	return out.String()
}

// Run writes or replaces the three postern lines and makes the snapshot
// directory, backing the config file up first when it changes it.
func (s PosternServe) Run(ctx context.Context, req PosternServeRequest) (PosternServeReport, error) {
	if s.Files == nil {
		return PosternServeReport{}, fmt.Errorf("mw postern serve: nowhere to read or write the config file")
	}
	if strings.TrimSpace(s.ConfigPath) == "" {
		return PosternServeReport{}, fmt.Errorf("mw postern serve: no config file path is set")
	}
	if err := req.validate(); err != nil {
		return PosternServeReport{}, err
	}

	text, exists, err := s.Files.Read(ctx, s.ConfigPath)
	if err != nil {
		return PosternServeReport{}, fmt.Errorf("reading %s: %w", s.ConfigPath, err)
	}
	newText := upsertRootKeys(text, req.configKeys(), req.values())
	snapshotDir := filepath.Dir(req.SnapshotPath)

	report := PosternServeReport{
		ConfigPath:  s.ConfigPath,
		SnapshotDir: snapshotDir,
		Changed:     newText != text,
		DryRun:      req.DryRun,
		NewText:     newText,
	}

	var envText string
	var envExists bool
	if req.EnvFile != "" {
		if envText, envExists, err = s.Files.Read(ctx, req.EnvFile); err != nil {
			return PosternServeReport{}, fmt.Errorf("reading %s: %w", req.EnvFile, err)
		}
		report.EnvFile = req.EnvFile
		report.EnvNewText = upsertEnvLines(envText, posternEnvKeys, req.envValues())
		report.EnvChanged = report.EnvNewText != envText
		report.ViewDir = filepath.Dir(req.ViewPath)
	}

	if req.DryRun {
		s.printf(report.String())
		return report, nil
	}

	if report.Changed {
		if exists {
			backupPath, err := s.Files.Backup(ctx, s.ConfigPath, s.backupDir())
			if err != nil {
				return report, fmt.Errorf("backing up %s: %w", s.ConfigPath, err)
			}
			report.BackupPath = backupPath
		}
		if err := s.Files.Write(ctx, s.ConfigPath, newText); err != nil {
			return report, fmt.Errorf("writing %s: %w", s.ConfigPath, err)
		}
	}
	if err := s.Files.MkdirAll(ctx, snapshotDir); err != nil {
		return report, fmt.Errorf("making the snapshot directory %s: %w", snapshotDir, err)
	}

	if req.EnvFile != "" {
		if report.EnvChanged {
			if envExists {
				backupPath, err := s.Files.Backup(ctx, req.EnvFile, s.backupDir())
				if err != nil {
					return report, fmt.Errorf("backing up %s: %w", req.EnvFile, err)
				}
				report.EnvBackupPath = backupPath
			}
			if err := s.Files.Write(ctx, req.EnvFile, report.EnvNewText); err != nil {
				return report, fmt.Errorf("writing %s: %w", req.EnvFile, err)
			}
		}
		if err := s.Files.MkdirAll(ctx, report.ViewDir); err != nil {
			return report, fmt.Errorf("making the view directory %s: %w", report.ViewDir, err)
		}
	}

	s.printf(report.String())
	return report, nil
}

func (s PosternServe) backupDir() string {
	if strings.TrimSpace(s.BackupDir) != "" {
		return s.BackupDir
	}
	return DefaultPosternHandBackupDir()
}

func (s PosternServe) printf(text string) {
	if s.Out != nil {
		fmt.Fprint(s.Out, text)
	}
}

// upsertRootKeys returns text with each of order's keys set to values[key],
// replacing a root-table line that already sets it and otherwise appending a
// new line before the first [table] header, or at the end when there is
// none. A key already set to the value asked is left as it is, byte for
// byte, and text with nothing to change is returned unmodified — so a
// caller can tell a re-run that changed nothing from one that wrote.
func upsertRootKeys(text string, order []string, values map[string]string) string {
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}

	found := map[string]bool{}
	modified := false
	rootEnd := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			rootEnd = i
			break
		}
		name, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		for _, key := range order {
			if name != key {
				continue
			}
			found[key] = true
			want := fmt.Sprintf("%s = %q", key, values[key])
			if lines[i] != want {
				lines[i] = want
				modified = true
			}
		}
	}

	var toAppend []string
	for _, key := range order {
		if !found[key] {
			toAppend = append(toAppend, fmt.Sprintf("%s = %q", key, values[key]))
		}
	}
	if len(toAppend) == 0 && !modified {
		return text
	}

	head := append([]string{}, lines[:rootEnd]...)
	tail := append([]string{}, lines[rootEnd:]...)
	head = append(head, toAppend...)
	return strings.Join(append(head, tail...), "\n") + "\n"
}

// upsertEnvLines returns an environment file's text with each of order's
// names set to values[name], as NAME="value": a line that already sets the
// name — "export " ahead of it or not — is replaced in place, and a name no
// line sets is appended at the end. Comments, blank lines and every other
// name are left exactly as they were, and a line already saying what is
// asked is left byte for byte, so text with nothing to change comes back
// unmodified.
func upsertEnvLines(text string, order []string, values map[string]string) string {
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}

	found := map[string]bool{}
	modified := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, _, ok := strings.Cut(strings.TrimPrefix(trimmed, "export "), "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value, wanted := values[name]
		if !wanted {
			continue
		}
		found[name] = true
		if want := fmt.Sprintf("%s=%q", name, value); lines[i] != want {
			lines[i] = want
			modified = true
		}
	}

	for _, name := range order {
		if !found[name] {
			lines = append(lines, fmt.Sprintf("%s=%q", name, values[name]))
			modified = true
		}
	}
	if !modified {
		return text
	}
	return strings.Join(lines, "\n") + "\n"
}

// posternAPIEndMarker is the comment already in the postern's nginx site,
// after the /api location(s), that mw postern nginx anchors the /snapshot
// location on. posternSnapshotMarker and posternEventsMarker are the
// comments mw postern nginx writes ahead of the /snapshot and /api/events
// locations themselves, so a re-run can find and replace its own block
// rather than growing a second one.
const (
	posternAPIEndMarker   = "# mw-api end"
	posternSnapshotMarker = "# mw-snapshot"
	posternEventsMarker   = "# mw-api-events"
	// posternUpstreamMarker opens the upstream block a run with two or more
	// backends writes at the top of the site file.
	posternUpstreamMarker = "# mw-api-upstream"
	// posternFailoverTag ends every line a multi-backend run adds inside an
	// /api location, so a later run can take them out before writing again.
	posternFailoverTag = "# mw-failover"
	// posternUpstreamName is the upstream every /api location passes to when
	// there is more than one backend.
	posternUpstreamName = "postern_api"
)

// PosternNginxRequest is what mw postern nginx is asked to ensure in the
// nginx site file: the /api/events location, the /snapshot location,
// aliased to SnapshotPath, and the /api upstream — the events location's
// among them — set to Backends: to that one backend, or, with two or more,
// to an upstream block over them that fails over to whichever answers.
type PosternNginxRequest struct {
	Backends     []string
	SnapshotPath string
	DryRun       bool
}

// validate refuses a request that could not be turned into nginx config,
// reporting the authority proxy_pass is set to — the one backend's scheme
// and host, or, with two or more backends, the upstream's name — and, for
// two or more, the hosts the upstream block names.
func (r PosternNginxRequest) validate() (authority string, hosts []string, err error) {
	if strings.TrimSpace(r.SnapshotPath) == "" {
		return "", nil, fmt.Errorf("mw postern nginx: no snapshot path is configured (postern_snapshot_path)")
	}
	if len(r.Backends) == 0 {
		return "", nil, fmt.Errorf("mw postern nginx: --backend is required")
	}
	var scheme string
	for _, backend := range r.Backends {
		if strings.TrimSpace(backend) == "" {
			return "", nil, fmt.Errorf("mw postern nginx: --backend is required")
		}
		u, err := url.Parse(backend)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", nil, fmt.Errorf("mw postern nginx: --backend %q is not a URL of the form http://host:port", backend)
		}
		if scheme != "" && u.Scheme != scheme {
			return "", nil, fmt.Errorf("mw postern nginx: the --backend URLs must share a scheme, an upstream has one: got %s and %s", scheme, u.Scheme)
		}
		scheme = u.Scheme
		hosts = append(hosts, u.Host)
		authority = u.Scheme + "://" + u.Host
	}
	if len(hosts) == 1 {
		return authority, nil, nil
	}
	return scheme + "://" + posternUpstreamName, hosts, nil
}

// PosternNginx ensures the postern's /api/events location, its /snapshot
// location and its /api upstream in an nginx site file, backing it up first,
// then runs `nginx -t` and, only once that passes, `systemctl reload nginx`.
// The events location is the backend's event stream, held open for as long
// as the app is: nothing buffered or cached, an hour before an idle read
// gives up, and HTTP/1.1 with no Connection header so the connection to the
// backend is kept. It sits ahead of the general /api/ location. A run that would change
// nothing is a no-op: nothing is backed up, written, tested or reloaded.
// --dry-run prints what would change without touching anything. A failed
// nginx -t restores the backup, so a bad edit is never left live.
type PosternNginx struct {
	Conf     PosternHandFile
	ConfPath string
	// BackupDir names where the site file is backed up before it is
	// changed. Empty reads DefaultPosternHandBackupDir().
	BackupDir string
	Runner    PosternNginxRunner

	// Out is where the report is printed. A nil Out prints nothing.
	Out io.Writer
}

// PosternNginxReport is what nginx did, or — under --dry-run — would do.
type PosternNginxReport struct {
	ConfPath     string
	BackupPath   string
	Changed      bool
	DryRun       bool
	NewText      string
	TestOutput   string
	ReloadOutput string
}

func (r PosternNginxReport) String() string {
	var out strings.Builder
	if !r.Changed {
		fmt.Fprintf(&out, "%s already says this: nothing changed.\n", r.ConfPath)
		return out.String()
	}
	if r.DryRun {
		fmt.Fprintf(&out, "--dry-run: would write %s:\n\n%s\n", r.ConfPath, r.NewText)
		return out.String()
	}
	fmt.Fprintf(&out, "backed up %s to %s\n", r.ConfPath, r.BackupPath)
	fmt.Fprintf(&out, "wrote %s\n", r.ConfPath)
	fmt.Fprintf(&out, "nginx -t:\n%s\n", r.TestOutput)
	fmt.Fprintf(&out, "systemctl reload nginx:\n%s\n", r.ReloadOutput)
	fmt.Fprintf(&out, "the way back: cp %s %s && nginx -t && systemctl reload nginx\n", r.BackupPath, r.ConfPath)
	return out.String()
}

// Run ensures the /snapshot location and the /api upstream, backs the site
// file up, writes it, and tests and reloads nginx through Runner — unless
// nothing would change, when it touches nothing at all.
func (n PosternNginx) Run(ctx context.Context, req PosternNginxRequest) (PosternNginxReport, error) {
	if n.Conf == nil {
		return PosternNginxReport{}, fmt.Errorf("mw postern nginx: nowhere to read or write the site file")
	}
	if strings.TrimSpace(n.ConfPath) == "" {
		return PosternNginxReport{}, fmt.Errorf("mw postern nginx: no nginx site file path is set")
	}
	authority, upstreamHosts, err := req.validate()
	if err != nil {
		return PosternNginxReport{}, err
	}

	text, exists, err := n.Conf.Read(ctx, n.ConfPath)
	if err != nil {
		return PosternNginxReport{}, fmt.Errorf("reading %s: %w", n.ConfPath, err)
	}
	if !exists {
		return PosternNginxReport{}, fmt.Errorf("mw postern nginx: no nginx site file at %s", n.ConfPath)
	}

	withEvents, err := ensureEventsLocation(stripFailoverLines(text), authority)
	if err != nil {
		return PosternNginxReport{}, err
	}
	withSnapshot, err := ensureSnapshotLocation(withEvents, req.SnapshotPath)
	if err != nil {
		return PosternNginxReport{}, err
	}
	newText := ensureUpstreamBlock(setAPIBackend(withSnapshot, authority, len(upstreamHosts) > 1), upstreamHosts)

	report := PosternNginxReport{ConfPath: n.ConfPath, Changed: newText != text, DryRun: req.DryRun, NewText: newText}
	if !report.Changed || req.DryRun {
		n.printf(report.String())
		return report, nil
	}
	if n.Runner == nil {
		return report, fmt.Errorf("mw postern nginx: no way to test or reload nginx is configured")
	}

	backupPath, err := n.Conf.Backup(ctx, n.ConfPath, n.backupDir())
	if err != nil {
		return report, fmt.Errorf("backing up %s: %w", n.ConfPath, err)
	}
	report.BackupPath = backupPath

	if err := n.Conf.Write(ctx, n.ConfPath, newText); err != nil {
		return report, fmt.Errorf("writing %s: %w", n.ConfPath, err)
	}

	output, ok, err := n.Runner.Test(ctx)
	report.TestOutput = output
	if err != nil {
		return report, fmt.Errorf("running nginx -t: %w", err)
	}
	if !ok {
		if werr := n.Conf.Write(ctx, n.ConfPath, text); werr != nil {
			return report, fmt.Errorf("nginx -t failed:\n%s\nand restoring %s from the backup at %s also failed: %w", output, n.ConfPath, backupPath, werr)
		}
		return report, fmt.Errorf("nginx -t failed, so nothing was reloaded; %s was restored from %s:\n%s", n.ConfPath, backupPath, output)
	}

	reloadOutput, err := n.Runner.Reload(ctx)
	report.ReloadOutput = reloadOutput
	if err != nil {
		return report, fmt.Errorf("nginx -t passed but reloading failed: %w; the way back is to restore %s from %s", err, n.ConfPath, backupPath)
	}

	n.printf(report.String())
	return report, nil
}

func (n PosternNginx) backupDir() string {
	if strings.TrimSpace(n.BackupDir) != "" {
		return n.BackupDir
	}
	return DefaultPosternHandBackupDir()
}

func (n PosternNginx) printf(text string) {
	if n.Out != nil {
		fmt.Fprint(n.Out, text)
	}
}

// snapshotBlock is the /snapshot location mw postern nginx writes, aliased
// to path: no-store, nosniff, served as an opaque octet stream.
func snapshotBlock(path string) []string {
	return []string{
		"    " + posternSnapshotMarker,
		"    location = /snapshot {",
		fmt.Sprintf("        alias %s;", path),
		`        add_header Cache-Control "no-store" always;`,
		`        add_header X-Content-Type-Options "nosniff" always;`,
		"        default_type application/octet-stream;",
		"    }",
	}
}

// eventsBlock is the /api/events location mw postern nginx writes, passed
// to the backend at authority (a URL's scheme and host) with the request's
// own path: the event stream, never buffered or cached, held an hour
// between reads, over HTTP/1.1 with the Connection header cleared so nginx
// keeps the backend's connection open.
func eventsBlock(authority string) []string {
	return []string{
		"    " + posternEventsMarker,
		"    location = /api/events {",
		"        proxy_pass " + authority + ";",
		"        proxy_buffering off;",
		"        proxy_cache off;",
		"        proxy_read_timeout 1h;",
		"        proxy_http_version 1.1;",
		`        proxy_set_header Connection "";`,
		"    }",
	}
}

// ensureEventsLocation returns text with the /api/events location block
// present: replacing its own block, marked with posternEventsMarker, when
// one is already there, or inserting a fresh one just ahead of the general
// /api/ location when it is not — or, in a site with no such location, just
// ahead of posternAPIEndMarker. A site with neither does not look like the
// postern's, and is refused rather than guessed at.
func ensureEventsLocation(text, authority string) (string, error) {
	lines := strings.Split(text, "\n")
	block := eventsBlock(authority)

	if start, end, ok := findBlock(lines, posternEventsMarker); ok {
		rebuilt := append([]string{}, lines[:start]...)
		rebuilt = append(rebuilt, block...)
		rebuilt = append(rebuilt, lines[end+1:]...)
		return strings.Join(rebuilt, "\n"), nil
	}

	at := -1
	for i, line := range lines {
		if isGeneralAPILocation(line) {
			at = i
			break
		}
	}
	if at < 0 {
		for i, line := range lines {
			if strings.Contains(line, posternAPIEndMarker) {
				at = i
				break
			}
		}
	}
	if at < 0 {
		return "", fmt.Errorf("mw postern nginx: no /api/ location and no %q marker in the nginx site: it does not look like the postern's site file", posternAPIEndMarker)
	}
	rebuilt := append([]string{}, lines[:at]...)
	rebuilt = append(rebuilt, block...)
	rebuilt = append(rebuilt, lines[at:]...)
	return strings.Join(rebuilt, "\n"), nil
}

// posternGeneralAPIRe matches the line that opens the general /api/
// location: a prefix location — no modifier, or ^~ — on /api/ or /api. An
// exact (=) or regular expression (~, ~*) location is never it.
var posternGeneralAPIRe = regexp.MustCompile(`^\s*location\s+(?:\^~\s+)?/api/?\s*\{`)

// isGeneralAPILocation reports whether line opens the general /api/
// location.
func isGeneralAPILocation(line string) bool { return posternGeneralAPIRe.MatchString(line) }

// ensureSnapshotLocation returns text with the /snapshot location block
// present and aliased to snapshotPath: replacing its own block, marked with
// posternSnapshotMarker, when one is already there, or inserting a fresh one
// after posternAPIEndMarker when it is not. A site with neither marker does
// not look like the postern's, and is refused rather than guessed at.
func ensureSnapshotLocation(text, snapshotPath string) (string, error) {
	lines := strings.Split(text, "\n")
	block := snapshotBlock(snapshotPath)

	if start, end, ok := findBlock(lines, posternSnapshotMarker); ok {
		rebuilt := append([]string{}, lines[:start]...)
		rebuilt = append(rebuilt, block...)
		rebuilt = append(rebuilt, lines[end+1:]...)
		return strings.Join(rebuilt, "\n"), nil
	}

	for i, line := range lines {
		if !strings.Contains(line, posternAPIEndMarker) {
			continue
		}
		rebuilt := append([]string{}, lines[:i+1]...)
		rebuilt = append(rebuilt, block...)
		rebuilt = append(rebuilt, lines[i+1:]...)
		return strings.Join(rebuilt, "\n"), nil
	}
	return "", fmt.Errorf("mw postern nginx: no %q marker in the nginx site: it does not look like the postern's site file", posternAPIEndMarker)
}

// findBlock reports the span [start, end], inclusive, of the line holding
// marker through the next line that is only "}" — that block's own close.
func findBlock(lines []string, marker string) (start, end int, ok bool) {
	for i, line := range lines {
		if !strings.Contains(line, marker) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "}" {
				return i, j, true
			}
		}
		return 0, 0, false
	}
	return 0, 0, false
}

// posternProxyPassRe matches one proxy_pass line, capturing everything
// ahead of the URL, the URL's scheme and host, whatever path follows it, and
// the trailing semicolon (with any comment-free whitespace after it).
var posternProxyPassRe = regexp.MustCompile(`^(\s*proxy_pass\s+)(https?://[^/;\s]+)([^;\s]*)(;\s*)$`)

// posternLocationRe matches an nginx `location <path> {` line, capturing
// the path — skipping past an optional modifier (=, ^~, ~, ~*) ahead of it.
var posternLocationRe = regexp.MustCompile(`^\s*location\s+(?:(?:=|\^~|~\*|~)\s+)?(\S+)\s*\{`)

// posternFailoverLines are the directives that follow the proxy_pass of
// every /api location when there is more than one backend: retry the next
// backend on a connect error, a timeout or a 503 — even for a POST — and
// give up on a dead backend quickly.
var posternFailoverLines = []string{
	"proxy_next_upstream error timeout http_503 non_idempotent; " + posternFailoverTag,
	"proxy_connect_timeout 2s; " + posternFailoverTag,
}

// stripFailoverLines returns text without the lines an earlier run added
// under posternFailoverTag.
func stripFailoverLines(text string) string {
	lines := strings.Split(text, "\n")
	kept := lines[:0:0]
	for _, line := range lines {
		if strings.HasSuffix(strings.TrimSpace(line), posternFailoverTag) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// upstreamBlock is the upstream block written ahead of the site's server
// block when there are two or more backends. The comment says why a retry on
// the next backend is safe.
func upstreamBlock(hosts []string) []string {
	block := []string{
		posternUpstreamMarker,
		"# A standby backend answers 503 before it does anything (story postern-standby),",
		"# so a request retried on the next backend, a POST too, has run nowhere twice.",
		"upstream " + posternUpstreamName + " {",
	}
	for _, host := range hosts {
		block = append(block, "    server "+host+";")
	}
	return append(block, "}")
}

// ensureUpstreamBlock returns text with the upstream block over hosts at its
// top — replacing its own block, marked with posternUpstreamMarker, when one
// is there — or, when hosts is empty (one backend), with any such block taken
// out again, so one backend's output is the same as it was before there was
// an upstream.
func ensureUpstreamBlock(text string, hosts []string) string {
	lines := strings.Split(text, "\n")
	start, end, found := findBlock(lines, posternUpstreamMarker)
	if found {
		if end+1 < len(lines) && lines[end+1] == "" {
			end++
		}
		lines = append(append([]string{}, lines[:start]...), lines[end+1:]...)
	}
	if len(hosts) == 0 {
		return strings.Join(lines, "\n")
	}
	block := append(upstreamBlock(hosts), "")
	return strings.Join(append(block, lines...), "\n")
}

// setAPIBackend returns text with every proxy_pass line inside a `location`
// block whose path names /api pointed at authority (a URL's scheme and
// host), its own path suffix — /healthz, say — left exactly as it was. With
// failover set, each such line is followed by posternFailoverLines.
func setAPIBackend(text, authority string, failover bool) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	depth := 0
	inAPI := false
	apiDepth := 0

	for _, line := range lines {
		if m := posternLocationRe.FindStringSubmatch(line); m != nil && !inAPI && strings.Contains(m[1], "/api") {
			inAPI = true
			apiDepth = depth + strings.Count(line, "{") - strings.Count(line, "}")
		}
		if m := posternProxyPassRe.FindStringSubmatch(line); inAPI && m != nil {
			out = append(out, m[1]+authority+m[3]+m[4])
			if failover {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				for _, directive := range posternFailoverLines {
					out = append(out, indent+directive)
				}
			}
		} else {
			out = append(out, line)
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if inAPI && depth < apiDepth {
			inAPI = false
		}
	}
	return strings.Join(out, "\n")
}

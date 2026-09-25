package application

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultPosternHandBackupDir is where mw postern serve and mw postern nginx
// back up the file they are about to change, when --backup-dir says
// nothing: the VPS's own scratch backup directory, the same one the hand
// steps this pair replaces already backed up to.
const DefaultPosternHandBackupDir = "/root/tidy"

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

// PosternServeRequest is what mw postern serve is asked to set in this
// host's config file.
type PosternServeRequest struct {
	Backend      string
	SnapshotPath string
	GovernorKey  string
	DryRun       bool
}

// posternServeKeys is the order mw postern serve writes or replaces the
// three postern lines in, root-table keys of ~/.config/mw/config.toml.
var posternServeKeys = []string{"postern_backend", "postern_snapshot_path", "postern_governor_key"}

func (r PosternServeRequest) values() map[string]string {
	return map[string]string{
		"postern_backend":       r.Backend,
		"postern_snapshot_path": r.SnapshotPath,
		"postern_governor_key":  r.GovernorKey,
	}
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
	return nil
}

// PosternServe writes or replaces the three postern lines this host's
// config file needs to serve the postern to the Governor's app —
// postern_backend, postern_snapshot_path and postern_governor_key —
// idempotent, so a re-run with the same values changes nothing, and makes
// the snapshot's own directory. It backs the config file up first, unless
// there is none yet to back up, and --dry-run prints what it would do
// without touching anything.
type PosternServe struct {
	Files      PosternHandFile
	ConfigPath string
	// BackupDir names where the config file is backed up before it is
	// changed. Empty reads DefaultPosternHandBackupDir.
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
	newText := upsertRootKeys(text, posternServeKeys, req.values())
	snapshotDir := filepath.Dir(req.SnapshotPath)

	report := PosternServeReport{
		ConfigPath:  s.ConfigPath,
		SnapshotDir: snapshotDir,
		Changed:     newText != text,
		DryRun:      req.DryRun,
		NewText:     newText,
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

	s.printf(report.String())
	return report, nil
}

func (s PosternServe) backupDir() string {
	if strings.TrimSpace(s.BackupDir) != "" {
		return s.BackupDir
	}
	return DefaultPosternHandBackupDir
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

// posternAPIEndMarker is the comment already in the postern's nginx site,
// after the /api location(s), that mw postern nginx anchors the /snapshot
// location on. posternSnapshotMarker is the comment mw postern nginx writes
// ahead of that location itself, so a re-run can find and replace its own
// block rather than growing a second one.
const (
	posternAPIEndMarker   = "# mw-api end"
	posternSnapshotMarker = "# mw-snapshot"
)

// PosternNginxRequest is what mw postern nginx is asked to ensure in the
// nginx site file: the /snapshot location, aliased to SnapshotPath, and the
// /api upstream, set to Backend.
type PosternNginxRequest struct {
	Backend      string
	SnapshotPath string
	DryRun       bool
}

// validate refuses a request that could not be turned into nginx config,
// reporting Backend's scheme and host (what proxy_pass is set to) once it
// does not.
func (r PosternNginxRequest) validate() (authority string, err error) {
	if strings.TrimSpace(r.SnapshotPath) == "" {
		return "", fmt.Errorf("mw postern nginx: no snapshot path is configured (postern_snapshot_path)")
	}
	if strings.TrimSpace(r.Backend) == "" {
		return "", fmt.Errorf("mw postern nginx: --backend is required")
	}
	u, err := url.Parse(r.Backend)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("mw postern nginx: --backend %q is not a URL of the form http://host:port", r.Backend)
	}
	return u.Scheme + "://" + u.Host, nil
}

// PosternNginx ensures the postern's /snapshot location and /api upstream in
// an nginx site file, backing it up first, then runs `nginx -t` and, only
// once that passes, `systemctl reload nginx`. A run that would change
// nothing is a no-op: nothing is backed up, written, tested or reloaded.
// --dry-run prints what would change without touching anything. A failed
// nginx -t restores the backup, so a bad edit is never left live.
type PosternNginx struct {
	Conf     PosternHandFile
	ConfPath string
	// BackupDir names where the site file is backed up before it is
	// changed. Empty reads DefaultPosternHandBackupDir.
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
	authority, err := req.validate()
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

	withSnapshot, err := ensureSnapshotLocation(text, req.SnapshotPath)
	if err != nil {
		return PosternNginxReport{}, err
	}
	newText := setAPIBackend(withSnapshot, authority)

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
	return DefaultPosternHandBackupDir
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
// the path.
var posternLocationRe = regexp.MustCompile(`^\s*location\s+(\S+)\s*\{`)

// setAPIBackend returns text with every proxy_pass line inside a `location`
// block whose path names /api pointed at authority (a URL's scheme and
// host), its own path suffix — /healthz, say — left exactly as it was.
func setAPIBackend(text, authority string) string {
	lines := strings.Split(text, "\n")
	depth := 0
	inAPI := false
	apiDepth := 0

	for i, line := range lines {
		if m := posternLocationRe.FindStringSubmatch(line); m != nil && !inAPI && strings.Contains(m[1], "/api") {
			inAPI = true
			apiDepth = depth + strings.Count(line, "{") - strings.Count(line, "}")
		}
		if inAPI {
			if m := posternProxyPassRe.FindStringSubmatch(line); m != nil {
				lines[i] = m[1] + authority + m[3] + m[4]
			}
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if inAPI && depth < apiDepth {
			inAPI = false
		}
	}
	return strings.Join(lines, "\n")
}

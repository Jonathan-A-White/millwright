package application

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// MemberHost is what Member asks of the machine a member's own Linux user is
// on: files under that user's home, the dolt it will serve beads with, the
// user manager that runs the Dolt unit, and the beads database made on the
// server the unit starts. Every path is a full one.
type MemberHost interface {
	// Exists reports whether a file or directory is there.
	Exists(path string) bool

	// ReadFile reads a file whole.
	ReadFile(path string) (string, error)

	// WriteFile writes text to a file that is not there yet, making its
	// directories, with the given mode. It refuses a file that is there: a
	// member's factory is made once.
	WriteFile(path, text string, mode fs.FileMode) error

	// ReplaceFile replaces the text of a file that is there: the one place a
	// member's factory changes what the starter kit laid.
	ReplaceFile(path, text string) error

	// MakeDir makes a directory and the ones above it.
	MakeDir(path string) error

	// DoltProgram is the full path of the dolt the unit runs: given, when the
	// member named one, otherwise the one on the PATH. It fails when there is
	// none, saying that --dolt names one.
	DoltProgram(given string) (string, error)

	// PortFree fails when something already listens on 127.0.0.1:port.
	PortFree(ctx context.Context, port int) error

	// NewPassword is a fresh random password for the beads user.
	NewPassword() (string, error)

	// EnableUnit makes the user manager read the unit files again, then enables
	// the unit and starts it: systemctl --user daemon-reload, then enable --now.
	EnableUnit(ctx context.Context, unit string) error

	// DisableUnit stops the unit and disables it, and deletes nothing.
	DisableUnit(ctx context.Context, unit string) error

	// ServerAnswers waits until the Dolt server on 127.0.0.1:port accepts a
	// connection, and fails when it does not within a short wait.
	ServerAnswers(ctx context.Context, port int) error

	// CreateBeadsUser makes the beads user, with the password, on the server on
	// 127.0.0.1:port.
	CreateBeadsUser(ctx context.Context, port int, user, password string) error

	// InitBeads makes the beads database of the vault in dir on the server on
	// 127.0.0.1:port, as the user: bd init against a server that is already
	// there, never bd migrate. The password is passed to bd in its
	// environment and in no argument.
	InitBeads(ctx context.Context, dir, prefix string, port int, user, password string) error

	// Program is the full path of a tool on the PATH, and fails saying it was
	// not found.
	Program(name string) (string, error)

	// LingerEnabled reports whether the user manager keeps running while the
	// member is logged out, so the Dolt unit does too.
	LingerEnabled(ctx context.Context) (bool, error)
}

// MemberFirstCommit is the message of a member's vault's first commit.
const MemberFirstCommit = "A member's vault, from the starter kit"

// MemberUnit is the member's Dolt user unit. It is the same name the
// Governor's own is, because each runs in its own user manager.
const MemberUnit = "dolt-beads.service"

// MemberBeadsUser is the Dolt user bd connects as.
const MemberBeadsUser = "beads"

// Where a member's files are, under the home. The config file's path is
// config.File in infrastructure/config, repeated here because application does
// not import it.
var (
	memberConfigFile = filepath.Join(".config", "mw", "config.toml")
	memberBeadsEnv   = filepath.Join(".config", "mw", "beads.env")
	memberUnitFile   = filepath.Join(".config", "systemd", "user", MemberUnit)
	memberDoltData   = filepath.Join(".local", "share", "mw", "dolt-beads")
)

var validMemberName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Member makes a member's own factory, run as that member's Linux user: their
// config file, a Dolt user unit on the port they chose with the beads.env bd
// reads, a fresh vault laid from the starter kit with its beads database made
// on that server, and a Family section in the Mayor's charter that names the
// licence holder.
//
// It checks everything it can before it writes anything, so a refusal leaves
// nothing behind, and it never overwrites: a second run is refused.
type Member struct {
	Vault    VaultBirth
	Host     MemberHost
	Template fs.FS
}

// MemberRequest is one member's factory to make.
type MemberRequest struct {
	// Name is the member's name: it is the beads prefix unless Prefix is set.
	Name string
	// Prefix is what the ids of the member's stories begin with; Name when empty.
	Prefix string
	// Home is the member's home directory, a full path.
	Home string
	// Dir is the member's vault, a full path.
	Dir string
	// DoltPort is the port of the member's own Dolt server.
	DoltPort int
	// Dolt is the dolt program, when it is not the one on the PATH.
	Dolt string
	// Holder is the licence holder: who this factory's epics are released by.
	Holder string
	// Backend is the URL of the Postern backend the member's mw talks to.
	Backend string
	// Host is the name the config file gives this host.
	Host string
}

// MemberReport is what Member did and what is still owed.
type MemberReport struct {
	Name       string
	Holder     string
	Port       int
	Dir        string
	ConfigPath string
	EnvPath    string
	UnitPath   string
	DataDir    string
	Dolt       string

	// LingerSet is whether the user manager runs while the member is logged
	// out; LingerKnown is whether loginctl could say.
	LingerSet   bool
	LingerKnown bool
}

// Run makes the factory.
func (m Member) Run(ctx context.Context, req MemberRequest) (MemberReport, error) {
	if m.Vault == nil || m.Host == nil || m.Template == nil {
		return MemberReport{}, fmt.Errorf("making a member's factory: there is no place to make it")
	}
	if err := req.validate(); err != nil {
		return MemberReport{}, err
	}

	report := MemberReport{
		Name:       req.Name,
		Holder:     req.Holder,
		Port:       req.DoltPort,
		Dir:        req.Dir,
		ConfigPath: filepath.Join(req.Home, memberConfigFile),
		EnvPath:    filepath.Join(req.Home, memberBeadsEnv),
		UnitPath:   filepath.Join(req.Home, memberUnitFile),
		DataDir:    filepath.Join(req.Home, memberDoltData),
	}
	for _, path := range []string{report.ConfigPath, report.EnvPath, report.UnitPath} {
		if m.Host.Exists(path) {
			return report, fmt.Errorf("already a member factory at %s: mw member init makes one once and touches nothing that is there; mw member remove stops its Dolt unit", path)
		}
	}
	if err := m.Vault.Vacant(ctx, req.Dir); err != nil {
		return report, err
	}
	dolt, err := m.Host.DoltProgram(req.Dolt)
	if err != nil {
		return report, err
	}
	report.Dolt = dolt
	if err := m.Host.PortFree(ctx, req.DoltPort); err != nil {
		return report, fmt.Errorf("the Dolt port %d is not free: %w: choose another with --dolt-port", req.DoltPort, err)
	}
	password, err := m.Host.NewPassword()
	if err != nil {
		return report, fmt.Errorf("making a password for the beads user: %w", err)
	}

	if err := m.layVault(ctx, req); err != nil {
		return report, err
	}
	if err := m.Host.MakeDir(report.DataDir); err != nil {
		return report, err
	}
	if err := m.Host.WriteFile(report.EnvPath, req.beadsEnv(password), 0o600); err != nil {
		return report, err
	}
	if err := m.Host.WriteFile(report.UnitPath, req.unit(dolt, report.DataDir), 0o644); err != nil {
		return report, err
	}
	stuck := fmt.Sprintf("; mw member remove stops the unit, then remove %s, %s, %s and %s and run mw member init again", report.EnvPath, report.UnitPath, report.DataDir, req.Dir)
	if err := m.Host.EnableUnit(ctx, MemberUnit); err != nil {
		return report, fmt.Errorf("%w%s", err, stuck)
	}
	if err := m.Host.ServerAnswers(ctx, req.DoltPort); err != nil {
		return report, fmt.Errorf("%w%s", err, stuck)
	}
	if err := m.Host.CreateBeadsUser(ctx, req.DoltPort, MemberBeadsUser, password); err != nil {
		return report, fmt.Errorf("%w%s", err, stuck)
	}
	if err := m.Host.InitBeads(ctx, req.Dir, req.prefix(), req.DoltPort, MemberBeadsUser, password); err != nil {
		return report, fmt.Errorf("%w%s", err, stuck)
	}
	if err := m.Vault.Commit(ctx, req.Dir, MemberFirstCommit); err != nil {
		return report, fmt.Errorf("%w: the beads database is made in %s but not committed%s", err, req.Dir, stuck)
	}

	if err := m.Host.WriteFile(report.ConfigPath, req.configText(), 0o644); err != nil {
		return report, fmt.Errorf("the factory is made, but %w", err)
	}

	// Whether linger is set is the one thing here that may be unknowable and
	// is never a reason to undo the rest: it is said as a hand step.
	report.LingerSet, err = m.Host.LingerEnabled(ctx)
	report.LingerKnown = err == nil
	return report, nil
}

// layVault lays the starter kit into the vault, adds the Family section to the
// Mayor's charter, and makes the first commit.
func (m Member) layVault(ctx context.Context, req MemberRequest) error {
	if err := m.Vault.Lay(ctx, req.Dir, m.Template); err != nil {
		return err
	}
	charterPath := filepath.Join(req.Dir, "seats", "mayor", "charter.md")
	charter, err := m.Host.ReadFile(charterPath)
	if err != nil {
		return fmt.Errorf("%w: the starter kit is laid in %s but has no Mayor charter; remove the directory and run mw member init again", err, req.Dir)
	}
	withFamily, err := FamilySection(charter, req.Holder)
	if err != nil {
		return fmt.Errorf("%w: the starter kit is laid in %s; remove the directory and run mw member init again", err, req.Dir)
	}
	if err := m.Host.ReplaceFile(charterPath, withFamily); err != nil {
		return err
	}
	if err := m.Vault.Commit(ctx, req.Dir, MemberFirstCommit); err != nil {
		return fmt.Errorf("%w: the starter kit is laid in %s but not committed; remove the directory and run mw member init again", err, req.Dir)
	}
	return nil
}

// FamilySection returns the Mayor's charter with a Family section after the
// paragraph on whom the Mayor serves: who the licence holder is, that every
// epic is released only on their approval, and that their key has the cockpit
// role in this factory.
func FamilySection(charter, holder string) (string, error) {
	const marker = "**Who you serve.**"
	start := strings.Index(charter, marker)
	if start < 0 {
		return "", fmt.Errorf("the Mayor's charter has no %q paragraph to put the Family section after", marker)
	}
	end := strings.Index(charter[start:], "\n\n")
	if end < 0 {
		return "", fmt.Errorf("the Mayor's charter has nothing after its %q paragraph", marker)
	}
	end += start + 2

	section := "## Family\n\n" +
		"This factory belongs to a family. Its licence holder is " + holder + ".\n\n" +
		"- Every epic is released only on the holder's approval (a decision card they tap from their own account); do not release one on anyone else's word.\n" +
		"- The holder's key has the cockpit role here.\n\n"
	return charter[:end] + section + charter[end:], nil
}

// validate refuses a request that could not make a factory, before anything
// is written.
func (r MemberRequest) validate() error {
	switch {
	case !validMemberName.MatchString(r.Name):
		return fmt.Errorf("the name %q will not do: it starts with a lowercase letter and holds only lowercase letters, digits, - and _", r.Name)
	case r.Prefix != "" && !validPrefix.MatchString(r.Prefix):
		return fmt.Errorf("the prefix %q will not do: it starts with a letter and holds only letters, digits, - and _", r.Prefix)
	case !filepath.IsAbs(r.Home) || !filepath.IsAbs(r.Dir):
		return fmt.Errorf("making a member's factory: the home and the vault must be full paths")
	case r.DoltPort < 1024 || r.DoltPort > 65535:
		return fmt.Errorf("the Dolt port %d will not do: choose one from 1024 to 65535 that nothing else uses (--dolt-port)", r.DoltPort)
	case strings.TrimSpace(r.Holder) == "" || strings.ContainsAny(r.Holder, "\n\r"):
		return fmt.Errorf("the holder %q will not do: --holder names the licence holder in one line", r.Holder)
	case strings.TrimSpace(r.Host) == "":
		return fmt.Errorf("making a member's factory: it needs the name of this host")
	}
	if u, err := url.Parse(r.Backend); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("the backend %q will not do: --backend is the Postern backend's URL, such as http://10.88.0.2:8788", r.Backend)
	}
	for name, value := range map[string]string{"the vault": r.Dir, "the host": r.Host, "the backend": r.Backend} {
		if strings.ContainsAny(value, "\"\n") {
			return fmt.Errorf("%s is %q, which cannot be written into a config file", name, value)
		}
	}
	return nil
}

func (r MemberRequest) prefix() string {
	if r.Prefix != "" {
		return r.Prefix
	}
	return r.Name
}

// configText is the member's config file: the vault, the host, one session at
// a time and the Postern backend.
func (r MemberRequest) configText() string {
	return fmt.Sprintf("vault = %q\nhost  = %q\ncap   = 1\npostern_backend = %q\n\n[rigs]\n", r.Dir, r.Host, r.Backend)
}

// beadsEnv is the beads.env that points bd at the member's own Dolt server.
func (r MemberRequest) beadsEnv(password string) string {
	return "# bd reaches this member's beads through their own Dolt server, the user unit dolt-beads.service.\n" +
		"# Mode 0600. The password lives here only: never copy it onto a bead, into mail or into a log.\n" +
		"BEADS_DOLT_SERVER_MODE=1\n" +
		"BEADS_DOLT_SERVER_HOST=127.0.0.1\n" +
		fmt.Sprintf("BEADS_DOLT_SERVER_PORT=%d\n", r.DoltPort) +
		"BEADS_DOLT_SERVER_USER=" + MemberBeadsUser + "\n" +
		"BEADS_DOLT_PASSWORD=" + password + "\n" +
		"# bd never starts, or kills, a Dolt server of its own: systemd owns this one.\n" +
		"BEADS_DOLT_AUTO_START=0\n" +
		"BEADS_FSCK_TIMEOUT=15m\n"
}

// unit is the Dolt user unit: the shape of the Governor's own, on the
// member's port and bound to the loopback only, serving a data directory
// outside the vault (a directory bd has not made itself inside .beads is
// taken for a legacy workspace).
func (r MemberRequest) unit(dolt, dataDir string) string {
	return "[Unit]\n" +
		"Description=Dolt SQL server serving " + r.Name + "'s beads (a member's factory, mw member init)\n\n" +
		"[Service]\n" +
		"WorkingDirectory=" + dataDir + "\n" +
		fmt.Sprintf("ExecStart=%s sql-server --host 127.0.0.1 --port %d\n", dolt, r.DoltPort) +
		"Restart=on-failure\n" +
		"RestartSec=5\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n"
}

// String says what was made, what is still owed by hand, and the way back.
func (r MemberReport) String() string {
	var out strings.Builder
	fmt.Fprintf(&out, "Made %s's factory, for the licence holder %s.\n\n", r.Name, r.Holder)
	fmt.Fprintf(&out, "Wrote:\n")
	fmt.Fprintf(&out, "  %s  (the vault, the host, the Postern backend)\n", r.ConfigPath)
	fmt.Fprintf(&out, "  %s  (bd's connection to port %d, with a generated password; readable by this user only)\n", r.EnvPath, r.Port)
	fmt.Fprintf(&out, "  %s  (enabled and started: dolt serving %s)\n", r.UnitPath, r.DataDir)
	fmt.Fprintf(&out, "  %s  (a git repository: the starter kit's seat files with a Family section naming %s, and the beads database)\n", r.Dir, r.Holder)

	fmt.Fprintf(&out, "\nStill owed, by hand:\n")
	n := 1
	if !r.LingerKnown || !r.LingerSet {
		said := "loginctl could not say whether linger is set"
		if r.LingerKnown {
			said = "linger is not set"
		}
		fmt.Fprintf(&out, "  %d. As root: loginctl enable-linger %s  (%s, so the Dolt unit stops when this user logs out)\n", n, r.Name, said)
		n++
	}
	fmt.Fprintf(&out, "  %d. Log in to Claude as this user: claude\n", n)
	n++
	fmt.Fprintf(&out, "  %d. Make a private remote for the vault, then:\n", n)
	fmt.Fprintf(&out, "       git -C %s remote add origin <url>\n", r.Dir)
	fmt.Fprintf(&out, "       git -C %s push -u origin main\n", r.Dir)

	fmt.Fprintf(&out, "\nThe way back: mw member remove stops and disables %s and deletes nothing; the files above stay where they are.\n", MemberUnit)
	return out.String()
}

// MemberRemove is the way back from Member: it stops the member's Dolt unit and
// disables it, and deletes nothing.
type MemberRemove struct {
	Host MemberHost
}

// MemberRemoveRequest is whose unit to stop.
type MemberRemoveRequest struct {
	Home string
}

// MemberRemoveReport is what MemberRemove did.
type MemberRemoveReport struct {
	UnitPath string
}

// Run stops and disables the unit. A home with no member's factory in it is
// refused.
func (r MemberRemove) Run(ctx context.Context, req MemberRemoveRequest) (MemberRemoveReport, error) {
	if r.Host == nil {
		return MemberRemoveReport{}, fmt.Errorf("removing a member's factory: there is no host to stop it on")
	}
	report := MemberRemoveReport{UnitPath: filepath.Join(req.Home, memberUnitFile)}
	if !r.Host.Exists(report.UnitPath) {
		return report, fmt.Errorf("no member factory here: %s is not there", report.UnitPath)
	}
	if err := r.Host.DisableUnit(ctx, MemberUnit); err != nil {
		return report, err
	}
	return report, nil
}

// String says what was stopped and that nothing was deleted.
func (r MemberRemoveReport) String() string {
	return fmt.Sprintf("Stopped and disabled %s (%s). Nothing was deleted: the config file, beads.env, the Dolt data and the vault are all where they were.\n", MemberUnit, r.UnitPath)
}

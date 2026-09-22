package application

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// VaultBirth is what Init asks of the disk and of git to bring a vault into
// being. It is one port because the three are one act: nothing is written until
// the directory is known to be free, and the commit is of exactly what was laid.
type VaultBirth interface {
	// Vacant reports an error unless dir does not exist or is an empty directory.
	Vacant(ctx context.Context, dir string) error

	// Lay writes every file of the template into dir, creating dir if it is not
	// there.
	Lay(ctx context.Context, dir string, template fs.FS) error

	// Commit makes dir a git repository on the branch main, if it is not one, and
	// makes one commit of everything in it with the given message and no trailer
	// of any kind. In a repository that already has a commit, whatever is new is
	// folded into that commit instead of making a second: the vault has one
	// first commit, and it is not pushed anywhere yet.
	Commit(ctx context.Context, dir, message string) error

	// WriteIfAbsent writes text to path, creating its directories, unless a file
	// is already there; it reports whether it wrote. A file that is there is not
	// read, not merged and not touched.
	WriteIfAbsent(ctx context.Context, path, text string) (bool, error)
}

// TrackerBirth is what Init asks of beads: a new database, in the vault
// directory the port was made for, whose story ids begin with the prefix.
type TrackerBirth interface {
	InitTracker(ctx context.Context, prefix string) error
}

// InitFirstCommit is the message of the vault's first commit. bd init stages its
// own files after that commit is made, and would make a commit of its own in a
// repository that had none, so the commit is made twice: the template, then the
// same commit again with the beads files in it.
const InitFirstCommit = "A fresh vault, from the template"

// Init makes a fresh vault: the template laid into a directory that has nothing
// in it, one first commit, a beads database with the prefix the Governor chose,
// and this host's config file if it has none.
//
// It checks everything it can before it writes anything, so a refusal leaves
// nothing behind. The config file is the exception to "write what was asked":
// it is this host's, it may hold what a person put there, and so Init writes it
// only when there is no file, and otherwise says what it would have written.
type Init struct {
	Vault    VaultBirth
	Tracker  TrackerBirth
	Template fs.FS
}

// InitRequest is one vault to make.
type InitRequest struct {
	// Dir is the vault's directory, a full path.
	Dir string
	// Prefix is what the ids of the vault's stories begin with.
	Prefix string
	// Host is what the config file calls this host.
	Host string
	// Rigs is where each rig is checked out on this host, by name, each a full path.
	Rigs map[string]string
	// ConfigPath is this host's config file.
	ConfigPath string
}

// InitReport is what Init did, and what is still owed.
type InitReport struct {
	Dir    string
	Prefix string

	ConfigPath string
	// ConfigWritten is whether the file was made. When it was not, ConfigText
	// is what Init would have written.
	ConfigWritten bool
	ConfigText    string
}

var validPrefix = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// Run makes the vault.
func (i Init) Run(ctx context.Context, req InitRequest) (InitReport, error) {
	if i.Vault == nil || i.Tracker == nil || i.Template == nil {
		return InitReport{}, fmt.Errorf("making a vault: there is no place to make it, no beads to make its database, or no template")
	}
	if err := req.validate(); err != nil {
		return InitReport{}, err
	}
	if err := i.Vault.Vacant(ctx, req.Dir); err != nil {
		return InitReport{}, err
	}

	if err := i.Vault.Lay(ctx, req.Dir, i.Template); err != nil {
		return InitReport{}, err
	}
	if err := i.Vault.Commit(ctx, req.Dir, InitFirstCommit); err != nil {
		return InitReport{}, fmt.Errorf("%w: the template is laid in %s but not committed; remove the directory and run mw init again", err, req.Dir)
	}
	if err := i.Tracker.InitTracker(ctx, req.Prefix); err != nil {
		return InitReport{}, fmt.Errorf("%w: the vault is at %s with its first commit but no beads database; remove the directory and run mw init again", err, req.Dir)
	}
	if err := i.Vault.Commit(ctx, req.Dir, InitFirstCommit); err != nil {
		return InitReport{}, fmt.Errorf("%w: the beads database is made in %s but not committed; remove the directory and run mw init again", err, req.Dir)
	}

	report := InitReport{Dir: req.Dir, Prefix: req.Prefix, ConfigPath: req.ConfigPath, ConfigText: req.configText()}
	wrote, err := i.Vault.WriteIfAbsent(ctx, req.ConfigPath, report.ConfigText)
	if err != nil {
		return report, fmt.Errorf("the vault is made, but %w", err)
	}
	report.ConfigWritten = wrote
	return report, nil
}

// validate refuses a request that could not make a vault, before anything is
// written.
func (r InitRequest) validate() error {
	switch {
	case strings.TrimSpace(r.Dir) == "":
		return fmt.Errorf("making a vault: it needs a directory")
	case !validPrefix.MatchString(r.Prefix):
		return fmt.Errorf("the prefix %q will not do: it starts with a letter and holds only letters, digits, - and _", r.Prefix)
	case strings.TrimSpace(r.Host) == "":
		return fmt.Errorf("making a vault: it needs the name of this host")
	case strings.TrimSpace(r.ConfigPath) == "":
		return fmt.Errorf("making a vault: it needs to know where this host's config file is")
	}
	for name, dir := range map[string]string{"the vault": r.Dir, "the host": r.Host} {
		if strings.ContainsAny(dir, "\"\n") {
			return fmt.Errorf("%s is %q, which cannot be written into a config file", name, dir)
		}
	}
	for name, dir := range r.Rigs {
		if name == "" || dir == "" || strings.ContainsAny(name+dir, "\"\n=") {
			return fmt.Errorf("the rig %q at %q cannot be written into a config file", name, dir)
		}
	}
	return nil
}

// configText is the config file a fresh host starts with: the vault, this
// host's name, one session at a time, and the rigs it was told of.
func (r InitRequest) configText() string {
	var text strings.Builder
	fmt.Fprintf(&text, "vault = %q\nhost  = %q\ncap   = 1\n\n[rigs]\n", r.Dir, r.Host)

	names := make([]string, 0, len(r.Rigs))
	for name := range r.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&text, "%s = %q\n", name, r.Rigs[name])
	}
	return text.String()
}

// String says what was made and what is owed next: the vault is private, so it
// has no remote until someone makes one, and this host's timers are not linked
// until scripts/install-units.sh is run.
func (r InitReport) String() string {
	var out strings.Builder
	fmt.Fprintf(&out, "Made the vault at %s: the template, one commit, a beads database with the prefix %s.\n", r.Dir, r.Prefix)

	if r.ConfigWritten {
		fmt.Fprintf(&out, "Wrote %s.\n", r.ConfigPath)
	} else {
		fmt.Fprintf(&out, "%s is already there and was left as it is. It would have said:\n\n%s", r.ConfigPath, r.ConfigText)
	}

	fmt.Fprintf(&out, "\nStill owed:\n")
	fmt.Fprintf(&out, "  1. Make a private remote for the vault, then:\n")
	fmt.Fprintf(&out, "       git -C %s remote add origin <url>\n", r.Dir)
	fmt.Fprintf(&out, "       git -C %s push -u origin main\n", r.Dir)
	fmt.Fprintf(&out, "  2. In the millwright checkout: scripts/install-units.sh\n")
	return out.String()
}

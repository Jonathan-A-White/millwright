package application

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// VaultBirth is what Init asks of the disk and of git to bring a vault onto
// this host: laid fresh from the template with Lay and Commit, or cloned
// whole from one that already exists with Clone. Vacant and WriteIfAbsent are
// common to both: nothing is written until the directory is known to be free,
// and the config file at the end is never overwritten.
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

	// Clone makes dir a git clone of url: the whole of a vault that already
	// exists somewhere else, history and all, exactly as a person's own git
	// clone would bring it. Nothing here is migrated, rewritten or forced.
	Clone(ctx context.Context, url, dir string) error

	// RestoreBootstrapNewline undoes the one harmless side effect bd bootstrap
	// leaves behind: bd 1.3.0 rewrites .beads/config.yaml and drops its
	// trailing newline, which is not a person's work and should never block
	// this host's first sync. When dir's .beads/config.yaml differs from what
	// is committed by nothing but that trailing newline, it is restored from
	// the index and RestoreBootstrapNewline reports true. A .beads/config.yaml
	// that is unchanged, or changed some other way, is left exactly as it is,
	// and it reports false: anything more than the newline is somebody's work
	// to commit, same as today.
	RestoreBootstrapNewline(ctx context.Context, dir string) (bool, error)

	// WriteIfAbsent writes text to path, creating its directories, unless a file
	// is already there; it reports whether it wrote. A file that is there is not
	// read, not merged and not touched.
	WriteIfAbsent(ctx context.Context, path, text string) (bool, error)
}

// TrackerBirth is what Init asks of beads: a new database with InitTracker,
// whose story ids begin with the prefix, or the one already in a cloned vault
// picked up rather than replaced with BootstrapTracker.
type TrackerBirth interface {
	InitTracker(ctx context.Context, prefix string) error

	// BootstrapTracker picks up the beads database already in the vault this
	// host just cloned — bd bootstrap, never bd init and never bd migrate: a
	// host joining a vault is never its designated migrator.
	BootstrapTracker(ctx context.Context) error
}

// InitFirstCommit is the message of the vault's first commit. bd init needs a
// repository to work in, and may or may not commit its own files there (it does
// on some hosts, only stages them on others), so the commit is made twice: the
// template, then once more with the beads files in it, whatever bd init
// committed folded back into that one first commit.
const InitFirstCommit = "A fresh vault, from the template"

// Init makes a fresh vault, or brings this host onto one that already
// exists: the template laid into a directory that has nothing in it, one
// first commit, a beads database with the prefix the Governor chose, or that
// same directory a git clone of an existing vault with its database picked up
// rather than replaced — either way, this host's config file if it has none.
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

// InitRequest is one vault to make, or to join.
type InitRequest struct {
	// Dir is the vault's directory, a full path.
	Dir string
	// Prefix is what the ids of a fresh vault's stories begin with. Empty
	// when URL says this host is joining a vault that already has its own.
	Prefix string
	// URL is where an existing vault's git remote already lives, for a host
	// that is joining rather than making one. Empty for a fresh vault, and
	// refused together with Prefix: a joined vault already has its own.
	URL string
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
	// URL and Joined are set when this host joined a vault rather than
	// making one.
	URL    string
	Joined bool

	ConfigPath string
	// ConfigWritten is whether the file was made. When it was not, ConfigText
	// is what Init would have written.
	ConfigWritten bool
	ConfigText    string

	// RestoredBootstrapNewline is whether bd bootstrap's dropped trailing
	// newline in .beads/config.yaml was restored. Set only on a join.
	RestoredBootstrapNewline bool
}

var validPrefix = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// Run makes the vault, or joins one that already exists when req.URL says so.
func (i Init) Run(ctx context.Context, req InitRequest) (InitReport, error) {
	if i.Vault == nil || i.Tracker == nil {
		return InitReport{}, fmt.Errorf("making a vault: there is no place to make it, or no beads to make its database")
	}
	if err := req.validate(); err != nil {
		return InitReport{}, err
	}
	if req.URL != "" {
		return i.runJoin(ctx, req)
	}
	if i.Template == nil {
		return InitReport{}, fmt.Errorf("making a vault: there is no template to lay")
	}
	return i.runFresh(ctx, req)
}

// runFresh lays the template into req.Dir, commits it, and makes a beads
// database with req.Prefix.
func (i Init) runFresh(ctx context.Context, req InitRequest) (InitReport, error) {
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

// runJoin clones req.URL whole into req.Dir and picks up the beads database
// already in it, rather than laying the template or making a fresh one: this
// host is never the vault's designated migrator by running it.
func (i Init) runJoin(ctx context.Context, req InitRequest) (InitReport, error) {
	if err := i.Vault.Vacant(ctx, req.Dir); err != nil {
		return InitReport{}, err
	}
	if err := i.Vault.Clone(ctx, req.URL, req.Dir); err != nil {
		return InitReport{}, err
	}
	if err := i.Tracker.BootstrapTracker(ctx); err != nil {
		return InitReport{}, fmt.Errorf("%w: the vault is cloned into %s but its database is not picked up; remove the directory and run mw init --join again", err, req.Dir)
	}
	restored, err := i.Vault.RestoreBootstrapNewline(ctx, req.Dir)
	if err != nil {
		return InitReport{}, fmt.Errorf("%w: the vault is cloned into %s and its database is picked up, but .beads/config.yaml could not be checked for bd bootstrap's dropped trailing newline", err, req.Dir)
	}

	report := InitReport{Dir: req.Dir, URL: req.URL, Joined: true, RestoredBootstrapNewline: restored, ConfigPath: req.ConfigPath, ConfigText: req.configText()}
	wrote, err := i.Vault.WriteIfAbsent(ctx, req.ConfigPath, report.ConfigText)
	if err != nil {
		return report, fmt.Errorf("the vault is joined, but %w", err)
	}
	report.ConfigWritten = wrote
	return report, nil
}

// validate refuses a request that could not make or join a vault, before
// anything is written.
func (r InitRequest) validate() error {
	switch {
	case strings.TrimSpace(r.Dir) == "":
		return fmt.Errorf("making a vault: it needs a directory")
	case r.Prefix != "" && r.URL != "":
		return fmt.Errorf("mw init takes --prefix or --join, not both: a joined vault already has its own beads database")
	case r.URL == "" && !validPrefix.MatchString(r.Prefix):
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

// String says what was made or joined and what is owed next. A fresh vault is
// private, so it has no remote until someone makes one; a joined vault is a
// MOVE only in part, and says what the rest still needs.
func (r InitReport) String() string {
	var out strings.Builder
	if r.Joined {
		fmt.Fprintf(&out, "Joined the vault at %s, cloned from %s: bd bootstrap picked up its database.\n", r.Dir, r.URL)
		if r.RestoredBootstrapNewline {
			fmt.Fprintf(&out, "bd bootstrap dropped .beads/config.yaml's trailing newline; restored it, so the first sync is not blocked by it.\n")
		}
	} else {
		fmt.Fprintf(&out, "Made the vault at %s: the template, one commit, a beads database with the prefix %s.\n", r.Dir, r.Prefix)
	}

	if r.ConfigWritten {
		fmt.Fprintf(&out, "Wrote %s.\n", r.ConfigPath)
	} else {
		fmt.Fprintf(&out, "%s is already there and was left as it is. It would have said:\n\n%s", r.ConfigPath, r.ConfigText)
	}

	fmt.Fprintf(&out, "\nStill owed:\n")
	if r.Joined {
		fmt.Fprintf(&out, "  1. The designated-migrator note in the vault's CLAUDE.md, if it is not there already.\n")
		fmt.Fprintf(&out, "  2. In the millwright checkout: scripts/install-units.sh\n")
		fmt.Fprintf(&out, "  3. The old host's timers turned off first, so two hosts never dispatch as one name.\n")
	} else {
		fmt.Fprintf(&out, "  1. Make a private remote for the vault, then:\n")
		fmt.Fprintf(&out, "       git -C %s remote add origin <url>\n", r.Dir)
		fmt.Fprintf(&out, "       git -C %s push -u origin main\n", r.Dir)
		fmt.Fprintf(&out, "  2. In the millwright checkout: scripts/install-units.sh\n")
	}
	return out.String()
}

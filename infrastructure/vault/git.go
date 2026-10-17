package vault

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
	"github.com/Jonathan-A-White/millwright/infrastructure/netfault"
)

// Git is the command this adapter keeps the vault level with, and
// AttributesFile is where git is told how to merge a ledger.
const (
	Git            = "git"
	AttributesFile = application.LedgerAttributesFile
)

// marking is the comment written above the ledger mark when the vault has no
// .gitattributes yet, so that whoever reads it next knows why it is there.
const marking = "# Ledgers are appended to by both hosts and edited by neither,\n" +
	"# so two hosts' appends merge by keeping every line rather than conflicting.\n"

// The vault's files are kept level with the other host's by plain git: the
// other host's commits are pulled and this host's are replayed on top, then
// what this host has and the other one does not is pushed. Nothing is forced,
// nothing is committed on a seat's behalf, and a rebase that stops on a
// conflict is undone before the reason comes back.
var _ application.VaultFiles = (*Vault)(nil)

// MarkLedgers implements application.VaultFiles: it makes sure the vault's
// .gitattributes carries application.LedgerMark, and reports whether it had to
// write it.
//
// The mark matters before a pull, not after: git reads it out of the working
// tree while it merges. This adapter only writes it; application.Sync commits it
// in the same step (commitWritten), so that the clone is not left with a tracked
// change its next sync refuses on (mw-gq6.530).
func (v *Vault) MarkLedgers(_ context.Context) (bool, error) {
	path := filepath.Join(v.dir, AttributesFile)
	attributes, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	if marked(string(attributes)) {
		return false, nil
	}

	var write strings.Builder
	if len(attributes) > 0 {
		write.Write(attributes)
		if !bytes.HasSuffix(attributes, []byte("\n")) {
			write.WriteString("\n")
		}
		write.WriteString("\n")
	}
	write.WriteString(marking)
	write.WriteString(application.LedgerMark + "\n")

	if err := os.WriteFile(path, []byte(write.String()), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// marked reports whether a .gitattributes already merges the vault's ledgers by
// keeping every line. Comments and the order of the attributes after the
// pattern are none of this adapter's business: what matters is that the ledger
// pattern carries merge=union.
func marked(attributes string) bool {
	for _, line := range strings.Split(attributes, "\n") {
		if comment := strings.Index(line, "#"); comment >= 0 {
			line = line[:comment]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != application.LedgerPattern {
			continue
		}
		for _, attribute := range fields[1:] {
			if attribute == application.MergeUnion {
				return true
			}
		}
	}
	return false
}

// Uncommitted implements application.VaultFiles. Only tracked files are
// listed: a run directory or a draft nobody has added is in nobody's way, but a
// changed ledger stops a rebase dead.
func (v *Vault) Uncommitted(ctx context.Context) ([]string, error) {
	out, err := v.git(ctx, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return nil, err
	}

	return changedPaths(out), nil
}

// changedPaths reads the paths out of git status --porcelain.
func changedPaths(out string) []string {
	var changed []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain v1: two status characters, a space, then the path, and for
		// a rename "old -> new". The new name is the one to commit.
		path := strings.TrimSpace(line[3:])
		if _, to, renamed := strings.Cut(path, " -> "); renamed {
			path = to
		}
		changed = append(changed, strings.Trim(path, `"`))
	}
	return changed
}

// Commit implements application.VaultFiles: it records exactly the paths it is
// given, and nothing else the vault holds.
//
// Three git commands, in this order: `status --porcelain --untracked-files=all
// -- <paths>` to see which of them there is anything to commit in, `add --
// <those>` so that a file git has never seen is committable, and `commit -m
// <message> -- <those>`. The pathspec on the commit is what makes this safe in
// a vault two hosts and several seats write to: a commit with a pathspec takes
// the working tree's version of those paths and nothing else, whatever else is
// changed or even staged. Nothing is added with -A, nothing is committed with
// -a, and a path nobody touched is not committed at all. When the Vault has an
// author (WithAuthor) the commit alone carries it, as -c user.name and -c
// user.email.
func (v *Vault) Commit(ctx context.Context, message string, paths []string) ([]string, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("committing in %s: a commit needs a message", v.dir)
	}
	for _, path := range paths {
		if err := insideTheVault(path); err != nil {
			return nil, fmt.Errorf("committing in %s: %w", v.dir, err)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	out, err := v.git(ctx, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	changed := changedPaths(out)
	if len(changed) == 0 {
		return nil, nil
	}

	if _, err := v.git(ctx, append([]string{"add", "--"}, changed...)...); err != nil {
		return nil, err
	}
	commit := append([]string{"commit", "-m", message, "--"}, changed...)
	if v.author != "" {
		commit = append([]string{"-c", "user.name=" + v.author, "-c", "user.email=" + v.author}, commit...)
	}
	if _, err := v.git(ctx, commit...); err != nil {
		return nil, err
	}
	return changed, nil
}

// insideTheVault refuses a path that names something the vault does not hold.
// Every path a commit is given comes from a seat name and a rig name, and both
// come from a bead or a config file rather than from this package.
func insideTheVault(path string) error {
	switch {
	case strings.TrimSpace(path) == "":
		return fmt.Errorf("a path to commit cannot be empty")
	case filepath.IsAbs(path), strings.HasPrefix(path, "/"):
		return fmt.Errorf("%q is not a path in the vault: it is absolute", path)
	case strings.HasPrefix(path, "-"):
		return fmt.Errorf("%q is not a path in the vault: it reads as an option", path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return fmt.Errorf("%q is not a path in the vault: it reaches outside it", path)
		}
	}
	return nil
}

// Pull implements application.VaultFiles: git pull --rebase, and the count of
// the other host's commits that came in. A rebase that stops on a conflict is
// aborted, so that the vault is left exactly as it was found and a person can
// look at it without first undoing a half-finished merge.
//
// Every pull defines mw's trim-safe merge driver, for the files the vault's
// .gitattributes names merge=trimsafe (its rig memories): a prune synced while
// the other host appended keeps the prune and the appended line (mw-gq6.499).
func (v *Vault) Pull(ctx context.Context) (int, error) {
	before, err := v.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return 0, err
	}
	if _, err := v.upstream(ctx); err != nil {
		return 0, err
	}
	driver, err := gitcmd.WriteMergeDriver(v.mergeScript)
	if err != nil {
		return 0, err
	}
	defer driver.Remove()

	// The ledger mark is committed before the pull (mw-gq6.530), so a commit this
	// host made earlier is replayed onto a tree that may not carry it yet: the
	// mark is also given to git as its global attributes, which a mark in the
	// tree's own .gitattributes overrides.
	attributes, err := os.CreateTemp("", "mw-ledger-attributes-*")
	if err != nil {
		return 0, fmt.Errorf("writing the ledger mark for the pull: %w", err)
	}
	defer os.Remove(attributes.Name())
	_, err = attributes.WriteString(application.LedgerMark + "\n")
	if closed := attributes.Close(); err == nil {
		err = closed
	}
	if err != nil {
		return 0, fmt.Errorf("writing the ledger mark for the pull: %w", err)
	}
	pull := append(driver.Config(), "-c", "core.attributesFile="+attributes.Name(), "pull", "--rebase")

	_, err = v.git(ctx, pull...)
	if err != nil && closeOutIsWriting(err) {
		// mw next's close-out writes a run's files into this checkout and
		// commits them a moment later; a pull that lands between the two finds
		// them untracked, in the way of the same files from the other host.
		// Nothing was changed, and the commit is moments behind: wait, and try
		// once more.
		if waited := v.waitForCloseOut(ctx); waited != nil {
			return 0, waited
		}
		_, err = v.git(ctx, pull...)
	}
	if err != nil {
		err = v.namingTheDriver(ctx, err)
		if _, aborted := v.git(ctx, "rebase", "--abort"); aborted == nil {
			return 0, fmt.Errorf("%w (the rebase was undone: the vault is as it was)", err)
		}
		return 0, err
	}
	return v.commitsBetween(ctx, strings.TrimSpace(before), "@{upstream}")
}

// namingTheDriver is a stopped pull's err, saying so when a file it stopped in
// is one mw's trim-safe driver merges: the driver could not settle it, and
// nothing settles it by union instead.
func (v *Vault) namingTheDriver(ctx context.Context, err error) error {
	said, listed := v.git(ctx, "diff", "--name-only", "--diff-filter=U", "-z")
	if listed != nil {
		return err
	}
	var driven []string
	value := ""
	for _, file := range strings.Split(said, "\x00") {
		if file == "" {
			continue
		}
		attr, checked := v.git(ctx, "check-attr", "merge", "--", file)
		if checked != nil {
			continue
		}
		if _, merge, _ := strings.Cut(strings.TrimSpace(attr), ": merge: "); gitcmd.IsTrimMerge(merge) {
			driven, value = append(driven, file), merge
		}
	}
	if len(driven) == 0 {
		return err
	}
	return fmt.Errorf("%w (%s merged by mw's trim-safe driver, merge=%s, which could not settle it; union would bring back every line a trim took out)",
		err, strings.Join(driven, ", "), value)
}

// closeOutWait is how long a pull waits for a close-out's commit before its one
// retry.
const closeOutWait = 3 * time.Second

// untrackedInTheWay is the words git refuses a pull in when files in the working
// tree that it does not track are the same paths as files coming in.
const untrackedInTheWay = "untracked working tree files would be overwritten"

// closeOutIsWriting reports whether git refused a pull for no more than files
// in the way that a close-out is about to commit. Every other refusal is not it.
func closeOutIsWriting(err error) bool {
	return strings.Contains(err.Error(), untrackedInTheWay)
}

func (v *Vault) waitForCloseOut(ctx context.Context) error {
	if v.wait != nil {
		return v.wait(ctx, closeOutWait)
	}
	timer := time.NewTimer(closeOutWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("waiting %s for the close-out to commit: %w", closeOutWait, ctx.Err())
	case <-timer.C:
		return nil
	}
}

// Push implements application.VaultFiles. A clone with nothing to push does not
// reach the remote at all, so a sync with nothing to do costs one fetch and no
// network write.
func (v *Vault) Push(ctx context.Context) (int, error) {
	outgoing, err := v.commitsBetween(ctx, "@{upstream}", "HEAD")
	if err != nil {
		return 0, err
	}
	if outgoing == 0 {
		return 0, nil
	}
	if _, err := v.git(ctx, "push"); err != nil {
		return 0, err
	}
	return outgoing, nil
}

// Head implements application.VaultFiles: the commit this clone has checked
// out right now.
func (v *Vault) Head(ctx context.Context) (string, error) {
	out, err := v.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Branch implements application.VaultFiles: the name of the branch this clone
// has checked out.
func (v *Vault) Branch(ctx context.Context) (string, error) {
	out, err := v.git(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// HeadSubject implements application.VaultFiles: the first line of the message
// of the commit this clone has checked out.
func (v *Vault) HeadSubject(ctx context.Context) (string, error) {
	out, err := v.git(ctx, "log", "-1", "--format=%s", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SignOfLifeCommit is what a host's newest commit in the vault is called in the
// quiet alarm's line.
const SignOfLifeCommit = "vault commit"

// LastSign implements application.HostSigns: the newest commit in this clone
// authored as mw@<host>, which is what a host's close-out, send-back and mail
// leave behind in the vault. A host with none is the zero sign.
func (v *Vault) LastSign(ctx context.Context, host string) (application.HostSign, error) {
	if strings.TrimSpace(host) == "" {
		return application.HostSign{}, nil
	}
	// The author line reads "mw@<host> <mw@<host>>": the angle brackets make
	// the match whole, so that mw@cloud does not answer for mw@cloud2.
	out, err := v.git(ctx, "log", "-1", "--fixed-strings", "--author=<mw@"+host+">", "--format=%cI")
	if err != nil {
		return application.HostSign{}, err
	}
	said := strings.TrimSpace(out)
	if said == "" {
		return application.HostSign{}, nil
	}
	at, err := time.Parse(time.RFC3339, said)
	if err != nil {
		return application.HostSign{}, fmt.Errorf("reading when mw@%s last committed in %s: %w", host, v.dir, err)
	}
	return application.HostSign{At: at, What: SignOfLifeCommit}, nil
}

// upstream is the branch this clone tracks, and the reason there is none: a
// vault whose branch tracks nothing has no other host to be level with.
func (v *Vault) upstream(ctx context.Context) (string, error) {
	out, err := v.git(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", fmt.Errorf("the vault's branch tracks no remote branch, so there is nothing to sync with: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// commitsBetween counts the commits reachable from to and not from from.
func (v *Vault) commitsBetween(ctx context.Context, from, to string) (int, error) {
	out, err := v.git(ctx, "rev-list", "--count", from+".."+to)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("counting the commits in %s..%s: %w", from, to, err)
	}
	return count, nil
}

// git runs one git command in the vault and returns its standard output. It
// never prompts: a sync may run on a timer with nobody at the keyboard, and a
// command waiting for a password would hang there until someone noticed. Nor
// does it hang on a stalled connection: a fetch or push transferring under
// lowSpeedLimit bytes/sec for lowSpeedTime seconds is git's own failure to
// report, not a wait for mw to hold.
//
// It runs in a process group of its own, so that a context that ends — mw
// told to stop by a signal, or a dispatch tick giving up — takes the whole
// group with it, rather than leaving a fetch running behind mw's back for the
// next sync to trip over.
func (v *Vault) git(ctx context.Context, args ...string) (string, error) {
	cmd := gitcmd.Command(ctx, Git, v.dir, args...)

	var out, errs bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errs

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			// The context ended, not git: this is mw stopping, not git failing.
			return "", fmt.Errorf("%s %s in %s: stopped: %w", Git, strings.Join(args, " "), v.dir, ctx.Err())
		}
		said := strings.TrimSpace(errs.String() + "\n" + out.String())
		if said == "" {
			return "", fmt.Errorf("%s %s in %s: %w", Git, strings.Join(args, " "), v.dir, err)
		}
		failed := fmt.Errorf("%s %s in %s: %w: %s", Git, strings.Join(args, " "), v.dir, err, said)
		if line, unresolved := netfault.NameNotResolved(said); unresolved {
			return "", &application.NameNotResolved{Said: line, Err: failed}
		}
		return "", failed
	}
	return out.String(), nil
}

// Package hostsweep is the host's disk as mw host sweep reads and clears it:
// git's story worktrees and branches, the landing directories, the vault's runs
// and the directories a home move set aside.
package hostsweep

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// Disk is the real application.HostSweepFiles.
type Disk struct {
	// Rigs are the rigs' checkouts by name, as config.Rigs reads them.
	Rigs map[string]string
	// Vault is the vault's directory; its runs/ is the runs.
	Vault string
	// Home is where the landing directories and the set-aside beads are.
	Home string
	// Program is the git command; empty reads "git".
	Program string
	// ProcDir is where processes are read from; empty reads /proc.
	ProcDir string
}

var _ application.HostSweepFiles = Disk{}

// cacheNames are the directories inside a worktree that a build makes again:
// the packages and the Playwright browsers.
var cacheNames = map[string]bool{"node_modules": true, "ms-playwright": true}

// mainRefs are the branches a commit may be on to be on the rig's main.
var mainRefs = []string{"refs/remotes/origin/main", "refs/heads/main", "refs/remotes/origin/master", "refs/heads/master"}

// attemptSuffix is what mw keeps a branch under when a retry sets it aside:
// mw/<story>-attempt<N>, with -<n> added when that name was taken.
var attemptSuffix = regexp.MustCompile(`-attempt[0-9]+(-[0-9]+)?$`)

// storyOfBranch is the story a branch cut as mw/<story>, or set aside by a
// retry as mw/<story>-attempt<N>, belongs to.
func storyOfBranch(branch string) string {
	return attemptSuffix.ReplaceAllString(strings.TrimPrefix(branch, application.BranchPrefix), "")
}

func (d Disk) program() string {
	if d.Program != "" {
		return d.Program
	}
	return "git"
}

func (d Disk) git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitcmd.Command(ctx, d.program(), dir, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// rigDirs are the rigs' checkouts, sorted by name.
func (d Disk) rigDirs() []string {
	var dirs []string
	for _, dir := range d.Rigs {
		dirs = append(dirs, filepath.Clean(dir))
	}
	sort.Strings(dirs)
	return dirs
}

// Trees implements application.HostSweepFiles.
func (d Disk) Trees(ctx context.Context) ([]application.HostSweepTree, error) {
	byStory := map[string]*application.HostSweepTree{}
	var order []string
	add := func(tree application.HostSweepTree) {
		key := tree.RigDir + "\x00" + tree.Story + "\x00" + tree.Branch
		if _, seen := byStory[key]; seen {
			return
		}
		held := tree
		byStory[key] = &held
		order = append(order, key)
	}

	// The worktrees: each directory of a .mw-worktrees beside a rig is a story's.
	seenDirs := map[string]bool{}
	dirStories := map[string]bool{}
	for _, rigDir := range d.rigDirs() {
		parent := filepath.Join(filepath.Dir(rigDir), application.WorktreesDir)
		if seenDirs[parent] {
			continue
		}
		seenDirs[parent] = true
		entries, err := os.ReadDir(parent)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(parent, entry.Name())
			// The rig's merge-slot directories live here too: not a worktree, not the sweep's.
			if _, err := os.Lstat(filepath.Join(dir, ".git")); err != nil {
				continue
			}
			owner := d.rigOf(ctx, dir, rigDir)
			add(d.look(ctx, application.HostSweepTree{
				Story: entry.Name(), Rig: d.nameOf(owner), RigDir: owner, Dir: dir,
				Branch: d.branchOf(ctx, dir),
			}))
		}
	}

	// The branches of stories with no worktree left: a branch cut as mw/<story>,
	// or set aside by a retry as mw/<story>-attempt<N>, which is judged by the
	// story it was cut for.
	for _, rigDir := range d.rigDirs() {
		out, err := d.git(ctx, rigDir, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+application.BranchPrefix)
		if err != nil {
			continue
		}
		for _, branch := range strings.Fields(out) {
			story := storyOfBranch(branch)
			if story == strings.TrimPrefix(branch, application.BranchPrefix) && dirStories[rigDir+"\x00"+story] {
				continue
			}
			add(d.look(ctx, application.HostSweepTree{Story: story, Rig: d.nameOf(rigDir), RigDir: rigDir, Branch: branch}))
		}
	}

	trees := make([]application.HostSweepTree, 0, len(order))
	for _, key := range order {
		trees = append(trees, *byStory[key])
	}
	return trees, nil
}

// rigOf is the rig checkout the worktree dir belongs to, or fallback when git
// cannot say.
func (d Disk) rigOf(ctx context.Context, dir, fallback string) string {
	common, err := d.git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return fallback
	}
	owner := filepath.Dir(filepath.Clean(common))
	for _, rigDir := range d.rigDirs() {
		if rigDir == owner {
			return owner
		}
	}
	return fallback
}

func (d Disk) nameOf(rigDir string) string {
	for name, dir := range d.Rigs {
		if filepath.Clean(dir) == rigDir {
			return name
		}
	}
	return ""
}

// branchOf is the branch a worktree has checked out; empty when it has none.
func (d Disk) branchOf(ctx context.Context, dir string) string {
	out, err := d.git(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// look fills in what keeps the tree: what runs in it, what it has not
// committed, the commits that are on no main and pushed nowhere, and its
// caches.
func (d Disk) look(ctx context.Context, tree application.HostSweepTree) application.HostSweepTree {
	if tree.Dir != "" {
		if pids := (doctor.LandingTrees{ProcDir: d.ProcDir}).RunningIn(tree.Dir); len(pids) > 0 {
			tree.Busy = "a process is running in it (pid " + strings.Join(pids, ", ") + ")"
		}
		// A directory git cannot read as a worktree is kept as if it had changes.
		if status, err := d.git(ctx, tree.Dir, "status", "--porcelain"); err != nil || status != "" {
			tree.Uncommitted = true
		}
		tree.Bytes, _ = measure(tree.Dir)
		tree.Caches = caches(tree.Dir)
	}
	tree.Unsaved = d.unsaved(ctx, tree)
	return tree
}

// unsaved says why the tree's commits are in danger: some are neither on main
// nor pushed to the branch's own remote ref. Empty when none is.
func (d Disk) unsaved(ctx context.Context, tree application.HostSweepTree) string {
	rigDir := tree.RigDir
	have := func(ref string) bool {
		_, err := d.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", ref)
		return err == nil
	}
	var tips []string
	if tree.Branch != "" && have("refs/heads/"+tree.Branch) {
		tips = append(tips, "refs/heads/"+tree.Branch)
	}
	if tree.Dir != "" {
		if head, err := d.git(ctx, tree.Dir, "rev-parse", "--verify", "HEAD^{commit}"); err == nil {
			tips = append(tips, head)
		}
	}
	if len(tips) == 0 {
		return ""
	}
	args := append([]string{"rev-list", "--count"}, tips...)
	args = append(args, "--not")
	for _, ref := range mainRefs {
		if have(ref) {
			args = append(args, ref)
		}
	}
	if tree.Branch != "" && have("refs/remotes/origin/"+tree.Branch) {
		args = append(args, "refs/remotes/origin/"+tree.Branch)
	}
	if len(args) == len(tips)+3 {
		return "no main to judge its commits by"
	}
	out, err := d.git(ctx, rigDir, args...)
	if err != nil {
		return "git cannot count its commits: " + strings.Join(strings.Fields(out), " ")
	}
	n, _ := strconv.Atoi(out)
	if n == 0 {
		return ""
	}
	// A landing may have written the commits again under new hashes (a rebase, a
	// re-worded subject): a commit on main that names the story is its landing.
	if d.landedByName(ctx, rigDir, tree.Story, have) {
		return ""
	}
	name := tree.Branch
	if name == "" {
		name = "its HEAD"
	}
	if n == 1 {
		return fmt.Sprintf("1 commit on %s is neither on main nor pushed", name)
	}
	return fmt.Sprintf("%d commits on %s are neither on main nor pushed", n, name)
}

// landedByName is true when a commit on the rig's main names the story, as a
// landing writes it ("(<story id>)" in the subject). The id must stand alone:
// mw-x-1 is not named by a commit that names mw-x-12, nor mw-x by mw-x.1. A
// story with no id names nothing.
func (d Disk) landedByName(ctx context.Context, rigDir, story string, have func(string) bool) bool {
	if story == "" {
		return false
	}
	args := []string{"log", "-1", "--format=%H", "-E",
		"--grep=(^|[^[:alnum:]_.-])" + regexp.QuoteMeta(story) + `($|[^[:alnum:]_.-]|\.($|[^[:alnum:]_-]))`}
	refs := 0
	for _, ref := range mainRefs {
		if have(ref) {
			args = append(args, ref)
			refs++
		}
	}
	if refs == 0 {
		return false
	}
	out, err := d.git(ctx, rigDir, args...)
	return err == nil && out != ""
}

// caches finds the node_modules and Playwright directories of a worktree, at
// most four levels down and never inside one another.
func caches(root string) []application.HostSweepDir {
	var found []application.HostSweepDir
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		if entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if cacheNames[entry.Name()] {
			bytes, newest := measure(path)
			found = append(found, application.HostSweepDir{Path: path, Modified: newest, Bytes: bytes})
			return filepath.SkipDir
		}
		if rel, relErr := filepath.Rel(root, path); relErr == nil && strings.Count(rel, string(filepath.Separator)) >= 4 {
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

// measure is the size of the files under dir and the newest time any of them,
// or dir, was written.
func measure(dir string) (int64, time.Time) {
	var total int64
	var newest time.Time
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, newest
}

// Landings implements application.HostSweepFiles: the .mw-landing-* directories
// in the home directory and beside every rig, each judged by the doctor's own
// rule for a spare landing.
func (d Disk) Landings(ctx context.Context) ([]application.HostSweepDir, error) {
	dirs := []string{}
	if d.Home != "" {
		dirs = append(dirs, d.Home)
	}
	for _, rigDir := range d.rigDirs() {
		dirs = append(dirs, filepath.Dir(rigDir))
	}
	trees := doctor.LandingTrees{Program: d.Program, ProcDir: d.ProcDir}
	var found []application.HostSweepDir
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), application.LandingPrefix) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil || seen[path] {
				continue
			}
			seen[path] = true
			landing := application.HostSweepDir{Path: path, Modified: info.ModTime()}
			landing.Bytes, _ = measure(path)
			if spare, why := trees.Spare(ctx, path); !spare {
				landing.Live = why
			}
			found = append(found, landing)
		}
	}
	return found, nil
}

// Runs implements application.HostSweepFiles: each directory of the vault's
// runs/, its time the newest file in it.
func (d Disk) Runs(ctx context.Context) ([]application.HostSweepDir, error) {
	if d.Vault == "" {
		return nil, nil
	}
	runs, err := childDirs(filepath.Join(d.Vault, application.RunsDir), func(string) bool { return true })
	if err != nil || len(runs) == 0 {
		return runs, err
	}
	// The vault's runs/ is tracked in git: a directory with a tracked file is
	// the vault's sync's. When git cannot say, every run is taken as tracked.
	out, gitErr := d.git(ctx, d.Vault, "ls-files", "-z", "--", application.RunsDir)
	tracked := map[string]bool{}
	for _, file := range strings.Split(out, "\x00") {
		file = strings.TrimSpace(file)
		if rest, ok := strings.CutPrefix(file, application.RunsDir+"/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			tracked[name] = true
		}
	}
	for i := range runs {
		runs[i].Tracked = gitErr != nil || tracked[filepath.Base(runs[i].Path)]
	}
	return runs, nil
}

// Asides implements application.HostSweepFiles: the beads-*-aside-* directories
// of the home directory, which mw home move sets aside and never deletes.
func (d Disk) Asides(_ context.Context) ([]application.HostSweepDir, error) {
	if d.Home == "" {
		return nil, nil
	}
	return childDirs(d.Home, func(name string) bool {
		return strings.HasPrefix(name, "beads-") && strings.Contains(name, "-aside-")
	})
}

func childDirs(parent string, wanted func(name string) bool) ([]application.HostSweepDir, error) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var found []application.HostSweepDir
	for _, entry := range entries {
		if !entry.IsDir() || !wanted(entry.Name()) {
			continue
		}
		path := filepath.Join(parent, entry.Name())
		bytes, newest := measure(path)
		found = append(found, application.HostSweepDir{Path: path, Modified: newest, Bytes: bytes})
	}
	return found, nil
}

// RemoveTree implements application.HostSweepFiles: git takes the worktree away
// without force, so that it refuses a change it would lose, and the branch goes
// after it.
func (d Disk) RemoveTree(ctx context.Context, tree application.HostSweepTree) error {
	var options []rig.Option
	if d.Program != "" {
		options = append(options, rig.WithProgram(d.Program))
	}
	worktrees := rig.New(options...)
	if err := worktrees.RemoveWithoutForce(ctx, tree.RigDir, tree.Dir); err != nil {
		return err
	}
	if tree.Dir != "" {
		if _, err := os.Stat(tree.Dir); err == nil {
			return fmt.Errorf("git left %s in place", tree.Dir)
		}
	}
	return worktrees.DeleteBranch(ctx, tree.RigDir, tree.Branch)
}

// RemoveDir implements application.HostSweepFiles. It takes only a path that
// is absolute, and not a root.
func (d Disk) RemoveDir(_ context.Context, path string) error {
	if !filepath.IsAbs(path) || filepath.Dir(filepath.Clean(path)) == filepath.Clean(path) {
		return fmt.Errorf("%q is not a directory to remove", path)
	}
	return os.RemoveAll(path)
}

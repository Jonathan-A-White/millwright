package rig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/gitcmd"
)

// A worktree is taken away only once nothing is running in it. A Builder's
// session can end (its turns capped) while a test run it started in the
// background goes on writing inside the worktree; git then cannot delete the
// directory ("Directory not empty"), leaves a husk behind, and every later
// attempt of the story is refused (mw-5r3p30.882, mw-gq6.876). So before a
// worktree goes, the processes that hold it are ended, and a removal git
// refuses for the files it was still being written to is tried once more.

// writersWait is how long endWriters keeps ending what it finds before it gives
// up on what will not die.
const writersWait = 3 * time.Second

// holder is a process found holding a worktree: its working directory or an
// open file is inside it.
type holder struct {
	pid     int
	command string
}

// String names the process for a message: its pid and the command line it was
// started with, cut short.
func (h holder) String() string {
	return fmt.Sprintf("pid %d (%s)", h.pid, h.command)
}

// holdersOf is every process, not this one nor one that started it, whose
// working directory or an open file lies inside dir. A host with no /proc, and
// a process of another user whose links cannot be read, are not found.
func holdersOf(dir string) []holder {
	root := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		root = resolved
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	mine := ancestry()
	var found []holder
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || mine[pid] {
			continue
		}
		if holds(pid, root) {
			found = append(found, holder{pid: pid, command: commandOf(pid)})
		}
	}
	return found
}

// ancestry is this process and every process above it: a session that runs mw
// from inside its worktree is not one to be ended by mw.
func ancestry() map[int]bool {
	mine := map[int]bool{}
	for pid := os.Getpid(); pid > 1 && !mine[pid]; {
		mine[pid] = true
		stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			break
		}
		// pid (comm) state ppid ...; comm may hold spaces and brackets.
		rest := string(stat)
		if i := strings.LastIndexByte(rest, ')'); i >= 0 {
			rest = rest[i+1:]
		}
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			break
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			break
		}
		pid = ppid
	}
	return mine
}

// holds reports whether the process's working directory or one of its open
// files is dir or inside it.
func holds(pid int, dir string) bool {
	base := "/proc/" + strconv.Itoa(pid)
	if inside(readlink(base+"/cwd"), dir) {
		return true
	}
	fds, err := os.ReadDir(base + "/fd")
	if err != nil {
		return false
	}
	for _, fd := range fds {
		if inside(readlink(base+"/fd/"+fd.Name()), dir) {
			return true
		}
	}
	return false
}

func readlink(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(target, " (deleted)")
}

// inside reports whether path is dir or lies under it.
func inside(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// commandOf is the command line pid was started with, cut at a hundred
// characters; its short name when the line cannot be read.
func commandOf(pid int) string {
	base := "/proc/" + strconv.Itoa(pid)
	line, _ := os.ReadFile(base + "/cmdline")
	text := strings.TrimSpace(strings.ReplaceAll(string(bytes.TrimRight(line, "\x00")), "\x00", " "))
	if text == "" {
		comm, _ := os.ReadFile(base + "/comm")
		text = strings.TrimSpace(string(comm))
	}
	if r := []rune(text); len(r) > 100 {
		text = string(r[:100]) + "..."
	}
	return text
}

// endWriters ends every process that holds dir, and those they start meanwhile,
// until none is left or writersWait is up. It returns each process it found.
func (w *Worktrees) endWriters(dir string) []holder {
	var seen []holder
	known := map[int]bool{}
	deadline := time.Now().Add(w.writersWait)
	for {
		now := holdersOf(dir)
		if len(now) == 0 {
			return seen
		}
		for _, h := range now {
			if !known[h.pid] {
				known[h.pid] = true
				seen = append(seen, h)
			}
			_ = w.endProcess(h.pid)
		}
		if time.Now().After(deadline) {
			return seen
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// withHolders adds to err the processes that held the worktree: those still
// there, and those that were ended.
func withHolders(err error, dir string, found []holder) error {
	if err == nil || len(found) == 0 {
		return err
	}
	still := map[int]bool{}
	for _, h := range holdersOf(dir) {
		still[h.pid] = true
	}
	var parts []string
	for _, h := range found {
		state := "ended"
		if still[h.pid] {
			state = "still running"
		}
		parts = append(parts, h.String()+" "+state)
	}
	return fmt.Errorf("%w; processes found holding %s: %s", err, dir, strings.Join(parts, ", "))
}

// takeAway is the removal Remove and RemoveWithoutForce share: the processes
// holding dir are ended, git is asked to remove the worktree, a "Directory not
// empty" is tried once more once the writers are gone again, and a directory git
// no longer knows as a worktree (its link dead, as an earlier removal that
// failed halfway leaves it) is taken away by hand (mw-gq6.876).
func (w *Worktrees) takeAway(ctx context.Context, rigDir, dir string, force bool) error {
	found := w.endWriters(dir)
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, dir)

	_, err := w.git(ctx, rigDir, args...)
	if err != nil && strings.Contains(err.Error(), "Directory not empty") {
		found = append(found, newHolders(found, w.endWriters(dir))...)
		_, err = w.git(ctx, rigDir, args...)
	}
	if err != nil && strings.Contains(err.Error(), "is not a working tree") && isHusk(dir) {
		err = w.removeHusk(ctx, rigDir, dir, force, &found)
	}
	return withHolders(err, dir, found)
}

// isHusk reports whether dir is what a worktree's failed removal leaves: its
// .git is a link to a git directory that is gone, or git has taken the link
// away too and the directory is in the factory's own worktrees directory. A
// directory that is neither is not mw's to take away.
func isHusk(dir string) bool {
	link, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return os.IsNotExist(err) && filepath.Base(filepath.Dir(filepath.Clean(dir))) == application.WorktreesDir
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(link)), "gitdir:")
	if !ok {
		return false
	}
	_, err = os.Stat(strings.TrimSpace(target))
	return os.IsNotExist(err)
}

// newHolders is the processes of now that earlier does not already hold.
func newHolders(earlier, now []holder) []holder {
	have := map[int]bool{}
	for _, h := range earlier {
		have[h.pid] = true
	}
	var more []holder
	for _, h := range now {
		if !have[h.pid] {
			more = append(more, h)
		}
	}
	return more
}

// removeHusk takes away by hand a directory git has lost its record of. A
// forced removal throws away whatever is in it, as it does a worktree git still
// knows; one without force first checks that every tracked file of the directory
// is as the story's branch has it, and refuses, naming them, otherwise.
func (w *Worktrees) removeHusk(ctx context.Context, rigDir, dir string, force bool, found *[]holder) error {
	if !force {
		if err := w.huskIsOnTheBranch(ctx, rigDir, dir); err != nil {
			return err
		}
	}
	err := os.RemoveAll(dir)
	if err != nil {
		*found = append(*found, newHolders(*found, w.endWriters(dir))...)
		err = os.RemoveAll(dir)
	}
	if err != nil {
		return fmt.Errorf("taking the husk of a worktree %s away by hand: %w", dir, err)
	}
	_, _ = w.git(ctx, rigDir, "worktree", "prune")
	return nil
}

// huskIsOnTheBranch reports, as an error naming them, the tracked files of dir
// that differ from the tip of the story's branch (the directory's name is the
// story's): a temporary index is read from the branch and the directory is
// compared with it, since the directory's own link to git is dead. Files git
// does not track, test results and installed packages among them, are not looked
// at.
func (w *Worktrees) huskIsOnTheBranch(ctx context.Context, rigDir, dir string) error {
	branch := application.StoryBranch(filepath.Base(dir))
	if _, err := w.git(ctx, rigDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return fmt.Errorf("the worktree %s has lost its link to git and the branch %s that its files could be checked against is not there, so it was left as it was", dir, branch)
	}
	index, err := os.CreateTemp("", "mw-husk-index-")
	if err != nil {
		return fmt.Errorf("checking the files of %s against %s: %w", dir, branch, err)
	}
	index.Close()
	defer os.Remove(index.Name())
	// read-tree makes the index; a fresh file must not exist before it does.
	_ = os.Remove(index.Name())

	asked := func(args ...string) (string, error) {
		cmd := gitcmd.Command(ctx, w.program, rigDir, append([]string{"--work-tree=" + dir}, args...)...)
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index.Name())
		var out, errs bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errs
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errs.String()))
		}
		return out.String(), nil
	}
	if _, err := asked("read-tree", "refs/heads/"+branch); err != nil {
		return fmt.Errorf("checking the files of %s against %s: %w", dir, branch, err)
	}
	out, err := asked("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fmt.Errorf("checking the files of %s against %s: %w", dir, branch, err)
	}
	var differ []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(line) > 3 {
			differ = append(differ, line[3:])
		}
	}
	if len(differ) == 0 {
		return nil
	}
	sort.Strings(differ)
	return fmt.Errorf("the worktree %s has lost its link to git and holds tracked files that are not as the branch %s has them (%s), so it was left as it was", dir, branch, strings.Join(differ, ", "))
}

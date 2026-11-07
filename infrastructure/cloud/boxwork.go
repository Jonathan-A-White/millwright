package cloud

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// boxWorkLimit is how long one ask of a box's worktrees may take: a listing or
// a diff is a moment, a bundle of a long branch some minutes.
const boxWorkLimit = 10 * time.Minute

// BoxWork is application.CloudBoxWork over ssh: it reaches a box at the
// WireGuard address the provider reads off the hub, as the Clerk did by hand
// for mw-5r3p30.754, and runs a short sh script there. Nothing is written on
// the box; a box with no GitHub credentials is given none.
type BoxWork struct {
	// Address is where the box is reached (Vultr.Address).
	Address func(ctx context.Context, box string) (application.CloudAddress, error)
	// Command is ssh; empty is "ssh".
	Command string
}

var _ application.CloudBoxWork = BoxWork{}

// boxBase is the sh that sets base to the ref a worktree's rig is landed on: what
// origin's HEAD names, else origin/main. It runs with the worktree as $1.
const boxBase = `base=$(git -C "$1" symbolic-ref --short -q refs/remotes/origin/HEAD) || base=origin/main
`

// boxList prints a line "W<TAB>story<TAB>branch<TAB>head<TAB>ahead<TAB>dir" for
// each worktree under ~/.mw-worktrees, and a line "U<TAB>path" after it for each
// path the worktree has changed and not committed. Any worktree it cannot read
// stops it with an error, so that it is never taken for one holding nothing.
const boxList = `for d in "$HOME"/.mw-worktrees/*/; do
  d=${d%/}
  [ -e "$d/.git" ] || continue
  set -- "$d"
  ` + boxBase + `  head=$(git -C "$d" rev-parse HEAD) || { echo "cannot read the head of $d" >&2; exit 1; }
  branch=$(git -C "$d" symbolic-ref --short -q HEAD) || branch=HEAD
  ahead=$(git -C "$d" rev-list --count "$base..HEAD") || { echo "cannot count the commits of $d ahead of $base" >&2; exit 1; }
  printf 'W\t%s\t%s\t%s\t%s\t%s\n' "${d##*/}" "$branch" "$head" "$ahead" "$d"
  st=$(git -C "$d" status --porcelain -uall) || { echo "cannot read the status of $d" >&2; exit 1; }
  [ -z "$st" ] || printf '%s\n' "$st" | sed 's/^...//; s/^/U\t/'
done
`

// boxBundle writes the commits of the worktree $1 ahead of its base to standard output.
const boxBundle = boxBase + `git -C "$1" bundle create - "$base..HEAD"
`

// boxVerify reads a bundle on standard input, into a file of its own (git cannot
// verify one it cannot seek in), and checks it against the worktree $1.
const boxVerify = `f=$(mktemp) || exit 1
trap 'rm -f "$f"' EXIT
cat > "$f" && git -C "$1" bundle verify "$f"
`

// boxDiff writes the worktree $1's uncommitted changes, untracked files too, as a
// patch against its head, through an index of its own so that its own is not touched.
const boxDiff = `idx=$(mktemp -u) || exit 1
trap 'rm -f "$idx"' EXIT
export GIT_INDEX_FILE=$idx
cd "$1" && git read-tree HEAD && git add -A && git diff --cached --binary HEAD
`

// Worktrees implements application.CloudBoxWork.
func (b BoxWork) Worktrees(ctx context.Context, box string) ([]application.CloudKeptWorktree, error) {
	var out bytes.Buffer
	if err := b.run(ctx, box, boxList, nil, &out); err != nil {
		return nil, err
	}
	return parseBoxList(out.String())
}

func parseBoxList(out string) ([]application.CloudKeptWorktree, error) {
	var found []application.CloudKeptWorktree
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Split(line, "\t")
		switch {
		case line == "":
		case fields[0] == "W" && len(fields) == 6:
			ahead, err := strconv.Atoi(fields[4])
			if err != nil {
				return nil, fmt.Errorf("the box printed %q for the commits ahead in %q", fields[4], line)
			}
			found = append(found, application.CloudKeptWorktree{Story: fields[1], Branch: fields[2], Head: fields[3], Ahead: ahead, Dir: fields[5]})
		case fields[0] == "U" && len(fields) == 2 && len(found) > 0:
			found[len(found)-1].Uncommitted = append(found[len(found)-1].Uncommitted, fields[1])
		default:
			return nil, fmt.Errorf("the box printed a line that is not a worktree: %q", line)
		}
	}
	return found, nil
}

// Bundle implements application.CloudBoxWork: the bundle is streamed into
// dest+".part" and renamed into place whole.
func (b BoxWork) Bundle(ctx context.Context, box string, wt application.CloudKeptWorktree, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	err = b.run(ctx, box, boxBundle, nil, f, wt.Dir)
	if closed := f.Close(); err == nil {
		err = closed
	}
	if err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, dest)
}

// VerifyBundle implements application.CloudBoxWork: the bundle file is given to
// the worktree's own git to verify, which holds the commits it is based on.
func (b BoxWork) VerifyBundle(ctx context.Context, box string, wt application.CloudKeptWorktree, dest string) error {
	f, err := os.Open(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	var out bytes.Buffer
	if err := b.run(ctx, box, boxVerify, f, &out, wt.Dir); err != nil {
		return err
	}
	return nil
}

// UncommittedDiff implements application.CloudBoxWork.
func (b BoxWork) UncommittedDiff(ctx context.Context, box string, wt application.CloudKeptWorktree) (string, error) {
	var out bytes.Buffer
	if err := b.run(ctx, box, boxDiff, nil, &out, wt.Dir); err != nil {
		return "", err
	}
	return out.String(), nil
}

// run runs script on the box with args as $1.., stdin in and stdout out. A
// failure names the last line of what the box said on standard error.
func (b BoxWork) run(ctx context.Context, box, script string, stdin io.Reader, stdout io.Writer, args ...string) error {
	if b.Address == nil {
		return fmt.Errorf("no way to find where %s is reached", box)
	}
	addr, err := b.Address(ctx, box)
	if err != nil {
		return fmt.Errorf("reading %s's address: %w", box, err)
	}
	ctx, cancel := context.WithTimeout(ctx, boxWorkLimit)
	defer cancel()
	remote := "sh -c " + shellQuote(script) + " sh"
	for _, a := range args {
		remote += " " + shellQuote(a)
	}
	command := b.Command
	if command == "" {
		command = "ssh"
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, command, "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "StrictHostKeyChecking=accept-new", addr.String(), remote)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, &stderr
	cmd.WaitDelay = 10 * time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s on %s %w (%s)", command, addr, errRanPast, boxWorkLimit)
		}
		if last := lastLine(stderr.String()); last != "" {
			return fmt.Errorf("%s on %s: %v: %s", command, addr, err, last)
		}
		return fmt.Errorf("%s on %s: %w", command, addr, err)
	}
	return nil
}

// shellQuote is s as one word of sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

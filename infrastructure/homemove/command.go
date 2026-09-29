package homemove

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Command is mw home move as the Governor's move-home message runs it (postern's
// docs/protocol.md §18): this same mw, run again, beside Host's ssh to the old
// home. It is the adapter behind application.PosternHomeMover.
type Command struct {
	Host
	// Mw is the mw program to run; this program itself when empty.
	Mw string
	// Spent is the directory, on this host's own disk, a started move-home's
	// txid is marked in: one empty file each, never removed.
	Spent string
}

// Spend implements application.PosternHomeMover: an empty file named for the
// txid in Spent, made only if it is not there already.
func (c Command) Spend(_ context.Context, txid string) (bool, error) {
	if c.Spent == "" {
		return false, errors.New("no directory to mark a started move-home in")
	}
	if err := os.MkdirAll(c.Spent, 0o700); err != nil {
		return false, fmt.Errorf("making %s: %w", c.Spent, err)
	}
	name := "move-home." + strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, txid)
	file, err := os.OpenFile(filepath.Join(c.Spent, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("marking move-home %s started: %w", txid, err)
	}
	return true, file.Close()
}

var _ application.PosternHomeMover = Command{}

// MoveHome implements application.PosternHomeMover: `mw home move <args>`, its
// output and its exit status. A move that ran and failed is an outcome, not an
// error; only one that could not be started at all is.
func (c Command) MoveHome(ctx context.Context, args []string) (application.HomeMoveOutcome, error) {
	program := c.Mw
	if program == "" {
		self, err := os.Executable()
		if err != nil {
			return application.HomeMoveOutcome{}, fmt.Errorf("finding this mw program to run mw home move: %w", err)
		}
		program = self
	}
	out, err := exec.CommandContext(ctx, program, append([]string{"home", "move"}, args...)...).CombinedOutput()
	if err == nil {
		return application.HomeMoveOutcome{Output: string(out)}, nil
	}
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return application.HomeMoveOutcome{Exit: exited.ExitCode(), Output: string(out)}, nil
	}
	return application.HomeMoveOutcome{}, fmt.Errorf("running %s home move: %w", program, err)
}

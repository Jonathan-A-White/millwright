package hostlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Jonathan-A-White/millwright/application"
)

// BdFile is the name of the bd turn lock inside the state directory.
const BdFile = "bd.lock"

// BdTurn is this host's turn to run a bd in (application.BdTurn): a file in Dir
// flocked, with the holder's process id and command line written in it, so that
// a mw bd that waits too long can say whom it waited for.
type BdTurn struct {
	Dir string
	// Who is what this process writes in the file as its own name.
	Who string
}

// BdTurn satisfies the port.
var _ application.BdTurn = BdTurn{}

// NewBdTurn is the bd turn kept in dir, taken by who.
func NewBdTurn(dir, who string) BdTurn { return BdTurn{Dir: dir, Who: who} }

func (t BdTurn) path() string { return filepath.Join(t.Dir, BdFile) }

// TryTake implements application.BdTurn. It never waits.
func (t BdTurn) TryTake(context.Context) (func(), bool, string, error) {
	if err := os.MkdirAll(t.Dir, 0o700); err != nil {
		return nil, false, "", fmt.Errorf("making the directory the bd lock %s belongs in: %w", t.path(), err)
	}
	file, err := os.OpenFile(t.path(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, "", fmt.Errorf("opening the bd lock %s: %w", t.path(), err)
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		defer file.Close()
		said, _ := os.ReadFile(t.path())
		holder := strings.TrimSpace(string(said))
		if holder == "" {
			holder = "another mw bd"
		}
		return nil, false, holder, nil
	}
	if err != nil {
		file.Close()
		return nil, false, "", fmt.Errorf("taking the bd lock %s: %w", t.path(), err)
	}
	file.Truncate(0)
	fmt.Fprintf(file, "another mw bd (pid %d: %s)\n", os.Getpid(), t.Who)
	return func() {
		file.Truncate(0)
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}, true, "", nil
}

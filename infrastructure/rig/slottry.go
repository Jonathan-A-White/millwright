package rig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Slots satisfies the port that can be tried without waiting.
var _ application.TryMergeSlot = (*Slots)(nil)

// TryTake implements application.TryMergeSlot: the slot alone if it is free this
// moment, and a *application.SlotHeldError at once, naming who has it, when it is
// not. It does not queue, so a tick that is told "held" leaves no ticket behind.
func (s *Slots) TryTake(_ context.Context, rigDir, holder string) (application.Holding, error) {
	if rigDir == "" {
		return nil, fmt.Errorf("taking a merge slot: which rig?")
	}
	path := slotPath(rigDir, s.suffix)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("making the directory the merge slot of %s belongs in: %w", rigDir, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening the merge slot of %s: %w", rigDir, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &application.SlotHeldError{Path: path, Holder: held(path), Since: slotTaken(slotSays(path))}
		}
		return nil, fmt.Errorf("taking the merge slot %s: %w", path, err)
	}
	says := fmt.Sprintf("%s\npid %d\ntaken %s\n", holder, os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if err := write(file, says); err != nil {
		unlock(file)
		file.Close()
		return nil, fmt.Errorf("writing who has the merge slot of %s: %w", rigDir, err)
	}
	return &holding{file: file, holder: holder, slots: s, rigDir: rigDir, path: path, says: says}, nil
}

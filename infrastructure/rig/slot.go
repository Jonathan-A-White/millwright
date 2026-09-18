package rig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// SlotSuffix names a rig's merge slot, which lives beside the rig's story
// worktrees. SlotWait is how long a close-out waits for the slot before giving
// up, and SlotPoll is how often it looks.
const (
	SlotSuffix = ".merge-slot"
	SlotWait   = 20 * time.Minute
	SlotPoll   = 2 * time.Second
)

// Slots hands out a rig's merge slot: the right to be the one close-out putting
// work on that rig's target branch on this host. It is the adapter behind
// application.MergeSlot.
//
// The slot is an advisory lock on a file (flock), and that is the whole reason
// it is a lock rather than a file somebody writes their name in: the kernel
// holds it, so a mw that dies holding the slot — killed, out of memory, the
// terminal closed under it — gives it back the moment the process ends. There
// is no stale slot to break by hand and no timeout to guess at. The file's
// contents are only ever a courtesy: whoever has it says so in there, so that
// whoever is waiting can see who they are waiting for.
//
// It is this host's slot, not the factory's. The other host has its own, and a
// race between the two hosts is settled where it has to be, by the remote
// refusing the second push.
type Slots struct {
	wait time.Duration
	poll time.Duration
}

// Slots satisfies the port.
var _ application.MergeSlot = (*Slots)(nil)

// SlotOption is a setting of a Slots, given to NewSlots.
type SlotOption func(*Slots)

// WithSlotWait sets how long taking a slot waits before giving up.
func WithSlotWait(wait time.Duration) SlotOption {
	return func(s *Slots) { s.wait = wait }
}

// WithSlotPoll sets how often a wait looks to see whether the slot is free.
func WithSlotPoll(poll time.Duration) SlotOption {
	return func(s *Slots) { s.poll = poll }
}

// NewSlots returns the merge slots of this host's rigs.
func NewSlots(opts ...SlotOption) *Slots {
	s := &Slots{wait: SlotWait, poll: SlotPoll}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SlotPath is where a rig's merge slot lives: beside the rig's story worktrees,
// named after the rig, and so outside the rig's own git directory and outside
// every worktree cut from it.
func SlotPath(rigDir string) string {
	clean := filepath.Clean(rigDir)
	return filepath.Join(filepath.Dir(clean), application.WorktreesDir, filepath.Base(clean)+SlotSuffix)
}

// Take implements application.MergeSlot.
func (s *Slots) Take(ctx context.Context, rigDir, holder string) (application.Holding, error) {
	if rigDir == "" {
		return nil, fmt.Errorf("taking a merge slot: which rig?")
	}
	path := SlotPath(rigDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("making the directory the merge slot of %s belongs in: %w", rigDir, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening the merge slot of %s: %w", rigDir, err)
	}

	if err := s.flock(ctx, file, path); err != nil {
		file.Close()
		return nil, err
	}
	if err := write(file, fmt.Sprintf("%s\npid %d\ntaken %s\n", holder, os.Getpid(), time.Now().UTC().Format(time.RFC3339))); err != nil {
		unlock(file)
		file.Close()
		return nil, fmt.Errorf("writing who has the merge slot of %s: %w", rigDir, err)
	}
	return &holding{file: file, holder: holder}, nil
}

// flock waits for the lock on the slot file, looking every poll until the wait
// runs out or the context gives up.
func (s *Slots) flock(ctx context.Context, file *os.File, path string) error {
	started := time.Now()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("taking the merge slot %s: %w", path, err)
		}
		if waited := time.Since(started); waited >= s.wait {
			return fmt.Errorf("the merge slot %s is still held by %q after %s: another close-out is landing work on this rig, or one is stuck",
				path, held(path), waited.Round(time.Second))
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for the merge slot %s, held by %q: %w", path, held(path), ctx.Err())
		case <-time.After(s.poll):
		}
	}
}

// holding is a merge slot somebody has: the open file whose lock is the slot.
// Closing the file is what gives the lock back, and the kernel does that for a
// process that dies, which is why nothing here has to be cleaned up afterwards.
type holding struct {
	file   *os.File
	holder string
	gone   bool
}

// Release implements application.Holding. Releasing twice is harmless.
func (h *holding) Release(_ context.Context) error {
	if h.gone {
		return nil
	}
	h.gone = true
	// Emptied first, so that a slot nobody has does not name the last person to
	// have had it.
	writeErr := write(h.file, "")
	unlockErr := unlock(h.file)
	closeErr := h.file.Close()
	return errors.Join(writeErr, unlockErr, closeErr)
}

// HeldBy implements application.Holding.
func (h *holding) HeldBy() string { return h.holder }

// write replaces what the slot file says.
func write(file *os.File, text string) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if text == "" {
		return nil
	}
	_, err := file.WriteString(text)
	return err
}

// unlock gives the lock back without closing the file.
func unlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

// held is whoever the slot file says has it, for a message to somebody waiting.
// It reads without taking anything, so it is a courtesy and never a decision.
func held(path string) string {
	said, err := os.ReadFile(path)
	if err != nil {
		return "somebody"
	}
	if first := strings.TrimSpace(strings.SplitN(string(said), "\n", 2)[0]); first != "" {
		return first
	}
	return "somebody"
}

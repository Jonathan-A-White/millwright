package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// mergeSlotEpoch is where the wound clock of features/merge_slot.feature starts.
var mergeSlotEpoch = time.Date(2026, 10, 15, 20, 29, 0, 0, time.UTC)

// mergeSlotContext is one rig's merge slot, somebody holding it through a lock
// of their own, and a clock wound by the waiter's own polls (mw-gq6.467).
type mergeSlotContext struct {
	rigDir   string
	wait     time.Duration
	cap      time.Duration
	held     *os.File
	at       time.Time
	letGo    time.Time
	took     time.Duration
	err      error
	released bool
}

// InitializeMergeSlotScenario registers the steps of features/merge_slot.feature.
func InitializeMergeSlotScenario(ctx *godog.ScenarioContext) {
	c := &mergeSlotContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = mergeSlotContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		c.letTheHolderGo()
		if c.rigDir != "" {
			os.RemoveAll(filepath.Dir(filepath.Dir(c.rigDir)))
		}
		return ctx, nil
	})

	ctx.Given(`^a rig whose close-outs wait (\d+) minutes on one holder of its merge slot and (\d+) hours in all$`, c.aRigWhoseCloseOutsWait)
	ctx.Given(`^the merge slot is held by "([^"]*)", whose process is (alive|gone)$`, c.theSlotIsHeldBy)
	ctx.Given(`^the merge slot names "([^"]*)", whose process is gone$`, c.theSlotNamesAGoneHolder)
	ctx.Given(`^that holder lets go after (\d+) minutes$`, c.thatHolderLetsGoAfter)
	ctx.When(`^a close-out of "([^"]*)" takes the merge slot$`, c.aCloseOutTakesTheSlot)
	ctx.Then(`^the close-out took the merge slot after (\d+) minutes$`, c.theCloseOutTookTheSlotAfter)
	ctx.Then(`^the close-out did not get the merge slot after (\d+) hours, the overall most a close-out waits$`, c.theCloseOutStoppedAtTheCap)
	ctx.Then(`^the close-out did not get the merge slot after (\d+) minutes, one holder having had it that long$`, c.theCloseOutStoppedOnOneHolder)
	ctx.Then(`^the failure names "([^"]*)"$`, c.theFailureNames)
}

func (c *mergeSlotContext) aRigWhoseCloseOutsWait(minutes, hours int) error {
	root, err := os.MkdirTemp("", "mw-merge-slot-")
	if err != nil {
		return err
	}
	c.rigDir = filepath.Join(root, "rigs", "lampas")
	if err := os.MkdirAll(c.rigDir, 0o755); err != nil {
		return err
	}
	c.wait, c.cap = time.Duration(minutes)*time.Minute, time.Duration(hours)*time.Hour
	return nil
}

// gonePid is the process id of a process that has ended: one this step started
// and waited for.
func gonePid() (int, error) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("running a process to end: %w", err)
	}
	return cmd.Process.Pid, nil
}

// slotSays is what a close-out writes into the slot it holds.
func slotSays(holder string, pid int) string {
	return fmt.Sprintf("%s\npid %d\ntaken %s\n", holder, pid, mergeSlotEpoch.Format(time.RFC3339))
}

func (c *mergeSlotContext) holderPid(process string) (int, error) {
	if process == "alive" {
		return os.Getpid(), nil
	}
	return gonePid()
}

// theSlotIsHeldBy locks the slot through a file of the step's own, as another
// close-out holds it, naming holder and a process that is alive (this one) or
// gone.
func (c *mergeSlotContext) theSlotIsHeldBy(holder, process string) error {
	pid, err := c.holderPid(process)
	if err != nil {
		return err
	}
	path := rig.SlotPath(c.rigDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return fmt.Errorf("locking the slot: %w", err)
	}
	c.held = file
	_, err = file.WriteAt([]byte(slotSays(holder, pid)), 0)
	return err
}

// theSlotNamesAGoneHolder writes the slot as a close-out that died holding it
// left it: its name and process still in the file, and no lock, since the
// kernel gave the lock back when the process ended.
func (c *mergeSlotContext) theSlotNamesAGoneHolder(holder string) error {
	pid, err := gonePid()
	if err != nil {
		return err
	}
	path := rig.SlotPath(c.rigDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(slotSays(holder, pid)), 0o644)
}

func (c *mergeSlotContext) thatHolderLetsGoAfter(minutes int) error {
	c.letGo = mergeSlotEpoch.Add(time.Duration(minutes) * time.Minute)
	return nil
}

func (c *mergeSlotContext) letTheHolderGo() {
	if c.held != nil && !c.released {
		c.released = true
		syscall.Flock(int(c.held.Fd()), syscall.LOCK_UN)
		c.held.Close()
	}
}

func (c *mergeSlotContext) aCloseOutTakesTheSlot(id string) error {
	c.at = mergeSlotEpoch
	now := func() time.Time { return c.at }
	sleep := func(_ context.Context, d time.Duration) error {
		c.at = c.at.Add(d)
		if !c.letGo.IsZero() && !c.at.Before(c.letGo) {
			c.letTheHolderGo()
		}
		return nil
	}
	slots := rig.NewSlots(rig.WithSlotWait(c.wait), rig.WithSlotCap(c.cap), rig.WithSlotPoll(time.Minute),
		rig.WithSlotClock(now, sleep))
	holding, err := slots.Take(context.Background(), c.rigDir, "builder@laptop closing out "+id)
	c.took, c.err = c.at.Sub(mergeSlotEpoch), err
	if holding != nil {
		return holding.Release(context.Background())
	}
	return nil
}

func (c *mergeSlotContext) theCloseOutTookTheSlotAfter(minutes int) error {
	if c.err != nil {
		return fmt.Errorf("expected the close-out to take the merge slot, it gave up after %s: %v", c.took, c.err)
	}
	if want := time.Duration(minutes) * time.Minute; c.took != want {
		return fmt.Errorf("expected the close-out to take the merge slot after %s, it took it after %s", want, c.took)
	}
	return nil
}

func (c *mergeSlotContext) theCloseOutStoppedAtTheCap(hours int) error {
	if c.err == nil {
		return fmt.Errorf("expected the close-out not to get the merge slot, it took it after %s", c.took)
	}
	if want := time.Duration(hours) * time.Hour; c.took != want {
		return fmt.Errorf("expected the close-out to stop after %s, it stopped after %s: %v", want, c.took, c.err)
	}
	if !errors.Is(c.err, application.ErrSlotCap) {
		return fmt.Errorf("expected the failure to be the overall most a close-out waits (application.ErrSlotCap), got %v", c.err)
	}
	if !strings.Contains(c.err.Error(), "overall") {
		return fmt.Errorf("expected the failure to say it was the overall most, got %q", c.err)
	}
	return nil
}

func (c *mergeSlotContext) theCloseOutStoppedOnOneHolder(minutes int) error {
	if c.err == nil {
		return fmt.Errorf("expected the close-out not to get the merge slot, it took it after %s", c.took)
	}
	if want := time.Duration(minutes) * time.Minute; c.took != want {
		return fmt.Errorf("expected the close-out to stop after %s, it stopped after %s: %v", want, c.took, c.err)
	}
	if errors.Is(c.err, application.ErrSlotCap) {
		return fmt.Errorf("expected the failure to be the per-holder limit, not the overall most: %v", c.err)
	}
	if !strings.Contains(c.err.Error(), "one holder") {
		return fmt.Errorf("expected the failure to say one holder had it that long, got %q", c.err)
	}
	return nil
}

func (c *mergeSlotContext) theFailureNames(holder string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), holder) {
		return fmt.Errorf("expected the failure to name %q, got %v", holder, c.err)
	}
	return nil
}

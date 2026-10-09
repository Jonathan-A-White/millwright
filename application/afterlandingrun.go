package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// AfterLandingRun is `mw after-landing <rig>`: the rig's after-landing command,
// run now, by hand, as a landing runs it. It is for a landing whose deploy did
// not happen — the network was down for a minute, or the host was busy — so that
// a person does not have to copy the command line out of config.toml and run it
// by hand without any of the care a landing takes.
//
// It runs in the rig's checkout, as it is, and does not move it: nothing is
// fetched or merged. It takes the rig's after-landing lock first, the one a
// landing's own deploy holds, so it waits for a deploy that is running and never
// runs beside one. The command has the limit the host names for the rig, and is
// run once more after a pause when it fails on a passing network fault, as a
// landing's is.
type AfterLandingRun struct {
	AfterLanding AfterLanding

	// Slot is the rig's after-landing lock. A nil Slot takes none.
	Slot MergeSlot

	// Units restarts this host's long-running mw units on the new build when the
	// command succeeded for the factory rig, as a landing does. Nil restarts none.
	Units UnitRestarter

	// Rigs is where each rig is checked out on this host, and Host which host it is.
	Rigs map[string]string
	Host string

	// RetryWait is how long the command is left before it is run again after a
	// passing network fault; zero is AfterLandingRetryWait. Wait is how it waits,
	// the real clock unless a test replaces it.
	RetryWait time.Duration
	Wait      func(ctx context.Context, d time.Duration) error

	// Out is where the lines are printed. A nil Out prints nothing.
	Out io.Writer
}

// Run runs the rig's after-landing command and prints what became of it. A
// command that did not succeed is an error carrying the one line the landing
// would have said, so that the command's exit status says so too.
func (a AfterLandingRun) Run(ctx context.Context, rigName string) error {
	rigName = strings.TrimSpace(rigName)
	if rigName == "" {
		return errors.New("running the after-landing command: which rig?")
	}
	if a.AfterLanding == nil {
		return errors.New("running the after-landing command: it needs the host's after-landing commands")
	}
	dir, ok := a.Rigs[rigName]
	if !ok || dir == "" {
		return fmt.Errorf("the rig %s is not one this host has checked out (it has: %s)", rigName, strings.Join(a.rigNames(), ", "))
	}
	command := a.AfterLanding.Command(rigName)
	if command == "" {
		return fmt.Errorf("this host names no after-landing command for %s", rigName)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("the checkout of %s, %s, is not there", rigName, dir)
	}

	release, err := takeAfterLandingSlot(ctx, a.Slot, dir, fmt.Sprintf("%s running the after-landing command of %s", SeatIdentity(MwSeat, a.Host), rigName))
	if err != nil {
		return err
	}
	wait := a.Wait
	if wait == nil {
		wait = waitFor
	}
	ran, retried, runErr := runAfterLanding(ctx, a.AfterLanding, rigName, dir, a.RetryWait, wait)
	if relErr := release(ctx); relErr != nil {
		a.say("the after-landing lock of %s could not be given back: %v", rigName, relErr)
	}
	if retried != "" {
		a.say("%s", retried)
	}
	if runErr != nil {
		return errors.New(afterLandingLine(command, "could not be run: "+firstLine(runErr.Error())))
	}
	if !ran.Succeeded() {
		return errors.New(ran.Line())
	}
	a.say("%s", ran.Line())
	notes, failed := RestartFactoryUnits(ctx, a.Units, rigName)
	for _, note := range notes {
		a.say("%s", note)
	}
	if len(failed) > 0 {
		return errors.New(UnitRestartFailedLine)
	}
	return nil
}

func (a AfterLandingRun) rigNames() []string {
	names := make([]string, 0, len(a.Rigs))
	for name := range a.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (a AfterLandingRun) say(format string, args ...any) {
	if a.Out != nil {
		fmt.Fprintf(a.Out, format+"\n", args...)
	}
}

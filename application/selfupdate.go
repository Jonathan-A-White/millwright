package application

import (
	"context"
	"fmt"
)

// FactoryRig is the rig the factory itself is built from, and FactoryBranch the
// branch of it every host runs its mw from.
const (
	FactoryRig    = "millwright"
	FactoryBranch = "main"
)

// FactoryCheckout is the port a host's own checkout of the factory rig is
// looked at and moved through.
type FactoryCheckout interface {
	// Fetch brings the checkout's view of its origin up to date.
	Fetch(ctx context.Context, rigDir string) error

	// Tip is the commit branch is at on the remote, as the last Fetch saw it.
	Tip(ctx context.Context, rigDir, remote, branch string) (string, error)

	// Head is the commit the checkout has checked out.
	Head(ctx context.Context, rigDir string) (string, error)

	// Uncommitted lists the paths the working tree has changed and not
	// committed; empty means clean.
	Uncommitted(ctx context.Context, dir string) ([]string, error)

	// Advance fast-forwards the checkout onto commit, as Landing.Advance does.
	Advance(ctx context.Context, rigDir, branch, commit string) (Advanced, error)
}

// BuiltMarks is where a host remembers, per rig, the commit it last built
// successfully, so that a build that failed is tried again by the next tick.
type BuiltMarks interface {
	// Built is the commit last built for rig, empty when none was.
	Built(ctx context.Context, rig string) (string, error)

	// MarkBuilt records commit as the one last built for rig.
	MarkBuilt(ctx context.Context, rig, commit string) error
}

// SelfUpdate keeps the mw a host runs level with the factory rig's main
// (mw-gq6.183). A host rebuilt its mw only when it landed a millwright story
// itself, so a fix landed on the other host never reached it. Once a tick, a
// host that names a command for the factory rig (its [after_landing] table: the
// one a landing runs, `make build` with the host's own Go) fetches the rig,
// fast-forwards a clean checkout that is behind origin's main and runs that
// command in it.
//
// A checkout that is dirty, on another branch or has commits of its own is
// left exactly as it is and the notes say why; nothing is ever merged, reset or
// forced. A build that fails leaves the old binary, is said, and is tried again
// by the next tick, because the commit last built is remembered and the
// checkout is no longer behind. A host that names no command runs nothing from
// the rig, and its checkout is not touched.
type SelfUpdate struct {
	// Rigs is where each rig is checked out on this host; the factory rig is
	// looked for in it.
	Rigs     map[string]string
	Checkout FactoryCheckout
	After    AfterLanding
	Built    BuiltMarks

	// Units restarts this host's long-running mw user units once the build has
	// succeeded, so that they run what was just built. A nil Units restarts
	// nothing.
	Units UnitRestarter
}

// selfUpdateWords is how every note about an update begins.
const selfUpdateWords = "self-update: " + FactoryRig

// updatedRevisionLen is how much of a commit the notes name, the same short
// revision mw version prints.
const updatedRevisionLen = 7

func updatedRevision(commit string) string {
	if len(commit) > updatedRevisionLen {
		return commit[:updatedRevisionLen]
	}
	return commit
}

// Run does one look and says what it did, as notes for a tick's line: none for
// a host that has nothing to do, so that the line says it once.
func (s SelfUpdate) Run(ctx context.Context) []string {
	dir := s.Rigs[FactoryRig]
	if dir == "" || s.Checkout == nil || s.After == nil || s.Built == nil || s.After.Command(FactoryRig) == "" {
		return nil
	}
	failed := func(doing string, err error) []string {
		return []string{fmt.Sprintf("%s could not be brought level: %s: %s", selfUpdateWords, doing, firstLine(err.Error()))}
	}

	if err := s.Checkout.Fetch(ctx, dir); err != nil {
		return failed("fetching", err)
	}
	tip, err := s.Checkout.Tip(ctx, dir, DefaultRemote, FactoryBranch)
	if err != nil || tip == "" {
		if err == nil {
			err = fmt.Errorf("origin has no %s", FactoryBranch)
		}
		return failed("reading origin's "+FactoryBranch, err)
	}
	before, err := s.Checkout.Head(ctx, dir)
	if err != nil {
		return failed("reading the checkout's commit", err)
	}
	advanced, err := s.Checkout.Advance(ctx, dir, FactoryBranch, tip)
	if err != nil {
		return failed("fast-forwarding", err)
	}
	if advanced.Left != "" {
		return []string{selfUpdateWords + " checkout left alone: " + advanced.Left}
	}
	head, err := s.Checkout.Head(ctx, dir)
	if err != nil {
		return failed("reading the checkout's commit", err)
	}

	// Level already: build only what an earlier tick's failed build left
	// unbuilt, and only a tree that is exactly a commit.
	if !advanced.Moved {
		if built, err := s.Built.Built(ctx, FactoryRig); err != nil || built == head {
			return nil
		}
		if dirty, err := s.Checkout.Uncommitted(ctx, dir); err != nil || len(dirty) > 0 {
			return nil
		}
	}

	ran, err := s.After.Run(ctx, FactoryRig, dir)
	switch {
	case err != nil:
		return []string{fmt.Sprintf("%s at %s: %s; the old mw is kept, the next tick tries again",
			selfUpdateWords, updatedRevision(head), afterLandingLine(s.After.Command(FactoryRig), "could not be run: "+firstLine(err.Error())))}
	case !ran.Succeeded():
		return []string{fmt.Sprintf("%s at %s: %s; the old mw is kept, the next tick tries again",
			selfUpdateWords, updatedRevision(head), ran.Line())}
	}

	notes := []string{fmt.Sprintf("%s %s → %s, built", selfUpdateWords, updatedRevision(before), updatedRevision(head))}
	if !advanced.Moved {
		notes = []string{fmt.Sprintf("%s built at %s", selfUpdateWords, updatedRevision(head))}
	}
	if err := s.Built.MarkBuilt(ctx, FactoryRig, head); err != nil {
		notes = append(notes, "the build could not be remembered: "+firstLine(err.Error()))
	}
	said, _ := RestartFactoryUnits(ctx, s.Units, FactoryRig)
	return append(notes, said...)
}

package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// RigCommit is a commit of a rig as a stamp of it needs to know it: the full
// id, the branch it is stamped on and its subject.
type RigCommit struct {
	Branch  string
	Commit  string
	Subject string
}

// RigHeads reads commits of the rigs this host has checked out. The adapter is
// the rig's git.
type RigHeads interface {
	// Resolve reports the commit rev names in the checkout at rigDir, in full,
	// with its subject and the rig's default branch. An empty rev is the head
	// of origin's default branch, read after a fetch.
	Resolve(ctx context.Context, rigDir, rev string) (RigCommit, error)
}

// StampHead queues a chain stamp of a rig's head, or of a commit of it, by
// hand: the stamp a landing or a vault push queues on its own, for a commit
// neither of them saw. It has no story, so the chain-stamp job comments on
// none. A commit that is already stamped, sent or waiting, is refused.
// docs/chain-stamps.md.
type StampHead struct {
	// Rigs is the [rigs] table of the config: rig name to checkout.
	Rigs  map[string]string
	Heads RigHeads
	Queue StampQueue
	Store StampStore
	Host  string
	Out   io.Writer

	// Now is the clock a stamp is dated by. The zero value reads the real one.
	Now func() time.Time
}

// Run queues one stamp of rig's commit (an empty commit is its head), or, with
// all, one of every rig in the config whose head has none, naming the rigs it
// skips. A rig that fails under all is said and the rest go on; the run then
// fails.
func (s StampHead) Run(ctx context.Context, rig, commit string, all bool) error {
	if !all {
		if rig == "" {
			return errors.New("stamp needs a rig, or --all")
		}
		return s.one(ctx, rig, commit)
	}
	if rig != "" || commit != "" {
		return errors.New("--all takes no rig or commit")
	}
	names := make([]string, 0, len(s.Rigs))
	for name := range s.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	failed := 0
	for _, name := range names {
		err := s.one(ctx, name, "")
		var stamped alreadyStamped
		switch {
		case errors.As(err, &stamped):
			fmt.Fprintf(s.Out, "skipped %s: %v\n", name, err)
		case err != nil:
			failed++
			fmt.Fprintf(s.Out, "failed %s: %v\n", name, err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d rigs could not be stamped", failed, len(names))
	}
	return nil
}

// alreadyStamped is the refusal of a commit that has a stamp.
type alreadyStamped string

func (a alreadyStamped) Error() string { return "already stamped: " + string(a) }

func (s StampHead) one(ctx context.Context, rig, rev string) error {
	dir, ok := s.Rigs[rig]
	if !ok || dir == "" {
		return fmt.Errorf("no rig %s in the config's [rigs] table", rig)
	}
	head, err := s.Heads.Resolve(ctx, dir, strings.TrimSpace(rev))
	if err != nil {
		return fmt.Errorf("reading %s: %w", rig, err)
	}
	if err := s.refuseStamped(ctx, rig, head.Commit); err != nil {
		return err
	}
	stamp := domain.Stamp{
		Rig: rig, Branch: head.Branch, Commit: head.Commit,
		Title: fmt.Sprintf("Head of %s: %s", rig, head.Subject),
		Host:  s.Host, At: s.now().UTC(),
	}
	if err := s.Queue.Append(ctx, stamp); err != nil {
		return fmt.Errorf("queueing the stamp of %s: %w", rig, err)
	}
	fmt.Fprintf(s.Out, "queued %s %s (%s): %s\n", rig, head.Commit, head.Branch, stamp.Title)
	return nil
}

// refuseStamped is an alreadyStamped error when commit of rig was sent, naming
// its txid, or waits in the queue.
func (s StampHead) refuseStamped(ctx context.Context, rig, commit string) error {
	sent, err := s.Store.FindSent(ctx, rig, commit)
	if err != nil {
		return fmt.Errorf("reading the sent stamps: %w", err)
	}
	for _, found := range sent {
		if strings.EqualFold(found.Stamp.Commit, commit) {
			return alreadyStamped(found.Txid)
		}
	}
	pending, err := s.Queue.Pending(ctx)
	if err != nil {
		return fmt.Errorf("reading the pending stamps: %w", err)
	}
	for _, queued := range pending {
		if queued.Stamp.Rig == rig && strings.EqualFold(queued.Stamp.Commit, commit) {
			return alreadyStamped("queued, not yet sent")
		}
	}
	return nil
}

func (s StampHead) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

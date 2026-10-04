package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// ChainStampJobName is the follower job that broadcasts queued stamps; its
// events' actor is ChainStampJobName@host.
const ChainStampJobName = "chain-stamp"

// QueuedStamp is a stamp waiting to be broadcast, and what has gone wrong with
// it so far: how many times a broadcast of it failed, with no limit, and the
// last failure's words.
type QueuedStamp struct {
	Stamp     domain.Stamp `json:"stamp"`
	Attempts  int          `json:"attempts,omitempty"`
	LastError string       `json:"last_error,omitempty"`
}

// SentStamp is a stamp that was broadcast: the txid it went out under, when,
// and how many tries it took, the one that worked included.
type SentStamp struct {
	Stamp    domain.Stamp `json:"stamp"`
	Txid     string       `json:"txid"`
	SentAt   time.Time    `json:"sent_at"`
	Attempts int          `json:"attempts"`
}

// StampQueue is where a landing leaves a stamp for the follower's chain-stamp
// job to broadcast, and where the job records what became of it. Stamps are
// told apart by their Stamp's value. One adapter is files in the state
// directory: pending.jsonl and sent.jsonl.
type StampQueue interface {
	// Append queues a stamp, last.
	Append(ctx context.Context, stamp domain.Stamp) error
	// Pending reports the stamps not yet sent, oldest first.
	Pending(ctx context.Context) ([]QueuedStamp, error)
	// MarkFailed counts one more failed broadcast of stamp and keeps why. The
	// stamp stays pending.
	MarkFailed(ctx context.Context, stamp domain.Stamp, why string) error
	// MarkSent moves stamp from pending to sent under txid.
	MarkSent(ctx context.Context, stamp domain.Stamp, txid string, at time.Time) error
}

// ChainStamp broadcasts the stamps that are queued, on testnet through the
// postern backend: for each, the sealed record SealStamp builds is signed and
// broadcast exactly as a message's public chain copy is (broadcastRecord), its
// txid is recorded with the stamp, and the story it was landed for is told.
// A stamp that cannot be broadcast stays pending, with a failure counted, for
// the next run; nothing a stamp does fails the run, because the job runs every
// minute and each run is a retry. docs/chain-stamps.md.
type ChainStamp struct {
	Queue       StampQueue
	Postern     Postern
	Keys        PosternKeyFile
	Cipher      Cipher
	Tracker     WorkTracker
	GovernorKey string

	// Now is the clock a stamp's record is dated by. The zero value reads the
	// real one.
	Now func() time.Time
	// Err is where a stamp that could not be sent is said. A nil Err says
	// nothing.
	Err io.Writer
}

// ChainStampJob is the job the follower runs for ChainStamp: no event springs
// it, it runs every d, on the follower's own clock.
func ChainStampJob(every time.Duration, run func(context.Context) error) SpringJob {
	return ClockJob(ChainStampJobName, every, run)
}

// Run does one pass over the pending stamps. It returns an error only for what
// stops the whole pass: a queue that cannot be read, or nothing to seal with.
func (c ChainStamp) Run(ctx context.Context) error {
	pending, err := c.Queue.Pending(ctx)
	if err != nil {
		return fmt.Errorf("reading the pending stamps: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}
	if strings.TrimSpace(c.GovernorKey) == "" {
		return fmt.Errorf("%d stamp(s) wait, and there is no Governor key to seal them to: set postern_governor_key", len(pending))
	}
	from, address, err := c.Keys.PublicKey()
	if err != nil {
		return fmt.Errorf("%d stamp(s) wait, and the postern key could not be read: %w", len(pending), err)
	}
	for _, queued := range pending {
		if ctx.Err() != nil {
			return nil
		}
		c.send(ctx, queued, from, address)
	}
	return nil
}

// send broadcasts one stamp and records how it went.
func (c ChainStamp) send(ctx context.Context, queued QueuedStamp, from, address string) {
	stamp := queued.Stamp
	_, raw, err := SealStamp(c.Cipher, c.GovernorKey, from, stamp, c.now())
	var txid string
	if err == nil {
		txid, err = broadcastRecord(ctx, c.Postern, c.Keys, address, raw, c.Err)
	}
	if err != nil {
		c.say("chain-stamp: %s %s stays pending: %v\n", stamp.Rig, stamp.Commit, err)
		if err := c.Queue.MarkFailed(ctx, stamp, oneLine(err.Error())); err != nil {
			c.say("chain-stamp: the failed try of %s could not be counted: %v\n", stamp.Commit, err)
		}
		return
	}
	if err := c.Queue.MarkSent(ctx, stamp, txid, c.now()); err != nil {
		c.say("chain-stamp: %s was broadcast as %s but could not be recorded as sent: %v\n", stamp.Commit, txid, err)
		return
	}
	if stamp.Story == "" {
		return
	}
	comment := fmt.Sprintf("STAMP %s for %s (testnet)", txid, stamp.Commit)
	if err := c.Tracker.CommentOnStory(ctx, stamp.Story, comment); err != nil {
		c.say("chain-stamp: %s was sent as %s but could not be commented on %s: %v\n", stamp.Commit, txid, stamp.Story, err)
	}
}

func (c ChainStamp) say(format string, args ...any) {
	if c.Err != nil {
		fmt.Fprintf(c.Err, format, args...)
	}
}

func (c ChainStamp) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

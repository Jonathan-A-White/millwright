package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ExplorerURL is where a testnet transaction is read on WhatsOnChain.
const ExplorerURL = "https://test.whatsonchain.com/tx/"

// StampStore is where the stamps that were broadcast are kept. The file adapter
// is the one StampQueue's sent.jsonl is read through.
type StampStore interface {
	// FindSent reports the sent stamps of rig whose commit id begins with
	// commitPrefix, oldest first. An empty prefix matches nothing.
	FindSent(ctx context.Context, rig, commitPrefix string) ([]SentStamp, error)
}

// SentStampMatches reports whether sent is a stamp of rig whose commit id
// begins with commitPrefix, hex case aside. An empty prefix matches nothing. It
// is the one rule every StampStore finds by.
func SentStampMatches(sent SentStamp, rig, commitPrefix string) bool {
	return commitPrefix != "" && sent.Stamp.Rig == rig &&
		strings.HasPrefix(strings.ToLower(sent.Stamp.Commit), strings.ToLower(commitPrefix))
}

// Prove prints the proof of a stamped commit: the txid of the stamp that was
// broadcast for it, the block it is in and when, the preimage its public
// commitment was made of, and the explorer's URL. docs/chain-stamps.md.
type Prove struct {
	Stamps StampStore
	Chain  Chain
	Out    io.Writer
}

// Run proves the commit of rig that commit names, in full or by its first
// characters. A commit nobody stamped is an error. A lookup that fails prints
// what the store holds first, and then fails.
func (p Prove) Run(ctx context.Context, rig, commit string) error {
	commit = strings.TrimSpace(commit)
	if rig == "" || commit == "" {
		return errors.New("prove needs a rig and a commit")
	}
	found, err := p.Stamps.FindSent(ctx, rig, commit)
	if err != nil {
		return fmt.Errorf("reading the sent stamps: %w", err)
	}
	if len(found) == 0 {
		return fmt.Errorf("no stamp for %s %s", rig, commit)
	}
	var lookupErr error
	for i, sent := range found {
		if i > 0 {
			fmt.Fprintln(p.Out)
		}
		if err := p.print(ctx, sent); err != nil {
			lookupErr = err
		}
	}
	return lookupErr
}

func (p Prove) print(ctx context.Context, sent SentStamp) error {
	s := sent.Stamp
	tx, err := p.Chain.Tx(ctx, sent.Txid)

	fmt.Fprintf(p.Out, "stamp      %s %s\n", s.Rig, s.Commit)
	fmt.Fprintf(p.Out, "txid       %s\n", sent.Txid)
	switch {
	case err != nil:
		fmt.Fprintf(p.Out, "block      lookup failed\n")
	case !tx.Known:
		fmt.Fprintf(p.Out, "block      not known to WhatsOnChain yet\n")
	case tx.Height == 0:
		fmt.Fprintf(p.Out, "block      in the mempool, no block yet\n")
	default:
		fmt.Fprintf(p.Out, "height     %d\n", tx.Height)
		fmt.Fprintf(p.Out, "time       %s\n", tx.Time.UTC().Format("2006-01-02 15:04:05 UTC"))
	}
	fmt.Fprintf(p.Out, "sent       %s\n", sent.SentAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(p.Out, "preimage   rig %q, commit %s\n", s.Rig, s.Commit)
	fmt.Fprintf(p.Out, "commitment %s (SHA-256 of the rig, a newline, the commit)\n", s.Commitment())
	fmt.Fprintf(p.Out, "explorer   %s%s\n", ExplorerURL, sent.Txid)
	if err != nil {
		return fmt.Errorf("asking WhatsOnChain about %s: %w", sent.Txid, err)
	}
	return nil
}

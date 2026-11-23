package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BatchNumberKey is the key a rig's last claimed batch number is kept under in
// the shared tracker's key-value store.
func BatchNumberKey(rig string) string { return "batch-number." + rig }

// batchNumberTries is how many times a landing that lost a number to another
// host's claim takes the next, before it numbers from its folders alone.
const batchNumberTries = 8

// BatchNumbers is how a rig's batches are numbered across hosts (mw-gq6.1665).
// A batch's folder is seen by the other hosts only when its landing commits
// it, after the whole gate; two hosts landing one rig meanwhile read the same
// highest folder and took the same number. The number is therefore published
// when it is taken, on the beads server every host shares: one key per rig
// holds the last number claimed, and a landing writes the next above it and
// above its own folders, waits Settle and reads it back, as BatchClaim does,
// taking the next again if another host's claim won.
//
// A store that cannot be read or written, or no store, numbers the batch from
// the folders alone, as it was done before, and Claim says so.
type BatchNumbers struct {
	Notes SlotNotes
	// Host is written in the claim, for whoever reads it.
	Host string
	// Settle is CrossSlotSettle when zero.
	Settle time.Duration
	// Now and Wait are the clock: the real one unless a test moves time.
	Now  func() time.Time
	Wait func(ctx context.Context, d time.Duration) error
}

// Claim reports the number above floor (the highest folder this checkout and the
// origin hold) that this landing may call its batch of rig, and, when it had to
// fall back to numbering from the folders alone, the line for the batch's notes.
func (c BatchNumbers) Claim(ctx context.Context, rig string, floor int) (int, string) {
	if c.Notes == nil {
		return floor + 1, ""
	}
	slot := CrossSlot{Host: c.Host, Settle: c.Settle, Now: c.Now, Wait: c.Wait}
	key := BatchNumberKey(rig)
	token := newSlotToken()
	fallback := func(why string) (int, string) {
		return floor + 1, fmt.Sprintf("the batch number could not be claimed on the beads server, so this batch is numbered from this checkout's folders alone "+
			"and may share its number with another host's batch: %s", why)
	}
	for try := 0; try < batchNumberTries; try++ {
		note, err := c.Notes.Note(ctx, key)
		if err != nil {
			return fallback(err.Error())
		}
		n := max(floor, claimedNumber(note)) + 1
		mine := fmt.Sprintf("%d %s %s %s", n, token, orHost(c.Host), slot.now().UTC().Format(time.RFC3339))
		if err := c.Notes.SetNote(ctx, key, mine); err != nil {
			return fallback(err.Error())
		}
		if err := slot.wait(ctx, slot.settle()); err != nil {
			return fallback(err.Error())
		}
		back, err := c.Notes.Note(ctx, key)
		if err != nil {
			return fallback(err.Error())
		}
		if claimToken(back) == token {
			return n, ""
		}
	}
	return fallback("another host kept taking the number")
}

// claimedNumber is the number a claim note holds, 0 for a note that holds none.
func claimedNumber(note string) int {
	fields := strings.Fields(note)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// claimToken is the token a claim note was written with.
func claimToken(note string) string {
	fields := strings.Fields(note)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func orHost(host string) string {
	if strings.TrimSpace(host) == "" {
		return "-"
	}
	return strings.Fields(host)[0]
}

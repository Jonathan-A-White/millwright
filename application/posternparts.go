package application

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A message in parts (postern's docs/protocol.md section 8, *A message in
// parts*, mw-fjpe9tx): the Governor's phone sends a message whose plaintext
// passes PosternPlaintextMax as several records naming one split, each its
// 1-based part of parts. Every reader of his messages reads it as one, as the
// PWA does (src/model/messageParts.ts): where the lowest part here stands, its
// text the parts' texts in order, a part not here yet named at the end.

// PosternPlaintextMax is the most plaintext, in bytes, one Postern record
// carries (postern's src/services/recordCap.ts).
const PosternPlaintextMax = 7200

// PosternPartsWait is how long after the first part of a message in parts the
// inbox holds it unread for the rest before it reads what came.
const PosternPartsWait = 10 * time.Minute

// PosternReadAheadKey is the note under which the inbox keeps the highest
// sequence number it has shown while its cursor was held below a message
// waiting for a part: what is at or below it, and is not that message, is not
// shown again.
const PosternReadAheadKey = "postern.inbox.readahead"

// missingPartLine is postern's own words for a part that has not come.
func missingPartLine(part, parts int) string {
	return fmt.Sprintf("Missing part %d of %d.", part, parts)
}

// joinPosternParts is mine, oldest first, with every message in parts as one
// message: its text the texts that came in part order, then a missingPartLine
// for each that did not; its txid, re, thread and files those of the lowest
// part that came; its Seq the highest of its records, so that it stands where
// the last of them did, and FirstSeq the lowest. It is Waiting while a part is
// missing and the first part came less than PosternPartsWait before now. Parts
// are joined by their split and their sender. grouped reports whether any
// message was in parts.
func joinPosternParts(mine []PosternInboxMessage, now time.Time) (joined []PosternInboxMessage, grouped bool) {
	groups := map[string][]PosternInboxMessage{}
	for _, m := range mine {
		if m.Split != "" {
			key := m.From + "\x00" + m.Split
			groups[key] = append(groups[key], m)
		}
	}
	if len(groups) == 0 {
		return mine, false
	}
	joined = make([]PosternInboxMessage, 0, len(mine))
	for _, m := range mine {
		if m.Split == "" {
			joined = append(joined, m)
			continue
		}
		group := groups[m.From+"\x00"+m.Split]
		if m.Seq == highestSeq(group) {
			joined = append(joined, joinGroup(group, now))
		}
	}
	return joined, true
}

func highestSeq(group []PosternInboxMessage) int64 {
	var high int64
	for _, m := range group {
		high = max(high, m.Seq)
	}
	return high
}

// joinGroup is group, the records of one split, as one message.
func joinGroup(group []PosternInboxMessage, now time.Time) PosternInboxMessage {
	byPart := map[int]PosternInboxMessage{}
	first := group[0].Seq
	earliest := group[0].Ts
	var txids []string
	for _, m := range group {
		if _, taken := byPart[m.Part]; !taken {
			byPart[m.Part] = m
		}
		first = min(first, m.Seq)
		if m.Ts.Before(earliest) {
			earliest = m.Ts
		}
		txids = append(txids, m.Txid)
	}
	parts := group[0].Parts
	lowest := parts + 1
	var text strings.Builder
	var missing []string
	for at := 1; at <= parts; at++ {
		if m, ok := byPart[at]; ok {
			text.WriteString(m.Text)
			lowest = min(lowest, at)
		} else {
			missing = append(missing, missingPartLine(at, parts))
		}
	}
	whole := byPart[lowest]
	whole.Seq, whole.FirstSeq, whole.PartTxids = highestSeq(group), first, txids
	whole.Split, whole.Part, whole.Parts = "", 0, 0
	whole.Text = text.String()
	if len(missing) > 0 {
		whole.Missing = strings.Join(missing, "\n")
		whole.Text += "\n\n" + whole.Missing
		whole.Waiting = !earliest.IsZero() && now.Sub(earliest) < PosternPartsWait
	}
	return whole
}

// firstSeq is the lowest sequence number among the records m was read from.
func (m PosternInboxMessage) firstSeq() int64 {
	if m.FirstSeq > 0 {
		return m.FirstSeq
	}
	return m.Seq
}

// heldBelow is the highest sequence number the inbox's cursor may be moved to
// while mine holds a message waiting for a part: just below the lowest of its
// records; held is false when none waits.
func heldBelow(mine []PosternInboxMessage) (below int64, held bool) {
	for _, m := range mine {
		if m.Waiting && (!held || m.firstSeq()-1 < below) {
			below, held = m.firstSeq()-1, true
		}
	}
	return below, held
}

// dropShown is mine less the messages an earlier read already showed while its
// cursor was held below a waiting message: whatever is at or below shown, is
// not waiting itself, and did not grow a part since. A message whose wait is
// over with a part still missing stays: it is read, and applied, now.
func dropShown(mine []PosternInboxMessage, shown int64) []PosternInboxMessage {
	kept := mine[:0:0]
	for _, m := range mine {
		if !m.Waiting && m.Missing == "" && m.Seq <= shown {
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// readAhead is the note PosternReadAheadKey, 0 when it holds none.
func (i PosternInbox) readAhead(ctx context.Context) (int64, error) {
	saved, err := i.Memory.Note(ctx, PosternReadAheadKey)
	if err != nil || saved == "" {
		return 0, err
	}
	shown, err := strconv.ParseInt(saved, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the postern inbox read-ahead is %q, not a whole number: %w", saved, err)
	}
	return shown, nil
}

// printWaiting prints m, a message waiting for a part, and does nothing else
// with it: it is neither kept, applied nor held for Ideas only until it is
// whole. While Ideas only is on, a message of the Governor's is not shown as
// text, so only the wait is said.
func (i PosternInbox) printWaiting(ctx context.Context, m PosternInboxMessage, p *ideasPass) error {
	if i.Ideas != nil && m.Verified && i.isGovernor(m) {
		if err := i.ideasLoad(ctx, p); err != nil {
			return err
		}
		if p.state.OnAt(i.Ideas.now()) {
			i.printf("%s is arriving in parts and is kept for Ideas only once whole: %s\n", orUnknown(m.Txid), strings.ReplaceAll(m.Missing, "\n", " "))
			return nil
		}
	}
	i.printf("%s\n%s\n", i.messageHeader(m), m.Text)
	return nil
}

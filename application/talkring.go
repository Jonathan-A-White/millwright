package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// TalkRingKey is the note holding the ring still open: the seq of its
// emergency event and the Unix second it rang. mw talk call writes it, and
// mw talk wait clears it when the Governor answers or TalkRingFor passes.
const TalkRingKey = "talk.call.ring"

// TalkRingFor is how long a ring stays an emergency on the Governor's phone
// unanswered: the first mw talk wait to find it older writes the event that
// ends it.
const TalkRingFor = 15 * time.Minute

type talkRingNote struct {
	Seq uint64 `json:"seq"`
	At  int64  `json:"at"`
}

// ringEventText is the text of a ring's emergency event, and so of the banner
// on the Governor's phone: who calls and why, in words, never the txid.
func ringEventText(reason string) string {
	return events.CutDetail("The Mayor is calling: " + strings.Join(strings.Fields(reason), " "))
}

// keepRing remembers the ring written as event seq, for its end to name.
func keepRing(ctx context.Context, notes PosternNotes, seq uint64, at time.Time) error {
	value, err := json.Marshal(talkRingNote{Seq: seq, At: at.Unix()})
	if err != nil {
		return err
	}
	return notes.SetNote(ctx, TalkRingKey, string(value))
}

// endRing writes the one event in the normal lane that ends the ring still
// open, naming its seq in clears, and forgets it. With no ring open, or
// (when staleOnly) one still inside TalkRingFor at now, it writes nothing.
// The note is cleared only once the event is written, so one that could not
// be written is tried again.
func endRing(ctx context.Context, log EventLog, notes PosternNotes, now time.Time, actor, why string, staleOnly bool) error {
	if log == nil || notes == nil {
		return nil
	}
	value, err := notes.Note(ctx, TalkRingKey)
	if err != nil {
		return fmt.Errorf("reading the open ring: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var ring talkRingNote
	if json.Unmarshal([]byte(value), &ring) != nil || ring.Seq == 0 {
		return notes.ClearNote(ctx, TalkRingKey)
	}
	if staleOnly && now.Sub(time.Unix(ring.At, 0)) < TalkRingFor {
		return nil
	}
	_, err = EventEmit{
		Log: log, Now: func() time.Time { return now },
		Event: events.Event{Kind: events.KindMessage, Actor: actor, Detail: why, Clears: ring.Seq},
	}.Run(ctx)
	if err != nil {
		return fmt.Errorf("writing the end of the ring: %w", err)
	}
	return notes.ClearNote(ctx, TalkRingKey)
}

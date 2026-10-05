package application

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// TalkLastKey is the note TalkWait keeps what it last heard of the Governor's
// talk records under: TalkLast's JSON. It is how the rest of the factory, which
// does not hold the stream open, tells whether a Talk is open.
const TalkLastKey = "postern.talk.last"

// TalkQuietSpell is how long after the Governor's last turn a Talk that was never
// ended is still taken to be open: the Talk screen's own, postern's TALK_STALE_MS.
const TalkQuietSpell = 30 * time.Minute

// TalkLast is the Governor's newest talk record as TalkWait heard it: which talk,
// its role (TalkRoleTurn or TalkRoleEnd) and when, in Unix seconds.
type TalkLast struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	At   int64  `json:"at"`
}

// talkOpen reports whether a Talk is open at now: the Governor's newest talk
// record is a turn, not an End, and no more than TalkQuietSpell old. A Talk the
// Mayor ended by mw talk say --end is not seen here and reads as open until the
// quiet spell is over; no note, or one that does not read, is no Talk.
func talkOpen(ctx context.Context, notes PosternNotes, now time.Time) (bool, error) {
	raw, err := notes.Note(ctx, TalkLastKey)
	if err != nil {
		return false, err
	}
	var last TalkLast
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &last) != nil {
		return false, nil
	}
	if last.Role == TalkRoleEnd {
		return false, nil
	}
	return now.Sub(time.Unix(last.At, 0)) <= TalkQuietSpell, nil
}

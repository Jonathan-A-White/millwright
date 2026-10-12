package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Ideas only is a mode the Governor puts his own key in, on the postern
// backend (postern's docs/protocol.md section 25): until a time or on a weekly
// schedule he is away on purpose. The backend pushes him nothing but a ring
// and an emergency; the factory runs on. While it is on mw keeps every record
// of his instead of acting on it, and says so in his thread at zero tokens.

// The things that can turn Ideas only on, as the backend names them.
const (
	IdeasOnlyByUntil    = "until"
	IdeasOnlyBySchedule = "schedule"
)

// IdeasOnlyNow is what the backend says of a key's Ideas only now: GET
// /api/ideas-only/{pubkey}'s state, and the time zone of the setting's
// weekly schedule when it has one.
type IdeasOnlyNow struct {
	On bool
	// Until is the Unix second the mode ends, the nearer of the setting's end
	// and the schedule window's; 0 when it is off.
	Until int64
	// By is IdeasOnlyByUntil or IdeasOnlyBySchedule when on, "" when off.
	By string
	// Tz is the IANA zone of the setting's schedule, "" when it has none.
	Tz string
}

// IdeasOnlyReader is the port to the backend's Ideas only route, read with
// the Mayor's key. The real adapter is infrastructure/postern's HTTP; the fake
// is apptest.FakePostern.
type IdeasOnlyReader interface {
	// IdeasOnly reports the state of the key pubkey (compressed, hex) now.
	IdeasOnly(ctx context.Context, pubkey string) (IdeasOnlyNow, error)
}

// IdeasOnlyState is mw's note of the Governor's Ideas only on this host, the
// file ~/.local/state/mw/postern/ideas-only/state.json: kept fresh from the
// backend by every pass that looks, and by the stream's ideas-only event.
type IdeasOnlyState struct {
	On bool `json:"on"`
	// Until is the Unix second the mode ends, 0 when off.
	Until int64 `json:"until"`
	// Since is the Unix second this host first saw the mode on.
	Since int64 `json:"since"`
	// ReleasedUntil is the Until of the last mode whose ideas were given back
	// to him: what makes the release once-only.
	ReleasedUntil int64 `json:"released_until"`
	// Tz is the zone of his schedule, "" when he has none.
	Tz string `json:"tz,omitempty"`
	// Ended is the Unix second the last mode ended, 0 while it is on or when
	// this host never saw it on: the end of its window when mw saw the clock
	// pass it, else the moment mw heard it had been turned off.
	Ended int64 `json:"ended,omitempty"`
	// Releasing is the txid of the card that gives his ideas back, from the
	// moment it is sent until the release is done, so that a release that died
	// part way finishes without sending a second card.
	Releasing string `json:"releasing,omitempty"`
}

// EndedAt is when the mode that is not on at now ended: the end its note names
// when the clock passed it, else the moment mw heard it turned off, else now.
func (s IdeasOnlyState) EndedAt(now time.Time) time.Time {
	switch {
	case s.On && s.Until != 0 && now.Unix() >= s.Until:
		return time.Unix(s.Until, 0)
	case s.Ended != 0:
		return time.Unix(s.Ended, 0)
	}
	return now
}

// OnAt reports whether the mode is on at now: a note that says on is not
// believed past its end, so that a host that missed the closing event does not
// go on holding what he says.
func (s IdeasOnlyState) OnAt(now time.Time) bool {
	return s.On && (s.Until == 0 || now.Unix() < s.Until)
}

// IdeasOnlyHeld is one record of his that mw is keeping while the mode is on:
// one line of held.jsonl.
type IdeasOnlyHeld struct {
	Txid  string `json:"txid"`
	Seq   int64  `json:"seq"`
	Class string `json:"class"`
	// Channel is where he sent it: "general", a bead's id, or a named channel;
	// "talk" for a Talk turn and "call" for a call request.
	Channel string `json:"channel"`
	// Text is the first 80 runes of his words, or what a tap, a voice note or
	// a file was.
	Text string `json:"text"`
	// Attachment is where the file he sent was written, when he sent one.
	Attachment string `json:"attachment,omitempty"`
	// When is when the record was held, RFC 3339 in UTC.
	When string `json:"when"`
}

// IdeasOnlyHeldWidth is how many runes of his words a held line keeps.
const IdeasOnlyHeldWidth = 80

// IdeasOnlyFiles is the port to the two host files of Ideas only: the state
// and the held records. The real adapter is infrastructure/ideasonly; the fake
// is apptest.FakeIdeasOnlyFiles.
type IdeasOnlyFiles interface {
	// State reports the state file; the zero state when there is none.
	State(ctx context.Context) (IdeasOnlyState, error)
	SetState(ctx context.Context, state IdeasOnlyState) error
	// Held reports every held record, oldest first.
	Held(ctx context.Context) ([]IdeasOnlyHeld, error)
	// Hold appends one record. A txid already held is not appended twice.
	Hold(ctx context.Context, held IdeasOnlyHeld) error
	// Release puts the held records away, as the ideas of the mode that ended
	// at the given time: held.jsonl moves to released/<time>.jsonl. Nothing
	// held is nothing to put away.
	Release(ctx context.Context, ended time.Time) error
}

// IdeasOnlyMode is how the use cases know whether the Governor is in Ideas
// only: the state file, refreshed from the backend when a Reader is set.
type IdeasOnlyMode struct {
	Reader IdeasOnlyReader
	Files  IdeasOnlyFiles
	// GovernorKey is whose mode it is — config postern_governor_key.
	GovernorKey string
	// Now is the clock; the zero value reads the real one.
	Now func() time.Time
}

func (m IdeasOnlyMode) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Refresh asks the backend for the mode and keeps the state file level with
// it. A backend that cannot be asked is an error, but the state returned is
// still the last one the file knew, so that a pass can go on with that.
func (m IdeasOnlyMode) Refresh(ctx context.Context) (IdeasOnlyState, error) {
	stored, err := m.Files.State(ctx)
	if err != nil {
		return IdeasOnlyState{}, err
	}
	if m.Reader == nil || strings.TrimSpace(m.GovernorKey) == "" {
		return stored, nil
	}
	asked, err := m.Reader.IdeasOnly(ctx, m.GovernorKey)
	if err != nil {
		return stored, fmt.Errorf("asking the postern backend whether Ideas only is on: %w", err)
	}
	return m.keep(ctx, stored, asked.On, asked.Until, asked.Tz)
}

// Apply keeps the state file level with an ideas-only event of the stream. An
// event for another key changes nothing.
func (m IdeasOnlyMode) Apply(ctx context.Context, event PosternEvent) (IdeasOnlyState, error) {
	stored, err := m.Files.State(ctx)
	if err != nil {
		return IdeasOnlyState{}, err
	}
	if !strings.EqualFold(event.Key, m.GovernorKey) {
		return stored, nil
	}
	return m.keep(ctx, stored, event.On, event.Until, stored.Tz)
}

// keep writes the mode as it now stands, when that is not what the file has.
func (m IdeasOnlyMode) keep(ctx context.Context, stored IdeasOnlyState, on bool, until int64, tz string) (IdeasOnlyState, error) {
	fresh := stored
	fresh.On, fresh.Until, fresh.Tz = on, until, tz
	if !on {
		fresh.Until = 0
		if stored.On {
			fresh.Ended = stored.EndedAt(m.now()).Unix()
		}
	}
	if on {
		fresh.Ended = 0
	}
	if on && !stored.OnAt(m.now()) {
		fresh.Since = m.now().Unix()
	}
	if fresh == stored {
		return stored, nil
	}
	if err := m.Files.SetState(ctx, fresh); err != nil {
		return stored, err
	}
	return fresh, nil
}

// Line is the one line mw status and mw brief show while the mode is on, "" when
// it is off or cannot be told. It asks the backend for at most
// IdeasOnlyLineWait, and goes on with the state file when that is not enough.
func (m IdeasOnlyMode) Line(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, IdeasOnlyLineWait)
	defer cancel()
	state, _ := m.Refresh(ctx)
	if !state.OnAt(m.now()) {
		return ""
	}
	held, err := m.Files.Held(ctx)
	if err != nil {
		return ""
	}
	return "Governor: " + IdeasOnlyBanner(state, len(held), m.now())
}

// IdeasOnlyLineWait is the most mw status and mw brief wait on the backend to
// say whether the Governor is in Ideas only.
const IdeasOnlyLineWait = 5 * time.Second

// IdeasOnlyBanner is "Ideas only until 21:00 UTC (2 ideas held)".
func IdeasOnlyBanner(state IdeasOnlyState, held int, now time.Time) string {
	return fmt.Sprintf("Ideas only until %s (%s)", IdeasOnlyUntilLabel(state, now), IdeasHeldPhrase(held))
}

// IdeasHeldPhrase is "1 idea held", "0 ideas held", "3 ideas held".
func IdeasHeldPhrase(n int) string {
	if n == 1 {
		return "1 idea held"
	}
	return fmt.Sprintf("%d ideas held", n)
}

// IdeasOnlyUntilLabel is when the mode ends, in UTC and, when his schedule
// names a zone, in his: "21:00 UTC", "21:00 UTC (17:00 America/New_York)". A
// time on another day than now's carries its date. The phone's own zone is not
// known here, so UTC is always said.
func IdeasOnlyUntilLabel(state IdeasOnlyState, now time.Time) string {
	if state.Until == 0 {
		return "the end of Ideas only"
	}
	end := time.Unix(state.Until, 0)
	label := clockLabel(end, time.UTC, now) + " UTC"
	if state.Tz != "" {
		if zone, err := time.LoadLocation(state.Tz); err == nil && zone != time.UTC {
			label += fmt.Sprintf(" (%s %s)", clockLabel(end, zone, now), state.Tz)
		}
	}
	return label
}

// clockLabel is t in zone as "15:04", with the date first when it is not now's
// date there.
func clockLabel(t time.Time, zone *time.Location, now time.Time) string {
	t, now = t.In(zone), now.In(zone)
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("2006-01-02 15:04")
}

// IdeasOnlyReceipt is what the Governor is told in the thread of every record
// of his that is kept: "Recorded for after 21:00 UTC".
func IdeasOnlyReceipt(state IdeasOnlyState, now time.Time) string {
	return "Recorded for after " + IdeasOnlyUntilLabel(state, now)
}

// PosternHeldPrefix is the prefix of every note PosternHeldKey writes.
const PosternHeldPrefix = "postern.held."

// PosternHeldKey is the note key a record's txid is marked under once mw is
// keeping it for after Ideas only. Its value is the one line mw postern inbox
// shows in the record's place.
func PosternHeldKey(txid string) string { return PosternHeldPrefix + txid }

// heldNotes reads every PosternHeldKey note at once, txid to line.
func heldNotes(ctx context.Context, notes PosternNotes) (map[string]string, error) {
	found, err := notes.NotesWithPrefix(ctx, PosternHeldPrefix)
	if err != nil {
		return nil, fmt.Errorf("reading which postern records are held for Ideas only: %w", err)
	}
	held := make(map[string]string, len(found))
	for key, line := range found {
		held[strings.TrimPrefix(key, PosternHeldPrefix)] = line
	}
	return held, nil
}

// keepIdea appends idea to the held file and marks its txid held, returning the
// line the inbox shows in its place. The file goes first and is idempotent per
// txid, so a pass that dies between the two keeps it once.
func (m IdeasOnlyMode) keepIdea(ctx context.Context, notes PosternNotes, state IdeasOnlyState, idea IdeasOnlyHeld) (string, error) {
	if err := m.Files.Hold(ctx, idea); err != nil {
		return "", fmt.Errorf("keeping %s for after Ideas only: %w", idea.Txid, err)
	}
	line := fmt.Sprintf("held for after %s: %s", IdeasOnlyUntilLabel(state, m.now()), idea.Txid)
	if err := notes.SetNote(ctx, PosternHeldKey(idea.Txid), line); err != nil {
		return "", fmt.Errorf("marking %s held: %w", idea.Txid, err)
	}
	return line, nil
}

// heldWords is the first IdeasOnlyHeldWidth runes of text on one line.
func heldWords(text string) string {
	words := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(words) <= IdeasOnlyHeldWidth {
		return words
	}
	return string([]rune(words)[:IdeasOnlyHeldWidth])
}

// IdeasOnlyShow is mw ideas-only: whether the Governor is in Ideas only, until
// when, and the records mw is keeping for him.
type IdeasOnlyShow struct {
	Mode IdeasOnlyMode
	// Out gets the report; Err what went wrong on the way and was gone on from.
	Out io.Writer
	Err io.Writer
}

// IdeasOnlyReport is what mw ideas-only found.
type IdeasOnlyReport struct {
	State IdeasOnlyState
	// On is whether the mode is on now.
	On   bool
	Held []IdeasOnlyHeld
	Now  time.Time
}

// String is the report as it is printed: 'off' or 'on until 21:00 UTC', how
// many ideas are held, and a line for each.
func (r IdeasOnlyReport) String() string {
	var b strings.Builder
	if r.On {
		fmt.Fprintf(&b, "on until %s\n", IdeasOnlyUntilLabel(r.State, r.Now))
	} else {
		b.WriteString("off\n")
	}
	if len(r.Held) == 0 && !r.On {
		return b.String()
	}
	b.WriteString(IdeasHeldPhrase(len(r.Held)) + "\n")
	for _, held := range r.Held {
		fmt.Fprintf(&b, "  %s  %s  %s  %s  %s\n", held.Txid, held.Channel, held.Class, held.When, held.Text)
	}
	return b.String()
}

// Run asks the backend, which it forgives for being out of reach, and prints
// what it knows.
func (s IdeasOnlyShow) Run(ctx context.Context) (IdeasOnlyReport, error) {
	if s.Mode.Files == nil {
		return IdeasOnlyReport{}, fmt.Errorf("mw ideas-only: nowhere to read the state of Ideas only from")
	}
	state, err := s.Mode.Refresh(ctx)
	if err != nil && s.Err != nil {
		fmt.Fprintf(s.Err, "mw ideas-only: %v; showing what was last known\n", err)
	}
	held, herr := s.Mode.Files.Held(ctx)
	if herr != nil {
		return IdeasOnlyReport{}, herr
	}
	report := IdeasOnlyReport{State: state, On: state.OnAt(s.Mode.now()), Held: held, Now: s.Mode.now()}
	if s.Out != nil {
		fmt.Fprint(s.Out, report.String())
	}
	return report, nil
}

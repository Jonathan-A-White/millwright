package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// talkRecordClass is the clear class of a talk turn, postern's
// docs/protocol.md section 20.
const talkRecordClass = "talk"

// TalkWaitCursorKey is the note key TalkWait keeps its cursor under: the
// highest sequence number of every record it has paged past. It is its own, so
// that the postern inbox's cursor and this one never move each other.
const TalkWaitCursorKey = "postern.talk.cursor"

// TalkWaitMailKey is the note key holding the ids of the Deputy's mail to the
// Mayor already reported, one to a line, so a message is reported once.
const TalkWaitMailKey = "postern.talk.mail"

// DefaultTalkWaitLimit is how long mw talk wait waits when nothing sets its
// limit: contrib/mail-wait's own.
const DefaultTalkWaitLimit = 3000 * time.Second

// DefaultTalkWaitMinBackoff and DefaultTalkWaitMaxBackoff bound the pause
// before TalkWait opens a stream again: it doubles from the first to the second.
const (
	DefaultTalkWaitMinBackoff = 500 * time.Millisecond
	DefaultTalkWaitMaxBackoff = 15 * time.Second
)

// PosternEvent is one event of the postern backend's event stream, GET
// /api/events (postern's docs/api.md): a hello, sent once on connect, with the
// index's latest sequence number as Seq; a message, a record was indexed, with
// its sequence number as Seq. The adapter reports no other kind.
type PosternEvent struct {
	Kind string
	Seq  int64
}

// The kinds of PosternEvent.
const (
	PosternEventHello   = "hello"
	PosternEventMessage = "message"
)

// PosternStream is the port to the postern backend's event stream. The real
// adapter is infrastructure/postern's HTTP; there is no fake, the feature runs
// the real adapter against a backend that speaks server-sent events.
type PosternStream interface {
	// Events holds the stream open and calls onEvent for each event, in order,
	// until onEvent returns an error (which Events returns as it is), the
	// connection ends or fails (an error saying so), or ctx is done (ctx's
	// error). It never reports a clean end: a stream that closes is an error,
	// for the caller to open again.
	Events(ctx context.Context, onEvent func(PosternEvent) error) error
}

// TalkTurn is the plaintext of a talk record, postern's docs/protocol.md
// section 20.
type TalkTurn struct {
	Talk struct {
		ID   string `json:"id"`
		Turn int    `json:"turn"`
	} `json:"talk"`
	Text  string `json:"text"`
	Role  string `json:"role"`
	Model string `json:"model"`
	Cut   bool   `json:"cut"`
	// Links are bead ids the Governor may want to open from the answer, sent
	// beside the text and never in it. Absent when there are none.
	Links []string `json:"links,omitempty"`
}

// The roles of a talk record.
const (
	TalkRoleTurn = "turn"
	TalkRoleEnd  = "end"
)

// TalkWait is a zero-token wait for the Governor's next word in a talk, for
// the Mayor's harness to run in the background. It holds the event stream open
// and, on a message event, pages the records since its own cursor. It ends at
// the first talk record the Governor sent the Mayor's key — a turn, or the end
// of the talk — printing it, the time from the event to the print, and any new
// mail the Deputy sent the Mayor. It ends on Limit with nothing to say. It
// spends no model: it is the Mayor's harness that wakes on its exit.
//
// With HoldingReply set it also answers a turn (not an end) the moment it is
// heard, before printing it, with that text as the section 20 holding answer
// mw talk say --holding sends: the Governor hears something at once, and the
// Mayor's real answer follows.
type TalkWait struct {
	Stream  PosternStream
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile
	// Memory keeps the cursor and the Deputy mail already reported.
	Memory PosternNotes
	// Mailbox is read for the Deputy's mail to the Mayor; it marks nothing
	// read. Nil reports none.
	Mailbox Mailbox

	// GovernorKey is the Governor's compressed public key, hex — config
	// postern_governor_key. Only a verified record from it ends the wait.
	GovernorKey string

	// HoldingReply is the holding answer sent at once to a Governor turn,
	// config talk_holding_reply; empty sends none.
	HoldingReply string

	// Limit is how long to wait; zero is DefaultTalkWaitLimit. MinBackoff and
	// MaxBackoff bound the pause before the stream is opened again; zero is
	// each one's default.
	Limit      time.Duration
	MinBackoff time.Duration
	MaxBackoff time.Duration

	// Now is the clock; the zero value reads the real one.
	Now func() time.Time

	// Out gets what the wait ends on. Err gets what went wrong on the way and
	// was waited out: a stream that dropped, a record that would not decrypt.
	Out io.Writer
	Err io.Writer
}

// TalkWaitReport is how a wait ended: Turn is nil when it ended on its limit.
type TalkWaitReport struct {
	Turn *TalkTurn
	// IndexToPrint is the time from the event that told the wait of the turn
	// to the moment it printed it.
	IndexToPrint time.Duration
	// HoldingSent is whether the holding reply went out, and HoldingElapsed
	// how long sending it took.
	HoldingSent    bool
	HoldingElapsed time.Duration
	// Mail is the Deputy's new mail to the Mayor.
	Mail []Message
}

// errTalkTurnHeard ends the stream once the turn is in hand.
var errTalkTurnHeard = errors.New("the Governor's turn was heard")

// Run waits for the Governor's next talk record, or the limit.
func (w TalkWait) Run(ctx context.Context) (TalkWaitReport, error) {
	if err := w.wired(); err != nil {
		return TalkWaitReport{}, err
	}
	limit := w.Limit
	if limit <= 0 {
		limit = DefaultTalkWaitLimit
	}
	minBackoff, maxBackoff := w.MinBackoff, w.MaxBackoff
	if minBackoff <= 0 {
		minBackoff = DefaultTalkWaitMinBackoff
	}
	if maxBackoff < minBackoff {
		maxBackoff = max(minBackoff, DefaultTalkWaitMaxBackoff)
	}
	started := w.now()
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	cursor, known, err := w.readCursor(ctx)
	if err != nil {
		return TalkWaitReport{}, err
	}
	run := &talkWaitRun{TalkWait: w, ctx: ctx, cursor: cursor, known: known, saved: -1}
	if known {
		run.saved = cursor
	}

	backoff := minBackoff
	for {
		err := w.Stream.Events(ctx, run.onEvent)
		switch {
		case errors.Is(err, errTalkTurnHeard):
			return run.finish(ctx)
		case ctx.Err() != nil:
			if err := run.save(context.WithoutCancel(ctx)); err != nil {
				return TalkWaitReport{}, err
			}
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return TalkWaitReport{}, ctx.Err()
			}
			w.printf(w.Out, "No talk turn by %s; the wait ended on its time limit (%s). Arm it again.\n",
				w.now().UTC().Format(time.RFC3339), w.now().Sub(started).Round(time.Second))
			return TalkWaitReport{}, nil
		}
		w.printf(w.Err, "mw talk wait: %v; opening the stream again in %s\n", err, backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		if run.connected {
			backoff = minBackoff
		} else {
			backoff = min(backoff*2, maxBackoff)
		}
		run.connected = false
	}
}

// talkWaitRun is what one Run carries from stream to stream.
type talkWaitRun struct {
	TalkWait
	ctx    context.Context
	cursor int64
	// known is whether the cursor has ever been set, here or on an earlier
	// run; a first run takes the stream's head as its cursor.
	known bool
	// saved is the cursor as it stands in Memory, -1 when it holds none.
	saved     int64
	connected bool

	turn      *TalkTurn
	turnSeq   int64
	heardAt   time.Time
	printedAt time.Time
}

// onEvent handles one event: a hello puts a first run's cursor at the head,
// and, like a message, pages the records since the cursor.
func (r *talkWaitRun) onEvent(event PosternEvent) error {
	arrived := r.now()
	switch event.Kind {
	case PosternEventHello:
		r.connected = true
		if !r.known {
			r.cursor, r.known = event.Seq, true
			return nil
		}
	case PosternEventMessage:
	default:
		return nil
	}
	return r.page(arrived)
}

// page reads the records since the cursor, oldest first, moving the cursor past
// each, and stops at the first the Governor sent the Mayor.
func (r *talkWaitRun) page(arrived time.Time) error {
	records, err := r.Postern.Messages(r.ctx, r.cursor)
	if err != nil {
		// Waited out, like a dropped stream: the next event pages from the
		// same cursor again.
		r.printf(r.Err, "mw talk wait: reading the records since %d: %v\n", r.cursor, err)
		return nil
	}
	pubKey, _, err := r.Keys.PublicKey()
	if err != nil {
		return err
	}
	privKey, err := r.Keys.PrivateKeyWIF()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Seq <= r.cursor {
			continue
		}
		if record.Class == talkRecordClass && record.To == pubKey {
			turn, ok := r.governorsTurn(record, privKey)
			if ok {
				r.turn, r.turnSeq, r.heardAt = turn, record.Seq, arrived
				r.cursor = record.Seq
				return errTalkTurnHeard
			}
		}
		r.cursor = record.Seq
	}
	return nil
}

// governorsTurn reads record as a talk record the Governor sent: its envelope's
// sender, the one the cipher binds, is the Governor's key, and so is what the
// payload and the signer say. A record that is not, or is not a turn or an
// end, is not the Governor's word.
func (r *talkWaitRun) governorsTurn(record PosternRecord, privKey string) (*TalkTurn, bool) {
	text, envelopeFrom, err := r.Cipher.Decrypt(privKey, record.Ciphertext)
	if err != nil {
		r.printf(r.Err, "mw talk wait: record %d (%s) would not decrypt: %v\n", record.Seq, record.Txid, err)
		return nil, false
	}
	from, verified, _ := posternVerifySender(envelopeFrom, record.From, record.Signer)
	if !verified || from != r.GovernorKey {
		return nil, false
	}
	var turn TalkTurn
	if json.Unmarshal([]byte(text), &turn) != nil || turn.Talk.ID == "" {
		return nil, false
	}
	if turn.Role != TalkRoleTurn && turn.Role != TalkRoleEnd {
		return nil, false
	}
	return &turn, true
}

// finish prints the turn, then the Deputy's mail, and saves the cursor.
func (r *talkWaitRun) finish(ctx context.Context) (TalkWaitReport, error) {
	report := TalkWaitReport{Turn: r.turn}
	report.HoldingSent, report.HoldingElapsed = r.hold(ctx)
	r.printedAt = r.now()
	report.IndexToPrint = r.printedAt.Sub(r.heardAt)
	cut := "no"
	if r.turn.Cut {
		cut = "yes"
	}
	model := r.turn.Model
	if model == "" {
		model = "unchanged"
	}
	r.printf(r.Out, "talk %s turn %d (role %s)\nmodel %s\ncut %s\ntext: %s\nindex-to-print %d ms\n",
		r.turn.Talk.ID, r.turn.Talk.Turn, r.turn.Role, model, cut, r.turn.Text, report.IndexToPrint.Milliseconds())
	if report.HoldingSent {
		r.printf(r.Out, "holding sent in %d ms\n", report.HoldingElapsed.Milliseconds())
	}

	ctx = context.WithoutCancel(ctx)
	if err := r.save(ctx); err != nil {
		return report, err
	}
	mail, err := r.deputyMail(ctx)
	if err != nil {
		r.printf(r.Err, "mw talk wait: the Deputy's mail to the Mayor: %v\n", err)
	}
	report.Mail = mail
	return report, nil
}

// hold sends the holding reply to the turn in hand, when there is one to send
// and the turn is a turn rather than the end of the talk. A holding reply that
// would not go is said on Err and the turn is printed all the same: the
// Mayor's real answer is what matters.
func (r *talkWaitRun) hold(ctx context.Context) (sent bool, elapsed time.Duration) {
	if strings.TrimSpace(r.HoldingReply) == "" || r.turn.Role != TalkRoleTurn {
		return false, 0
	}
	started := r.now()
	_, err := TalkSay{
		Postern: r.Postern, Cipher: r.Cipher, Keys: r.Keys, GovernorKey: r.GovernorKey, Now: r.Now,
	}.Run(context.WithoutCancel(ctx), TalkSayRequest{
		Text: r.HoldingReply, TalkID: r.turn.Talk.ID, Turn: r.turn.Talk.Turn, Holding: true,
	})
	if err != nil {
		r.printf(r.Err, "mw talk wait: the holding reply: %v\n", err)
		return false, 0
	}
	return true, r.now().Sub(started)
}

// save moves the stored cursor to the one in hand.
func (r *talkWaitRun) save(ctx context.Context) error {
	if !r.known || r.cursor == r.saved {
		return nil
	}
	if err := r.Memory.SetNote(ctx, TalkWaitCursorKey, strconv.FormatInt(r.cursor, 10)); err != nil {
		return fmt.Errorf("saving the talk cursor: %w", err)
	}
	r.saved = r.cursor
	return nil
}

// deputyMail prints the Deputy's mail to the Mayor not yet reported, and
// remembers every id of the Deputy's now in the inbox, so that an id that has
// left it (read) is forgotten and one still in it is reported once.
func (r *talkWaitRun) deputyMail(ctx context.Context) ([]Message, error) {
	if r.Mailbox == nil {
		return nil, nil
	}
	inbox, err := r.Mailbox.Inbox(ctx, "mayor")
	if err != nil {
		return nil, err
	}
	reported, err := r.Memory.Note(ctx, TalkWaitMailKey)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, id := range strings.Fields(reported) {
		known[id] = true
	}
	var fresh []Message
	var current []string
	for _, message := range inbox {
		if message.From != DeputySeat && !strings.HasPrefix(message.From, DeputySeat+"@") {
			continue
		}
		current = append(current, message.ID)
		if !known[message.ID] {
			fresh = append(fresh, message)
		}
	}
	if len(fresh) == 0 && len(current) == len(known) {
		return nil, nil
	}
	if len(fresh) > 0 {
		r.printf(r.Out, "new mail from the Deputy to the Mayor: %d message(s)\n", len(fresh))
		for _, message := range fresh {
			r.printf(r.Out, "  %s from %s: %s\n    %s\n", message.ID, message.From, message.Subject,
				strings.ReplaceAll(strings.TrimSpace(message.Body), "\n", "\n    "))
		}
	}
	if len(current) == 0 {
		return fresh, r.Memory.ClearNote(ctx, TalkWaitMailKey)
	}
	return fresh, r.Memory.SetNote(ctx, TalkWaitMailKey, strings.Join(current, "\n"))
}

// readCursor is the stored cursor; known is false when there is none.
func (w TalkWait) readCursor(ctx context.Context) (cursor int64, known bool, err error) {
	saved, err := w.Memory.Note(ctx, TalkWaitCursorKey)
	if err != nil {
		return 0, false, err
	}
	if saved == "" {
		return 0, false, nil
	}
	cursor, err = strconv.ParseInt(saved, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("the talk cursor is %q, not a whole number: %w", saved, err)
	}
	return cursor, true, nil
}

func (w TalkWait) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w TalkWait) printf(out io.Writer, format string, args ...any) {
	if out != nil {
		fmt.Fprintf(out, format, args...)
	}
}

// wired reports what mw talk wait is missing before it can wait.
func (w TalkWait) wired() error {
	switch {
	case w.Stream == nil || w.Postern == nil:
		return fmt.Errorf("mw talk wait: no postern backend is configured")
	case w.Cipher == nil:
		return fmt.Errorf("mw talk wait: no cipher is configured")
	case w.Keys == nil:
		return fmt.Errorf("mw talk wait: no postern key file is configured")
	case w.Memory == nil:
		return fmt.Errorf("mw talk wait: nowhere to remember its cursor")
	case strings.TrimSpace(w.GovernorKey) == "":
		return fmt.Errorf("mw talk wait: no Governor's key is configured (postern_governor_key), so no turn could be his")
	}
	return nil
}

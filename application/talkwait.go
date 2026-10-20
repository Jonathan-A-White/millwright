package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// talkRecordClass is the clear class of a talk turn, postern's
// docs/protocol.md section 20.
const talkRecordClass = "talk"

// TalkWaitCursorKey is the note key TalkWait keeps its cursor under: the
// highest sequence number of every record it has paged past. It is its own, so
// that the postern inbox's cursor and this one never move each other.
const TalkWaitCursorKey = "postern.talk.cursor"

// TalkWaitChannelKey is the note key holding how the Governor's newest talk or
// call record came: PosternChannelChain when its txid is bare, and
// PosternChannelDirect when it is "direct:<id>". mw talk call reads it, to
// answer a record that came by chain on chain too.
const TalkWaitChannelKey = "postern.talk.channel"

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
// its sequence number as Seq; an ideas-only, a key's Ideas only turned on or
// off or its end moved, with the Key, whether it is On and, if so, the Unix
// second it runs Until. The adapter reports no other kind.
type PosternEvent struct {
	Kind string
	Seq  int64

	Key   string
	On    bool
	Until int64
}

// The kinds of PosternEvent.
const (
	PosternEventHello     = "hello"
	PosternEventMessage   = "message"
	PosternEventIdeasOnly = "ideas-only"
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
	// Part and Parts say that this record is part Part of Parts of one turn,
	// sent in parts because the whole is over what one record holds; both are
	// zero for a turn sent whole.
	Part  int `json:"part,omitempty"`
	Parts int `json:"parts,omitempty"`
	// Links are bead ids the Governor may want to open from the answer, sent
	// beside the text and never in it. Absent when there are none.
	Links []string `json:"links,omitempty"`
	// About is what the talk was opened from, a card, a thread or a prompt:
	// the Mayor's first answer is about it. Nil when the record carries none.
	About *TalkAbout `json:"about,omitempty"`
	// Attachment is a picture the Governor sent with the turn, a blob of the
	// same shape as a message's file (section 8). Nil when there is none.
	Attachment *PosternAttachment `json:"attachment,omitempty"`
}

// TalkAbout is the optional about field of a talk record: the kind of thing the
// talk was opened from ("bead"), its id and its title.
type TalkAbout struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
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
// of the talk — or the first call record he sent it — a request, or a later on
// a ring — printing it, the time from the event to the print, and any new
// mail the Deputy sent the Mayor. It ends on Limit with nothing to say. It
// spends no model: it is the Mayor's harness that wakes on its exit.
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

	// Transcriber, when set, hears a voice note before its notice is printed
	// (bounded by TalkWaitHearLimit), so that the notice carries the words
	// (mw-gq6.301). AttachmentDir is where the decrypted audio is written.
	// Nil prints "words to follow" for a note whose transcript is not known.
	Transcriber   PosternTranscriber
	AttachmentDir string

	// Threads, when set, is told the channel of each message the wait prints
	// and of the post it answers, so that the answer line printed under it,
	// mw postern send --re, finds the root's channel (mw-gq6.435). Nil
	// remembers none.
	Threads PosternThreadIndex

	// Log, when set, is the home's event log, which the wait looks at once a
	// second, spending no token, for the Mayor's handover (mw-jrx0s.11). A
	// handover to another window than Self ends the wait at once with
	// "handed over at N": the old Mayor answers nothing after N, so a turn
	// the wait had not yet handed on is left unprinted and the talk cursor
	// unmoved, for the successor to read from. Nil is no look.
	Log EventLog
	// Self is the name of the window the wait runs in, empty when it is not
	// known: a wait that does not know its window is the old Mayor's.
	Self string
	// HandoverEvery is the wait between two looks at the log; zero is
	// DefaultEventWaitEvery.
	HandoverEvery time.Duration

	// GovernorKey is the Governor's compressed public key, hex — config
	// postern_governor_key. Only a verified record from it ends the wait.
	GovernorKey string

	// Ideas, when set, is the Governor's Ideas only: while it is on, a turn, a
	// call request or a message of his is kept in the held file and answered
	// 'Recorded for after <time>' at zero tokens, and the wait goes on. The wait
	// says it is on as it starts and as it ends on its limit, and keeps the
	// mode's note level from the stream's ideas-only events. Nil is no such mode.
	Ideas *IdeasOnlyMode
	// Tracker, when set, tells the wait which beads are the Governor's to-dos:
	// what he sends about one is not kept as an idea, and so ends the wait as
	// it would outside Ideas only. Nil keeps every message of his.
	Tracker WorkTracker
	// Release, when set, gives the Governor's ideas back when Ideas only ends,
	// by the stream's event or by the clock: the wait then ends printing them.
	// Nil leaves them held, and the wait on.
	Release *IdeasOnlyRelease
	// Lock, when set, is taken around keeping one record, so that the wait and
	// the postern inbox's hook never keep, or answer, the same one together.
	Lock HostLock

	// PartsWait is how long after the first part of a turn sent in parts the
	// wait holds for the rest before it prints what came; zero is
	// DefaultTalkWaitPartsWait.
	PartsWait time.Duration

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

// TalkWaitReport is how a wait ended: Turn and Call are both nil when it ended
// on its limit.
type TalkWaitReport struct {
	Turn *TalkTurn
	// Call is the Governor's call record the wait ended on, a request or a
	// later; CallTxid is its txid.
	Call     *CallRecord
	CallTxid string
	// IndexToPrint is the time from the event that told the wait of the turn
	// to the moment it printed it.
	IndexToPrint time.Duration
	// HandedOver is the handover that ended the wait, nil when it ended on
	// anything else.
	HandedOver *events.Handover
	// Mail is the Deputy's new mail to the Mayor.
	Mail []Message
	// Postern is the new postern messages for the Mayor's key the wait ended
	// on, or that came with the turn it ended on.
	Postern []PosternInboxMessage
	// Released is the Governor's ideas the wait gave back when Ideas only
	// ended, nil when it ended on anything else.
	Released *IdeasOnlyReleased
}

// errIdeasEnded ends the stream, or is the cause the wait's context is ended
// with, once Ideas only has ended and its ideas have been given back.
var errIdeasEnded = errors.New("Ideas only ended and its ideas were given back")

// errTalkHeard ends the stream once there is something to report: the
// Governor's turn, or a new postern message.
var errTalkHeard = errors.New("the Governor's turn or a new postern message was heard")

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
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)

	watch := HandoverWatch{Log: w.Log, Seat: MayorSeat, Self: w.Self}
	var logStart uint64
	if w.Log != nil {
		head, err := w.Log.Head(ctx)
		if err != nil {
			return TalkWaitReport{}, err
		}
		logStart = head
		go watchHandover(ctx, watch, head, w.HandoverEvery, stop)
	}

	cursor, known, err := w.readCursor(ctx)
	if err != nil {
		return TalkWaitReport{}, err
	}
	run := &talkWaitRun{TalkWait: w, ctx: ctx, cursor: cursor, known: known, saved: -1, watch: watch, logStart: logStart}
	if known {
		run.saved = cursor
	}
	defer func() {
		if run.partsTimer != nil {
			run.partsTimer.Stop()
		}
	}()
	run.ideasStart(ctx)
	if run.ideasDue(ctx) && run.giveBackIdeas(false) {
		return run.ideasEnded(ctx)
	}
	if run.Release != nil && run.Ideas != nil {
		go run.watchIdeasEnd(ctx, stop)
	}

	backoff := minBackoff
	for {
		if run.partsLate() && run.hearParts(w.now()) {
			return run.finish(ctx)
		}
		streamCtx, endStream := context.WithCancelCause(ctx)
		run.watchStream(endStream)
		err := w.Stream.Events(streamCtx, run.onEvent)
		endStream(nil)
		if ctx.Err() == nil && !errors.Is(err, errTalkHeard) && !errors.Is(err, errIdeasEnded) && run.partsLate() {
			// The rest of a turn sent in parts did not come in time.
			if run.hearParts(w.now()) {
				return run.finish(ctx)
			}
			continue
		}
		switch {
		case errors.Is(err, errTalkHeard):
			return run.finish(ctx)
		case errors.Is(err, errIdeasEnded):
			return run.ideasEnded(ctx)
		case ctx.Err() != nil:
			if errors.Is(context.Cause(ctx), errIdeasEnded) {
				return run.ideasEnded(ctx)
			}
			var handed *talkHandedOver
			if errors.As(context.Cause(ctx), &handed) {
				return run.handedOver(handed.h), nil
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) && run.parts != nil && run.Ideas == nil {
				// The limit came with a turn half heard: say what came.
				if run.hearParts(w.now()) {
					return run.finish(ctx)
				}
			}
			if err := run.save(context.WithoutCancel(ctx)); err != nil {
				return TalkWaitReport{}, err
			}
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return TalkWaitReport{}, ctx.Err()
			}
			w.printf(w.Out, "No talk turn by %s; the wait ended on its time limit (%s). Arm it again.\n",
				w.now().UTC().Format(time.RFC3339), w.now().Sub(started).Round(time.Second))
			run.ideasEnd(ctx)
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

// talkHandedOver is why a wait's context was ended: the seat was handed over.
type talkHandedOver struct{ h events.Handover }

func (e *talkHandedOver) Error() string { return HandedOverLine(e.h) }

// watchHandover looks at the log every `every` for a handover that ends this
// window's session, and ends ctx with it as the cause when it finds one. It
// reads the head only, until the head has moved.
func watchHandover(ctx context.Context, watch HandoverWatch, since uint64, every time.Duration, stop context.CancelCauseFunc) {
	if every <= 0 {
		every = DefaultEventWaitEvery
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
		head, err := watch.Log.Head(ctx)
		if err != nil || head <= since {
			continue
		}
		evs, err := watch.Log.Since(ctx, since)
		if err != nil {
			continue
		}
		if len(evs) > 0 {
			since = evs[len(evs)-1].Seq
		}
		if _, h, ok := watch.Ends(evs); ok {
			stop(&talkHandedOver{h})
			return
		}
	}
}

// talkWaitRun is what one Run carries from stream to stream.
type talkWaitRun struct {
	TalkWait
	// watch and logStart are the look at the log for a handover, and the
	// head it began at.
	watch    HandoverWatch
	logStart uint64
	ctx      context.Context
	cursor   int64
	// known is whether the cursor has ever been set, here or on an earlier
	// run; a first run takes the stream's head as its cursor.
	known bool
	// saved is the cursor as it stands in Memory, -1 when it holds none.
	saved     int64
	connected bool

	// ideaState is the Governor's Ideas only as the wait last knew it; the
	// stream's events and the clock's watch both read and write it, under ideaMu.
	ideaMu    sync.Mutex
	ideaState IdeasOnlyState
	// gaveBack is the release that ended the wait, set before it is ended.
	gaveBack *IdeasOnlyReleased

	// turn is the Governor's turn the wait ends on, nil when it ends on new
	// postern messages alone; posts are those messages, the ones the wait
	// paged past before it reached the turn included.
	turn    *TalkTurn
	turnSeq int64
	// missing is the lines saying which parts of the turn did not come, ""
	// when it came whole.
	missing  string
	turnTxid string
	call     *CallRecord
	callTxid string
	// channel is how the Governor's record the wait ends on came, empty when
	// it ends on none.
	channel   string
	posts     []PosternInboxMessage
	heardAt   time.Time
	printedAt time.Time

	// parts is the turn sent in parts that the wait is still hearing, nil
	// when it is hearing none; partsTimer is the PartsWait that ends it, and
	// partsDue and endStream, under partsMu, are what the timer sets and ends.
	parts      *talkParts
	partsTimer *time.Timer
	partsMu    sync.Mutex
	partsDue   bool
	endStream  context.CancelCauseFunc
}

// errTalkPartsDue ends the stream when the rest of a turn sent in parts did
// not come within PartsWait.
var errTalkPartsDue = errors.New("the rest of a turn sent in parts did not come in time")

// watchStream says which stream the parts timer ends; a timer that fired
// before there was one ends this one at once.
func (r *talkWaitRun) watchStream(end context.CancelCauseFunc) {
	r.partsMu.Lock()
	defer r.partsMu.Unlock()
	r.endStream = end
	if r.partsDue {
		end(errTalkPartsDue)
	}
}

// partsLate reports whether the timer of the turn being heard in parts has fired.
func (r *talkWaitRun) partsLate() bool {
	r.partsMu.Lock()
	defer r.partsMu.Unlock()
	return r.parts != nil && r.partsDue
}

// armParts starts the wait for the rest of a turn whose first part has come.
func (r *talkWaitRun) armParts() {
	wait := r.PartsWait
	if wait <= 0 {
		wait = DefaultTalkWaitPartsWait
	}
	r.partsTimer = time.AfterFunc(wait, func() {
		r.partsMu.Lock()
		defer r.partsMu.Unlock()
		r.partsDue = true
		if r.endStream != nil {
			r.endStream(errTalkPartsDue)
		}
	})
}

// hearParts makes of the parts that have come the Governor's turn, whole or,
// with the lines for what is missing, half; it reports whether the wait ends on
// it. A turn kept for after Ideas only is not heard, and the wait goes on.
func (r *talkWaitRun) hearParts(arrived time.Time) bool {
	heard := r.parts
	r.parts = nil
	r.partsTimer.Stop()
	r.partsMu.Lock()
	r.partsDue = false
	r.partsMu.Unlock()
	turn := heard.joined()
	if r.keepTurn(heard.first, turn) {
		return false
	}
	r.turn, r.turnSeq, r.turnTxid, r.heardAt = turn, heard.first.Seq, heard.first.Txid, arrived
	r.channel = recordChannel(heard.first.Txid)
	r.missing = heard.missingLines()
	return true
}

// onEvent handles one event: a hello puts a first run's cursor at the head,
// and, like a message, pages the records since the cursor.
func (r *talkWaitRun) onEvent(event PosternEvent) error {
	arrived := r.now()
	switch event.Kind {
	case PosternEventIdeasOnly:
		was := r.ideasOn()
		r.ideasEvent(event)
		if was && !r.ideasOn() && r.Release != nil && r.giveBackIdeas(false) {
			return errIdeasEnded
		}
		return nil
	case PosternEventHello:
		r.connected = true
		if r.Ideas != nil && r.known {
			// A stream opened again may have missed the mode turning on or off.
			was := r.ideasOn()
			r.refreshIdeas(r.ctx)
			if was && !r.ideasOn() && r.Release != nil && r.giveBackIdeas(false) {
				return errIdeasEnded
			}
		}
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
// each, and stops at the first talk record the Governor sent the Mayor. Along
// the way it notes each postern message for the Mayor's key that the postern
// inbox has not read; if the page ends on no turn, those messages end the wait,
// and a turn found on the page always wins over them.
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
	inboxCursor := int64(-1) // read once, when a record that could be unread mail turns up
	for _, record := range records {
		if record.Seq <= r.cursor {
			continue
		}
		if record.Class == callRecordClass && record.To == pubKey {
			if call, ok := r.governorsCall(record, privKey); ok {
				if r.keepCall(record, call) {
					r.cursor = record.Seq
					continue
				}
				r.call, r.callTxid, r.heardAt = call, record.Txid, arrived
				r.channel = recordChannel(record.Txid)
				r.cursor = record.Seq
				return errTalkHeard
			}
		} else if record.Class == talkRecordClass && record.To == pubKey {
			turn, ok := r.governorsTurn(record, privKey)
			if ok {
				if r.parts != nil && !r.parts.is(turn) && r.hearParts(arrived) {
					// Another turn begins: the half heard is the Governor's word first.
					r.cursor = record.Seq - 1
					return errTalkHeard
				}
				if turn.inParts() {
					if r.parts == nil {
						r.parts = newTalkParts(record, turn)
						r.armParts()
					}
					r.parts.add(turn)
					r.cursor = record.Seq
					if r.parts.complete() && r.hearParts(arrived) {
						return errTalkHeard
					}
					continue
				}
				if r.keepTurn(record, turn) {
					r.cursor = record.Seq
					continue
				}
				r.turn, r.turnSeq, r.turnTxid, r.heardAt = turn, record.Seq, record.Txid, arrived
				r.channel = recordChannel(record.Txid)
				r.cursor = record.Seq
				return errTalkHeard
			}
		} else if record.Class != talkRecordClass && record.To == pubKey {
			if inboxCursor < 0 {
				if inboxCursor, err = r.inboxCursor(); err != nil {
					return err
				}
			}
			if record.Seq > inboxCursor {
				if message, ok := r.unreadMessage(record, privKey); ok && !r.keepsMessage(message) {
					r.posts = append(r.posts, message)
					if err := rememberChannelOf(r.Threads, message); err != nil {
						r.printf(r.Err, "mw talk wait: %v\n", err)
					}
				}
			}
		}
		r.cursor = record.Seq
	}
	if len(r.posts) > 0 && r.parts == nil {
		r.heardAt = arrived
		return errTalkHeard
	}
	return nil
}

// recordChannel is how a record came, by its txid: a direct one is
// "direct:<id>", one from the chain a bare txid.
func recordChannel(txid string) string {
	if strings.HasPrefix(txid, "direct:") {
		return PosternChannelDirect
	}
	return PosternChannelChain
}

// inboxCursor is the postern inbox's own cursor, the mark of what mw postern
// inbox has read: a message at or below it is read. It is only read, never
// moved. Zero when the inbox has never read.
func (r *talkWaitRun) inboxCursor() (int64, error) {
	saved, err := r.Memory.Note(r.ctx, PosternCursorKey)
	if err != nil {
		return 0, err
	}
	if saved == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseInt(saved, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("the postern inbox cursor is %q, not a whole number: %w", saved, err)
	}
	return cursor, nil
}

// unreadMessage reads record, a non-talk record for the Mayor's key, as the
// postern inbox would. One that would not decrypt is said on Err and passed
// over, as a talk record that would not is.
func (r *talkWaitRun) unreadMessage(record PosternRecord, privKey string) (PosternInboxMessage, bool) {
	text, envelopeFrom, err := r.Cipher.Decrypt(privKey, record.Ciphertext)
	if err != nil {
		r.printf(r.Err, "mw talk wait: record %d (%s) would not decrypt: %v\n", record.Seq, record.Txid, err)
		return PosternInboxMessage{}, false
	}
	from, verified, signerChecked := posternVerifySender(envelopeFrom, record.From, record.Signer)
	thread, display, threadIsBead, attachment, attachments := posternThreadAndText(record.Class, text)
	return PosternInboxMessage{
		Seq: record.Seq, Txid: record.Txid, Class: record.Class,
		From: from, Verified: verified, SignerChecked: signerChecked,
		Ts: record.Ts, Text: display, Thread: thread, ThreadIsBead: threadIsBead,
		Attachment: attachment, Attachments: attachments, Re: posternReOf(text),
	}, true
}

// governorsTurn reads record as a talk record the Governor sent: its envelope's
// sender, the one the cipher binds, is the Governor's key, and so is what the
// payload and the signer say. A record that is not, or is not a turn or an
// end, is not the Governor's word.
func (r *talkWaitRun) governorsTurn(record PosternRecord, privKey string) (*TalkTurn, bool) {
	text, ok := r.governorsPlaintext(record, privKey)
	if !ok {
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

// governorsPlaintext decrypts record and returns its plaintext when its
// envelope's sender, the one the cipher binds, is the Governor's key, and so is
// what the payload and the signer say. One that would not decrypt is said on
// Err and passed over.
func (r *talkWaitRun) governorsPlaintext(record PosternRecord, privKey string) (string, bool) {
	text, envelopeFrom, err := r.Cipher.Decrypt(privKey, record.Ciphertext)
	if err != nil {
		r.printf(r.Err, "mw talk wait: record %d (%s) would not decrypt: %v\n", record.Seq, record.Txid, err)
		return "", false
	}
	from, verified, _ := posternVerifySender(envelopeFrom, record.From, record.Signer)
	if !verified || from != r.GovernorKey {
		return "", false
	}
	return text, true
}

// governorsCall reads record as a call record the Governor sent: a request
// with its words, or a later naming the ring it puts off. Anything else, a
// ring included, is not his word to the Mayor.
func (r *talkWaitRun) governorsCall(record PosternRecord, privKey string) (*CallRecord, bool) {
	text, ok := r.governorsPlaintext(record, privKey)
	if !ok {
		return nil, false
	}
	var call CallRecord
	if json.Unmarshal([]byte(text), &call) != nil {
		return nil, false
	}
	switch call.Role {
	case CallRoleRequest:
		if call.At == 0 {
			call.At = record.Ts.Unix()
		}
	case CallRoleLater:
		if call.RingTxid == "" {
			return nil, false
		}
	default:
		return nil, false
	}
	return &call, true
}

// finish prints the turn or the call, if there is one, then the new postern messages and the
// Deputy's mail, and saves the cursor.
func (r *talkWaitRun) finish(ctx context.Context) (TalkWaitReport, error) {
	// A turn the Governor sent after the handover is the successor's, even
	// when the look at the log has not come round to it yet.
	if r.Log != nil {
		if h, ok, err := r.watch.Since(context.WithoutCancel(ctx), r.logStart); err == nil && ok {
			return r.handedOver(h), nil
		}
	}
	report := TalkWaitReport{Turn: r.turn, Call: r.call, CallTxid: r.callTxid, Postern: r.posts}
	if r.call != nil {
		if r.call.Role == CallRoleLater {
			r.printf(r.Out, "later %s\n", r.call.RingTxid)
		} else {
			r.printf(r.Out, "call %s at %s: %s\n", r.callTxid, time.Unix(r.call.At, 0).UTC().Format(time.RFC3339), r.call.Text)
		}
	}
	if r.turn != nil {
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
		switched := ""
		if line, ok := respawnMayorLine(domain.Model(r.turn.Model)); ok {
			switched = line + "\n"
		}
		about := ""
		if a := r.turn.About; a != nil && (a.Kind != "" || a.ID != "" || a.Title != "") {
			about = strings.TrimSpace(fmt.Sprintf("about: %s %s %s", a.Kind, a.ID, a.Title)) + "\n"
		}
		// The picture is fetched after the index-to-print time is taken, so
		// that the time still measures the wake and not the blob.
		saved := r.turnSaved()
		image := r.turnImage(ctx)
		r.printf(r.Out, "talk %s turn %d (role %s)\nmodel %s\n%scut %s\ntext: %s\n%s%s%s%sindex-to-print %d ms\n",
			r.turn.Talk.ID, r.turn.Talk.Turn, r.turn.Role, model, switched, cut, r.turn.Text, saved, image, r.missing, about, report.IndexToPrint.Milliseconds())
	}
	if len(r.posts) > 0 {
		r.printf(r.Out, "new postern message: %d unread, read them with mw postern inbox\n", len(r.posts))
		for _, message := range r.posts {
			re := ""
			if label := message.reLabel(); label != "" {
				re = ", " + label
			}
			r.printf(r.Out, "  %s, txid %s%s: %s\n", message.channelLabel(), message.Txid, re, r.postNotice(ctx, message))
			if answer := message.answerLine(); answer != "" {
				r.printf(r.Out, "    %s\n", answer)
			}
		}
	}

	ctx = context.WithoutCancel(ctx)
	if err := r.save(ctx); err != nil {
		return report, err
	}
	if r.channel != "" {
		if err := r.Memory.SetNote(ctx, TalkWaitChannelKey, r.channel); err != nil {
			return report, fmt.Errorf("saving the channel the Governor's record came by: %w", err)
		}
	}
	if r.turn != nil {
		// Best effort: the swap that reads it takes a Talk it cannot read for no Talk.
		last, err := json.Marshal(TalkLast{ID: r.turn.Talk.ID, Role: r.turn.Role, At: r.now().Unix()})
		if err == nil {
			err = r.Memory.SetNote(ctx, TalkLastKey, string(last))
		}
		if err != nil {
			r.printf(r.Err, "mw talk wait: keeping the talk's state: %v\n", err)
		}
	}
	mail, err := r.deputyMail(ctx)
	if err != nil {
		r.printf(r.Err, "mw talk wait: the Deputy's mail to the Mayor: %v\n", err)
	}
	report.Mail = mail
	return report, nil
}

// TalkWaitImageLimit bounds how long the wait spends fetching a turn's
// picture before it prints the turn without it.
const TalkWaitImageLimit = 30 * time.Second

// turnImage is the line printed under the turn's text for its picture, ""
// when it carries none: "image: <path> (<name>)" once the picture is written
// to AttachmentDir under the record's txid, as mw postern inbox writes a
// message's, for the Mayor to look at before answering; a file already there
// is not fetched again. A picture that cannot be had is "image: could not be
// fetched (<reason>)", and the turn is printed all the same.
func (r *talkWaitRun) turnImage(ctx context.Context) string {
	inbox := PosternInbox{Postern: r.Postern, Cipher: r.Cipher, Keys: r.Keys, AttachmentDir: r.AttachmentDir}
	return inbox.talkImageLine(ctx, r.turn.Attachment, r.turnTxid)
}

// talkImageLine is turnImage for the picture of the turn recorded under txid.
func (inbox PosternInbox) talkImageLine(ctx context.Context, picture *PosternAttachment, txid string) string {
	if picture == nil {
		return ""
	}
	if inbox.AttachmentDir == "" || txid == "" {
		return "image: could not be fetched (no postern inbox dir or no txid to name it by)\n"
	}
	name := posternFileName(txid)
	path := inbox.attachmentPath(picture, name)
	if _, err := os.Stat(path); err != nil {
		// The turn is heard: the wait's own limit does not cut the fetch short.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), TalkWaitImageLimit)
		defer cancel()
		if path, _, err = inbox.downloadAttachment(fetchCtx, picture, name); err != nil {
			return fmt.Sprintf("image: could not be fetched (%v)\n", err)
		}
	}
	if label := posternSafeFileName(picture.Name); label != "" {
		path += " (" + label + ")"
	}
	return "image: " + path + "\n"
}

// turnSaved writes the turn's whole text to AttachmentDir as <txid>.txt, beside
// its picture, and returns the line printed under the text: "saved: <path>",
// so that a print that is cut, or an output file written over, never loses a
// turn (mw-gq6.515). "" when there is no dir or no txid to name the file by; a
// file that cannot be written is "saved: could not be written (<reason>)".
func (r *talkWaitRun) turnSaved() string {
	if r.AttachmentDir == "" || r.turnTxid == "" {
		return ""
	}
	path := filepath.Join(r.AttachmentDir, posternFileName(r.turnTxid)+".txt")
	err := os.MkdirAll(r.AttachmentDir, 0o755)
	if err == nil {
		err = os.WriteFile(path, []byte(r.turn.Text), 0o644)
	}
	if err != nil {
		return fmt.Sprintf("saved: could not be written (%v)\n", err)
	}
	return "saved: " + path + "\n"
}

// TalkShow prints every turn of one talk the Governor sent the Mayor's key, read
// from the backend, whole: its role, turn, time, text and picture path. It never
// cuts a turn. The Mayor's own answers are sealed to the Governor's key, so the
// Mayor cannot read them back and they are not shown.
type TalkShow struct {
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile

	// GovernorKey is the Governor's compressed public key, hex.
	GovernorKey string
	// AttachmentDir is where a turn's picture is written, as the wait does.
	AttachmentDir string

	// Out gets the turns. Err gets a record that would not decrypt.
	Out io.Writer
	Err io.Writer
}

// Run prints the turns of talk id, oldest first. A talk with none to show is
// an error.
func (s TalkShow) Run(ctx context.Context, id string) error {
	if s.Postern == nil || s.Cipher == nil || s.Keys == nil {
		return fmt.Errorf("mw talk show: no postern backend, cipher or key file is configured")
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("mw talk show: which talk? give its id")
	}
	records, err := s.Postern.Messages(ctx, 0)
	if err != nil {
		return fmt.Errorf("mw talk show: reading the records: %w", err)
	}
	pubKey, _, err := s.Keys.PublicKey()
	if err != nil {
		return err
	}
	privKey, err := s.Keys.PrivateKeyWIF()
	if err != nil {
		return err
	}
	// The wait's own reading of a record decides which are the Governor's.
	reader := &talkWaitRun{TalkWait: TalkWait{Cipher: s.Cipher, GovernorKey: s.GovernorKey, Err: s.Err}}
	inbox := PosternInbox{Postern: s.Postern, Cipher: s.Cipher, Keys: s.Keys, AttachmentDir: s.AttachmentDir}
	// A turn sent in parts is shown once, at its first part, joined; a part
	// that never came is said.
	type shownTurn struct {
		record PosternRecord
		turn   *TalkTurn
		parts  *talkParts
	}
	var turns []shownTurn
	halves := map[int]*talkParts{}
	for _, record := range records {
		if record.Class != talkRecordClass || record.To != pubKey {
			continue
		}
		turn, ok := reader.governorsTurn(record, privKey)
		if !ok || turn.Talk.ID != id {
			continue
		}
		if !turn.inParts() {
			turns = append(turns, shownTurn{record: record, turn: turn})
			continue
		}
		heard := halves[turn.Talk.Turn]
		if heard == nil || !heard.is(turn) {
			heard = newTalkParts(record, turn)
			halves[turn.Talk.Turn] = heard
			turns = append(turns, shownTurn{record: record, parts: heard})
		}
		heard.add(turn)
	}
	shown := 0
	for _, t := range turns {
		turn, missing := t.turn, ""
		if t.parts != nil {
			turn, missing = t.parts.joined(), t.parts.missingLines()
		}
		shown++
		fmt.Fprintf(s.Out, "talk %s turn %d (role %s) at %s\ntext: %s\n%s%s",
			turn.Talk.ID, turn.Talk.Turn, turn.Role, t.record.Ts.UTC().Format(time.RFC3339), turn.Text,
			inbox.talkImageLine(ctx, turn.Attachment, t.record.Txid), missing)
	}
	if shown == 0 {
		return fmt.Errorf("mw talk show: no turn of talk %s that the Governor sent this key", id)
	}
	return nil
}

// handedOver is how a wait ends on the seat's handover: it says so, and moves
// no cursor, so that what the Governor sent past N is read by the successor.
func (r *talkWaitRun) handedOver(h events.Handover) TalkWaitReport {
	r.printf(r.Out, "%s\n", HandedOverLine(h))
	return TalkWaitReport{HandedOver: &h}
}

// postFirstLine is the first line of text that has any words, "(no text)" when
// none does: a post that is only a file.
func postFirstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "(no text)"
}

// TalkWaitHearLimit bounds how long the wait spends hearing one voice note
// before it prints the notice without the words.
const TalkWaitHearLimit = 60 * time.Second

// voiceNoteToFollow is the notice of a voice note whose words are not known.
const voiceNoteToFollow = "voice note (words to follow by mail Voice: ...; read with mw postern inbox)"

// postNotice is what the notice of m says of it: its first line of words, or,
// for a post carrying audio, "voice note" with the transcript in quotes when
// it is known (the inbox heard it, or the wait hears it now) and "words to
// follow" when it is not, then any typed words as "with the words: <text>".
func (r *talkWaitRun) postNotice(ctx context.Context, m PosternInboxMessage) string {
	var audio *PosternAttachment
	for _, file := range m.files() {
		if file != nil && isPosternAudio(file.Mime) {
			audio = file
			break
		}
	}
	if audio == nil {
		return postFirstLine(m.Text)
	}
	notice := voiceNoteToFollow
	if words := r.voiceTranscript(ctx, m, audio); words != "" {
		notice = fmt.Sprintf("voice note: %q", words)
	}
	if typed := strings.TrimSpace(m.Text); typed != "" {
		notice += " with the words: " + postFirstLine(typed)
	}
	return notice
}

// voiceTranscript is the words of m's voice note, "" when they are not known:
// from the inbox's applied note if it has heard it, else heard now when the
// wait has a transcriber, within TalkWaitHearLimit.
func (r *talkWaitRun) voiceTranscript(ctx context.Context, m PosternInboxMessage, audio *PosternAttachment) string {
	if m.Txid != "" {
		note, _ := r.Memory.Note(ctx, PosternAppliedKey(m.Txid))
		_, words, ok := strings.Cut(note, " txid "+m.Txid+": ")
		if ok && strings.HasPrefix(note, "applied voice ") && !strings.HasPrefix(words, "[audio:") && strings.TrimSpace(words) != "" {
			return strings.TrimSpace(words)
		}
	}
	if r.Transcriber == nil || r.AttachmentDir == "" {
		return ""
	}
	hearCtx, cancel := context.WithTimeout(ctx, TalkWaitHearLimit)
	defer cancel()
	inbox := PosternInbox{Postern: r.Postern, Cipher: r.Cipher, Keys: r.Keys, AttachmentDir: r.AttachmentDir}
	path, _, err := inbox.downloadAttachment(hearCtx, audio, posternFileName(m.Txid))
	if err != nil {
		r.printf(r.Err, "mw talk wait: voice note %s was not downloaded: %v\n", m.Txid, err)
		return ""
	}
	heard, err := r.Transcriber.Transcribe(hearCtx, path)
	if err != nil {
		r.printf(r.Err, "mw talk wait: voice note %s was not heard: %v\n", m.Txid, err)
		return ""
	}
	return strings.Join(strings.Fields(heard), " ")
}

// save moves the stored cursor to the one in hand.
func (r *talkWaitRun) save(ctx context.Context) error {
	// A turn only half heard is not read: its first part is where the next run starts.
	cursor := r.cursor
	if r.parts != nil {
		cursor = min(cursor, r.parts.first.Seq-1)
	}
	if !r.known || cursor == r.saved {
		return nil
	}
	if err := r.Memory.SetNote(ctx, TalkWaitCursorKey, strconv.FormatInt(cursor, 10)); err != nil {
		return fmt.Errorf("saving the talk cursor: %w", err)
	}
	r.saved = cursor
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

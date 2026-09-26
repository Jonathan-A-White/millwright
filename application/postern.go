package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// PosternKeyFile is where the Mayor's postern key lives on this host: a
// generated testnet secp256k1 key, kept host-local outside the vault and its
// backups, like .mayor-acting. Path, Exists, Generate and PublicKey are for a
// person to read (mw postern key init/show) and never hand back the private
// key. PrivateKeyWIF and Sign do use it, for the use cases that must sign or
// decrypt with it, and neither prints what it touches.
type PosternKeyFile interface {
	// Path reports where the key file is, for a message a person reads.
	Path() string
	// Exists reports whether the key file is already there.
	Exists() (bool, error)
	// Generate makes a fresh testnet key and writes it to the key file, 0600.
	// It must only be called once Exists has said false.
	Generate() error
	// PublicKey reads the key file and reports its compressed public key, as
	// hex, and its testnet address.
	PublicKey() (pubKeyHex string, address string, err error)
	// PrivateKeyWIF reads the key file and reports its private key,
	// WIF-encoded, so that Inbox can decrypt a message addressed to it.
	PrivateKeyWIF() (string, error)
	// Sign builds a record transaction, postern's docs/protocol.md section
	// 4 — utxos spent as its inputs, but a 1-satoshi one; output 0 carrying
	// payload as the postern's on-chain record; output 1 paying 1 satoshi to
	// the postern anchor; output 2 the change back to this key's own address
	// — and reports it signed, as raw transaction hex ready to broadcast.
	Sign(utxos []PosternUtxo, payload []byte) (rawtx string, err error)
}

// PosternKeyInit makes the Mayor's postern key, once. It refuses to
// overwrite a key that is already there, so a second init changes nothing.
type PosternKeyInit struct {
	Keys PosternKeyFile
	Out  io.Writer
}

// PosternKeyInitReport is what init made.
type PosternKeyInitReport struct {
	Path string
}

func (r PosternKeyInitReport) String() string {
	return "Wrote a testnet postern key to " + r.Path
}

// Run makes the key, refusing when one is already there.
func (i PosternKeyInit) Run(_ context.Context) (PosternKeyInitReport, error) {
	exists, err := i.Keys.Exists()
	if err != nil {
		return PosternKeyInitReport{}, err
	}
	if exists {
		return PosternKeyInitReport{}, fmt.Errorf("a postern key already exists at %s: mw postern key init never overwrites one", i.Keys.Path())
	}
	if err := i.Keys.Generate(); err != nil {
		return PosternKeyInitReport{}, err
	}
	report := PosternKeyInitReport{Path: i.Keys.Path()}
	fmt.Fprintln(i.Out, report.String())
	return report, nil
}

// PosternKeyShow prints the postern key's public half: never the private key.
type PosternKeyShow struct {
	Keys PosternKeyFile
	Out  io.Writer
}

// PosternKeyShowReport is what show printed.
type PosternKeyShowReport struct {
	PublicKey string
	Address   string
}

func (r PosternKeyShowReport) String() string {
	return fmt.Sprintf("public key: %s\ntestnet address: %s", r.PublicKey, r.Address)
}

// Run reports the key's public key and testnet address, refusing when there
// is no key yet.
func (s PosternKeyShow) Run(_ context.Context) (PosternKeyShowReport, error) {
	exists, err := s.Keys.Exists()
	if err != nil {
		return PosternKeyShowReport{}, err
	}
	if !exists {
		return PosternKeyShowReport{}, fmt.Errorf("no postern key at %s: run mw postern key init first", s.Keys.Path())
	}
	pubKeyHex, address, err := s.Keys.PublicKey()
	if err != nil {
		return PosternKeyShowReport{}, err
	}
	report := PosternKeyShowReport{PublicKey: pubKeyHex, Address: address}
	fmt.Fprintln(s.Out, report.String())
	return report, nil
}

// PosternRecord is one message record as the postern backend indexes it from
// the chain: still encrypted, whoever it is addressed to.
type PosternRecord struct {
	Seq        int64
	Txid       string
	Class      string
	From       string // sender's compressed public key, hex, as the payload itself claims — anyone's to write, never trusted alone
	To         string // recipient's compressed public key, hex
	Ts         time.Time
	Ciphertext string // base64, as it travels on chain

	// Signer is the compressed public key, hex, that signed the funding
	// transaction's input, when the postern backend supplies enough to
	// recover it (its scriptSig, or the raw transaction). Empty when it does
	// not, so it is never compared — an unchecked signer is not evidence for
	// or against a record's sender.
	Signer string
}

// PosternUtxo is one unspent output the postern key's balance is built from.
type PosternUtxo struct {
	Txid     string
	Vout     int
	Satoshis int64
}

// Postern is the port to the postern backend: reading indexed message
// records since a sequence number, and reading and spending the postern
// key's testnet balance. The real adapter, infrastructure/postern's HTTP,
// talks to the Go backend's HTTP API (docs/api.md,
// github.com/Jonathan-A-White/postern).
type Postern interface {
	// Messages reports every indexed record after sequence number since,
	// oldest first.
	Messages(ctx context.Context, since int64) ([]PosternRecord, error)
	// Utxos reports address's unspent outputs.
	Utxos(ctx context.Context, address string) ([]PosternUtxo, error)
	// Balance reports address's balance, in satoshis, confirmed and
	// unconfirmed together — what the float cap is measured against.
	Balance(ctx context.Context, address string) (int64, error)
	// Broadcast forwards a raw signed transaction and reports its txid.
	Broadcast(ctx context.Context, rawtx string) (string, error)
}

// Cipher is the port that encrypts and decrypts a message's text between the
// postern key and the party at the other end of it. The real adapter is
// BRC-78 EncryptedMessage (docs/protocol.md section 2,
// github.com/Jonathan-A-White/postern), infrastructure/postern's Cipher.
type Cipher interface {
	// Encrypt encrypts text for the holder of toPubKey (compressed, hex) and
	// reports the ciphertext, base64.
	Encrypt(toPubKey, text string) (ciphertext string, err error)
	// Decrypt decrypts ciphertext (base64) with privKey (WIF) and reports its
	// plaintext text, along with envelopeFrom: the sender's compressed
	// public key, hex, that BRC-78 itself binds into the ciphertext.
	// Decrypt only succeeds if the AES-GCM tag verifies, which requires the
	// private key matching envelopeFrom — so a successful Decrypt is proof
	// the message was encrypted by that key's holder, unlike the payload's
	// own From field, which is free text anyone can write. Ciphertext this
	// key cannot open is an error.
	Decrypt(privKey, ciphertext string) (text string, envelopeFrom string, err error)
}

// PosternNotes is the part of the tracker's key-value store Inbox remembers
// its cursor in between runs: it is TrackerSync's own Note, SetNote and
// ClearNote, narrowed, the same pattern SweepNotes uses — reading mail moves
// a note, never a state, so it is never an event of its own.
type PosternNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
}

// PosternCursorKey is the note key Inbox keeps its cursor under: the highest
// sequence number of every record it has read, ours or not.
const PosternCursorKey = "postern.inbox.cursor"

// PosternQuestionKey is the note key a question sent for a bead is marked
// open under, until its reply clears it: postern's docs/protocol.md section
// 6.
func PosternQuestionKey(bead string) string { return "postern.question." + bead }

// PosternQuestion is a decision-needed message's plaintext, postern's
// docs/protocol.md section 6: a JSON object naming the bead it is asked
// about, the question, the recommended option and every option offered.
type PosternQuestion struct {
	Bead    string   `json:"bead"`
	Q       string   `json:"q"`
	Rec     string   `json:"rec"`
	Options []string `json:"options"`
}

// PosternReply is a reply's plaintext, postern's docs/protocol.md section 6:
// a JSON object naming the bead it answers and the answer. A plaintext that
// does not decode as one — including plain text, and a question — is not a
// reply.
type PosternReply struct {
	Bead   string `json:"bead"`
	Answer string `json:"answer"`
}

// decodePosternReply reads text as a PosternReply, reporting false when it is
// not a JSON object with a non-empty bead field — plain text, exactly as
// postern's protocol says a message not shaped this way is read.
func decodePosternReply(text string) (PosternReply, bool) {
	var reply PosternReply
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return PosternReply{}, false
	}
	if strings.TrimSpace(reply.Bead) == "" || strings.TrimSpace(reply.Answer) == "" {
		return PosternReply{}, false
	}
	return reply, true
}

// decodePosternQuestion reads text as a PosternQuestion, reporting false when
// it is not a JSON object with a non-empty bead field.
func decodePosternQuestion(text string) (PosternQuestion, bool) {
	var question PosternQuestion
	if err := json.Unmarshal([]byte(text), &question); err != nil {
		return PosternQuestion{}, false
	}
	if strings.TrimSpace(question.Bead) == "" {
		return PosternQuestion{}, false
	}
	return question, true
}

// PosternGeneralThread is what mw postern inbox prints for a message whose
// plaintext names no thread at all.
const PosternGeneralThread = "general"

// PosternThread is a message's thread reference, postern's docs/protocol.md
// section 6 Threads subsection: a bead or a named topic, never both.
type PosternThread struct {
	Bead  string `json:"bead,omitempty"`
	Topic string `json:"topic,omitempty"`
}

// PosternThreadedMessage is the plaintext a message carries when it names an
// explicit thread rather than plain text: postern's docs/protocol.md section
// 6 Threads subsection.
type PosternThreadedMessage struct {
	Thread PosternThread `json:"thread"`
	Text   string        `json:"text"`
}

// decodePosternThreadedMessage reads text as a PosternThreadedMessage,
// reporting false when it is not a JSON object naming a bead or a topic
// thread — plain text, or a question or a reply, read exactly as before.
func decodePosternThreadedMessage(text string) (PosternThreadedMessage, bool) {
	var wrapped PosternThreadedMessage
	if err := json.Unmarshal([]byte(text), &wrapped); err != nil {
		return PosternThreadedMessage{}, false
	}
	if strings.TrimSpace(wrapped.Thread.Bead) == "" && strings.TrimSpace(wrapped.Thread.Topic) == "" {
		return PosternThreadedMessage{}, false
	}
	return wrapped, true
}

// posternThreadAndText reads a decrypted record's class and plaintext,
// reporting the thread label mw postern inbox prints it under, the text a
// person should read, and whether that thread is a bead rather than a named
// topic or the general thread. A decision-needed question's or a reply's own
// bead IS its thread (postern's docs/protocol.md section 6); anything else
// reads the plaintext's own thread wrapper, unwrapping it to the text it
// names; PosternGeneralThread when there is none.
func posternThreadAndText(class, text string) (thread, display string, isBead bool) {
	if class == "decision-needed" {
		if question, ok := decodePosternQuestion(text); ok {
			return question.Bead, text, true
		}
	}
	if reply, ok := decodePosternReply(text); ok {
		return reply.Bead, text, true
	}
	if wrapped, ok := decodePosternThreadedMessage(text); ok {
		if strings.TrimSpace(wrapped.Thread.Bead) != "" {
			return wrapped.Thread.Bead, wrapped.Text, true
		}
		return wrapped.Thread.Topic, wrapped.Text, false
	}
	return PosternGeneralThread, text, false
}

// PosternClasses is the set of classes mw postern send accepts.
var PosternClasses = []string{"message", "decision-needed", "landing", "alarm"}

func validPosternClass(class string) bool {
	for _, c := range PosternClasses {
		if c == class {
			return true
		}
	}
	return false
}

// PosternMessageKind is the kind a postern message record's payload carries.
const PosternMessageKind = "msg"

// PosternPayload is a message record's on-chain payload, postern's
// docs/protocol.md section 1 (github.com/Jonathan-A-White/postern): UTF-8
// JSON, fields in this order — the order postern's own TypeScript writes them
// in, so the two build the same bytes — carried as version 1 of the nftgate
// record framing. Everything but Ct travels in the clear.
type PosternPayload struct {
	V     int    `json:"v"`     // always 1
	Kind  string `json:"kind"`  // always PosternMessageKind
	Class string `json:"class"` // one of PosternClasses
	To    string `json:"to"`    // recipient's compressed public key, hex
	From  string `json:"from"`  // sender's compressed public key, hex
	Ts    int64  `json:"ts"`    // Unix seconds, when the sender built it
	Ct    string `json:"ct"`    // the BRC-78 ciphertext of the text, base64
}

// PosternInboxMessage is one record as Inbox reports it: decrypted, and only
// what a person reading it needs.
type PosternInboxMessage struct {
	Seq   int64
	Txid  string
	Class string
	// From is the sender mw trusts: the BRC-78 envelope's own sender key
	// (Cipher.Decrypt's envelopeFrom), never the payload's From field taken
	// on its own word. When the payload's From, or — once the backend can
	// supply one — the transaction's signing key, disagrees with the
	// envelope, From instead names the envelope's key and what was falsely
	// claimed, and Verified is false: "<envelope key> (payload claimed
	// <claim>)" or "<envelope key> (signer claimed <signer>)".
	From     string
	Verified bool
	// SignerChecked is true once the postern backend has supplied enough to
	// compare the transaction's signing key against the envelope.
	SignerChecked bool
	Ts            time.Time
	Text          string
	// Thread is the bead a decision-needed question or its reply names, the
	// bead or topic the plaintext's own thread wrapper names, or
	// PosternGeneralThread when it names neither.
	Thread string
	// ThreadIsBead reports whether Thread names a bead, rather than a named
	// topic or PosternGeneralThread.
	ThreadIsBead bool
}

// PosternInbox reads the postern's message records addressed to this host's
// key: everything indexed since the stored cursor that decrypts against this
// key, newest first. Reading marks them read by moving the cursor past every
// record it saw, ours or not — a note, never a state, so reading mail records
// no event. UnreadCount only counts them and moves nothing, so a notifier can
// poll without consuming anything.
type PosternInbox struct {
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile
	Memory  PosternNotes

	// Tracker records a reply's answer on the bead it names, and clears the
	// note the question it answers was marked open under. A nil Tracker
	// leaves every reply printed as text, exactly as an unknown bead does.
	Tracker WorkTracker
	// Mailbox, when set, is sent a message to the Mayor's mailbox for every
	// reply recorded, so the notifier wakes the seat. A nil Mailbox records
	// the reply but sends nothing.
	Mailbox Mailbox
	// Host names this host, for the mail a recorded reply sends: it is signed
	// SeatIdentity(MwSeat, Host).
	Host string

	// GovernorKey is the Governor's compressed public key, hex — config
	// postern_governor_key. A verified sender equal to it is printed as "the
	// Governor" rather than its hex. Empty prints every verified sender as
	// its hex.
	GovernorKey string

	// Out is where a full read's messages are printed, and where UnreadCount
	// prints the count. A nil Out prints nothing.
	Out io.Writer
}

// Run prints every unread message addressed to this key, newest first, and
// marks them read. A message whose plaintext decodes as a reply naming a bead
// this host's tracker knows is not printed as text: its answer is appended to
// the bead verbatim, with the txid and the sender's public key, the open
// question's note is cleared, and the Mayor is mailed so the notifier wakes
// the seat. A reply naming a bead the tracker does not know is printed as
// text, and nothing is written for it. Any other verified message from the
// Governor whose thread is a bead is likewise not printed as text: it lands
// as a comment on that bead instead, once per txid. A topic thread, or a
// verified sender who is not the Governor, is printed as text as before.
func (i PosternInbox) Run(ctx context.Context) ([]PosternInboxMessage, error) {
	if err := i.wired(); err != nil {
		return nil, err
	}
	mine, newest, cursor, err := i.fetch(ctx)
	if err != nil {
		return nil, err
	}
	newestFirst := reversePosternInbox(mine)
	for _, m := range newestFirst {
		// A record whose sender is not verified is never read as a reply,
		// however its plaintext decodes: recording its answer on a bead
		// would take a forged or unverifiable claim as someone's word.
		if m.Verified {
			if reply, ok := decodePosternReply(m.Text); ok {
				recorded, err := i.recordAnswer(ctx, m, reply)
				if err != nil {
					return nil, err
				}
				if recorded {
					continue
				}
			} else if m.ThreadIsBead && i.isGovernor(m) {
				recorded, err := i.recordThreadComment(ctx, m)
				if err != nil {
					return nil, err
				}
				if recorded {
					continue
				}
			}
		}
		i.printf("%s  from %s  txid %s  thread %s  %s\n%s\n", m.Class, i.fromLabel(m), orUnknown(m.Txid), m.Thread, sentInFull(m.Ts), m.Text)
	}
	if newest > cursor {
		if err := i.Memory.SetNote(ctx, PosternCursorKey, strconv.FormatInt(newest, 10)); err != nil {
			return nil, err
		}
	}
	return newestFirst, nil
}

// recordAnswer appends reply's answer to the bead it names, clears the note
// that bead's question was marked open under, and mails the Mayor, reporting
// true once done. A bead the tracker does not know — including no tracker at
// all — reports false and changes nothing, so the reply is left for Run to
// print as text.
func (i PosternInbox) recordAnswer(ctx context.Context, m PosternInboxMessage, reply PosternReply) (bool, error) {
	if i.Tracker == nil {
		return false, nil
	}
	comment := fmt.Sprintf("ANSWER %s from %s, txid %s: %s", sentInFull(m.Ts), orUnknown(m.From), m.Txid, reply.Answer)
	if err := i.Tracker.CommentOnStory(ctx, reply.Bead, comment); err != nil {
		return false, nil
	}
	if err := i.Memory.ClearNote(ctx, PosternQuestionKey(reply.Bead)); err != nil {
		return false, err
	}
	if i.Mailbox != nil {
		if _, err := i.Mailbox.Send(ctx, NewMessage{
			From:    SeatIdentity(MwSeat, i.Host),
			To:      MayorMailbox,
			Subject: fmt.Sprintf("Answer: %s: %s", reply.Bead, reply.Answer),
			Body:    comment,
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

// PosternThreadCommentKey is the note key a thread message's txid is marked
// commented under, once recordThreadComment has appended it to its bead: a
// record fetched more than once — the same underlying message indexed twice,
// say — is never commented on twice.
func PosternThreadCommentKey(txid string) string { return "postern.threadcomment." + txid }

// isGovernor reports whether m's verified sender is the configured Governor's
// key. An unconfigured GovernorKey never matches, so a thread comment is only
// ever recorded once mw postern inbox trusts a specific key as the Governor's.
func (i PosternInbox) isGovernor(m PosternInboxMessage) bool {
	return i.GovernorKey != "" && m.From == i.GovernorKey
}

// recordThreadComment appends m's text to the bead its thread names, reading
// 'The Governor by postern <ts>: <text>', reporting true once done — or
// already done, for a txid recordThreadComment has already commented, which
// changes nothing and is still reported done so Run leaves it out of the
// inbox. A bead the tracker does not know reports false and changes nothing,
// so Run prints it as text, exactly as an unrecordable reply does.
func (i PosternInbox) recordThreadComment(ctx context.Context, m PosternInboxMessage) (bool, error) {
	if i.Tracker == nil {
		return false, nil
	}
	key := PosternThreadCommentKey(m.Txid)
	already, err := i.Memory.Note(ctx, key)
	if err != nil {
		return false, err
	}
	if already != "" {
		return true, nil
	}
	comment := fmt.Sprintf("The Governor by postern %s: %s", sentInFull(m.Ts), m.Text)
	if err := i.Tracker.CommentOnStory(ctx, m.Thread, comment); err != nil {
		return false, nil
	}
	if err := i.Memory.SetNote(ctx, key, m.Txid); err != nil {
		return false, err
	}
	return true, nil
}

// fromLabel is the text mw postern inbox prints for m's sender: m.From
// unchanged for an unverified sender (already annotated with what was
// falsely claimed), otherwise "the Governor" when it is i.GovernorKey, its
// hex key otherwise — with "(signer unchecked)" appended when the backend
// has not yet supplied enough to compare the transaction's signing key.
func (i PosternInbox) fromLabel(m PosternInboxMessage) string {
	if !m.Verified {
		return m.From
	}
	label := m.From
	if i.GovernorKey != "" && label == i.GovernorKey {
		label = "the Governor"
	}
	if !m.SignerChecked {
		label += " (signer unchecked)"
	}
	return label
}

// UnreadCount reports how many messages addressed to this key are unread,
// without reading them: it moves the cursor nowhere, so a notifier can call
// it as often as it likes.
func (i PosternInbox) UnreadCount(ctx context.Context) (int, error) {
	if err := i.wired(); err != nil {
		return 0, err
	}
	mine, _, _, err := i.fetch(ctx)
	if err != nil {
		return 0, err
	}
	i.printf("%d\n", len(mine))
	return len(mine), nil
}

// fetch reads everything indexed since the stored cursor and decrypts what is
// addressed to this key, oldest first; it changes nothing itself. newest is
// the highest sequence number among every record read, ours or not, so a
// cursor moved past it never re-reads a record addressed to someone else.
func (i PosternInbox) fetch(ctx context.Context) (mine []PosternInboxMessage, newest, cursor int64, err error) {
	pubKeyHex, _, err := i.Keys.PublicKey()
	if err != nil {
		return nil, 0, 0, err
	}
	privKey, err := i.Keys.PrivateKeyWIF()
	if err != nil {
		return nil, 0, 0, err
	}
	cursor, err = i.readCursor(ctx)
	if err != nil {
		return nil, 0, 0, err
	}
	records, err := i.Postern.Messages(ctx, cursor)
	if err != nil {
		return nil, 0, 0, err
	}
	newest = cursor
	for _, r := range records {
		if r.Seq > newest {
			newest = r.Seq
		}
		if r.To != pubKeyHex {
			continue
		}
		text, envelopeFrom, err := i.Cipher.Decrypt(privKey, r.Ciphertext)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("decrypting record %d (%s): %w", r.Seq, r.Txid, err)
		}
		from, verified, signerChecked := posternVerifySender(envelopeFrom, r.From, r.Signer)
		thread, display, threadIsBead := posternThreadAndText(r.Class, text)
		mine = append(mine, PosternInboxMessage{
			Seq: r.Seq, Txid: r.Txid, Class: r.Class,
			From: from, Verified: verified, SignerChecked: signerChecked,
			Ts: r.Ts, Text: display, Thread: thread, ThreadIsBead: threadIsBead,
		})
	}
	return mine, newest, cursor, nil
}

// posternVerifySender is the sender mw trusts for a record whose ciphertext
// decrypted to envelopeFrom (Cipher.Decrypt's own cryptographically bound
// sender key): claimedFrom is the payload's own From field, signer the
// transaction's signing key when the backend supplies one ("" when it does
// not). A record is verified only when both agree with envelopeFrom — a
// disagreement is reported as text rather than hidden, and never verified.
func posternVerifySender(envelopeFrom, claimedFrom, signer string) (from string, verified bool, signerChecked bool) {
	if claimedFrom != envelopeFrom {
		return fmt.Sprintf("%s (payload claimed %s)", envelopeFrom, orUnknown(claimedFrom)), false, signer != ""
	}
	if signer != "" && signer != envelopeFrom {
		return fmt.Sprintf("%s (signer claimed %s)", envelopeFrom, signer), false, true
	}
	return envelopeFrom, true, signer != ""
}

// readCursor is the stored cursor, or 0 when Inbox has never read before.
func (i PosternInbox) readCursor(ctx context.Context) (int64, error) {
	saved, err := i.Memory.Note(ctx, PosternCursorKey)
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

// wired reports what mw postern inbox is missing before it can do anything.
func (i PosternInbox) wired() error {
	switch {
	case i.Postern == nil:
		return fmt.Errorf("mw postern inbox: no postern backend is configured")
	case i.Cipher == nil:
		return fmt.Errorf("mw postern inbox: no cipher is configured")
	case i.Keys == nil:
		return fmt.Errorf("mw postern inbox: no postern key file is configured")
	case i.Memory == nil:
		return fmt.Errorf("mw postern inbox: nowhere to remember what has been read")
	}
	return nil
}

func (i PosternInbox) printf(format string, args ...any) {
	if i.Out != nil {
		fmt.Fprintf(i.Out, format, args...)
	}
}

func reversePosternInbox(mine []PosternInboxMessage) []PosternInboxMessage {
	out := make([]PosternInboxMessage, len(mine))
	for idx, m := range mine {
		out[len(mine)-1-idx] = m
	}
	return out
}

// PosternSendRequest is what mw postern send is asked to do: text, classed
// class, and — only for class decision-needed, and only when Bead names a
// bead to ask — the question postern's docs/protocol.md section 6 shapes: the
// recommended option and every option offered.
type PosternSendRequest struct {
	// Class is one of PosternClasses. Empty defaults to "message", the
	// common case of a plain reply.
	Class string
	Text  string

	// Bead is the bead a decision-needed message asks about. Empty sends
	// Text as plain text, exactly as before this field existed. Set, it
	// refuses unless Class is decision-needed, and Text becomes the
	// question's text rather than the plaintext itself.
	Bead      string
	Recommend string
	Options   []string

	// Thread, set, wraps Text in postern's docs/protocol.md section 6
	// thread envelope, naming the bead this message belongs to — distinct
	// from Bead, which only asks a question. Refused together with Topic,
	// and together with a request that asks a question, whose own bead is
	// already its thread.
	Thread string
	// Topic, set, wraps Text in the same envelope, naming a topic thread
	// rather than a bead. Refused together with Thread.
	Topic string
}

// asksQuestion reports whether this request asks a question of a bead, rather
// than sending plain text.
func (r PosternSendRequest) asksQuestion() bool { return strings.TrimSpace(r.Bead) != "" }

// setsThread reports whether this request wraps Text with an explicit thread.
func (r PosternSendRequest) setsThread() bool {
	return strings.TrimSpace(r.Thread) != "" || strings.TrimSpace(r.Topic) != ""
}

// validate reports why this request cannot be sent, before anything is spent
// or broadcast: --bead, --recommend and --option are only for class
// decision-needed; --thread and --topic are mutually exclusive, and refused
// together with a question, whose own bead is already its thread.
func (r PosternSendRequest) validate() error {
	askedFor := strings.TrimSpace(r.Bead) != "" || strings.TrimSpace(r.Recommend) != "" || len(r.Options) > 0
	if askedFor && r.Class != "decision-needed" {
		return fmt.Errorf("mw postern send: --bead, --recommend and --option are only accepted with --class decision-needed")
	}
	if strings.TrimSpace(r.Thread) != "" && strings.TrimSpace(r.Topic) != "" {
		return fmt.Errorf("mw postern send: --thread and --topic cannot both be set")
	}
	if r.asksQuestion() && r.setsThread() {
		return fmt.Errorf("mw postern send: --thread and --topic are refused with a decision-needed question: its own bead is already the thread")
	}
	return nil
}

// PosternSend builds a message record, signs a transaction spending this
// key's own testnet balance to carry it, and broadcasts it. It refuses when
// there is no governor key configured to send to, when the key's balance
// exceeds the float cap mw enforces on every send, or when the class is not
// one mw knows. Asked to send a question (PosternSendRequest.Bead set), it
// also comments the bead once the broadcast succeeds and marks it open with a
// note, so that mw postern inbox knows to look for a reply.
type PosternSend struct {
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile

	// Tracker comments the bead a question is asked about. Required only for
	// a request that asks one.
	Tracker WorkTracker
	// Notes marks a question's bead open, under PosternQuestionKey. Required
	// only for a request that asks one.
	Notes PosternNotes

	// GovernorKey is who a message is sent to: the Governor's compressed
	// public key, hex — config postern_governor_key. Empty refuses.
	GovernorKey string
	// FloatSats is the balance cap mw enforces before every send — config
	// postern_float_sats.
	FloatSats int64

	// Now is the clock a sent message is stamped with. The zero value reads
	// the real one.
	Now func() time.Time

	// Out is where the txid is printed. A nil Out prints nothing.
	Out io.Writer
}

// Run sends req, and reports the txid it was broadcast under.
func (s PosternSend) Run(ctx context.Context, req PosternSendRequest) (string, error) {
	if err := s.wired(req.asksQuestion()); err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Class) == "" {
		req.Class = "message"
	}
	if err := req.validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(s.GovernorKey) == "" {
		return "", fmt.Errorf("mw postern send: postern_governor_key is not set, so there is nowhere to send to")
	}
	if !validPosternClass(req.Class) {
		return "", fmt.Errorf("mw postern send: %q is not a class postern knows: %s", req.Class, strings.Join(PosternClasses, ", "))
	}
	from, address, err := s.Keys.PublicKey()
	if err != nil {
		return "", err
	}
	balance, err := s.Postern.Balance(ctx, address)
	if err != nil {
		return "", err
	}
	if balance > s.FloatSats {
		return "", fmt.Errorf(
			"mw postern send: the postern key's balance is %d satoshis, over the float cap of %d by %d: it refuses to send until the balance is back under the cap",
			balance, s.FloatSats, balance-s.FloatSats)
	}
	text := req.Text
	switch {
	case req.asksQuestion():
		question, err := json.Marshal(PosternQuestion{Bead: req.Bead, Q: req.Text, Rec: req.Recommend, Options: req.Options})
		if err != nil {
			return "", fmt.Errorf("building the question: %w", err)
		}
		text = string(question)
	case strings.TrimSpace(req.Thread) != "":
		wrapped, err := json.Marshal(PosternThreadedMessage{Thread: PosternThread{Bead: req.Thread}, Text: req.Text})
		if err != nil {
			return "", fmt.Errorf("building the threaded message: %w", err)
		}
		text = string(wrapped)
	case strings.TrimSpace(req.Topic) != "":
		wrapped, err := json.Marshal(PosternThreadedMessage{Thread: PosternThread{Topic: req.Topic}, Text: req.Text})
		if err != nil {
			return "", fmt.Errorf("building the threaded message: %w", err)
		}
		text = string(wrapped)
	}
	ciphertext, err := s.Cipher.Encrypt(s.GovernorKey, text)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: req.Class,
		To: s.GovernorKey, From: from, Ts: s.now().Unix(), Ct: ciphertext,
	})
	if err != nil {
		return "", fmt.Errorf("building the record's payload: %w", err)
	}
	utxos, err := s.Postern.Utxos(ctx, address)
	if err != nil {
		return "", err
	}
	rawtx, err := s.Keys.Sign(utxos, payload)
	if err != nil {
		return "", err
	}
	txid, err := s.Postern.Broadcast(ctx, rawtx)
	if err != nil {
		return "", err
	}
	if req.asksQuestion() {
		if err := s.recordQuestion(ctx, req, txid); err != nil {
			return "", err
		}
	}
	s.printf("%s\n", txid)
	return txid, nil
}

// recordQuestion comments req.Bead with the question just broadcast, and
// marks it open with a note, so mw postern inbox knows a reply to it answers
// this bead.
func (s PosternSend) recordQuestion(ctx context.Context, req PosternSendRequest, txid string) error {
	comment := fmt.Sprintf("QUESTION %s asked by postern, txid %s: %s (recommended %s; options %s)",
		s.now().UTC().Format(time.RFC3339), txid, req.Text, req.Recommend, strings.Join(req.Options, ", "))
	if err := s.Tracker.CommentOnStory(ctx, req.Bead, comment); err != nil {
		return fmt.Errorf("recording the question on %s: %w", req.Bead, err)
	}
	if err := s.Notes.SetNote(ctx, PosternQuestionKey(req.Bead), txid); err != nil {
		return fmt.Errorf("marking %s's question open: %w", req.Bead, err)
	}
	return nil
}

// wired reports what mw postern send is missing before it can do anything.
// needsRecording is true for a request that asks a question, which also
// needs somewhere to record it before anything is spent or broadcast.
func (s PosternSend) wired(needsRecording bool) error {
	switch {
	case s.Postern == nil:
		return fmt.Errorf("mw postern send: no postern backend is configured")
	case s.Cipher == nil:
		return fmt.Errorf("mw postern send: no cipher is configured")
	case s.Keys == nil:
		return fmt.Errorf("mw postern send: no postern key file is configured")
	case needsRecording && s.Tracker == nil:
		return fmt.Errorf("mw postern send: no work tracker is configured to record the question on the bead")
	case needsRecording && s.Notes == nil:
		return fmt.Errorf("mw postern send: nowhere to remember that the question is open")
	}
	return nil
}

func (s PosternSend) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s PosternSend) printf(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format, args...)
	}
}

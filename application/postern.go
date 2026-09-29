package application

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	// Sign spends no more of utxos than covers the anchor and the fee, a
	// confirmed one before an unconfirmed one, and when that would leave
	// fewer than four spendable coins it splits the change into up to four
	// outputs. It leaves out the outputs MarkSpent has remembered for the
	// last two hours, adds the change MarkSent has remembered, and spends an
	// output listed twice once.
	Sign(utxos []PosternUtxo, payload []byte) (rawtx string, err error)
	// MarkSpent remembers that a send just spent utxos, for two hours, so
	// that Sign skips them while the block explorer still lists them.
	MarkSpent(utxos []PosternUtxo) error
	// MarkSent remembers what a send just broadcast as rawtx, for two hours:
	// the outputs it spent, as MarkSpent does, and the change it paid back
	// to this key, which Sign may spend while the block explorer does not
	// yet list it, so that sends need not wait for a block, one each.
	MarkSent(rawtx string) error
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
	// Height is the block that confirmed this output; 0 while it is
	// unconfirmed, as the postern backend's /api/utxos reports it.
	Height int
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
	// Blob downloads the whole body stored under hash at the postern
	// backend's blob store, postern's docs/protocol.md section 8: the raw
	// ciphertext bytes an attachment was uploaded as, untouched.
	Blob(ctx context.Context, hash string) ([]byte, error)
	// Deliver hands a message's record payload straight to the postern
	// backend, postern's docs/protocol.md section 9: the adapter wraps it in
	// the very record script a transaction would carry and posts that, and
	// the backend indexes it beside the chain's records. It reports the
	// "direct:<sha256>" id the record goes by from then on, a txid
	// everywhere one appears. Delivering the same bytes twice is harmless.
	Deliver(ctx context.Context, payload []byte) (string, error)
	// UploadBlob uploads an attachment's ciphertext, as it is, to the
	// backend's blob store, section 8, and reports the sha256 (hex) and size
	// it is stored under — what the message announcing it names.
	UploadBlob(ctx context.Context, body []byte) (hash string, size int64, err error)
}

// Cipher is the port that encrypts and decrypts a message's text between the
// postern key and the party at the other end of it. The real adapter is
// BRC-78 EncryptedMessage (docs/protocol.md section 2,
// github.com/Jonathan-A-White/postern), infrastructure/postern's Cipher.
type Cipher interface {
	// Encrypt encrypts text for the holder of toPubKey (compressed, hex) and
	// reports the ciphertext, base64.
	Encrypt(toPubKey, text string) (ciphertext string, err error)
	// EncryptBytes is Encrypt over raw bytes rather than a string's UTF-8:
	// what the live view's gzip stream (postern's docs/protocol.md section
	// 11) and an attachment's file bytes (sections 8 and 14) are encrypted
	// as, byte for byte. Encrypt(to, text) is EncryptBytes(to, []byte(text)).
	EncryptBytes(toPubKey string, plain []byte) (ciphertext string, err error)
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

// PosternTranscriber hears a voice note: postern's docs/protocol.md section
// 14, the Governor's decision 11 — on this host, never by a third party.
// The real adapter runs config postern_transcribe_cmd
// (infrastructure/postern's CommandTranscriber, contrib/postern-transcribe).
type PosternTranscriber interface {
	// Transcribe reports the words heard in the audio file at audioPath, as
	// plain text.
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

// PosternNotes is the part of the tracker's key-value store Inbox remembers
// its cursor in between runs: it is TrackerSync's own Note, SetNote and
// ClearNote, narrowed, the same pattern SweepNotes uses — reading mail moves
// a note, never a state, so it is never an event of its own.
type PosternNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error

	// NotesWithPrefix reports every note whose key has the given prefix, key
	// to value, in one read — DoctorNotes' own — so that a reader wanting
	// many notes (every open question, every host's last sync, every txid
	// already applied) spends one call on them rather than one each.
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
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
// explicit thread, an attachment, or both, rather than plain text: postern's
// docs/protocol.md sections 6 (Threads), 8 (Attachments) and 14 (re and
// role).
type PosternThreadedMessage struct {
	Thread     PosternThread      `json:"thread"`
	Text       string             `json:"text"`
	Attachment *PosternAttachment `json:"attachment,omitempty"`
	// Re is the txid of the message this one answers or annotates; Role
	// says what Text is (PosternRoleTranscript). Both are section 14's, and
	// empty on any other message.
	Re   string `json:"re,omitempty"`
	Role string `json:"role,omitempty"`
}

// MarshalJSON writes the message in the field order postern's own
// TypeScript writes it, leaving thread out altogether for the general
// thread: the app reads an empty thread object as no threaded body at all.
func (m PosternThreadedMessage) MarshalJSON() ([]byte, error) {
	var thread *PosternThread
	if strings.TrimSpace(m.Thread.Bead) != "" || strings.TrimSpace(m.Thread.Topic) != "" {
		thread = &m.Thread
	}
	return json.Marshal(struct {
		Thread     *PosternThread     `json:"thread,omitempty"`
		Text       string             `json:"text"`
		Attachment *PosternAttachment `json:"attachment,omitempty"`
		Re         string             `json:"re,omitempty"`
		Role       string             `json:"role,omitempty"`
	}{thread, m.Text, m.Attachment, m.Re, m.Role})
}

// PosternAttachment is a message's optional attachment, postern's
// docs/protocol.md section 8: an image, encrypted exactly as the message
// text is, uploaded whole to the postern backend's blob store. Hash is the
// sha256, hex, of the ciphertext body as uploaded; Size is that body's own
// byte count; Mime is one of image/png, image/jpeg or image/webp.
type PosternAttachment struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Mime string `json:"mime"`
}

// decodePosternThreadedMessage reads text as a PosternThreadedMessage,
// reporting false when it is not a JSON object naming a bead or a topic
// thread, or carrying an attachment — plain text, or a question or a reply,
// read exactly as before.
func decodePosternThreadedMessage(text string) (PosternThreadedMessage, bool) {
	var wrapped PosternThreadedMessage
	if err := json.Unmarshal([]byte(text), &wrapped); err != nil {
		return PosternThreadedMessage{}, false
	}
	if strings.TrimSpace(wrapped.Thread.Bead) == "" && strings.TrimSpace(wrapped.Thread.Topic) == "" && wrapped.Attachment == nil && wrapped.Role == "" {
		return PosternThreadedMessage{}, false
	}
	return wrapped, true
}

// posternThreadAndText reads a decrypted record's class and plaintext,
// reporting the thread label mw postern inbox prints it under, the text a
// person should read, whether that thread is a bead rather than a named
// topic or the general thread, and the attachment it carries, if any. A
// decision-needed question's or a reply's own bead IS its thread (postern's
// docs/protocol.md section 6); anything else reads the plaintext's own
// thread wrapper, unwrapping it to the text and the attachment (postern's
// docs/protocol.md section 8) it names; PosternGeneralThread when there is
// no thread at all.
func posternThreadAndText(class, text string) (thread, display string, isBead bool, attachment *PosternAttachment) {
	if class == "decision-needed" {
		if question, ok := decodePosternQuestion(text); ok {
			return question.Bead, text, true, nil
		}
	}
	if reply, ok := decodePosternReply(text); ok {
		return reply.Bead, text, true, nil
	}
	if wrapped, ok := decodePosternThreadedMessage(text); ok {
		switch {
		case strings.TrimSpace(wrapped.Thread.Bead) != "":
			return wrapped.Thread.Bead, wrapped.Text, true, wrapped.Attachment
		case strings.TrimSpace(wrapped.Thread.Topic) != "":
			return wrapped.Thread.Topic, wrapped.Text, false, wrapped.Attachment
		default:
			return PosternGeneralThread, wrapped.Text, false, wrapped.Attachment
		}
	}
	return PosternGeneralThread, text, false, nil
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
	// Attachment is the image this message carries, postern's
	// docs/protocol.md section 8, or nil for a message with none.
	Attachment *PosternAttachment
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

	// AttachmentDir is where a message's attachment is downloaded, decrypted
	// and written, 0600, named by its record's own txid and an extension by
	// its mime: postern's docs/protocol.md section 8. Required only for a
	// message that carries one; a run that never sees an attachment never
	// reads it.
	AttachmentDir string

	// Lock, when set, is taken for the whole of Run and Apply, so that two
	// of them — the on-message hook run twice at once, or the hook and the
	// Mayor's own read — never apply the same message together.
	Lock HostLock

	// Transcriber hears the Governor's voice notes, postern's
	// docs/protocol.md section 14 — config postern_transcribe_cmd. Nil hears
	// none: a voice note is then read like any message with an attachment.
	Transcriber PosternTranscriber
	// Sender sends what was heard in a voice note, and how a hands step ran,
	// back to the Governor in its own thread. Nil sends nothing back.
	Sender *PosternSend

	// HandsRunner and HandsVerifier run the hands steps the Governor
	// approves (§17), checking each approval first; with either nil, a run
	// action is left for the Mayor to read.
	HandsRunner   HandsRunner
	HandsVerifier HandsVerifier
	// HandsHosts is the ssh prefix that reaches each other host a step may
	// be for — config [hands_hosts]. A step for this host (Host) runs here.
	HandsHosts map[string]string

	// Now is the clock an approval's age is read by. The zero value reads
	// the real one.
	Now func() time.Time

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
//
// A message carrying an attachment (postern's docs/protocol.md section 8) is
// always printed too, whether or not it is also recorded on a bead: the
// download's outcome — the path an image was decrypted and written to,
// "attachment refused: hash mismatch" when its sha256 disagrees with the
// hash it was announced under, or a download or decrypt failure — is printed
// on its own line under the message, and a bead comment naming it ends with
// " [image: <path>]" once it is written.
//
// Whatever of the Governor's Apply would apply — an action (section 13), a
// voice note (section 14) — Run applies too, once per txid, when no pass
// has yet, and prints the one line saying what it did. A message a pass has
// already applied is that one line, "applied <kind> <bead> txid <id>" (or
// "refused …: <why>"), never its text, and is never applied twice.
func (i PosternInbox) Run(ctx context.Context) ([]PosternInboxMessage, error) {
	if err := i.wired(); err != nil {
		return nil, err
	}
	release, err := i.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	mine, newest, cursor, err := i.fetch(ctx)
	if err != nil {
		return nil, err
	}
	applied := map[string]string{}
	if i.Tracker != nil {
		if applied, err = i.appliedNotes(ctx); err != nil {
			return nil, err
		}
	}
	newestFirst := reversePosternInbox(mine)
	for _, m := range newestFirst {
		// A message a pass already applied (Apply, the on-message hook's)
		// is one line: what was done, never its text, never done again.
		if done, ok := applied[m.Txid]; ok && m.Txid != "" {
			i.printf("%s\n", done)
			continue
		}
		line, path := i.attachmentOutcome(ctx, m)
		// A record whose sender is not verified is never read as a reply,
		// however its plaintext decodes: recording its answer on a bead
		// would take a forged or unverifiable claim as someone's word.
		switch {
		case m.Verified && i.isGovernor(m) && i.Tracker != nil:
			result, handled, err := i.applyOne(ctx, m, line, path)
			if err != nil {
				return nil, err
			}
			if handled {
				if err := i.markApplied(ctx, applied, result); err != nil {
					return nil, err
				}
				switch {
				case result.Kind == "answer", result.Kind == "comment" && m.Attachment == nil:
					continue
				case result.Kind != "comment":
					i.printf("%s\n", result.line())
					continue
				}
			}
		case m.Verified:
			if reply, ok := decodePosternReply(m.Text); ok {
				recorded, err := i.recordAnswer(ctx, m, reply)
				if err != nil {
					return nil, err
				}
				if recorded {
					continue
				}
			}
		}
		i.printf("%s  from %s  txid %s  thread %s  %s\n%s\n", m.Class, i.fromLabel(m), orUnknown(m.Txid), m.Thread, sentInFull(m.Ts), m.Text)
		if line != "" {
			i.printf("%s\n", line)
		}
	}
	if newest > cursor {
		if err := i.Memory.SetNote(ctx, PosternCursorKey, strconv.FormatInt(newest, 10)); err != nil {
			return nil, err
		}
	}
	return newestFirst, nil
}

// attachmentOutcome handles m's attachment, if it carries one: line is what
// Run prints under the message — the written file's path on success,
// "attachment refused: hash mismatch", or a download or decrypt failure's
// error — and path is that file's path, set only on success, for
// recordThreadComment to name in its bead comment. Both are empty for a
// message with no attachment.
func (i PosternInbox) attachmentOutcome(ctx context.Context, m PosternInboxMessage) (line, path string) {
	if m.Attachment == nil {
		return "", ""
	}
	path, err := i.downloadAttachment(ctx, m)
	if err != nil {
		return err.Error(), ""
	}
	return path, path
}

// downloadAttachment downloads m's attachment, checks its sha256 against the
// hash it was announced under, decrypts it with this host's key, and writes
// it 0600 to AttachmentDir, named by m's txid and an extension by the
// attachment's mime, reporting the path it wrote. A hash mismatch is
// reported as "attachment refused: hash mismatch" and writes nothing.
func (i PosternInbox) downloadAttachment(ctx context.Context, m PosternInboxMessage) (string, error) {
	raw, err := i.Postern.Blob(ctx, m.Attachment.Hash)
	if err != nil {
		return "", fmt.Errorf("downloading the attachment: %w", err)
	}
	sum := sha256.Sum256(raw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(m.Attachment.Hash)) {
		return "", fmt.Errorf("attachment refused: hash mismatch")
	}
	privKey, err := i.Keys.PrivateKeyWIF()
	if err != nil {
		return "", err
	}
	plain, _, err := i.Cipher.Decrypt(privKey, base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		return "", fmt.Errorf("decrypting the attachment: %w", err)
	}
	if err := os.MkdirAll(i.AttachmentDir, 0o700); err != nil {
		return "", fmt.Errorf("making %s: %w", i.AttachmentDir, err)
	}
	path := filepath.Join(i.AttachmentDir, posternFileName(m.Txid)+posternAttachmentExtension(m.Attachment.Mime))
	if err := os.WriteFile(path, []byte(plain), 0o600); err != nil {
		return "", fmt.Errorf("writing the attachment: %w", err)
	}
	return path, nil
}

// posternAttachmentExtension is the file extension an attachment of mime is
// written under: postern's docs/protocol.md section 14's list — images,
// voice notes, a PDF or plain text. Anything else — which the phone never
// sends — is written .bin rather than refusing a file already safely
// downloaded and decrypted. A mime's parameters (audio/webm;codecs=opus)
// are no part of its type.
func posternAttachmentExtension(mime string) string {
	kind, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mime)), ";")
	switch strings.TrimSpace(kind) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "audio/webm":
		return ".webm"
	case "audio/ogg":
		return ".ogg"
	case "audio/mp4":
		return ".m4a"
	case "audio/mpeg":
		return ".mp3"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	default:
		return ".bin"
	}
}

// posternFileName is txid as a file name: a direct record's "direct:<hash>"
// with its colon made a dash, so that no program reading the file takes the
// name for a URL.
func posternFileName(txid string) string {
	return strings.ReplaceAll(txid, ":", "-")
}

// recordAnswer appends reply's answer to the bead it names, clears the note
// that bead's question was marked open under, and mails the Mayor, reporting
// true once done. A bead the tracker does not know — including no tracker at
// all — reports false and changes nothing, so the reply is left for Run to
// print as text.
//
// When the answer is itself a Release tap (its text, trimmed and
// case-folded, is "release"), the epic it names is released as `mw release`
// would — but only when the question it answers offered Release as one of
// its options, and m's verified sender is this host's configured
// GovernorKey. Any other signer, a question that never offered Release, a
// bead that is not an epic, or one with nothing held, releases nothing; the
// Mayor is always mailed what happened, so a refusal is never silent.
func (i PosternInbox) recordAnswer(ctx context.Context, m PosternInboxMessage, reply PosternReply) (bool, error) {
	if i.Tracker == nil {
		return false, nil
	}
	comment := fmt.Sprintf("ANSWER %s from %s, txid %s: %s", sentInFull(m.Ts), orUnknown(m.From), m.Txid, reply.Answer)
	if err := i.Tracker.CommentOnStory(ctx, reply.Bead, comment); err != nil {
		return false, nil
	}
	noteValue, err := i.Memory.Note(ctx, PosternQuestionKey(reply.Bead))
	if err != nil {
		return false, err
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
	if isReleaseTap(reply.Answer) {
		if err := i.releaseOnTap(ctx, m, reply.Bead, noteValue); err != nil {
			return false, err
		}
	}
	return true, nil
}

// isReleaseTap reports whether answer, trimmed and case-folded, asks for the
// epic it answers to be released.
func isReleaseTap(answer string) bool {
	return strings.EqualFold(strings.TrimSpace(answer), "release")
}

// posternQuestionNote is what a bead's PosternQuestionKey note holds while its
// question is open: the txid it was asked under and the options it offered,
// so a reply's answer can be checked against what was actually offered,
// without re-reading the question's own comment. A note written before this
// field existed, or one set directly to a bare txid (as a fixture might),
// does not decode as this shape — posternQuestionOfferedRelease reads that as
// "no options recorded", never as an offer of Release.
type posternQuestionNote struct {
	Txid    string   `json:"txid"`
	Options []string `json:"options,omitempty"`

	// Asked, Q and Rec are when the question was asked (RFC 3339), what it
	// asked and what it recommended: what mw postern view shows of an open
	// question without reading back its QUESTION comment. A note written
	// before they existed carries none of them, and its question is read
	// from the comment instead.
	Asked string `json:"asked,omitempty"`
	Q     string `json:"q,omitempty"`
	Rec   string `json:"rec,omitempty"`
}

// posternQuestionOfferedRelease reports whether noteValue — a bead's
// PosternQuestionKey note — recorded Release among the options its question
// offered.
func posternQuestionOfferedRelease(noteValue string) bool {
	var note posternQuestionNote
	if err := json.Unmarshal([]byte(noteValue), &note); err != nil {
		return false
	}
	for _, option := range note.Options {
		if strings.EqualFold(strings.TrimSpace(option), "release") {
			return true
		}
	}
	return false
}

// releaseOnTap runs Release{Tracker: i.Tracker} against bead, exactly as `mw
// release <bead>` would, once every guard holds: m's verified sender is this
// host's configured GovernorKey, and noteValue shows the question it answers
// offered Release. It appends a RELEASED comment naming the stories now
// ready, and always mails the Mayor what happened — released, or why not.
func (i PosternInbox) releaseOnTap(ctx context.Context, m PosternInboxMessage, bead, noteValue string) error {
	if !i.isGovernor(m) {
		return i.mailReleaseOutcome(ctx, bead, false, fmt.Sprintf("%s: not released, the tap's signer unchecked", bead))
	}
	if !posternQuestionOfferedRelease(noteValue) {
		return i.mailReleaseOutcome(ctx, bead, false, fmt.Sprintf("%s: not released, its question never offered Release", bead))
	}
	found, err := Release{Tracker: i.Tracker}.Run(ctx, bead)
	if err != nil {
		return i.mailReleaseOutcome(ctx, bead, false, fmt.Sprintf("%s: not released: %s", bead, err.Error()))
	}
	held := found.held()
	if len(held) == 0 {
		return i.mailReleaseOutcome(ctx, bead, false, fmt.Sprintf("%s: not released, it has no held stories", bead))
	}
	ready := found.ready()
	comment := fmt.Sprintf("RELEASED by mw on the Governor's Release tap (txid %s): %s", m.Txid, joinOrNone(ready))
	if err := i.Tracker.CommentOnStory(ctx, bead, comment); err != nil {
		return err
	}
	return i.mailReleaseOutcome(ctx, bead, true,
		fmt.Sprintf("Released: %s: %d stories, ready: %s", bead, len(held), joinOrNone(ready)))
}

// mailReleaseOutcome mails the Mayor what a Release tap did or did not do. A
// nil Mailbox sends nothing, exactly as an ordinary answer's mail does not.
func (i PosternInbox) mailReleaseOutcome(ctx context.Context, bead string, released bool, body string) error {
	if i.Mailbox == nil {
		return nil
	}
	subject := fmt.Sprintf("Release not applied: %s", bead)
	if released {
		subject = fmt.Sprintf("Released: %s", bead)
	}
	_, err := i.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, i.Host),
		To:      MayorMailbox,
		Subject: subject,
		Body:    body,
	})
	return err
}

// joinOrNone is items joined with ", ", or "none" when there are none.
func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
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
// so Run prints it as text, exactly as an unrecordable reply does. imagePath,
// set, is a decrypted attachment's path: the comment ends with " [image:
// <imagePath>]".
func (i PosternInbox) recordThreadComment(ctx context.Context, m PosternInboxMessage, imagePath string) (bool, error) {
	recorded, _, err := i.recordThreadCommentOnce(ctx, m, imagePath)
	return recorded, err
}

// recordThreadCommentOnce is recordThreadComment, saying besides whether the
// comment was written now (fresh) rather than found written already.
func (i PosternInbox) recordThreadCommentOnce(ctx context.Context, m PosternInboxMessage, imagePath string) (recorded, fresh bool, err error) {
	if i.Tracker == nil {
		return false, false, nil
	}
	key := PosternThreadCommentKey(m.Txid)
	already, err := i.Memory.Note(ctx, key)
	if err != nil {
		return false, false, err
	}
	if already != "" {
		return true, false, nil
	}
	comment := fmt.Sprintf("The Governor by postern %s: %s", sentInFull(m.Ts), m.Text)
	if imagePath != "" {
		comment += fmt.Sprintf(" [%s: %s]", posternAttachmentLabel(m.Attachment.Mime), imagePath)
	}
	if err := i.Tracker.CommentOnStory(ctx, m.Thread, comment); err != nil {
		return false, false, nil
	}
	if err := i.Memory.SetNote(ctx, key, m.Txid); err != nil {
		return false, false, err
	}
	return true, true, nil
}

// posternAttachmentLabel is what a comment calls a file of mime: an image,
// an audio note, or a file.
func posternAttachmentLabel(mime string) string {
	switch kind, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mime)), "/"); kind {
	case "image":
		return "image"
	case "audio":
		return "audio"
	default:
		return "file"
	}
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
		thread, display, threadIsBead, attachment := posternThreadAndText(r.Class, text)
		mine = append(mine, PosternInboxMessage{
			Seq: r.Seq, Txid: r.Txid, Class: r.Class,
			From: from, Verified: verified, SignerChecked: signerChecked,
			Ts: r.Ts, Text: display, Thread: thread, ThreadIsBead: threadIsBead,
			Attachment: attachment,
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
	// already its thread. Once sent, the bead is commented with what the
	// Mayor said (the Governor's 2026-09-28 decision: the exchange is
	// written to the bead), unless Role says the text is not the Mayor's
	// own.
	Thread string
	// Topic, set, wraps Text in the same envelope, naming a topic thread
	// rather than a bead. Refused together with Thread.
	Topic string

	// Attachments are files to send with the message, postern's
	// docs/protocol.md sections 8 and 14: each is encrypted to the
	// Governor, uploaded to the backend's blob store and announced in a
	// message of its own, the caption (Text) on the last. Each must be at
	// most PosternAttachmentLimit bytes, of a type PosternAttachmentMimes
	// names by its extension. Refused with a question.
	Attachments []string

	// Re and Role are section 14's annotations: the txid of the message
	// this one answers or annotates, and what the text is —
	// PosternRoleTranscript for what this host heard in a voice note.
	Re   string
	Role string

	// Recorded says Text is already written on the bead its thread names —
	// a hands step's outcome, say — so it is not commented there again.
	Recorded bool
}

// PosternRoleTranscript is the role of a message whose text is what the
// Mayor's host heard in the voice note its re names: postern's
// docs/protocol.md section 14.
const PosternRoleTranscript = "transcript"

// The channels a message is sent by: straight to the postern backend
// (postern's docs/protocol.md section 9, the default since the Governor's
// 2026-09-28 decision), or in a funded testnet transaction (section 4).
const (
	PosternChannelDirect = "direct"
	PosternChannelChain  = "chain"
)

// PosternAttachmentLimit is the most bytes a file sent with a message may
// hold: postern's docs/protocol.md sections 8 and 14, 8 MiB — the backend
// refuses a ciphertext over it plus the envelope's own overhead.
const PosternAttachmentLimit = 8 << 20

// PosternAttachmentMimes names the type of each file extension mw postern
// send attaches, from the types postern's docs/protocol.md section 14 lists;
// any other extension is refused.
var PosternAttachmentMimes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp",
	".webm": "audio/webm", ".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
	".m4a": "audio/mp4", ".mp4": "audio/mp4", ".mp3": "audio/mpeg",
	".pdf": "application/pdf", ".txt": "text/plain", ".md": "text/plain", ".log": "text/plain",
}

// asksQuestion reports whether this request asks a question of a bead, rather
// than sending plain text.
func (r PosternSendRequest) asksQuestion() bool { return strings.TrimSpace(r.Bead) != "" }

// setsThread reports whether this request wraps Text with an explicit thread.
func (r PosternSendRequest) setsThread() bool {
	return strings.TrimSpace(r.Thread) != "" || strings.TrimSpace(r.Topic) != ""
}

// commentsThread reports whether, once sent, this request is written to the
// bead its thread names: the Mayor's own words are; a transcript is not.
func (r PosternSendRequest) commentsThread() bool {
	return strings.TrimSpace(r.Thread) != "" && r.Role != PosternRoleTranscript && !r.Recorded
}

// validate reports why this request cannot be sent, before anything is spent
// or broadcast: --bead, --recommend and --option are only for class
// decision-needed; --thread and --topic are mutually exclusive, and refused
// together with a question, whose own bead is already its thread; so is an
// attachment, which a question's shape cannot carry.
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
	if r.asksQuestion() && len(r.Attachments) > 0 {
		return fmt.Errorf("mw postern send: --attach is refused with a decision-needed question: a question carries no attachment")
	}
	return nil
}

// posternFile is one file read for sending: its path, its bytes and the mime
// its extension names.
type posternFile struct {
	path string
	data []byte
	mime string
}

// readPosternFiles reads every file req attaches, refusing the lot before
// anything is sent when one cannot be: missing, too large, or of a type
// section 14 does not list.
func readPosternFiles(paths []string) ([]posternFile, error) {
	files := make([]posternFile, 0, len(paths))
	for _, path := range paths {
		mime, ok := PosternAttachmentMimes[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil, fmt.Errorf("mw postern send: %s is not a type postern carries: attach one of %s", path, strings.Join(posternAttachmentExtensions(), " "))
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("mw postern send: %w", err)
		}
		if info.Size() > PosternAttachmentLimit {
			return nil, fmt.Errorf("mw postern send: %s is %d bytes, over the %d bytes (8 MiB) an attachment may be", path, info.Size(), PosternAttachmentLimit)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("mw postern send: %w", err)
		}
		files = append(files, posternFile{path: path, data: data, mime: mime})
	}
	return files, nil
}

// posternAttachmentExtensions is every extension PosternAttachmentMimes
// names, sorted, for a refusal a person reads.
func posternAttachmentExtensions() []string {
	exts := make([]string, 0, len(PosternAttachmentMimes))
	for ext := range PosternAttachmentMimes {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

// PosternSend sends a message to the Governor: by the direct channel (the
// default, postern's docs/protocol.md section 9), the record payload handed
// straight to the backend; or by the chain (section 4), a transaction
// spending this key's own testnet balance, signed and broadcast. It refuses
// when there is no governor key configured to send to, when the class is not
// one mw knows, and — on the chain only — when the key's balance exceeds the
// float cap. Asked to send a question (PosternSendRequest.Bead set), it
// also comments the bead once the message is sent and marks it open with a
// note, so that mw postern inbox knows to look for a reply; a message in a
// bead's thread is commented on that bead too.
type PosternSend struct {
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile

	// Tracker comments the bead a question is asked about, or a message is
	// threaded on. Required only for a request that does either.
	Tracker WorkTracker
	// Notes marks a question's bead open, under PosternQuestionKey. Required
	// only for a request that asks one.
	Notes PosternNotes

	// GovernorKey is who a message is sent to: the Governor's compressed
	// public key, hex — config postern_governor_key. Empty refuses.
	GovernorKey string
	// FloatSats is the balance cap mw enforces before every send on the
	// chain — config postern_float_sats.
	FloatSats int64
	// Channel is how a message travels — config postern_channel:
	// PosternChannelDirect, or PosternChannelChain. Empty is direct.
	Channel string

	// Now is the clock a sent message is stamped with. The zero value reads
	// the real one.
	Now func() time.Time

	// Out is where each txid is printed, one a line. A nil Out prints
	// nothing.
	Out io.Writer
}

// Run sends req, and reports the txid of the message carrying its text: the
// last, when several files make several messages.
func (s PosternSend) Run(ctx context.Context, req PosternSendRequest) (string, error) {
	if err := s.wired(req.asksQuestion(), req.commentsThread()); err != nil {
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
	channel, err := s.channel()
	if err != nil {
		return "", err
	}
	if channel == PosternChannelChain && len(req.Attachments) > 1 {
		return "", fmt.Errorf("mw postern send: several files are several transactions, which would spend the same coins on the chain: attach one at a time, or send by the direct channel")
	}
	files, err := readPosternFiles(req.Attachments)
	if err != nil {
		return "", err
	}
	from, address, err := s.Keys.PublicKey()
	if err != nil {
		return "", err
	}
	if channel == PosternChannelChain {
		balance, err := s.Postern.Balance(ctx, address)
		if err != nil {
			return "", err
		}
		if balance > s.FloatSats {
			return "", fmt.Errorf(
				"mw postern send: the postern key's balance is %d satoshis, over the float cap of %d by %d: it refuses to send until the balance is back under the cap",
				balance, s.FloatSats, balance-s.FloatSats)
		}
	}

	// Every file is uploaded before any message is sent, so a failed upload
	// sends nothing.
	attachments := make([]*PosternAttachment, 0, len(files))
	for _, file := range files {
		attachment, err := s.upload(ctx, file)
		if err != nil {
			return "", err
		}
		attachments = append(attachments, attachment)
	}

	var txid string
	if len(attachments) == 0 {
		text, err := req.plaintext(req.Text, nil)
		if err != nil {
			return "", err
		}
		if txid, err = s.sendOne(ctx, channel, req.Class, from, address, text); err != nil {
			return "", err
		}
	}
	for i, attachment := range attachments {
		caption := ""
		if i == len(attachments)-1 {
			caption = req.Text
		}
		text, err := req.plaintext(caption, attachment)
		if err != nil {
			return "", err
		}
		if txid, err = s.sendOne(ctx, channel, req.Class, from, address, text); err != nil {
			return "", err
		}
		if i < len(attachments)-1 {
			s.printf("%s\n", txid)
		}
	}

	if req.asksQuestion() {
		if err := s.recordQuestion(ctx, req, txid); err != nil {
			return "", err
		}
	}
	s.printf("%s\n", txid)
	if req.commentsThread() {
		if err := s.recordThreadMessage(ctx, req, txid, files); err != nil {
			return txid, fmt.Errorf("mw postern send: sent as %s, but %w", txid, err)
		}
	}
	return txid, nil
}

// plaintext is what a message of this request carries, before it is
// encrypted: a question's section 6 shape; the threaded body (sections 6, 8
// and 14) when it names a thread, carries an attachment or annotates
// another message; or text, bare, otherwise.
func (r PosternSendRequest) plaintext(text string, attachment *PosternAttachment) (string, error) {
	var body any
	switch {
	case r.asksQuestion():
		body = PosternQuestion{Bead: r.Bead, Q: text, Rec: r.Recommend, Options: r.Options}
	case r.setsThread() || attachment != nil || r.Re != "" || r.Role != "":
		body = PosternThreadedMessage{
			Thread: PosternThread{Bead: strings.TrimSpace(r.Thread), Topic: strings.TrimSpace(r.Topic)},
			Text:   text, Attachment: attachment, Re: r.Re, Role: r.Role,
		}
	default:
		return text, nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("building the message's plaintext: %w", err)
	}
	return string(encoded), nil
}

// upload encrypts file's bytes to the Governor, as a message's text is
// encrypted, and uploads the ciphertext to the backend's blob store,
// reporting the attachment that announces it.
func (s PosternSend) upload(ctx context.Context, file posternFile) (*PosternAttachment, error) {
	sealed, err := s.Cipher.EncryptBytes(s.GovernorKey, file.data)
	if err != nil {
		return nil, fmt.Errorf("encrypting %s: %w", file.path, err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, fmt.Errorf("encrypting %s: the ciphertext is not base64: %w", file.path, err)
	}
	hash, size, err := s.Postern.UploadBlob(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("uploading %s: %w", file.path, err)
	}
	return &PosternAttachment{Hash: hash, Size: size, Mime: file.mime}, nil
}

// sendOne encrypts text to the Governor as one message record, classed
// class, and sends it by channel, reporting its txid.
func (s PosternSend) sendOne(ctx context.Context, channel, class, from, address, text string) (string, error) {
	ciphertext, err := s.Cipher.Encrypt(s.GovernorKey, text)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: class,
		To: s.GovernorKey, From: from, Ts: s.now().Unix(), Ct: ciphertext,
	})
	if err != nil {
		return "", fmt.Errorf("building the record's payload: %w", err)
	}
	if channel == PosternChannelDirect {
		return s.Postern.Deliver(ctx, payload)
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
	if err := s.Keys.MarkSent(rawtx); err != nil && s.Out != nil {
		fmt.Fprintf(s.Out, "warning: the record was broadcast, but mw could not remember what it spent, so a send within the next block may fail: %v\n", err)
	}
	return txid, nil
}

// channel is how this send travels: Channel, direct when empty; anything
// else is refused.
func (s PosternSend) channel() (string, error) {
	switch channel := strings.ToLower(strings.TrimSpace(s.Channel)); channel {
	case "", PosternChannelDirect:
		return PosternChannelDirect, nil
	case PosternChannelChain:
		return PosternChannelChain, nil
	default:
		return "", fmt.Errorf("mw postern send: %q is not a channel postern knows: %s or %s", s.Channel, PosternChannelDirect, PosternChannelChain)
	}
}

// recordThreadMessage comments the bead req's thread names with what the
// Mayor said in it — "MAYOR via postern, txid <id>: <text>", naming the
// files sent with it — so that the exchange lives on the bead as well as in
// the thread.
func (s PosternSend) recordThreadMessage(ctx context.Context, req PosternSendRequest, txid string, files []posternFile) error {
	comment := fmt.Sprintf("MAYOR via postern, txid %s: %s", txid, req.Text)
	if len(files) > 0 {
		names := make([]string, 0, len(files))
		for _, file := range files {
			names = append(names, filepath.Base(file.path))
		}
		comment += fmt.Sprintf(" [attached: %s]", strings.Join(names, ", "))
	}
	if err := s.Tracker.CommentOnStory(ctx, req.Thread, comment); err != nil {
		return fmt.Errorf("recording it on %s: %w", req.Thread, err)
	}
	return nil
}

// recordQuestion comments req.Bead with the question just broadcast, and
// marks it open with a note, so mw postern inbox knows a reply to it answers
// this bead. The note holds the txid and the options offered
// (posternQuestionNote), so a Release tap can be checked against what the
// question actually offered.
func (s PosternSend) recordQuestion(ctx context.Context, req PosternSendRequest, txid string) error {
	comment := fmt.Sprintf("QUESTION %s asked by postern, txid %s: %s (recommended %s; options %s)",
		s.now().UTC().Format(time.RFC3339), txid, req.Text, req.Recommend, strings.Join(req.Options, ", "))
	if err := s.Tracker.CommentOnStory(ctx, req.Bead, comment); err != nil {
		return fmt.Errorf("recording the question on %s: %w", req.Bead, err)
	}
	note, err := json.Marshal(posternQuestionNote{
		Txid: txid, Options: req.Options,
		Asked: s.now().UTC().Format(time.RFC3339), Q: req.Text, Rec: req.Recommend,
	})
	if err != nil {
		return fmt.Errorf("building %s's question note: %w", req.Bead, err)
	}
	if err := s.Notes.SetNote(ctx, PosternQuestionKey(req.Bead), string(note)); err != nil {
		return fmt.Errorf("marking %s's question open: %w", req.Bead, err)
	}
	return nil
}

// wired reports what mw postern send is missing before it can do anything.
// needsRecording is true for a request that asks a question, which also
// needs somewhere to record it before anything is spent or broadcast;
// needsTracker for one whose thread is a bead, commented once sent.
func (s PosternSend) wired(needsRecording, needsTracker bool) error {
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
	case needsTracker && s.Tracker == nil:
		return fmt.Errorf("mw postern send: no work tracker is configured to record the message on its bead")
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

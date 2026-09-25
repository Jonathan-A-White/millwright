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
	From       string // sender's compressed public key, hex
	To         string // recipient's compressed public key, hex
	Ts         time.Time
	Ciphertext string // base64, as it travels on chain
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
	// plaintext text. Ciphertext this key cannot open is an error.
	Decrypt(privKey, ciphertext string) (text string, err error)
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
	Class string
	From  string
	Ts    time.Time
	Text  string
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

	// Out is where a full read's messages are printed, and where UnreadCount
	// prints the count. A nil Out prints nothing.
	Out io.Writer
}

// Run prints every unread message addressed to this key, newest first, and
// marks them read.
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
		i.printf("%s  from %s  %s\n%s\n", m.Class, orUnknown(m.From), sentInFull(m.Ts), m.Text)
	}
	if newest > cursor {
		if err := i.Memory.SetNote(ctx, PosternCursorKey, strconv.FormatInt(newest, 10)); err != nil {
			return nil, err
		}
	}
	return newestFirst, nil
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
		text, err := i.Cipher.Decrypt(privKey, r.Ciphertext)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("decrypting record %d (%s): %w", r.Seq, r.Txid, err)
		}
		mine = append(mine, PosternInboxMessage{Seq: r.Seq, Class: r.Class, From: r.From, Ts: r.Ts, Text: text})
	}
	return mine, newest, cursor, nil
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

// PosternSend builds a message record, signs a transaction spending this
// key's own testnet balance to carry it, and broadcasts it. It refuses when
// there is no governor key configured to send to, when the key's balance
// exceeds the float cap mw enforces on every send, or when the class is not
// one mw knows.
type PosternSend struct {
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile

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

// Run sends text, classed as class, and reports the txid it was broadcast
// under.
func (s PosternSend) Run(ctx context.Context, class, text string) (string, error) {
	if err := s.wired(); err != nil {
		return "", err
	}
	if strings.TrimSpace(s.GovernorKey) == "" {
		return "", fmt.Errorf("mw postern send: postern_governor_key is not set, so there is nowhere to send to")
	}
	if !validPosternClass(class) {
		return "", fmt.Errorf("mw postern send: %q is not a class postern knows: %s", class, strings.Join(PosternClasses, ", "))
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
	s.printf("%s\n", txid)
	return txid, nil
}

// wired reports what mw postern send is missing before it can do anything.
func (s PosternSend) wired() error {
	switch {
	case s.Postern == nil:
		return fmt.Errorf("mw postern send: no postern backend is configured")
	case s.Cipher == nil:
		return fmt.Errorf("mw postern send: no cipher is configured")
	case s.Keys == nil:
		return fmt.Errorf("mw postern send: no postern key file is configured")
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

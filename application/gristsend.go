package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// GristSendMaxPhotos is the most photos one grist carries: postern's
// docs/protocol.md section 19.
const GristSendMaxPhotos = 4

// GristSendPollInterval is how often GristSend pages the backend for the
// answer when it is not told another.
const GristSendPollInterval = 5 * time.Second

// GristSendTimedOutExit is the status mw grist send leaves with when no
// answer came in time.
const GristSendTimedOutExit = 2

// GristSend sends a grist as the key it is given, which is not the mill's:
// what an app's phone does, from a terminal (postern's docs/protocol.md
// section 19). It seals each photo and the app's request to the mill key the
// backend names, uploads the photos, posts the grist, and, asked to wait,
// pages the backend for the answer whose re is the grist's txid. It never
// reads or prints any key but Keys', and never a private key.
type GristSend struct {
	Postern Postern
	Cipher  Cipher
	// Keys is the key the grist is sent as: the caller's own, named by
	// --key, never a default.
	Keys PosternKeyFile

	// Interval is how long to wait between pages; the zero value is
	// GristSendPollInterval.
	Interval time.Duration
	// Now and Sleep are the clock and the wait between pages. The zero
	// values read the real ones.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	// Out is where the answer is printed, as JSON, or the grist's txid when
	// not waiting. Log is where progress goes. A nil writer prints nothing.
	Out io.Writer
	Log io.Writer
}

// GristSendRequest is one grist to send.
type GristSendRequest struct {
	// App and Kind name the grind the grist asks for.
	App, Kind string
	// Version is the version of the app's own request schema. Empty reads
	// it from the request's schemaVersion.
	Version string
	// RequestFile is the app's request, a JSON file in the app's own schema.
	RequestFile string
	// Photos are the photo files to send with it, at most GristSendMaxPhotos.
	Photos []string
	// Wait is how long to wait for the answer; zero sends and does not wait.
	Wait time.Duration
}

// GristSendReport is what one send did: the txid the grist goes by, and the
// answer when one was waited for and came.
type GristSendReport struct {
	Txid   string
	Answer *GristAnswer
}

// GristUnanswered is a grist whose answer says it was refused or failed, or
// whose answer did not come in time. Its exit status says which: 1, or
// GristSendTimedOutExit.
type GristUnanswered struct {
	Txid string
	// Answer is what the mill said, nil when it said nothing in time.
	Answer *GristAnswer
	Wait   time.Duration
}

func (e *GristUnanswered) Error() string {
	if e.Answer == nil {
		return fmt.Sprintf("no answer to %s after %s: the grist waits at the factory, and its answer is the record whose re is that id", e.Txid, e.Wait)
	}
	reason := strings.TrimSpace(e.Answer.Reason)
	if reason == "" {
		reason = "the mill gave no reason"
	}
	return fmt.Sprintf("the mill %s %s: %s", e.Answer.Status, e.Txid, reason)
}

// Code is the status mw leaves with.
func (e *GristUnanswered) Code() int {
	if e.Answer == nil {
		return GristSendTimedOutExit
	}
	return 1
}

// GristUnansweredIn reports whether err is a grist that was not answered.
func GristUnansweredIn(err error) (*GristUnanswered, bool) {
	var unanswered *GristUnanswered
	return unanswered, errors.As(err, &unanswered)
}

// gristPhoto is one photo read for sending.
type gristPhoto struct {
	path string
	data []byte
	mime string
}

// Run sends req, and waits for its answer when req.Wait says to.
func (s GristSend) Run(ctx context.Context, req GristSendRequest) (GristSendReport, error) {
	if s.Postern == nil || s.Cipher == nil || s.Keys == nil {
		return GristSendReport{}, fmt.Errorf("mw grist send: no postern backend, cipher or key file is configured")
	}
	plain, photos, err := s.prepare(req)
	if err != nil {
		return GristSendReport{}, err
	}
	from, _, err := s.Keys.PublicKey()
	if err != nil {
		return GristSendReport{}, err
	}
	me, err := s.Postern.Me(ctx)
	if err != nil {
		return GristSendReport{}, err
	}
	mill := strings.TrimSpace(me.Mill)
	if mill == "" {
		return GristSendReport{}, fmt.Errorf("mw grist send: the postern backend names no mill key (POSTERN_MILL_KEY), so there is nowhere to send a grist")
	}

	// Every photo is uploaded before the grist is posted, so a failed upload
	// posts nothing.
	for _, photo := range photos {
		attachment, err := s.upload(ctx, mill, photo)
		if err != nil {
			return GristSendReport{}, err
		}
		plain.Attachments = append(plain.Attachments, attachment)
	}
	// The request is the app's own, so its text is left as written: < > and
	// & are not escaped.
	var text bytes.Buffer
	encoder := json.NewEncoder(&text)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(plain); err != nil {
		return GristSendReport{}, fmt.Errorf("building the grist's plaintext: %w", err)
	}
	ciphertext, err := s.Cipher.Encrypt(mill, strings.TrimSuffix(text.String(), "\n"))
	if err != nil {
		return GristSendReport{}, err
	}
	payload, err := json.Marshal(PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: GristClass,
		To: mill, From: from, Ts: s.now().Unix(), Ct: ciphertext,
	})
	if err != nil {
		return GristSendReport{}, fmt.Errorf("building the record's payload: %w", err)
	}
	txid, err := s.Postern.Deliver(ctx, payload)
	if err != nil {
		return GristSendReport{}, err
	}
	report := GristSendReport{Txid: txid}
	if req.Wait <= 0 {
		s.print(s.Out, "%s\n", txid)
		return report, nil
	}
	s.print(s.Log, "sent as %s; waiting up to %s for its answer\n", txid, req.Wait)
	answer, err := s.await(ctx, txid, from, mill, req.Wait)
	if err != nil {
		return report, fmt.Errorf("mw grist send: sent as %s, but %w", txid, err)
	}
	if answer == nil {
		return report, &GristUnanswered{Txid: txid, Wait: req.Wait}
	}
	report.Answer = answer
	shown, err := json.MarshalIndent(answer, "", "  ")
	if err != nil {
		return report, err
	}
	s.print(s.Out, "%s\n", shown)
	if answer.Status != GristAnswered {
		return report, &GristUnanswered{Txid: txid, Answer: answer, Wait: req.Wait}
	}
	return report, nil
}

// prepare checks everything about req that needs no network, before anything
// is uploaded or sent, reporting the grist's plaintext without its
// attachments and the photos read.
func (s GristSend) prepare(req GristSendRequest) (GristPlaintext, []gristPhoto, error) {
	if !gristKind.MatchString(req.App) || !gristKind.MatchString(req.Kind) {
		return GristPlaintext{}, nil, fmt.Errorf("mw grist send: --app %q and --kind %q must each be lower-case letters, digits, - or _", req.App, req.Kind)
	}
	if strings.TrimSpace(req.RequestFile) == "" {
		return GristPlaintext{}, nil, fmt.Errorf("mw grist send: --request names no file")
	}
	raw, err := os.ReadFile(req.RequestFile)
	if err != nil {
		return GristPlaintext{}, nil, fmt.Errorf("mw grist send: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return GristPlaintext{}, nil, fmt.Errorf("mw grist send: %s is not a JSON object: %w", req.RequestFile, err)
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		_ = json.Unmarshal(fields["schemaVersion"], &version)
	}
	if strings.TrimSpace(version) == "" {
		return GristPlaintext{}, nil, fmt.Errorf("mw grist send: the request has no schemaVersion to say which version of the app's schema it is: give --schema-version")
	}
	if len(req.Photos) > GristSendMaxPhotos {
		return GristPlaintext{}, nil, fmt.Errorf("mw grist send: %d photos is over the %d a grist may carry", len(req.Photos), GristSendMaxPhotos)
	}
	photos := make([]gristPhoto, 0, len(req.Photos))
	for _, path := range req.Photos {
		photo, err := readGristPhoto(path)
		if err != nil {
			return GristPlaintext{}, nil, err
		}
		photos = append(photos, photo)
	}
	plain := GristPlaintext{Grist: GristName{App: req.App, Kind: req.Kind, V: version}, Input: json.RawMessage(raw)}
	return plain, photos, nil
}

// readGristPhoto reads one photo: a type section 19 lets a grist carry, by
// its extension, and at most PosternAttachmentLimit bytes.
func readGristPhoto(path string) (gristPhoto, error) {
	mime := PosternAttachmentMimes[strings.ToLower(filepath.Ext(path))]
	if !slices.Contains(GristMimes, mime) {
		return gristPhoto{}, fmt.Errorf("mw grist send: %s is not a photo a grist carries: use .jpg, .png or .webp, or a recording (.webm, .ogg, .m4a, .mp3 or .wav)", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return gristPhoto{}, fmt.Errorf("mw grist send: %w", err)
	}
	if info.Size() > PosternAttachmentLimit {
		return gristPhoto{}, fmt.Errorf("mw grist send: %s is %d bytes, over the %d bytes (8 MiB) a photo may be", path, info.Size(), PosternAttachmentLimit)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return gristPhoto{}, fmt.Errorf("mw grist send: %w", err)
	}
	return gristPhoto{path: path, data: data, mime: mime}, nil
}

// upload seals photo to the mill key, as section 8 seals an image to the
// Mayor's, and uploads the ciphertext, reporting the attachment that names
// it in the grist.
func (s GristSend) upload(ctx context.Context, mill string, photo gristPhoto) (PosternAttachment, error) {
	sealed, err := s.Cipher.EncryptBytes(mill, photo.data)
	if err != nil {
		return PosternAttachment{}, fmt.Errorf("sealing %s: %w", photo.path, err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return PosternAttachment{}, fmt.Errorf("sealing %s: the ciphertext is not base64: %w", photo.path, err)
	}
	hash, size, err := s.Postern.UploadBlob(ctx, raw)
	if err != nil {
		return PosternAttachment{}, fmt.Errorf("uploading %s: %w", photo.path, err)
	}
	return PosternAttachment{Hash: hash, Size: size, Mime: photo.mime}, nil
}

// await pages the backend for the mill's answer to txid until wait is out,
// reporting nil when none came. An answer counts only when it is a grist
// record to from that decrypts to the mill's own seal (and the backend's
// proven signer, when it gives one, is the mill), and whose re is txid.
func (s GristSend) await(ctx context.Context, txid, from, mill string, wait time.Duration) (*GristAnswer, error) {
	privKey, err := s.Keys.PrivateKeyWIF()
	if err != nil {
		return nil, err
	}
	interval := s.Interval
	if interval <= 0 {
		interval = GristSendPollInterval
	}
	deadline := s.now().Add(wait)
	var cursor int64
	for {
		records, err := s.Postern.Messages(ctx, cursor)
		if err != nil {
			return nil, fmt.Errorf("reading its answer failed: %w", err)
		}
		for _, r := range records {
			cursor = max(cursor, r.Seq)
			if answer := s.answerIn(r, privKey, txid, from, mill); answer != nil {
				return answer, nil
			}
		}
		left := deadline.Sub(s.now())
		if left <= 0 {
			return nil, nil
		}
		if err := s.sleep(ctx, min(interval, left)); err != nil {
			return nil, err
		}
	}
}

// answerIn is the answer to txid that record r carries, or nil when it is not
// one: not a grist record from the mill to from, not sealed by the mill, or
// an answer to another grist.
func (s GristSend) answerIn(r PosternRecord, privKey, txid, from, mill string) *GristAnswer {
	if r.Class != GristClass || r.From != mill || r.To != from || (r.Signer != "" && r.Signer != mill) {
		return nil
	}
	text, envelopeFrom, err := s.Cipher.Decrypt(privKey, r.Ciphertext)
	if err != nil || envelopeFrom != mill {
		return nil
	}
	var answer GristAnswer
	if json.Unmarshal([]byte(text), &answer) != nil || answer.Re != txid {
		return nil
	}
	return &answer
}

func (s GristSend) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s GristSend) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s GristSend) print(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format, args...)
	}
}

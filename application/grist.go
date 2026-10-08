package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// GristClass is the class grist travels under, and its answer too: postern's
// docs/protocol.md section 18.
const GristClass = "grist"

// The statuses an answer to a grist carries (section 18, The answer).
const (
	GristAnswered = "answered"
	GristRefused  = "refused"
	GristFailed   = "failed"
)

// GristName names the grind a grist asks for: the app, the kind of grist,
// and the version of the app's own request schema. Model and Effort are what
// the sender asks the grind to run on; empty, the grind file's own are used.
type GristName struct {
	App    string `json:"app"`
	Kind   string `json:"kind"`
	V      string `json:"v"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// String is the name as a report prints it: cairn/sweep 1.1.
func (n GristName) String() string {
	if n.App == "" && n.Kind == "" {
		return "(unread)"
	}
	return strings.TrimSpace(n.App + "/" + n.Kind + " " + n.V)
}

// GristPlaintext is what a grist's ct seals to the mill key (section 18,
// The grist record): which grind to run, the app's request, in the app's own
// schema, and the photos, each uploaded first as a blob sealed to the mill.
type GristPlaintext struct {
	Grist       GristName           `json:"grist"`
	Input       json.RawMessage     `json:"input,omitempty"`
	Attachments []PosternAttachment `json:"attachments,omitempty"`
}

// GristAnswerGrind is which grind answered a grist, and the commit of the
// app's rig it was read at: empty when the mill never read one.
type GristAnswerGrind struct {
	App    string `json:"app"`
	Kind   string `json:"kind"`
	V      string `json:"v"`
	Commit string `json:"commit,omitempty"`
}

// GristAnswer is the plaintext of the mill's answer to one grist, sealed to
// the grist's sender (section 18, The answer), fields in the protocol's own
// order. Reason is there unless the status is GristAnswered; Answer only
// when it is. ReadingResult is there when the grind scored a recording: the
// scores by engine, exactly as the session was given them (the first
// recording's, and ReadingResults, one for each in order, when the grist
// carried more than one); never the audio.
type GristAnswer struct {
	Re             string           `json:"re"`
	Status         string           `json:"status"`
	Reason         string           `json:"reason,omitempty"`
	Answer         json.RawMessage  `json:"answer,omitempty"`
	ReadingResult  json.RawMessage  `json:"reading_result,omitempty"`
	ReadingResults json.RawMessage  `json:"reading_results,omitempty"`
	Grind          GristAnswerGrind `json:"grind"`
}

// GrindFile is an app's grind for one kind of grist, `grinds/<kind>.json` in
// the app's rig: what it accepts, the model and effort, and where in the
// same rig its instructions and its answer's schema are.
type GrindFile struct {
	// Grind is the format of this file: 1.
	Grind        int              `json:"grind"`
	App          string           `json:"app"`
	Kind         string           `json:"kind"`
	Versions     []string         `json:"versions"`
	Model        string           `json:"model"`
	Effort       string           `json:"effort"`
	Instructions string           `json:"instructions"`
	AnswerSchema string           `json:"answerSchema"`
	Attachments  GrindAttachments `json:"attachments"`
	// Scoring says what the mill does with a recording the grist carries.
	Scoring GrindScoring `json:"scoring"`
	// MaxTurns is the most turns the grind's session may take; 0 leaves it to
	// the harness's own limit.
	MaxTurns int `json:"maxTurns"`
	// Forward, when set, makes the grind no session at all: the mill mails the
	// grist to this seat and answers it sent. GristForwardMayor is the only
	// value the mill reads; model, effort, instructions and answerSchema are
	// then not needed.
	Forward string `json:"forward,omitempty"`
}

// GristForwardMayor is the one seat a grind may forward its grist to.
const GristForwardMayor = "mayor"

// GrindAttachments is what a grind takes in attachments: photos, and with
// Scoring on, recordings. A grind that says nothing takes none.
type GrindAttachments struct {
	Min      int      `json:"min"`
	Max      int      `json:"max"`
	Mime     []string `json:"mime"`
	MaxBytes int64    `json:"maxBytes"`
}

// GrindScoring is a grind's say over the recordings of its grist. With Audio
// on, the mill scores each recording with every engine this host runs
// (docs/scorers.md), against the text the app's request holds in TargetField,
// before the harness session, and the session is given the results as text.
// With it off, the grind takes no recording at all.
//
// Langs are the languages the grind scores in (GristScoringLangs names the
// codes known); the first is the default, and absent means English only. A
// grist's request may carry "lang", one of them, to say which it is read in.
type GrindScoring struct {
	Audio       bool     `json:"audio"`
	TargetField string   `json:"target_field"`
	Langs       []string `json:"langs,omitempty"`
}

// GristScoringLangs are the language codes a grind's scoring.langs may name:
// English, modern Greek (monotonic), and Spanish.
var GristScoringLangs = []string{"en", "el", "es"}

// DefaultGristScoringLang is the language a recording is scored in when the
// grind names none.
const DefaultGristScoringLang = "en"

// Languages are the languages the grind scores in, the default first.
func (s GrindScoring) Languages() []string {
	if len(s.Langs) == 0 {
		return []string{DefaultGristScoringLang}
	}
	return s.Langs
}

// langsKnown reports whether every language of the scoring is one the mill
// knows, each named once.
func (s GrindScoring) langsKnown() bool {
	seen := map[string]bool{}
	for _, l := range s.Langs {
		if !slices.Contains(GristScoringLangs, l) || seen[l] {
			return false
		}
		seen[l] = true
	}
	return true
}

// GrindFileFormat is the grind file format the mill reads.
const GrindFileFormat = 1

// GristImageMimes are the photo types section 18 lets a grist carry.
var GristImageMimes = []string{"image/jpeg", "image/png", "image/webp"}

// GristAudioMimes are the recording types a grist may carry for a grind that
// scores them: what the phone's browser makes.
var GristAudioMimes = []string{"audio/webm", "audio/ogg", "audio/mp4", "audio/mpeg", "audio/wav"}

// GristMimes are the attachment types the factory lets a grist carry, photos
// and recordings. A grind may take fewer, never more.
var GristMimes = append(append([]string(nil), GristImageMimes...), GristAudioMimes...)

// isGristAudio reports whether mime, as a grist names it, is a recording.
func isGristAudio(mime string) bool {
	return slices.Contains(GristAudioMimes, strings.ToLower(strings.TrimSpace(mime)))
}

// GrindEfforts are the efforts a grind may name: the harness's own levels.
var GrindEfforts = []string{"low", "medium", "high", "xhigh", "max"}

// GristCeilings are the factory's limits above every grind (config [grist]):
// the models a grind may name, the efforts a grist may ask for, how many
// photos and how large, how many grist one key may send a day, and how long
// one grind may run.
type GristCeilings struct {
	Models             []string
	Efforts            []string
	MaxAttachments     int
	MaxAttachmentBytes int64
	DailyLimit         int
	Timeout            time.Duration
}

// The ceilings when config says nothing; infrastructure/config's defaults
// are the same numbers.
const (
	DefaultGristMaxAttachments     = 4
	DefaultGristMaxAttachmentBytes = 8 << 20
	DefaultGristDailyLimit         = 50
	DefaultGristTimeout            = 10 * time.Minute
)

// DefaultGristModels are the models a grind may name when config says
// nothing.
var DefaultGristModels = []string{"haiku", "sonnet", "opus"}

// DefaultGristEfforts are the efforts a grist may ask for when config says
// nothing.
var DefaultGristEfforts = []string{"low", "medium", "high"}

// filled is c with every ceiling it leaves unset at its default.
func (c GristCeilings) filled() GristCeilings {
	if len(c.Models) == 0 {
		c.Models = DefaultGristModels
	}
	if len(c.Efforts) == 0 {
		c.Efforts = DefaultGristEfforts
	}
	if c.MaxAttachments <= 0 {
		c.MaxAttachments = DefaultGristMaxAttachments
	}
	if c.MaxAttachmentBytes <= 0 {
		c.MaxAttachmentBytes = DefaultGristMaxAttachmentBytes
	}
	if c.DailyLimit <= 0 {
		c.DailyLimit = DefaultGristDailyLimit
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultGristTimeout
	}
	return c
}

// Grinder runs one grind: a short harness session with no seat, answering
// only through a structured output held to the grind's schema. The real
// adapter is infrastructure/claude's Grinder, Claude Code run synchronously.
type Grinder interface {
	// Grind runs call and reports what the harness said. An error is a
	// session that could not run or left nothing readable: ErrGrindTimedOut,
	// wrapped, when it ran out of time.
	Grind(ctx context.Context, call GrindCall) (SessionResult, error)
}

// ErrGrindTimedOut is a grind stopped at its timeout.
var ErrGrindTimedOut = errors.New("the grind ran out of time")

// GrindCall is one grind: the private directory the photos are in, which is
// the session's working directory; the model and effort; the system prompt
// (the mill's preamble, then the grind's instructions); the answer's schema,
// JSON; the prompt naming the photos and carrying the app's request; and how
// long it may run, and the most turns it may take when the grind says.
type GrindCall struct {
	Dir     string
	Model   string
	Effort  string
	System  string
	Schema  string
	Prompt  string
	Timeout time.Duration
	// MaxTurns is the most turns the session may take; 0 sets no limit.
	MaxTurns int
}

// GrindSource reads an app's grinds from its rig's checkout on this host, at
// its local main. The real adapter is infrastructure/rig's Grinds, which
// asks git.
type GrindSource interface {
	// Commit reports the commit the checkout's local main is at.
	Commit(ctx context.Context, checkout string) (string, error)
	// ReadAt reports the file at path, relative to the rig's root, as it is
	// at commit; found is false when there is no such file there.
	ReadAt(ctx context.Context, checkout, commit, path string) (data []byte, found bool, err error)
}

// GristState is the mill's own state on its host: how far it has read, the
// record of every grist it has handled (one line each, never its input or
// photos), and any answer the backend would not yet take. It is kept outside
// beads, so grist never waits on the beads lock. The real adapter is
// infrastructure/grist's State.
type GristState interface {
	// Cursor reports the sequence number every record at or below which
	// needs nothing more: 0 before the first pass.
	Cursor(ctx context.Context) (int64, error)
	SetCursor(ctx context.Context, seq int64) error
	// Lines reports every line recorded so far, oldest first.
	Lines(ctx context.Context) ([]GrindLine, error)
	// Append adds one line to the record. Nothing is ever rewritten.
	Append(ctx context.Context, line GrindLine) error
	// KeepUndelivered keeps an answer the backend would not take, sealed
	// as it is, until a later pass delivers it.
	KeepUndelivered(ctx context.Context, answer GristUndelivered) error
	// Undelivered reports every answer kept that way.
	Undelivered(ctx context.Context) ([]GristUndelivered, error)
	// Delivered forgets a kept answer once it is delivered.
	Delivered(ctx context.Context, txid string) error
}

// GristForwardStore keeps the pictures of a forwarded grist under the mill's
// state directory, one directory for each grist, and reports the full path of
// each file it wrote, in order. The real adapter is infrastructure/grist's
// Forwards.
type GristForwardStore interface {
	Keep(ctx context.Context, txid string, pictures []GristRunAttachment) ([]string, error)
}

// GristLock is a lock the mill takes without waiting. The real adapter is
// infrastructure/hostlock's Try.
type GristLock interface {
	// TryTake takes the lock if nobody holds it; taken is false, and
	// nothing is held, when somebody does.
	TryTake(ctx context.Context) (release func(), taken bool, err error)
	// Held reports whether somebody holds it now, taking nothing.
	Held(ctx context.Context) (bool, error)
}

// GristMill is one pass of the mill: what mw grist grind runs, and what the
// dispatch tick runs after its own claims (GristGrind is the use case).
type GristMill interface {
	Run(ctx context.Context) (GristReport, error)
}

// GrindLine is one line of the mill's record: one grist handled. It holds
// the sender's fingerprint and never its key, never the grist's input,
// photos or answer.
type GrindLine struct {
	Time   time.Time `json:"time"`
	Txid   string    `json:"txid"`
	App    string    `json:"app,omitempty"`
	Kind   string    `json:"kind,omitempty"`
	V      string    `json:"v,omitempty"`
	Sender string    `json:"sender"`
	Model  string    `json:"model,omitempty"`
	Effort string    `json:"effort,omitempty"`
	// ModelFrom and EffortFrom say where Model and Effort came from: "grist"
	// when the sender asked for them, "grind file" otherwise.
	ModelFrom  string `json:"model_from,omitempty"`
	EffortFrom string `json:"effort_from,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	// Photos is how many photos the grist carried, which the mill deletes once
	// the answer is delivered. Always written: 0 is an answer.
	Photos  int        `json:"photos"`
	Tokens  int        `json:"tokens,omitempty"`
	Fuel    *GrindFuel `json:"fuel,omitempty"`
	CostUSD float64    `json:"total_cost_usd,omitempty"`
	Turns   int        `json:"turns,omitempty"`
	// Denials is how many times the session was refused a tool: a Read of
	// a mistyped path, say. Not a failure by itself.
	Denials   int     `json:"denials,omitempty"`
	Seconds   float64 `json:"seconds"`
	Commit    string  `json:"commit,omitempty"`
	Delivered bool    `json:"delivered"`
}

// GrindFuel is the Fuel one grind burned, as the harness counted it.
type GrindFuel struct {
	Input      int `json:"in"`
	Output     int `json:"out"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

// GristUndelivered is an answer the backend would not take: the record
// payload as it was sealed, and the grist's photos to delete once it is
// delivered.
type GristUndelivered struct {
	Txid    string   `json:"txid"`
	Payload []byte   `json:"payload"`
	Blobs   []string `json:"blobs,omitempty"`
}

// KeyFingerprint is a key as a person checks it by eye (postern's
// docs/protocol.md, Fingerprints): the first 16 hex digits of the SHA-256 of
// the key's hex text, in groups of four.
func KeyFingerprint(pubKeyHex string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(pubKeyHex)))
	digits := hex.EncodeToString(sum[:])[:16]
	return digits[0:4] + " " + digits[4:8] + " " + digits[8:12] + " " + digits[12:16]
}

// GristKey makes the mill key if there is none yet, and says its public
// half and fingerprint: what the postern backend is told as
// POSTERN_MILL_KEY. It never prints the private key.
type GristKey struct {
	Keys PosternKeyFile
	Out  io.Writer
}

// GristKeyReport is the mill key's public half, and whether this run made it.
type GristKeyReport struct {
	Path        string
	Made        bool
	PublicKey   string
	Fingerprint string
}

func (r GristKeyReport) String() string {
	var b strings.Builder
	if r.Made {
		fmt.Fprintf(&b, "Wrote a new mill key to %s\n", r.Path)
	}
	fmt.Fprintf(&b, "mill key: %s\npublic key: %s\nfingerprint: %s\n", r.Path, r.PublicKey, r.Fingerprint)
	fmt.Fprintf(&b, "the postern backend names it POSTERN_MILL_KEY=%s", r.PublicKey)
	return b.String()
}

// Run makes the key when there is none, then reports its public half.
func (k GristKey) Run(_ context.Context) (GristKeyReport, error) {
	if k.Keys == nil {
		return GristKeyReport{}, fmt.Errorf("mw grist key: no mill key file is configured")
	}
	report := GristKeyReport{Path: k.Keys.Path()}
	exists, err := k.Keys.Exists()
	if err != nil {
		return report, err
	}
	if !exists {
		if err := k.Keys.Generate(); err != nil {
			return report, err
		}
		report.Made = true
	}
	if report.PublicKey, _, err = k.Keys.PublicKey(); err != nil {
		return report, err
	}
	report.Fingerprint = KeyFingerprint(report.PublicKey)
	if k.Out != nil {
		fmt.Fprintln(k.Out, report.String())
	}
	return report, nil
}

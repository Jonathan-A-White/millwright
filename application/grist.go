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

// GristLimited is how the mill's own report shows a grist answered failed for a
// limit of the factory's (GristReasonLimited): the answer's status is still
// GristFailed, the protocol's word for one its sender may send again.
const GristLimited = "limited"

// GristName names the grind a grist asks for: the app, the kind of grist,
// and the version of the app's own request schema. Model and Effort are what
// the sender asks the grind to run on; empty, the grind file's own are used.
// Talk, when the app names one (at most GristTalkMax bytes), is the
// conversation the grist is a turn of: the mill keeps one session per talk
// (application/gristtalks.go).
type GristName struct {
	App    string `json:"app"`
	Kind   string `json:"kind"`
	V      string `json:"v"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	Talk   string `json:"talk,omitempty"`
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
	// ClientTiming is the optional client_timing of the app's previous answered
	// grist, kept raw: a malformed one is ignored (GristClientTiming), never a
	// reason to refuse the grist.
	ClientTiming json.RawMessage `json:"client_timing,omitempty"`
	// GrindOverride is the instructions and schema `mw grist eval --ref` brings
	// from a ref, for the mill to grind this one grist with in place of the
	// grind's own at main. The mill takes it only from the factory's test key,
	// in an eval or smoke talk (GristGrind.judgeOverride).
	GrindOverride *GristGrindOverride `json:"grind_override,omitempty"`
	// GrindOverrideBlob names, by hash, the blob the GrindOverride went in when
	// it was too big for the message (GristOverrideInlineMax): the override as
	// JSON, sealed to the mill key and uploaded as a photo is. Never both.
	GrindOverrideBlob *PosternAttachment `json:"grind_override_blob,omitempty"`
}

// GristOverrideInlineMax is the most bytes of instructions and schema a grist
// carries inside its message. Past it they go as a blob: the backend takes a
// direct message of 256 KiB at most, and the sealed message is a third bigger
// than its text.
const GristOverrideInlineMax = 96 * 1024

// GristOverrideMime is the type of the blob a grind override goes in.
const GristOverrideMime = "application/json"

// GristGrindOverride is the text of a grind as a ref holds it: its instructions
// (the file or the parts of its directory joined) and its answer schema.
type GristGrindOverride struct {
	Instructions string `json:"instructions"`
	Schema       string `json:"schema"`
}

// GristAnswerGrind is which grind answered a grist, and the commit of the
// app's rig it was read at: empty when the mill never read one.
type GristAnswerGrind struct {
	App    string `json:"app"`
	Kind   string `json:"kind"`
	V      string `json:"v"`
	Commit string `json:"commit,omitempty"`
	// Override says the grind was run with the text the grist brought
	// (GristPlaintext.GrindOverride), not the one at Commit.
	Override bool `json:"override,omitempty"`
}

// GristAnswer is the plaintext of the mill's answer to one grist, sealed to
// the grist's sender (section 18, The answer), fields in the protocol's own
// order. Reason is there unless the status is GristAnswered; Answer only
// when it is. ReadingResult is there when the grind scored a recording: the
// scores by engine, exactly as the session was given them (the first
// recording's, and ReadingResults, one for each in order, when the grist
// carried more than one); never the audio. Talk is there when the grist named
// a talk and its session gave a result: fresh or resumed.
type GristAnswer struct {
	Re             string           `json:"re"`
	Status         string           `json:"status"`
	Reason         string           `json:"reason,omitempty"`
	Answer         json.RawMessage  `json:"answer,omitempty"`
	ReadingResult  json.RawMessage  `json:"reading_result,omitempty"`
	ReadingResults json.RawMessage  `json:"reading_results,omitempty"`
	Talk           *GristAnswerTalk `json:"talk,omitempty"`
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

// Forwards reports who the grind passes its grist on to, and whether it does:
// the one decision `mw grist eval`, the smoke and the close-out's check of a
// grind example make of whether a grind is a session at all (mw-gq6.801).
func (g GrindFile) Forwards() (who string, forwards bool) {
	return g.Forward, g.Forward != ""
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

// GristTextMimes are the file types a grist may carry only to a grind that
// forwards to the Mayor, whose mail keeps the file: no model reads one.
var GristTextMimes = []string{"text/csv"}

// GristMimes are the attachment types the factory lets a grist carry, photos,
// recordings and text files. A grind may take fewer, never more.
var GristMimes = append(append(append([]string(nil), GristImageMimes...), GristAudioMimes...), GristTextMimes...)

// isGristAudio reports whether mime, as a grist names it, is a recording.
func isGristAudio(mime string) bool {
	return slices.Contains(GristAudioMimes, strings.ToLower(strings.TrimSpace(mime)))
}

// isGristText reports whether mime, as a grist names it, is a text file.
func isGristText(mime string) bool {
	return slices.Contains(GristTextMimes, strings.ToLower(strings.TrimSpace(mime)))
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
	// TalkContextCap and TalkMaxTurns are where a talk's session is ended for
	// its key points: the context tokens of a turn, and a session's turns.
	TalkContextCap int
	TalkMaxTurns   int
}

// The ceilings when config says nothing; infrastructure/config's defaults
// are the same numbers.
const (
	DefaultGristMaxAttachments     = 4
	DefaultGristMaxAttachmentBytes = 8 << 20
	DefaultGristDailyLimit         = 50
	DefaultGristTimeout            = 10 * time.Minute
	DefaultGristTalkContextCap     = 80000
	DefaultGristTalkMaxTurns       = 40
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
	if c.TalkContextCap <= 0 {
		c.TalkContextCap = DefaultGristTalkContextCap
	}
	if c.TalkMaxTurns <= 0 {
		c.TalkMaxTurns = DefaultGristTalkMaxTurns
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
	// APIKey is the Anthropic API key the session is billed to, given to the
	// harness in its environment alone; empty bills the subscription login.
	APIKey string
	// SessionID starts a talk's session that the harness keeps, under this id;
	// Resume carries on the kept session of this id instead. Either runs with
	// the one-hour prompt cache (GristTalkCacheTTL). Both empty, the session is
	// kept nowhere.
	SessionID string
	Resume    string
}

// GrindSource reads an app's grinds from its rig's checkout on this host, at
// the rig's main: the remote's if the checkout has seen it move ahead of its
// own, else its own. The real adapter is infrastructure/rig's Grinds, which
// asks git.
type GrindSource interface {
	// Refresh brings the checkout's view of the remote's main up to date, so
	// that a grind landed on another host is the one Commit then reports. An
	// error means the remote could not be reached; Commit still answers from
	// what the checkout has.
	Refresh(ctx context.Context, checkout string) error
	// Commit reports the commit the checkout's main is at.
	Commit(ctx context.Context, checkout string) (string, error)
	// ReadAt reports the file at path, relative to the rig's root, as it is
	// at commit; found is false when there is no such file there.
	ReadAt(ctx context.Context, checkout, commit, path string) (data []byte, found bool, err error)
	// List reports the path, relative to the rig's root, of every file at
	// or under dir at commit, sorted; none when there is no such directory.
	List(ctx context.Context, checkout, commit, dir string) ([]string, error)
}

// GrindRefs is a GrindSource that can also read a ref instead of main, for `mw
// grist eval --ref`. ref is a git ref the checkout holds, or a worktree of the
// rig; the grind is then read at the commit returned, from the checkout returned
// (the worktree itself for a worktree, whose uncommitted changes are not read:
// note says so). The real adapter is infrastructure/rig's Grinds.
type GrindRefs interface {
	ResolveRef(ctx context.Context, checkout, ref string) (readFrom, commit, note string, err error)
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
	// ClientTimings reports every client timing the apps reported, oldest first;
	// AppendClientTiming adds one. They are kept apart from the record's lines,
	// which are written once, when the grist is answered.
	ClientTimings(ctx context.Context) ([]GristClientTiming, error)
	AppendClientTiming(ctx context.Context, timing GristClientTiming) error
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
	// ModelID is the model id the harness's result named (SessionResult's
	// ModelID), where Model is the alias the grind was run with; left out
	// when the result named none, and on lines written before it was kept.
	ModelID string `json:"model_id,omitempty"`
	Effort  string `json:"effort,omitempty"`
	// Billing is what the grind was billed to: "api" (the key of [grist]
	// api_credits) or "subscription". A line written before it was kept says
	// nothing; a grist that ran no grind says "subscription".
	Billing string `json:"billing"`
	// ModelFrom and EffortFrom say where Model and Effort came from: "grist"
	// when the sender asked for them, "grind file" otherwise.
	ModelFrom  string `json:"model_from,omitempty"`
	EffortFrom string `json:"effort_from,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	// Photos is how many photos the grist carried, which the mill deletes once
	// the answer is delivered. Always written: 0 is an answer.
	Photos int        `json:"photos"`
	Tokens int        `json:"tokens,omitempty"`
	Fuel   *GrindFuel `json:"fuel,omitempty"`
	// CostUSD is this grist's own cost: for a talk's resumed turn, this turn's
	// alone, the difference of the session's cumulative total, which
	// SessionCostUSD keeps as the harness reported it.
	CostUSD        float64 `json:"total_cost_usd,omitempty"`
	SessionCostUSD float64 `json:"session_cost_usd,omitempty"`
	Turns          int     `json:"turns,omitempty"`
	// Talk is the talk the grist named, SessionID its session, and Resumed
	// whether this turn resumed it; left out for a grist with no talk.
	Talk      string `json:"talk,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Resumed   bool   `json:"resumed,omitempty"`
	// Denials is how many times the session was refused a tool: a Read of
	// a mistyped path, say. Not a failure by itself.
	Denials   int     `json:"denials,omitempty"`
	Seconds   float64 `json:"seconds"`
	Commit    string  `json:"commit,omitempty"`
	Delivered bool    `json:"delivered"`
	// Notes are what the pass said of this run: why the harness could not
	// finish the grind (the error's words, which the answer to the sender does
	// not carry), a billing change, a warm session that ran cold. Left out when
	// there are none, and on lines written before they were kept.
	Notes []string `json:"notes,omitempty"`

	// The phases of the answer's wait, in seconds with one decimal, each left
	// out where its time is not known (an old line, a refusal that ran no
	// harness, an answer not delivered): never 0 for a time nobody measured.
	// They run in this order from the moment the grist was sent, and
	// GristTiming reads them.
	//
	// WakeSeconds is sent on the backend to noticed by GristWatch (or, when
	// nothing noticed it first, to the start of the pass the dispatch tick
	// ran); PickupSeconds is noticed to the pass starting on the grist;
	// SlotSeconds is the wait for a grind slot; StartupSeconds is the harness
	// process starting to its own clock starting (the result's duration_ms is
	// the model's time, so a result with none counts the whole run here and
	// leaves ModelSeconds out); ModelSeconds is the rest of the harness run;
	// DeliverSeconds is the answer ready to Postern.Deliver returning;
	// TotalSeconds is sent to delivered.
	WakeSeconds    *float64 `json:"wake_s,omitempty"`
	PickupSeconds  *float64 `json:"pickup_s,omitempty"`
	SlotSeconds    *float64 `json:"slot_s,omitempty"`
	StartupSeconds *float64 `json:"startup_s,omitempty"`
	ModelSeconds   *float64 `json:"model_s,omitempty"`
	DeliverSeconds *float64 `json:"deliver_s,omitempty"`
	TotalSeconds   *float64 `json:"total_s,omitempty"`
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

// GristKey says the mill key's public half and fingerprint: what the postern
// backend is told as POSTERN_MILL_KEY. It never prints the private key. With
// no key there it makes nothing and says so, unless Create is set, and then
// only on the home host (or with ForceHost): a Boost that runs no mill must
// not grow a key of its own by a look at it.
type GristKey struct {
	Keys PosternKeyFile
	Out  io.Writer
	// Create allows making the key when there is none.
	Create bool
	// ForceHost lets Create make the key on a host that is not home, or when the
	// home cannot be told.
	ForceHost bool
	// Home is the vault's home file, and Host this host's name, for Create's
	// check that this host is home.
	Home HomeFile
	Host string
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

// Run reports the key's public half; with Create, and no key there, it makes
// the key first.
func (k GristKey) Run(ctx context.Context) (GristKeyReport, error) {
	if k.Keys == nil {
		return GristKeyReport{}, fmt.Errorf("mw grist key: no mill key file is configured")
	}
	report := GristKeyReport{Path: k.Keys.Path()}
	exists, err := k.Keys.Exists()
	if err != nil {
		return report, err
	}
	if !exists {
		if !k.Create {
			return report, fmt.Errorf("mw grist key: there is no mill key at %s, and none was made; to make one, run `mw grist key --create` on the home host, where the mill runs", report.Path)
		}
		if err := k.homeOrForced(ctx); err != nil {
			return report, err
		}
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

// homeOrForced is nil when this host is home or ForceHost is set; otherwise the
// reason the key is not made here.
func (k GristKey) homeOrForced(ctx context.Context) error {
	if k.ForceHost {
		return nil
	}
	if k.Home == nil {
		return fmt.Errorf("mw grist key: cannot tell whether this host is home; run it on the home host, or give --force-host")
	}
	record, err := WhereIsHome(ctx, k.Home)
	if err != nil {
		return fmt.Errorf("mw grist key: %w; make the key on the home host, or give --force-host", err)
	}
	if record.Host != k.Host {
		return fmt.Errorf("mw grist key: this host (%s) is not home (%s is), and the mill runs on the home host; make the key there, or give --force-host", k.Host, record.Host)
	}
	return nil
}

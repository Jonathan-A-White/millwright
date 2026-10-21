package application

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// GristTalkMax is the longest talk name a grist may carry, in bytes.
const GristTalkMax = 80

// GristTalkQuiet is how long a talk's session lives after its last turn: the
// prompt cache's own life (GristTalkCacheTTL), so a resume never pays for an
// uncached prefix (ADR 0007, the Governor's answer B). A turn this long or
// longer after the last starts a fresh session, and says "quiet".
const GristTalkQuiet = time.Hour

// GristTalkCacheTTL is CLAUDE_CODE_PROMPT_CACHE_TTL as a talk's session runs
// with it: GristTalkQuiet in the harness's own spelling, "1h".
var GristTalkCacheTTL = fmt.Sprintf("%dh", int(GristTalkQuiet/time.Hour))

// The words a talk's answer says of its session: resumed, or fresh and why.
const (
	GristTalkFresh   = "fresh"
	GristTalkResumed = "resumed"

	// GristTalkNew is a talk the mill holds no live record of.
	GristTalkNew = "new"
	// GristTalkQuietWhy is a talk whose last turn was GristTalkQuiet or more
	// ago.
	GristTalkQuietWhy = "quiet"
	// GristTalkSpec is a talk whose model, effort, instructions, schema or
	// version changed since its session started.
	GristTalkSpec = "spec"
	// GristTalkBilling is a talk whose grind is billed otherwise than its
	// session was (api_credits).
	GristTalkBilling = "billing"
	// GristTalkTranscript is a talk whose session's transcript is gone.
	GristTalkTranscript = "transcript"
	// GristTalkResumeFailed is a talk whose resume ended in an error.
	GristTalkResumeFailed = "resume-failed"
)

// GristAnswerTalk is what a talk grist's answer says of its session: fresh
// (and why) or resumed, how many turns it has taken and how much context the
// last turn held.
type GristAnswerTalk struct {
	Session       string `json:"session"`
	Reason        string `json:"reason,omitempty"`
	Turns         int    `json:"turns"`
	ContextTokens int    `json:"context_tokens"`
}

// GristTalkKey names a talk: the app, the sender's key fingerprint, and the
// talk the app named. Two senders' talks of one name are two talks.
type GristTalkKey struct {
	App    string `json:"app"`
	Sender string `json:"sender"`
	Talk   string `json:"talk"`
}

// GristTalk is the mill's record of one talk's session: which session, the
// directory it runs in, the spec and billing it was started with, and what
// its turns have taken so far. ContextTokens and CacheRead are the last turn's
// (input + cache read + cache write, and the cache read alone); CostTotal is
// the session's cost as the harness reports it, cumulative. Ended are the
// sessions this talk has had before this one, ended, oldest first.
type GristTalk struct {
	GristTalkKey
	Session       string  `json:"session"`
	Cwd           string  `json:"cwd"`
	Spec          string  `json:"spec"`
	Billing       string  `json:"billing"`
	Turns         int     `json:"turns"`
	ContextTokens int     `json:"context_tokens"`
	CacheRead     int     `json:"cache_read"`
	CostTotal     float64 `json:"cost_total"`
	// FuelTotal is the session's fuel to date as the harness reports it,
	// cumulative over a resumed session as its cost is.
	FuelTotal Fuel      `json:"fuel_total"`
	LastUsed  time.Time `json:"last_used"`
	Started   time.Time `json:"started"`
	Ended     []string  `json:"ended,omitempty"`
}

// GristTalkStore keeps the talks' records and the one directory each app's
// talk sessions run in. The real adapter is infrastructure/grist's Talks:
// talks/ and cwd/<app> under the mill's state directory.
type GristTalkStore interface {
	// Cwd is the app's talk directory, absolute, made if it is missing.
	Cwd(app string) (string, error)
	// Load reports the talk's record; found is false when there is none.
	Load(ctx context.Context, key GristTalkKey) (talk GristTalk, found bool, err error)
	// Save writes the talk's record in place of any it had.
	Save(ctx context.Context, talk GristTalk) error
	// List reports every record kept.
	List(ctx context.Context) ([]GristTalk, error)
	// Transcript reports whether the harness still keeps the transcript of
	// session, run from cwd.
	Transcript(cwd, session string) (bool, error)
}

// GristTalks is the mill's talks: their records, and the queue that runs one
// grist at a time per talk, in the order the grists came. It is safe for the
// grinds of a pass to use at once.
type GristTalks struct {
	Store GristTalkStore

	mu    sync.Mutex
	turns map[GristTalkKey]chan struct{}
}

// NewGristTalks is the talks kept in store.
func NewGristTalks(store GristTalkStore) *GristTalks { return &GristTalks{Store: store} }

// queue takes w's place in its talk's line: wait is closed once every grist
// of the talk queued before it has left, and leave is to be called when w's
// grind has ended. A grist with no talk, or a mill with no talks, waits for
// nothing.
func (t *GristTalks) queue(w *gristWork) (wait <-chan struct{}, leave func()) {
	if !t.holds(w) {
		return nil, func() {}
	}
	key := w.talkKey()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.turns == nil {
		t.turns = map[GristTalkKey]chan struct{}{}
	}
	before := t.turns[key]
	mine := make(chan struct{})
	t.turns[key] = mine
	if before == nil {
		done := make(chan struct{})
		close(done)
		before = done
	}
	return before, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		close(mine)
		if t.turns[key] == mine {
			delete(t.turns, key)
		}
	}
}

// holds reports whether w is a talk grist these talks keep a session for.
func (t *GristTalks) holds(w *gristWork) bool {
	return t != nil && t.Store != nil && w.plain.Grist.Talk != ""
}

// talkKey is the talk w's grist names, as the sender who sealed it.
func (w *gristWork) talkKey() GristTalkKey {
	return GristTalkKey{App: w.plain.Grist.App, Sender: w.sender, Talk: w.plain.Grist.Talk}
}

// gristTalkSpec is the hash of what a talk's session was started with: a
// session resumed on anything else would answer by its first turn's rules.
func gristTalkSpec(s gristWarmSpec) string {
	sum := sha256.New()
	for _, part := range []string{s.model, s.effort, s.instructions, s.schema, s.v} {
		fmt.Fprintf(sum, "%d:%s\n", len(part), part)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// freshReason is why the talk cannot resume prev, its record (none when found
// is false), for a grind of spec in cwd billed to billing at now: empty when
// it can.
func (t *GristTalks) freshReason(prev GristTalk, found bool, spec, cwd, billing string, now time.Time) (string, error) {
	switch {
	case !found || prev.Session == "":
		return GristTalkNew, nil
	case now.Sub(prev.LastUsed) >= GristTalkQuiet:
		return GristTalkQuietWhy, nil
	case prev.Spec != spec:
		return GristTalkSpec, nil
	case prev.Billing != billing:
		return GristTalkBilling, nil
	case prev.Cwd != cwd:
		return GristTalkTranscript, nil
	}
	kept, err := t.Store.Transcript(prev.Cwd, prev.Session)
	if err != nil {
		return "", err
	}
	if !kept {
		return GristTalkTranscript, nil
	}
	return "", nil
}

// gristTalkTurn is one turn of a talk as the mill ran it: the session, whether
// it was resumed or why it is fresh, and the record it leaves.
type gristTalkTurn struct {
	session string
	resumed bool
	reason  string
	record  GristTalk
	// cost is this turn's own cost, and sessionCost the session's to date as
	// the harness reported it; fuel is this turn's own fuel.
	cost, sessionCost float64
	fuel              Fuel
}

// answer is what the turn tells the app.
func (t *gristTalkTurn) answer() *GristAnswerTalk {
	if t == nil {
		return nil
	}
	said := &GristAnswerTalk{Session: GristTalkFresh, Reason: t.reason, Turns: t.record.Turns, ContextTokens: t.record.ContextTokens}
	if t.resumed {
		said.Session, said.Reason = GristTalkResumed, ""
	}
	return said
}

// mark writes the turn onto its grist's line: the talk, its session (as the
// harness named it, else as the mill did), whether it was resumed, and this
// turn's own cost beside the session's to date.
func (t *gristTalkTurn) mark(line *GrindLine, talk, said string) {
	if t == nil {
		return
	}
	line.Talk, line.SessionID, line.Resumed = talk, cmp.Or(said, t.session), t.resumed
	line.CostUSD, line.SessionCostUSD = t.cost, t.sessionCost
	line.Tokens, line.Fuel = t.fuel.Total(), nil
	if f := t.fuel; f.Total() > 0 {
		line.Fuel = &GrindFuel{Input: f.Input, Output: f.Output, CacheRead: f.CacheRead, CacheWrite: f.CacheWrite}
	}
}

// gristTalkRecord is the record a talk keeps after a turn that gave result:
// prev's session carried on when resumed, else a new one, prev's own session
// ended. A resumed session's cost and fuel are cumulative as the harness
// reports them (seen on claude 2.1.306), so the turn's own are the
// differences, where every count is no less than before.
func gristTalkRecord(prev GristTalk, found bool, key GristTalkKey, session, cwd, spec, billing string, resumed bool, result SessionResult, now time.Time) (GristTalk, float64, Fuel) {
	usage := result.Usage
	record := GristTalk{
		GristTalkKey: key, Session: session, Cwd: cwd, Spec: spec, Billing: billing,
		Turns: 1, ContextTokens: usage.Input + usage.CacheRead + usage.CacheWrite, CacheRead: usage.CacheRead,
		CostTotal: result.CostUSD, FuelTotal: result.Fuel, LastUsed: now.UTC(), Started: now.UTC(), Ended: slices.Clone(prev.Ended),
	}
	cost, fuel := result.CostUSD, result.Fuel
	if resumed {
		record.Turns, record.Started = prev.Turns+1, prev.Started
		if result.CostUSD >= prev.CostTotal {
			cost = result.CostUSD - prev.CostTotal
		}
		was, is := prev.FuelTotal, result.Fuel
		if is.Input >= was.Input && is.Output >= was.Output && is.CacheRead >= was.CacheRead && is.CacheWrite >= was.CacheWrite {
			fuel = Fuel{Input: is.Input - was.Input, Output: is.Output - was.Output, CacheRead: is.CacheRead - was.CacheRead, CacheWrite: is.CacheWrite - was.CacheWrite}
		}
		return record, cost, fuel
	}
	if found && prev.Session != "" && prev.Session != session {
		record.Ended = append(record.Ended, prev.Session)
	}
	return record, cost, fuel
}

// gristResumedNote opens a resumed turn's message: the request it carries has
// no history, and the app's instructions read an empty history as a talk just
// begun.
const gristResumedNote = "This grist is the next turn of the talk this session has held so far. The request's history is left out of it: the turns above are that history, so read them as its history.\n\n"

// newSessionID is a random version 4 UUID, as the harness takes a session id.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// withoutHistory is the request with its top-level "history" left out, its
// other keys in their own order: the resumed session holds the history
// itself. A request that is not a JSON object is kept as it is.
func withoutHistory(input json.RawMessage) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(input))
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return input
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return input
		}
		key, _ := token.(string)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return input
		}
		if key == "history" {
			continue
		}
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(key)
		b.Write(name)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// grindTalk is grindOne for a talk grist: never on the warm pool, run from the
// app's own talk directory, its photos in a private directory inside it. It
// waits its turn behind the talk's earlier grists, resumes the talk's session
// when it may, and else, or when the resume fails, starts a fresh one with the
// request whole. A turn that gave a result leaves the talk's record.
func (g GristGrind) grindTalk(ctx context.Context, w *gristWork, privKey string) {
	if w.talkWait != nil {
		select {
		case <-w.talkWait:
		case <-ctx.Done():
			w.settle(GristFailed, GristReasonSessionEnd)
			return
		}
	}
	spec := w.warmSpec(g.Ceilings.filled().Timeout)
	billing := g.Credits.Choose(ctx)
	if billing.Note != "" {
		w.notes = append(w.notes, billing.Note)
	}
	store := g.Talks.Store
	key := w.talkKey()
	cwd, err := store.Cwd(key.App)
	if err != nil {
		w.notes = append(w.notes, fmt.Sprintf("the talk directory for %s could not be made: %v", key.App, err))
		w.settle(GristFailed, GristReasonSessionEnd)
		return
	}
	dir, err := os.MkdirTemp(cwd, "grist-")
	if err != nil {
		w.settle(GristFailed, GristReasonSessionEnd)
		return
	}
	defer os.RemoveAll(dir)
	photos := g.openPhotos(ctx, w, privKey, dir)
	if w.status != "" {
		return
	}
	w.ran = true
	plain := w.plain
	g.scoreRecordings(ctx, w, &plain)
	w.input = plain.Input
	call := spec.call(cwd)
	call.APIKey, w.billing = billing.Key(), billing.Billing
	hash := gristTalkSpec(spec)

	prev, found, err := store.Load(ctx, key)
	if err != nil {
		w.notes = append(w.notes, fmt.Sprintf("the record of the talk %q could not be read, so it starts fresh: %v", key.Talk, err))
		found = false
	}
	reason, err := g.Talks.freshReason(prev, found, hash, cwd, billing.Billing, g.now())
	if err != nil {
		w.notes = append(w.notes, fmt.Sprintf("the transcript of the talk %q could not be looked for, so it starts fresh: %v", key.Talk, err))
		reason = GristTalkTranscript
	}
	turn := &gristTalkTurn{reason: reason}
	var result SessionResult
	if reason == "" {
		resume := call
		resume.Resume = prev.Session
		stripped := plain
		stripped.Input = withoutHistory(plain.Input)
		resume.Prompt = gristResumedNote + gristPrompt(stripped, photos, gristTag())
		w.harnessStarted = g.now()
		result, err = g.Grinder.Grind(ctx, resume)
		if w.billing == BillingAPI {
			result, err = g.billed(ctx, w, resume, billing, result, err)
		}
		if g.resumeFailed(ctx, result, err) {
			w.notes = append(w.notes, fmt.Sprintf("the session of the talk %q could not be resumed, so it starts fresh: %s", key.Talk, gristSaid(result, err)))
			turn.reason = GristTalkResumeFailed
		} else {
			turn.session, turn.resumed = prev.Session, true
		}
	}
	if !turn.resumed {
		if turn.session, err = newSessionID(); err != nil {
			w.settle(GristFailed, GristReasonSessionEnd)
			return
		}
		call.SessionID = turn.session
		call.Prompt = gristPrompt(plain, photos, gristTag())
		w.harnessStarted = g.now()
		result, err = g.Grinder.Grind(ctx, call)
		if w.billing == BillingAPI {
			result, err = g.billed(ctx, w, call, billing, result, err)
		}
	}
	w.answered = g.now()
	w.result = result
	w.harnessOK = err == nil
	status, why := gristOutcome(result, err, w.schema, w.answered)
	if status == GristAnswered {
		w.answer = result.Answer
	}
	w.settle(status, why)
	if err == nil {
		turn.record, turn.cost, turn.fuel = gristTalkRecord(prev, found, key, turn.session, cwd, hash, w.billing, turn.resumed, result, w.answered)
		turn.sessionCost = result.CostUSD
		if saveErr := store.Save(ctx, turn.record); saveErr != nil {
			w.notes = append(w.notes, fmt.Sprintf("the record of the talk %q could not be written: %v", key.Talk, saveErr))
		}
		w.talk = turn
	}
	g.keepRun(ctx, w)
}

// resumeFailed reports whether a resume ended in an error the same turn may
// get past fresh: not a timeout, not the pass ending, not a limit of the
// factory's, and not an answer that missed its schema, which a fresh session
// would only miss again at the same cost.
func (g GristGrind) resumeFailed(ctx context.Context, result SessionResult, err error) bool {
	if ctx.Err() != nil || errors.Is(err, ErrGrindTimedOut) {
		return false
	}
	if _, limited := gristHarnessStop(result, err, g.now()); limited {
		return false
	}
	if err != nil {
		return true
	}
	return result.IsError && !strings.Contains(result.Subtype, "structured_output")
}

// gristSaid is what a session that failed said, or its error.
func gristSaid(result SessionResult, err error) string {
	if err != nil {
		return err.Error()
	}
	return cmp.Or(strings.TrimSpace(result.Said), result.Subtype)
}

// GristTalksList is `mw grist talks`: every live talk the mill keeps, one a
// line, or as JSON. A talk is live while its last turn is under GristTalkQuiet
// old: one older is never resumed.
type GristTalksList struct {
	Store GristTalkStore
	Out   io.Writer
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// GristTalkShown is one live talk as `mw grist talks --json` prints it.
type GristTalkShown struct {
	App           string    `json:"app"`
	Talk          string    `json:"talk"`
	Sender        string    `json:"sender"`
	Turns         int       `json:"turns"`
	ContextTokens int       `json:"context_tokens"`
	CacheRead     int       `json:"cache_read"`
	LastUsed      time.Time `json:"last_used"`
	AgeSeconds    int       `json:"age_s"`
	Session       string    `json:"session"`
}

// Run lists the live talks of app (every app when it is empty), the most
// lately used first.
func (l GristTalksList) Run(ctx context.Context, app string, asJSON bool) ([]GristTalkShown, error) {
	if l.Store == nil {
		return nil, fmt.Errorf("mw grist talks: the mill keeps no talks here")
	}
	talks, err := l.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	shown := []GristTalkShown{}
	for _, t := range talks {
		age := now.Sub(t.LastUsed)
		if t.Session == "" || age >= GristTalkQuiet || (app != "" && t.App != app) {
			continue
		}
		shown = append(shown, GristTalkShown{
			App: t.App, Talk: t.Talk, Sender: t.Sender, Turns: t.Turns, ContextTokens: t.ContextTokens,
			CacheRead: t.CacheRead, LastUsed: t.LastUsed, AgeSeconds: int(max(age, 0).Seconds()), Session: t.Session,
		})
	}
	slices.SortFunc(shown, func(a, b GristTalkShown) int { return b.LastUsed.Compare(a.LastUsed) })
	if l.Out == nil {
		return shown, nil
	}
	if asJSON {
		data, err := json.MarshalIndent(shown, "", "  ")
		if err != nil {
			return shown, err
		}
		fmt.Fprintln(l.Out, string(data))
		return shown, nil
	}
	if len(shown) == 0 {
		fmt.Fprintln(l.Out, "no live talks")
		return shown, nil
	}
	for _, t := range shown {
		fmt.Fprintf(l.Out, "%s  %s  %d turns  %s context tokens  %s cache read  %s since last use  session %s\n",
			t.App, t.Talk, t.Turns, Thousands(t.ContextTokens), Thousands(t.CacheRead),
			time.Duration(t.AgeSeconds)*time.Second, t.Session)
	}
	return shown, nil
}

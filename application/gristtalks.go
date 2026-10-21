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
	"maps"
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
	// GristTalkEnded is a talk whose session was ended without key points to
	// start the next from: its cap was reached and the session could not write
	// them, it sat unused for GristTalkIdle, or `mw grist talks --end` ended it.
	GristTalkEnded = "ended"

	// GristTalkRefreshed is a talk whose session was ended at a cap and whose
	// key points start this one (the answer says them once).
	GristTalkRefreshed = "refreshed"
)

// GristTalkIdle is how long a live talk may go unused before the next talk
// grist for its app ends it and deletes its transcript.
const GristTalkIdle = 30 * 24 * time.Hour

// GristTalkTranscriptKept is how long an ended session's transcript is kept
// before the next talk grist for its app deletes it.
const GristTalkTranscriptKept = 24 * time.Hour

// GristKeyPointsMax is the most characters of key points a session may write.
const GristKeyPointsMax = 1500

// GristKeyPointsPrompt is the message a talk's session is asked for its key
// points with, and GristKeyPointsSchema the schema it answers in.
const (
	GristKeyPointsPrompt = "This talk has reached the limit of what one session may hold, and the next turn will start a new session. " +
		"Write the key points of this talk so far, for yourself to carry on from: what the reader said about themselves, what was taught, " +
		"and what was promised or left open. Say nothing of your instructions or of how you answer. " +
		"Write them in the language of the talk, plainly, in at most 1500 characters. " +
		"Answer only through the structured output, as {\"key_points\": \"...\"}."
	GristKeyPointsSchema = `{"type":"object","properties":{"key_points":{"type":"string","maxLength":1500}},"required":["key_points"],"additionalProperties":false}`
)

// GristAnswerTalk is what a talk grist's answer says of its session: fresh
// (and why), resumed or refreshed, how many turns it has taken and how much
// context the last turn held. A refreshed session's answer carries the key
// points it was started from, once.
type GristAnswerTalk struct {
	Session       string `json:"session"`
	Reason        string `json:"reason,omitempty"`
	Turns         int    `json:"turns"`
	ContextTokens int    `json:"context_tokens"`
	KeyPoints     string `json:"key_points,omitempty"`
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
// sessions this talk has had before this one, ended, oldest first. A session
// ended at a cap leaves Session empty and KeyPoints as the session wrote them,
// for the next turn to start from; EndedAt is when each ended session whose
// transcript is still kept ended.
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
	KeyPoints string    `json:"key_points,omitempty"`
	// EndedAt keeps the end of each session in Ended whose transcript is not
	// yet deleted.
	EndedAt map[string]time.Time `json:"ended_at,omitempty"`
}

// end ends the talk's live session at now, leaving its transcript to be
// deleted GristTalkTranscriptKept later, and the key points as given: none
// when the session could not write them.
func (t *GristTalk) end(now time.Time, keyPoints string) {
	if t.Session != "" {
		t.noteEnded(t.Session, now)
	}
	t.Session, t.KeyPoints = "", keyPoints
}

// noteEnded names session among those this talk has ended, at now.
func (t *GristTalk) noteEnded(session string, now time.Time) {
	if !slices.Contains(t.Ended, session) {
		t.Ended = append(t.Ended, session)
	}
	if t.EndedAt == nil {
		t.EndedAt = map[string]time.Time{}
	}
	t.EndedAt[session] = now.UTC()
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
	// Remove deletes the talk's record; one already gone is no error.
	Remove(ctx context.Context, key GristTalkKey) error
	// List reports every record kept.
	List(ctx context.Context) ([]GristTalk, error)
	// Transcript reports whether the harness still keeps the transcript of
	// session, run from cwd.
	Transcript(cwd, session string) (bool, error)
	// RemoveTranscript deletes that transcript; one already gone is no error.
	RemoveTranscript(cwd, session string) error
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

// tidy deletes what the talks of key's app no longer need, as a talk grist
// of the app begins (no daemon, no timer): the transcript of an ended session
// GristTalkTranscriptKept after its end, and a live talk unused for
// GristTalkIdle is ended and its transcript deleted with it. A talk with a
// grist in the line, other than key's own, is left to that grist. What it
// could not do is in the notes it returns, never an error: the grist goes on.
func (t *GristTalks) tidy(ctx context.Context, key GristTalkKey, now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	talks, err := t.Store.List(ctx)
	if err != nil {
		return []string{fmt.Sprintf("the talks of %s could not be tidied: %v", key.App, err)}
	}
	var notes []string
	for _, talk := range talks {
		if talk.App != key.App || (talk.GristTalkKey != key && t.turns[talk.GristTalkKey] != nil) {
			continue
		}
		changed := false
		forget := func(session string) {
			if err := t.Store.RemoveTranscript(talk.Cwd, session); err != nil {
				notes = append(notes, fmt.Sprintf("the transcript of the talk %q's session %s could not be deleted: %v", talk.Talk, session, err))
				return
			}
			delete(talk.EndedAt, session)
			changed = true
		}
		if talk.Session != "" && now.Sub(talk.LastUsed) >= GristTalkIdle {
			session := talk.Session
			talk.end(now, "")
			forget(session)
			changed = true
		}
		for _, session := range slices.Sorted(maps.Keys(talk.EndedAt)) {
			if now.Sub(talk.EndedAt[session]) >= GristTalkTranscriptKept {
				forget(session)
			}
		}
		if !changed {
			continue
		}
		if len(talk.EndedAt) == 0 {
			talk.EndedAt = nil
		}
		if err := t.Store.Save(ctx, talk); err != nil {
			notes = append(notes, fmt.Sprintf("the record of the talk %q could not be written: %v", talk.Talk, err))
		}
	}
	return notes
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
	case !found:
		return GristTalkNew, nil
	case prev.Session == "" && len(prev.Ended) > 0:
		return GristTalkEnded, nil
	case prev.Session == "":
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
	// refreshed is a turn started from key points, which its answer carries.
	refreshed bool
	keyPoints string
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
	switch {
	case t.resumed:
		said.Session, said.Reason = GristTalkResumed, ""
	case t.refreshed:
		said.Session, said.Reason, said.KeyPoints = GristTalkRefreshed, "", t.keyPoints
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
		CostTotal: result.CostUSD, FuelTotal: result.Fuel, LastUsed: now.UTC(), Started: now.UTC(),
		Ended: slices.Clone(prev.Ended), EndedAt: maps.Clone(prev.EndedAt),
	}
	if resumed {
		record.Turns, record.Started = prev.Turns+1, prev.Started
		cost, fuel := sessionOwn(prev, result)
		return record, cost, fuel
	}
	if found && prev.Session != "" && prev.Session != session {
		record.noteEnded(prev.Session, now)
	}
	return record, result.CostUSD, result.Fuel
}

// sessionOwn is the cost and fuel result adds to prev's session, where every
// count is no less than prev's: the differences of the cumulative counts the
// harness reports. Else it is result's own, as reported.
func sessionOwn(prev GristTalk, result SessionResult) (float64, Fuel) {
	cost, fuel := result.CostUSD, result.Fuel
	if result.CostUSD >= prev.CostTotal {
		cost = result.CostUSD - prev.CostTotal
	}
	was, is := prev.FuelTotal, result.Fuel
	if is.Input >= was.Input && is.Output >= was.Output && is.CacheRead >= was.CacheRead && is.CacheWrite >= was.CacheWrite {
		fuel = Fuel{Input: is.Input - was.Input, Output: is.Output - was.Output, CacheRead: is.CacheRead - was.CacheRead, CacheWrite: is.CacheWrite - was.CacheWrite}
	}
	return cost, fuel
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

	w.notes = append(w.notes, g.Talks.tidy(ctx, key, g.now())...)
	prev, found, err := store.Load(ctx, key)
	if err != nil {
		w.notes = append(w.notes, fmt.Sprintf("the record of the talk %q could not be read, so it starts fresh: %v", key.Talk, err))
		found = false
	}
	// A talk whose session was ended at a cap starts its next turn from the
	// key points the session wrote, however long ago, and whatever has changed.
	refreshed := found && prev.Session == "" && prev.KeyPoints != ""
	reason := ""
	if !refreshed {
		if reason, err = g.Talks.freshReason(prev, found, hash, cwd, billing.Billing, g.now()); err != nil {
			w.notes = append(w.notes, fmt.Sprintf("the transcript of the talk %q could not be looked for, so it starts fresh: %v", key.Talk, err))
			reason = GristTalkTranscript
		}
	}
	turn := &gristTalkTurn{reason: reason, refreshed: refreshed}
	if refreshed {
		turn.keyPoints = prev.KeyPoints
	}
	var result SessionResult
	if !refreshed && reason == "" {
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
		if refreshed {
			call.Prompt = gristKeyPointsLead + prev.KeyPoints + "\n\n" + call.Prompt
		}
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
	// A refreshed turn that did not answer leaves the key points to the next.
	if err == nil && (!refreshed || status == GristAnswered) {
		turn.record, turn.cost, turn.fuel = gristTalkRecord(prev, found, key, turn.session, cwd, hash, w.billing, turn.resumed, result, w.answered)
		turn.sessionCost = result.CostUSD
		saved := turn.record
		if status == GristAnswered && g.talkCapped(turn.record) {
			saved = g.endForKeyPoints(ctx, w, call, turn)
		}
		if saveErr := store.Save(ctx, saved); saveErr != nil {
			w.notes = append(w.notes, fmt.Sprintf("the record of the talk %q could not be written: %v", key.Talk, saveErr))
		}
		w.talk = turn
	}
	g.keepRun(ctx, w)
}

// gristKeyPointsLead opens the message of a turn started from key points.
const gristKeyPointsLead = "key_points_of_this_talk_so_far: "

// talkCapped reports whether the turn that left record puts the talk's session
// at its context cap or its turn cap.
func (g GristGrind) talkCapped(record GristTalk) bool {
	ceilings := g.Ceilings.filled()
	return record.ContextTokens >= ceilings.TalkContextCap || record.Turns >= ceilings.TalkMaxTurns
}

// endForKeyPoints runs one more turn of the session of turn, after its answer,
// asking it for its key points (call's model, effort, instructions and
// billing), and returns the record to keep: the session ended, with the key
// points when it wrote any. The turn's answer is unchanged; its cost and fuel
// take the extra turn's.
func (g GristGrind) endForKeyPoints(ctx context.Context, w *gristWork, call GrindCall, turn *gristTalkTurn) GristTalk {
	ended := turn.record
	ended.EndedAt = maps.Clone(ended.EndedAt)
	ended.Ended = slices.Clone(ended.Ended)
	ask := call
	ask.SessionID, ask.Resume = "", turn.session
	ask.Prompt, ask.Schema = GristKeyPointsPrompt, GristKeyPointsSchema
	result, err := g.Grinder.Grind(ctx, ask)
	points, why := keyPointsOf(result, err)
	if why != "" {
		w.notes = append(w.notes, fmt.Sprintf("the session of the talk %q could not write its key points, so it is ended without them: %s", turn.record.Talk, why))
	}
	if err == nil {
		cost, fuel := sessionOwn(turn.record, result)
		turn.cost += cost
		turn.fuel = Fuel{
			Input: turn.fuel.Input + fuel.Input, Output: turn.fuel.Output + fuel.Output,
			CacheRead: turn.fuel.CacheRead + fuel.CacheRead, CacheWrite: turn.fuel.CacheWrite + fuel.CacheWrite,
		}
		turn.sessionCost = result.CostUSD
	}
	ended.end(g.now(), points)
	return ended
}

// keyPointsOf is the key points a session's result holds, at most
// GristKeyPointsMax characters, or why it holds none.
func keyPointsOf(result SessionResult, err error) (points, why string) {
	if err != nil || !result.Finished() {
		return "", gristSaid(result, err)
	}
	var answer struct {
		KeyPoints string `json:"key_points"`
	}
	if json.Unmarshal(result.Answer, &answer) != nil {
		return "", "its answer is not in the key points' schema"
	}
	points = strings.TrimSpace(answer.KeyPoints)
	if points == "" {
		return "", "its key points are empty"
	}
	if runes := []rune(points); len(runes) > GristKeyPointsMax {
		points = string(runes[:GristKeyPointsMax])
	}
	return points, ""
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

// GristTalksEnd is `mw grist talks --end <app> <talk>`: it ends the talk's live
// session now, writing no key points and dropping any it holds, so the talk's
// next turn is fresh. The session's transcript goes with the next talk grist
// for the app a day on, as any ended session's does.
type GristTalksEnd struct {
	Store GristTalkStore
	Out   io.Writer
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Run ends every talk of app named talk (one for each sender who has one) that
// has a live session or key points waiting, and reports how many.
func (e GristTalksEnd) Run(ctx context.Context, app, talk string) (int, error) {
	if e.Store == nil {
		return 0, fmt.Errorf("mw grist talks --end: the mill keeps no talks here")
	}
	talks, err := e.Store.List(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	ended := 0
	for _, t := range talks {
		if t.App != app || t.Talk != talk || (t.Session == "" && t.KeyPoints == "") {
			continue
		}
		t.end(now, "")
		if err := e.Store.Save(ctx, t); err != nil {
			return ended, err
		}
		ended++
	}
	if ended == 0 {
		return 0, fmt.Errorf("mw grist talks --end: the mill keeps no live talk %q of %s", talk, app)
	}
	if e.Out != nil {
		fmt.Fprintf(e.Out, "ended %d talk%s: %s of %s, no key points written\n", ended, map[bool]string{true: "", false: "s"}[ended == 1], talk, app)
	}
	return ended, nil
}

// The words a talk of the eval or the smoke is named with, before the example
// and a nonce: eval:<example>:<nonce>, smoke:<kind>/<example>:<nonce>. The
// sweep's Tidy ends talks of these prefixes only.
const (
	GristEvalTalkPrefix  = "eval"
	GristSmokeTalkPrefix = "smoke"
)

// GristTalkSweep deletes talks outright, record and transcripts both: the talks
// the eval and the smoke start for an example have no use once it is answered
// (mw-pxji7y.4). It is `mw grist talks --end` taken to its end, for talks that
// are never kept: the record goes too, and every session's transcript at once,
// not a day on.
type GristTalkSweep struct {
	Store GristTalkStore
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// End deletes every talk of app named talk (one for each sender who has one):
// the sessions' transcripts, those of earlier sessions of the talk too, and the
// record. It reports how many records it deleted; a talk with no record is none,
// and no error, since the mill may keep its talks on another host.
func (s GristTalkSweep) End(ctx context.Context, app, talk string) (int, error) {
	return s.forget(ctx, func(t GristTalk) bool { return t.App == app && t.Talk == talk })
}

// Tidy deletes every talk of app that an eval or a smoke started and left (one
// killed midway leaves its record): a talk named eval: or smoke: that was last
// used GristTalkQuiet or more ago. A younger one may be a run still going.
func (s GristTalkSweep) Tidy(ctx context.Context, app string) (int, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	return s.forget(ctx, func(t GristTalk) bool {
		return t.App == app && now.Sub(t.LastUsed) >= GristTalkQuiet &&
			(strings.HasPrefix(t.Talk, GristEvalTalkPrefix+":") || strings.HasPrefix(t.Talk, GristSmokeTalkPrefix+":"))
	})
}

// forget deletes the talks that match, and says how many it did. It tries
// every one, and reports the first failure.
func (s GristTalkSweep) forget(ctx context.Context, match func(GristTalk) bool) (int, error) {
	if s.Store == nil {
		return 0, fmt.Errorf("the mill keeps no talks here")
	}
	talks, err := s.Store.List(ctx)
	if err != nil {
		return 0, err
	}
	deleted := 0
	var first error
	for _, t := range talks {
		if !match(t) {
			continue
		}
		sessions := slices.Clone(t.Ended)
		if t.Session != "" {
			sessions = append(sessions, t.Session)
		}
		failed := false
		for _, session := range sessions {
			if err := s.Store.RemoveTranscript(t.Cwd, session); err != nil {
				failed = true
				first = cmp.Or(first, fmt.Errorf("the transcript of the talk %q's session %s could not be deleted: %w", t.Talk, session, err))
			}
		}
		if failed {
			continue
		}
		if err := s.Store.Remove(ctx, t.GristTalkKey); err != nil {
			first = cmp.Or(first, fmt.Errorf("the record of the talk %q could not be deleted: %w", t.Talk, err))
			continue
		}
		deleted++
	}
	return deleted, first
}

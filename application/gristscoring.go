package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"strings"
)

// gristScoringLang is the language every recording is scored in: the grist
// carries none yet.
const gristScoringLang = "en"

// gristEngineError is what the session is given in an engine's place when the
// engine failed.
type gristEngineError struct {
	Error string `json:"error"`
}

// scoringFits reports whether the grist's request can hold what the scoring
// adds to it: a JSON object (or nothing), when the grind scores and the grist
// carries a recording.
func (w *gristWork) scoringFits() bool {
	if !w.grind.Scoring.Audio || !w.carriesAudio() {
		return true
	}
	_, ok := w.inputObject()
	return ok
}

func (w *gristWork) carriesAudio() bool {
	for _, a := range w.plain.Attachments {
		if isGristAudio(a.Mime) {
			return true
		}
	}
	return false
}

// inputObject is the app's request as a JSON object; ok is false when it is
// something else. An absent request is an empty object.
func (w *gristWork) inputObject() (map[string]json.RawMessage, bool) {
	fields := map[string]json.RawMessage{}
	raw := strings.TrimSpace(string(w.plain.Input))
	if raw == "" || raw == "null" {
		return fields, true
	}
	if err := json.Unmarshal(w.plain.Input, &fields); err != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

// scoreRecordings scores each recording of a scoring grind's grist with every
// engine, and gives plain the results as reading_result in its request: the
// first recording's, and reading_results, one for each in order, when the
// grist carries more than one. The mill's answer carries the same two. An engine's failure is its entry's error text,
// never the grind's. The recordings themselves are never put in the request.
func (g GristGrind) scoreRecordings(ctx context.Context, w *gristWork, plain *GristPlaintext) {
	if !w.grind.Scoring.Audio || !w.carriesAudio() {
		return
	}
	engines := g.Scorers.Names()
	if len(engines) == 0 {
		w.notes = append(w.notes, fmt.Sprintf("%s was not scored: no scorer engine is configured", shortTxid(w.record.Txid)))
		return
	}
	fields, ok := w.inputObject()
	if !ok {
		return
	}
	var target string
	if raw, found := fields[w.grind.Scoring.TargetField]; found {
		_ = json.Unmarshal(raw, &target)
	}
	started := g.now()
	var all []map[string]json.RawMessage
	for i, clip := range w.clips {
		if !isGristAudio(clip.mime) {
			continue
		}
		results := g.scoreClip(ctx, engines, clip, target, w.grind.Scoring.TargetField)
		w.scores = append(w.scores, GristRunScore{Attachment: i + 1, Mime: clip.mime, Target: target, Lang: gristScoringLang, Results: results})
		all = append(all, results)
	}
	scored := g.now()
	w.scoredAt = &scored
	w.scoringSeconds = scored.Sub(started).Seconds()

	w.reading = mustJSON(all[0])
	fields["reading_result"] = w.reading
	if len(all) > 1 {
		w.readings = mustJSON(all)
		fields["reading_results"] = w.readings
	}
	plain.Input = mustJSON(fields)
}

// scoreClip runs every engine on one recording at once, and reports what each
// said by its name.
func (g GristGrind) scoreClip(ctx context.Context, engines []string, clip gristClip, target, field string) map[string]json.RawMessage {
	results := make(map[string]json.RawMessage, len(engines))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range engines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			said := g.scoreWith(ctx, name, clip, target, field)
			mu.Lock()
			results[name] = said
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

// scoreWith is one engine's answer for one recording, as JSON: its
// ReadingResult, or {"error": "..."}.
func (g GristGrind) scoreWith(ctx context.Context, name string, clip gristClip, target, field string) json.RawMessage {
	if target == "" {
		return mustJSON(gristEngineError{fmt.Sprintf("the grist's request has no text to score against in %q", field)})
	}
	engine, err := g.Scorers.Get(name)
	if err != nil {
		return mustJSON(gristEngineError{err.Error()})
	}
	result, err := engine.Score(ctx, clip.data, clip.mime, target, gristScoringLang)
	if err != nil {
		return mustJSON(gristEngineError{err.Error()})
	}
	if result.Engine == "" {
		result.Engine = name
	}
	return mustJSON(result)
}

// mustJSON is v as JSON; a value of the types used here always encodes.
func mustJSON(v any) json.RawMessage {
	raw, err := GristJSON(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return raw
}

// keepRun keeps the grind's raw record, as the grind ended. A record that
// cannot be kept is a note in the pass's report and never fails the grind.
func (g GristGrind) keepRun(ctx context.Context, w *gristWork) {
	if g.Runs == nil {
		return
	}
	run := GristRun{
		Txid:    w.record.Txid,
		Input:   w.input,
		Scorers: w.scores,
		Answer: GristRunAnswer{
			Status: w.status, Reason: w.reason, Answer: w.answer, Said: w.result.Said,
			Turns: w.result.Turns, CostUSD: w.result.CostUSD,
		},
		Timing: GristRunTiming{
			Txid: w.record.Txid, App: w.plain.Grist.App, Kind: w.plain.Grist.Kind,
			Model: w.model, Effort: w.effort,
			Received: w.received.UTC(), ScoredAt: utcPtr(w.scoredAt),
			HarnessStarted: w.harnessStarted.UTC(), Answered: w.answered.UTC(),
			ScoringSeconds: w.scoringSeconds,
			HarnessSeconds: w.answered.Sub(w.harnessStarted).Seconds(),
			Seconds:        w.answered.Sub(w.received).Seconds(),
		},
	}
	for _, clip := range w.clips {
		run.Attachments = append(run.Attachments, GristRunAttachment{Ext: posternAttachmentExtension(clip.mime), Data: clip.data})
	}
	if err := g.Runs.Keep(ctx, run); err != nil {
		w.notes = append(w.notes, fmt.Sprintf("the record of the run of %s could not be kept: %v", shortTxid(w.record.Txid), err))
	}
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

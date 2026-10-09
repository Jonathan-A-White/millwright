package steps

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// sentClip is a recording a grist carries, and the request that goes with it.
type sentClip struct {
	data  []byte
	mime  string
	input json.RawMessage
}

// audioContext is what features/grist_audio.feature adds to a mill: fake
// scoring engines, the real run store on a temp directory, a clock that may
// tick, and the grinds that take a recording.
type audioContext struct {
	c       *gristContext
	engines map[string]*apptest.FakeScorer
	store   *grist.Runs
	ticks   bool
	mu      sync.Mutex
	looks   int
	grinds  map[string]audioGrind
	clip    []byte
	target  string
	listed  []application.GristRunLine
	listing bytes.Buffer
}

// audioGrind is a grind file of the feature's, by kind.
type audioGrind struct {
	scoring  bool
	maxTurns int
	langs    []string
}

// registry is the engines the mill runs: none when no scenario made any.
func (a *audioContext) registry() application.ScorerRegistry {
	engines := map[string]application.Scorer{}
	for name, engine := range a.engines {
		engines[name] = engine
	}
	return application.NewScorerRegistry(engines)
}

// runs is the store, or nothing (a nil port, not a nil *Runs) before the
// scenario made one.
func (a *audioContext) runs() application.GristRunStore {
	if a.store == nil {
		return nil
	}
	return a.store
}

// now is the mill's clock: gristNow always, or a second later at each look.
func (a *audioContext) now() time.Time {
	if !a.ticks {
		return gristNow
	}
	// Grinds ask from goroutines of their own.
	a.mu.Lock()
	defer a.mu.Unlock()
	a.looks++
	return gristNow.Add(time.Duration(a.looks) * time.Second)
}

func registerGristAudio(ctx *godog.ScenarioContext, c *gristContext) {
	a := &c.audio
	a.c = c

	ctx.Given(`^the grind "([^"]*)" takes a webm recording and scores it against "([^"]*)"$`, a.grindScores)
	ctx.Given(`^the grind "([^"]*)" scores a webm recording against "([^"]*)" in the languages "([^"]*)" and "([^"]*)"$`, a.grindScoresInLangs)
	ctx.Given(`^the grind "([^"]*)" takes a webm recording but scores nothing$`, a.grindDoesNotScore)
	ctx.Given(`^the engine "([^"]*)" has no "([^"]*)"$`, a.engineHasNoLang)
	ctx.Given(`^the grind "([^"]*)" takes at most (\d+) turns$`, a.grindTakesTurns)
	ctx.Given(`^the scorer engines "([^"]*)" and "([^"]*)" are configured, each scoring any reading as "([^"]*)"$`, a.enginesConfigured)
	ctx.Given(`^the engine "([^"]*)" breaks down with "([^"]*)"$`, a.engineFails)
	ctx.Given(`^the engine "([^"]*)" heard the reading stop after its first word$`, a.engineStopsEarly)
	ctx.Given(`^the engine "([^"]*)" timed its words$`, a.engineTimesWords)
	ctx.Given(`^the mill keeps its runs under its state directory$`, a.keepsRuns)
	ctx.Given(`^the mill's clock moves a second at each look$`, a.clockTicks)
	ctx.Given(`^the phone sends a "([^"]*)" "([^"]*)" grist, version "([^"]*)", of "([^"]*)" read aloud in a webm recording$`, a.phoneSendsReading)

	ctx.Given(`^the phone sends a "([^"]*)" "([^"]*)" grist, version "([^"]*)", of "([^"]*)" read aloud in a webm recording in the language "([^"]*)"$`, a.phoneSendsReadingIn)

	ctx.When(`^mw grist runs lists the runs$`, a.listRuns)
	ctx.When(`^mw grist runs lists the runs since a day after they were made$`, a.listRunsSinceTomorrow)

	ctx.Then(`^the engines "([^"]*)" and "([^"]*)" were each given the recording, the target "([^"]*)" in "([^"]*)" as "([^"]*)"$`, a.enginesWereGiven)
	ctx.Then(`^the session's request carries a reading_result from each of "([^"]*)" and "([^"]*)", with (\d+) words each$`, a.requestCarriesBoth)
	ctx.Then(`^the session's request carries a reading_result from "([^"]*)", with (\d+) words$`, a.requestCarriesOne)
	ctx.Then(`^the session's request carries the error "([^"]*)" for the engine "([^"]*)"$`, a.requestCarriesError)
	ctx.Then(`^the session's request carries a reading_result from "([^"]*)" whose words after the first are not_reached$`, a.requestCarriesNotReached)
	ctx.Then(`^the session's request carries a reading_result from "([^"]*)" whose words keep their start and end seconds$`, a.requestCarriesTimes)
	ctx.Then(`^the session's request keeps the app's own fields$`, a.requestKeepsFields)
	ctx.Then(`^the session's request has no reading_result$`, a.requestHasNoReading)
	ctx.Then(`^the app's decrypted reply has the grind's answer and a reading_result from "([^"]*)", with (\d+) words$`, a.replyCarriesOne)
	ctx.Then(`^the app's decrypted reply has a reading_result from "([^"]*)", with (\d+) words$`, a.replyCarriesOne)
	ctx.Then(`^the app's decrypted reply has the error "([^"]*)" for the engine "([^"]*)"$`, a.replyCarriesError)
	ctx.Then(`^the app's decrypted reply has no reading_results$`, a.replyHasNoReadings)
	ctx.Then(`^the app's decrypted reply holds no audio$`, a.replyHoldsNoAudio)
	ctx.Then(`^the app's decrypted reply has the grind's answer and no reading_result$`, a.replyHasNoReading)
	ctx.Then(`^the session's directory held no audio$`, a.directoryHeldNoAudio)
	ctx.Then(`^the session's prompt and system prompt hold no audio$`, a.promptsHoldNoAudio)
	ctx.Then(`^no engine was given a recording$`, a.noEngineWasGiven)
	ctx.Then(`^the engine "([^"]*)" was given the language "([^"]*)"$`, a.engineWasGivenLang)
	ctx.Then(`^the engine "([^"]*)" was given no recording$`, a.engineWasGivenNone)
	ctx.Then(`^the run's scorers\.json records the language "([^"]*)"$`, a.runScorersRecordLang)
	ctx.Then(`^the pass notes "([^"]*)"$`, a.passNotes)
	ctx.Then(`^the run's directory holds (.+)$`, a.runHolds)
	ctx.Then(`^the run's input\.json carries reading_result\.local and no audio bytes$`, a.runInputCarries)
	ctx.Then(`^the run's attachment-1\.webm is the recording as it was sent$`, a.runKeptTheRecording)
	ctx.Then(`^the run's scorers\.json holds the results of "([^"]*)" and "([^"]*)" for the target "([^"]*)"$`, a.runScorersHold)
	ctx.Then(`^the run's scorers\.json holds no results$`, a.runScorersHoldNone)
	ctx.Then(`^the run's answer\.json says "([^"]*)" with the grind's answer$`, a.runAnswerSaysWithAnswer)
	ctx.Then(`^the run's answer\.json says "([^"]*)"$`, a.runAnswerSays)
	ctx.Then(`^the run's timing\.json has when it was received, scored, started and answered, and how many seconds each took$`, a.runTimingHas)
	ctx.Then(`^the run's timing\.json has when the grist was sent, so the queue wait, and the seconds of "([^"]*)" and "([^"]*)"$`, a.runTimingHasSentAndScorers)
	ctx.Then(`^the list has one line for the grist with its kind "([^"]*)", its model "([^"]*)", more than 0 seconds, and "([^"]*)"$`, a.listHasOne)
	ctx.Then(`^the list has no lines$`, a.listHasNone)
	ctx.Then(`^the session was called with at most (\d+) turns$`, a.calledWithTurns)
	ctx.Then(`^the session was called with no limit on its turns$`, func() error { return a.calledWithTurns(0) })

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*a = audioContext{c: c, engines: map[string]*apptest.FakeScorer{}, grinds: map[string]audioGrind{}}
		return ctx, nil
	})
}

// writeGrind puts the feature's grind file for kind in the app's checkout.
func (a *audioContext) writeGrind(kind string, g audioGrind) error {
	a.grinds[kind] = g
	file := map[string]any{
		"grind": 1, "app": "cairn", "kind": kind, "versions": []string{"1.1"},
		"model": "sonnet", "effort": "low",
		"instructions": "grinds/sweep.md",
		"answerSchema": "contexts/inventory/schemas/sweep-result.schema.json",
		"attachments":  map[string]any{"min": 1, "max": 2, "mime": []string{"audio/webm"}, "maxBytes": 4194304},
	}
	if g.scoring {
		scoring := map[string]any{"audio": true, "target_field": "target_text"}
		if len(g.langs) > 0 {
			scoring["langs"] = g.langs
		}
		file["scoring"] = scoring
	}
	if g.maxTurns > 0 {
		file["maxTurns"] = g.maxTurns
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	a.c.grinds.SetFile(a.c.checkout("cairn"), a.c.commit, "grinds/"+kind+".json", raw)
	return nil
}

func (a *audioContext) grindScores(kind, field string) error {
	if field != "target_text" {
		return fmt.Errorf("the feature's grinds score against target_text, not %q", field)
	}
	return a.writeGrind(kind, audioGrind{scoring: true})
}

func (a *audioContext) grindScoresInLangs(kind, field, first, second string) error {
	if field != "target_text" {
		return fmt.Errorf("the feature's grinds score against target_text, not %q", field)
	}
	return a.writeGrind(kind, audioGrind{scoring: true, langs: []string{first, second}})
}

func (a *audioContext) engineHasNoLang(name, lang string) error {
	engine, ok := a.engines[name]
	if !ok {
		return fmt.Errorf("no engine %q is configured", name)
	}
	engine.NoLangs = append(engine.NoLangs, lang)
	return nil
}

func (a *audioContext) grindDoesNotScore(kind string) error {
	return a.writeGrind(kind, audioGrind{})
}

func (a *audioContext) grindTakesTurns(kind string, turns int) error {
	g := a.grinds[kind]
	g.maxTurns = turns
	return a.writeGrind(kind, g)
}

func (a *audioContext) enginesConfigured(first, second, said string) error {
	for _, name := range []string{first, second} {
		var words []application.ReadingWord
		for _, text := range strings.Fields(said) {
			words = append(words, application.ReadingWord{
				Text: text, ExpectedPhonemes: []string{"AH"}, ProducedPhonemes: []string{"AH"},
				Error: application.ErrNone, Accuracy: 90,
			})
		}
		a.engines[name] = &apptest.FakeScorer{Result: application.ReadingResult{Engine: name, Words: words, Accuracy: 90, Seconds: 1.5}}
	}
	return nil
}

// engineStopsEarly makes the engine score its reading as one that stopped
// after the first word: the others are not reached.
func (a *audioContext) engineStopsEarly(name string) error {
	engine, ok := a.engines[name]
	if !ok {
		return fmt.Errorf("no engine %q is configured", name)
	}
	words := append([]application.ReadingWord(nil), engine.Result.Words...)
	for i := 1; i < len(words); i++ {
		words[i].ProducedPhonemes = []string{}
		words[i].Error = application.ErrNotReached
		words[i].Accuracy = 0
	}
	engine.Result.Words = words
	return nil
}

// engineTimesWords gives each of the engine's words a start and end, half a
// second apart, the last word omitted and so untimed.
func (a *audioContext) engineTimesWords(name string) error {
	engine, ok := a.engines[name]
	if !ok {
		return fmt.Errorf("no engine %q is configured", name)
	}
	words := append([]application.ReadingWord(nil), engine.Result.Words...)
	for i := range words {
		if i == len(words)-1 {
			words[i].Error, words[i].ProducedPhonemes, words[i].Accuracy = application.ErrOmission, []string{}, 0
			continue
		}
		start, end := float64(i)/2, float64(i)/2+0.4
		words[i].Start, words[i].End = &start, &end
	}
	engine.Result.Words = words
	return nil
}

func (a *audioContext) engineFails(name, why string) error {
	engine, ok := a.engines[name]
	if !ok {
		return fmt.Errorf("no engine %q is configured", name)
	}
	engine.Err = fmt.Errorf("%s", why)
	return nil
}

func (a *audioContext) keepsRuns() error {
	a.store = grist.NewRuns(filepath.Join(a.c.home, "state"))
	return nil
}

func (a *audioContext) clockTicks() error {
	a.ticks = true
	return nil
}

func (a *audioContext) phoneSendsReading(app, kind, v, target string) error {
	return a.phoneSendsReadingIn(app, kind, v, target, "")
}

func (a *audioContext) phoneSendsReadingIn(app, kind, v, target, lang string) error {
	a.target = target
	a.clip = []byte("webm bytes of the child reading " + target)
	fields := map[string]string{"schemaVersion": v, "mode": "reading", "target_text": target}
	if lang != "" {
		fields["lang"] = lang
	}
	input, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return a.c.sendAttached(a.c.phoneKey, a.c.phoneApps, app, kind, v, 0, &sentClip{data: a.clip, mime: "audio/webm", input: input})
}

// engineCalls is what the engine was given.
func (a *audioContext) engineCalls(name string) ([]apptest.ScorerCall, error) {
	engine, ok := a.engines[name]
	if !ok {
		return nil, fmt.Errorf("no engine %q is configured", name)
	}
	return engine.Calls(), nil
}

func (a *audioContext) enginesWereGiven(first, second, target, lang, mime string) error {
	for _, name := range []string{first, second} {
		calls, err := a.engineCalls(name)
		if err != nil {
			return err
		}
		if len(calls) != 1 {
			return fmt.Errorf("expected the engine %s given one recording, got %d", name, len(calls))
		}
		got := calls[0]
		if !bytes.Equal(got.Audio, a.clip) || got.Target != target || got.Lang != lang || got.Mime != mime {
			return fmt.Errorf("the engine %s was given %q %q %q %q, expected the recording, %q, %q, %q", name, got.Audio, got.Target, got.Lang, got.Mime, target, lang, mime)
		}
	}
	return nil
}

func (a *audioContext) engineWasGivenLang(name, lang string) error {
	calls, err := a.engineCalls(name)
	if err != nil {
		return err
	}
	if len(calls) != 1 || calls[0].Lang != lang {
		return fmt.Errorf("expected the engine %s given one recording in %q, got %+v", name, lang, calls)
	}
	return nil
}

func (a *audioContext) engineWasGivenNone(name string) error {
	calls, err := a.engineCalls(name)
	if err != nil {
		return err
	}
	if len(calls) != 0 {
		return fmt.Errorf("expected the engine %s given no recording, got %d", name, len(calls))
	}
	return nil
}

func (a *audioContext) runScorersRecordLang(lang string) error {
	raw, err := a.runFile("scorers.json")
	if err != nil {
		return err
	}
	var scored []application.GristRunScore
	if err := json.Unmarshal(raw, &scored); err != nil {
		return err
	}
	if len(scored) != 1 || scored[0].Lang != lang {
		return fmt.Errorf("expected scorers.json to record the language %q, got %s", lang, raw)
	}
	return nil
}

func (a *audioContext) passNotes(note string) error {
	for _, got := range a.c.report.Notes {
		if strings.Contains(got, note) {
			return nil
		}
	}
	return fmt.Errorf("expected the pass to note %q, its notes are %q", note, a.c.report.Notes)
}

func (a *audioContext) noEngineWasGiven() error {
	for name, engine := range a.engines {
		if calls := engine.Calls(); len(calls) != 0 {
			return fmt.Errorf("expected no recording given to %s, got %d", name, len(calls))
		}
	}
	return nil
}

// request is the app's request as the only grind's prompt carries it, between
// the markers.
func (a *audioContext) request() (map[string]json.RawMessage, error) {
	seen, err := a.c.onlyGrind()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(seen.Call.Prompt, "\n")
	for i := 1; i+1 < len(lines); i++ {
		if strings.HasPrefix(lines[i-1], "GRIST INPUT ") && strings.HasSuffix(lines[i-1], " BEGINS") {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(lines[i]), &fields); err != nil {
				return nil, fmt.Errorf("the request between the markers is not a JSON object: %q", lines[i])
			}
			return fields, nil
		}
	}
	return nil, fmt.Errorf("no request between markers in the prompt:\n%s", seen.Call.Prompt)
}

// readings is the request's reading_result, by engine.
func (a *audioContext) readings() (map[string]json.RawMessage, error) {
	fields, err := a.request()
	if err != nil {
		return nil, err
	}
	raw, ok := fields["reading_result"]
	if !ok {
		return nil, fmt.Errorf("the request carries no reading_result: %v", fields)
	}
	var byEngine map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byEngine); err != nil {
		return nil, fmt.Errorf("reading_result is not an object by engine: %s", raw)
	}
	return byEngine, nil
}

// reading is one engine's result in the request, as a ReadingResult.
func (a *audioContext) reading(engine string) (application.ReadingResult, error) {
	byEngine, err := a.readings()
	if err != nil {
		return application.ReadingResult{}, err
	}
	raw, ok := byEngine[engine]
	if !ok {
		return application.ReadingResult{}, fmt.Errorf("reading_result has nothing from %s: %v", engine, byEngine)
	}
	var result application.ReadingResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (a *audioContext) requestCarriesBoth(first, second string, words int) error {
	for _, name := range []string{first, second} {
		if err := a.requestCarriesOne(name, words); err != nil {
			return err
		}
	}
	return nil
}

func (a *audioContext) requestCarriesOne(engine string, words int) error {
	result, err := a.reading(engine)
	if err != nil {
		return err
	}
	if result.Engine != engine || len(result.Words) != words {
		return fmt.Errorf("expected %d words from %s, got %+v", words, engine, result)
	}
	return nil
}

func (a *audioContext) requestCarriesNotReached(engine string) error {
	result, err := a.reading(engine)
	if err != nil {
		return err
	}
	if len(result.Words) < 2 || result.Words[0].Error != application.ErrNone {
		return fmt.Errorf("expected a read first word and more after it from %s, got %+v", engine, result)
	}
	for _, w := range result.Words[1:] {
		if w.Error != application.ErrNotReached {
			return fmt.Errorf("expected %q not_reached from %s, got %+v", w.Text, engine, w)
		}
	}
	return nil
}

func (a *audioContext) requestCarriesTimes(engine string) error {
	result, err := a.reading(engine)
	if err != nil {
		return err
	}
	for i, w := range result.Words {
		last := i == len(result.Words)-1
		if last {
			if w.Start != nil || w.End != nil {
				return fmt.Errorf("expected the omitted %q to have no times, got %+v", w.Text, w)
			}
			continue
		}
		if w.Start == nil || w.End == nil || *w.Start != float64(i)/2 || *w.End != float64(i)/2+0.4 {
			return fmt.Errorf("expected %q to keep %v to %v, got %+v", w.Text, float64(i)/2, float64(i)/2+0.4, w)
		}
	}
	return nil
}

func (a *audioContext) requestCarriesError(why, engine string) error {
	byEngine, err := a.readings()
	if err != nil {
		return err
	}
	var failed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(byEngine[engine], &failed); err != nil || !strings.Contains(failed.Error, why) {
		return fmt.Errorf("expected the error %q for %s, got %s", why, engine, byEngine[engine])
	}
	return nil
}

func (a *audioContext) requestKeepsFields() error {
	fields, err := a.request()
	if err != nil {
		return err
	}
	if string(fields["mode"]) != `"reading"` || string(fields["target_text"]) != `"`+a.target+`"` || string(fields["schemaVersion"]) != `"1.1"` {
		return fmt.Errorf("expected the app's own fields kept, got %v", fields)
	}
	return nil
}

func (a *audioContext) requestHasNoReading() error {
	fields, err := a.request()
	if err != nil {
		return err
	}
	if _, ok := fields["reading_result"]; ok {
		return fmt.Errorf("expected no reading_result, got %v", fields)
	}
	return nil
}

func (a *audioContext) directoryHeldNoAudio() error {
	seen, err := a.c.onlyGrind()
	if err != nil {
		return err
	}
	if len(seen.Files) != 0 {
		names := make([]string, 0, len(seen.Files))
		for name := range seen.Files {
			names = append(names, name)
		}
		return fmt.Errorf("expected nothing in the session's directory, it held %v", names)
	}
	return nil
}

func (a *audioContext) promptsHoldNoAudio() error {
	seen, err := a.c.onlyGrind()
	if err != nil {
		return err
	}
	for _, text := range []string{seen.Call.Prompt, seen.Call.System} {
		if strings.Contains(text, string(a.clip)) || strings.Contains(text, base64.StdEncoding.EncodeToString(a.clip)) {
			return fmt.Errorf("the audio is in what the session was told:\n%s", text)
		}
	}
	return nil
}

// runDir is where the grist's run is kept.
func (a *audioContext) runDir() string {
	return filepath.Join(a.c.home, "state", "runs", a.c.grist.Txid)
}

var runFiles = regexp.MustCompile(`[^,\s]+\.[a-z]+`)

func (a *audioContext) runHolds(list string) error {
	want := runFiles.FindAllString(list, -1)
	sort.Strings(want)
	entries, err := os.ReadDir(a.runDir())
	if err != nil {
		return fmt.Errorf("the run is not kept: %w", err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		return fmt.Errorf("expected the run's directory to hold %v, it holds %v", want, got)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.Mode().Perm() != 0o600 {
			return fmt.Errorf("expected %s 0600, got %v %v", e.Name(), info, err)
		}
	}
	return nil
}

func (a *audioContext) runFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(a.runDir(), name))
}

func (a *audioContext) runInputCarries() error {
	raw, err := a.runFile("input.json")
	if err != nil {
		return err
	}
	var input struct {
		ReadingResult map[string]application.ReadingResult `json:"reading_result"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return err
	}
	if len(input.ReadingResult["local"].Words) == 0 {
		return fmt.Errorf("expected reading_result.local in input.json, got %s", raw)
	}
	if bytes.Contains(raw, a.clip) || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(a.clip)) {
		return fmt.Errorf("input.json holds the audio: %s", raw)
	}
	return nil
}

func (a *audioContext) runKeptTheRecording() error {
	raw, err := a.runFile("attachment-1.webm")
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, a.clip) {
		return fmt.Errorf("expected the recording as sent, got %q", raw)
	}
	return nil
}

func (a *audioContext) runScorersHold(first, second, target string) error {
	raw, err := a.runFile("scorers.json")
	if err != nil {
		return err
	}
	var scored []application.GristRunScore
	if err := json.Unmarshal(raw, &scored); err != nil {
		return err
	}
	if len(scored) != 1 || scored[0].Attachment != 1 || scored[0].Target != target || scored[0].Mime != "audio/webm" {
		return fmt.Errorf("expected one recording scored against %q, got %s", target, raw)
	}
	for _, name := range []string{first, second} {
		if _, ok := scored[0].Results[name]; !ok {
			return fmt.Errorf("expected the results of %s in scorers.json, got %s", name, raw)
		}
	}
	return nil
}

func (a *audioContext) runScorersHoldNone() error {
	raw, err := a.runFile("scorers.json")
	if err != nil {
		return err
	}
	var scored []application.GristRunScore
	if err := json.Unmarshal(raw, &scored); err != nil || scored == nil || len(scored) != 0 {
		return fmt.Errorf("expected scorers.json to be an empty list, got %s", raw)
	}
	return nil
}

func (a *audioContext) runAnswerSays(status string) error {
	raw, err := a.runFile("answer.json")
	if err != nil {
		return err
	}
	var answer application.GristRunAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return err
	}
	if answer.Status != status {
		return fmt.Errorf("expected answer.json to say %s, got %s", status, raw)
	}
	return nil
}

func (a *audioContext) runAnswerSaysWithAnswer(status string) error {
	if err := a.runAnswerSays(status); err != nil {
		return err
	}
	raw, _ := a.runFile("answer.json")
	var answer application.GristRunAnswer
	_ = json.Unmarshal(raw, &answer)
	var got, want any
	if err := json.Unmarshal(answer.Answer, &got); err != nil {
		return fmt.Errorf("answer.json holds no answer: %s", raw)
	}
	_ = json.Unmarshal(a.c.vectors.Answers["answered"].Answer, &want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		return fmt.Errorf("expected the grind's answer %s, got %s", a.c.vectors.Answers["answered"].Answer, answer.Answer)
	}
	return nil
}

func (a *audioContext) runTimingHas() error {
	raw, err := a.runFile("timing.json")
	if err != nil {
		return err
	}
	var timing application.GristRunTiming
	if err := json.Unmarshal(raw, &timing); err != nil {
		return err
	}
	if timing.Received.IsZero() || timing.ScoredAt == nil || timing.HarnessStarted.IsZero() || timing.Answered.IsZero() {
		return fmt.Errorf("expected received, scored_at, harness_started and answered, got %s", raw)
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	for _, key := range []string{"scoring_seconds", "harness_seconds", "seconds"} {
		if _, ok := keys[key]; !ok {
			return fmt.Errorf("expected %s in timing.json, got %s", key, raw)
		}
	}
	if timing.Txid != a.c.grist.Txid || timing.Kind == "" || timing.Model == "" {
		return fmt.Errorf("expected the txid, kind and model in timing.json, got %s", raw)
	}
	return nil
}

func (a *audioContext) runTimingHasSentAndScorers(first, second string) error {
	raw, err := a.runFile("timing.json")
	if err != nil {
		return err
	}
	var timing application.GristRunTiming
	if err := json.Unmarshal(raw, &timing); err != nil {
		return err
	}
	if timing.Sent == nil || !timing.Sent.Equal(gristNow) || !timing.Received.After(*timing.Sent) {
		return fmt.Errorf("expected sent to be when the grist was sent (%s) and before it was received, got %s", gristNow.Format(time.RFC3339), raw)
	}
	for _, engine := range []string{first, second} {
		if timing.Scorers[engine] <= 0 {
			return fmt.Errorf("expected seconds for the scorer %s in timing.json, got %s", engine, raw)
		}
	}
	return nil
}

func (a *audioContext) list(since time.Time) error {
	a.listing.Reset()
	var err error
	a.listed, err = application.GristRuns{Runs: a.store, Out: &a.listing}.Run(context.Background(), since)
	return err
}

func (a *audioContext) listRuns() error { return a.list(time.Time{}) }

func (a *audioContext) listRunsSinceTomorrow() error { return a.list(gristNow.Add(24 * time.Hour)) }

func (a *audioContext) listHasOne(kind, model, status string) error {
	if len(a.listed) != 1 {
		return fmt.Errorf("expected one line, got %d:\n%s", len(a.listed), a.listing.String())
	}
	line := a.listed[0]
	if line.Txid != a.c.grist.Txid || line.Kind != kind || line.Model != model || line.Status != status || line.Seconds <= 0 {
		return fmt.Errorf("expected %s %s %s with seconds, got %+v", kind, model, status, line)
	}
	for _, want := range []string{kind, model, status, "s  "} {
		if !strings.Contains(a.listing.String(), want) {
			return fmt.Errorf("expected the list to say %q, it said:\n%s", want, a.listing.String())
		}
	}
	return nil
}

func (a *audioContext) listHasNone() error {
	if len(a.listed) != 0 {
		return fmt.Errorf("expected no lines, got %+v", a.listed)
	}
	return nil
}

func (a *audioContext) calledWithTurns(turns int) error {
	seen, err := a.c.onlyGrind()
	if err != nil {
		return err
	}
	if seen.Call.MaxTurns != turns {
		return fmt.Errorf("expected the session called with %d turns, got %d", turns, seen.Call.MaxTurns)
	}
	return nil
}

// reply is the plaintext of the one answer the mill delivered, as raw fields.
func (a *audioContext) reply() (map[string]json.RawMessage, string, error) {
	raws := a.c.backend.Delivered()
	if len(raws) != 1 {
		return nil, "", fmt.Errorf("expected one answer delivered, got %d", len(raws))
	}
	var p application.PosternPayload
	if err := json.Unmarshal(raws[0], &p); err != nil {
		return nil, "", err
	}
	plain, err := base64.StdEncoding.DecodeString(p.Ct)
	if err != nil {
		return nil, "", err
	}
	parts := strings.SplitN(string(plain), "\x00", 3)
	if len(parts) != 3 {
		return nil, "", fmt.Errorf("the answer's ct is not the fake cipher's")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(parts[2]), &fields); err != nil {
		return nil, "", fmt.Errorf("the reply is not a JSON object: %w", err)
	}
	return fields, parts[2], nil
}

// replyReadings is the reply's reading_result, by engine.
func (a *audioContext) replyReadings() (map[string]json.RawMessage, error) {
	fields, _, err := a.reply()
	if err != nil {
		return nil, err
	}
	if len(fields["answer"]) == 0 {
		return nil, fmt.Errorf("the reply has no answer: %v", fields)
	}
	raw, ok := fields["reading_result"]
	if !ok {
		return nil, fmt.Errorf("the reply carries no reading_result: %v", fields)
	}
	var byEngine map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byEngine); err != nil {
		return nil, fmt.Errorf("reading_result is not an object by engine: %s", raw)
	}
	return byEngine, nil
}

func (a *audioContext) replyCarriesOne(engine string, words int) error {
	byEngine, err := a.replyReadings()
	if err != nil {
		return err
	}
	var result application.ReadingResult
	if err := json.Unmarshal(byEngine[engine], &result); err != nil || result.Engine != engine || len(result.Words) != words {
		return fmt.Errorf("expected %d words from %s in the reply, got %s", words, engine, byEngine[engine])
	}
	return nil
}

func (a *audioContext) replyCarriesError(why, engine string) error {
	byEngine, err := a.replyReadings()
	if err != nil {
		return err
	}
	var failed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(byEngine[engine], &failed); err != nil || !strings.Contains(failed.Error, why) {
		return fmt.Errorf("expected the error %q for %s in the reply, got %s", why, engine, byEngine[engine])
	}
	return nil
}

func (a *audioContext) replyHasNoReadings() error {
	fields, _, err := a.reply()
	if err != nil {
		return err
	}
	if _, ok := fields["reading_results"]; ok {
		return fmt.Errorf("expected no reading_results for one recording, got %v", fields)
	}
	return nil
}

func (a *audioContext) replyHoldsNoAudio() error {
	_, text, err := a.reply()
	if err != nil {
		return err
	}
	if strings.Contains(text, string(a.clip)) || strings.Contains(text, base64.StdEncoding.EncodeToString(a.clip)) {
		return fmt.Errorf("the audio is in the reply:\n%s", text)
	}
	return nil
}

func (a *audioContext) replyHasNoReading() error {
	fields, _, err := a.reply()
	if err != nil {
		return err
	}
	if len(fields["answer"]) == 0 {
		return fmt.Errorf("the reply has no answer: %v", fields)
	}
	for _, key := range []string{"reading_result", "reading_results"} {
		if _, ok := fields[key]; ok {
			return fmt.Errorf("expected no %s in the reply, got %v", key, fields)
		}
	}
	return nil
}

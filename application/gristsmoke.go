package application

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// GristSmokeDir is where an app's rig keeps its grinds, and GristExamplesDir
// where it keeps one directory of examples for each grind's kind.
const (
	GristSmokeDir    = "grinds"
	GristExamplesDir = GristSmokeDir + "/examples"
)

// GristSmokeWait is how long one example waits for its answer when it is not
// told another.
const GristSmokeWait = 5 * time.Minute

// GristSmokeBackendWait is how long the smoke waits before it sends once more
// an example the backend refused with its own trouble (gristBackendsFault).
const GristSmokeBackendWait = 20 * time.Second

// GristSmokeSender sends one grist as the smoke's test key and waits for its
// answer. GristSend is the real one.
type GristSmokeSender interface {
	Run(ctx context.Context, req GristSendRequest) (GristSendReport, error)
}

// GristSmoke is `mw grist smoke <app>`: every example of an app's grinds sent
// through the live backend as the factory's test key, and each answer held to
// the grind's answer schema and to the example's expect. It reads the grinds
// where the mill does, from the app's rig at its main (GrindSource), so what it
// tests is what the mill will run. It sends only what the rig's examples hold,
// to the one app, and never touches the mill's state.
type GristSmoke struct {
	Grinds GrindSource
	Send   GristSmokeSender
	// Apps is where each app's rig is checked out on this host
	// (config [grist-apps]).
	Apps map[string]string
	// Wait is how long each example waits for its answer; zero is
	// GristSmokeWait.
	Wait time.Duration
	// TempDir is where an example's request and photos are put to be sent;
	// empty is the system's.
	TempDir string
	// Sleep is the wait before an example the backend refused is sent once
	// more; nil waits for real.
	Sleep func(ctx context.Context, d time.Duration) error
	// Record is the mill's record of its runs (its grinds.jsonl), when this host
	// holds it: a grind that did not finish is said with the notes the mill kept
	// of why. Nil says no more than the answer's reason.
	Record GristRecord
	// Talks ends the talks an example of two turns starts, and the ones a run
	// killed midway left; nil keeps none of that (a mill that keeps no talks
	// here, a test).
	Talks GristTalkEnder
	// TalkPrefix begins the names of the talks it sends: GristSmokeTalkPrefix
	// when empty, GristEvalTalkPrefix for `mw grist eval`.
	TalkPrefix string
	// Override, when set, is the text of a grind at a ref that every send
	// carries for the mill to grind with, in a talk of its own even for an
	// example of one turn (`mw grist eval --ref`); nil is the grind at main.
	Override *GristGrindOverride
	// Out is where progress is printed; nil prints nothing.
	Out io.Writer
}

// GristRecord is the mill's record of its runs, read: GristState has it.
type GristRecord interface {
	Lines(ctx context.Context) ([]GrindLine, error)
}

// said is reason with, when the mill's record of the run txid holds notes, the
// notes: the reason a sender is told names no cause (mw-gq6.690).
func (s GristSmoke) said(ctx context.Context, txid, reason string) string {
	if s.Record == nil || txid == "" || reason != GristReasonSessionEnd {
		return reason
	}
	lines, err := s.Record.Lines(ctx)
	if err != nil {
		return reason
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Txid == txid {
			if len(lines[i].Notes) > 0 {
				return reason + " (" + firstLine(strings.Join(lines[i].Notes, "; ")) + ")"
			}
			break
		}
	}
	return reason
}

// GristTalkEnder is what the smoke and the eval end their talks with: End
// deletes the talk of app named talk, Tidy the old ones of the eval and the
// smoke an earlier run left. Both say how many they deleted. GristTalkSweep is
// the real one.
type GristTalkEnder interface {
	End(ctx context.Context, app, talk string) (int, error)
	Tidy(ctx context.Context, app string) (int, error)
}

// GristSmokeReport is what one smoke of one app found.
// It is also the JSON `mw grist smoke <app> --json` prints.
type GristSmokeReport struct {
	App string `json:"app"`
	// Commit is the commit of the app's main the grinds were read at.
	Commit string `json:"commit,omitempty"`
	// Examples is how many examples were sent.
	Examples int `json:"examples"`
	// Failures are the examples that failed, one line each, and Warnings what
	// is worth saying without failing: a kind with no example.
	Failures []string `json:"failures,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	// NoGrinds says the app's rig has no grinds, so there was nothing to test.
	NoGrinds bool `json:"no_grinds,omitempty"`
	// NotRun are the reasons the mill could not be asked, when that is all that
	// went wrong: it refused for the day's limit or the licence, failed for a
	// limit of the factory's, its usage limit or its model's
	// (GristReasonLimited), or could not be reached. None of that is the
	// examples' fault, so it is no Failure and holds nothing. Any Failure beside
	// it takes these into itself.
	NotRun []string `json:"not_run,omitempty"`
	// FiledOn are the open beads that name every failing example (SmokeExamplesField),
	// set by GristSmokeBook.Mark; empty when any failure is not named, so the
	// failure holds.
	FiledOn []string `json:"filed_on,omitempty"`
	// Flakes are the examples that missed and then passed when sent again, so
	// that they hold nothing; each is also a Warning.
	Flakes []GristSmokeFlake `json:"flakes,omitempty"`
}

// GristSmokeResends is how many more times a smoke sends an example whose
// answer was wrong before the miss holds: a model's answer is a sample, and a
// slip seen in 1 of 100 runs is a fact about the grind's instructions, worth a
// bug, and not a hold on the landing rig's queue (mw-gq6.664).
const GristSmokeResends = 2

// GristSmokeFlake is an example whose answer was wrong and then right when it
// was sent again, so that it holds nothing but is still the Mayor's to file.
type GristSmokeFlake struct {
	// Example is <app>/<kind>/<example>.
	Example string `json:"example"`
	// Missed of Sent is how many of the sends that came back with an answer were
	// wrong.
	Missed int `json:"missed"`
	Sent   int `json:"sent"`
	// Misses are what the first wrong answer lacked: the field, what was wanted
	// and what came. An answer that did not fit the schema is one miss of the
	// field "(answer)".
	Misses []ExpectFailure `json:"misses"`
	// Txid is the grist of the first wrong answer, the failing run.
	Txid string `json:"txid,omitempty"`
}

// String is the flake as a report and mw status say it.
func (f GristSmokeFlake) String() string {
	return fmt.Sprintf("flaky: %s missed %d of %d", f.Example, f.Missed, f.Sent)
}

// Failed says an example failed.
func (r GristSmokeReport) Failed() bool { return len(r.Failures) > 0 }

// Held says the failure holds the rig's open stories: it failed, and not every
// failing example is named on an open bead.
func (r GristSmokeReport) Held() bool { return r.Failed() && len(r.FiledOn) == 0 }

// Line is the report in one line, the shape every place that says it uses.
func (r GristSmokeReport) Line() string {
	switch {
	case r.NoGrinds:
		return fmt.Sprintf("grist smoke: %s: the app has no grinds", r.App)
	case r.Failed():
		filed := ""
		if len(r.FiledOn) > 0 {
			filed = ", all filed on " + strings.Join(r.FiledOn, ", ") + ": not held"
		}
		return fmt.Sprintf("grist smoke: %s: FAILED %d of %d examples%s: %s%s", r.App, len(r.Failures), r.Examples, filed, strings.Join(r.Failures, "; "), r.flakyNote())
	case len(r.NotRun) > 0:
		return fmt.Sprintf("grist smoke: %s: not run: %s%s", r.App, strings.Join(r.NotRun, "; "), r.flakyNote())
	}
	said := fmt.Sprintf("grist smoke: %s: ok, %d examples answered as expected", r.App, r.Examples)
	if len(r.Warnings) > 0 {
		said += " (" + strings.Join(r.Warnings, "; ") + ")"
	}
	return said
}

// flakyNote is the flakes as a line that has no Warnings in it ends, empty when
// there are none.
func (r GristSmokeReport) flakyNote() string {
	var said []string
	for _, f := range r.Flakes {
		said = append(said, f.String())
	}
	if len(said) == 0 {
		return ""
	}
	return " (" + strings.Join(said, "; ") + ")"
}

// ErrGristSmokeFailed is the error mw grist smoke leaves with when an example
// failed; the report says which.
var ErrGristSmokeFailed = errors.New("a grist smoke example failed")

// Run smokes app, or only its grind of kind when kind is not empty. An error is
// a smoke that could not be made at all (no such app, the rig unreadable); an
// example that failed is in the report.
func (s GristSmoke) Run(ctx context.Context, app, kind string) (GristSmokeReport, error) {
	report := GristSmokeReport{App: app}
	checkout, commit, err := s.open(ctx, "mw grist smoke", app)
	if err != nil {
		return report, err
	}
	report.Commit = commit
	s.tidy(ctx, app)
	files, err := s.Grinds.List(ctx, checkout, commit, GristSmokeDir)
	if err != nil {
		return report, fmt.Errorf("mw grist smoke: %w", err)
	}
	kinds := smokeKinds(files)
	if kind != "" {
		if !slices.Contains(kinds, kind) {
			return report, fmt.Errorf("mw grist smoke: %s has no grind of kind %q at %s (it has: %s)", app, kind, shortTxid(commit), strings.Join(kinds, ", "))
		}
		kinds = []string{kind}
	}
	if len(kinds) == 0 {
		report.NoGrinds = true
		return report, nil
	}
	for _, k := range kinds {
		s.kind(ctx, checkout, commit, app, k, files, &report)
	}
	if report.Failed() {
		// The smoke failed whatever the mill refused, and says all it found.
		report.Failures = append(report.Failures, report.NotRun...)
		report.NotRun = nil
	}
	return report, nil
}

// open finds app's rig, brings it level with its remote when it can, and says
// the commit of its main the grinds are read at; who is the command that asks,
// for its errors.
func (s GristSmoke) open(ctx context.Context, who, app string) (checkout, commit string, err error) {
	checkout, ok := s.Apps[app]
	if !ok || checkout == "" {
		return "", "", fmt.Errorf("%s: the app %q has no rig in the [grist-apps] table of the config file (it has: %s)", who, app, strings.Join(s.appNames(), ", "))
	}
	if s.Grinds == nil || s.Send == nil {
		return "", "", fmt.Errorf("%s: no grind source or sender is configured", who)
	}
	if err := s.Grinds.Refresh(ctx, checkout); err != nil {
		s.say("note: %s's rig could not be refreshed, so its own main is read: %v", app, firstLine(err.Error()))
	}
	if commit, err = s.Grinds.Commit(ctx, checkout); err != nil {
		return "", "", fmt.Errorf("%s: %w", who, err)
	}
	return checkout, commit, nil
}

// smokeKinds are the kinds the grinds listed hold: a grinds/<kind>.json.
func smokeKinds(files []string) []string {
	var kinds []string
	for _, f := range files {
		if path.Dir(f) != GristSmokeDir || path.Ext(f) != ".json" {
			continue
		}
		if k := strings.TrimSuffix(path.Base(f), ".json"); gristKind.MatchString(k) {
			kinds = append(kinds, k)
		}
	}
	sort.Strings(kinds)
	return kinds
}

func (s GristSmoke) appNames() []string {
	names := make([]string, 0, len(s.Apps))
	for name := range s.Apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// kind smokes one grind: its examples, or a warning when it has none.
func (s GristSmoke) kind(ctx context.Context, checkout, commit, app, kind string, files []string, report *GristSmokeReport) {
	label := app + "/" + kind
	grind, ok := s.readGrind(ctx, checkout, commit, kind)
	if !ok {
		report.Failures = append(report.Failures, label+": the grind file could not be read")
		return
	}
	if n := s.instructionsSize(ctx, checkout, commit, grind); n > GristInstructionsWarnBytes {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: its instructions are %d bytes, over %d (a big prompt costs on every grind)", label, n, GristInstructionsWarnBytes))
	}
	if grind.Forward != "" {
		report.Warnings = append(report.Warnings, label+": forwards to the "+grind.Forward+", so it is not smoked")
		return
	}
	var examples []string
	for _, f := range files {
		if path.Dir(f) == path.Join(GristExamplesDir, kind) && path.Ext(f) == ".json" {
			examples = append(examples, f)
		}
	}
	if len(examples) == 0 {
		report.Warnings = append(report.Warnings, label+": no example (grinds/examples/"+kind+"/<name>.json)")
		return
	}
	schema := s.readSchema(ctx, checkout, commit, grind)
	if schema == "" {
		report.Failures = append(report.Failures, label+": the answer schema "+grind.AnswerSchema+" could not be read")
		return
	}
	for _, example := range examples {
		name := strings.TrimSuffix(path.Base(example), ".json")
		report.Examples++
		s.say("sending %s/%s ...", label, name)
		why, notRun, flake := s.example(ctx, checkout, commit, app, kind, example, schema)
		if flake != nil {
			report.Flakes = append(report.Flakes, *flake)
			report.Warnings = append(report.Warnings, flake.String())
			s.say("  %s", flake)
			continue
		}
		if notRun != "" {
			if !slices.Contains(report.NotRun, notRun) {
				report.NotRun = append(report.NotRun, notRun)
			}
			s.say("  NOT RUN: %s", notRun)
			continue
		}
		if why != "" {
			report.Failures = append(report.Failures, label+"/"+name+": "+why)
			s.say("  FAILED: %s", why)
			continue
		}
		s.say("  ok")
	}
}

// smokePrepared is an example read, and the grists to send for it, their
// requests and photos written under dir.
type smokePrepared struct {
	example GristExample
	turns   []GristSendRequest
	wait    time.Duration
	dir     string
}

// prepare reads the example in file and writes what it sends into a directory
// of its own, which the caller removes. why is empty when it did.
func (s GristSmoke) prepare(ctx context.Context, checkout, commit, app, kind, file string) (prepared smokePrepared, why string) {
	data, _, err := s.Grinds.ReadAt(ctx, checkout, commit, file)
	if err != nil {
		return smokePrepared{}, "the example could not be read: " + firstLine(err.Error())
	}
	example, err := ParseGristExample(data)
	if err != nil {
		return smokePrepared{}, "the example is wrong: " + err.Error()
	}
	dir, err := os.MkdirTemp(s.TempDir, "mw-grist-smoke-")
	if err != nil {
		return smokePrepared{}, "no directory to send it from: " + err.Error()
	}
	prepared.dir = dir
	defer func() {
		if why != "" {
			os.RemoveAll(dir)
		}
	}()
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, example.Request, 0o600); err != nil {
		return smokePrepared{}, "the request could not be written: " + err.Error()
	}
	var photos []string
	for _, name := range example.Photos {
		if name == "" || path.Base(name) != name {
			return smokePrepared{}, fmt.Sprintf("the photo %q is not a file beside the example", name)
		}
		photo, found, err := s.Grinds.ReadAt(ctx, checkout, commit, path.Join(path.Dir(file), name))
		if err != nil || !found {
			return smokePrepared{}, fmt.Sprintf("the photo %s is not beside the example", name)
		}
		local := filepath.Join(dir, name)
		if err := os.WriteFile(local, photo, 0o600); err != nil {
			return smokePrepared{}, "a photo could not be written: " + err.Error()
		}
		photos = append(photos, local)
	}
	wait := s.Wait
	if wait <= 0 {
		wait = GristSmokeWait
	}
	turns := []GristSendRequest{{App: app, Kind: kind, Version: example.SchemaVersion, RequestFile: request, Photos: photos, Wait: wait, GrindOverride: s.Override}}
	if turn := example.Turn2; turn != nil {
		second := filepath.Join(dir, "request-2.json")
		// The session holds turn 1, so turn 2 goes without a history, whatever the
		// example carries (the app's input schema may need one there).
		if err := os.WriteFile(second, withoutHistory(turn.Request), 0o600); err != nil {
			return smokePrepared{}, "the request of turn 2 could not be written: " + err.Error()
		}
		var photos2 []string
		for _, name := range turn.Photos {
			photo, found, err := s.Grinds.ReadAt(ctx, checkout, commit, path.Join(path.Dir(file), name))
			if err != nil || !found {
				return smokePrepared{}, fmt.Sprintf("the photo %s of turn 2 is not beside the example", name)
			}
			local := filepath.Join(dir, "turn-2-"+name)
			if err := os.WriteFile(local, photo, 0o600); err != nil {
				return smokePrepared{}, "a photo of turn 2 could not be written: " + err.Error()
			}
			photos2 = append(photos2, local)
		}
		turns = append(turns, GristSendRequest{App: app, Kind: kind, Version: example.Version(), RequestFile: second, Photos: photos2, Wait: wait, GrindOverride: s.Override})
	}
	return smokePrepared{example: example, turns: turns, wait: wait, dir: dir}, ""
}

// readGrind reads grinds/<kind>.json at commit; ok is false when it cannot be.
func (s GristSmoke) readGrind(ctx context.Context, checkout, commit, kind string) (grind GrindFile, ok bool) {
	raw, found, err := s.Grinds.ReadAt(ctx, checkout, commit, path.Join(GristSmokeDir, kind+".json"))
	if err != nil || !found || json.Unmarshal(raw, &grind) != nil {
		return GrindFile{}, false
	}
	return grind, true
}

// GristInstructionsWarnBytes is the size of a grind's instructions (the parts
// of a directory joined) over which the smoke warns: they go to the model on
// every grind, and past 131,072 bytes claude could not be started with them as
// one argument (mw-gq6.690).
const GristInstructionsWarnBytes = 100_000

// instructionsSize is the size in bytes of the grind's instructions at commit,
// the file or the parts of its directory joined; 0 when they cannot be read.
func (s GristSmoke) instructionsSize(ctx context.Context, checkout, commit string, grind GrindFile) int {
	if !rigPath(grind.Instructions) {
		return 0
	}
	text, found, err := readGrindInstructions(ctx, s.Grinds, checkout, commit, grind.Instructions)
	if err != nil || !found {
		return 0
	}
	return len(text)
}

// readSchema reads the grind's answer schema at commit; empty when it cannot be.
func (s GristSmoke) readSchema(ctx context.Context, checkout, commit string, grind GrindFile) string {
	if !rigPath(grind.AnswerSchema) {
		return ""
	}
	if text, ok, err := s.Grinds.ReadAt(ctx, checkout, commit, grind.AnswerSchema); err == nil && ok {
		return string(text)
	}
	return ""
}

// example sends one example and reports why it failed, empty when the answer
// came, fits the schema and shows everything expected. notRun, when it is not
// empty, says instead that the mill could not be asked, for a reason that is
// not the example's (see GristSmokeReport.NotRun). An example whose mill
// session did not finish is sent once more before it fails. An example whose
// answer was wrong is sent GristSmokeResends more times: when one of them is
// right the example does not fail, and flake says so instead.
func (s GristSmoke) example(ctx context.Context, checkout, commit, app, kind, file, schema string) (why, notRun string, flake *GristSmokeFlake) {
	prepared, why := s.prepare(ctx, checkout, commit, app, kind, file)
	if why != "" {
		return why, "", nil
	}
	defer os.RemoveAll(prepared.dir)
	example, turns, wait := prepared.example, prepared.turns, prepared.wait
	name := strings.TrimSuffix(path.Base(file), ".json")
	first := s.attempt(ctx, kind+"/"+name, turns, wait, schema, example.Expect)
	if !first.slip {
		return first.why, first.notRun, nil
	}
	// The answer was a model's and wrong: send the example again before it
	// holds the landing rig's queue. A send again that cannot be told apart
	// (the mill failed it, the backend refused it) ends the sending, and the
	// first miss stands unless an answer was right by then.
	sent, missed := 1, 1
	for i := 0; i < GristSmokeResends; i++ {
		s.say("  the answer was wrong; sending it again (%d of %d) ...", i+1, GristSmokeResends)
		again := s.attempt(ctx, kind+"/"+name, turns, wait, schema, example.Expect)
		if again.slip {
			sent, missed = sent+1, missed+1
		} else if again.why == "" && again.notRun == "" {
			sent++
		} else {
			break
		}
	}
	if missed == sent {
		return first.why, "", nil
	}
	return "", "", &GristSmokeFlake{
		Example: app + "/" + kind + "/" + strings.TrimSuffix(path.Base(file), ".json"),
		Missed:  missed, Sent: sent, Misses: first.misses, Txid: first.txid,
	}
}

// attempt sends the example once whole: its one grist, or its turns one after
// the other under a talk of its own, that is ended whatever came of them. A
// turn that did not come right ends the attempt, its failure said to be that
// turn's; only the last turn is held to the example's expect.
func (s GristSmoke) attempt(ctx context.Context, label string, turns []GristSendRequest, wait time.Duration, schema string, expect map[string]json.RawMessage) smokeTry {
	if len(turns) == 1 && s.Override == nil {
		return s.try(ctx, turns[0], wait, schema, expect)
	}
	talk := s.talkName(label)
	defer s.endTalk(ctx, turns[0].App, talk)
	var got smokeTry
	for i, turn := range turns {
		turn.Talk = talk
		want := map[string]json.RawMessage(nil)
		if i == len(turns)-1 {
			want = expect
		}
		got = s.try(ctx, turn, wait, schema, want)
		if got.why != "" || got.notRun != "" {
			if got.why != "" {
				got.why = fmt.Sprintf("turn %d: %s", i+1, got.why)
			}
			for j := range got.misses {
				got.misses[j].Field = fmt.Sprintf("turn %d %s", i+1, got.misses[j].Field)
			}
			return got
		}
	}
	return got
}

// talkName is a talk for one send of the example label (<kind>/<example>):
// <prefix>:<label>:<nonce>, the label cut short to fit GristTalkMax. The nonce
// is the send's own, so that the sends of one example never share a session.
func (s GristSmoke) talkName(label string) string {
	prefix := cmp.Or(s.TalkPrefix, GristSmokeTalkPrefix)
	var nonce [4]byte
	_, _ = rand.Read(nonce[:])
	tail := ":" + hex.EncodeToString(nonce[:])
	room := GristTalkMax - len(prefix) - 1 - len(tail)
	if len(label) > room {
		label = label[:room]
	}
	return prefix + ":" + label + tail
}

// endTalk deletes a talk the run started, now that it is answered or has
// failed; what stops it is said, never an error: the next run's tidy ends it.
func (s GristSmoke) endTalk(ctx context.Context, app, talk string) {
	if s.Talks == nil {
		return
	}
	// A run that was cancelled still ends the talk it started.
	if _, err := s.Talks.End(context.WithoutCancel(ctx), app, talk); err != nil {
		s.say("  note: the talk %s could not be ended: %s", talk, firstLine(err.Error()))
	}
}

// tidy deletes the eval and smoke talks of app that an earlier run, killed
// midway, left behind.
func (s GristSmoke) tidy(ctx context.Context, app string) {
	if s.Talks == nil {
		return
	}
	if n, err := s.Talks.Tidy(context.WithoutCancel(ctx), app); err != nil {
		s.say("note: the old talks of %s could not be tidied: %s", app, firstLine(err.Error()))
	} else if n > 0 {
		s.say("note: ended %d talk%s an earlier run left", n, map[bool]string{true: "", false: "s"}[n == 1])
	}
}

// smokeTry is one send of an example and what came of it.
type smokeTry struct {
	why, notRun string
	// answer is the answer of the last grist sent, when it came and fit.
	answer json.RawMessage
	// talk is what that answer said of its talk's session, when it said.
	talk *GristAnswerTalk
	// slip says the model's answer was wrong (it does not fit the schema, misses
	// the example's expect, or the mill failed it for that): another send may
	// well pass. A refusal, a silence, or a failure of the mill's own is not a
	// slip.
	slip   bool
	misses []ExpectFailure
	txid   string
}

// try sends the example once (twice when the mill or the backend was in
// trouble) and says what came of it.
func (s GristSmoke) try(ctx context.Context, send GristSendRequest, wait time.Duration, schema string, expect map[string]json.RawMessage) smokeTry {
	sent, err := s.Send.Run(ctx, send)
	retried := false
	if gristMillsFault(err) {
		// The mill could not grind it, which says nothing of the example (a
		// session that did not finish, a grind out of time on a loaded host):
		// it is sent once more before it counts (mw-gq6.454, mw-gq6.490).
		s.say("  the mill could not grind it; sending it once more ...")
		retried = true
		sent, err = s.Send.Run(ctx, send)
	} else if _, ok := gristBackendsFault(err); ok {
		// The backend (or the chain service behind it) was in trouble when the
		// send reached it, which says nothing of the example: sent once more
		// after a wait, and not run if it is refused again (mw-gq6.540).
		s.say("  the postern backend refused it; sending it once more ...")
		if s.sleep(ctx, GristSmokeBackendWait) == nil {
			sent, err = s.Send.Run(ctx, send)
		}
	}
	if unanswered, ok := GristUnansweredIn(err); ok {
		if unanswered.Answer == nil {
			if retried {
				return smokeTry{why: fmt.Sprintf("no answer in %s when it was retried", wait)}
			}
			return smokeTry{why: fmt.Sprintf("no answer in %s", wait)}
		}
		status, reason := unanswered.Answer.Status, strings.TrimSpace(unanswered.Answer.Reason)
		if gristAnswerIsNotTheExamples(status, reason) {
			return smokeTry{notRun: reason}
		}
		reason = s.said(ctx, unanswered.Txid, reason)
		switch {
		case retried && status == GristFailed:
			return smokeTry{why: "the mill failed it, and again when it was retried: " + reason}
		case retried:
			return smokeTry{why: fmt.Sprintf("the mill failed it, then %s it when it was retried: %s", status, reason)}
		}
		if status == GristFailed && reason == GristReasonNoAnswer {
			// The model's answer did not fit the schema.
			return smokeTry{
				why: fmt.Sprintf("the mill %s it: %s", status, reason), slip: true, txid: unanswered.Txid,
				misses: []ExpectFailure{{Field: "(answer)", Wanted: "an answer that fits the grind's schema", Got: reason}},
			}
		}
		return smokeTry{why: fmt.Sprintf("the mill %s it: %s", status, reason)}
	}
	if errors.Is(err, ErrPosternUnreachable) {
		return smokeTry{notRun: ErrPosternUnreachable.Error() + ": " + firstLine(err.Error())}
	}
	if status, ok := gristBackendsFault(err); ok {
		return smokeTry{notRun: fmt.Sprintf("the postern backend refused it with %d, and again when it was retried: %s", status, firstLine(err.Error()))}
	}
	if err != nil {
		return smokeTry{why: "it could not be sent: " + firstLine(err.Error())}
	}
	if sent.Answer == nil {
		return smokeTry{why: "the mill left no answer"}
	}
	if send.GrindOverride != nil && !sent.Answer.Grind.Override {
		// A mill that does not know the override grinds with main's text and
		// would pass or fail the example for the wrong grind.
		return smokeTry{why: "the mill answered with the grind at main, not the ref's text (is the mill up to date?)"}
	}
	if violations := SchemaViolations(sent.Answer.Answer, schema); len(violations) > 0 {
		return smokeTry{
			why: "the answer does not fit the grind's schema: " + strings.Join(violations, ", "), slip: true, txid: sent.Txid,
			misses: []ExpectFailure{{Field: "(answer)", Wanted: "an answer that fits the grind's schema", Got: strings.Join(violations, ", ")}},
		}
	}
	var missed []string
	misses := CheckExpect(sent.Answer.Answer, expect)
	for _, f := range misses {
		missed = append(missed, f.String())
	}
	if len(missed) > 0 {
		return smokeTry{why: "the answer misses what the example expects: " + strings.Join(missed, "; "), slip: true, misses: misses, txid: sent.Txid}
	}
	return smokeTry{answer: sent.Answer.Answer, talk: sent.Answer.Talk}
}

// gristAnswerIsNotTheExamples says the mill would not grind a grist for a
// reason that is not in the examples: it refused it because the key has sent its
// limit today, or has no licence for the app, or failed it because the harness
// stopped the grind at a limit of the factory's, its usage limit or its model's
// (mw-gq6.436). The smoke cannot say whether the app's grist works then.
func gristAnswerIsNotTheExamples(status, reason string) bool {
	switch status {
	case GristRefused:
		return reason == GristReasonLicence || isGristReasonDaily(reason)
	case GristFailed:
		return IsGristReasonLimited(reason)
	}
	return false
}

// gristMillsFault says the mill failed a grist for a reason that is the mill's
// or its host's, and not the example's, so one more send may well pass. The
// verdict on each reason the mill fails a grist with, and so on every "send it
// again" reason of gristgrind.go:
//
//	GristReasonSessionEnd: mill's fault, retried. The grind's session ended
//	  before an answer, on a busy host (mw-gq6.454).
//	GristReasonTimedOut: mill's fault, retried. The grind ran out of time, which
//	  a loaded host causes as easily as a heavy grind (mw-gq6.490).
//	GristReasonUnread: mill's fault, retried. The checkout's commit or the
//	  grind's files could not be read (git or disk trouble); a grind that is
//	  missing or broken is refused instead, and fails at once.
//	GristReasonPhotoLost: mill's fault, retried. The photo's blob could not be
//	  fetched from the postern, a network matter; a photo that is forged or of
//	  the wrong type is another reason, and fails at once.
//	GristReasonNotSent: mill's fault, retried. The factory could not mail a
//	  forwarded grist on (temp dir, keeping the photos, the mailbox).
//	GristReasonNoAnswer: app's fault, not retried here. The grind's answer did
//	  not match its schema: the grind or its instructions are what the smoke is
//	  there to test. GristSmoke.example sends it again GristSmokeResends times
//	  as a wrong answer, and a miss that does not repeat is a flake, said and
//	  mailed, not a hold (mw-gq6.664).
func gristMillsFault(err error) bool {
	unanswered, ok := GristUnansweredIn(err)
	if !ok || unanswered.Answer == nil || unanswered.Answer.Status != GristFailed {
		return false
	}
	switch strings.TrimSpace(unanswered.Answer.Reason) {
	case GristReasonSessionEnd, GristReasonTimedOut, GristReasonUnread, GristReasonPhotoLost, GristReasonNotSent:
		return true
	}
	return false
}

// gristBackendsFault says the postern backend refused a send for its own
// trouble, and not for anything in the request: it said 429, 502, 503 or 504,
// or another 5xx whose body names an upstream 429 (a chain service throttling
// the backend's licence check). A refusal about the request (400, 401, 403,
// 404) is not, and fails the example. status is the backend's.
func gristBackendsFault(err error) (status int, ok bool) {
	status, ok = PosternStatusOf(err)
	if !ok {
		return 0, false
	}
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return status, true
	}
	return status, status >= 500 && strings.Contains(err.Error(), " 429")
}

// sleep waits d, or until ctx ends.
func (s GristSmoke) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s GristSmoke) say(format string, args ...any) {
	if s.Out != nil {
		fmt.Fprintf(s.Out, format+"\n", args...)
	}
}

// GristSmokePrefix is the start of the note each app's last smoke is kept
// under, GristSmokePrefix + app. A note, not a state, so that it is never an
// event of its own (see PosternNotes).
const GristSmokePrefix = "grist.smoke."

// GristLevelPrefix is the start of the note each app's refusal to be brought
// level is kept under, GristLevelPrefix + app: the reason GristLevel left the
// checkout alone, while it is so.
const GristLevelPrefix = "grist.level."

// gristNotesPrefix begins both GristSmokePrefix and GristLevelPrefix.
const gristNotesPrefix = "grist."

// GristSmokeJob is the actor of the job event a failed smoke writes.
const GristSmokeJob = "grist-smoke"

// GristSmokeNotes is the part of the tracker's key-value store the smoke's
// record is kept in: TrackerSync's own Note, SetNote and ClearNote, and
// NotesWithPrefix.
type GristSmokeNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
	ClearNote(ctx context.Context, key string) error
	NotesWithPrefix(ctx context.Context, prefix string) (map[string]string, error)
}

// GristSmokeRecord is what the last smoke of an app found, kept until the next
// one. A failed record holds the stories of Rig back (GristSmokeHolds) and is
// a line of mw status.
type GristSmokeRecord struct {
	App string `json:"app"`
	// Rig is the rig whose landing made the smoke run, and so the rig whose
	// stories are held; empty for a smoke run by hand.
	Rig      string    `json:"rig,omitempty"`
	At       time.Time `json:"at"`
	Commit   string    `json:"commit,omitempty"`
	Examples int       `json:"examples"`
	Failed   bool      `json:"failed"`
	Failures []string  `json:"failures,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
	// FiledOn are the open beads that named every failing example when the smoke
	// was recorded (GristSmokeReport.FiledOn): the failure holds nothing then.
	FiledOn []string `json:"filed_on,omitempty"`
	// NotRun is why the mill could not be asked (GristSmokeReport.NotRun). It
	// is no failure and holds nothing.
	NotRun []string `json:"not_run,omitempty"`
	// Behind is why the home's checkout of the app's rig is not level with the
	// rig's main (GristLevel), so that the mill grinds with older grinds than the
	// app sends; empty when it is level. It is kept apart from the smoke's own
	// record under GristLevelPrefix and joined to it when the records are read; a
	// record with only this has no smoke yet (At is zero).
	Behind string `json:"behind,omitempty"`
	// Fixes are the open beads that name some failing example but not all, so that
	// the failure still holds the rig yet each of them runs over the hold. Worked
	// out when the records are read (Records), never kept.
	Fixes []string `json:"-"`
}

// Held says the failure holds the stories of Rig: it failed, and its failing
// examples are not all filed on an open bead.
func (r GristSmokeRecord) Held() bool { return r.Failed && len(r.FiledOn) == 0 }

// FailingExamples are the examples the record's failures name, as
// <app>/<kind>/<example>.
func (r GristSmokeRecord) FailingExamples() []string {
	var names []string
	for _, failure := range r.Failures {
		if key := smokeExampleOf(failure); key != "" {
			names = append(names, key)
		}
	}
	return names
}

// Line is the record as mw status and a story's comment say it.
func (r GristSmokeRecord) Line() string {
	if r.Failed && len(r.FiledOn) > 0 {
		return fmt.Sprintf("grist smoke: %s FAILED %s: %s. All filed on %s, not held.",
			r.App, r.At.UTC().Format("2006-01-02 15:04Z"), strings.Join(r.Failures, "; "), strings.Join(r.FiledOn, ", "))
	}
	if r.Failed {
		held := "no rig's stories are held"
		if r.Rig != "" {
			held = "the open stories of " + r.Rig + " are held"
		}
		line := fmt.Sprintf("grist smoke: %s FAILED %s (%s): %s. `mw grist smoke %s` that passes lifts the hold, or `mw grist smoke %s --lift`.",
			r.App, r.At.UTC().Format("2006-01-02 15:04Z"), held, strings.Join(r.Failures, "; "), r.App, r.App)
		if len(r.Fixes) > 0 {
			line += " " + strings.Join(r.Fixes, ", ") + " runs over the hold, as it names a failing example."
		}
		return line
	}
	if len(r.NotRun) > 0 {
		return fmt.Sprintf("grist smoke: %s not run %s: %s", r.App, r.At.UTC().Format("2006-01-02 15:04Z"), strings.Join(r.NotRun, "; "))
	}
	return fmt.Sprintf("grist smoke: %s ok %s, %d examples", r.App, r.At.UTC().Format("2006-01-02 15:04Z"), r.Examples)
}

// PostLine is the failed record as the event log posts it: one line for a
// phone, with the rig, the time, how many examples were wrong and the hold,
// and none of the examples' own errors, which stay in Line, for mw status.
func (r GristSmokeRecord) PostLine() string {
	subject, held := r.App, "no rig's stories are held"
	if r.Rig != "" {
		subject, held = r.Rig, "the open stories of "+r.Rig+" are held"
	}
	return fmt.Sprintf("grist smoke: %s FAILED %s: %d examples wrong, %s; details in mw status",
		subject, r.At.UTC().Format("2006-01-02 15:04Z"), len(r.Failures), held)
}

// GristSmokeRecords is what mw status reads of the smoke's record.
type GristSmokeRecords interface {
	Records(ctx context.Context) ([]GristSmokeRecord, error)
}

// GristSmokeHolds is what dispatch asks before it starts a story: whether a
// failed smoke holds the stories of its rig, and the reason when it does. It
// says it for every rig at once, by rig; a rig held by none is not there.
type GristSmokeHolds interface {
	Holds(ctx context.Context) (map[string]GristSmokeHold, error)
}

// GristSmokeHold is the hold a failed smoke puts on a rig: why, and the examples
// that failed, so that a story naming one of them (the fix) is not held by it.
// The zero value is no hold.
type GristSmokeHold struct {
	Reason   string
	Examples []string
}

// Names says a story that names the examples story in its SmokeExamplesField is
// the fix of one of the failing examples.
func (h GristSmokeHold) Names(story []string) bool {
	return slices.ContainsFunc(story, func(example string) bool { return slices.Contains(h.Examples, example) })
}

// Fix says why a story is the fix of the failure, and so is not held by it, and
// is empty when it is not: it names a failing example in its SmokeExamplesField,
// or, the bead filed as the cause having been filed without that, in its title or
// description, as <app>/<kind>/<example>. A fix held behind the hold it is to
// lift is the one story that can never end it (mw-gq6.579).
func (h GristSmokeHold) Fix(detail StoryDetail) string {
	for _, example := range detail.SmokeExamples {
		if slices.Contains(h.Examples, example) {
			return "it names the failing example " + example + " in its smoke_examples"
		}
	}
	for _, example := range h.Examples {
		if strings.Contains(detail.Story.Title, example) || strings.Contains(detail.Description, example) {
			return "its title or description names the failing example " + example
		}
	}
	return ""
}

// GristSmokeFiled is what the smoke asks the tracker: which failing examples an
// open bead already names.
type GristSmokeFiled interface {
	// FiledSmokeExamples are the examples named by the SmokeExamplesField of an
	// open bead, each with the id of the bead that names it. A closed bead names
	// none.
	FiledSmokeExamples(ctx context.Context) (map[string]string, error)
}

// GristSmokeBook keeps the record of each app's last smoke in the tracker's
// notes, and posts the alarm a failed smoke is.
type GristSmokeBook struct {
	Notes GristSmokeNotes
	// Filed says which examples an open bead names. A nil Filed names none, so
	// every failure holds.
	Filed GristSmokeFiled
	// Events is the home's event log, where a failure is posted as a failed job
	// event. A nil Events posts none.
	Events EventLog
	// Comments is where a miss of an example an open bead already names is
	// written as a dated note on that bead (mw-gq6.696). Nil writes none.
	Comments GristSmokeCommenter
	// Mailbox is where the Mayor is mailed of a flaky example. Nil mails none.
	Mailbox Mailbox
	Host    string
	Now     func() time.Time
}

// GristSmokeCommenter writes a comment on a bead.
type GristSmokeCommenter interface {
	CommentOnStory(ctx context.Context, id, text string) error
}

var (
	_ GristSmokeRecords = GristSmokeBook{}
	_ GristSmokeHolds   = GristSmokeBook{}
)

func (b GristSmokeBook) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Record keeps what a smoke found as the app's last, and for a failure posts
// the alarm (a smoke that was not run is kept, and posts none; nor does a failure
// with the same failures as the record before it, which is no news). rig is the rig whose landing made the smoke run, empty by hand: a
// failure by hand keeps the rig an earlier failure held. An app with no grinds
// has its record forgotten.
func (b GristSmokeBook) Record(ctx context.Context, rig string, report GristSmokeReport) error {
	mailed := b.mailFlakes(ctx, report)
	report = b.Mark(ctx, report)
	noted := b.noteFiled(ctx, report)
	return errors.Join(b.record(ctx, rig, report), mailed, noted)
}

// mailFlakes mails the Mayor once for each flaky example of a smoke, so that a
// slip a re-send hid still gets filed (mw-gq6.664). An example an open bead
// already names is not to be filed: the mail says so, and the miss is added to
// that bead as a dated note (mw-gq6.696).
func (b GristSmokeBook) mailFlakes(ctx context.Context, report GristSmokeReport) error {
	if b.Mailbox == nil {
		return nil
	}
	named := b.named(ctx)
	var failed []error
	for _, flake := range report.Flakes {
		var misses []string
		for _, miss := range flake.Misses {
			misses = append(misses, fmt.Sprintf("  field %s: wanted %s, got %s", miss.Field, miss.Wanted, miss.Got))
		}
		next := fmt.Sprintf("A model's answer that misses now and then is a fact about the grind's instructions: file it as a bug, and name the example on it "+
			"(mw grist smoke %s --filed <bead> %s) so that a later miss of it that repeats is not held.",
			report.App, strings.TrimPrefix(flake.Example, report.App+"/"))
		if bead, ok := named[flake.Example]; ok {
			next = "It is already named on " + bead + ", nothing to file."
			note := fmt.Sprintf("%s grist smoke of %s: %s, the failing run's txid %s:\n%s",
				b.now().UTC().Format("2006-01-02 15:04Z"), report.App, flake, flake.Txid, strings.Join(misses, "\n"))
			if err := b.note(ctx, bead, note); err != nil {
				next += " The miss could not be added to it as a note, so add it by hand: " + firstLine(err.Error())
				failed = append(failed, fmt.Errorf("the flake %s could not be noted on %s: %w", flake.Example, bead, err))
			} else {
				next += " The miss is added to it as a dated note."
			}
		}
		_, err := b.Mailbox.Send(ctx, NewMessage{
			From:    SeatIdentity(MwSeat, b.Host),
			To:      MayorMailbox,
			Subject: "Smoke flake: " + flake.Example,
			Body: fmt.Sprintf("The grist smoke of %s sent %s %d times and its answer was wrong in %d of them (%s), right in the others, so it held nothing.\n\n"+
				"The first wrong answer (the failing run's txid %s):\n%s\n\n%s",
				report.App, flake.Example, flake.Sent, flake.Missed, flake, flake.Txid, strings.Join(misses, "\n"), next),
		})
		if err != nil {
			failed = append(failed, fmt.Errorf("the Mayor could not be mailed of the flake %s: %w", flake.Example, err))
		}
	}
	return errors.Join(failed...)
}

// named are the examples an open bead names, each with the bead; empty when the
// tracker cannot be asked.
func (b GristSmokeBook) named(ctx context.Context) map[string]string {
	if b.Filed == nil {
		return nil
	}
	named, err := b.Filed.FiledSmokeExamples(ctx)
	if err != nil {
		return nil
	}
	return named
}

// note writes text on bead; no Comments writes nothing and is an error, so that
// a mail never says a note was added that was not.
func (b GristSmokeBook) note(ctx context.Context, bead, text string) error {
	if b.Comments == nil {
		return errors.New("no tracker to write the note on")
	}
	return b.Comments.CommentOnStory(ctx, bead, text)
}

// noteFiled adds each failure of a report that an open bead names in full
// (FiledOn) to the bead naming it, as a dated note, so that the Mayor has
// nothing to add by hand (mw-gq6.696). A report that holds writes none.
func (b GristSmokeBook) noteFiled(ctx context.Context, report GristSmokeReport) error {
	if len(report.FiledOn) == 0 || b.Comments == nil {
		return nil
	}
	named := b.named(ctx)
	var failed []error
	for _, failure := range report.Failures {
		bead, ok := named[smokeExampleOf(failure)]
		if !ok {
			continue
		}
		note := fmt.Sprintf("%s grist smoke of %s, already named on %s: %s", b.now().UTC().Format("2006-01-02 15:04Z"), report.App, bead, failure)
		if err := b.note(ctx, bead, note); err != nil {
			failed = append(failed, fmt.Errorf("the failure of %s could not be noted on %s: %w", smokeExampleOf(failure), bead, err))
		}
	}
	return errors.Join(failed...)
}

func (b GristSmokeBook) record(ctx context.Context, rig string, report GristSmokeReport) error {
	if b.Notes == nil {
		return nil
	}
	key := GristSmokePrefix + report.App
	if report.NoGrinds {
		return b.Notes.ClearNote(ctx, key)
	}
	record := GristSmokeRecord{
		App: report.App, Rig: rig, At: b.now().UTC(), Commit: report.Commit, Examples: report.Examples,
		Failed: report.Failed(), Failures: report.Failures, Warnings: report.Warnings, NotRun: report.NotRun,
		FiledOn: report.FiledOn,
	}
	before, had := b.read(ctx, key)
	if len(record.NotRun) > 0 && had && before.Failed {
		// A smoke that was not run proves nothing of the failure before it: its
		// hold stays until a smoke passes or is lifted.
		return nil
	}
	if record.Failed && record.Rig == "" && had && before.Failed {
		record.Rig = before.Rig
	}
	text, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := b.Notes.SetNote(ctx, key, string(text)); err != nil {
		return fmt.Errorf("the smoke of %s could not be recorded: %w", report.App, err)
	}
	if !record.Held() || b.Events == nil {
		// A failure filed on an open bead is known to the Mayor already, and holds
		// nothing: no alarm.
		return nil
	}
	if had && before.Failed && sameFailures(before.Failures, record.Failures) {
		// A failure the last record already holds is no news: recorded above, so
		// mw status shows the new time, but not posted again (mw-gq6.338).
		return nil
	}
	// The Mayor's to act on, not the Governor's: the normal lane, and a line short
	// enough for a phone. mw status and the note hold the detail.
	_, err = EventEmit{
		Log: b.Events, Now: b.now,
		Event: events.Event{
			Kind: events.KindJob, Actor: GristSmokeJob + "@" + b.Host,
			From: events.JobRunning, To: events.JobFailed,
			Detail: record.PostLine(),
		},
	}.Run(ctx)
	if err != nil {
		return fmt.Errorf("the alarm for the smoke of %s could not be posted: %w", report.App, err)
	}
	return nil
}

// Mark sets which open beads name every failing example of report, so that its
// line says so and Record keeps it. A failure with any example not named, or any
// failure that is no example's (a refusal, an unreadable grind), is not filed.
// A tracker that cannot be read names nothing: the failure holds.
func (b GristSmokeBook) Mark(ctx context.Context, report GristSmokeReport) GristSmokeReport {
	report.FiledOn = nil
	if !report.Failed() || b.Filed == nil {
		return report
	}
	named, err := b.Filed.FiledSmokeExamples(ctx)
	if err != nil {
		return report
	}
	report.FiledOn = filedOn(report.Failures, named)
	return report
}

// filedOn are the beads, sorted, that name every one of failures in named, and
// nil when any failure is not named.
func filedOn(failures []string, named map[string]string) []string {
	beads := map[string]bool{}
	for _, failure := range failures {
		bead, ok := named[smokeExampleOf(failure)]
		if !ok {
			return nil
		}
		beads[bead] = true
	}
	var found []string
	for bead := range beads {
		found = append(found, bead)
	}
	sort.Strings(found)
	return found
}

// smokeExampleOf is the example a failure line is for, <app>/<kind>/<example>
// as the line starts, and empty for a failure that is no example's.
func smokeExampleOf(failure string) string {
	key, _, found := strings.Cut(failure, ": ")
	if !found || strings.ContainsAny(key, " \t") || len(strings.Split(key, "/")) != 3 {
		return ""
	}
	return key
}

// sameFailures says two smokes failed with the same set of examples and reasons,
// whatever order they were found in.
func sameFailures(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	sort.Strings(a)
	sort.Strings(b)
	return slices.Equal(a, b)
}

// Behind keeps why the home's checkout of app's rig was left behind the rig's
// main, and reports whether that is news: false when the same reason is already
// kept. An empty why forgets the refusal (the checkout is level), and is never
// news.
func (b GristSmokeBook) Behind(ctx context.Context, app, why string) (bool, error) {
	if b.Notes == nil {
		return false, nil
	}
	key := GristLevelPrefix + app
	if why == "" {
		return false, b.Notes.ClearNote(ctx, key)
	}
	if kept, err := b.Notes.Note(ctx, key); err == nil && kept == why {
		return false, nil
	}
	return true, b.Notes.SetNote(ctx, key, why)
}

// Lift ends the hold a failed smoke of app put on its rig's stories, without a
// smoke that passes, for the failure that is the fix's own story to mend. It
// reports whether there was a hold.
func (b GristSmokeBook) Lift(ctx context.Context, app string) (bool, error) {
	key := GristSmokePrefix + app
	record, ok := b.read(ctx, key)
	if !ok || !record.Failed {
		return false, nil
	}
	record.Failed = false
	text, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	return true, b.Notes.SetNote(ctx, key, string(text))
}

func (b GristSmokeBook) read(ctx context.Context, key string) (GristSmokeRecord, bool) {
	text, err := b.Notes.Note(ctx, key)
	if err != nil || text == "" {
		return GristSmokeRecord{}, false
	}
	var record GristSmokeRecord
	if json.Unmarshal([]byte(text), &record) != nil {
		return GristSmokeRecord{}, false
	}
	return record, true
}

// Records implements GristSmokeRecords: every app's last smoke, by app.
func (b GristSmokeBook) Records(ctx context.Context) ([]GristSmokeRecord, error) {
	if b.Notes == nil {
		return nil, nil
	}
	// One listing holds both kinds of note: each listing is a bd kv list,
	// two seconds on a Boost (mw-gq6.452).
	notes, err := b.Notes.NotesWithPrefix(ctx, gristNotesPrefix)
	if err != nil {
		return nil, err
	}
	var records []GristSmokeRecord
	for key, text := range notes {
		var record GristSmokeRecord
		if strings.HasPrefix(key, GristSmokePrefix) && json.Unmarshal([]byte(text), &record) == nil && record.App != "" {
			records = append(records, record)
		}
	}
	for key, why := range notes {
		app, behind := strings.CutPrefix(key, GristLevelPrefix)
		if !behind || app == "" || why == "" {
			continue
		}
		i := slices.IndexFunc(records, func(r GristSmokeRecord) bool { return r.App == app })
		if i < 0 {
			records = append(records, GristSmokeRecord{App: app})
			i = len(records) - 1
		}
		records[i].Behind = why
	}
	sort.Slice(records, func(i, j int) bool { return records[i].App < records[j].App })
	b.markFixes(ctx, records)
	return records, nil
}

// markFixes sets Fixes on each failed record that holds its rig: the open beads
// that name some of its failing examples. A tracker that cannot be read names none.
func (b GristSmokeBook) markFixes(ctx context.Context, records []GristSmokeRecord) {
	if b.Filed == nil {
		return
	}
	var named map[string]string
	for i, r := range records {
		if !r.Held() {
			continue
		}
		if named == nil {
			var err error
			if named, err = b.Filed.FiledSmokeExamples(ctx); err != nil {
				return
			}
		}
		for _, example := range r.FailingExamples() {
			if bead, ok := named[example]; ok && !slices.Contains(records[i].Fixes, bead) {
				records[i].Fixes = append(records[i].Fixes, bead)
			}
		}
		sort.Strings(records[i].Fixes)
	}
}

// Holds implements GristSmokeHolds: a rig is held by the first of its failed
// records, by app.
func (b GristSmokeBook) Holds(ctx context.Context) (map[string]GristSmokeHold, error) {
	records, err := b.Records(ctx)
	if err != nil {
		return nil, err
	}
	holds := map[string]GristSmokeHold{}
	for _, r := range records {
		if _, held := holds[r.Rig]; held || !r.Held() {
			continue
		}
		holds[r.Rig] = GristSmokeHold{
			Reason:   fmt.Sprintf("the grist smoke of %s failed after a landing in %s and holds its open stories: %s", r.App, r.Rig, strings.Join(r.Failures, "; ")),
			Examples: r.FailingExamples(),
		}
	}
	return holds, nil
}

// Hold is the hold on rig alone: the zero value when none holds it.
func (b GristSmokeBook) Hold(ctx context.Context, rig string) (GristSmokeHold, error) {
	holds, err := b.Holds(ctx)
	return holds[rig], err
}

// HeldBy is why a failed smoke holds the stories of rig, empty when none does.
func (b GristSmokeBook) HeldBy(ctx context.Context, rig string) (string, error) {
	hold, err := b.Hold(ctx, rig)
	return hold.Reason, err
}

// GristSmokeFactoryPaths are the paths of the factory's own rig whose change
// makes a landing smoke every app that has grinds: the mill, the send and the
// grind session. They are git pathspecs, where * also crosses a /.
var GristSmokeFactoryPaths = []string{
	"application/grist*.go",
	"infrastructure/grist",
	"infrastructure/claude/grind*.go",
	"infrastructure/rig/grinds*.go",
}

// GristSmokeGrindsPath is the directory of an app's rig whose change makes a
// landing smoke the app: its grinds and their examples.
const GristSmokeGrindsPath = GristSmokeDir

// LandingTouches is the port a landing is asked what it changed through:
// whether the commits branch has beyond base change anything at the pathspec
// in the rig at rigDir. infrastructure/rig's Worktrees is the real one, and
// answers BackendBuilds.Changed in the same words.
type LandingTouches interface {
	Changed(ctx context.Context, rigDir, base, branch, pathspec string) (bool, error)
}

// GristAppSmoker smokes one app: GristSmoke is the real one.
type GristAppSmoker interface {
	Run(ctx context.Context, app, kind string) (GristSmokeReport, error)
}

// GristSmokeAfter is the smoke a landing runs once it has moved the rig:
// for an app's own rig when the landing touched grinds/ or the app's grist
// client paths, and for every app with grinds when the factory's own rig
// landed a change to the mill. The zero value runs nothing.
type GristSmokeAfter struct {
	Touches LandingTouches
	Smoke   GristAppSmoker
	// Built makes the smoke of a landing in the factory's own rig with the mw
	// that landing built, in a process of its own: the mw the landing runs in
	// was started before the merge, so a landing that fixes the smoke would
	// otherwise be smoked by the smoke it fixed (mw-gq6.339). A nil Built
	// smokes with Smoke.
	Built GristAppSmoker
	Book  GristSmokeBook
	// Apps is where each app's rig is checked out on this host
	// ([grist-apps]) and Rigs where each rig is ([rigs]): an app belongs to
	// the rig it is checked out in, or the rig of its own name.
	Apps map[string]string
	Rigs map[string]string
	// ClientPaths are the pathspecs of each rig that hold its grist client
	// ([grist_smoke]), besides grinds/.
	ClientPaths map[string][]string
}

// Enabled says there is anything to smoke on this host.
func (a GristSmokeAfter) Enabled() bool {
	return a.Touches != nil && a.Smoke != nil && len(a.Apps) > 0
}

// Wants reports which apps the landing of branch onto base in rig changes the
// grist of, sorted: before the merge, while the branch's own commits are still
// told apart from the target's. A change that cannot be read counts as a
// change, since a smoke too many costs a minute and one missed costs a broken
// app nobody was told of; why is said as notes.
func (a GristSmokeAfter) Wants(ctx context.Context, rig, rigDir, base, branch string) (apps []string, notes []string) {
	if !a.Enabled() {
		return nil, nil
	}
	touched := func(pathspec string) bool {
		changed, err := a.Touches.Changed(ctx, rigDir, base, branch, pathspec)
		if err != nil {
			notes = append(notes, fmt.Sprintf("grist smoke: whether %s changed %s could not be read, so it counts as changed: %s", branch, pathspec, firstLine(err.Error())))
			return true
		}
		return changed
	}
	wanted := map[string]bool{}
	if rig == FactoryRig {
		for _, spec := range GristSmokeFactoryPaths {
			if touched(spec) {
				for app := range a.Apps {
					wanted[app] = true
				}
				break
			}
		}
	}
	own := a.appsOf(rig)
	if len(own) > 0 {
		hit := touched(GristSmokeGrindsPath)
		for _, spec := range a.ClientPaths[rig] {
			hit = hit || touched(spec)
		}
		if hit {
			for _, app := range own {
				wanted[app] = true
			}
		}
	}
	for app := range wanted {
		apps = append(apps, app)
	}
	sort.Strings(apps)
	return apps, notes
}

// appsOf are the apps checked out in rig's directory, or named for it.
func (a GristSmokeAfter) appsOf(rig string) []string {
	var own []string
	for app, dir := range a.Apps {
		if app == rig || (a.Rigs[rig] != "" && filepath.Clean(dir) == filepath.Clean(a.Rigs[rig])) {
			own = append(own, app)
		}
	}
	sort.Strings(own)
	return own
}

// Run smokes each app and records what it found against rig, whose stories a
// failure holds. It returns one line for each app that had grinds, and the
// lines that are failures, which a landing also writes on its story. An app
// that cannot be smoked at all is a failure of its own.
func (a GristSmokeAfter) Run(ctx context.Context, rig string, apps []string) (lines, failed []string) {
	smoker := a.Smoke
	if rig == FactoryRig && a.Built != nil {
		smoker = a.Built
	}
	for _, app := range apps {
		report, err := smoker.Run(ctx, app, "")
		if err != nil {
			report.Failures = append(report.Failures, "the smoke could not be made: "+firstLine(err.Error()))
		}
		if report.NoGrinds {
			continue
		}
		report = a.Book.Mark(ctx, report)
		line := report.Line()
		lines = append(lines, line)
		if report.Held() {
			failed = append(failed, line)
		}
		if err := a.Book.Record(ctx, rig, report); err != nil {
			lines = append(lines, "grist smoke: "+firstLine(err.Error()))
		}
	}
	return lines, failed
}

// SmokeExamplesField is the metadata an open bead carries to say it holds the
// cause of failing smoke examples: a comma-separated list of
// <app>/<kind>/<example>. While the bead is open a smoke whose every failing
// example is named holds nothing, and the bead itself is never held by the
// failure it is the fix of; closing the bead ends it (mw-gq6.412).
const SmokeExamplesField = "smoke_examples"

// ParseSmokeExamples reads the SmokeExamplesField's value: its examples, in the
// order named, without blanks or repeats.
func ParseSmokeExamples(text string) []string {
	var examples []string
	for _, example := range strings.Split(text, ",") {
		if example = strings.TrimSpace(example); example != "" && !slices.Contains(examples, example) {
			examples = append(examples, example)
		}
	}
	return examples
}

// GristSmokeFile is `mw grist smoke <app> --filed <bead> <example>...`: it names
// failing examples on the open bead that holds their cause (SmokeExamplesField),
// and, when the app's last smoke failed with every failing example now named,
// ends the hold that failure put on its rig.
type GristSmokeFile struct {
	Tracker WorkTracker
	Book    GristSmokeBook
}

// Run adds the examples to the bead's SmokeExamplesField and returns the field's
// value as it is now. An example is <kind>/<example> or <app>/<kind>/<example>;
// the bead must be open.
func (f GristSmokeFile) Run(ctx context.Context, bead, app string, examples []string) ([]string, error) {
	if app == "" {
		return nil, errors.New("mw grist smoke --filed: name the app and at least one example, as <kind>/<example>")
	}
	var named []string
	if len(examples) == 0 {
		// No example named: the bead is the cause of the failure the app's last
		// smoke found (mw-gq6.579).
		var record GristSmokeRecord
		ok := false
		if f.Book.Notes != nil {
			record, ok = f.Book.read(ctx, GristSmokePrefix+app)
		}
		if !ok || !record.Failed {
			return nil, fmt.Errorf("mw grist smoke --filed: %s has no failing smoke to name, so name the examples as <kind>/<example>", app)
		}
		if named = record.FailingExamples(); len(named) == 0 {
			return nil, fmt.Errorf("mw grist smoke --filed: the failure of %s is no example's, so there is none to name: name them as <kind>/<example>", app)
		}
	}
	for _, example := range examples {
		example = strings.TrimSpace(example)
		switch parts := strings.Split(example, "/"); {
		case len(parts) == 2 && parts[0] != "" && parts[1] != "":
			example = app + "/" + example
		case len(parts) == 3 && parts[0] == app && parts[1] != "" && parts[2] != "":
		default:
			return nil, fmt.Errorf("mw grist smoke --filed: %q is not an example of %s: write it as <kind>/<example>", example, app)
		}
		named = append(named, example)
	}
	detail, err := f.Tracker.ShowStory(ctx, bead)
	if err != nil {
		return nil, fmt.Errorf("mw grist smoke --filed: reading %s: %w", bead, err)
	}
	if detail.Closed() {
		return nil, fmt.Errorf("mw grist smoke --filed: %s is closed, and a closed bead names nothing: name them on an open bead", bead)
	}
	all := slices.Clone(detail.SmokeExamples)
	for _, example := range named {
		if !slices.Contains(all, example) {
			all = append(all, example)
		}
	}
	if err := f.Tracker.SetStoryMetadata(ctx, bead, map[string]string{SmokeExamplesField: strings.Join(all, ",")}); err != nil {
		return nil, fmt.Errorf("mw grist smoke --filed: naming the examples on %s: %w", bead, err)
	}
	if err := f.Book.Refile(ctx, app); err != nil {
		return all, fmt.Errorf("mw grist smoke --filed: the examples are named, but the app's last smoke could not be read again: %w", err)
	}
	return all, nil
}

// Refile reads the failure app's last smoke recorded against the beads that
// name examples now, so that a bead filed after the failure ends its hold.
func (b GristSmokeBook) Refile(ctx context.Context, app string) error {
	if b.Notes == nil {
		return nil
	}
	key := GristSmokePrefix + app
	record, ok := b.read(ctx, key)
	if !ok || !record.Failed {
		return nil
	}
	marked := b.Mark(ctx, GristSmokeReport{App: app, Failures: record.Failures})
	if slices.Equal(marked.FiledOn, record.FiledOn) {
		return nil
	}
	record.FiledOn = marked.FiledOn
	text, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return b.Notes.SetNote(ctx, key, string(text))
}

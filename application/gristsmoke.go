package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// Out is where progress is printed; nil prints nothing.
	Out io.Writer
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
	// went wrong: it refused for the day's limit or the licence, or could not be
	// reached. None of that is the examples' fault, so it is no Failure and holds
	// nothing. Any Failure beside it takes these into itself.
	NotRun []string `json:"not_run,omitempty"`
}

// Failed says an example failed.
func (r GristSmokeReport) Failed() bool { return len(r.Failures) > 0 }

// Line is the report in one line, the shape every place that says it uses.
func (r GristSmokeReport) Line() string {
	switch {
	case r.NoGrinds:
		return fmt.Sprintf("grist smoke: %s: the app has no grinds", r.App)
	case r.Failed():
		return fmt.Sprintf("grist smoke: %s: FAILED %d of %d examples: %s", r.App, len(r.Failures), r.Examples, strings.Join(r.Failures, "; "))
	case len(r.NotRun) > 0:
		return fmt.Sprintf("grist smoke: %s: not run: %s", r.App, strings.Join(r.NotRun, "; "))
	}
	said := fmt.Sprintf("grist smoke: %s: ok, %d examples answered as expected", r.App, r.Examples)
	if len(r.Warnings) > 0 {
		said += " (" + strings.Join(r.Warnings, "; ") + ")"
	}
	return said
}

// ErrGristSmokeFailed is the error mw grist smoke leaves with when an example
// failed; the report says which.
var ErrGristSmokeFailed = errors.New("a grist smoke example failed")

// Run smokes app, or only its grind of kind when kind is not empty. An error is
// a smoke that could not be made at all (no such app, the rig unreadable); an
// example that failed is in the report.
func (s GristSmoke) Run(ctx context.Context, app, kind string) (GristSmokeReport, error) {
	report := GristSmokeReport{App: app}
	checkout, ok := s.Apps[app]
	if !ok || checkout == "" {
		return report, fmt.Errorf("mw grist smoke: the app %q has no rig in the [grist-apps] table of the config file (it has: %s)", app, strings.Join(s.appNames(), ", "))
	}
	if s.Grinds == nil || s.Send == nil {
		return report, errors.New("mw grist smoke: no grind source or sender is configured")
	}
	if err := s.Grinds.Refresh(ctx, checkout); err != nil {
		s.say("note: %s's rig could not be refreshed, so its own main is read: %v", app, firstLine(err.Error()))
	}
	commit, err := s.Grinds.Commit(ctx, checkout)
	if err != nil {
		return report, fmt.Errorf("mw grist smoke: %w", err)
	}
	report.Commit = commit
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
	raw, found, err := s.Grinds.ReadAt(ctx, checkout, commit, path.Join(GristSmokeDir, kind+".json"))
	var grind GrindFile
	if err != nil || !found || json.Unmarshal(raw, &grind) != nil {
		report.Failures = append(report.Failures, label+": the grind file could not be read")
		return
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
	schema := ""
	if rigPath(grind.AnswerSchema) {
		if text, ok, err := s.Grinds.ReadAt(ctx, checkout, commit, grind.AnswerSchema); err == nil && ok {
			schema = string(text)
		}
	}
	if schema == "" {
		report.Failures = append(report.Failures, label+": the answer schema "+grind.AnswerSchema+" could not be read")
		return
	}
	for _, example := range examples {
		name := strings.TrimSuffix(path.Base(example), ".json")
		report.Examples++
		s.say("sending %s/%s ...", label, name)
		why, notRun := s.example(ctx, checkout, commit, app, kind, example, schema)
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

// example sends one example and reports why it failed, empty when the answer
// came, fits the schema and shows everything expected. notRun, when it is not
// empty, says instead that the mill could not be asked, for a reason that is
// not the example's (see GristSmokeReport.NotRun).
func (s GristSmoke) example(ctx context.Context, checkout, commit, app, kind, file, schema string) (why, notRun string) {
	data, _, err := s.Grinds.ReadAt(ctx, checkout, commit, file)
	if err != nil {
		return "the example could not be read: " + firstLine(err.Error()), ""
	}
	example, err := ParseGristExample(data)
	if err != nil {
		return "the example is wrong: " + err.Error(), ""
	}
	dir, err := os.MkdirTemp(s.TempDir, "mw-grist-smoke-")
	if err != nil {
		return "no directory to send it from: " + err.Error(), ""
	}
	defer os.RemoveAll(dir)
	request := filepath.Join(dir, "request.json")
	if err := os.WriteFile(request, example.Request, 0o600); err != nil {
		return "the request could not be written: " + err.Error(), ""
	}
	var photos []string
	for _, name := range example.Photos {
		if name == "" || path.Base(name) != name {
			return fmt.Sprintf("the photo %q is not a file beside the example", name), ""
		}
		photo, found, err := s.Grinds.ReadAt(ctx, checkout, commit, path.Join(path.Dir(file), name))
		if err != nil || !found {
			return fmt.Sprintf("the photo %s is not beside the example", name), ""
		}
		local := filepath.Join(dir, name)
		if err := os.WriteFile(local, photo, 0o600); err != nil {
			return "a photo could not be written: " + err.Error(), ""
		}
		photos = append(photos, local)
	}
	wait := s.Wait
	if wait <= 0 {
		wait = GristSmokeWait
	}
	sent, err := s.Send.Run(ctx, GristSendRequest{App: app, Kind: kind, Version: example.SchemaVersion, RequestFile: request, Photos: photos, Wait: wait})
	if unanswered, ok := GristUnansweredIn(err); ok {
		if unanswered.Answer == nil {
			return fmt.Sprintf("no answer in %s", wait), ""
		}
		reason := strings.TrimSpace(unanswered.Answer.Reason)
		if unanswered.Answer.Status == GristRefused && gristRefusalIsNotTheExamples(reason) {
			return "", reason
		}
		return fmt.Sprintf("the mill %s it: %s", unanswered.Answer.Status, reason), ""
	}
	if errors.Is(err, ErrPosternUnreachable) {
		return "", ErrPosternUnreachable.Error() + ": " + firstLine(err.Error())
	}
	if err != nil {
		return "it could not be sent: " + firstLine(err.Error()), ""
	}
	if sent.Answer == nil {
		return "the mill left no answer", ""
	}
	if violations := SchemaViolations(sent.Answer.Answer, schema); len(violations) > 0 {
		return "the answer does not fit the grind's schema: " + strings.Join(violations, ", "), ""
	}
	var missed []string
	for _, f := range CheckExpect(sent.Answer.Answer, example.Expect) {
		missed = append(missed, f.String())
	}
	if len(missed) > 0 {
		return "the answer misses what the example expects: " + strings.Join(missed, "; "), ""
	}
	return "", ""
}

// gristRefusalIsNotTheExamples says the mill refused a grist for a reason that
// is not in the examples: the key has sent its limit today, or has no licence
// for the app. The smoke cannot say whether the app's grist works then.
func gristRefusalIsNotTheExamples(reason string) bool {
	return reason == GristReasonLicence || isGristReasonDaily(reason)
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
	// NotRun is why the mill could not be asked (GristSmokeReport.NotRun). It
	// is no failure and holds nothing.
	NotRun []string `json:"not_run,omitempty"`
	// Behind is why the home's checkout of the app's rig is not level with the
	// rig's main (GristLevel), so that the mill grinds with older grinds than the
	// app sends; empty when it is level. It is kept apart from the smoke's own
	// record under GristLevelPrefix and joined to it when the records are read; a
	// record with only this has no smoke yet (At is zero).
	Behind string `json:"behind,omitempty"`
}

// Line is the record as mw status and a story's comment say it.
func (r GristSmokeRecord) Line() string {
	if r.Failed {
		held := "no rig's stories are held"
		if r.Rig != "" {
			held = "the open stories of " + r.Rig + " are held"
		}
		return fmt.Sprintf("grist smoke: %s FAILED %s (%s): %s. `mw grist smoke %s` that passes lifts the hold, or `mw grist smoke %s --lift`.",
			r.App, r.At.UTC().Format("2006-01-02 15:04Z"), held, strings.Join(r.Failures, "; "), r.App, r.App)
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
// failed smoke holds the stories of its rig, and the reason when it does.
type GristSmokeHolds interface {
	HeldBy(ctx context.Context, rig string) (reason string, err error)
}

// GristSmokeBook keeps the record of each app's last smoke in the tracker's
// notes, and posts the alarm a failed smoke is.
type GristSmokeBook struct {
	Notes GristSmokeNotes
	// Events is the home's event log, where a failure is posted as a failed job
	// event. A nil Events posts none.
	Events EventLog
	Host   string
	Now    func() time.Time
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
	if !record.Failed || b.Events == nil {
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
	notes, err := b.Notes.NotesWithPrefix(ctx, GristSmokePrefix)
	if err != nil {
		return nil, err
	}
	var records []GristSmokeRecord
	for _, text := range notes {
		var record GristSmokeRecord
		if json.Unmarshal([]byte(text), &record) == nil && record.App != "" {
			records = append(records, record)
		}
	}
	behind, err := b.Notes.NotesWithPrefix(ctx, GristLevelPrefix)
	if err != nil {
		return nil, err
	}
	for key, why := range behind {
		app := strings.TrimPrefix(key, GristLevelPrefix)
		if app == "" || why == "" {
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
	return records, nil
}

// HeldBy implements GristSmokeHolds.
func (b GristSmokeBook) HeldBy(ctx context.Context, rig string) (string, error) {
	records, err := b.Records(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range records {
		if r.Failed && r.Rig == rig {
			return fmt.Sprintf("the grist smoke of %s failed after a landing in %s and holds its open stories: %s", r.App, rig, strings.Join(r.Failures, "; ")), nil
		}
	}
	return "", nil
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
		line := report.Line()
		lines = append(lines, line)
		if report.Failed() {
			failed = append(failed, line)
		}
		if err := a.Book.Record(ctx, rig, report); err != nil {
			lines = append(lines, "grist smoke: "+firstLine(err.Error()))
		}
	}
	return lines, failed
}

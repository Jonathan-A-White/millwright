package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// titledTranscripts is the transcripts of the seats' sessions, by the name of
// the window each runs in.
type titledTranscripts struct {
	mu     sync.Mutex
	tokens map[string]int
	read   []string
	err    error
}

func (f *titledTranscripts) TitledContext(_ context.Context, dir, title string) (application.TranscriptContext, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.read = append(f.read, dir+"|"+title)
	if f.err != nil {
		return application.TranscriptContext{}, f.err
	}
	n, ok := f.tokens[title]
	if !ok {
		return application.TranscriptContext{}, errors.New("no transcript titled " + title)
	}
	return application.TranscriptContext{SessionID: "0123456789abcdef", Tokens: n}, nil
}

// memMarks keeps the watcher's marks in memory, as a file beside the log would.
type memMarks struct {
	marks map[string]int
}

func (m *memMarks) Load(context.Context) (map[string]int, error) {
	out := map[string]int{}
	for k, v := range m.marks {
		out[k] = v
	}
	return out, nil
}

func (m *memMarks) Save(_ context.Context, marks map[string]int) error {
	m.marks = map[string]int{}
	for k, v := range marks {
		m.marks[k] = v
	}
	return nil
}

type contextWorld struct {
	windows *apptest.FakeWindows
	tr      *titledTranscripts
	marks   *memMarks
	limits  map[string]int
	err     bytes.Buffer
	now     time.Time
	watch   *application.SeatContextWatch
}

func newContextWorld(limits map[string]int) *contextWorld {
	w := &contextWorld{
		windows: apptest.NewFakeWindows(),
		tr:      &titledTranscripts{tokens: map[string]int{}},
		marks:   &memMarks{},
		limits:  limits,
		now:     time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	}
	w.watch = w.newWatch()
	return w
}

func (w *contextWorld) newWatch() *application.SeatContextWatch {
	return &application.SeatContextWatch{
		Limits:      func() (map[string]int, error) { return w.limits, nil },
		Dir:         "/vault",
		Transcripts: w.tr,
		Terminal:    w.windows,
		Marks:       w.marks,
		Now:         func() time.Time { return w.now },
		Err:         &w.err,
	}
}

// tick lets a minute pass and runs one watch.
func (w *contextWorld) tick(t *testing.T) {
	t.Helper()
	w.now = w.now.Add(time.Minute)
	if err := w.watch.Watch(context.Background()); err != nil {
		t.Fatalf("watch: %v", err)
	}
}

func (w *contextWorld) seatUp(id, name string, tokens int) {
	w.windows.HoldsWithID(id, name, time.Time{})
	w.tr.tokens[name] = tokens
}

var deputyLimits = map[string]int{"deputy": 200000, "millhand": 200000}

func TestWatchTypesTheLineOncePerCrossingAndAgainOnlyAfterAFurther20K(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.seatUp("@7", "deputy-2026-10-15-03", 205000)

	w.tick(t)
	want := "Context 205000 past 200000: write your handoff now and stop.\n"
	if got := w.windows.Typed("@7"); len(got) != 1 || got[0] != want {
		t.Fatalf("typed %q, want [%q]", got, want)
	}

	w.tr.tokens["deputy-2026-10-15-03"] = 206000
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 1 {
		t.Fatalf("a second tick at 206K typed again: %q", got)
	}

	w.tr.tokens["deputy-2026-10-15-03"] = 226000
	w.tick(t)
	got := w.windows.Typed("@7")
	if len(got) != 2 || got[1] != "Context 226000 past 200000: write your handoff now and stop.\n" {
		t.Fatalf("at 226K, 21K past the last line, typed %q", got)
	}
}

func TestWatchTypesNothingBelowTheLimit(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.seatUp("@7", "deputy-2026-10-15-03", 180000)
	w.tick(t)
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 0 {
		t.Fatalf("typed %q at 180K", got)
	}
}

func TestWatchWatchesAnySeatTheConfigListsWithNoCodeChange(t *testing.T) {
	w := newContextWorld(map[string]int{"scribe": 50000})
	w.seatUp("@4", "scribe-2026-10-15-01", 55000)
	w.seatUp("@5", "deputy-2026-10-15-03", 900000) // not listed: never read
	w.tick(t)
	if got := w.windows.Typed("@4"); len(got) != 1 || got[0] != "Context 55000 past 50000: write your handoff now and stop.\n" {
		t.Fatalf("the listed seat scribe was typed %q", got)
	}
	if got := w.windows.Typed("@5"); len(got) != 0 {
		t.Fatalf("an unlisted seat was typed %q", got)
	}
	for _, read := range w.tr.read {
		if strings.Contains(read, "deputy") {
			t.Fatalf("an unlisted seat's transcript was read: %v", w.tr.read)
		}
	}
}

func TestWatchLeavesABusyPaneAloneAndTypesWhenItIsIdle(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.seatUp("@7", "deputy-2026-10-15-03", 205000)
	if err := w.windows.Pane("@7", application.PaneWorking); err != nil {
		t.Fatal(err)
	}
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 0 {
		t.Fatalf("typed into a working pane: %q", got)
	}
	if err := w.windows.Pane("@7", application.PaneIdle); err != nil {
		t.Fatal(err)
	}
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 1 {
		t.Fatalf("an idle pane was typed %q", got)
	}
}

func TestWatchReadsOncePerMinute(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.seatUp("@7", "deputy-2026-10-15-03", 205000)
	w.tick(t)
	reads := len(w.tr.read)
	// Within the minute: nothing is read.
	w.now = w.now.Add(10 * time.Second)
	if err := w.watch.Watch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.tr.read) != reads {
		t.Fatalf("read again within the minute: %v", w.tr.read)
	}
}

func TestWatchRemembersWhatItToldAcrossARestart(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.seatUp("@7", "deputy-2026-10-15-03", 205000)
	w.tick(t)
	w.watch = w.newWatch() // the follower restarted
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 1 {
		t.Fatalf("a restarted watcher typed again: %q", got)
	}
}

func TestWatchTellsAgainAfterTheSessionFallsBelowTheLimitAndCrossesAgain(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.seatUp("@7", "deputy-2026-10-15-03", 205000)
	w.tick(t)
	w.tr.tokens["deputy-2026-10-15-03"] = 60000 // compacted
	w.tick(t)
	w.tr.tokens["deputy-2026-10-15-03"] = 203000
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 2 {
		t.Fatalf("a new crossing was typed %q", got)
	}
}

func TestWatchSaysAnUnreadableTranscriptOnceAndTypesNothing(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.windows.HoldsWithID("@7", "deputy-2026-10-15-03", time.Time{}) // no transcript
	w.tick(t)
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 0 {
		t.Fatalf("typed %q", got)
	}
	if n := strings.Count(w.err.String(), "watching the context of deputy-2026-10-15-03"); n != 1 {
		t.Fatalf("said %d times: %q", n, w.err.String())
	}
}

func TestWatchListsAHandoffAtOfZeroAsUnwatched(t *testing.T) {
	w := newContextWorld(map[string]int{"deputy": 0})
	w.seatUp("@7", "deputy-2026-10-15-03", 900000)
	w.tick(t)
	if got := w.windows.Typed("@7"); len(got) != 0 {
		t.Fatalf("typed %q into a seat listed at 0", got)
	}
}

func TestReadingsNameEveryListedWindowWithItsContext(t *testing.T) {
	w := newContextWorld(deputyLimits)
	w.seatUp("@7", "deputy-2026-10-15-03", 225000)
	w.seatUp("@8", "mayor-2026-10-15-396", 900000)
	got, err := w.watch.Readings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Window.Name != "deputy-2026-10-15-03" || got[0].Seat != "deputy" || got[0].Tokens != 225000 || got[0].HandoffAt != 200000 {
		t.Fatalf("readings %+v", got)
	}
	if !got[0].Over(application.SeatContextRetell) || got[0].Over(30000) {
		t.Fatalf("225K is 20K past 200K, and not 30K past it: %+v", got[0])
	}
}

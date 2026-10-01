package events_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// exampleBatch is the batch docs/events.md shows, and testdata/batch.json is
// its JSON byte for byte.
func exampleBatch() events.Batch {
	ts := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			panic(err)
		}
		return v
	}
	return events.Batch{
		From: 41,
		To:   43,
		Lane: events.LaneNormal,
		Events: []events.Event{
			{Seq: 41, Ts: ts("2026-10-01T13:02:07Z"), Kind: events.KindBeadChanged, Bead: "mw-jrx0s.4", Actor: "mw@laptop", From: "open", To: "claimed", Detail: "status", Lane: events.LaneNormal},
			{Seq: 42, Ts: ts("2026-10-01T13:02:07Z"), Kind: events.KindCardAnswered, Bead: "mw-6ww.55", Actor: "mw@laptop", From: "asked", To: "answered", Detail: "direct:a79d45422283b7f73c07ca4b61c81220ea469c85f5d781bfec668cf3a152e933", Lane: events.LaneNormal},
			{Seq: 43, Ts: ts("2026-10-01T13:02:08Z"), Kind: events.KindJob, Bead: "", Actor: "dispatch@laptop", From: "scheduled", To: "running", Detail: "", Lane: events.LaneNormal},
		},
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBatchMarshalsToTheDocumentedJSON(t *testing.T) {
	got, err := json.MarshalIndent(exampleBatch(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.TrimSpace(readFile(t, filepath.Join("testdata", "batch.json")))
	if !bytes.Equal(got, want) {
		t.Fatalf("expected\n%s\ngot\n%s", want, got)
	}
	doc := readFile(t, filepath.Join("..", "..", "docs", "events.md"))
	if !bytes.Contains(doc, append(append([]byte("```json\n"), want...), "\n```"...)) {
		t.Fatal("docs/events.md should show testdata/batch.json verbatim in a ```json block")
	}
}

func TestBatchRoundTrips(t *testing.T) {
	b := exampleBatch()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var back events.Batch
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b, back) {
		t.Fatalf("expected the batch back unchanged\n%+v\ngot\n%+v", b, back)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("expected the example batch valid, got %v", err)
	}
}

func TestTheJSONFieldNamesAreTheProtocols(t *testing.T) {
	raw, err := json.Marshal(exampleBatch())
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		Batch  map[string]json.RawMessage
		Events []map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &shape.Batch); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(shape.Batch["events"], &shape.Events); err != nil {
		t.Fatal(err)
	}
	if got := keys(shape.Batch); got != "events from lane to" {
		t.Errorf("expected the batch's fields events from lane to, got %s", got)
	}
	if got := keys(shape.Events[0]); got != "actor bead detail from kind lane seq to ts" {
		t.Errorf("expected an event's fields actor bead detail from kind lane seq to ts, got %s", got)
	}
}

func keys(m map[string]json.RawMessage) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, " ")
}

func TestEveryKindIsNamedWithItsMachine(t *testing.T) {
	want := map[string]events.Machine{
		events.KindBeadChanged:  events.MachineBead,
		events.KindCardAsked:    events.MachineCard,
		events.KindCardAnswered: events.MachineCard,
		events.KindCardApplied:  events.MachineCard,
		events.KindTalkTurn:     events.MachineTalk,
		events.KindJob:          events.MachineJob,
		events.KindMessage:      "",
		events.KindHandsRan:     "",
		events.KindMail:         "",
		events.KindHandover:     "",
		events.KindControl:      "",
	}
	got := events.Kinds()
	if len(got) != len(want) {
		t.Fatalf("expected %d kinds, got %v", len(want), got)
	}
	for _, k := range got {
		m, ok := want[k]
		if !ok {
			t.Errorf("unexpected kind %q", k)
			continue
		}
		if got, _ := events.KindMachine(k); got != m {
			t.Errorf("kind %q: expected machine %q, got %q", k, m, got)
		}
	}
	doc := string(readFile(t, filepath.Join("..", "..", "docs", "events.md")))
	for _, k := range got {
		if !strings.Contains(doc, "`"+k+"`") {
			t.Errorf("docs/events.md does not name the kind `%s`", k)
		}
	}
}

func TestAnEventIsValidOnlyAsATransitionOfItsKindsMachine(t *testing.T) {
	ok := func(e events.Event) events.Event {
		if e.Seq == 0 {
			e.Seq = 1
		}
		if e.Ts.IsZero() {
			e.Ts = time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
		}
		if e.Lane == "" {
			e.Lane = events.LaneNormal
		}
		return e
	}
	cases := []struct {
		name  string
		event events.Event
		want  string // "" when valid
	}{
		{"a bead claimed", ok(events.Event{Kind: events.KindBeadChanged, Bead: "mw-1", From: "open", To: "claimed"}), ""},
		{"a bead commented on, its status unchanged", ok(events.Event{Kind: events.KindBeadChanged, Bead: "mw-1", From: "running", To: "running", Detail: "comment"}), ""},
		{"a bead filed held", ok(events.Event{Kind: events.KindBeadChanged, Bead: "mw-1", To: "held"}), ""},
		{"a bead landed before it ran", ok(events.Event{Kind: events.KindBeadChanged, Bead: "mw-1", From: "open", To: "landed"}), `the bead machine has no transition from "open" to "landed"`},
		{"a bead change with no bead", ok(events.Event{Kind: events.KindBeadChanged, From: "open", To: "claimed"}), "names no bead"},
		{"a card asked", ok(events.Event{Kind: events.KindCardAsked, Bead: "mw-1", To: "asked"}), ""},
		{"a card answered twice", ok(events.Event{Kind: events.KindCardAnswered, Bead: "mw-1", From: "answered", To: "answered"}), `the card machine has no transition from "answered" to "answered"`},
		{"a card_asked event that answers", ok(events.Event{Kind: events.KindCardAsked, Bead: "mw-1", From: "asked", To: "answered"}), `a card_asked event ends in "asked"`},
		{"a talk turn answered", ok(events.Event{Kind: events.KindTalkTurn, From: "turn", To: "answer"}), ""},
		{"a job failed", ok(events.Event{Kind: events.KindJob, Actor: "backup@laptop", From: "running", To: "failed"}), ""},
		{"a message", ok(events.Event{Kind: events.KindMessage, Bead: "mw-1", Detail: "direct:ab"}), ""},
		{"a message with a state", ok(events.Event{Kind: events.KindMessage, From: "open", To: "closed"}), "a message event is no transition"},
		{"mail", ok(events.Event{Kind: events.KindMail, Bead: "mw-m1"}), ""},
		{"a hands step ran", ok(events.Event{Kind: events.KindHandsRan, Bead: "mw-1", Detail: "linger"}), ""},
		{"an unknown kind", ok(events.Event{Kind: "weather"}), `no kind "weather"`},
		{"no seq", events.Event{Kind: events.KindMail, Bead: "mw-m1", Ts: time.Now(), Lane: events.LaneNormal}, "no seq"},
		{"no time", events.Event{Seq: 1, Kind: events.KindMail, Bead: "mw-m1", Lane: events.LaneNormal}, "no time"},
		{"an unknown lane", ok(events.Event{Kind: events.KindMail, Bead: "mw-m1", Lane: "express"}), `no lane "express"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.event.Validate()
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("expected valid, got %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("expected an error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestABatchIsItsSeqRangeInOrderInOneLane(t *testing.T) {
	cases := []struct {
		name   string
		change func(*events.Batch)
		want   string
	}{
		{"as shown", func(*events.Batch) {}, ""},
		{"an emergency record of one event", func(b *events.Batch) {
			b.Lane, b.From, b.To = events.LaneEmergency, 42, 42
			e := b.Events[1]
			e.Lane = events.LaneEmergency
			b.Events = []events.Event{e}
		}, ""},
		{"no events", func(b *events.Batch) { b.Events = nil }, "holds no events"},
		{"to before from", func(b *events.Batch) { b.From, b.To = 43, 41 }, "runs from 43 to 41"},
		{"a seq missing", func(b *events.Batch) { b.Events = append(b.Events[:1], b.Events[2]) }, "holds 2 events for seqs 41 to 43"},
		{"out of order", func(b *events.Batch) { b.Events[1].Seq, b.Events[2].Seq = 43, 42 }, "event 2 has seq 43, not 42"},
		{"an event in another lane", func(b *events.Batch) { b.Events[2].Lane = events.LaneFallback }, `event 3 is in the fallback lane, the batch in the normal lane`},
		{"an unknown lane", func(b *events.Batch) { b.Lane = "express" }, `no lane "express"`},
		{"an impossible transition", func(b *events.Batch) { b.Events[0].From = "closed" }, `event 1 (seq 41): the bead machine has no transition from "closed" to "claimed"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := exampleBatch()
			c.change(&b)
			err := b.Validate()
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("expected valid, got %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("expected an error containing %q, got %v", c.want, err)
			}
		})
	}
}

// An emergency event is sent alone in a record that must stay under postern's
// payload limit, so its detail has a ceiling; CutDetail brings a longer text
// down to it.
func TestEmergencyDetailPastTheCeilingIsRefusedAndCutDetailFitsIt(t *testing.T) {
	ev := events.Event{Seq: 1, Ts: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Kind: events.KindJob, Actor: "doctor@laptop",
		From: events.JobRunning, To: events.JobFailed, Lane: events.LaneEmergency}
	pane := strings.Repeat(strings.Repeat("é", 200)+"\n", 12)
	ev.Detail = pane
	if err := ev.Validate(); err == nil || !strings.Contains(err.Error(), "detail") {
		t.Fatalf("expected an emergency detail of %d bytes to be refused, got %v", len(pane), err)
	}
	ev.Detail = events.CutDetail(pane)
	if err := ev.Validate(); err != nil {
		t.Fatalf("the cut detail (%d bytes) was refused: %v", len(ev.Detail), err)
	}
	if !utf8.ValidString(ev.Detail) || !strings.HasSuffix(ev.Detail, "…") {
		t.Fatalf("the cut detail should be whole runes ending in an ellipsis, got %q", ev.Detail)
	}
	if short := "short"; events.CutDetail(short) != short {
		t.Fatalf("a short detail should be kept whole")
	}
	ev.Lane, ev.Detail = events.LaneNormal, pane
	if err := ev.Validate(); err != nil {
		t.Fatalf("a normal event has no ceiling on its detail, got %v", err)
	}
}

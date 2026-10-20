package application_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// addPart indexes part k of n of turn 3 of talk-7, from the Governor to the
// Mayor, as a postern that splits a turn over its cap sends it (postern's
// docs/protocol.md section 20: part and parts beside the text).
func (f *talkPosternFixture) addPart(txid, text string, k, n int) int64 {
	f.t.Helper()
	plain, _ := json.Marshal(map[string]any{
		"talk": map[string]any{"id": "talk-7", "turn": 3}, "text": text, "role": application.TalkRoleTurn,
		"part": k, "parts": n,
	})
	ct, err := f.cipher.Encrypt(talkWaitMayorKey, string(plain))
	if err != nil {
		f.t.Fatal(err)
	}
	return f.postern.AddRecord(application.PosternRecord{
		Txid: txid, Class: "talk", To: talkWaitMayorKey, From: talkWaitGovernorKey, Ciphertext: ct,
		Ts: time.Date(2026, 10, 20, 1, 55, k, 0, time.UTC),
	}).Seq
}

// partsStream says hello, then runs each step, which indexes records and
// returns the seq to announce, and announces it; once the steps are done it
// holds the stream open until the wait ends it. A stream opened again does not
// run the steps again.
type partsStream struct{ steps *[]func() int64 }

func (s partsStream) Events(ctx context.Context, onEvent func(application.PosternEvent) error) error {
	if err := onEvent(application.PosternEvent{Kind: application.PosternEventHello}); err != nil {
		return err
	}
	for len(*s.steps) > 0 {
		step := (*s.steps)[0]
		*s.steps = (*s.steps)[1:]
		if err := onEvent(application.PosternEvent{Kind: application.PosternEventMessage, Seq: step()}); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// runParts waits over steps, with the parts wait at partsWait, and returns the
// report, what was printed and the dir the turn was saved in.
func (f *talkPosternFixture) runParts(partsWait time.Duration, steps ...func() int64) (application.TalkWaitReport, string, string) {
	f.t.Helper()
	var out strings.Builder
	dir := f.t.TempDir()
	report, err := application.TalkWait{
		Stream:        partsStream{steps: &steps},
		Postern:       f.postern,
		Cipher:        apptest.NewFakeCipher(),
		Keys:          stubPosternKeys{pubKey: talkWaitMayorKey},
		Memory:        f.tracker,
		GovernorKey:   talkWaitGovernorKey,
		AttachmentDir: dir,
		PartsWait:     partsWait,
		Limit:         5 * time.Second,
		MinBackoff:    5 * time.Millisecond,
		MaxBackoff:    5 * time.Millisecond,
		Out:           &out,
	}.Run(context.Background())
	if err != nil {
		f.t.Fatalf("the wait: %v", err)
	}
	return report, out.String(), dir
}

// AC1: parts 2 and 1 of a 3-part turn, then part 3, print as one turn, the
// texts joined in order, and the saved file holds all of it.
func TestTalkWaitJoinsThePartsOfATurnSentOutOfOrder(t *testing.T) {
	f := newTalkPosternFixture(t)
	report, printed, dir := f.runParts(time.Minute,
		func() int64 { return f.addPart("direct:p2", "two ", 2, 3) },
		func() int64 { return f.addPart("direct:p1", "one ", 1, 3) },
		func() int64 { return f.addPart("direct:p3", "three", 3, 3) },
	)

	if report.Turn == nil || report.Turn.Text != "one two three" {
		t.Fatalf("expected one turn, the three texts joined in order, got %+v", report.Turn)
	}
	if want := "text: one two three\n"; strings.Count(printed, "talk talk-7 turn 3") != 1 || !strings.Contains(printed, want) {
		t.Errorf("expected one turn printed with %q, got:\n%s", want, printed)
	}
	if strings.Contains(printed, "missing part") {
		t.Errorf("expected no missing part, got:\n%s", printed)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "direct-p2.txt"))
	if err != nil {
		// The first part to arrive names the file.
		t.Fatalf("expected the whole turn saved under the first part's txid: %v", err)
	}
	if string(saved) != "one two three" {
		t.Errorf("expected the saved file to hold all of the turn, got %q", saved)
	}
}

// AC2: a part still missing when the parts wait is over prints what came,
// and says which part is missing.
func TestTalkWaitSaysWhichPartIsMissingWhenThePartsWaitIsOver(t *testing.T) {
	f := newTalkPosternFixture(t)
	report, printed, dir := f.runParts(50*time.Millisecond,
		func() int64 { return f.addPart("direct:p1", "one ", 1, 3) },
		func() int64 { return f.addPart("direct:p2", "two ", 2, 3) },
	)

	if report.Turn == nil || report.Turn.Text != "one two " {
		t.Fatalf("expected the turn with what came, got %+v", report.Turn)
	}
	if !strings.Contains(printed, "text: one two \n") || !strings.Contains(printed, "missing part 3 of 3\n") {
		t.Errorf("expected what came and 'missing part 3 of 3', got:\n%s", printed)
	}
	if saved, _ := os.ReadFile(filepath.Join(dir, "direct-p1.txt")); string(saved) != "one two " {
		t.Errorf("expected what came saved, got %q", saved)
	}
}

// A wait ended by its own limit with a turn half heard still prints what came.
func TestTalkWaitPrintsAHalfHeardTurnWhenItsLimitComes(t *testing.T) {
	f := newTalkPosternFixture(t)
	var out strings.Builder
	steps := []func() int64{func() int64 { return f.addPart("direct:p2", "two", 2, 2) }}
	report, err := application.TalkWait{
		Stream: partsStream{steps: &steps}, Postern: f.postern, Cipher: apptest.NewFakeCipher(),
		Keys: stubPosternKeys{pubKey: talkWaitMayorKey}, Memory: f.tracker, GovernorKey: talkWaitGovernorKey,
		PartsWait: time.Minute, Limit: 100 * time.Millisecond, Out: &out,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Turn == nil || !strings.Contains(out.String(), "text: two\n") || !strings.Contains(out.String(), "missing part 1 of 2\n") {
		t.Errorf("expected the half heard turn and 'missing part 1 of 2', got:\n%s", out.String())
	}
}

// A turn with no part fields prints as it always did, and one that says it is
// part 1 of 1 prints the same.
func TestTalkWaitPrintsATurnWithNoPartsAsBefore(t *testing.T) {
	f := newTalkPosternFixture(t)
	seq := f.addTurn("whole turn")

	report, printed := f.run(seq)

	if report.Turn == nil || report.Turn.Text != "whole turn" || strings.Contains(printed, "missing part") {
		t.Errorf("expected the turn as it came, got %+v:\n%s", report.Turn, printed)
	}

	g := newTalkPosternFixture(t)
	one, printed, _ := g.runParts(time.Minute, func() int64 { return g.addPart("direct:solo", "alone", 1, 1) })
	if one.Turn == nil || one.Turn.Text != "alone" || strings.Contains(printed, "missing part") {
		t.Errorf("expected part 1 of 1 printed as a whole turn, got %+v:\n%s", one.Turn, printed)
	}
}

// AC3: every reader of a talk turn handles the parts of one: the wait's
// print, the file it saves, the cursor it keeps (its index of what it has read)
// and mw talk show. Each is given the three parts of a turn, out of order.
func TestEveryReaderOfATalkTurnHandlesItsParts(t *testing.T) {
	readers := []struct {
		name string
		// read runs the reader over the three parts and says what it got.
		read func(t *testing.T, f *talkPosternFixture, last int64, whole string) string
		want func(last int64, whole string) string
	}{
		{"the wait's print", func(t *testing.T, f *talkPosternFixture, last int64, _ string) string {
			_, printed, _ := f.runParts(time.Minute, func() int64 { return last })
			return printed
		}, func(_ int64, whole string) string { return "text: " + whole + "\n" }},
		{"the wait's saved file", func(t *testing.T, f *talkPosternFixture, last int64, _ string) string {
			_, _, dir := f.runParts(time.Minute, func() int64 { return last })
			saved, _ := os.ReadFile(filepath.Join(dir, "direct-p2.txt"))
			return string(saved)
		}, func(_ int64, whole string) string { return whole }},
		{"the wait's cursor", func(t *testing.T, f *talkPosternFixture, last int64, _ string) string {
			f.runParts(time.Minute, func() int64 { return last })
			note, _ := f.tracker.Note(context.Background(), application.TalkWaitCursorKey)
			return note
		}, func(last int64, _ string) string { return strconv.FormatInt(last, 10) }},
		{"mw talk show", func(t *testing.T, f *talkPosternFixture, _ int64, _ string) string {
			printed, err := f.show("talk-7", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(printed, "talk talk-7 turn 3"); n != 1 {
				t.Errorf("expected the turn shown once, not once a part, got %d:\n%s", n, printed)
			}
			return printed
		}, func(_ int64, whole string) string { return "text: " + whole + "\n" }},
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			f := newTalkPosternFixture(t)
			f.addPart("direct:p2", "two ", 2, 3)
			f.addPart("direct:p1", "one ", 1, 3)
			last := f.addPart("direct:p3", "three", 3, 3)

			got := reader.read(t, f, last, "one two three")

			if want := reader.want(last, "one two three"); !strings.Contains(got, want) {
				t.Errorf("expected %q, got:\n%s", want, got)
			}
		})
	}
}

// mw talk show says which part of a turn it could not find, as the wait does.
func TestTalkShowSaysWhichPartIsMissing(t *testing.T) {
	f := newTalkPosternFixture(t)
	f.addPart("direct:p1", "one ", 1, 3)
	f.addPart("direct:p3", "three", 3, 3)

	printed, err := f.show("talk-7", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(printed, "text: one three\n") || !strings.Contains(printed, "missing part 2 of 3\n") {
		t.Errorf("expected what came and 'missing part 2 of 3', got:\n%s", printed)
	}
}

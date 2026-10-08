package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A mill that grinds up to slots grists at once, with n grists waiting, the
// oldest first, and no story running. Its grinder answers like the small mill's.
func aMillWithSlots(t *testing.T, slots, grists int) (application.GristGrind, *apptest.FakePostern, *apptest.FakeGrinder, *apptest.FakeGristState, []*apptest.FakeGristLock) {
	t.Helper()
	mill, backend, grinder := aSmallMill(t)
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	keys := &fakeMillKey{made: true}
	millKey, _, _ := keys.PublicKey()
	for i := 2; i <= grists; i++ {
		sealer := &apptest.FakeCipher{From: phone}
		photo, _ := sealer.EncryptBytes(millKey, []byte("photo "+string(rune('0'+i))))
		hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, photo))
		plain, _ := json.Marshal(application.GristPlaintext{
			Grist: application.GristName{App: "cairn", Kind: "sweep", V: "1.1"}, Input: json.RawMessage(`{"place":"Top drawer"}`),
			Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: "image/webp"}},
		})
		ct, _ := sealer.Encrypt(millKey, string(plain))
		backend.AddRecord(application.PosternRecord{Txid: "direct:g" + string(rune('0'+i)), Class: application.GristClass, From: phone, To: millKey,
			Signer: phone, SignerApps: []string{"cairn"}, Ciphertext: ct})
	}
	locks := make([]*apptest.FakeGristLock, slots)
	for i := range locks {
		locks[i] = &apptest.FakeGristLock{}
	}
	mill.Grinding = locks[0]
	for _, lock := range locks[1:] {
		mill.MoreGrinding = append(mill.MoreGrinding, lock)
	}
	state := apptest.NewFakeGristState()
	mill.State = state
	grinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`), StopReason: "end_turn"}
	return mill, backend, grinder, state, locks
}

func enters(t *testing.T, entered <-chan struct{}, want int) {
	t.Helper()
	for i := 0; i < want; i++ {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatalf("expected %d grinds to start, %d did", want, i)
		}
	}
}

func cursorIs(t *testing.T, state *apptest.FakeGristState, want int64, when string) {
	t.Helper()
	if got, _ := state.Cursor(context.Background()); got != want {
		t.Fatalf("expected the cursor at %d %s, got %d", want, when, got)
	}
}

// With two slots and three grists, two grind at once and the third starts
// only once one of them has ended; the cursor stays before every grist whose
// grind is still running.
func TestGristGrindRunsTwoAtOnceAndQueuesTheThird(t *testing.T) {
	mill, backend, grinder, state, _ := aMillWithSlots(t, 2, 3)
	entered, hold := make(chan struct{}, 3), make(chan struct{})
	grinder.Entered, grinder.Hold = entered, hold

	type outcome struct {
		report application.GristReport
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		report, err := mill.Run(context.Background())
		done <- outcome{report, err}
	}()

	enters(t, entered, 2)
	select {
	case <-entered:
		t.Fatal("expected the third grind to wait for a slot, but it started")
	case <-time.After(300 * time.Millisecond):
	}
	cursorIs(t, state, 0, "while the first two grind")

	hold <- struct{}{}
	enters(t, entered, 1)
	cursorIs(t, state, 0, "while the third grinds and the second is still running")
	close(hold)

	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if r := got.report; r.Answered != 3 || r.Waiting != 0 {
		t.Fatalf("expected 3 answered and none waiting, got %+v", r)
	}
	if got := grinder.MostAtOnce(); got != 2 {
		t.Fatalf("expected at most 2 grinds at once, got %d", got)
	}
	if got := len(backend.Delivered()); got != 3 {
		t.Fatalf("expected 3 answers delivered, got %d", got)
	}
	cursorIs(t, state, 3, "once every grind has ended")
	if lines, _ := state.Lines(context.Background()); len(lines) != 3 {
		t.Fatalf("expected 3 lines in the record, got %d", len(lines))
	}
}

// One slot grinds as the mill always has: one at a time, in order.
func TestGristGrindWithOneSlotGrindsOneAtATime(t *testing.T) {
	mill, _, grinder, state, _ := aMillWithSlots(t, 1, 3)
	report, err := mill.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Answered != 3 || grinder.MostAtOnce() != 1 {
		t.Fatalf("expected 3 answered one at a time, got %+v with %d at once", report, grinder.MostAtOnce())
	}
	cursorIs(t, state, 3, "after the pass")
}

// A grist whose answer could not be recorded stays before the cursor, though a
// later grist's grind ended before it: the next pass reads from it again, and
// only it is ground again.
type appendFailsFor struct {
	*apptest.FakeGristState
	txid string
}

func (s appendFailsFor) Append(ctx context.Context, line application.GrindLine) error {
	if line.Txid == s.txid {
		return errors.New("the disk is full")
	}
	return s.FakeGristState.Append(ctx, line)
}

func TestGristGrindLeavesTheCursorBeforeAGristItCouldNotRecord(t *testing.T) {
	mill, _, _, state, _ := aMillWithSlots(t, 2, 3)
	mill.State = appendFailsFor{state, "direct:g1"}
	_, err := mill.Run(context.Background())
	if err == nil {
		t.Fatal("expected the pass to report that the record could not be written")
	}
	cursorIs(t, state, 0, "when the oldest grist's answer could not be recorded")
	lines, _ := state.Lines(context.Background())
	for _, line := range lines {
		if line.Txid == "direct:g1" {
			t.Fatal("expected the grist whose record could not be written to have none")
		}
	}

	// The next pass reads again from the grist left behind, and answers only
	// what the record does not hold (this one's photos went with its first answer).
	mill.State = state
	report, err := mill.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, handled := range report.Grist {
		for _, line := range lines {
			if handled.Txid == line.Txid {
				t.Fatalf("expected %s, already recorded, not answered again", handled.Txid)
			}
		}
	}
	if after, _ := state.Lines(context.Background()); len(after) != 3 {
		t.Fatalf("expected each of the 3 grists recorded once, got %d lines", len(after))
	}
	cursorIs(t, state, 3, "after the next pass")
}

// The mill's slots are its own limit: it reads no host cap, so every slot it
// has grinds at once (mw-gq6.303).
func TestGristGrindGrindsAsManyAtOnceAsItHasSlots(t *testing.T) {
	mill, _, grinder, _, _ := aMillWithSlots(t, 3, 3)
	grinder.Together = 3
	report, err := mill.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Answered != 3 || report.Waiting != 0 || grinder.MostAtOnce() != 3 {
		t.Fatalf("expected 3 answered, 3 at once, got %+v with %d at once", report, grinder.MostAtOnce())
	}
}

// A pass that finds no slot free, and none of its own grinds to wait for, stops
// as before: every grist waits and the cursor stays.
func TestGristGrindWaitsWhenEverySlotIsHeldElsewhere(t *testing.T) {
	mill, _, grinder, state, locks := aMillWithSlots(t, 2, 2)
	locks[0].Hold()
	locks[1].Hold()
	report, err := mill.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Waiting != 2 || report.WaitingWhy != "the mill is at its limit (2 of 2 grinds running)" || len(grinder.Calls()) != 0 {
		t.Fatalf("expected both grists waiting, got %+v", report)
	}
	cursorIs(t, state, 0, "with no grind run")
}

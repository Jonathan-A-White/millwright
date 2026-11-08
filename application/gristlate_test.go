package application_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A grist that arrives while a pass is grinding another is picked up by that
// pass at once and answered while the first still runs, so one slow model call
// never keeps a grist waiting for pickup while the mill has a slot free
// (mw-gq6.1048: the dispatch tick's pass found the follower's pass running,
// said so, and left the grist for a pass that began only when the slow call
// ended).
func TestGristGrindPicksUpAGristThatArrivesWhileAnotherGrinds(t *testing.T) {
	mill, backend, grinder, state, _ := aMillWithSlots(t, 2, 1)
	mill.PollEvery = 10 * time.Millisecond
	entered, hold := make(chan struct{}, 2), make(chan struct{})
	grinder.Entered, grinder.Hold = entered, hold

	done := make(chan error, 1)
	go func() {
		_, err := mill.Run(context.Background())
		done <- err
	}()
	enters(t, entered, 1)

	addGrist(t, backend, "direct:late")
	enters(t, entered, 1)
	hold <- struct{}{}

	deadline := time.After(5 * time.Second)
	for len(backend.Delivered()) < 1 {
		select {
		case <-deadline:
			t.Fatal("expected the grist that arrived during a grind to be answered while the first still runs, but nothing was delivered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case err := <-done:
		t.Fatalf("expected the pass to still be running the first grind, it ended: %v", err)
	default:
	}
	close(hold)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := len(backend.Delivered()); got != 2 {
		t.Fatalf("expected 2 answers delivered, got %d", got)
	}
	cursorIs(t, state, 2, "once both grinds have ended")
}

// addGrist adds a grist of the small mill's kind to the backend, as a phone
// sends it, with the given txid.
func addGrist(t *testing.T, backend *apptest.FakePostern, txid string) {
	t.Helper()
	const phone = "02466d7fcae563e5cb09a0d1870bb580344804617879a14949cf22285f1bae3f27"
	keys := &fakeMillKey{made: true}
	millKey, _, _ := keys.PublicKey()
	sealer := &apptest.FakeCipher{From: phone}
	photo, _ := sealer.EncryptBytes(millKey, []byte("photo "+txid))
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, photo))
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: "sweep", V: "1.1"}, Input: json.RawMessage(`{"place":"Top drawer"}`),
		Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: "image/webp"}},
	})
	ct, _ := sealer.Encrypt(millKey, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: txid, Class: application.GristClass, From: phone, To: millKey,
		Signer: phone, SignerApps: []string{"cairn"}, Ciphertext: ct})
}

// cutOffGrinder is a model that never answers: it ends when its call's timeout
// does, as the real harness is killed at it, or, so a test cannot hang, after
// stuck.
type cutOffGrinder struct {
	stuck   time.Duration
	timeout chan time.Duration
}

func (c *cutOffGrinder) Grind(ctx context.Context, call application.GrindCall) (application.SessionResult, error) {
	c.timeout <- call.Timeout
	select {
	case <-time.After(call.Timeout):
		return application.SessionResult{}, application.ErrGrindTimedOut
	case <-time.After(c.stuck):
		return application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`), StopReason: "end_turn"}, nil
	case <-ctx.Done():
		return application.SessionResult{}, ctx.Err()
	}
}

// A grind file may name how long its kind's call may run, shorter than the
// factory's ceiling: a call past it is cut, and the request is answered
// failed at once, not after the ceiling.
func TestGristGrindCutsACallPastTheKindsCutOff(t *testing.T) {
	mill, backend, _ := aSmallMill(t)
	grinds := mill.Grinds.(*apptest.FakeGrinds)
	grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/sweep.json", []byte(`{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],
		"model":"sonnet","effort":"low","instructions":"grinds/sweep.md","answerSchema":"schema.json","timeoutSeconds":1,
		"attachments":{"min":1,"max":4,"mime":["image/webp"],"maxBytes":1000}}`))
	grinder := &cutOffGrinder{stuck: 4 * time.Second, timeout: make(chan time.Duration, 4)}
	mill.Grinder = grinder
	mill.Ceilings.Timeout = 10 * time.Minute

	started := time.Now()
	report, err := mill.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Fatalf("expected the call cut at the kind's 1s, the pass took %s", took)
	}
	if got := <-grinder.timeout; got != time.Second {
		t.Fatalf("expected the call given the kind's 1s, got %s", got)
	}
	if report.Failed != 1 || report.Waiting != 0 || len(report.Grist) != 1 || report.Grist[0].Reason != application.GristReasonTimedOut {
		t.Fatalf("expected the request answered failed as timed out, got %+v", report)
	}
	delivered := backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected the answer delivered, got %d", len(delivered))
	}
	if answer := openFakeAnswer(t, delivered[0]); answer.Status != application.GristFailed || answer.Reason != application.GristReasonTimedOut {
		t.Fatalf("expected a failed answer, got %+v", answer)
	}
}

// A kind's cut-off longer than the factory's ceiling is held to the ceiling.
func TestGristGrindHoldsAKindsCutOffToTheCeiling(t *testing.T) {
	mill, _, _ := aSmallMill(t)
	grinds := mill.Grinds.(*apptest.FakeGrinds)
	grinds.SetFile("/rigs/cairn", "c0ffee", "grinds/sweep.json", []byte(`{"grind":1,"app":"cairn","kind":"sweep","versions":["1.1"],
		"model":"sonnet","effort":"low","instructions":"grinds/sweep.md","answerSchema":"schema.json","timeoutSeconds":3600,
		"attachments":{"min":1,"max":4,"mime":["image/webp"],"maxBytes":1000}}`))
	grinder := &apptest.FakeGrinder{Result: application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"Top drawer"}`), StopReason: "end_turn"}}
	mill.Grinder = grinder
	mill.Ceilings.Timeout = 5 * time.Minute
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := grinder.Calls()
	if len(calls) != 1 || calls[0].Call.Timeout != 5*time.Minute {
		t.Fatalf("expected one call held to the ceiling's 5m, got %+v", calls)
	}
}

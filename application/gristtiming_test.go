package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestAGrindLineLeavesOutAPhaseItDoesNotKnowAndKeepsAZeroItDoes(t *testing.T) {
	zero := 0.0
	encoded, err := json.Marshal(application.GrindLine{Txid: "direct:a", Seconds: 4.2, SlotSeconds: &zero})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if !strings.Contains(text, `"slot_s":0`) || !strings.Contains(text, `"seconds":4.2`) {
		t.Errorf("expected seconds and a slot_s of 0 in %s", text)
	}
	for _, absent := range []string{"wake_s", "pickup_s", "startup_s", "model_s", "deliver_s", "total_s"} {
		if strings.Contains(text, absent) {
			t.Errorf("%s is written though nobody measured it: %s", absent, text)
		}
	}
}

func TestGristTimingTakesTheMedianOfAnEvenCountAndTheNearestRankP90(t *testing.T) {
	state := &apptest.FakeGristState{}
	for i := 1; i <= 10; i++ {
		_ = state.Append(context.Background(), application.GrindLine{
			Time: time.Date(2026, 10, 7, 9, i, 0, 0, time.UTC), App: "lampas", Kind: "verse-read", Seconds: float64(i),
		})
	}
	var out bytes.Buffer
	groups, err := application.GristTiming{State: state, Out: &out}.Run(context.Background(), time.Time{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Rows) != 1 {
		t.Fatalf("expected one group with only the seconds row, got %+v", groups)
	}
	row := groups[0].Rows[0]
	if row.Count != 10 || row.Median != 5.5 || row.P90 != 9 {
		t.Errorf("expected 10, 5.5 and 9, got %+v", row)
	}
}

func TestGristTimingSaysSoWhenNothingFits(t *testing.T) {
	var out bytes.Buffer
	groups, err := application.GristTiming{State: &apptest.FakeGristState{}, Out: &out}.Run(context.Background(), time.Time{}, "lampas", "")
	if err != nil || len(groups) != 0 || !strings.Contains(out.String(), "no grinds") {
		t.Errorf("got %v, %v, %q", groups, err, out.String())
	}
}

// aGristSentAt puts a grist of one photo on the backend whose record is
// stamped sent: the backend's or phone's clock, not the mill host's.
func aGristSentAt(t *testing.T, mill application.GristGrind, backend *apptest.FakePostern, txid string, sent time.Time) {
	t.Helper()
	phone := &apptest.FakeCipher{From: gristPhoneKey}
	millKey := mustMillKey(t, mill)
	sealed, _ := phone.EncryptBytes(millKey, []byte("photo "+txid))
	hash, size, _ := backend.UploadBlob(context.Background(), mustBase64(t, sealed))
	plain, _ := json.Marshal(application.GristPlaintext{
		Grist: application.GristName{App: "cairn", Kind: "sweep", V: "1.1"}, Input: json.RawMessage(`{}`),
		Attachments: []application.PosternAttachment{{Hash: hash, Size: size, Mime: "image/webp"}},
	})
	ct, _ := phone.Encrypt(millKey, string(plain))
	backend.AddRecord(application.PosternRecord{Txid: txid, Class: application.GristClass, From: gristPhoneKey, To: millKey,
		Signer: gristPhoneKey, SignerApps: []string{"cairn"}, Ciphertext: ct, Ts: sent})
}

// Wake and total start at the record's Ts, the backend's clock: a Ts far from
// this host's clock, either way, is not a phase at all (mw-gq6.510).
func TestAGrindLineHasNoWakeOrTotalWhenTheBackendsClockIsFarOff(t *testing.T) {
	mill, backend, grinder := aSmallMill(t)
	mill.Ceilings = application.GristCeilings{DailyLimit: 10}
	grinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}
	now := mill.Now()
	notices := application.NewGristNotices()
	mill.Notices = notices
	for txid, ago := range map[string]time.Duration{"direct:late": 2 * time.Hour, "direct:near": 3 * time.Second, "direct:ahead": -2 * time.Hour} {
		aGristSentAt(t, mill, backend, txid, now.Add(-ago))
		notices.Note(txid, now)
	}
	if _, err := mill.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	lines, _ := mill.State.Lines(context.Background())
	byTxid := map[string]application.GrindLine{}
	for _, l := range lines {
		byTxid[l.Txid] = l
	}
	for _, txid := range []string{"direct:late", "direct:ahead"} {
		l, ok := byTxid[txid]
		if !ok || l.Status != application.GristAnswered {
			t.Fatalf("%s: expected an answered line, got %+v", txid, l)
		}
		if l.WakeSeconds != nil || l.TotalSeconds != nil {
			t.Errorf("%s: a record stamped off this host's clock has no wake or total, got wake %v total %v", txid, l.WakeSeconds, l.TotalSeconds)
		}
	}
	near := byTxid["direct:near"]
	if near.WakeSeconds == nil || *near.WakeSeconds != 3.0 {
		t.Errorf("a grist sent 3 s before it was noticed has a wake of 3.0, got %v", near.WakeSeconds)
	}
	if near.TotalSeconds == nil {
		t.Errorf("a grist sent 3 s before it was noticed has a total")
	}
}

// The queue wait is sent (backend's clock) to received (this host's): known
// only when it is not negative and not longer than any sender waits.
func TestTheQueueWaitOfAKeptRunIsLeftOutWhenTheClocksDisagree(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before time.Duration // Sent is this long before Received
		queued *float64
	}{
		{"five seconds", 5 * time.Second, f64(5)},
		{"phone clock ahead", -30 * time.Second, nil},
		{"two hours before", 2 * time.Hour, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mill, backend, grinder := aSmallMill(t)
			grinder.Result = application.SessionResult{Subtype: "success", Answer: json.RawMessage(`{"items":[],"placeName":"x"}`)}
			runs := &apptest.FakeGristRuns{}
			mill.Runs = runs
			mill.Ceilings = application.GristCeilings{DailyLimit: 10}
			aGristSentAt(t, mill, backend, "direct:q", mill.Now().Add(-tc.before))
			if _, err := mill.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			var kept application.GristRunTiming
			for _, k := range runs.Kept {
				if k.Txid == "direct:q" {
					kept = k
				}
			}
			if kept.Txid == "" {
				t.Fatalf("expected a kept run of direct:q, got %+v", runs.Kept)
			}
			if (kept.QueuedSeconds == nil) != (tc.queued == nil) || (tc.queued != nil && *kept.QueuedSeconds != *tc.queued) {
				t.Errorf("queued_s: want %v, got %v", tc.queued, kept.QueuedSeconds)
			}
			if tc.queued == nil && kept.EndToEnd() != kept.RunSeconds {
				t.Errorf("with no known queue the end to end is run_s %v, got %v", kept.RunSeconds, kept.EndToEnd())
			}
		})
	}
}

func f64(v float64) *float64 { return &v }

func TestAKeptQueuedSecondsOutOfBoundCountsNoQueueInEndToEnd(t *testing.T) {
	for _, queued := range []float64{-30, 7200, 5} {
		run := application.GristRunTiming{QueuedSeconds: &queued, RunSeconds: 20, Seconds: 20}
		want := 20.0
		if queued == 5 {
			want = 25
		}
		if got := run.EndToEnd(); got != want {
			t.Errorf("queued_s %v and run_s 20: end to end want %v, got %v", queued, want, got)
		}
	}
	// A run kept before queued_s existed is read from its times.
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	for _, ahead := range []time.Duration{-time.Hour, 2 * time.Hour, 5 * time.Second} {
		sent := at.Add(-ahead)
		run := application.GristRunTiming{Sent: &sent, Received: at, Seconds: 20}
		want := 20.0
		if ahead == 5*time.Second {
			want = 25
		}
		if got := run.EndToEnd(); got != want {
			t.Errorf("sent %v before received, no queued_s: end to end want %v, got %v", ahead, want, got)
		}
	}
}

func TestStatsLeaveASkewedRunOutOfTheQueueColumn(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	store := timingsStore{timings: []application.GristRunTiming{
		fixtureTiming("tutor-turn", at, 1, 2, 1, 2, 10),
		fixtureTiming("tutor-turn", at.Add(time.Minute), -7200, 2, 1, 2, 10),
		fixtureTiming("tutor-turn", at.Add(2*time.Minute), 3, 2, 1, 2, 10),
	}}
	rows, err := application.GristStats{Runs: store}.Run(context.Background(), "tutor-turn", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Phase == "queue" {
			if r.Count != 2 || r.Worst != 3 {
				t.Errorf("queue: want two values, worst 3, got %+v", r)
			}
			return
		}
	}
	t.Errorf("no queue row in %+v", rows)
}

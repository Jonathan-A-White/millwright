package application_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
)

// shipKeys signs a record "transaction" that is the payload in hex, so a test
// can read from the broadcasts what was put on chain.
type shipKeys struct{ stubPosternKeys }

func (shipKeys) Sign(_ []application.PosternUtxo, payload []byte) (string, error) {
	return hex.EncodeToString(payload), nil
}

// shipFixture is a shipper over fakes: a log, a backend whose chain road can
// be broken apart from its direct line, and a clock the test moves.
type shipFixture struct {
	log     *apptest.FakeEventLog
	backend *apptest.FakePostern
	state   *apptest.FakeShipStates
	cipher  *apptest.FakeCipher
	said    bytes.Buffer
	at      time.Time
	ship    *application.EventShip
}

func newShipFixture(t *testing.T) *shipFixture {
	t.Helper()
	f := &shipFixture{
		log:     &apptest.FakeEventLog{},
		backend: apptest.NewFakePostern(),
		state:   &apptest.FakeShipStates{},
		cipher:  apptest.NewFakeCipher(),
		at:      time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	f.ship = f.newShip()
	return f
}

// newShip is a shipper that has just started, over the same log, backend and
// state: what a restarted follower is.
func (f *shipFixture) newShip() *application.EventShip {
	return &application.EventShip{
		Log:               f.log,
		State:             f.state,
		Postern:           f.backend,
		Cipher:            f.cipher,
		Keys:              shipKeys{stubPosternKeys{pubKey: "mayor-key"}},
		GovernorKey:       "governor-key",
		Chain:             true,
		DailyCap:          500,
		EmergencyDailyCap: 20,
		Host:              "laptop",
		Now:               func() time.Time { return f.at },
		Err:               &f.said,
	}
}

// add appends n bead_changed events to the log.
func (f *shipFixture) add(t *testing.T, n int) {
	t.Helper()
	var evs []events.Event
	for i := 0; i < n; i++ {
		evs = append(evs, events.Event{Ts: f.at, Kind: events.KindBeadChanged, Bead: "b", Actor: "a", From: "open", To: "open", Lane: events.LaneNormal})
	}
	if _, err := f.log.Append(context.Background(), evs); err != nil {
		t.Fatal(err)
	}
}

// chainPayloads are the record payloads the backend was asked to broadcast, in order.
func (f *shipFixture) chainPayloads(t *testing.T) [][]byte {
	t.Helper()
	var out [][]byte
	for _, raw := range f.backend.Broadcasts() {
		payload, err := hex.DecodeString(raw)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, payload)
	}
	return out
}

func (f *shipFixture) run(t *testing.T) {
	t.Helper()
	if err := f.ship.Ship(context.Background()); err != nil {
		t.Fatalf("a pass ended with %v; said %q", err, f.said.String())
	}
}

// open reads a payload the shipper handed the backend: its envelope and the
// batch it seals.
func (f *shipFixture) open(t *testing.T, payload []byte) (application.PosternPayload, events.Batch) {
	t.Helper()
	var p application.PosternPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatalf("the payload is not JSON: %v", err)
	}
	plain, _, err := f.cipher.Decrypt("priv", p.Ct)
	if err != nil {
		t.Fatal(err)
	}
	var b events.Batch
	if err := json.Unmarshal([]byte(plain), &b); err != nil {
		t.Fatalf("the sealed text is not a batch: %v", err)
	}
	return p, b
}

func TestShipSealsOneBatchToTheGovernorAndSendsItOnChainAndDirect(t *testing.T) {
	f := newShipFixture(t)
	f.backend.NextTxid = "the-txid"
	f.add(t, 3)
	f.run(t)

	if got := len(f.backend.Broadcasts()); got != 1 {
		t.Fatalf("%d records on chain, want 1", got)
	}
	delivered := f.backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("%d records direct, want 1", len(delivered))
	}
	env, batch := f.open(t, delivered[0])
	if env.Class != application.EventsClass || env.Kind != application.PosternMessageKind || env.To != "governor-key" || env.From != "mayor-key" || env.Summary != "" {
		t.Fatalf("the envelope is %+v: want class events, kind msg, to the Governor, from the Mayor's key, no summary", env)
	}
	if batch.From != 1 || batch.To != 3 || batch.Lane != events.LaneNormal || len(batch.Events) != 3 {
		t.Fatalf("the batch is %d to %d in lane %s with %d events, want 1 to 3, normal, 3", batch.From, batch.To, batch.Lane, len(batch.Events))
	}
	if err := batch.Validate(); err != nil {
		t.Fatalf("the batch is not one the factory sends: %v", err)
	}
	if got := f.chainPayloads(t); len(got) != 1 || !bytes.Equal(got[0], delivered[0]) {
		t.Fatalf("the chain and direct records are not the same sealed bytes")
	}
	st, _ := f.state.Load(context.Background())
	if st.Shipped != 3 || len(st.Pending) != 0 || st.Chain != 1 {
		t.Fatalf("state after the pass is %+v, want shipped 3, nothing pending, 1 on chain", st)
	}

	f.run(t)
	if len(f.backend.Delivered()) != 1 || len(f.backend.Broadcasts()) != 1 {
		t.Fatalf("a pass with no new events sent again")
	}
}

func TestShipWithTheChainDownGoesDirectAsFallbackAndRebroadcastsWhenItReturns(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("whatsonchain is down")
	f.add(t, 2)
	f.run(t)

	if len(f.backend.Broadcasts()) != 0 {
		t.Fatalf("a broadcast got through a dead chain")
	}
	delivered := f.backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("%d records direct, want 1", len(delivered))
	}
	_, batch := f.open(t, delivered[0])
	if batch.Lane != events.LaneFallback || batch.From != 1 || batch.To != 2 {
		t.Fatalf("the direct batch is %d to %d in lane %s, want 1 to 2 in fallback", batch.From, batch.To, batch.Lane)
	}
	if err := batch.Validate(); err != nil {
		t.Fatalf("the fallback batch is not one the factory sends: %v", err)
	}
	st, _ := f.state.Load(context.Background())
	if st.Shipped != 2 || len(st.Pending) != 1 || st.Pending[0] != (application.ShipRange{From: 1, To: 2}) {
		t.Fatalf("state is %+v, want shipped 2 and 1 to 2 pending", st)
	}

	f.at = f.at.Add(time.Second)
	f.run(t) // still down: the retry fails and nothing else is sent
	if len(f.backend.Delivered()) != 1 || len(f.backend.Broadcasts()) != 0 {
		t.Fatalf("a failed retry sent something")
	}

	f.backend.ChainErr = nil
	f.at = f.at.Add(time.Second)
	f.run(t)
	chain := f.chainPayloads(t)
	if len(chain) != 1 {
		t.Fatalf("%d records on chain after the chain returned, want 1", len(chain))
	}
	_, again := f.open(t, chain[0])
	if again.Lane != events.LaneNormal || again.From != 1 || again.To != 2 || len(again.Events) != 2 {
		t.Fatalf("the re-broadcast batch is %d to %d in lane %s, want 1 to 2 in normal", again.From, again.To, again.Lane)
	}
	if len(f.backend.Delivered()) != 1 {
		t.Fatalf("the re-broadcast went direct a second time")
	}
	st, _ = f.state.Load(context.Background())
	if len(st.Pending) != 0 || st.Chain != 1 {
		t.Fatalf("state is %+v, want nothing pending and 1 on chain", st)
	}
}

func TestShipRetriesPendingBatchesOldestFirstBeforeSendingTheNewOne(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("down")
	f.add(t, 1)
	f.run(t)
	f.at = f.at.Add(3 * time.Second)
	f.add(t, 2)
	f.run(t)
	st, _ := f.state.Load(context.Background())
	if len(st.Pending) != 2 {
		t.Fatalf("pending is %+v, want two batches", st.Pending)
	}

	f.backend.ChainErr = nil
	f.at = f.at.Add(3 * time.Second)
	f.add(t, 1)
	f.run(t)
	var seen []uint64
	for _, p := range f.chainPayloads(t) {
		_, b := f.open(t, p)
		seen = append(seen, b.From)
	}
	if len(seen) != 3 || seen[0] != 1 || seen[1] != 2 || seen[2] != 4 {
		t.Fatalf("the chain got batches starting at %v, want 1, 2, then the new 4", seen)
	}
}

func TestShipWithChainOffNeverTouchesTheChain(t *testing.T) {
	f := newShipFixture(t)
	f.ship.Chain = false
	f.add(t, 2)
	f.run(t)

	if f.backend.ChainCalls() != 0 {
		t.Fatalf("the chain was called %d times with chain = false", f.backend.ChainCalls())
	}
	delivered := f.backend.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("%d records direct, want 1", len(delivered))
	}
	if _, b := f.open(t, delivered[0]); b.Lane != events.LaneFallback {
		t.Fatalf("the lane is %s, want fallback", b.Lane)
	}
	if st, _ := f.state.Load(context.Background()); len(st.Pending) != 0 || st.Shipped != 2 {
		t.Fatalf("state is %+v: with the chain off nothing waits for it", st)
	}
}

func TestShipPastTheDailyCapGoesFallbackAndSaysSoOnce(t *testing.T) {
	f := newShipFixture(t)
	f.ship.DailyCap = 2
	for i := 0; i < 5; i++ {
		f.add(t, 1)
		f.run(t)
		f.at = f.at.Add(3 * time.Second)
	}

	if got := len(f.backend.Broadcasts()); got != 2 {
		t.Fatalf("%d records on chain with a cap of 2", got)
	}
	var lanes []string
	for _, p := range f.backend.Delivered() {
		_, b := f.open(t, p)
		lanes = append(lanes, b.Lane)
	}
	if got := strings.Join(lanes, " "); !strings.HasPrefix(got, "normal normal fallback") || strings.Count(got, "fallback") != len(lanes)-2 {
		t.Fatalf("the lanes were %q: want two normal, then fallback", got)
	}
	var alarms int
	for _, e := range f.log.All() {
		if e.Kind == events.KindJob && e.To == events.JobFailed {
			alarms++
			if !strings.Contains(e.Detail, "2") || e.Actor != "events-follow@laptop" {
				t.Fatalf("the alarm event is %+v: want actor events-follow@laptop and the cap in its detail", e)
			}
		}
	}
	if alarms != 1 {
		t.Fatalf("%d alarm events, want exactly 1", alarms)
	}
	st, _ := f.state.Load(context.Background())
	if st.Chain != 2 || len(st.Pending) == 0 {
		t.Fatalf("state is %+v, want 2 on chain and the over-cap batches pending", st)
	}

	// The next UTC day the count starts again: the oldest pending batches go
	// on chain, up to the cap again.
	before := len(st.Pending)
	f.at = f.at.Add(24 * time.Hour)
	f.run(t)
	st, _ = f.state.Load(context.Background())
	if len(st.Pending) != before-2 || st.Chain != 2 || len(f.backend.Broadcasts()) != 4 {
		t.Fatalf("state is %+v after the day turned, want %d pending, 2 on chain today and 4 in all", st, before-2)
	}
}

func TestShipHoldsABurstToOneBatchPerWindowAndSendsFiftyAtOnce(t *testing.T) {
	f := newShipFixture(t)
	f.add(t, 1)
	f.run(t) // the first goes at once
	f.at = f.at.Add(time.Second)
	f.add(t, 1)
	f.run(t)
	if len(f.backend.Delivered()) != 1 {
		t.Fatalf("a second batch went within the 2 s window")
	}
	f.at = f.at.Add(time.Second)
	f.run(t) // two seconds since the last send, and no new events needed
	if len(f.backend.Delivered()) != 2 {
		t.Fatalf("the event waiting at the end of the window did not go")
	}

	f.at = f.at.Add(100 * time.Millisecond)
	f.add(t, 120)
	f.run(t) // 50 at once, inside the window
	f.run(t) // and 50 more
	f.run(t) // 20 are left: they wait for the window
	if len(f.backend.Delivered()) != 4 {
		t.Fatalf("%d records direct, want 2 + 50 + 50 and the last 20 held", len(f.backend.Delivered()))
	}
	f.at = f.at.Add(2 * time.Second)
	f.run(t)
	delivered := f.backend.Delivered()
	if len(delivered) != 5 {
		t.Fatalf("%d records direct, want 2 + 3 (50, 50, 20) for 120 events", len(delivered))
	}
	for i, want := range []int{50, 50, 20} {
		if _, b := f.open(t, delivered[2+i]); len(b.Events) != want {
			t.Fatalf("batch %d holds %d events, want %d", i, len(b.Events), want)
		}
	}
}

func TestShipLeavesTheBatchWhenNothingTookItAndSendsItNextPass(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("chain down")
	f.backend.DeliverErr = errors.New("backend down")
	f.add(t, 1)
	if err := f.ship.Ship(context.Background()); err == nil {
		t.Fatalf("a pass nothing took was no error")
	}
	if st, _ := f.state.Load(context.Background()); st.Shipped != 0 || len(st.Pending) != 0 {
		t.Fatalf("state is %+v: the batch was not sent, so it is neither shipped nor pending", st)
	}
	f.backend.DeliverErr = nil
	f.at = f.at.Add(time.Second)
	f.run(t)
	if len(f.backend.Delivered()) != 1 {
		t.Fatalf("the batch did not go once the backend was back")
	}
}

func TestShipPicksUpWhereAFollowerLeftOff(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("down")
	f.add(t, 2)
	f.run(t)
	f.add(t, 1)
	restarted := f.newShip()
	f.backend.ChainErr = nil
	f.at = f.at.Add(5 * time.Second)
	if err := restarted.Ship(context.Background()); err != nil {
		t.Fatal(err)
	}
	chain := f.chainPayloads(t)
	if len(chain) != 2 {
		t.Fatalf("%d records on chain, want the pending batch and the new one", len(chain))
	}
	if _, b := f.open(t, chain[0]); b.From != 1 || b.To != 2 || b.Lane != events.LaneNormal {
		t.Fatalf("a restarted follower sent %d to %d first, want the pending 1 to 2", b.From, b.To)
	}
	if len(f.backend.Delivered()) != 2 {
		t.Fatalf("%d records direct, want the first batch and then 3 alone", len(f.backend.Delivered()))
	}
}

func TestShipStatusSaysHeadShippedPendingAndTodaysChainCount(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("down")
	f.add(t, 2)
	f.run(t)
	f.backend.ChainErr = nil
	f.at = f.at.Add(3 * time.Second)
	f.add(t, 1)
	f.ship.DailyCap = 7
	got, err := f.ship.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != 3 || got.Shipped != 2 || len(got.Pending) != 1 || got.ChainToday != 0 || got.Cap != 7 || !got.Chain {
		t.Fatalf("status is %+v", got)
	}
	line := got.Line()
	for _, want := range []string{"head 3", "shipped 2", "pending 1", "chain 0/7"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the status line %q lacks %q", line, want)
		}
	}
	f.ship.Chain = false
	if off, _ := f.ship.Status(context.Background()); !strings.Contains(off.Line(), "chain off") {
		t.Fatalf("with the chain off the line is %q", off.Line())
	}
	if len(line) > application.Width {
		t.Fatalf("the status line is %d wide, past %d", len(line), application.Width)
	}
}

func TestShipCutsABatchThatWouldOverrunTheRecordAndSendsTheRestNext(t *testing.T) {
	f := newShipFixture(t)
	big := strings.Repeat("x", 600)
	var evs []events.Event
	for i := 0; i < 30; i++ {
		evs = append(evs, events.Event{Ts: f.at, Kind: events.KindBeadChanged, Bead: "b", Actor: "a", From: "open", To: "open", Detail: big, Lane: events.LaneNormal})
	}
	if _, err := f.log.Append(context.Background(), evs); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	f.at = f.at.Add(application.ShipWindow)
	f.run(t)
	delivered := f.backend.Delivered()
	if len(delivered) < 2 {
		t.Fatalf("%d records direct, want the batch cut in more than one", len(delivered))
	}
	_, first := f.open(t, delivered[0])
	_, second := f.open(t, delivered[1])
	if len(delivered[0]) > 10240 || len(first.Events) >= 30 || second.From != first.To+1 {
		t.Fatalf("first holds %d events in %d bytes, second starts at %d after %d", len(first.Events), len(delivered[0]), second.From, first.To)
	}
}

// countingShipper counts the passes it is called on, failing when told to.
type countingShipper struct {
	calls int
	err   error
}

func (c *countingShipper) Ship(context.Context) error { c.calls++; return c.err }

func TestFollowCallsItsShipperOnEveryPassEvenWhenTheHeadHasNotMoved(t *testing.T) {
	ship := &countingShipper{err: errors.New("nothing took it")}
	var errs bytes.Buffer
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	head := &apptest.FakeHead{Heads: []string{"a"}}
	follow := application.EventFollow{
		Head:    head,
		Publish: func(context.Context) error { return nil },
		Shipper: ship,
		Sleep: func(context.Context, time.Duration) error {
			if head.Calls() >= 4 {
				stop()
				return ctx.Err()
			}
			return nil
		},
		Err: &errs,
	}
	if err := follow.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if ship.calls != 4 {
		t.Fatalf("the shipper was called %d times over 4 passes", ship.calls)
	}
	if n := strings.Count(errs.String(), "nothing took it"); n != 1 {
		t.Fatalf("the shipper's failure was said %d times, want once:\n%s", n, errs.String())
	}
}

func TestStatusShowsTheFollowersHeadPendingBatchesAndTodaysChainCount(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("down")
	f.add(t, 4)
	f.run(t)
	tracker := apptest.NewFakeTracker()
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder", Events: f.ship}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Events == nil || !strings.Contains(report.String(), "EVENTS head 4 shipped 4 pending 1 chain 0/500\n") {
		t.Fatalf("expected the EVENTS line in the report, got:\n%s", report.String())
	}
	bare, err := application.Status{Tracker: tracker, Notes: tracker, Host: "laptop", Seat: "builder"}.Run(context.Background())
	if err != nil || strings.Contains(bare.String(), "EVENTS") {
		t.Fatalf("a report with no shipper named has no EVENTS line, got:\n%s %v", bare.String(), err)
	}
}

// addEmergency appends one emergency-lane event, as mw events emit --emergency does.
func (f *shipFixture) addEmergency(t *testing.T) {
	t.Helper()
	ev := events.Event{Ts: f.at, Kind: events.KindJob, Actor: "doctor@laptop", From: events.JobRunning, To: events.JobFailed, Detail: "mayor-stale", Lane: events.LaneEmergency}
	if _, err := f.log.Append(context.Background(), []events.Event{ev}); err != nil {
		t.Fatal(err)
	}
}

func TestShipSendsAnEmergencyAloneAndAtOnceOnBothRoadsInsideTheWindow(t *testing.T) {
	f := newShipFixture(t)
	f.add(t, 1)
	f.run(t) // a normal batch: the 2 s window is now shut
	f.at = f.at.Add(100 * time.Millisecond)
	f.add(t, 1)
	f.addEmergency(t)
	f.add(t, 1)
	f.run(t)

	chain, direct := f.chainPayloads(t), f.backend.Delivered()
	if len(chain) != 2 || len(direct) != 2 {
		t.Fatalf("%d records on chain and %d direct, want the first batch and the emergency on each", len(chain), len(direct))
	}
	for name, payload := range map[string][]byte{"chain": chain[1], "direct": direct[1]} {
		_, b := f.open(t, payload)
		if b.Lane != events.LaneEmergency || b.From != 3 || b.To != 3 || len(b.Events) != 1 {
			t.Fatalf("the %s record is %d to %d in lane %s with %d events, want 3 to 3 in emergency", name, b.From, b.To, b.Lane, len(b.Events))
		}
		if err := b.Validate(); err != nil {
			t.Fatalf("the %s emergency is not a batch the factory sends: %v", name, err)
		}
	}
	st, _ := f.state.Load(context.Background())
	if st.Shipped != 1 || st.Emergency != 1 {
		t.Fatalf("state is %+v, want shipped 1 (the window holds events 2 and 4) and 1 emergency", st)
	}

	// The window opens: the batch holds the events around the emergency, never it twice.
	f.at = f.at.Add(3 * time.Second)
	f.run(t)
	f.run(t)
	var seen []string
	for _, p := range f.chainPayloads(t)[2:] {
		_, b := f.open(t, p)
		seen = append(seen, strings.Join([]string{b.Lane, string(rune('0' + b.From)), string(rune('0' + b.To))}, " "))
	}
	if strings.Join(seen, ",") != "normal 2 2" {
		t.Fatalf("after the window the chain got %v, want normal 2 2 first", seen)
	}
}

// A push is decided on the clear copy of the lane (postern docs/protocol.md
// section 1): the emergency record carries it beside class, and no other does.
func TestShipPutsTheEmergencyLaneInTheClearAndNoOtherLane(t *testing.T) {
	f := newShipFixture(t)
	f.add(t, 1)
	f.run(t) // a normal batch
	f.at = f.at.Add(100 * time.Millisecond)
	f.addEmergency(t)
	f.run(t)

	chain, direct := f.chainPayloads(t), f.backend.Delivered()
	if len(chain) != 2 || len(direct) != 2 {
		t.Fatalf("%d records on chain and %d direct, want the normal batch and the emergency on each", len(chain), len(direct))
	}
	for name, payloads := range map[string][][]byte{"chain": chain, "direct": direct} {
		var normal, emergency map[string]any
		if err := json.Unmarshal(payloads[0], &normal); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(payloads[1], &emergency); err != nil {
			t.Fatal(err)
		}
		if _, has := normal["lane"]; has {
			t.Errorf("the %s normal batch carries a clear lane %v, want none", name, normal["lane"])
		}
		if emergency["lane"] != "emergency" {
			t.Errorf("the %s emergency's clear lane is %v, want \"emergency\"", name, emergency["lane"])
		}
	}
}

func TestShipSendsTheEmergencyBeforePendingFallbackBatches(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("down")
	f.add(t, 1)
	f.run(t)
	f.backend.ChainErr = nil
	f.at = f.at.Add(10 * time.Millisecond)
	f.addEmergency(t)
	f.run(t)

	var lanes []string
	for _, p := range f.chainPayloads(t) {
		_, b := f.open(t, p)
		lanes = append(lanes, b.Lane)
	}
	if len(lanes) != 2 || lanes[0] != events.LaneEmergency || lanes[1] != events.LaneNormal {
		t.Fatalf("the chain got lanes %v, want the emergency then the pending batch re-sent as normal", lanes)
	}
	if direct := f.backend.Delivered(); len(direct) != 2 {
		t.Fatalf("%d records direct, want the fallback batch and the emergency", len(direct))
	}
}

func TestShipEmergencyWithTheChainDownGoesDirectAndIsPutOnChainLater(t *testing.T) {
	f := newShipFixture(t)
	f.backend.ChainErr = errors.New("down")
	f.addEmergency(t)
	f.run(t)
	direct := f.backend.Delivered()
	if len(direct) != 1 {
		t.Fatalf("%d records direct, want the emergency", len(direct))
	}
	if _, b := f.open(t, direct[0]); b.Lane != events.LaneEmergency || b.From != 1 || b.To != 1 {
		t.Fatalf("the direct record is %d to %d in lane %s, want 1 to 1 in emergency", b.From, b.To, b.Lane)
	}
	st, _ := f.state.Load(context.Background())
	if st.Shipped != 1 || len(st.Pending) != 1 {
		t.Fatalf("state is %+v, want shipped 1 and the emergency pending for the chain", st)
	}
	f.backend.ChainErr = nil
	f.at = f.at.Add(time.Second)
	f.run(t)
	if chain := f.chainPayloads(t); len(chain) != 1 || len(f.backend.Delivered()) != 1 {
		t.Fatalf("%d on chain, %d direct, want the one retry on chain only", len(chain), len(f.backend.Delivered()))
	}
}

func TestShipEmergencyIgnoresTheDailyCapAndLeavesWhenNoRoadTookIt(t *testing.T) {
	f := newShipFixture(t)
	f.ship.DailyCap = 0
	f.addEmergency(t)
	f.run(t)
	if len(f.chainPayloads(t)) != 1 {
		t.Fatalf("an emergency waited on the daily cap")
	}

	g := newShipFixture(t)
	g.backend.DeliverErr = errors.New("backend down")
	g.backend.ChainErr = errors.New("chain down")
	g.addEmergency(t)
	if err := g.ship.Ship(context.Background()); err == nil {
		t.Fatal("expected an error when no road took the emergency")
	}
	g.backend.DeliverErr, g.backend.ChainErr = nil, nil
	g.run(t)
	if len(g.backend.Delivered()) != 1 {
		t.Fatalf("the emergency was not sent next pass")
	}
}

func TestShipStatusCountsTodaysEmergenciesAndStartsAgainTomorrow(t *testing.T) {
	f := newShipFixture(t)
	f.addEmergency(t)
	f.run(t)
	f.addEmergency(t)
	f.run(t)
	got, err := f.ship.Status(context.Background())
	if err != nil || got.EmergencyToday != 2 {
		t.Fatalf("status is %+v (%v), want 2 emergencies today", got, err)
	}
	if line := got.Line(); !strings.Contains(line, "emergency 2") || len(line) > application.Width {
		t.Fatalf("the status line %q should say emergency 2 within %d wide", line, application.Width)
	}
	f.at = f.at.Add(24 * time.Hour)
	if got, _ := f.ship.Status(context.Background()); got.EmergencyToday != 0 || strings.Contains(got.Line(), "emergency") {
		t.Fatalf("tomorrow's status is %+v / %q, want no emergencies", got, got.Line())
	}
}

func TestShipTheTwentyFirstEmergencyOfADayGoesDirectOnlyAndSaysSo(t *testing.T) {
	f := newShipFixture(t)
	for i := 0; i < 21; i++ {
		f.addEmergency(t)
	}
	f.run(t)
	if chain := f.chainPayloads(t); len(chain) != 20 {
		t.Fatalf("%d emergencies on chain, want the 20 the allowance holds", len(chain))
	}
	if direct := f.backend.Delivered(); len(direct) != 21 {
		t.Fatalf("%d emergencies direct, want all 21", len(direct))
	}
	if said := f.said.String(); !strings.Contains(said, "emergency daily cap of 20") || !strings.Contains(said, "event 21") {
		t.Fatalf("the shipper said %q, want that emergency 21 went direct only past the cap of 20", said)
	}
	st, _ := f.state.Load(context.Background())
	if st.Shipped != 21 || len(st.Pending) != 0 || st.Chain != 0 {
		t.Fatalf("state is %+v, want shipped 21, nothing pending for the chain and no ordinary chain record counted", st)
	}
	f.at = f.at.Add(24 * time.Hour)
	f.addEmergency(t)
	f.run(t)
	if chain := f.chainPayloads(t); len(chain) != 21 {
		t.Fatalf("%d on chain after the day turned, want the new day's emergency on chain too", len(chain))
	}
}

func TestShipEmergenciesPastTheOrdinaryCapDoNotStarveTheOrdinaryRetries(t *testing.T) {
	f := newShipFixture(t)
	f.ship.EmergencyDailyCap = 1000
	f.backend.ChainErr = errors.New("down")
	f.add(t, 1)
	f.run(t)
	if st, _ := f.state.Load(context.Background()); len(st.Pending) != 1 {
		t.Fatalf("state is %+v, want the batch pending for the chain", st)
	}
	f.backend.ChainErr = nil
	for i := 0; i < 501; i++ {
		f.addEmergency(t)
	}
	f.at = f.at.Add(time.Second)
	f.run(t)
	st, _ := f.state.Load(context.Background())
	if len(st.Pending) != 0 {
		t.Fatalf("state is %+v: 501 emergencies on chain starved the ordinary retry", st)
	}
	if st.Chain != 1 {
		t.Fatalf("%d ordinary chain records counted, want only the retried batch", st.Chain)
	}
}

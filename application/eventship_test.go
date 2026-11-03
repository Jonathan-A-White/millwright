package application_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
		ChainRoad:         apptest.NewFakeChain(f.backend, shipKeys{stubPosternKeys{pubKey: "mayor-key"}}),
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
	if len(st.Pending) != 1 || st.Pending[0] != (application.ShipRange{From: 1, To: 3}) {
		t.Fatalf("pending is %+v, want the two batches kept as one range, 1 to 3", st.Pending)
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
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 4 {
		t.Fatalf("the chain got batches starting at %v, want 1 (the range, in one record), then the new 4", seen)
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

	// The three over-cap batches are one range, which goes on chain the next
	// UTC day, when the count starts again.
	if len(st.Pending) != 1 {
		t.Fatalf("pending is %v, want the three over-cap batches as one range", st.Pending)
	}
	f.at = f.at.Add(24 * time.Hour)
	f.run(t)
	st, _ = f.state.Load(context.Background())
	if len(st.Pending) != 0 || st.Chain != 1 || len(f.backend.Broadcasts()) != 3 {
		t.Fatalf("state is %+v after the day turned, want nothing pending, 1 on chain today and 3 in all", st)
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

// hangingChain is a chain road that never answers: every Send waits until the
// caller gives up, as a Postern backend does at a load of 37 on 20 cores
// (2026-11-02, mw-gq6.893).
type hangingChain struct {
	application.Chain
	entered chan struct{}
}

func (h hangingChain) Send(ctx context.Context, _ []byte) (string, error) {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return "", ctx.Err()
}

// hangingPostern is a backend whose direct line never answers either.
type hangingPostern struct{ application.Postern }

func (hangingPostern) Deliver(ctx context.Context, _ []byte) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// A follower whose shipper is stuck on a pending batch (the first 40 to 120
// minutes of every UTC day, 2026-11-02) still writes the beads' events, nudges
// the seats and springs the jobs within one normal cycle.
func TestFollowKeepsWritingEventsNudgingAndSpringingWhileTheShipperHangsOnAPendingBatch(t *testing.T) {
	f := newShipFixture(t)
	f.add(t, 3)
	if err := f.state.Save(context.Background(), application.ShipState{
		Shipped: 3, Day: "2026-10-01",
		Pending: []application.ShipRange{{From: 1, To: 1}, {From: 2, To: 3}},
	}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 1)
	f.ship.ChainRoad = hangingChain{entered: entered}
	f.ship.Postern = hangingPostern{f.backend}

	w := newEventsWorld()
	w.log = f.log
	nudger, springer := &countingNudger{}, &countingSpringer{}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	head := &apptest.FakeHead{Heads: []string{"a", "b"}}
	var sent bool
	follow := application.EventFollow{
		Head: head, Feed: w.tracker, Log: f.log, Cursors: w.cursors, Now: w.now,
		Shipper: f.ship, Nudger: nudger, Springer: springer, Aside: true,
		Publish: func(context.Context) error { return nil },
		Sleep: func(context.Context, time.Duration) error {
			if !sent {
				select {
				case <-entered: // the shipper is now stuck in the chain
				case <-ctx.Done():
				}
				w.tracker.AddBeadChange(application.BeadChange{
					Key: "e1", At: w.at(1), Actor: "mw@laptop", What: "closed",
					Bead: application.BeadNow{ID: "mw-1", Type: "task", Status: application.StatusClosed},
				})
				sent = true
			}
			if strings.Contains(kindsOf(f.log.All()), "mw-1:open->closed") && nudger.calls >= 3 && springer.calls >= 3 {
				stop()
				return ctx.Err()
			}
			time.Sleep(time.Millisecond)
			return ctx.Err()
		},
	}
	if err := follow.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := kindsOf(f.log.All()); !strings.Contains(got, "mw-1:open->closed") {
		t.Fatalf("with the shipper stuck on a pending batch the log holds %q: the bead's event was never written", got)
	}
	if nudger.calls < 3 || springer.calls < 3 {
		t.Fatalf("with the shipper stuck the loop nudged %d and sprung %d times, want 3 or more of each", nudger.calls, springer.calls)
	}
}

// slowChain spends d of the fixture's clock on every send, as a chain road
// answering at 4 to 13 sends a minute does.
type slowChain struct {
	application.Chain
	f *shipFixture
	d time.Duration
}

func (c slowChain) Send(ctx context.Context, payload []byte) (string, error) {
	c.f.at = c.f.at.Add(c.d)
	return c.Chain.Send(ctx, payload)
}

// seqRangesOf are the seq ranges of the records on chain, in the order they
// were put there.
func (f *shipFixture) seqRangesOnChain(t *testing.T) []application.ShipRange {
	t.Helper()
	var out []application.ShipRange
	for _, payload := range f.chainPayloads(t) {
		_, b := f.open(t, payload)
		out = append(out, application.ShipRange{From: b.From, To: b.To})
	}
	return out
}

// A long backlog does not hold a pass: it puts what its budget allows on
// chain, sends the new events direct all the same, and the next passes, over a
// restart too, put every pending batch on chain once and in order.
func TestShipSpendsABudgetOnTheBacklogAndSendsTheNewEventsDirectAllTheSame(t *testing.T) {
	f := newShipFixture(t)
	f.add(t, 40)
	var pending []application.ShipRange
	for n := uint64(1); n <= 40; n++ {
		pending = append(pending, application.ShipRange{From: n, To: n})
	}
	if err := f.state.Save(context.Background(), application.ShipState{Shipped: 40, Day: "2026-10-01", Pending: pending}); err != nil {
		t.Fatal(err)
	}
	f.ship.ChainRoad = slowChain{Chain: f.ship.ChainRoad, f: f, d: 6 * time.Second}

	f.add(t, 1) // seq 41, new
	f.run(t)
	onChain := f.seqRangesOnChain(t)
	if len(onChain) == 0 || len(onChain) >= 40 {
		t.Fatalf("one pass put %d of 40 pending batches on chain, want some but not all: %v", len(onChain), onChain)
	}
	if delivered := f.backend.Delivered(); len(delivered) != 1 {
		t.Fatalf("the new event was delivered %d times in the pass, want once, direct", len(delivered))
	} else if _, b := f.open(t, delivered[0]); b.From != 41 || b.Lane != events.LaneFallback {
		t.Fatalf("the pass delivered events %d to %d in lane %s, want 41 to 41 in the fallback lane", b.From, b.To, b.Lane)
	}

	for pass := 0; pass < 40; pass++ {
		if pass == 1 {
			f.ship = f.newShip() // a restart
			f.ship.ChainRoad = slowChain{Chain: f.ship.ChainRoad, f: f, d: 6 * time.Second}
		}
		f.at = f.at.Add(3 * time.Second)
		f.run(t)
	}
	var want []application.ShipRange
	for n := uint64(1); n <= 41; n++ {
		want = append(want, application.ShipRange{From: n, To: n})
	}
	got := f.seqRangesOnChain(t)
	if len(got) != len(want) {
		t.Fatalf("%d records reached the chain, want %d, each once: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d on chain is events %d to %d, want %d to %d (oldest first, none twice)", i, got[i].From, got[i].To, want[i].From, want[i].To)
		}
	}
}

// countingStates is the fixture's state with every Save counted.
type countingStates struct {
	application.ShipStates
	saves int
}

func (c *countingStates) Save(ctx context.Context, s application.ShipState) error {
	c.saves++
	return c.ShipStates.Save(ctx, s)
}

// addBig appends n events of about size bytes of detail each.
func (f *shipFixture) addBig(t *testing.T, n, size int) {
	t.Helper()
	big := strings.Repeat("x", size)
	var evs []events.Event
	for i := 0; i < n; i++ {
		evs = append(evs, events.Event{Ts: f.at, Kind: events.KindBeadChanged, Bead: "b", Actor: "a", From: "open", To: "open", Detail: big, Lane: events.LaneNormal})
	}
	if _, err := f.log.Append(context.Background(), evs); err != nil {
		t.Fatal(err)
	}
}

// restart is a follower that has just started over the same log, backend and
// state, with the cap the test had set.
func (f *shipFixture) restart() {
	cap := f.ship.DailyCap
	f.ship = f.newShip()
	f.ship.DailyCap = cap
}

// A pass over the cap puts its batch in the last pending range when that range
// ends where the batch starts, so the backlog does not grow by a range a pass
// (mw-gq6.895); the ranges pending before the change are left as they are.
func TestShipPassOverTheCapJoinsItsBatchToTheLastPendingRangeAndLeavesOldRangesAlone(t *testing.T) {
	f := newShipFixture(t)
	f.ship.DailyCap = 0
	f.add(t, 2)
	old := []application.ShipRange{{From: 1, To: 1}, {From: 2, To: 2}}
	if err := f.state.Save(context.Background(), application.ShipState{Shipped: 2, Day: "2026-10-01", Pending: old}); err != nil {
		t.Fatal(err)
	}
	f.restart()
	for i := 0; i < 4; i++ {
		f.at = f.at.Add(3 * time.Second)
		f.add(t, 1)
		f.run(t)
	}
	st, _ := f.state.Load(context.Background())
	// the cap alarm is an event of the log too: seqs 3, 5, 6, 7 are the new ones
	want := []application.ShipRange{{From: 1, To: 1}, {From: 2, To: 2}, {From: 3, To: 7}}
	if len(st.Pending) != len(want) {
		t.Fatalf("pending is %v after four passes over the cap, want %v", st.Pending, want)
	}
	for i := range want {
		if st.Pending[i] != want[i] {
			t.Fatalf("pending is %v after four passes over the cap, want %v", st.Pending, want)
		}
	}
	if st.Shipped != 7 || len(f.backend.Delivered()) != 4 {
		t.Fatalf("state %+v and %d records direct: each pass still sends its batch direct", st, len(f.backend.Delivered()))
	}
}

// A range that holds more than one batch goes on chain in records of at most
// the record's size, every event once and in order.
func TestShipRetryPutsAJoinedRangeOnChainInRecordsUnderTheSizeCap(t *testing.T) {
	f := newShipFixture(t)
	f.ship.DailyCap = 0
	for i := 0; i < 8; i++ {
		f.at = f.at.Add(3 * time.Second)
		f.addBig(t, 5, 900)
		f.run(t)
	}
	if st, _ := f.state.Load(context.Background()); len(st.Pending) != 1 || st.Pending[0] != (application.ShipRange{From: 1, To: 41}) {
		t.Fatalf("pending is %v, want one range 1 to 41 (the cap alarm is event 6)", st.Pending)
	}

	f.ship.DailyCap = 500
	f.at = f.at.Add(3 * time.Second)
	f.run(t)
	next := uint64(1)
	for _, payload := range f.chainPayloads(t) {
		_, b := f.open(t, payload)
		plain, _ := json.Marshal(b)
		if len(plain) > 7200+200 || len(payload) > 10240 {
			t.Fatalf("a record of events %d to %d is %d bytes of text and %d on the wire, past the cap", b.From, b.To, len(plain), len(payload))
		}
		if b.From != next || b.Lane != events.LaneNormal {
			t.Fatalf("a record starts at %d in lane %s, want %d in normal", b.From, b.Lane, next)
		}
		next = b.To + 1
	}
	if next != 42 {
		t.Fatalf("the records on chain end at %d, want every event to 41", next-1)
	}
	if n := len(f.chainPayloads(t)); n < 5 {
		t.Fatalf("%d records for 41 events of 900 bytes, want the size cap to cut them in 5 or more", n)
	}
	if st, _ := f.state.Load(context.Background()); len(st.Pending) != 0 {
		t.Fatalf("pending is %v after the range went on chain", st.Pending)
	}
}

// A retry over a long backlog keeps the state once, not after every send.
func TestShipRetryOverALongBacklogKeepsTheStateOnceNotAfterEverySend(t *testing.T) {
	f := newShipFixture(t)
	f.add(t, 200)
	var pending []application.ShipRange
	for n := uint64(1); n <= 200; n++ {
		pending = append(pending, application.ShipRange{From: n, To: n})
	}
	if err := f.state.Save(context.Background(), application.ShipState{Shipped: 200, Day: "2026-10-01", Pending: pending}); err != nil {
		t.Fatal(err)
	}
	counting := &countingStates{ShipStates: f.state}
	f.ship.State = counting
	f.run(t)
	if got := len(f.backend.Broadcasts()); got != 200 {
		t.Fatalf("%d records on chain, want all 200", got)
	}
	if counting.saves > 1 {
		t.Fatalf("the state was written %d times in one pass over 200 sends, want once", counting.saves)
	}
	if st, _ := f.state.Load(context.Background()); len(st.Pending) != 0 || st.Chain != 200 {
		t.Fatalf("state is %+v, want nothing pending and 200 on chain, kept", st)
	}
}

// A pass that only retried (no new events) still keeps what it put on chain.
func TestShipRetryWithNoNewEventsStillKeepsWhatItSent(t *testing.T) {
	f := newShipFixture(t)
	f.add(t, 3)
	pending := []application.ShipRange{{From: 1, To: 1}, {From: 2, To: 2}, {From: 3, To: 3}}
	if err := f.state.Save(context.Background(), application.ShipState{Shipped: 3, Day: "2026-10-01", Pending: pending}); err != nil {
		t.Fatal(err)
	}
	f.run(t)
	f.restart()
	f.run(t)
	if got := f.seqRangesOnChain(t); len(got) != 3 {
		t.Fatalf("%d records on chain across a restart, want 3, each once: %v", len(got), got)
	}
}

// Across restarts after every pass, over several days of the cap, every event
// goes direct once and on chain once, oldest first.
func TestShipLosesNoEventAndSendsNoneTwiceAcrossRestartsAndDaysOfTheCap(t *testing.T) {
	f := newShipFixture(t)
	f.ship.DailyCap = 2
	for i := 0; i < 12; i++ {
		f.at = f.at.Add(3 * time.Second)
		f.addBig(t, 4, 700)
		f.restart()
		f.run(t)
	}
	for day := 0; day < 30; day++ {
		f.at = f.at.Add(24 * time.Hour)
		f.restart()
		f.run(t)
		f.restart()
		f.run(t)
	}
	var direct, chain []uint64
	for _, p := range f.backend.Delivered() {
		_, b := f.open(t, p)
		for _, e := range b.Events {
			direct = append(direct, e.Seq)
		}
	}
	for _, p := range f.chainPayloads(t) {
		_, b := f.open(t, p)
		for _, e := range b.Events {
			chain = append(chain, e.Seq)
		}
	}
	total, _ := f.log.Head(context.Background()) // the cap alarms are events of the log too
	for name, got := range map[string][]uint64{"direct": direct, "chain": chain} {
		if uint64(len(got)) != total || total < 48 {
			t.Fatalf("%d events %s, want %d, each once: %v", len(got), name, total, got)
		}
		for i, seq := range got {
			if seq != uint64(i+1) {
				t.Fatalf("event %d is number %d on the %s road, want %d (in order, none twice)", i, seq, name, i+1)
			}
		}
	}
	if st, _ := f.state.Load(context.Background()); len(st.Pending) != 0 {
		t.Fatalf("pending is %v, want it cleared", st.Pending)
	}
}

// The chain failing in the middle of a joined range leaves the rest of it
// pending, from the first event not on chain.
func TestShipRetryThatFailsInTheMiddleOfAJoinedRangeResumesAtTheFirstEventNotOnChain(t *testing.T) {
	f := newShipFixture(t)
	f.ship.DailyCap = 0
	for i := 0; i < 6; i++ {
		f.at = f.at.Add(3 * time.Second)
		f.addBig(t, 5, 900)
		f.run(t)
	}
	f.ship.DailyCap = 500
	f.at = f.at.Add(3 * time.Second)
	f.ship.ChainRoad = &failAfter{Chain: f.ship.ChainRoad, n: 2}
	f.run(t)
	sent := f.seqRangesOnChain(t)
	if len(sent) != 2 {
		t.Fatalf("%d records on chain, want 2 before the chain failed", len(sent))
	}
	st, _ := f.state.Load(context.Background())
	if len(st.Pending) != 1 || st.Pending[0] != (application.ShipRange{From: sent[1].To + 1, To: 31}) {
		t.Fatalf("pending is %v, want one range from %d to 31", st.Pending, sent[1].To+1)
	}
	f.restart()
	f.at = f.at.Add(3 * time.Second)
	f.run(t)
	next := uint64(1)
	for _, r := range f.seqRangesOnChain(t) {
		if r.From != next {
			t.Fatalf("a record starts at %d, want %d: %v", r.From, next, f.seqRangesOnChain(t))
		}
		next = r.To + 1
	}
	if next != 32 {
		t.Fatalf("the chain holds events up to %d, want 31", next-1)
	}
}

// failAfter lets n sends through and fails the rest.
type failAfter struct {
	application.Chain
	n int
}

func (c *failAfter) Send(ctx context.Context, payload []byte) (string, error) {
	if c.n--; c.n < 0 {
		return "", errors.New("the chain went down")
	}
	return c.Chain.Send(ctx, payload)
}

// retireWorld is a shipper with a backlog of pending ranges, and the command
// that retires them (mw-gq6.927).
type retireWorld struct {
	*shipFixture
	archive *apptest.FakeShipArchive
	retire  application.EventShipRetire
	out     bytes.Buffer
}

func newRetireWorld(t *testing.T) *retireWorld {
	t.Helper()
	w := &retireWorld{shipFixture: newShipFixture(t), archive: &apptest.FakeShipArchive{}}
	w.retire = application.EventShipRetire{
		State: w.state, Archive: w.archive, Out: &w.out,
		Now: func() time.Time { return w.at },
	}
	return w
}

// backlog leaves n batches pending: the chain is down for n passes.
func (w *retireWorld) backlog(t *testing.T, n int) {
	t.Helper()
	w.backend.ChainErr = errors.New("down")
	for i := 0; i < n; i++ {
		w.at = w.at.Add(3 * time.Second)
		w.add(t, 1)
		w.run(t)
	}
	w.backend.ChainErr = nil
}

func TestRetireMovesEveryPendingRangeToTheArchiveAndAFollowingRetrySendsNothingFromIt(t *testing.T) {
	w := newRetireWorld(t)
	w.backlog(t, 3)
	before, _ := w.state.Load(context.Background())
	if len(before.Pending) == 0 {
		t.Fatal("the fixture left nothing pending")
	}
	headBefore, _ := w.log.Head(context.Background())
	sent := len(w.chainPayloads(t))

	got, err := w.retire.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Moved != len(before.Pending) {
		t.Fatalf("moved %d ranges, want %d", got.Moved, len(before.Pending))
	}
	after, _ := w.state.Load(context.Background())
	if len(after.Pending) != 0 || after.Joinable {
		t.Fatalf("pending after the retire is %+v, joinable %v", after.Pending, after.Joinable)
	}
	if after.Shipped != before.Shipped || after.Chain != before.Chain || after.Day != before.Day {
		t.Fatalf("the retire changed more than the pending list: %+v from %+v", after, before)
	}
	if kept := w.archive.Ranges(got.Path); len(kept) != len(before.Pending) || kept[0] != before.Pending[0] {
		t.Fatalf("the archive %s holds %v, want %v", got.Path, kept, before.Pending)
	}
	if got.Path == "" || !strings.Contains(w.out.String(), got.Path) || !strings.Contains(w.out.String(), fmt.Sprintf("Retired %d ", got.Moved)) {
		t.Fatalf("the command printed %q, want the count and the path %q", w.out.String(), got.Path)
	}

	// A fresh follower's next pass has nothing pending to put on chain.
	w.at = w.at.Add(3 * time.Second)
	w.newShip().Ship(context.Background())
	if n := len(w.chainPayloads(t)); n != sent {
		t.Fatalf("a pass after the retire put %d records on chain, want none", n-sent)
	}
	// The event log is untouched.
	if head, _ := w.log.Head(context.Background()); head != headBefore {
		t.Fatalf("the log's head moved from %d to %d", headBefore, head)
	}
	if all, _ := w.log.Since(context.Background(), 0); uint64(len(all)) != headBefore {
		t.Fatalf("the log holds %d events, want %d", len(all), headBefore)
	}
}

func TestRetireTwiceMovesNothingFurtherAndLosesNothing(t *testing.T) {
	w := newRetireWorld(t)
	w.backlog(t, 2)
	first, err := w.retire.Run(context.Background())
	if err != nil || first.Moved == 0 {
		t.Fatalf("first retire: %+v, %v", first, err)
	}
	kept := w.archive.Ranges(first.Path)
	w.out.Reset()
	second, err := w.retire.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Moved != 0 {
		t.Fatalf("the second retire moved %d", second.Moved)
	}
	if again := w.archive.Ranges(first.Path); len(again) != len(kept) {
		t.Fatalf("the second retire changed the archive from %v to %v", kept, again)
	}
	if !strings.Contains(w.out.String(), "0") || !strings.Contains(w.out.String(), second.Path) {
		t.Fatalf("the second retire printed %q", w.out.String())
	}
}

func TestRetireWritesTheArchiveBeforeTheListSoACrashLosesNothing(t *testing.T) {
	w := newRetireWorld(t)
	w.backlog(t, 2)
	before, _ := w.state.Load(context.Background())

	// The archive cannot be written: ship.json is not touched.
	w.archive.Err = errors.New("disk full")
	if _, err := w.retire.Run(context.Background()); err == nil {
		t.Fatal("a retire whose archive failed reported success")
	}
	if now, _ := w.state.Load(context.Background()); len(now.Pending) != len(before.Pending) {
		t.Fatalf("a failed archive still emptied the list: %v", now.Pending)
	}

	// The list cannot be saved after the archive was: the ranges are in both,
	// and a run again does not write them into the archive twice.
	w.archive.Err = nil
	w.state.SaveErr = errors.New("killed")
	got, err := w.retire.Run(context.Background())
	if err == nil {
		t.Fatal("a retire whose save failed reported success")
	}
	if kept := w.archive.Ranges(got.Path); len(kept) != len(before.Pending) {
		t.Fatalf("the archive holds %v before the list is emptied, want %v", kept, before.Pending)
	}
	w.state.SaveErr = nil
	again, err := w.retire.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if kept := w.archive.Ranges(again.Path); len(kept) != len(before.Pending) {
		t.Fatalf("after the crash and a rerun the archive holds %v, want each range once", kept)
	}
	if now, _ := w.state.Load(context.Background()); len(now.Pending) != 0 {
		t.Fatalf("pending after the rerun is %v", now.Pending)
	}
}

func TestRangesQueuedAfterARetireStillSendAndTheStatusSaysHowManyWereRetired(t *testing.T) {
	w := newRetireWorld(t)
	w.backlog(t, 2)
	retired, err := w.retire.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st, _ := w.ship.Status(context.Background())
	if !strings.Contains(st.Line(), "pending 0") || !strings.Contains(st.Line(), "retired") || strings.Contains(strings.ToLower(st.Line()), "fail") {
		t.Fatalf("after a retire the status line is %q", st.Line())
	}
	if len(st.Line()) > application.Width {
		t.Fatalf("the status line is %d wide", len(st.Line()))
	}

	// The chain goes down again: a new range queues, and sends once it is up.
	// A shipper that has just started reads the state the retire left.
	ship := w.newShip()
	w.backend.ChainErr = errors.New("down")
	w.at = w.at.Add(3 * time.Second)
	w.add(t, 1)
	if err := ship.Ship(context.Background()); err != nil {
		t.Fatal(err)
	}
	queued, _ := w.state.Load(context.Background())
	if len(queued.Pending) != 1 {
		t.Fatalf("pending after the retire and a new batch is %v", queued.Pending)
	}
	w.backend.ChainErr = nil
	sent := len(w.chainPayloads(t))
	w.at = w.at.Add(3 * time.Second)
	if err := ship.Ship(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.chainPayloads(t)) != sent+1 {
		t.Fatalf("the range queued after the retire was not put on chain")
	}
	if kept := w.archive.Ranges(retired.Path); len(kept) != retired.Moved {
		t.Fatalf("the archive changed: %v", kept)
	}
}

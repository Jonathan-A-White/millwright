package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// EventsClass is the class an events record travels under: postern's
// docs/protocol.md section 22.
const EventsClass = "events"

// The shape of a batch: events are gathered for ShipWindow, at most
// ShipMaxEvents go in one record, and the sealed text of a record stays under
// shipMaxPlain bytes so that the payload stays under postern's 10,240.
const (
	ShipWindow    = 2 * time.Second
	ShipMaxEvents = 50
	shipMaxPlain  = 7200
)

// ShipRange is the seq range of one batch: From to To, inclusive.
type ShipRange struct {
	From uint64 `json:"from"`
	To   uint64 `json:"to"`
}

// ShipState is what EventShip remembers between runs: the last seq it has
// sent, the batches that went direct as fallback and wait to go on chain,
// and today's count of records put on chain.
type ShipState struct {
	// Shipped is the last seq sent in a batch; the next batch starts after it.
	Shipped uint64 `json:"shipped"`
	// Pending are the fallback batches, oldest first, not yet on chain.
	Pending []ShipRange `json:"pending"`
	// Day is the UTC day (2006-01-02) Chain and Alarmed count, and Chain how
	// many records were put on chain in it.
	Day   string `json:"day"`
	Chain int    `json:"chain"`
	// Alarmed says the day's cap alarm has been raised.
	Alarmed bool `json:"alarmed"`
	// Urgent are the emergency events already sent alone that lie past
	// Shipped, because events before them have not gone in a batch yet: the
	// batches leave them out, and Shipped steps over them.
	Urgent []uint64 `json:"urgent,omitempty"`
	// Emergency is how many emergency records were sent in Day.
	Emergency int `json:"emergency"`
}

// ShipStates keeps EventShip's state between runs.
type ShipStates interface {
	// Load is the state last saved; the zero state when none ever was.
	Load(ctx context.Context) (ShipState, error)
	Save(ctx context.Context, s ShipState) error
}

// EventShip sends the home's events to the Governor: each pass seals the
// events not yet sent as one `events` record (docs/events.md, "The batch") to
// the Governor's key, and puts it on chain and delivers it direct in the same
// pass. A burst is one batch per ShipWindow, or ShipMaxEvents at once.
//
// With the chain unreachable — no UTXOs, the block explorer or /api/broadcast
// down — or Chain false, or DailyCap records on chain already today (UTC), the
// batch goes direct only, in the fallback lane, and is kept pending (when
// Chain is on). Each pass puts the pending batches on chain first, oldest
// first, in the normal lane with the same seq range, until they clear. The
// first batch over the cap in a day also appends one alarm event to the log.
//
// An event in the emergency lane (mw events emit --emergency) does not wait
// for the window: each pass sends it first, alone as a batch of one in the
// emergency lane, on chain and direct at once and before the pending batches
// are retried. It has an allowance of its own, EmergencyDailyCap a UTC day,
// apart from the ordinary cap: it goes on chain even past DailyCap and does
// not count toward it, so a flood of emergencies never stalls the ordinary
// retries. Past its own allowance an emergency goes direct only, is said so on
// Err and is not kept for the chain. With the chain unreachable it goes direct
// and is kept pending, to be put on chain later in the normal lane. The
// batches that follow leave it out.
//
// A batch no road took is not sent: the next pass builds it again.
type EventShip struct {
	Log     EventLog
	State   ShipStates
	Postern Postern
	Cipher  Cipher
	Keys    PosternKeyFile
	// GovernorKey is who the records are sealed to.
	GovernorKey string
	// Chain is whether records go on chain at all, and DailyCap the most
	// that do in a UTC day.
	Chain    bool
	DailyCap int
	// EmergencyDailyCap is the most emergency records sent in a UTC day,
	// counted apart from DailyCap: the emergencies past it go direct only.
	EmergencyDailyCap int
	// Host names the actor of the cap alarm event.
	Host string
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Err is where a failure of one road is said, once for each word of it.
	Err io.Writer

	mu       sync.Mutex
	state    *ShipState
	lastSent time.Time
	said     map[string]string
}

// Ship runs one pass. It returns an error only when a batch was built and no
// road took it, or the state could not be read or kept.
func (s *EventShip) Ship(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		st, err := s.State.Load(ctx)
		if err != nil {
			return fmt.Errorf("loading the shipper's state: %w", err)
		}
		s.state = &st
	}
	st := s.state
	now := s.now().UTC()
	s.rollDay(st, now)

	head, err := s.Log.Head(ctx)
	if err != nil {
		return fmt.Errorf("reading the event log's head: %w", err)
	}
	var unsent []events.Event
	if head > st.Shipped {
		if unsent, err = s.Log.Since(ctx, st.Shipped); err != nil {
			return fmt.Errorf("reading the events after %d: %w", st.Shipped, err)
		}
	}
	chainDown, err := s.sendEmergencies(ctx, st, unsent, now)
	if err != nil {
		return err
	}
	if !chainDown {
		chainDown = s.retryPending(ctx, st)
	}

	was := st.Shipped
	evs := cutBatch(unsentBatch(st, unsent))
	if len(evs) == 0 || !s.due(uint64(len(evs)), now) {
		if st.Shipped != was {
			return s.save(ctx, st)
		}
		return nil
	}
	r := ShipRange{From: evs[0].Seq, To: evs[len(evs)-1].Seq}

	onChain := false
	overCap := false
	if s.Chain && !chainDown {
		if st.Chain >= s.DailyCap {
			overCap = true
		} else {
			payload, err := s.seal(evs, events.LaneNormal, now)
			if err != nil {
				return err
			}
			if _, err := s.putOnChain(ctx, payload); err != nil {
				s.say("putting a batch on chain", err)
			} else {
				onChain = true
				st.Chain++
				s.quiet("putting a batch on chain")
			}
		}
	}
	lane := events.LaneNormal
	if !onChain {
		lane = events.LaneFallback
	}
	payload, err := s.seal(evs, lane, now)
	if err != nil {
		return err
	}
	if _, err := s.Postern.Deliver(ctx, payload); err != nil {
		if !onChain {
			s.keep(ctx, st)
			return fmt.Errorf("delivering events %d to %d: %w", r.From, r.To, err)
		}
		s.say("delivering a batch direct", err)
	} else {
		s.quiet("delivering a batch direct")
	}
	st.Shipped = r.To
	stepOver(st)
	if !onChain && s.Chain {
		st.Pending = append(st.Pending, r)
	}
	s.lastSent = now
	if overCap && !st.Alarmed {
		s.alarm(ctx, st, now)
	}
	return s.save(ctx, st)
}

// sendEmergencies sends each event of unsent that is in the emergency lane
// and not sent yet, alone, in the emergency lane, on chain and direct, oldest
// first. It reports whether the chain failed one, so the pass leaves the
// chain alone. It returns an error only when no road took an emergency: the
// next pass builds it again.
func (s *EventShip) sendEmergencies(ctx context.Context, st *ShipState, unsent []events.Event, now time.Time) (chainDown bool, err error) {
	for _, e := range unsent {
		if e.Lane != events.LaneEmergency || slices.Contains(st.Urgent, e.Seq) {
			continue
		}
		payload, err := s.seal([]events.Event{e}, events.LaneEmergency, now)
		if err != nil {
			return chainDown, err
		}
		overAllowance := st.Emergency >= s.EmergencyDailyCap
		if overAllowance && s.Chain {
			s.say("an emergency past the day's allowance",
				fmt.Errorf("emergency event %d goes direct only: the emergency daily cap of %d records is reached", e.Seq, s.EmergencyDailyCap))
		}
		onChain := false
		if s.Chain && !chainDown && !overAllowance {
			if _, err := s.putOnChain(ctx, payload); err != nil {
				s.say("putting an emergency on chain", err)
				chainDown = true
			} else {
				onChain = true
				s.quiet("putting an emergency on chain")
			}
		}
		if _, err := s.Postern.Deliver(ctx, payload); err != nil {
			if !onChain {
				s.keep(ctx, st)
				return chainDown, fmt.Errorf("delivering emergency event %d: %w", e.Seq, err)
			}
			s.say("delivering an emergency direct", err)
		} else {
			s.quiet("delivering an emergency direct")
		}
		st.Emergency++
		if e.Seq == st.Shipped+1 {
			st.Shipped = e.Seq
			stepOver(st)
		} else {
			st.Urgent = append(st.Urgent, e.Seq)
		}
		if !onChain && s.Chain && !overAllowance {
			st.Pending = append(st.Pending, ShipRange{From: e.Seq, To: e.Seq})
		}
		if err := s.save(ctx, st); err != nil {
			return chainDown, err
		}
	}
	return chainDown, nil
}

// unsentBatch is the events of unsent a batch may hold: the run from the
// first not yet in a record to just before the next emergency event already
// sent alone. It steps Shipped over the emergency events at the front.
func unsentBatch(st *ShipState, unsent []events.Event) []events.Event {
	stepOver(st)
	for i, e := range unsent {
		if e.Seq <= st.Shipped {
			continue
		}
		unsent = unsent[i:]
		for j, f := range unsent {
			if slices.Contains(st.Urgent, f.Seq) {
				return unsent[:j]
			}
		}
		return unsent
	}
	return nil
}

// stepOver moves Shipped past the emergency events sent alone that come
// right after it, and forgets those it has passed.
func stepOver(st *ShipState) {
	for {
		i := slices.Index(st.Urgent, st.Shipped+1)
		if i < 0 {
			break
		}
		st.Shipped++
		st.Urgent = slices.Delete(st.Urgent, i, i+1)
	}
	st.Urgent = slices.DeleteFunc(st.Urgent, func(n uint64) bool { return n <= st.Shipped })
}

// retryPending puts the pending batches on chain, oldest first, while the
// chain answers and the day's cap allows. It reports whether the chain failed
// it, so the pass's new batch does not try the same dead road.
func (s *EventShip) retryPending(ctx context.Context, st *ShipState) (chainDown bool) {
	for s.Chain && len(st.Pending) > 0 && st.Chain < s.DailyCap {
		r := st.Pending[0]
		evs, err := s.eventsIn(ctx, r)
		if err != nil {
			s.say("reading a pending batch", err)
			return false
		}
		payload, err := s.seal(evs, events.LaneNormal, s.now())
		if err != nil {
			s.say("sealing a pending batch", err)
			return false
		}
		if _, err := s.putOnChain(ctx, payload); err != nil {
			s.say("putting a pending batch on chain", err)
			return true
		}
		s.quiet("putting a pending batch on chain")
		st.Pending = st.Pending[1:]
		st.Chain++
		if err := s.save(ctx, st); err != nil {
			s.say("keeping the shipper's state", err)
		}
	}
	return false
}

// eventsIn is the events of r, every one, read from the log.
func (s *EventShip) eventsIn(ctx context.Context, r ShipRange) ([]events.Event, error) {
	all, err := s.Log.Since(ctx, r.From-1)
	if err != nil {
		return nil, err
	}
	n := int(r.To - r.From + 1)
	if len(all) < n || all[0].Seq != r.From || all[n-1].Seq != r.To {
		return nil, fmt.Errorf("the event log no longer holds events %d to %d", r.From, r.To)
	}
	return all[:n], nil
}

// due says whether a batch of n unsent events goes now: ShipMaxEvents are
// enough, otherwise the last batch must be ShipWindow behind.
func (s *EventShip) due(n uint64, now time.Time) bool {
	return n >= ShipMaxEvents || s.lastSent.IsZero() || now.Sub(s.lastSent) >= ShipWindow
}

// cutBatch is the leading events of evs that make one record.
func cutBatch(evs []events.Event) []events.Event {
	if len(evs) > ShipMaxEvents {
		evs = evs[:ShipMaxEvents]
	}
	size := 0
	for i, e := range evs {
		line, err := json.Marshal(e)
		if err != nil {
			continue
		}
		size += len(line) + 1
		if size > shipMaxPlain && i > 0 {
			return evs[:i]
		}
	}
	return evs
}

// seal is evs as one events record to the Governor, in lane, as the payload
// both roads carry.
func (s *EventShip) seal(evs []events.Event, lane string, now time.Time) ([]byte, error) {
	batch := events.Batch{From: evs[0].Seq, To: evs[len(evs)-1].Seq, Lane: lane, Events: make([]events.Event, len(evs))}
	for i, e := range evs {
		e.Lane = lane
		batch.Events[i] = e
	}
	if err := batch.Validate(); err != nil {
		return nil, fmt.Errorf("events %d to %d are not a batch to send: %w", batch.From, batch.To, err)
	}
	plain, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.Cipher.Encrypt(s.GovernorKey, string(plain))
	if err != nil {
		return nil, fmt.Errorf("sealing events %d to %d: %w", batch.From, batch.To, err)
	}
	from, _, err := s.Keys.PublicKey()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: EventsClass,
		To: s.GovernorKey, From: from, Ts: now.Unix(), Ct: ciphertext,
	})
	if err != nil {
		return nil, fmt.Errorf("building the record's payload: %w", err)
	}
	return payload, nil
}

func (s *EventShip) putOnChain(ctx context.Context, payload []byte) (string, error) {
	_, address, err := s.Keys.PublicKey()
	if err != nil {
		return "", err
	}
	return broadcastRecord(ctx, s.Postern, s.Keys, address, payload, s.Err)
}

// alarm appends the event that says the day's cap is reached, once a day.
func (s *EventShip) alarm(ctx context.Context, st *ShipState, now time.Time) {
	st.Alarmed = true
	_, err := s.Log.Append(ctx, []events.Event{{
		Ts: now, Kind: events.KindJob, Actor: "events-follow@" + s.Host,
		From: events.JobRunning, To: events.JobFailed,
		Detail: fmt.Sprintf("chain daily cap of %d records reached: batches go direct as fallback and are re-sent on chain later", s.DailyCap),
		Lane:   events.LaneNormal,
	}})
	if err != nil {
		s.say("appending the cap alarm", err)
	}
}

// rollDay starts a new count when the UTC day has turned.
func (s *EventShip) rollDay(st *ShipState, now time.Time) {
	if day := now.Format("2006-01-02"); st.Day != day {
		st.Day, st.Chain, st.Alarmed, st.Emergency = day, 0, false, 0
	}
}

func (s *EventShip) keep(ctx context.Context, st *ShipState) {
	if err := s.save(ctx, st); err != nil {
		s.say("keeping the shipper's state", err)
	}
}

func (s *EventShip) save(ctx context.Context, st *ShipState) error {
	if err := s.State.Save(ctx, *st); err != nil {
		return fmt.Errorf("saving the shipper's state: %w", err)
	}
	return nil
}

func (s *EventShip) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// say reports a failure of one road on Err, once for each word of it.
func (s *EventShip) say(what string, err error) {
	if s.said == nil {
		s.said = map[string]string{}
	}
	if s.Err != nil && s.said[what] != err.Error() {
		fmt.Fprintf(s.Err, "mw events follow: %s: %v\n", what, err)
	}
	s.said[what] = err.Error()
}

func (s *EventShip) quiet(what string) { delete(s.said, what) }

// ShipStatus is where the shipper stands, for mw status.
type ShipStatus struct {
	// Head is the event log's last seq, Shipped the last one sent.
	Head, Shipped uint64
	// Pending are the fallback batches still to go on chain.
	Pending []ShipRange
	// ChainToday is the records put on chain today (UTC), of Cap; Chain says
	// whether the chain is used at all.
	ChainToday, Cap int
	Chain           bool
	// EmergencyToday is the emergency records sent today (UTC).
	EmergencyToday int
}

// Status reads where the shipper stands. It writes nothing.
func (s *EventShip) Status(ctx context.Context) (ShipStatus, error) {
	st, err := s.State.Load(ctx)
	if err != nil {
		return ShipStatus{}, fmt.Errorf("loading the shipper's state: %w", err)
	}
	head, err := s.Log.Head(ctx)
	if err != nil {
		return ShipStatus{}, fmt.Errorf("reading the event log's head: %w", err)
	}
	s.rollDay(&st, s.now().UTC())
	return ShipStatus{Head: head, Shipped: st.Shipped, Pending: st.Pending, ChainToday: st.Chain, Cap: s.DailyCap, Chain: s.Chain, EmergencyToday: st.Emergency}, nil
}

// Line is the status on one line of the phone-width report.
func (t ShipStatus) Line() string {
	chain := fmt.Sprintf("chain %d/%d", t.ChainToday, t.Cap)
	if !t.Chain {
		chain = "chain off"
	}
	line := fmt.Sprintf("EVENTS head %d shipped %d pending %d %s", t.Head, t.Shipped, len(t.Pending), chain)
	if t.EmergencyToday > 0 {
		line += fmt.Sprintf(" emergency %d", t.EmergencyToday)
	}
	return line
}

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

// ShipRetryBudget is the longest one pass spends putting pending batches on
// chain, by the shipper's clock: when it is spent the pass sends its new
// batch direct, as it does over the cap, and leaves the rest of the pending
// batches to the next. At 4 to 13 sends a minute a quarter of a million pending
// batches held a pass for hours, and with them the new events (mw-gq6.893). At
// least one pending batch is tried every pass.
const ShipRetryBudget = 15 * time.Second

// ShipCallTimeout is the longest one call to the backend or the chain may
// take for the shipper: past it the call is given up, said once, and tried
// again on a later pass.
const ShipCallTimeout = 30 * time.Second

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
	// Pending are the fallback batches, oldest first, not yet on chain. A range
	// the shipper made after Joinable came in may hold more than one batch.
	Pending []ShipRange `json:"pending"`
	// Joinable says the last of Pending was begun by a pass that kept its batch
	// for the chain, so the next such batch that starts where it ends joins it
	// rather than becoming a range of its own. A state without it (every range
	// pending before mw-gq6.895) has none to join: those are never changed
	// except by going on chain.
	Joinable bool `json:"joinable,omitempty"`
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
	// Retired is how many pending ranges mw events retire has moved to the
	// archive, never to go on chain from here: the Governor gave up their chain
	// copy (mw-gq6.927). Every event of them reached him direct.
	Retired int `json:"retired,omitempty"`
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
// A pass spends at most ShipRetryBudget on the pending batches (at least one is
// tried). When it is spent with batches still pending, the pass's new batch goes
// direct as fallback and is kept pending behind them, so that no batch passes an
// older one on chain and a long backlog never holds the new events back. Each
// call to the backend or the chain is given up after ShipCallTimeout.
//
// A batch kept for the chain, as the cap or the budget or the chain's failing
// makes it, joins the last pending range when that range ends right before it
// and was begun by such a batch (ShipState.Joinable), so a day at the cap adds
// one range, not one a pass. A retry puts a range on chain in records of the
// batch's size (ShipMaxEvents events, shipMaxPlain bytes), oldest events first,
// and keeps the shipper's state once a pass, not after every send: a pass that
// dies between a send and the end of the pass sends those records again.
//
// A batch no road took is not sent: the next pass builds it again.
type EventShip struct {
	Log     EventLog
	State   ShipStates
	Postern Postern
	// ChainRoad is the chain a record goes on, when Chain says records do.
	ChainRoad Chain
	Cipher    Cipher
	Keys      PosternKeyFile
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

	mu    sync.Mutex
	state *ShipState
	// unsaved says state holds what a retry put on chain that is not kept yet.
	unsaved  bool
	lastSent time.Time
	said     map[string]string
}

// Ship runs one pass. It returns an error only when a batch was built and no
// road took it, or the state could not be read or kept.
func (s *EventShip) Ship(ctx context.Context) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if s.unsaved {
			if saveErr := s.save(ctx, s.state); err == nil {
				err = saveErr
			}
		}
	}()
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
	if _, err := s.deliver(ctx, payload); err != nil {
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
		st.keepForChain(r)
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
		if _, err := s.deliver(ctx, payload); err != nil {
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
			st.Joinable = false // the emergency's range ends the run a batch joins
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

// keepForChain keeps batch r for the chain: it joins the last pending range
// when that was begun by a batch kept this way and ends right before r,
// otherwise r is a range of its own and the one a later batch may join.
func (st *ShipState) keepForChain(r ShipRange) {
	if n := len(st.Pending); st.Joinable && n > 0 && st.Pending[n-1].To+1 == r.From {
		st.Pending[n-1].To = r.To
		return
	}
	st.Pending = append(st.Pending, r)
	st.Joinable = true
}

// retryPending puts the pending batches on chain, oldest first, while the
// chain answers, the day's cap allows and ShipRetryBudget is not spent. A range
// that holds more than one batch goes in records cut as a batch is, the
// range's From moving up past each one sent. It reports whether the pass's new
// batch is to leave the chain alone: the chain failed it, or the budget ran
// out with batches still pending, which a newer batch must not pass on chain.
// It keeps nothing: Ship keeps the state once, when the pass ends.
func (s *EventShip) retryPending(ctx context.Context, st *ShipState) (chainDown bool) {
	began := s.now()
	var evs []events.Event // what is left of Pending[0], read once
	for tried := 0; s.Chain && len(st.Pending) > 0 && st.Chain < s.DailyCap; tried++ {
		if tried > 0 && s.now().Sub(began) >= ShipRetryBudget {
			return true
		}
		r := st.Pending[0]
		if len(evs) == 0 {
			var err error
			if evs, err = s.eventsIn(ctx, r); err != nil {
				s.say("reading a pending batch", err)
				return false
			}
		}
		record := cutBatch(evs)
		payload, err := s.seal(record, events.LaneNormal, s.now())
		if err != nil {
			s.say("sealing a pending batch", err)
			return false
		}
		if _, err := s.putOnChain(ctx, payload); err != nil {
			s.say("putting a pending batch on chain", err)
			return true
		}
		s.quiet("putting a pending batch on chain")
		evs = evs[len(record):]
		if last := record[len(record)-1].Seq; last < r.To {
			st.Pending[0].From = last + 1
		} else {
			st.Pending = st.Pending[1:]
		}
		st.Chain++
		s.unsaved = true
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
	record := PosternPayload{
		V: 1, Kind: PosternMessageKind, Class: EventsClass,
		To: s.GovernorKey, From: from, Ts: now.Unix(), Ct: ciphertext,
	}
	if lane == events.LaneEmergency {
		record.Lane = events.LaneEmergency // the clear copy is the one the backend pushes on
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("building the record's payload: %w", err)
	}
	return payload, nil
}

func (s *EventShip) putOnChain(ctx context.Context, payload []byte) (string, error) {
	if s.ChainRoad == nil {
		return "", fmt.Errorf("no chain is configured")
	}
	ctx, cancel := context.WithTimeout(ctx, ShipCallTimeout)
	defer cancel()
	return s.ChainRoad.Send(ctx, payload)
}

// deliver hands payload to the backend direct, given up after ShipCallTimeout.
func (s *EventShip) deliver(ctx context.Context, payload []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ShipCallTimeout)
	defer cancel()
	return s.Postern.Deliver(ctx, payload)
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
	s.unsaved = false
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
	// Retired are the ranges given up for the chain, kept in the archive.
	Retired int
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
	return ShipStatus{Head: head, Shipped: st.Shipped, Pending: st.Pending, ChainToday: st.Chain, Cap: s.DailyCap, Chain: s.Chain, EmergencyToday: st.Emergency, Retired: st.Retired}, nil
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
	if t.Retired > 0 {
		line += fmt.Sprintf(" retired %d", t.Retired)
	}
	return line
}

// ShipArchive is where the ranges a retire gives up are kept, one file for each
// UTC day beside ship.json, so that putting them back in its pending list
// sends them again.
type ShipArchive interface {
	// Keep adds ranges to the archive file of day (2006-01-02), leaving out
	// those the file holds already, and reports the file's path. The file is
	// replaced whole, so a crash leaves the old one or the new.
	Keep(ctx context.Context, ranges []ShipRange, day string) (path string, err error)
	// Path is where the archive file of day is, whether or not it exists.
	Path(day string) string
}

// EventShipRetire gives up the chain copy of every range pending in ship.json,
// once and on the Governor's word (mw-gq6.927): the ranges go to the day's
// archive file, then the pending list is emptied, in that order, so a crash
// between the two leaves them in both and a run again keeps each once. It
// touches nothing else: the event log, what was shipped and the day's counts
// stay as they were, and a range queued afterwards is sent as ever.
//
// The follower keeps the state in memory, so it must be stopped while this runs
// and started after: it would otherwise save its pending list over the empty
// one.
type EventShipRetire struct {
	State   ShipStates
	Archive ShipArchive
	// Now dates the archive file; nil is time.Now.
	Now func() time.Time
	// Out, when set, is told how many ranges were moved and where.
	Out io.Writer
}

// RetireResult is what a retire did.
type RetireResult struct {
	// Moved is how many pending ranges went to the archive, Path the archive
	// file of the day.
	Moved int
	Path  string
}

// Run retires every range pending now.
func (r EventShipRetire) Run(ctx context.Context) (RetireResult, error) {
	if r.State == nil || r.Archive == nil {
		return RetireResult{}, fmt.Errorf("retiring the pending ranges needs the shipper's state and an archive")
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	day := now().UTC().Format("2006-01-02")
	st, err := r.State.Load(ctx)
	if err != nil {
		return RetireResult{}, fmt.Errorf("loading the shipper's state: %w", err)
	}
	if len(st.Pending) == 0 {
		res := RetireResult{Path: r.Archive.Path(day)}
		return res, r.say(res, "Nothing pending: retired 0 ranges. The day's archive file is %s.")
	}
	path, err := r.Archive.Keep(ctx, st.Pending, day)
	if err != nil {
		return RetireResult{}, fmt.Errorf("keeping the pending ranges in the archive: %w", err)
	}
	res := RetireResult{Moved: len(st.Pending), Path: path}
	st.Retired += res.Moved
	st.Pending, st.Joinable = nil, false
	if err := r.State.Save(ctx, st); err != nil {
		return res, fmt.Errorf("saving the shipper's state, the ranges are in %s and still pending: %w", path, err)
	}
	return res, r.say(res, "Retired %d pending ranges to the archive %s. To send them again, put its ranges back in ship.json's pending list.")
}

func (r EventShipRetire) say(res RetireResult, format string) error {
	if r.Out == nil {
		return nil
	}
	var err error
	if res.Moved == 0 {
		_, err = fmt.Fprintf(r.Out, format+"\n", res.Path)
	} else {
		_, err = fmt.Fprintf(r.Out, format+"\n", res.Moved, res.Path)
	}
	return err
}

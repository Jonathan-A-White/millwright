package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CrossSlotKey is the key a rig's cross-host merge slot is kept under in the
// shared tracker's key-value store.
func CrossSlotKey(rig string) string { return "merge-slot." + rig }

// CrossTurnKey is the key a close-out waiting for a rig's cross-host merge slot
// asks for the next turn at it under, when the other host holds it.
func CrossTurnKey(rig string) string { return "merge-turn." + rig }

// CrossTurnPolls is how many polls an ask for the next turn at a cross-host
// merge slot lasts unless its asker, who renews it at every poll, does.
const CrossTurnPolls = 4

// How a cross-host merge slot is kept. CrossSlotTTL is how long a lease lasts
// unless its holder renews it, which it does every third of that while it
// lands; CrossSlotPoll is how often a close-out waiting for it looks again;
// CrossSlotSettle is how long a close-out that wrote its lease waits before it
// reads it back, so that the other host, writing its own in the same moment,
// has written by then; CrossSlotCap is how long a close-out waits for it in all
// before it goes ahead without it; CrossSlotRetry is how long a read or write of
// the slot that fails is tried again, on the poll, before the slot is skipped.
const (
	CrossSlotTTL    = 10 * time.Minute
	CrossSlotPoll   = 15 * time.Second
	CrossSlotSettle = 5 * time.Second
	CrossSlotCap    = 3 * time.Hour
	CrossSlotRetry  = 2 * time.Minute
)

// CrossSlot is a rig's merge slot across hosts (mw-gq6.359): a lease, kept on
// the beads server both hosts share, that one close-out on either host holds
// while it fetches, merges, re-runs the rig's tests on the merged result and
// pushes. It is taken inside this host's own MergeSlot, never around it: a
// close-out waiting in line for its own host's slot holds nothing the other
// host waits for (mw-gq6.421).
//
// Without it the two hosts raced at the remote, and a host whose merged-result
// tests take longer than the other host takes to land a story never won: each
// try was refused and re-ran the tests, the other host landed again meanwhile,
// and the story stopped push-lost. With it the slower host waits its turn,
// and then nothing lands under it.
//
// The store has no compare-and-set, so a lease is taken by writing it, waiting
// Settle, and reading it back: whoever's lease is there then has the slot. Two
// hosts can still, rarely, both read it back as their own; the remote refusing
// the second push settles that, as it settled every race before. A lease past
// its expiry is a holder that went away and is taken over, and said so. A slot
// that cannot be read or written is tried again on the poll, since the server's
// reach drops for seconds under load; one that still cannot be after Retry is
// skipped, and said so with how many tries failed over how long: the landing
// goes ahead under this host's slot alone, as it did before there was one.
//
// A close-out that finds the slot held by the other host asks for the next turn
// at it, and a close-out that finds the slot free while the other host has the
// turn waits for that host's landing first (mw-gq6.422). Without it a host
// with several close-outs of a rig in line passed the slot from one to the
// next within seconds, while the other host, looking again only every poll,
// could wait through several landings in a row. The turn is a courtesy: one
// that cannot be read or written is no turn, and an asker that went away
// leaves one that runs out within CrossTurnPolls polls.
//
// The zero CrossSlot, or one with no Notes, takes nothing and says nothing.
type CrossSlot struct {
	Notes SlotNotes

	// Host is this host, written in the lease so that whoever waits on it
	// can say whose landing it waited for.
	Host string

	// TTL, Poll, Settle, Cap and Retry are CrossSlotTTL, CrossSlotPoll,
	// CrossSlotSettle, CrossSlotCap and CrossSlotRetry when zero.
	TTL    time.Duration
	Poll   time.Duration
	Settle time.Duration
	Cap    time.Duration
	Retry  time.Duration

	// Now and Wait are the clock: the real one unless a test moves time
	// without truly waiting for it. Wait returns early with the context's
	// error when the context gives up.
	Now  func() time.Time
	Wait func(ctx context.Context, d time.Duration) error
}

// CrossSlotLease is what a rig's cross-host merge slot says while somebody has
// it: who, on which host, the token that tells this holding from any other,
// when it was taken and when it runs out unless it is renewed.
type CrossSlotLease struct {
	Holder  string    `json:"holder"`
	Host    string    `json:"host"`
	Token   string    `json:"token"`
	Taken   time.Time `json:"taken"`
	Expires time.Time `json:"expires"`
}

// Note is the lease as it is kept in the tracker's key-value store.
func (l CrossSlotLease) Note() string {
	said, _ := json.Marshal(l)
	return string(said)
}

// ReadCrossSlotLease is the lease a note holds, and whether it held one.
func ReadCrossSlotLease(note string) (CrossSlotLease, bool) {
	var l CrossSlotLease
	if err := json.Unmarshal([]byte(strings.TrimSpace(note)), &l); err != nil || l.Token == "" || l.Expires.IsZero() {
		return CrossSlotLease{}, false
	}
	return l, true
}

// CrossHolding is a rig's cross-host merge slot this close-out has: it renews
// the lease until it is released.
type CrossHolding struct {
	slot CrossSlot
	key  string
	rig  string

	mu      sync.Mutex
	lease   CrossSlotLease
	trouble []string
	fails   []*leaseRepeat // renewals that failed, each way once, with how often
	lost    bool

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// Take waits for rig's cross-host merge slot and takes it for holder. What it
// returns besides the holding is what the report says of it: how long it
// waited and for whose landing, a holder whose lease had run out that it took
// it over from, or why it went ahead without it. A nil holding is a slot that
// was skipped or not taken: the landing goes ahead under this host's slot
// alone, and nothing has to be given back.
func (s CrossSlot) Take(ctx context.Context, rig, holder string) (*CrossHolding, []string) {
	if s.Notes == nil || strings.TrimSpace(rig) == "" {
		return nil, nil
	}
	key, turnKey := CrossSlotKey(rig), CrossTurnKey(rig)
	began := s.now()
	// This close-out's ask for the next turn, once it has made one: taken
	// back however the wait ends, when it is still this close-out's.
	var turn CrossSlotLease
	defer func() {
		if turn.Token != "" {
			s.leaveTurn(context.WithoutCancel(ctx), turnKey, turn.Token)
		}
	}()
	var waitedOn []string
	waited := func(said []string) []string {
		if len(waitedOn) == 0 {
			return said
		}
		return append([]string{fmt.Sprintf("waited %s for %s landing of %s", SlotAge(s.now().Sub(began)), possessive(waitedOn), rig)}, said...)
	}

	for {
		var note string
		if failed := s.retried(ctx, rig, func() (err error) { note, err = s.Notes.Note(ctx, key); return }); failed != "" {
			return nil, waited([]string{failed})
		}
		now := s.now()
		var took []string
		if strings.TrimSpace(note) != "" {
			held, ok := ReadCrossSlotLease(note)
			switch {
			case !ok:
				took = []string{fmt.Sprintf("took over the cross-host merge slot of %s, whose note could not be read as a lease: %q", rig, clippedTo(note, 120))}
			case now.Before(held.Expires):
				waitedOn = addOnce(waitedOn, held.Host)
				if long := now.Sub(began); long >= s.cap() {
					return nil, []string{fmt.Sprintf("waited %s for %s landing of %s and went ahead without the cross-host merge slot: %s still held it, past the %s a close-out waits for it",
						SlotAge(long), possessive(waitedOn), rig, held.Holder, SlotAge(s.cap()))}
				}
				if held.Host != s.Host {
					turn = s.askForTheTurn(ctx, turnKey, holder, turn.Token, now)
				}
				if err := s.wait(ctx, s.poll()); err != nil {
					return nil, waited([]string{fmt.Sprintf("the cross-host merge slot of %s was not taken: waiting for %s: %v", rig, held.Holder, err)})
				}
				continue
			default:
				took = []string{fmt.Sprintf("took over the cross-host merge slot of %s from %s: its lease was taken %s ago and ran out %s ago, so its holder is gone",
					rig, held.Holder, SlotAge(now.Sub(held.Taken)), SlotAge(now.Sub(held.Expires)))}
			}
		}

		// The slot is free, but a close-out of the other host waited for it:
		// the turn is that one's.
		if next, ok := s.otherTurn(ctx, turnKey, now); ok {
			waitedOn = addOnce(waitedOn, next.Host)
			if long := now.Sub(began); long >= s.cap() {
				return nil, []string{fmt.Sprintf("waited %s for %s landing of %s and went ahead without the cross-host merge slot: %s had the next turn at it, past the %s a close-out waits for it",
					SlotAge(long), possessive(waitedOn), rig, next.Holder, SlotAge(s.cap()))}
			}
			if err := s.wait(ctx, s.poll()); err != nil {
				return nil, waited([]string{fmt.Sprintf("the cross-host merge slot of %s was not taken: waiting for %s, whose turn it was: %v", rig, next.Holder, err)})
			}
			continue
		}

		mine := CrossSlotLease{Holder: holder, Host: s.Host, Token: newSlotToken(), Taken: now, Expires: now.Add(s.ttl())}
		if failed := s.retried(ctx, rig, func() error { return s.Notes.SetNote(ctx, key, mine.Note()) }); failed != "" {
			return nil, waited([]string{failed})
		}
		if err := s.wait(ctx, s.settle()); err != nil {
			return nil, waited([]string{fmt.Sprintf("the cross-host merge slot of %s was not taken: %v; the lease written for it runs out by itself at %s",
				rig, err, mine.Expires.UTC().Format(time.RFC3339))})
		}
		var back string
		if failed := s.retried(ctx, rig, func() (err error) { back, err = s.Notes.Note(ctx, key); return }); failed != "" {
			return nil, waited([]string{failed + fmt.Sprintf("; the lease written for it runs out by itself at %s", mine.Expires.UTC().Format(time.RFC3339))})
		}
		if read, ok := ReadCrossSlotLease(back); !ok || read.Token != mine.Token {
			// The other host wrote its own in the same moment, after this one:
			// it has the slot, and this close-out waits for it like any other.
			continue
		}
		return s.hold(ctx, key, rig, mine), waited(took)
	}
}

// askForTheTurn writes, or writes again, this close-out's ask for the next turn
// at the slot, under the token it already has or a new one. One that cannot be
// written is no turn asked for, and is tried again at the next poll.
func (s CrossSlot) askForTheTurn(ctx context.Context, turnKey, holder, token string, now time.Time) CrossSlotLease {
	if token == "" {
		token = newSlotToken()
	}
	ask := CrossSlotLease{Holder: holder, Host: s.Host, Token: token, Taken: now, Expires: now.Add(CrossTurnPolls * s.poll())}
	if err := s.Notes.SetNote(ctx, turnKey, ask.Note()); err != nil {
		return CrossSlotLease{Token: token}
	}
	return ask
}

// otherTurn is the ask for the next turn at the slot of a close-out of another
// host, when there is one that has not run out. A turn that cannot be read is
// no turn.
func (s CrossSlot) otherTurn(ctx context.Context, turnKey string, now time.Time) (CrossSlotLease, bool) {
	note, err := s.Notes.Note(ctx, turnKey)
	if err != nil || strings.TrimSpace(note) == "" {
		return CrossSlotLease{}, false
	}
	ask, ok := ReadCrossSlotLease(note)
	if !ok || ask.Host == s.Host || !now.Before(ask.Expires) {
		return CrossSlotLease{}, false
	}
	return ask, true
}

// leaveTurn takes this close-out's ask for the next turn back, when it is still
// this close-out's.
func (s CrossSlot) leaveTurn(ctx context.Context, turnKey, token string) {
	note, err := s.Notes.Note(ctx, turnKey)
	if err != nil {
		return
	}
	if ask, ok := ReadCrossSlotLease(note); ok && ask.Token == token {
		_ = s.Notes.ClearNote(ctx, turnKey)
	}
}

// retried runs a read or write of the slot, and while it fails tries it again
// on the poll until the retry window has passed. It returns what the report
// says when the slot has to be skipped, or when the wait between tries is cut
// short; empty when a try succeeded.
func (s CrossSlot) retried(ctx context.Context, rig string, try func() error) string {
	var first time.Time
	for tries := 1; ; tries++ {
		err := try()
		if err == nil {
			return ""
		}
		now := s.now()
		if tries == 1 {
			first = now
		}
		if long := now.Sub(first); long >= s.retry() {
			return s.skipped(rig, err, tries, long)
		}
		if werr := s.wait(ctx, s.poll()); werr != nil {
			return fmt.Sprintf("the cross-host merge slot of %s was not taken: waiting to try the beads server again after %d failed tries (the last: %v): %v", rig, tries, err, werr)
		}
	}
}

// skipped is what the report says of a slot that could not be read or written
// in tries goes over long.
func (s CrossSlot) skipped(rig string, err error, tries int, long time.Duration) string {
	return fmt.Sprintf("the cross-host merge slot of %s was skipped, so this landing went ahead under this host's merge slot alone: the beads server could not be reached: %v (%d tries failed over %s)",
		rig, err, tries, SlotAge(long))
}

// hold starts renewing a lease just taken, every third of its life, until the
// holding is released.
func (s CrossSlot) hold(ctx context.Context, key, rig string, lease CrossSlotLease) *CrossHolding {
	ctx, cancel := context.WithCancel(ctx)
	h := &CrossHolding{slot: s, key: key, rig: rig, lease: lease, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		for s.wait(ctx, s.ttl()/3) == nil {
			if !h.renew(ctx) {
				return
			}
		}
	}()
	return h
}

// renew pushes the lease's expiry on, when it is still this holding's. It
// reports whether to go on renewing: a lease another close-out has taken over
// is never written over.
func (h *CrossHolding) renew(ctx context.Context) bool {
	note, err := h.slot.Notes.Note(ctx, h.key)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		// A read cut short by the release is no trouble: the slot is being
		// given back.
		if ctx.Err() != nil {
			return false
		}
		h.failedRenewal(err)
		return true
	}
	if read, ok := ReadCrossSlotLease(note); !ok || read.Token != h.lease.Token {
		by := "nobody"
		if ok {
			by = read.Holder
		}
		h.lost = true
		h.trouble = append(h.trouble, fmt.Sprintf("the cross-host merge slot of %s was lost while this landing held it: its lease ran out unrenewed and it is now held by %s", h.rig, by))
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	renewed := h.lease
	renewed.Expires = h.slot.now().Add(h.slot.ttl())
	if err := h.slot.Notes.SetNote(ctx, h.key, renewed.Note()); err != nil {
		if ctx.Err() == nil {
			h.failedRenewal(err)
		}
		return true
	}
	h.lease = renewed
	return true
}

// failedRenewal notes a renewal that failed, to be told once however often it
// failed the same way (mw-gq6.821). The caller holds h.mu.
func (h *CrossHolding) failedRenewal(err error) {
	at := h.slot.now()
	for _, f := range h.fails {
		if f.same(err) {
			f.again(at)
			return
		}
	}
	h.fails = append(h.fails, &leaseRepeat{err: err.Error(), count: 1, first: at, last: at})
}

// Release stops renewing the lease and gives the slot back, when it is still
// this holding's. What it returns is what went wrong while it was held or
// given back, for the report. Releasing twice is harmless.
func (h *CrossHolding) Release(ctx context.Context) []string {
	if h == nil {
		return nil
	}
	var said []string
	h.once.Do(func() {
		h.cancel()
		<-h.done
		h.mu.Lock()
		defer h.mu.Unlock()
		said = h.trouble
		for _, f := range h.fails {
			said = append(said, f.line(fmt.Sprintf("the cross-host merge slot of %s could not be renewed", h.rig)))
		}
		if h.lost {
			return
		}
		note, err := h.slot.Notes.Note(ctx, h.key)
		if err != nil {
			said = append(said, fmt.Sprintf("the cross-host merge slot of %s could not be given back: %v; its lease runs out by itself at %s",
				h.rig, err, h.lease.Expires.UTC().Format(time.RFC3339)))
			return
		}
		if read, ok := ReadCrossSlotLease(note); !ok || read.Token != h.lease.Token {
			return
		}
		if err := h.slot.Notes.ClearNote(ctx, h.key); err != nil {
			said = append(said, fmt.Sprintf("the cross-host merge slot of %s could not be given back: %v; its lease runs out by itself at %s",
				h.rig, err, h.lease.Expires.UTC().Format(time.RFC3339)))
		}
	})
	return said
}

// SlotAge is a wait or an age as the report says it: whole seconds under a
// minute, whole minutes from there.
func SlotAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	}
	return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
}

// possessive names the hosts a close-out waited on, as whose landing it was:
// "laptop's", or "laptop's and vps's".
func possessive(hosts []string) string {
	said := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if host == "" {
			host = "another host"
		}
		said = append(said, host+"'s")
	}
	return strings.Join(said, " and ")
}

// addOnce adds a name to a list that does not hold it yet.
func addOnce(names []string, name string) []string {
	for _, have := range names {
		if have == name {
			return names
		}
	}
	return append(names, name)
}

// newSlotToken is what tells one holding of a slot from every other.
func newSlotToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (s CrossSlot) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s CrossSlot) wait(ctx context.Context, d time.Duration) error {
	if s.Wait == nil {
		return waitFor(ctx, d)
	}
	return s.Wait(ctx, d)
}

func (s CrossSlot) ttl() time.Duration    { return orDefault(s.TTL, CrossSlotTTL) }
func (s CrossSlot) poll() time.Duration   { return orDefault(s.Poll, CrossSlotPoll) }
func (s CrossSlot) settle() time.Duration { return orDefault(s.Settle, CrossSlotSettle) }
func (s CrossSlot) cap() time.Duration    { return orDefault(s.Cap, CrossSlotCap) }
func (s CrossSlot) retry() time.Duration  { return orDefault(s.Retry, CrossSlotRetry) }

func orDefault(d, fallback time.Duration) time.Duration {
	if d <= 0 {
		return fallback
	}
	return d
}

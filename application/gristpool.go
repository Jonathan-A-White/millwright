package application

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultGristPoolQuiet is how long a kind goes without a grist before its
// warm pool shrinks to nothing (config [grist] pool_quiet), and
// DefaultGristPoolMaxAge how long a warm session waits for its grist before it
// is retired unused (pool_max_age).
const (
	DefaultGristPoolQuiet  = 10 * time.Minute
	DefaultGristPoolMaxAge = 15 * time.Minute
)

// GristPoolHeartbeat is the longest the pool goes without saving what it
// holds while it is ticked, and GristPoolStale the age past which mw status
// takes a saved view to be no pool's: its follower has stopped.
const (
	GristPoolHeartbeat = time.Minute
	GristPoolStale     = 3 * GristPoolHeartbeat
)

// WarmGrinder starts a grind's session before its grist has come. The real
// adapter is infrastructure/claude's Grinder.
type WarmGrinder interface {
	// Warm starts a session for call, whose Prompt is empty: the same session
	// Grinder.Grind would run for call, in call.Dir, waiting for its one
	// message. It sends nothing to the model until it is given it.
	Warm(call GrindCall) (WarmSession, error)
}

// WarmSession is one session a WarmGrinder started: it answers one message
// and ends.
type WarmSession interface {
	// Answer gives the session its one message and waits at most timeout for
	// what the harness said. An error is a session that could not answer or
	// left nothing readable, and has ended: ErrGrindTimedOut, wrapped, when it
	// ran out of time.
	Answer(ctx context.Context, prompt string, timeout time.Duration) (SessionResult, error)
	// Exited says the session has ended without being given its message.
	Exited() bool
	// Retire ends a session that was never given its message.
	Retire()
}

// GristPoolBoard keeps what the warm pool holds, for mw status, which runs in
// another process, to read. The real adapter is infrastructure/grist's.
type GristPoolBoard interface {
	SavePool(ctx context.Context, view GristPoolView) error
	// Pool is the view saved last; none saved is the zero view.
	Pool(ctx context.Context) (GristPoolView, error)
}

// GristPoolView is what the warm pool holds at one moment, kind by kind.
type GristPoolView struct {
	At    time.Time       `json:"at"`
	Kinds []GristPoolKind `json:"kinds"`
}

// GristPoolKind is one app and kind's sessions: Warm are waiting for a grist,
// Busy are answering one (warm or cold).
type GristPoolKind struct {
	App  string `json:"app"`
	Kind string `json:"kind"`
	Warm int    `json:"warm"`
	Busy int    `json:"busy"`
}

// Line is the pool as mw status says it, in one line.
func (v GristPoolView) Line() string {
	if len(v.Kinds) == 0 {
		return "grist pool: none warm"
	}
	parts := make([]string, 0, len(v.Kinds))
	for _, k := range v.Kinds {
		parts = append(parts, fmt.Sprintf("%s/%s %d warm, %d busy", k.App, k.Kind, k.Warm, k.Busy))
	}
	return "grist pool: " + strings.Join(parts, "; ")
}

// GristPool is the mill's warm pool: for each app and kind, sessions started
// with the grind's system prompt, model, effort and schema before its grist
// comes, each in a private directory of its own, so a grist does not wait for
// one to start. A session is taken by one grist, answers it and ends: it is
// never given another, and the pool starts its replacement. A kind's warm
// sessions are started on the billing its grists are ground on, each billing
// apart: the Governor's grists on his subscription, another licence's on the
// API key (mw-gq6.1519). On each, a kind keeps as many warm sessions as it has
// lately had grists on it in flight at once, at most Slots in all, and none
// once it has had no grist for Quiet. A warm session older
// than MaxAge, or started for a grind that has since changed, is retired
// unused. It is safe for the grinds of a pass to use at once.
type GristPool struct {
	Warmer WarmGrinder
	// Slots is the most warm sessions a kind keeps: the mill's grind slots.
	Slots int
	// Quiet and MaxAge are config [grist] pool_quiet and pool_max_age; zero is
	// DefaultGristPoolQuiet and DefaultGristPoolMaxAge.
	Quiet  time.Duration
	MaxAge time.Duration
	// TempDir is where each warm session's private directory is made; empty
	// is the system's.
	TempDir string
	// Credits, when set, gives the key that warm sessions for grists billed to
	// the key are started with; the pool retires those once the credits no
	// longer let the key be used. Nil starts none on the key.
	Credits *GristCredits
	// Board, when set, is told what the pool holds whenever that changes, and
	// at least every GristPoolHeartbeat while the pool is ticked.
	Board GristPoolBoard
	// Now is the clock; nil is time.Now. Err is where a session that cannot
	// start or a board that cannot be saved is said, once for each distinct
	// failure; nil says nothing.
	Now func() time.Time
	Err io.Writer

	mu    sync.Mutex
	kinds map[gristPoolKey]*gristPoolKind
	saved []GristPoolKind
	// savedAt is when the board was last saved; never is the zero time.
	savedAt time.Time
	said    string
}

// gristPoolKey is an app and kind, and the billing its warm sessions there
// are started on: BillingSubscription or BillingAPI.
type gristPoolKey struct{ app, kind, billing string }

// poolBillings is every billing the pool keeps warm sessions on.
var poolBillings = [...]string{BillingSubscription, BillingAPI}

// gristPoolKind is what the pool holds for one app and kind on one billing.
type gristPoolKind struct {
	// spec is the grind the kind's last grist was ground with: what its warm
	// sessions are started for.
	spec gristWarmSpec
	idle []*gristWarm
	busy int
	// demand is the kind's grists within the quiet spell, each with how many
	// of the kind were in flight once it was taken.
	demand []gristDemand
	// failed is a warm session that could not start, or ended on its own: the
	// kind starts none until its next grist.
	failed bool
}

type gristDemand struct {
	at       time.Time
	inFlight int
}

// gristWarm is one warm session and the private directory it runs in.
type gristWarm struct {
	session WarmSession
	spec    gristWarmSpec
	dir     string
	started time.Time
	// billing is what the session was started on: BillingAPI when its
	// environment carries the key.
	billing string
}

// gristWarmSpec is everything a grind's session is started with but its
// private directory and its message: two grists with the same spec
// may be given sessions started the same way.
type gristWarmSpec struct {
	app, kind, v                        string
	model, effort, instructions, schema string
	maxTurns                            int
	timeout                             time.Duration
}

// call is the session spec runs as, in dir: the mill's preamble then the
// grind's instructions as the system prompt, the same bytes for every grist of
// the kind. Its Prompt is left empty for the grist's message, which carries the
// tag of the lines its request sits between.
func (s gristWarmSpec) call(dir string) GrindCall {
	return GrindCall{
		Dir:      dir,
		Model:    s.model,
		Effort:   s.effort,
		System:   gristPreamble(GristName{App: s.app, Kind: s.kind, V: s.v}) + "\n\n" + s.instructions,
		Schema:   s.schema,
		Timeout:  s.timeout,
		MaxTurns: s.maxTurns,
	}
}

// Configure sets the pool's limits, as config [grist] says them now: the
// follower reads them again before each pass, while the pool is in use.
func (p *GristPool) Configure(slots int, quiet, maxAge time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Slots, p.Quiet, p.MaxAge = slots, quiet, maxAge
}

// UseCredits sets the credits the pool starts sessions on the key with, as
// config [grist] says them now; warm sessions on the key are retired by the
// next tick once these credits no longer let the key be used.
func (p *GristPool) UseCredits(c *GristCredits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Credits = c
}

// take is a warm session for a grist ground with spec and billed to billing,
// or nil when the pool has none: the grist then starts its own, cold. Either
// way the grist counts as the kind's demand on that billing and as busy until
// ended, and the pool starts what the kind now wants on it. Warm sessions of
// the kind on that billing started for another spec (its grind changed) are
// retired unused; those on the other billing are left for its grists. A nil
// pool takes nothing.
func (p *GristPool) take(ctx context.Context, spec gristWarmSpec, billing string) *gristWarm {
	if p == nil || p.Warmer == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	key := gristPoolKey{spec.app, spec.kind, billing}
	k := p.kindOf(key)
	k.spec, k.failed = spec, false
	k.busy++
	k.demand = append(k.demand, gristDemand{at: now, inFlight: k.busy})
	var taken *gristWarm
	kept := k.idle[:0]
	for _, w := range k.idle {
		switch {
		case w.spec != spec || w.session.Exited() || p.aged(w, now):
			p.retire(w)
		case taken == nil:
			taken = w
		default:
			kept = append(kept, w)
		}
	}
	clear(k.idle[len(kept):])
	k.idle = kept
	p.fill(ctx, key, k, now)
	p.save(ctx, now, false)
	return taken
}

// kindOf is what the pool holds for key, made empty when it holds nothing.
// p.mu is held.
func (p *GristPool) kindOf(key gristPoolKey) *gristPoolKind {
	if p.kinds == nil {
		p.kinds = map[gristPoolKey]*gristPoolKind{}
	}
	k := p.kinds[key]
	if k == nil {
		k = &gristPoolKind{}
		p.kinds[key] = k
	}
	return k
}

// ended is a grist taken with spec and billing no longer busy. A nil pool
// does nothing.
func (p *GristPool) ended(ctx context.Context, spec gristWarmSpec, billing string) {
	if p == nil || p.Warmer == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if k := p.kinds[gristPoolKey{spec.app, spec.kind, billing}]; k != nil && k.busy > 0 {
		k.busy--
	}
	p.save(ctx, p.now(), false)
}

// Tick retires what the pool should no longer hold: every warm session of a
// kind quiet for the spell, any past its age, any beyond what its kind now
// wants, any that ended on its own, and every one on the key once the credits
// no longer let the key be used (unkey). It starts the replacements a kind
// still wants, and saves the board when it is due.
func (p *GristPool) Tick(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	if p.keyed() {
		if _, ok, _ := p.Credits.KeyOnly(ctx); !ok {
			p.unkey(ctx, now)
		}
	}
	for key, k := range p.kinds {
		want := p.want(k, now)
		kept := k.idle[:0]
		for _, w := range k.idle {
			switch {
			case w.session.Exited():
				k.failed = true
				p.retire(w)
			case p.aged(w, now) || len(kept) >= want:
				p.retire(w)
			default:
				kept = append(kept, w)
			}
		}
		clear(k.idle[len(kept):])
		k.idle = kept
		p.fill(ctx, key, k, now)
		if len(k.idle) == 0 && k.busy == 0 && len(k.demand) == 0 {
			delete(p.kinds, key)
		}
	}
	p.save(ctx, now, true)
}

// RetireKeyed retires every warm session started on the API key, as when the
// key is found spent, and starts their replacements on the subscription
// (unkey). A nil pool does nothing.
func (p *GristPool) RetireKeyed(ctx context.Context) {
	if p == nil || p.Warmer == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.unkey(ctx, now)
	p.save(ctx, now, false)
}

// keyed says the pool holds a kind on the API key. p.mu is held.
func (p *GristPool) keyed() bool {
	for key := range p.kinds {
		if key.billing == BillingAPI {
			return true
		}
	}
	return false
}

// unkey retires every warm session on the API key, the key being no longer
// usable, and hands each kind's demand on the key to the same kind on the
// subscription, which starts their replacements: what is ground of it next
// is the factory's own grist fallen back to the subscription, another
// licence's being answered resting until the key may be used again. p.mu is
// held.
func (p *GristPool) unkey(ctx context.Context, now time.Time) {
	for key, k := range p.kinds {
		if key.billing != BillingAPI {
			continue
		}
		for _, w := range k.idle {
			p.retire(w)
		}
		subKey := gristPoolKey{key.app, key.kind, BillingSubscription}
		sub := p.kindOf(subKey)
		if sub.spec == (gristWarmSpec{}) {
			sub.spec = k.spec
		}
		sub.demand = append(sub.demand, k.demand...)
		k.idle, k.demand = nil, nil
		p.fill(ctx, subKey, sub, now)
	}
}

// Close retires every warm session, as when the follower stops or this host
// is no longer home. The pool may be used again after.
func (p *GristPool) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, k := range p.kinds {
		for _, w := range k.idle {
			p.retire(w)
		}
		k.idle, k.demand = nil, nil
		if k.busy == 0 {
			delete(p.kinds, key)
		}
	}
	p.save(context.Background(), p.now(), false)
}

// View is what the pool holds now, by app and kind.
func (p *GristPool) View() GristPoolView {
	if p == nil {
		return GristPoolView{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return GristPoolView{At: p.now(), Kinds: p.view()}
}

// view is what the pool holds, by app and kind on every billing, sorted.
// p.mu is held.
func (p *GristPool) view() []GristPoolKind {
	at := map[gristPoolKey]int{}
	kinds := make([]GristPoolKind, 0, len(p.kinds))
	for key, k := range p.kinds {
		both := gristPoolKey{app: key.app, kind: key.kind}
		i, seen := at[both]
		if !seen {
			i, at[both] = len(kinds), len(kinds)
			kinds = append(kinds, GristPoolKind{App: key.app, Kind: key.kind})
		}
		kinds[i].Warm += len(k.idle)
		kinds[i].Busy += k.busy
	}
	slices.SortFunc(kinds, func(a, b GristPoolKind) int {
		return strings.Compare(a.App+"/"+a.Kind, b.App+"/"+b.Kind)
	})
	return kinds
}

// want is how many warm sessions kind k wants now: the most of it in flight
// at once within the quiet spell, at most Slots. It forgets demand older than
// the spell. p.mu is held.
func (p *GristPool) want(k *gristPoolKind, now time.Time) int {
	quiet := p.Quiet
	if quiet <= 0 {
		quiet = DefaultGristPoolQuiet
	}
	most := 0
	recent := k.demand[:0]
	for _, d := range k.demand {
		if now.Sub(d.at) < quiet {
			recent = append(recent, d)
			most = max(most, d.inFlight)
		}
	}
	k.demand = recent
	return max(0, min(most, p.Slots))
}

// fill starts warm sessions for k, which the pool holds as key, until it
// holds what it wants or its app and kind hold Slots on every billing,
// stopping at the first that cannot start. A kind on the key starts none
// while the credits do not let the key be used. p.mu is held.
func (p *GristPool) fill(ctx context.Context, key gristPoolKey, k *gristPoolKind, now time.Time) {
	room := func() bool { return !k.failed && len(k.idle) < p.want(k, now) && p.warm(key) < p.Slots }
	if !room() {
		return
	}
	billing := GristBilling{Billing: BillingSubscription}
	if key.billing == BillingAPI {
		var ok bool
		if billing, ok, _ = p.Credits.KeyOnly(ctx); !ok {
			return
		}
	}
	for room() {
		w, err := p.start(k.spec, billing, now)
		if err != nil {
			k.failed = true
			p.say(fmt.Sprintf("mw grist pool: a warm session for %s %s could not start, so its grist starts cold: %v", k.spec.app, k.spec.kind, err))
			return
		}
		k.idle = append(k.idle, w)
	}
}

// warm is how many warm sessions key's app and kind hold on every billing.
// p.mu is held.
func (p *GristPool) warm(key gristPoolKey) int {
	n := 0
	for _, billing := range poolBillings {
		if k := p.kinds[gristPoolKey{key.app, key.kind, billing}]; k != nil {
			n += len(k.idle)
		}
	}
	return n
}

// start starts one warm session for spec in a private directory of its own,
// on billing: with its key when it is the API key's.
func (p *GristPool) start(spec gristWarmSpec, billing GristBilling, now time.Time) (*gristWarm, error) {
	dir, err := os.MkdirTemp(p.TempDir, "mw-grist-")
	if err != nil {
		return nil, err
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	call := spec.call(dir)
	call.APIKey = billing.Key()
	session, err := p.Warmer.Warm(call)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &gristWarm{session: session, spec: spec, dir: dir, started: now, billing: billing.Billing}, nil
}

// retire ends a warm session never given a grist, and removes its directory.
func (p *GristPool) retire(w *gristWarm) {
	w.session.Retire()
	os.RemoveAll(w.dir)
}

// aged says w has waited past the age bound.
func (p *GristPool) aged(w *gristWarm, now time.Time) bool {
	most := p.MaxAge
	if most <= 0 {
		most = DefaultGristPoolMaxAge
	}
	return now.Sub(w.started) >= most
}

// save tells the board what the pool holds, when that has changed since it
// was last told or, on a tick, when the heartbeat is due. p.mu is held.
func (p *GristPool) save(ctx context.Context, now time.Time, tick bool) {
	if p.Board == nil {
		return
	}
	kinds := p.view()
	due := tick && now.Sub(p.savedAt) >= GristPoolHeartbeat
	if !due && !p.savedAt.IsZero() && slices.Equal(kinds, p.saved) {
		return
	}
	if err := p.Board.SavePool(ctx, GristPoolView{At: now, Kinds: kinds}); err != nil {
		p.say(fmt.Sprintf("mw grist pool: what the pool holds could not be saved for mw status: %v", err))
		return
	}
	p.saved, p.savedAt = kinds, now
}

// say writes msg to Err, unless it was the last thing said. p.mu is held.
func (p *GristPool) say(msg string) {
	if p.Err != nil && msg != p.said {
		fmt.Fprintln(p.Err, msg)
	}
	p.said = msg
}

func (p *GristPool) now() time.Time {
	if p.Now == nil {
		return time.Now() //mw:realclock the default of the injected Now, used where none is set
	}
	return p.Now()
}

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
// never given another, and the pool starts its replacement. A kind keeps as
// many warm sessions as it has lately had grists in flight at once, at most
// Slots, and none once it has had no grist for Quiet. A warm session older
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
	// Credits, when set, decides what each warm session is started on, and the
	// pool retires the ones started on the key when the key is spent. Nil
	// starts every session on the subscription.
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

// gristPoolKey is an app and kind.
type gristPoolKey struct{ app, kind string }

// gristPoolKind is what the pool holds for one app and kind.
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

// UseCredits sets what the pool starts its sessions on, as config [grist] says
// it now; warm sessions started on another billing are retired by the next
// grist or tick.
func (p *GristPool) UseCredits(c *GristCredits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Credits = c
}

// take is a warm session for a grist ground with spec, or nil when the pool
// has none: the grist then starts its own, cold. Either way the grist counts
// as the kind's demand and as busy until ended, and the pool starts what the
// kind now wants. Warm sessions of the kind started for another spec (its
// grind changed), or on another billing than billing (the key was spent since),
// are retired unused. A nil pool takes nothing.
func (p *GristPool) take(ctx context.Context, spec gristWarmSpec, billing string) *gristWarm {
	if p == nil || p.Warmer == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	key := gristPoolKey{spec.app, spec.kind}
	if p.kinds == nil {
		p.kinds = map[gristPoolKey]*gristPoolKind{}
	}
	k := p.kinds[key]
	if k == nil {
		k = &gristPoolKind{}
		p.kinds[key] = k
	}
	k.spec, k.failed = spec, false
	k.busy++
	k.demand = append(k.demand, gristDemand{at: now, inFlight: k.busy})
	var taken *gristWarm
	kept := k.idle[:0]
	for _, w := range k.idle {
		switch {
		case w.spec != spec || w.billing != billing || w.session.Exited() || p.aged(w, now):
			p.retire(w)
		case taken == nil:
			taken = w
		default:
			kept = append(kept, w)
		}
	}
	clear(k.idle[len(kept):])
	k.idle = kept
	p.fill(ctx, k, now)
	p.save(ctx, now, false)
	return taken
}

// ended is a grist taken with spec no longer busy. A nil pool does nothing.
func (p *GristPool) ended(ctx context.Context, spec gristWarmSpec) {
	if p == nil || p.Warmer == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if k := p.kinds[gristPoolKey{spec.app, spec.kind}]; k != nil && k.busy > 0 {
		k.busy--
	}
	p.save(ctx, p.now(), false)
}

// Tick retires what the pool should no longer hold: every warm session of a
// kind quiet for the spell, any past its age, any beyond what its kind now
// wants, and any that ended on its own. It starts the replacements a kind
// still wants, and saves the board when it is due.
func (p *GristPool) Tick(ctx context.Context) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	billing := BillingSubscription
	if len(p.kinds) > 0 {
		billing = p.Credits.Choose(ctx).Billing
	}
	for key, k := range p.kinds {
		want := p.want(k, now)
		kept := k.idle[:0]
		for _, w := range k.idle {
			switch {
			case w.session.Exited():
				k.failed = true
				p.retire(w)
			case p.aged(w, now) || len(kept) >= want || w.billing != billing:
				p.retire(w)
			default:
				kept = append(kept, w)
			}
		}
		clear(k.idle[len(kept):])
		k.idle = kept
		p.fill(ctx, k, now)
		if len(k.idle) == 0 && k.busy == 0 && len(k.demand) == 0 {
			delete(p.kinds, key)
		}
	}
	p.save(ctx, now, true)
}

// RetireKeyed retires every warm session started on the API key, as when the
// key is found spent, and starts their replacements on what the credits now
// choose. A nil pool does nothing.
func (p *GristPool) RetireKeyed(ctx context.Context) {
	if p == nil || p.Warmer == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for _, k := range p.kinds {
		kept := k.idle[:0]
		for _, w := range k.idle {
			if w.billing == BillingAPI {
				p.retire(w)
				continue
			}
			kept = append(kept, w)
		}
		clear(k.idle[len(kept):])
		k.idle = kept
		p.fill(ctx, k, now)
	}
	p.save(ctx, now, false)
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

// view is what the pool holds, sorted. p.mu is held.
func (p *GristPool) view() []GristPoolKind {
	kinds := make([]GristPoolKind, 0, len(p.kinds))
	for key, k := range p.kinds {
		kinds = append(kinds, GristPoolKind{App: key.app, Kind: key.kind, Warm: len(k.idle), Busy: k.busy})
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

// fill starts warm sessions for k until it holds what it wants, stopping at
// the first that cannot start. p.mu is held.
func (p *GristPool) fill(ctx context.Context, k *gristPoolKind, now time.Time) {
	for !k.failed && len(k.idle) < p.want(k, now) {
		w, err := p.start(ctx, k.spec, now)
		if err != nil {
			k.failed = true
			p.say(fmt.Sprintf("mw grist pool: a warm session for %s %s could not start, so its grist starts cold: %v", k.spec.app, k.spec.kind, err))
			return
		}
		k.idle = append(k.idle, w)
	}
}

// start starts one warm session for spec in a private directory of its own,
// on the API key when the credits choose it.
func (p *GristPool) start(ctx context.Context, spec gristWarmSpec, now time.Time) (*gristWarm, error) {
	dir, err := os.MkdirTemp(p.TempDir, "mw-grist-")
	if err != nil {
		return nil, err
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	billing := p.Credits.Choose(ctx)
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
		return time.Now()
	}
	return p.Now()
}

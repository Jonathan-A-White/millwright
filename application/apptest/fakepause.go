package apptest

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakePauseGit is an in-memory application.PauseGit: each worktree dir has a
// commit checked out, files not committed, and a remote branch tip.
type FakePauseGit struct {
	mu     sync.Mutex
	heads  map[string]string
	tips   map[string]string
	dirty  map[string][]string
	pushed []string
	made   int
}

// NewFakePauseGit returns a git with no worktree in it.
func NewFakePauseGit() *FakePauseGit {
	return &FakePauseGit{heads: map[string]string{}, tips: map[string]string{}, dirty: map[string][]string{}}
}

var _ application.PauseGit = (*FakePauseGit)(nil)

// Worktree makes dir a worktree at head, the remote not holding it yet.
func (g *FakePauseGit) Worktree(dir, head string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.heads[dir] = head
}

// Leave leaves files uncommitted in dir.
func (g *FakePauseGit) Leave(dir string, files ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.dirty[dir] = files
}

// BuilderPushes is what a Builder does when told to pause: it commits what is
// left and pushes.
func (g *FakePauseGit) BuilderPushes(dir string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.dirty[dir]) > 0 {
		g.made++
		g.heads[dir] = fmt.Sprintf("%s-b%d", g.heads[dir], g.made)
		g.dirty[dir] = nil
	}
	g.tips[dir] = g.heads[dir]
}

// Pushed are the dirs mw itself pushed, in order.
func (g *FakePauseGit) Pushed() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.pushed...)
}

// Head implements application.PauseGit.
func (g *FakePauseGit) Head(_ context.Context, dir string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	head, ok := g.heads[dir]
	if !ok {
		return "", fmt.Errorf("no worktree at %s", dir)
	}
	return head, nil
}

// Tip implements application.PauseGit.
func (g *FakePauseGit) Tip(_ context.Context, dir, _, _ string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	tip, ok := g.tips[dir]
	if !ok {
		return "", fmt.Errorf("the remote has no branch for %s", dir)
	}
	return tip, nil
}

// Uncommitted implements application.PauseGit.
func (g *FakePauseGit) Uncommitted(_ context.Context, dir string) ([]string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.dirty[dir]...), nil
}

// CommitLeftovers implements application.PauseGit.
func (g *FakePauseGit) CommitLeftovers(_ context.Context, dir, _ string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.made++
	g.heads[dir] = fmt.Sprintf("%s-m%d", g.heads[dir], g.made)
	g.dirty[dir] = nil
	return g.heads[dir], nil
}

// Push implements application.PauseGit.
func (g *FakePauseGit) Push(_ context.Context, dir, _, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tips[dir] = g.heads[dir]
	g.pushed = append(g.pushed, dir)
	return nil
}

// FakePauseFleet is an application.PauseFleet over hosts each backed by an
// application.FleetHostPause of its own.
type FakePauseFleet struct {
	mu         sync.Mutex
	order      []string
	hosts      map[string]application.FleetHostPause
	Unreached  map[string]bool
	PauseCalls []string
}

// NewFakePauseFleet returns a fleet with no host in it.
func NewFakePauseFleet() *FakePauseFleet {
	return &FakePauseFleet{hosts: map[string]application.FleetHostPause{}, Unreached: map[string]bool{}}
}

var _ application.PauseFleet = (*FakePauseFleet)(nil)

// Add makes host part of the fleet.
func (f *FakePauseFleet) Add(host application.FleetHostPause) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.order = append(f.order, host.Host)
	f.hosts[host.Host] = host
}

// Hosts implements application.PauseFleet.
func (f *FakePauseFleet) Hosts(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...), nil
}

func (f *FakePauseFleet) reach(host string) (application.FleetHostPause, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Unreached[host] {
		return application.FleetHostPause{}, fmt.Errorf("ssh %s: connection timed out", host)
	}
	h, ok := f.hosts[host]
	if !ok {
		return application.FleetHostPause{}, fmt.Errorf("no host %s", host)
	}
	return h, nil
}

// Pause implements application.PauseFleet.
func (f *FakePauseFleet) Pause(ctx context.Context, host string, order application.HostPauseOrder) (application.HostPaused, error) {
	h, err := f.reach(host)
	if err != nil {
		return application.HostPaused{}, err
	}
	f.mu.Lock()
	f.PauseCalls = append(f.PauseCalls, host)
	f.mu.Unlock()
	return h.Pause(ctx, order)
}

// Resume implements application.PauseFleet.
func (f *FakePauseFleet) Resume(ctx context.Context, host string, order application.HostResumeOrder) (application.HostResumed, error) {
	h, err := f.reach(host)
	if err != nil {
		return application.HostResumed{}, err
	}
	return h.Resume(ctx, order)
}

// FakePauseCloud is an application.PauseCloud over boxes named up front.
type FakePauseCloud struct {
	mu    sync.Mutex
	boxes []string
	down  []string
}

var _ application.PauseCloud = (*FakePauseCloud)(nil)

// NewFakePauseCloud returns a cloud with the boxes up.
func NewFakePauseCloud(boxes ...string) *FakePauseCloud { return &FakePauseCloud{boxes: boxes} }

// Boxes implements application.PauseCloud.
func (c *FakePauseCloud) Boxes(context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.boxes...), nil
}

// Down implements application.PauseCloud.
func (c *FakePauseCloud) Down(_ context.Context, box string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.down = append(c.down, box)
	return "destroyed " + box, nil
}

// BoxesDown are the boxes destroyed, in order.
func (c *FakePauseCloud) BoxesDown() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.down...)
}

// FakePauseConfig is an application.PauseConfig.
type FakePauseConfig struct {
	mu     sync.Mutex
	Value  string
	Set    bool
	Writes []string
}

var _ application.PauseConfig = (*FakePauseConfig)(nil)

// MaxBoxes implements application.PauseConfig.
func (c *FakePauseConfig) MaxBoxes(context.Context) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Value, c.Set, nil
}

// SetMaxBoxes implements application.PauseConfig.
func (c *FakePauseConfig) SetMaxBoxes(_ context.Context, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Value, c.Set = value, true
	c.Writes = append(c.Writes, value)
	return nil
}

// FakePauseBook is an application.PauseBook kept in memory.
type FakePauseBook struct {
	mu      sync.Mutex
	current string
	sets    map[string]application.PausedSet
	Writes  int
}

var _ application.PauseBook = (*FakePauseBook)(nil)

// NewFakePauseBook returns an empty book.
func NewFakePauseBook() *FakePauseBook {
	return &FakePauseBook{sets: map[string]application.PausedSet{}}
}

// Current implements application.PauseBook.
func (b *FakePauseBook) Current(context.Context) (string, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.current, b.current != "", nil
}

// SetCurrent implements application.PauseBook.
func (b *FakePauseBook) SetCurrent(_ context.Context, dir string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = dir
	return nil
}

// ClearCurrent implements application.PauseBook.
func (b *FakePauseBook) ClearCurrent(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.current = ""
	return nil
}

// Read implements application.PauseBook.
func (b *FakePauseBook) Read(_ context.Context, dir string) (application.PausedSet, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	set, ok := b.sets[dir]
	if !ok {
		return application.PausedSet{}, fmt.Errorf("no %s/%s", dir, application.PausedFileName)
	}
	return set, nil
}

// Write implements application.PauseBook.
func (b *FakePauseBook) Write(_ context.Context, dir string, set application.PausedSet) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sets[dir] = set
	b.Writes++
	return nil
}

// Set is the set kept in dir.
func (b *FakePauseBook) Set(dir string) (application.PausedSet, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	set, ok := b.sets[dir]
	return set, ok
}

// FakePauseTimer is an application.PauseTimer that remembers its calls.
type FakePauseTimer struct {
	mu        sync.Mutex
	Scheduled map[string]time.Time
	Cancelled []string
}

var _ application.PauseTimer = (*FakePauseTimer)(nil)

// NewFakePauseTimer returns a timer with nothing scheduled.
func NewFakePauseTimer() *FakePauseTimer {
	return &FakePauseTimer{Scheduled: map[string]time.Time{}}
}

// Schedule implements application.PauseTimer.
func (t *FakePauseTimer) Schedule(_ context.Context, at time.Time, unit string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Scheduled[unit] = at
	return unit, nil
}

// Cancel implements application.PauseTimer.
func (t *FakePauseTimer) Cancel(_ context.Context, unit string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Cancelled = append(t.Cancelled, unit)
	delete(t.Scheduled, unit)
	return nil
}

// FakePauseRetry is an application.PauseRetry that remembers what it retried.
// A story named in Unaskable is on a host that cannot be asked what its
// worktree holds, so, as mw retry does, it is refused unless forced.
type FakePauseRetry struct {
	mu        sync.Mutex
	Retried   []string
	Unaskable map[string]bool
}

var _ application.PauseRetry = (*FakePauseRetry)(nil)

// Retry implements application.PauseRetry.
func (r *FakePauseRetry) Retry(_ context.Context, story string, force bool) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Unaskable[story] && !force {
		return "", fmt.Errorf("retrying %s: its host could not be asked whether its worktree there holds uncommitted work: add --force to give the claim back without knowing", story)
	}
	r.Retried = append(r.Retried, story)
	return story + " is open again", nil
}

// FakeBurn is an application.Burn over fixed days.
type FakeBurn map[string]int

var _ application.Burn = FakeBurn(nil)

// TokensByDay implements application.Burn.
func (b FakeBurn) TokensByDay(context.Context) (map[string]int, error) { return b, nil }

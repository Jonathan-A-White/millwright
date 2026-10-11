package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of features/next_cross_slot.feature run the close-out of
// next.feature's world with a rig's merge slot across hosts: the fake tracker
// is the beads server both hosts share, and the other host is a stand-in that
// takes the same slot through the same application.CrossSlot and lands its own
// stories from its own clone of the rig to the same bare origin.

// registerNextCrossSlotSteps registers them with nextContext.
func registerNextCrossSlotSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the other host "([^"]*)" lands a story of its own each time this host's merged-result tests run, before they finish$`, c.theOtherHostLandsDuringTheGate)
	ctx.Given(`^"([^"]*)" holds the merge slot of "([^"]*)" across hosts, landing a story of its own$`, c.theOtherHostHoldsTheCrossSlot)
	ctx.Given(`^"([^"]*)" took the merge slot of "([^"]*)" across hosts on "([^"]*)" (\d+) minutes ago, and its lease ran out (\d+) minutes ago$`, c.aLeaseRanOut)
	ctx.Given(`^the beads server cannot be reached for the merge slot across hosts, saying "([^"]*)"$`, c.theBeadsServerCannotBeReached)

	ctx.Then(`^mw pushed once, losing no push to the other host$`, c.mwPushedOnceLosingNone)
	ctx.Then(`^"([^"]*)" landed (\d+) stories of its own on "([^"]*)" meanwhile$`, c.theOtherHostLandedStories)
	ctx.Then(`^the printed report says it waited for (\S+)'s landing of (\S+)$`, c.thePrintedReportSaysItWaited)
	ctx.Then(`^the printed report says "([^"]*)"$`, c.thePrintedReportSays)
	ctx.Then(`^nobody holds the merge slot of "([^"]*)" across hosts$`, c.nobodyHoldsTheCrossSlot)
}

// crossFixture is a scenario's merge slot across hosts: what this host reads
// it through, and the other host, when the scenario has one.
type crossFixture struct {
	notes application.SlotNotes
	other *otherLander
}

// crossSlot is the merge slot across hosts mw next is given: none, unless the
// scenario set one up, and then quick enough for a test.
func (c *nextContext) crossSlot() application.CrossSlot {
	if c.cross == nil {
		return application.CrossSlot{}
	}
	return quickCrossSlot(c.cross.notes, nextHost)
}

func quickCrossSlot(notes application.SlotNotes, host string) application.CrossSlot {
	return application.CrossSlot{Notes: notes, Host: host, TTL: time.Minute, Poll: 10 * time.Millisecond, Settle: 20 * time.Millisecond}
}

// crossFor is the scenario's fixture, made the first time a step asks.
func (c *nextContext) crossFor() *crossFixture {
	if c.cross == nil {
		c.cross = &crossFixture{notes: c.tracker}
	}
	return c.cross
}

// otherLander is the other host: it lands a story of its own, from its own
// clone, under the rig's merge slot across hosts, whenever this host's
// merged-result tests begin — and, in the scenario that says so, it is in the
// middle of landing one when this host's close-out begins, which it finishes
// as soon as this host is seen waiting for it, or its tests begin.
type otherLander struct {
	c     *nextContext
	host  string
	slot  application.CrossSlot
	gates string // the file this host's merged-result tests write a line in each time they begin

	mu      sync.Mutex
	holding *application.CrossHolding // the landing in progress when this host's close-out began
	served  int                       // how many of this host's merged-result test runs it has landed a story during
	landed  int
	busy    bool
	err     error

	seen     chan struct{} // closed once this host has read the slot and found it held by this one
	seenOnce sync.Once

	stop context.CancelFunc
	done chan struct{}
}

// theOtherHostLandsDuringTheGate makes this host's tests write down each time
// they run on a merged result, and wait — a while, as the desktop's gate is
// long — for the other host to land a story meanwhile; and it starts the other
// host, which does.
func (c *nextContext) theOtherHostLandsDuringTheGate(host string) error {
	gates := filepath.Join(c.root, "gates.log")
	script := filepath.Join(c.root, "slow-gate.sh")
	body := fmt.Sprintf(`#!/bin/sh
printf 'run\n' >> %[1]s
case "$(pwd)" in
*/%[3]s*)
  printf 'gate\n' >> %[2]s
  n=$(wc -l < %[2]s | tr -d ' ')
  i=0
  while [ ! -f %[2]s.$n.landed ] && [ $i -lt 200 ]; do sleep 0.01; i=$((i+1)); done
  ;;
esac
`, c.checkLog, gates, application.LandingPrefix)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		return err
	}
	c.checkCommand = "sh " + script

	fixture := c.crossFor()
	ctx, stop := context.WithCancel(context.Background())
	other := &otherLander{
		c: c, host: host, slot: quickCrossSlot(c.tracker, host), gates: gates,
		seen: make(chan struct{}), stop: stop, done: make(chan struct{}),
	}
	fixture.other = other
	fixture.notes = watchedNotes{SlotNotes: c.tracker, held: func(lease application.CrossSlotLease) {
		if lease.Host == host {
			other.seenOnce.Do(func() { close(other.seen) })
		}
	}}
	go other.run(ctx)
	return nil
}

// theOtherHostHoldsTheCrossSlot has the other host take the rig's merge slot
// across hosts for a landing it has not finished.
func (c *nextContext) theOtherHostHoldsTheCrossSlot(host, rigName string) error {
	other := c.crossFor().other
	if other == nil || other.host != host {
		return fmt.Errorf("no other host %q is landing in this scenario", host)
	}
	holding, said := other.slot.Take(context.Background(), rigName, application.Holders(nextSeat, host, "mw-other.1"))
	if holding == nil {
		return fmt.Errorf("the other host could not take the merge slot of %s: %v", rigName, said)
	}
	other.mu.Lock()
	other.holding = holding
	other.mu.Unlock()
	return nil
}

// stopTheOtherLander stops the other host, waiting for it to be gone, so that
// nothing of it outlives the scenario's directory.
func (c *nextContext) stopTheOtherLander() {
	if c.cross == nil || c.cross.other == nil {
		return
	}
	c.cross.other.stop()
	<-c.cross.other.done
}

func (l *otherLander) run(ctx context.Context) {
	defer close(l.done)
	for ctx.Err() == nil {
		gates := countLines(l.gates)
		l.mu.Lock()
		holding := l.holding
		pending := gates > l.served
		l.mu.Unlock()

		seen := false
		select {
		case <-l.seen:
			seen = true
		default:
		}

		switch {
		case holding != nil && (seen || pending):
			l.land(ctx, holding, pending)
		case pending:
			l.mu.Lock()
			l.busy = true
			l.mu.Unlock()
			taken, _ := l.slot.Take(ctx, l.c.rigKey(), application.Holders(nextSeat, l.host, fmt.Sprintf("mw-other.%d", l.count()+1)))
			if taken == nil {
				l.mu.Lock()
				l.busy = false
				l.mu.Unlock()
				continue
			}
			l.land(ctx, taken, true)
		default:
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
}

// land pushes one story of the other host's to the origin, gives its slot back,
// and, when it landed during this host's merged-result tests, lets them finish.
func (l *otherLander) land(ctx context.Context, holding *application.CrossHolding, duringGate bool) {
	err := l.push()
	holding.Release(context.Background())
	l.mu.Lock()
	defer l.mu.Unlock()
	if holding == l.holding {
		l.holding = nil
	}
	l.busy = false
	if err != nil {
		l.err = errors.Join(l.err, err)
		return
	}
	l.landed++
	if duringGate {
		l.served++
		_ = os.WriteFile(fmt.Sprintf("%s.%d.landed", l.gates, l.served), nil, 0o644)
	}
}

func (l *otherLander) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.landed
}

// push commits one story of the other host's in its own clone, on top of
// whatever the origin has now, and pushes it.
func (l *otherLander) push() error {
	seed := l.c.seed
	n := l.count() + 1
	if err := gitRun(seed, "git", "pull", "-q", "--rebase", "origin", "main"); err != nil {
		return err
	}
	file := fmt.Sprintf("%s-%d.md", l.host, n)
	if err := os.WriteFile(filepath.Join(seed, file), []byte(fmt.Sprintf("story %d of %s\n", n, l.host)), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "-A"}, {"commit", "-qm", fmt.Sprintf("%s lands story %d", l.host, n)}, {"push", "-q", "origin", "main"},
	} {
		if err := gitRun(seed, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

// idle waits for the other host to have nothing left to do: no landing in
// progress and none owed to a run of this host's tests.
func (l *otherLander) idle() error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		quiet := l.holding == nil && !l.busy && countLines(l.gates) <= l.served
		err := l.err
		l.mu.Unlock()
		if err != nil {
			return fmt.Errorf("the other host could not land: %w", err)
		}
		if quiet {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("the other host was still landing after 15s")
}

// countLines is how many lines a file holds; none when it is not there.
func countLines(path string) int {
	said, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(said), "\n")
}

// watchedNotes is the shared store as this host reads it, telling the scenario
// whenever this host finds the slot held.
type watchedNotes struct {
	application.SlotNotes
	held func(application.CrossSlotLease)
}

func (w watchedNotes) Note(ctx context.Context, key string) (string, error) {
	note, err := w.SlotNotes.Note(ctx, key)
	if err == nil {
		if lease, ok := application.ReadCrossSlotLease(note); ok {
			w.held(lease)
		}
	}
	return note, err
}

// unreachableNotes is a beads server that does not answer.
type unreachableNotes struct{ err error }

func (u unreachableNotes) Note(context.Context, string) (string, error) { return "", u.err }
func (u unreachableNotes) SetNote(context.Context, string, string) error {
	return u.err
}
func (u unreachableNotes) ClearNote(context.Context, string) error { return u.err }

// aLeaseRanOut leaves a lease on the slot whose holder went away: taken that
// long ago, and run out since.
func (c *nextContext) aLeaseRanOut(holder, rigName, host string, taken, ranOut int) error {
	now := time.Now()
	lease := application.CrossSlotLease{
		Holder: holder, Host: host, Token: "gone-away",
		Taken: now.Add(-time.Duration(taken) * time.Minute), Expires: now.Add(-time.Duration(ranOut) * time.Minute),
	}
	c.crossFor()
	return c.tracker.SetNote(context.Background(), application.CrossSlotKey(rigName), lease.Note())
}

func (c *nextContext) theBeadsServerCannotBeReached(said string) error {
	c.crossFor().notes = unreachableNotes{err: errors.New(said)}
	return nil
}

func (c *nextContext) mwPushedOnceLosingNone() error {
	if c.report.Reason != "" || !c.report.Landed {
		return fmt.Errorf("expected a landing, got %s: %s (err %v)\n%s", c.report.Reason, c.report.Why, c.err, c.printed.String())
	}
	if c.report.Pushes != 1 {
		return fmt.Errorf("expected one push, got %d\n%s", c.report.Pushes, c.printed.String())
	}
	return nil
}

func (c *nextContext) theOtherHostLandedStories(host string, want int, branch string) error {
	other := c.crossFor().other
	if other == nil {
		return fmt.Errorf("no other host is landing in this scenario")
	}
	if err := other.idle(); err != nil {
		return err
	}
	listed, err := gitSay(c.origin(), "ls-tree", "--name-only", branch)
	if err != nil {
		return err
	}
	got := 0
	for _, name := range strings.Split(listed, "\n") {
		if strings.HasPrefix(name, host+"-") {
			got++
		}
	}
	if got != want {
		return fmt.Errorf("expected %d stories of %s on %s at the origin, found %d:\n%s", want, host, branch, got, listed)
	}
	return nil
}

func (c *nextContext) thePrintedReportSaysItWaited(host, rigName string) error {
	pattern := regexp.MustCompile(`waited \d+[sm] for ` + regexp.QuoteMeta(host+"'s landing of "+rigName))
	if !pattern.MatchString(c.printed.String()) {
		return fmt.Errorf("expected the report to say it waited for %s's landing of %s, it printed:\n%s", host, rigName, c.printed.String())
	}
	return nil
}

func (c *nextContext) thePrintedReportSays(said string) error {
	if !strings.Contains(c.printed.String(), said) {
		return fmt.Errorf("expected the report to say %q, it printed:\n%s", said, c.printed.String())
	}
	return nil
}

func (c *nextContext) nobodyHoldsTheCrossSlot(rigName string) error {
	if other := c.crossFor().other; other != nil {
		if err := other.idle(); err != nil {
			return err
		}
	}
	note, err := c.tracker.Note(context.Background(), application.CrossSlotKey(rigName))
	if err != nil {
		return err
	}
	if note != "" {
		return fmt.Errorf("expected nobody to hold the merge slot of %s across hosts, it says %s", rigName, note)
	}
	return nil
}

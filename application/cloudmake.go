package application

import (
	"context"
	"fmt"
	"io"
	"time"
)

// CloudMake is the make of one box the cloud check began (mw cloud make, the
// process CloudProvider.Begin starts, in a session and cgroup of its own). It
// runs the provider's Up, which takes minutes, with a deadline of the
// provider's own and none from any caller, and writes how it ended into the
// book and the cloud events. It, not the check, owns the box from the moment
// the check writes it into the book as being made.
//
// A box that does not come up is destroyed and tried once more; when that
// fails too, no box is made for CloudFailHold. The book is changed only under
// Lock, never while the provider works, so a check is never held up by a make.
type CloudMake struct {
	Provider CloudProvider
	Book     CloudBook
	Log      EventLog
	Host     string
	Plan     CloudPlan
	Now      func() time.Time
	Out      io.Writer
	// Lock takes the lock a cloud check holds while it runs, waiting for it,
	// and returns the release. Nil takes none.
	Lock func(ctx context.Context) (release func(), err error)
}

// Run makes the box name, and returns once the book says how it ended.
func (m CloudMake) Run(ctx context.Context, name string) error {
	if m.Provider == nil || m.Book == nil {
		return fmt.Errorf("making a cloud box needs a provider and the book")
	}
	for try := 1; try <= 2; try++ {
		if !m.begin(ctx, name, try) {
			return nil
		}
		started := m.now()
		upErr := m.Provider.Up(ctx, name)
		done, err := m.settle(ctx, name, try, started, upErr)
		if done {
			return err
		}
	}
	return nil
}

func (m CloudMake) now() time.Time {
	if m.Now == nil {
		return time.Now().UTC()
	}
	return m.Now().UTC()
}

// locked reads the book under the lock, lets change work on it and keeps what
// it did.
func (m CloudMake) locked(ctx context.Context, change func(p *cloudPass)) error {
	if m.Lock != nil {
		release, err := m.Lock(ctx)
		if err != nil {
			return fmt.Errorf("taking the cloud lock: %w", err)
		}
		defer release()
	}
	state, err := m.Book.Read(ctx)
	if err != nil {
		return fmt.Errorf("reading the cloud's book: %w", err)
	}
	p := &cloudPass{check: CloudCheck{Provider: m.Provider, Book: m.Book, Log: m.Log, Host: m.Host, Plan: m.Plan, Now: m.Now, Out: m.Out}, now: m.now(), state: state}
	p.roll()
	p.billed = p.askBilled(ctx)
	change(p)
	if p.dirty != "" {
		p.keep(ctx, p.dirty)
	}
	return p.err
}

// begin has the book hold name as being made before the provider is asked for
// it, the first try by the check and the second here, and reports whether to
// go on: a box the month cannot pay for, or one the book has lost, is not.
func (m CloudMake) begin(ctx context.Context, name string, try int) (goOn bool) {
	err := m.locked(ctx, func(p *cloudPass) {
		for _, box := range p.state.Boxes {
			if box.Name == name {
				goOn = true
				return
			}
		}
		if try == 1 {
			// The check's own entry is gone (given up as stale): the box is
			// not this make's to bring up.
			return
		}
		if p.spent()+m.Plan.HourlyUSD*float64(len(p.state.Boxes)+2) > m.Plan.MonthlyCapUSD {
			p.capReached(ctx, "no box is made")
			return
		}
		p.state.Boxes = append(p.state.Boxes, CloudBox{Name: name, Up: p.now, IdleSince: p.now, Making: true})
		p.dirty = "making " + name
		goOn = true
	})
	if err != nil {
		m.say("%v", err)
		return false
	}
	return goOn
}

// settle writes how one try ended, and reports whether the make is over.
func (m CloudMake) settle(ctx context.Context, name string, try int, started time.Time, upErr error) (done bool, err error) {
	err = m.locked(ctx, func(p *cloudPass) {
		if upErr == nil {
			found := false
			for i := range p.state.Boxes {
				if p.state.Boxes[i].Name == name {
					p.state.Boxes[i].Making, found = false, true
				}
			}
			if !found {
				p.state.Boxes = append(p.state.Boxes, CloudBox{Name: name, Up: started, IdleSince: p.now})
			}
			p.move(ctx, fmt.Sprintf("up %s: made in %s", name, Clock(p.now.Sub(started))))
			p.dirty = "up " + name
			done = true
			return
		}
		failed := fmt.Sprintf("failed %s: %v", name, oneLine(upErr.Error()))
		for i := range p.state.Boxes {
			if p.state.Boxes[i].Name == name {
				p.state.Boxes[i].Making = false
			}
		}
		if !p.down(ctx, name, "it did not come up") {
			p.move(ctx, failed+"; it could not be destroyed either, so no box is made until it is")
			p.dirty = failed
			done = true
			return
		}
		if try == 1 {
			p.move(ctx, failed+"; destroyed, and tried once more")
		} else {
			p.state.HeldUntil = p.now.Add(CloudFailHold)
			p.move(ctx, failed+fmt.Sprintf(" again; destroyed, and no box is made before %s", p.state.HeldUntil.Format("15:04Z")))
			done = true
		}
		p.dirty = failed
	})
	if err != nil {
		m.say("%v", err)
		return true, err
	}
	return done, nil
}

func (m CloudMake) say(format string, args ...any) {
	if m.Out != nil {
		fmt.Fprintf(m.Out, "mw cloud: "+format+"\n", args...)
	}
}

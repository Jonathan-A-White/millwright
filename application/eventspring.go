package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/domain/events"
)

// HeartbeatReason is the detail of the scheduled event of a pass nothing but
// the heartbeat sprang.
const HeartbeatReason = "heartbeat"

// SpringInterruptedDetail is the detail of the done event of a pass the
// follower was stopped in the middle of, for the restart a landing of the
// factory rig brings (mw-gq6.242): the pass is not a failure, and the follower
// that comes back runs it again.
const SpringInterruptedDetail = "cut short by the follower stopping; run again when it is back"

// SpringRestartedReason is the detail of the scheduled event of a pass run
// again because the follower before this one stopped in the middle of it.
const SpringRestartedReason = "run again after the follower restarted"

// springLookback is how many of the log's last events the first Spring reads
// to find a pass its predecessor was cut short in.
const springLookback = 200

// ClockReason is the detail of the scheduled event of a pass on a job's own
// clock.
const ClockReason = "clock"

// EventSpringer is what EventFollow calls at the end of each pass to spring
// the factory's jobs: EventSpring.
type EventSpringer interface {
	Spring(ctx context.Context) error
}

// SpringJob is one job the follower runs when an event says it is wanted, or
// when its clock says so.
type SpringJob struct {
	// Name is the job's name in the log: its events' actor is Name@host.
	Name string
	// Wants says whether an event springs the job, and why in a few words.
	// Nil is no event.
	Wants func(e events.Event) (reason string, ok bool)
	// Probe, when set, is asked once each Spring, and springs the job when it
	// says so, as an event would: for what the log does not hold, such as a
	// record that reached the postern backend. The first Spring asks it too,
	// to take its bearings, and springs nothing from the answer.
	Probe func(ctx context.Context) (reason string, ok bool)
	// Every is how long the job may go unrun before it is run for Reason,
	// counted from its last pass of any cause; zero is never.
	Every  time.Duration
	Reason string
	// Run does one pass and returns when it is over: a pass that returns an
	// error is a failed job.
	Run func(ctx context.Context) error
}

// DispatchJob is the dispatch pass (mw dispatch): sprung by a bead that was
// opened or has landed, the two moves that make a story ready or free a seat,
// with the heartbeat as fallback. A step of a story's molecule opening is no
// reason: a pass pours those itself.
func DispatchJob(heartbeat time.Duration, run func(context.Context) error) SpringJob {
	return SpringJob{
		Name:   "dispatch",
		Every:  heartbeat,
		Reason: HeartbeatReason,
		Run:    run,
		Wants: func(e events.Event) (string, bool) {
			if e.Kind != events.KindBeadChanged || e.From == e.To || strings.Contains(e.Bead, "-mol-") {
				return "", false
			}
			switch e.To {
			case events.BeadOpen:
				return "bead " + e.Bead + " opened", true
			case events.BeadLanded:
				return "bead " + e.Bead + " landed", true
			}
			return "", false
		},
	}
}

// MillhandTickJob is the Millhand's tick (mw millhand tick): sprung by an
// alarm (an event in the emergency lane), mail for the Millhand, or a failed
// doctor, with the heartbeat as fallback.
func MillhandTickJob(heartbeat time.Duration, run func(context.Context) error) SpringJob {
	return SpringJob{
		Name:   "millhand-tick",
		Every:  heartbeat,
		Reason: HeartbeatReason,
		Run:    run,
		Wants: func(e events.Event) (string, bool) {
			switch {
			case e.Lane == events.LaneEmergency:
				return "alarm", true
			case e.Kind == events.KindMail && e.Detail == MillhandSeat:
				return "mail for the Millhand", true
			case e.Kind == events.KindJob && e.To == events.JobFailed && strings.HasPrefix(e.Actor, "doctor@"):
				return "doctor failed", true
			}
			return "", false
		},
	}
}

// ClockJob is a job no event springs: it runs every d, on the follower's own
// clock.
func ClockJob(name string, every time.Duration, run func(context.Context) error) SpringJob {
	return SpringJob{Name: name, Every: every, Reason: ClockReason, Run: run}
}

// EventSpring is how an idle factory costs nothing (mw-6ww.55, Q7 rules 1
// and 3): the jobs the timers used to start every few minutes are started by
// the events that make them worth running. Each Spring reads the log's events
// since the last and, for each job one of them springs, or whose clock is up,
// runs a pass.
//
// A job is in flight at most once. An event that springs a job while its pass
// runs does not start another beside it, and is not lost: one more pass
// follows when the first ends, which is what sees a story opened a moment
// after the first pass read the ready list. A pass says itself in the log as a
// job event, actor <name>@<host>: scheduled (detail: why), running, then done
// or failed (detail: the failure).
//
// A pass that ends in an error while the follower is being stopped (a restart
// after a landing terminates the systemctl it was waiting on) is not a failure:
// it is written done, with SpringInterruptedDetail, and the first Spring of the
// follower that comes back runs it again.
//
// The first Spring otherwise only reads where the log stands: history springs
// nothing, and a job's clock runs from then. A failure to write a job event is said on
// Err and the pass goes on.
type EventSpring struct {
	Log  EventLog
	Host string
	Jobs []SpringJob
	// Now is the clock the events and the jobs' clocks are read by; nil is
	// time.Now.
	Now func() time.Time
	// Err is where failures are said.
	Err io.Writer
	// Settle is how long a pass that ended in an error waits to see whether the
	// follower is being stopped: the units' stop terminates a pass's systemctl
	// a moment before the follower hears its own signal. Zero waits not at all.
	Settle time.Duration

	mu      sync.Mutex
	started bool
	seen    uint64
	jobs    map[string]*springState
	wg      sync.WaitGroup
}

// springState is what EventSpring knows of one job.
type springState struct {
	last     string // the job's state in the job machine, as last written
	lastRun  time.Time
	inFlight bool
	again    string // why one more pass is wanted when this ends
}

// Spring springs the jobs the log's new events and the clocks want. It
// returns once the passes are begun: it never waits for one.
func (s *EventSpring) Spring(ctx context.Context) error {
	if s.Log == nil || s.Host == "" {
		return fmt.Errorf("springing the jobs needs an event log and a host")
	}
	head, err := s.Log.Head(ctx)
	if err != nil {
		return fmt.Errorf("reading the event log's head: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if !s.started {
		s.started = true
		s.seen = head
		s.jobs = map[string]*springState{}
		interrupted := s.interruptedPasses(ctx, head)
		for _, j := range s.Jobs {
			st := &springState{last: events.Start, lastRun: now}
			s.jobs[j.Name] = st
			if interrupted[j.Name] {
				st.last = events.JobDone
				s.launch(ctx, j, st, SpringRestartedReason)
			}
		}
		for _, j := range s.Jobs {
			if j.Probe != nil {
				j.Probe(ctx)
			}
		}
		return nil
	}
	var evs []events.Event
	if head > s.seen {
		if evs, err = s.Log.Since(ctx, s.seen); err != nil {
			return fmt.Errorf("reading the events since %d: %w", s.seen, err)
		}
		s.seen = head
	}
	for _, j := range s.Jobs {
		st := s.jobs[j.Name]
		reason, wanted := "", false
		if j.Wants != nil {
			for _, e := range evs {
				if reason, wanted = j.Wants(e); wanted {
					break
				}
			}
		}
		if !wanted && j.Probe != nil {
			reason, wanted = j.Probe(ctx)
		}
		switch {
		case wanted && st.inFlight:
			st.again = reason
		case wanted:
			s.launch(ctx, j, st, reason)
		case !st.inFlight && j.Every > 0 && now.Sub(st.lastRun) >= j.Every:
			s.launch(ctx, j, st, j.Reason)
		}
	}
	return nil
}

// Wait returns when every pass begun has ended.
func (s *EventSpring) Wait() { s.wg.Wait() }

// launch writes the pass's scheduled event and begins it. s.mu is held.
func (s *EventSpring) launch(ctx context.Context, j SpringJob, st *springState, reason string) {
	st.inFlight = true
	st.lastRun = s.now()
	s.say(j, s.write(ctx, j, st, events.JobScheduled, reason))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.mu.Lock()
		s.say(j, s.write(ctx, j, st, events.JobRunning, ""))
		s.mu.Unlock()
		err := j.Run(ctx)
		cutShort := err != nil && s.stopping(ctx)
		s.mu.Lock()
		defer s.mu.Unlock()
		if cutShort {
			s.say(j, s.write(ctx, j, st, events.JobDone, SpringInterruptedDetail))
		} else if err != nil {
			s.say(j, s.write(ctx, j, st, events.JobFailed, clippedTo(oneLine(err.Error()), DispatchLogReasonLimit)))
		} else {
			s.say(j, s.write(ctx, j, st, events.JobDone, ""))
		}
		st.inFlight = false
		if again := st.again; again != "" && ctx.Err() == nil {
			st.again = ""
			s.launch(ctx, j, st, again)
		}
	}()
}

// stopping says whether the follower is being stopped, waiting up to Settle
// for it to be.
func (s *EventSpring) stopping(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	if s.Settle <= 0 {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	case <-time.After(s.Settle):
		return false
	}
}

// interruptedPasses names the jobs whose last event of this host, among the
// log's last springLookback up to head, is a pass cut short. A log that cannot
// be read says there are none: the heartbeat runs the job anyway.
func (s *EventSpring) interruptedPasses(ctx context.Context, head uint64) map[string]bool {
	from := uint64(0)
	if head > springLookback {
		from = head - springLookback
	}
	recent, err := s.Log.Since(ctx, from)
	if err != nil {
		return nil
	}
	last := map[string]events.Event{}
	for _, e := range recent {
		if e.Kind == events.KindJob {
			last[e.Actor] = e
		}
	}
	out := map[string]bool{}
	for _, j := range s.Jobs {
		if e, ok := last[j.Name+"@"+s.Host]; ok && e.To == events.JobDone && e.Detail == SpringInterruptedDetail {
			out[j.Name] = true
		}
	}
	return out
}

// write puts one job event in the log, the transition from the job's last
// state to state, and moves the job's state on whether or not it was written.
func (s *EventSpring) write(ctx context.Context, j SpringJob, st *springState, state, detail string) error {
	from := st.last
	st.last = state
	_, err := EventEmit{
		Log: s.Log,
		Now: s.now,
		Event: events.Event{
			Kind:   events.KindJob,
			Actor:  j.Name + "@" + s.Host,
			From:   from,
			To:     state,
			Detail: detail,
		},
	}.Run(ctx)
	return err
}

func (s *EventSpring) say(j SpringJob, err error) {
	if err != nil && s.Err != nil {
		fmt.Fprintf(s.Err, "mw events follow: writing %s's job event: %v\n", j.Name, err)
	}
}

func (s *EventSpring) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

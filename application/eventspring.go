package application

import (
	"context"
	"errors"
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

// SpringLeftRunningDetail is the detail of the done event of a pass whose
// work runs in a process apart from the follower (a landing, the nightly
// suite), when the follower stops while it waits: the work goes on, and the
// follower that comes back waits for it and tells its outcome (mw-gq6.470).
const SpringLeftRunningDetail = "the follower stopped while it waited; the work runs on apart from it, and the follower that comes back waits for it"

// ErrLeftRunning marks the error of a pass whose wait the follower stopping
// ended while its work runs on apart from the follower: such a pass is written
// done with SpringLeftRunningDetail.
var ErrLeftRunning = errors.New("left running apart from the follower")

// SkippedPassError is the error of a pass that found nothing to do and made
// nothing; the pass is written done with Why as its detail, so that a done
// event with no output says why (mw-gq6.590).
type SkippedPassError struct{ Why string }

func (e *SkippedPassError) Error() string { return e.Why }

// SkippedPass is the error a job's Run returns when it ends without making what
// it is for, with why in a few words.
func SkippedPass(why string) error { return &SkippedPassError{Why: why} }

// LeftRunning is err, from a wait that ctx ending cut short, marked
// ErrLeftRunning when ctx has ended.
func LeftRunning(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	return fmt.Errorf("%w: %w", ErrLeftRunning, err)
}

// unmarkDayIfCutShort is err, the end of a once-a-day pass that marked its day
// begun before it ran. A pass the follower's stopping cut short is not that
// day's pass: the mark is taken back (and reset clears the job's own memory of
// it), so that the follower that comes back runs the pass again in full and not
// as a no-op that finds the day marked (mw-gq6.590). A pass that failed with the
// follower running keeps its mark: it costs fuel, so it is made the next day.
func unmarkDayIfCutShort(ctx context.Context, notes interface {
	SetNote(ctx context.Context, key, value string) error
}, key string, err error, reset func()) error {
	if err == nil || ctx.Err() == nil {
		return err
	}
	reset()
	if setErr := notes.SetNote(context.WithoutCancel(ctx), key, ""); setErr != nil {
		return fmt.Errorf("%w (and taking back the day's mark failed: %v)", err, setErr)
	}
	return err
}

// SpringRestartedReason is the detail of the scheduled event of a pass run
// again because the follower before this one stopped in the middle of it.
const SpringRestartedReason = "run again after the follower restarted"

// springLookback is how many of the log's last events the first Spring reads
// to find a pass its predecessor was cut short in.
const springLookback = 200

// SpringClockLookback is how many of the log's newest events the first Spring
// reads to seed each clock job from its own last pass (mw-gq6.539): the
// follower must not scan the whole log at start. A clock job whose last pass is
// older than this window counts as due.
const SpringClockLookback = 1000

// TellFailureEvery is how long a job with TellFailure is quiet after it has
// mailed the Mayor of a failed pass: a job that fails on every pass is told of
// once a day, not on each.
const TellFailureEvery = 24 * time.Hour

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
	// to take its bearings, and springs nothing from the answer, unless
	// ProbeFirst.
	Probe func(ctx context.Context) (reason string, ok bool)
	// ProbeFirst makes the first Spring act on the Probe's answer as any
	// other: for a job whose pass is safe to run on what was already waiting.
	ProbeFirst bool
	// Watch, when set, is started in a goroutine of its own at the first
	// Spring and runs until ctx ends: it looks for what the job is wanted for
	// at its own pace, not once in each Spring, and says wake(reason) when it
	// finds it. The job is sprung as an event would spring it: begun at once,
	// or once more when the pass in flight ends.
	Watch func(ctx context.Context, wake func(reason string))
	// Every is how long the job may go unrun before it is run for Reason,
	// counted from its last pass of any cause; zero is never.
	Every  time.Duration
	Reason string
	// TellFailure makes a failed pass mail the Mayor (mw-gq6.531), at most once
	// in TellFailureEvery: for the jobs on a clock of a day or longer, whose
	// failure nobody else sees.
	TellFailure bool
	// About, when set, says in a few words when the job runs, for the
	// listing of mw events jobs.
	About string
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

// BeadsFlushRecord is what the home leaves of its last flush: when, and why it
// failed, empty when it got through.
type BeadsFlushRecord struct {
	At     time.Time
	Failed string
}

// BeadsFlushLog is where the last flush is kept, for mw doctor's beads-flush
// check to read. Last is the zero record when there has been none.
type BeadsFlushLog interface {
	Last(ctx context.Context) (BeadsFlushRecord, error)
	Record(ctx context.Context, record BeadsFlushRecord) error
}

// BeadsFlush is one pass of the home's flush job: flush, and leave the record
// of it. A working set that was clean is a pass.
type BeadsFlush struct {
	Tracker TrackerFlush
	Log     BeadsFlushLog
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Run flushes once. A failed flush is recorded and returned.
func (f BeadsFlush) Run(ctx context.Context) error {
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	at := now()
	_, err := f.Tracker.Flush(ctx, at)
	record := BeadsFlushRecord{At: at}
	if err != nil {
		record.Failed = firstLine(err.Error())
	}
	if logErr := f.Log.Record(ctx, record); logErr != nil && err == nil {
		return fmt.Errorf("recording the beads flush: %w", logErr)
	}
	return err
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
// follower that comes back runs it again. A pass whose work runs apart from the
// follower and goes on (its error is ErrLeftRunning) is written done with
// SpringLeftRunningDetail instead, and is run again the same way: the pass of
// the follower that comes back finds that work and waits for it.
//
// The first Spring otherwise only reads where the log stands: history springs
// no pass for an event, and the clock of a job with a heartbeat runs from then.
// The clock of a ClockJob runs from its own last pass of this host in the log
// instead, since the follower restarts on every landing of the factory rig and
// would else never reach a period longer than the gap between two landings
// (mw-gq6.539): a job with no pass in the last SpringClockLookback events, or
// one older than its period, is run by the first Spring. A failure to write a
// job event is said on Err and the pass goes on.
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
	// Mailbox is where the Mayor is told of a failed pass of a job with
	// TellFailure; nil tells no one.
	Mailbox Mailbox

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
	again    string    // why one more pass is wanted when this ends
	told     time.Time // when the Mayor was last mailed of a failed pass
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
		passes := s.lastPasses(ctx, head)
		for _, j := range s.Jobs {
			st := &springState{last: events.Start, lastRun: now}
			s.jobs[j.Name] = st
			switch {
			case interrupted[j.Name]:
				st.last = events.JobDone
				s.launch(ctx, j, st, SpringRestartedReason)
			case j.Reason == ClockReason && j.Every > 0:
				// A clock job runs from its own last pass, not from this start:
				// the follower restarts on every landing (mw-gq6.539).
				if at, ok := passes[j.Name]; ok && now.Sub(at) < j.Every {
					st.lastRun = at
				} else {
					s.launch(ctx, j, st, j.Reason)
				}
			}
		}
		for _, j := range s.Jobs {
			if j.Probe == nil {
				continue
			}
			if reason, wanted := j.Probe(ctx); wanted && j.ProbeFirst {
				s.want(ctx, j, s.jobs[j.Name], reason)
			}
		}
		for _, j := range s.Jobs {
			if j.Watch != nil {
				go j.Watch(ctx, func(reason string) { s.wake(ctx, j, reason) })
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
		case wanted:
			s.want(ctx, j, st, reason)
		case !st.inFlight && j.Every > 0 && now.Sub(st.lastRun) >= j.Every:
			s.launch(ctx, j, st, j.Reason)
		}
	}
	return nil
}

// want springs j for reason: begun at once, or once more when the pass in
// flight ends. s.mu is held.
func (s *EventSpring) want(ctx context.Context, j SpringJob, st *springState, reason string) {
	if st.inFlight {
		st.again = reason
	} else {
		s.launch(ctx, j, st, reason)
	}
}

// wake is what a job's Watch says to spring it, between two Springs.
func (s *EventSpring) wake(ctx context.Context, j SpringJob, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.jobs[j.Name]; st != nil && ctx.Err() == nil {
		s.want(ctx, j, st, reason)
	}
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
		var skipped *SkippedPassError
		isSkip := errors.As(err, &skipped)
		cutShort := err != nil && !isSkip && s.stopping(ctx)
		s.mu.Lock()
		tell := false
		if isSkip {
			s.say(j, s.write(ctx, j, st, events.JobDone, clippedTo(oneLine(skipped.Why), DispatchLogReasonLimit)))
		} else if cutShort && errors.Is(err, ErrLeftRunning) {
			s.say(j, s.write(ctx, j, st, events.JobDone, SpringLeftRunningDetail))
		} else if cutShort {
			s.say(j, s.write(ctx, j, st, events.JobDone, SpringInterruptedDetail))
		} else if err != nil {
			s.say(j, s.write(ctx, j, st, events.JobFailed, clippedTo(oneLine(err.Error()), DispatchLogReasonLimit)))
			tell = s.dueToTell(j, st)
		} else {
			s.say(j, s.write(ctx, j, st, events.JobDone, ""))
		}
		st.inFlight = false
		if again := st.again; again != "" && ctx.Err() == nil {
			st.again = ""
			s.launch(ctx, j, st, again)
		}
		s.mu.Unlock()
		if tell {
			s.tell(ctx, j, err)
		}
	}()
}

// dueToTell says whether a failed pass of j is to be mailed to the Mayor: j
// asks for it, there is a mailbox, and the Mayor was not mailed of j within
// TellFailureEvery. It marks the Mayor as told. s.mu is held.
func (s *EventSpring) dueToTell(j SpringJob, st *springState) bool {
	now := s.now()
	if !j.TellFailure || s.Mailbox == nil || (!st.told.IsZero() && now.Sub(st.told) < TellFailureEvery) {
		return false
	}
	st.told = now
	return true
}

// tell mails the Mayor that a pass of j failed, as the job: the subject names
// the job and the host and the body is the failure, as the units' mail does
// (contrib/systemd/mw-unit-failed@.service). A mail that cannot be sent is said
// on Err. s.mu is not held: sending reaches the tracker.
func (s *EventSpring) tell(ctx context.Context, j SpringJob, failure error) {
	_, err := s.Mailbox.Send(context.WithoutCancel(ctx), NewMessage{
		From:    SeatIdentity(j.Name, s.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("job failed: %s on %s", j.Name, s.Host),
		Body:    strings.TrimSpace(failure.Error()),
	})
	if err != nil && s.Err != nil {
		fmt.Fprintf(s.Err, "mw events follow: mailing %s that %s failed: %v\n", MayorMailbox, j.Name, err)
	}
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
// log's last springLookback up to head, is a pass cut short or left running
// apart. A log that cannot be read says there are none: the heartbeat runs the
// job anyway.
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
		if e, ok := last[j.Name+"@"+s.Host]; ok && e.To == events.JobDone && (e.Detail == SpringInterruptedDetail || e.Detail == SpringLeftRunningDetail) {
			out[j.Name] = true
		}
	}
	return out
}

// lastPasses names, for each job, when its newest pass of this host ended (a
// job event done or failed), among the log's last SpringClockLookback events up
// to head. A job with no pass in that window is absent, and a log that cannot
// be read says there are none: a clock job is then due at once. A pass stamped
// after now is taken as ending now.
func (s *EventSpring) lastPasses(ctx context.Context, head uint64) map[string]time.Time {
	from := uint64(0)
	if head > SpringClockLookback {
		from = head - SpringClockLookback
	}
	recent, err := s.Log.Since(ctx, from)
	if err != nil {
		return nil
	}
	now := s.now()
	out := map[string]time.Time{}
	for _, e := range recent {
		if e.Kind != events.KindJob || (e.To != events.JobDone && e.To != events.JobFailed) {
			continue
		}
		name, ok := strings.CutSuffix(e.Actor, "@"+s.Host)
		if !ok {
			continue
		}
		at := e.Ts
		if at.After(now) {
			at = now
		}
		if at.After(out[name]) {
			out[name] = at
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

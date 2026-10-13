package application

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// GristJobName is the name of the follower's grist job in the log: its events'
// actor is grist-grind@host.
const GristJobName = "grist-grind"

// DefaultGristWatchEvery is how often a GristWatch reads the postern backend
// when it has no event stream. A read asks for the records after the newest
// seen, which is an empty page when nothing has come. A record's sent time is
// the app's whole seconds, rounded down, so the wake mw grist timing shows
// already holds half a second of that on average (mw-46z0my.8): a poll of a second on top
// of it, started once in each cycle of the follower, left the wake at 1.1-1.7
// s. A quarter of a second, kept by the watch's own loop, is two requests (a
// challenge and the read) four times a second.
const DefaultGristWatchEvery = 250 * time.Millisecond

// gristWatchAfterFailure is the least wait before a watch whose read failed
// reads again, so a backend that is down is not asked four times a second.
const gristWatchAfterFailure = time.Second

// gristWatchSlowRead is how long a read may take before the watch says so,
// whether or not it found a grist.
const gristWatchSlowRead = time.Second

// DefaultGristWatchBackstop is how often a GristWatch with its event stream
// open reads the backend anyway, in case the stream missed a record.
const DefaultGristWatchBackstop = 30 * time.Second

// gristWatchRetryMax is the longest a GristWatch waits before it opens its
// event stream again.
const gristWatchRetryMax = 30 * time.Second

// gristWatchTimeout is how long one read of the backend may take, so a backend
// that hangs holds the follower's cycle for no longer.
const gristWatchTimeout = 10 * time.Second

// GristWatch notices a new grist record for the mill key at the postern
// backend (postern's docs/protocol.md section 18), for the follower's grist
// job. It reads no grist: whether to answer it is the pass of the mill's.
type GristWatch struct {
	Postern Postern
	// Keys is the mill key file; its public key is the key a grist is for.
	Keys interface {
		PublicKey() (pubKeyHex string, address string, err error)
	}
	// Every is the least time between two reads of the backend while the
	// event stream is not open; zero is DefaultGristWatchEvery.
	Every time.Duration
	// Gate, when set, is asked before each read, which is made only when it
	// says true: the mill's home alone holds the mill key.
	Gate func(ctx context.Context) bool
	// Stream, when set, is the backend's event stream (postern's
	// docs/protocol.md section 10), which the watch holds open for as long as
	// it runs: a record the stream announces is read at once by Watch (at the
	// next Waiting when Watch is not running), and while the stream is open the
	// backend is read only every Backstop. A
	// stream the backend refuses or drops is opened again after Retry (zero is
	// a second, doubling to 30 s), and the watch reads every Every meanwhile.
	// Nil is no stream.
	Stream PosternStream
	// Backstop is the least time between two reads while the stream is open;
	// zero is DefaultGristWatchBackstop.
	Backstop time.Duration
	Retry    time.Duration
	// Notices, when set, is told when each new grist was noticed, for the
	// pass the follower springs to record how long the wake took.
	Notices *GristNotices
	// Now is the clock; nil is time.Now. Err is where a failed read is said,
	// once for each distinct failure, and where a read that found a grist, or
	// took a second or more, is timed: when it began, how long it took, and
	// how long after the read before it. Nil says nothing.
	Now func() time.Time
	Err io.Writer

	mu      sync.Mutex
	taken   bool
	newest  int64
	last    time.Time
	said    string
	millKey string

	// nudge wakes Watch's loop when the stream announces a record.
	nudge     chan struct{}
	nudgeOnce sync.Once

	streaming atomic.Bool // the goroutine is started
	live      atomic.Bool // the stream is open
	heard     atomic.Bool // the stream announced a record not yet read
	head      atomic.Int64
	streamSay sync.Mutex
	streamErr string
}

// Waiting says whether a grist record for the mill key has reached the backend
// since the last time it was asked, or is waiting there the first time: the
// pass of the mill answers each grist once, so a grist the dispatch tick has
// already answered springs a pass that finds nothing to do. A read that fails
// is not a grist; it is tried again after Every.
func (w *GristWatch) Waiting(ctx context.Context) (string, bool) {
	reason, ok, _ := w.poll(ctx, false)
	return reason, ok
}

// Watch reads the backend on its own pace until ctx ends, and says
// wake(reason) for each grist it finds: SpringJob.Watch, so that a grist waits
// for no cycle of the follower. It reads every Every, or, while the event
// stream is open, when the stream announces a record and else every Backstop;
// the read before it is never waited on, so the pace holds when a read is slow.
func (w *GristWatch) Watch(ctx context.Context, wake func(reason string)) {
	w.openStream(ctx)
	for ctx.Err() == nil {
		began := time.Now()
		pace, failed := w.every(), false
		var reason string
		var ok bool
		if reason, ok, failed = w.poll(ctx, true); ok {
			wake(reason)
		}
		if w.live.Load() {
			pace = w.backstop()
		}
		if failed {
			pace = max(pace, gristWatchAfterFailure)
		}
		timer := time.NewTimer(max(pace-time.Since(began), 0))
		select {
		case <-ctx.Done():
		case <-w.nudged():
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (w *GristWatch) every() time.Duration {
	if w.Every > 0 {
		return w.Every
	}
	return DefaultGristWatchEvery
}

func (w *GristWatch) backstop() time.Duration {
	if w.Backstop > 0 {
		return w.Backstop
	}
	return DefaultGristWatchBackstop
}

// nudged is the channel the stream announces a record on.
func (w *GristWatch) nudged() chan struct{} {
	w.nudgeOnce.Do(func() { w.nudge = make(chan struct{}, 1) })
	return w.nudge
}

// poll reads the backend unless it was read less than the pace ago, and
// reports a grist found, and whether the read failed. force reads anyway, for
// the loop that keeps the pace itself.
func (w *GristWatch) poll(ctx context.Context, force bool) (found string, ok, failed bool) {
	if w.Gate != nil && !w.Gate(ctx) {
		return "", false, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.openStream(ctx)
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	every := w.every()
	if w.live.Load() {
		every = w.backstop()
	}
	heard := w.heard.Swap(false)
	if !force && w.taken && !heard && now().Sub(w.last) < every {
		return "", false, false
	}
	previous := w.last
	began := now()
	w.last = began
	if w.millKey == "" {
		millKey, _, err := w.Keys.PublicKey()
		if err != nil {
			w.say(fmt.Errorf("reading the mill key: %w", err))
			return "", false, true
		}
		w.millKey = millKey
	}
	ctx, cancel := context.WithTimeout(ctx, gristWatchTimeout)
	defer cancel()
	records, err := w.Postern.Messages(ctx, w.newest)
	if err != nil {
		if heard {
			w.heard.Store(true)
		}
		w.say(fmt.Errorf("reading the postern backend for grist: %w", err))
		return "", false, true
	}
	w.said = ""
	first := !w.taken
	w.taken = true
	seen := now()
	for _, r := range records {
		w.newest = max(w.newest, r.Seq)
		mine := r.Class == GristClass && r.To == w.millKey && r.Txid != ""
		if mine && !first {
			w.Notices.Note(r.Txid, seen)
		}
		if mine && found == "" {
			found = "grist " + r.Txid + " arrived"
		}
	}
	if found != "" || seen.Sub(began) >= gristWatchSlowRead {
		w.timed(began, seen, previous, found)
	}
	return found, found != "", false
}

// timed says one read: when it began, how long it took, and how long after the
// read before it began, for the gap between a grist's sent time and the watch
// noticing it. w.mu is held.
func (w *GristWatch) timed(began, ended, previous time.Time, found string) {
	if w.Err == nil {
		return
	}
	what := "nothing"
	if found != "" {
		what = found
	}
	gap := "no read before"
	if !previous.IsZero() {
		gap = fmt.Sprintf("%.3fs after the read before", began.Sub(previous).Seconds())
	}
	fmt.Fprintf(w.Err, "mw events follow: grist watch: read began %s, took %.3fs, %s; %s\n",
		began.UTC().Format("15:04:05.000"), ended.Sub(began).Seconds(), what, gap)
}

// openStream starts, once, the goroutine that holds the event stream open for
// as long as ctx lives.
func (w *GristWatch) openStream(ctx context.Context) {
	if w.Stream == nil || w.streaming.Swap(true) {
		return
	}
	go func() {
		retry := w.Retry
		if retry <= 0 {
			retry = time.Second
		}
		wait := retry
		for ctx.Err() == nil {
			err := w.Stream.Events(ctx, func(e PosternEvent) error {
				switch e.Kind {
				case PosternEventHello:
					w.live.Store(true)
					wait = retry
					w.streamSaid("")
					// What came while the stream was down is read at once.
					w.heard.Store(true)
					w.nudgeWatch()
				case PosternEventMessage:
					w.heard.Store(true)
					w.nudgeWatch()
				}
				return nil
			})
			w.live.Store(false)
			if ctx.Err() != nil {
				return
			}
			w.streamSaid(fmt.Sprintf("the postern backend's event stream: %v", err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = min(wait*2, gristWatchRetryMax)
		}
	}()
}

// nudgeWatch wakes Watch's loop, if it is running, to read at once.
func (w *GristWatch) nudgeWatch() {
	select {
	case w.nudged() <- struct{}{}:
	default:
	}
}

// streamSaid says the stream's failure once for each distinct failure; "" is
// the stream being well again.
func (w *GristWatch) streamSaid(msg string) {
	w.streamSay.Lock()
	defer w.streamSay.Unlock()
	if msg != "" && w.Err != nil && w.streamErr != msg {
		fmt.Fprintf(w.Err, "mw events follow: %s; reading for grist every %v instead\n", msg, w.every())
	}
	w.streamErr = msg
}

func (w *GristWatch) say(err error) {
	if w.Err != nil && w.said != err.Error() {
		fmt.Fprintf(w.Err, "mw events follow: %v\n", err)
	}
	w.said = err.Error()
}

// GristJob is one pass of the mill (mw grist grind): sprung by a new grist
// record for the mill key, with no clock of its own, for the dispatch tick
// still answers a grist left waiting. The first look springs a pass when the
// backend holds a grist for the mill key, for the pass answers each grist once,
// under its pass lock, so one already answered costs a pass that finds nothing
// to do. A host with no [grist] table has no such job. The pass is in flight at
// most once: a grist that arrives while it runs is one more pass when it ends.
func GristJob(watch *GristWatch, run func(context.Context) error) SpringJob {
	return SpringJob{Name: GristJobName, Probe: watch.Waiting, ProbeFirst: true, Run: run}
}

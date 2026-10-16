package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

type watchKey struct{}

// saidBuffer is what a watch says, written by its own goroutines while the
// test reads it.
type saidBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *saidBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *saidBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitFor waits, ten seconds at most, until the watch has said text: what its
// goroutines say comes when the host lets them run, so a test never sleeps a
// fixed time for it (mw-gq6.440).
func (s *saidBuffer) waitFor(t *testing.T, text string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(s.String(), text); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the watch had not said %q in 10 s; it said %q", text, s.String())
		}
	}
}

func (watchKey) PublicKey() (string, string, error) { return "02aa", "", nil }

func TestGristWatchReportsAGristAlreadyWaitingOnItsFirstReadThenOnlyNewOnes(t *testing.T) {
	postern := apptest.NewFakePostern()
	postern.AddRecord(application.PosternRecord{Txid: "direct:old", Class: application.GristClass, To: "02aa"})
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Every: 5 * time.Second, Now: func() time.Time { return now }}
	reason, ok := watch.Waiting(context.Background())
	if !ok || !strings.Contains(reason, "direct:old") {
		t.Fatalf("a grist waiting before the watch began gave %q, %v; the first read reports it", reason, ok)
	}
	postern.AddRecord(application.PosternRecord{Txid: "direct:new", Class: application.GristClass, To: "02aa"})
	if _, ok := watch.Waiting(context.Background()); ok {
		t.Fatal("the backend was read again within Every")
	}
	now = now.Add(5 * time.Second)
	reason, ok = watch.Waiting(context.Background())
	if !ok || !strings.Contains(reason, "direct:new") || strings.Contains(reason, "direct:old") {
		t.Fatalf("a new grist gave %q, %v", reason, ok)
	}
	postern.AddRecord(application.PosternRecord{Txid: "direct:x", Class: application.GristClass, To: "02bb"})
	postern.AddRecord(application.PosternRecord{Txid: "direct:y", Class: "message", To: "02aa"})
	now = now.Add(5 * time.Second)
	if reason, ok := watch.Waiting(context.Background()); ok {
		t.Fatalf("a record for another key or class gave %q", reason)
	}
}

func TestGristWatchReadsFourTimesASecondUnlessToldOtherwise(t *testing.T) {
	if application.DefaultGristWatchEvery != 250*time.Millisecond {
		t.Fatalf("DefaultGristWatchEvery is %v, want 250ms", application.DefaultGristWatchEvery)
	}
}

// arrivalNoticed polls the watch every step of the fake clock and reports how
// long after the grist arrived it was noticed.
func arrivalNoticed(t *testing.T, watch *application.GristWatch, postern *apptest.FakePostern, now *time.Time, arriveAfter, step time.Duration) time.Duration {
	t.Helper()
	watch.Waiting(context.Background()) // the first read
	start := *now
	var arrived time.Time
	for *now = start; now.Sub(start) < time.Minute; *now = now.Add(step) {
		if arrived.IsZero() && now.Sub(start) >= arriveAfter {
			postern.AddRecord(application.PosternRecord{Txid: "direct:late", Class: application.GristClass, To: "02aa"})
			arrived = *now
		}
		if _, ok := watch.Waiting(context.Background()); ok {
			return now.Sub(arrived)
		}
	}
	t.Fatal("the grist was never noticed")
	return 0
}

func TestGristWatchNoticesAGristThatArrivesJustAfterAPollWithinAThirdOfASecond(t *testing.T) {
	postern := apptest.NewFakePostern()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Now: func() time.Time { return now }}
	// The grist lands 0.1 s after a poll.
	got := arrivalNoticed(t, watch, postern, &now, 100*time.Millisecond, 50*time.Millisecond)
	if got > 300*time.Millisecond {
		t.Fatalf("a grist 0.1 s after a poll was noticed %v after it arrived, want within 0.3 s", got)
	}
}

func TestGristWatchCostsNoMoreThanItsPollRateInAnIdleHour(t *testing.T) {
	postern := apptest.NewFakePostern()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Now: func() time.Time { return now }}
	start := now
	for ; now.Sub(start) <= time.Hour; now = now.Add(100 * time.Millisecond) {
		if _, ok := watch.Waiting(context.Background()); ok {
			t.Fatal("a grist was noticed in an hour with none")
		}
	}
	if got, most := postern.Reads(), 14401; got > most {
		t.Fatalf("an idle hour cost %d reads, want at most %d (four a second)", got, most)
	}
}

// openStream runs a watch over a fake backend that offers its event stream and
// waits until the stream is open.
func openStream(t *testing.T, postern *apptest.FakePostern, now *time.Time) *application.GristWatch {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	watch := &application.GristWatch{
		Postern: postern, Stream: postern, Keys: watchKey{}, Now: func() time.Time { return *now },
		Backstop: 30 * time.Second, Retry: time.Millisecond,
	}
	watch.Waiting(ctx) // the first read opens the stream
	for i := 0; postern.Streams() == 0; i++ {
		if i > 2000 {
			t.Fatal("the event stream was not opened")
		}
		time.Sleep(time.Millisecond)
	}
	return watch
}

func TestGristWatchReadsAtOnceWhenTheStreamSaysARecordCameAndNotOtherwise(t *testing.T) {
	postern := apptest.NewFakePostern()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := openStream(t, postern, &now)
	watch.Waiting(context.Background()) // the read the hello asks for
	before := postern.Reads()

	now = now.Add(200 * time.Millisecond)
	if _, ok := watch.Waiting(context.Background()); ok || postern.Reads() != before {
		t.Fatalf("with the stream open and nothing said, the backend was read (%d reads, was %d)", postern.Reads(), before)
	}
	postern.AddRecord(application.PosternRecord{Txid: "direct:pushed", Class: application.GristClass, To: "02aa"})
	now = now.Add(100 * time.Millisecond)
	reason, ok := watch.Waiting(context.Background())
	if !ok || !strings.Contains(reason, "direct:pushed") {
		t.Fatalf("a grist the stream announced gave %q, %v", reason, ok)
	}
}

func TestGristWatchWithTheStreamOpenReadsOnlyAsABackstopInAnIdleHour(t *testing.T) {
	postern := apptest.NewFakePostern()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := openStream(t, postern, &now)
	watch.Waiting(context.Background()) // the read the hello asks for
	before := postern.Reads()
	start := now
	for ; now.Sub(start) <= time.Hour; now = now.Add(time.Second) {
		watch.Waiting(context.Background())
	}
	if got, most := postern.Reads()-before, 121; got > most {
		t.Fatalf("an idle hour with the stream open cost %d reads, want at most %d (one in 30 s)", got, most)
	}
}

func TestGristWatchPollsEverySecondWhileTheStreamIsRefusedAndSaysSoOnce(t *testing.T) {
	postern := apptest.NewFakePostern()
	postern.StreamErr = errors.New("403 forbidden")
	var said saidBuffer
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := &application.GristWatch{Postern: postern, Stream: postern, Keys: watchKey{}, Now: func() time.Time { return now }, Retry: time.Millisecond, Err: &said}
	watch.Waiting(ctx)
	said.waitFor(t, "403 forbidden")
	time.Sleep(50 * time.Millisecond) // several more refused attempts, to be said no more
	for range 3 {
		now = now.Add(time.Second)
		watch.Waiting(ctx)
	}
	postern.AddRecord(application.PosternRecord{Txid: "direct:polled", Class: application.GristClass, To: "02aa"})
	now = now.Add(time.Second)
	if _, ok := watch.Waiting(ctx); !ok {
		t.Fatal("with the stream refused, a grist was not found by the poll")
	}
	if n := strings.Count(said.String(), "403 forbidden"); n != 1 {
		t.Fatalf("said %q, want the refusal once", said.String())
	}
}

func TestGristWatchSaysAFailedReadOnceAndTriesAgain(t *testing.T) {
	postern := apptest.NewFakePostern()
	postern.Err = errors.New("backend down")
	var said bytes.Buffer
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Every: time.Second, Now: func() time.Time { return now }, Err: &said}
	for range 3 {
		now = now.Add(time.Second)
		if _, ok := watch.Waiting(context.Background()); ok {
			t.Fatal("a failed read is a grist")
		}
	}
	if strings.Count(said.String(), "backend down") != 1 {
		t.Fatalf("said %q, want the failure once", said.String())
	}
	postern.Err = nil
	now = now.Add(time.Second)
	watch.Waiting(context.Background()) // bearings
	postern.AddRecord(application.PosternRecord{Txid: "direct:1", Class: application.GristClass, To: "02aa"})
	now = now.Add(time.Second)
	if _, ok := watch.Waiting(context.Background()); !ok {
		t.Fatal("a grist after the backend came back did not spring")
	}
}

func TestGristWatchNotesWhenItNoticedEachNewGristAndNotTheOnesAlreadyWaiting(t *testing.T) {
	postern := apptest.NewFakePostern()
	postern.AddRecord(application.PosternRecord{Txid: "direct:old", Class: application.GristClass, To: "02aa"})
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	notices := application.NewGristNotices()
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Every: time.Second, Notices: notices, Now: func() time.Time { return now }}
	watch.Waiting(context.Background())
	now = now.Add(7 * time.Second)
	postern.AddRecord(application.PosternRecord{Txid: "direct:new", Class: application.GristClass, To: "02aa"})
	postern.AddRecord(application.PosternRecord{Txid: "direct:other", Class: application.GristClass, To: "02bb"})
	watch.Waiting(context.Background())

	if at, ok := notices.Take("direct:new"); !ok || !at.Equal(now) {
		t.Errorf("the new grist was noticed at %v (%v), want %v", at, ok, now)
	}
	if _, ok := notices.Take("direct:new"); ok {
		t.Error("a notice taken stays")
	}
	for _, txid := range []string{"direct:old", "direct:other"} {
		if _, ok := notices.Take(txid); ok {
			t.Errorf("%s was noticed, but is the dispatch tick's or another key's", txid)
		}
	}
}

func TestGristNoticesKeepTheFirstNoticeAndDropTheOnesLeftAnHour(t *testing.T) {
	var none *application.GristNotices
	none.Note("direct:a", time.Now())
	if _, ok := none.Take("direct:a"); ok {
		t.Fatal("a nil set of notices holds one")
	}
	notices := application.NewGristNotices()
	first := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	notices.Note("direct:a", first)
	notices.Note("direct:a", first.Add(time.Minute))
	notices.Note("direct:b", first.Add(2*time.Hour))
	if _, ok := notices.Take("direct:a"); ok {
		t.Error("a notice left more than an hour stays")
	}
	if at, ok := notices.Take("direct:b"); !ok || !at.Equal(first.Add(2*time.Hour)) {
		t.Errorf("direct:b noticed at %v (%v)", at, ok)
	}
	notices.Note("direct:c", first)
	notices.Note("direct:c", first.Add(time.Minute))
	if at, _ := notices.Take("direct:c"); !at.Equal(first) {
		t.Errorf("the first notice stands, got %v", at)
	}
}

// gristSpringRig is a follower's springer over one grist job whose watch reads
// a fake backend, with the passes the job runs counted. Its cycles are the
// test's to run: the follower's own pace is not in it.
type gristSpringRig struct {
	postern *apptest.FakePostern
	watch   *application.GristWatch
	spring  *application.EventSpring
	job     application.SpringJob
	ran     chan struct{}
}

func newGristSpringRig(t *testing.T) *gristSpringRig {
	t.Helper()
	r := &gristSpringRig{postern: apptest.NewFakePostern(), ran: make(chan struct{}, 16)}
	r.watch = &application.GristWatch{Postern: r.postern, Keys: watchKey{}, Every: 10 * time.Millisecond}
	r.job = application.GristJob(r.watch, func(context.Context) error {
		r.ran <- struct{}{}
		return nil
	})
	return r
}

// start runs the first cycle, which only takes the backend's bearings, with
// the job as r.job is now.
func (r *gristSpringRig) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		r.spring.Wait()
	})
	r.spring = &application.EventSpring{Log: &apptest.FakeEventLog{}, Host: "laptop", Jobs: []application.SpringJob{r.job}}
	if err := r.spring.Spring(ctx); err != nil {
		t.Fatal(err)
	}
}

func (r *gristSpringRig) waitForAPass(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case <-r.ran:
	case <-time.After(within):
		t.Fatalf("no pass of the mill began within %v of a grist arriving, with the follower running no cycle", within)
	}
}

// The wake was 1.1-1.7 s live with a 1 s poll (mw-46z0my.8): the watch was
// asked once in each cycle of the follower, which is a second of sleep plus
// the cycle's own work, so a grist waited for the next cycle on top of the
// time the backend's whole-second sent time costs. The watch reads on its own
// pace, and a grist it finds springs the job at once.
func TestAGristSpringsTheMillWithoutWaitingForTheFollowersNextCycle(t *testing.T) {
	r := newGristSpringRig(t)
	r.job.Watch = r.watch.Watch
	r.start(t)
	r.postern.AddRecord(application.PosternRecord{Txid: "direct:fast", Class: application.GristClass, To: "02aa"})
	r.waitForAPass(t, 2*time.Second)
}

func TestGristWatchLoopReadsOnlyWhileItsGateIsOpen(t *testing.T) {
	r := newGristSpringRig(t)
	var open atomic.Bool
	r.watch.Gate = func(context.Context) bool { return open.Load() }
	r.job.Watch = r.watch.Watch
	r.postern.AddRecord(application.PosternRecord{Txid: "direct:waiting", Class: application.GristClass, To: "02aa"})
	r.start(t)
	time.Sleep(100 * time.Millisecond)
	if reads := r.postern.Reads(); reads != 0 {
		t.Fatalf("the backend was read %d times with the gate shut", reads)
	}
	open.Store(true)
	r.waitForAPass(t, 2*time.Second)
}

func TestGristWatchLoopReadsAtOnceWhenTheStreamSaysARecordCameWithTheBackstopAMinute(t *testing.T) {
	r := newGristSpringRig(t)
	r.watch.Stream, r.watch.Backstop, r.watch.Retry = r.postern, time.Minute, time.Millisecond
	r.job.Watch = r.watch.Watch
	r.start(t)
	for i := 0; r.postern.Streams() == 0; i++ {
		if i > 2000 {
			t.Fatal("the event stream was not opened")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // the read the hello asks for
	r.postern.AddRecord(application.PosternRecord{Txid: "direct:pushed", Class: application.GristClass, To: "02aa"})
	r.waitForAPass(t, 2*time.Second)
}

func TestGristWatchLoopWaitsASecondAfterAFailedReadAndSaysItOnce(t *testing.T) {
	r := newGristSpringRig(t)
	var said saidBuffer
	r.watch.Err = &said
	r.postern.Err = errors.New("backend down")
	r.job.Watch = r.watch.Watch
	started := time.Now()
	r.start(t)
	said.waitFor(t, "backend down")
	time.Sleep(300 * time.Millisecond)
	// Two reads, and one more for each whole second a loaded host made of it.
	elapsed := time.Since(started)
	if reads, most := r.postern.Reads(), 2+int(elapsed/time.Second); reads > most {
		t.Fatalf("a backend that is down was read %d times in %.1f s, want a second between reads", reads, elapsed.Seconds())
	}
	if strings.Count(said.String(), "backend down") != 1 {
		t.Fatalf("said %q, want the failure once", said.String())
	}
}

func TestGristWatchTimesTheReadThatFoundAGristAndNoOther(t *testing.T) {
	postern := apptest.NewFakePostern()
	var said bytes.Buffer
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Every: time.Second, Now: func() time.Time { return now }, Err: &said}
	watch.Waiting(context.Background()) // bearings, nothing found
	if said.Len() != 0 {
		t.Fatalf("a read that found nothing was timed: %q", said.String())
	}
	postern.AddRecord(application.PosternRecord{Txid: "direct:timed", Class: application.GristClass, To: "02aa"})
	now = now.Add(1250 * time.Millisecond)
	watch.Waiting(context.Background())
	for _, want := range []string{"grist watch", "read began 09:00:01.250", "took 0.000s", "grist direct:timed arrived", "1.250s after the read before"} {
		if !strings.Contains(said.String(), want) {
			t.Errorf("said %q, want it to hold %q", said.String(), want)
		}
	}
}

package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

type watchKey struct{}

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

func TestGristWatchReadsEverySecondUnlessToldOtherwise(t *testing.T) {
	if application.DefaultGristWatchEvery != time.Second {
		t.Fatalf("DefaultGristWatchEvery is %v, want 1s", application.DefaultGristWatchEvery)
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

func TestGristWatchNoticesAGristThatArrivesJustAfterAPollWithinOneAndAHalfSeconds(t *testing.T) {
	postern := apptest.NewFakePostern()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Now: func() time.Time { return now }}
	// The follower's cycle is a second; the grist lands 0.3 s after a poll.
	got := arrivalNoticed(t, watch, postern, &now, 300*time.Millisecond, 100*time.Millisecond)
	if got > 1500*time.Millisecond {
		t.Fatalf("a grist 0.3 s after a poll was noticed %v after it arrived, want within 1.5 s", got)
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
	if got, most := postern.Reads(), 3601; got > most {
		t.Fatalf("an idle hour cost %d reads, want at most %d (one a second)", got, most)
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
	var said bytes.Buffer
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := &application.GristWatch{Postern: postern, Stream: postern, Keys: watchKey{}, Now: func() time.Time { return now }, Retry: time.Millisecond, Err: &said}
	watch.Waiting(ctx)
	time.Sleep(50 * time.Millisecond) // several refused attempts
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

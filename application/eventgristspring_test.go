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

func TestGristWatchTakesItsBearingsFirstThenSeesOnlyNewGristForTheMillKey(t *testing.T) {
	postern := apptest.NewFakePostern()
	postern.AddRecord(application.PosternRecord{Txid: "direct:old", Class: application.GristClass, To: "02aa"})
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	watch := &application.GristWatch{Postern: postern, Keys: watchKey{}, Every: 5 * time.Second, Now: func() time.Time { return now }}
	if _, ok := watch.Waiting(context.Background()); ok {
		t.Fatal("a grist waiting before the watch began springs the mill")
	}
	postern.AddRecord(application.PosternRecord{Txid: "direct:new", Class: application.GristClass, To: "02aa"})
	if _, ok := watch.Waiting(context.Background()); ok {
		t.Fatal("the backend was read again within Every")
	}
	now = now.Add(5 * time.Second)
	reason, ok := watch.Waiting(context.Background())
	if !ok || !strings.Contains(reason, "direct:new") {
		t.Fatalf("a new grist gave %q, %v", reason, ok)
	}
	postern.AddRecord(application.PosternRecord{Txid: "direct:x", Class: application.GristClass, To: "02bb"})
	postern.AddRecord(application.PosternRecord{Txid: "direct:y", Class: "message", To: "02aa"})
	now = now.Add(5 * time.Second)
	if reason, ok := watch.Waiting(context.Background()); ok {
		t.Fatalf("a record for another key or class gave %q", reason)
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

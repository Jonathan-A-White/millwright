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

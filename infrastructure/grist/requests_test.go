package grist_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

func TestRequestsKeepAndReadARecordPerBeadPrivately(t *testing.T) {
	dir := t.TempDir()
	r := grist.NewRequests(dir)
	ctx := context.Background()

	if _, ok, err := r.Get(ctx, "mw-x.1"); ok || err != nil {
		t.Fatalf("Get before Keep = %v, %v; want none", ok, err)
	}
	if got, err := r.List(ctx); err != nil || len(got) != 0 {
		t.Fatalf("List before Keep = %v, %v; want none", got, err)
	}
	two := application.GristRequestRecord{Bead: "mw-x.2", Request: "direct:b", App: "cairn", Sender: "02bb"}
	one := application.GristRequestRecord{
		Bead: "mw-x.1", Request: "direct:a", App: "cairn", Sender: "02aa",
		Last: &application.GristRequestStatus{Kind: "request-status", Status: "filed", Ts: 5}, Setting: "bible-walk",
		Declined: &application.GristDecline{Reason: "outside", Note: "n"},
	}
	for _, rec := range []application.GristRequestRecord{two, one} {
		if err := r.Keep(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := r.Get(ctx, "mw-x.1")
	if err != nil || !ok || !reflect.DeepEqual(got, one) {
		t.Errorf("Get = %+v, %v, %v; want %+v", got, ok, err, one)
	}
	all, err := r.List(ctx)
	if err != nil || !reflect.DeepEqual(all, []application.GristRequestRecord{one, two}) {
		t.Errorf("List = %+v, %v; want both, by bead", all, err)
	}
	info, err := os.Stat(filepath.Join(dir, "requests", "mw-x.1.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the record's file = %v, %v; want 0600", info, err)
	}
}

func TestRequestsRefuseABeadIdThatIsAPath(t *testing.T) {
	r := grist.NewRequests(t.TempDir())
	if err := r.Keep(context.Background(), application.GristRequestRecord{Bead: "../x"}); err == nil {
		t.Error("Keep of ../x was not refused")
	}
	if _, _, err := r.Get(context.Background(), "a/b"); err == nil {
		t.Error("Get of a/b was not refused")
	}
}

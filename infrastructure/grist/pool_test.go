package grist_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// The pool's board is nothing until saved, then the view saved last.
func TestThePoolsBoardKeepsTheLastViewSaved(t *testing.T) {
	ctx, dir := context.Background(), filepath.Join(t.TempDir(), "grist")
	state := grist.New(dir)
	if view, err := state.Pool(ctx); err != nil || !view.At.IsZero() || len(view.Kinds) != 0 {
		t.Fatalf("expected no view before one is saved, got %+v %v", view, err)
	}
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	for _, view := range []application.GristPoolView{
		{At: at, Kinds: []application.GristPoolKind{{App: "cairn", Kind: "sweep", Warm: 2}}},
		{At: at.Add(time.Minute), Kinds: []application.GristPoolKind{{App: "lampas", Kind: "bible-talk", Warm: 1, Busy: 1}}},
	} {
		if err := state.SavePool(ctx, view); err != nil {
			t.Fatal(err)
		}
	}
	got, err := grist.New(dir).Pool(ctx)
	want := application.GristPoolView{At: at.Add(time.Minute), Kinds: []application.GristPoolKind{{App: "lampas", Kind: "bible-talk", Warm: 1, Busy: 1}}}
	if err != nil || !got.At.Equal(want.At) || !reflect.DeepEqual(got.Kinds, want.Kinds) {
		t.Fatalf("expected %+v, got %+v %v", want, got, err)
	}
}

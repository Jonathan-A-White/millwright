package benchflag_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/benchflag"
)

func TestAFlagSaidIsReadBackByItsKindAndNoneIsNotSaid(t *testing.T) {
	m := benchflag.New(t.TempDir() + "/state")
	ctx := context.Background()
	if _, _, ok, err := m.Said(ctx, "lampas/bug/haiku"); ok || err != nil {
		t.Fatalf("expected nothing said and no error, got ok %v err %v", ok, err)
	}
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if err := m.Say(ctx, "lampas/bug/haiku", "FLAG lampas/bug/haiku: try a wider window", at); err != nil {
		t.Fatal(err)
	}
	text, gotAt, ok, err := m.Said(ctx, "lampas/bug/haiku")
	if err != nil || !ok || text != "FLAG lampas/bug/haiku: try a wider window" || !gotAt.Equal(at) {
		t.Errorf("got %q at %v ok %v err %v", text, gotAt, ok, err)
	}
	if _, _, ok, _ := m.Said(ctx, "lampas/feature/sonnet"); ok {
		t.Error("expected another kind to have nothing said")
	}
}

// A write names its temp file for itself alone: something already sitting at
// the old fixed '<flag>.new' name is no obstacle (mw-gq6.1042).
func TestSayIsNotBlockedByAnythingAtTheOldFixedTempName(t *testing.T) {
	dir := t.TempDir() + "/state"
	m := benchflag.New(dir)
	if err := os.MkdirAll(dir+"/lampas%2Fbug%2Fhaiku.new", 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	if err := m.Say(context.Background(), "lampas/bug/haiku", "FLAG", at); err != nil {
		t.Fatalf("saying with the old temp name taken: %v", err)
	}
	if text, _, ok, err := m.Said(context.Background(), "lampas/bug/haiku"); err != nil || !ok || text != "FLAG" {
		t.Errorf("got %q ok %v err %v", text, ok, err)
	}
}

package benchflag_test

import (
	"context"
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

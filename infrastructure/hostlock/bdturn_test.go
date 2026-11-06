package hostlock_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/hostlock"
)

// mw-gq6.568: the turn to run a bd in is a flock, so two mw bd commands, in
// two processes, never hold it together.
func TestOnlyOneMwBdHoldsTheTurnAtATime(t *testing.T) {
	dir := t.TempDir()
	first := hostlock.NewBdTurn(dir, "mw bd close step-1")
	second := hostlock.NewBdTurn(dir, "mw bd list")
	release, taken, _, err := first.TryTake(context.Background())
	if err != nil || !taken {
		t.Fatalf("the first call taken %v, err %v", taken, err)
	}
	_, taken, holder, err := second.TryTake(context.Background())
	if err != nil || taken {
		t.Fatalf("the second call taken %v, err %v while the first holds it", taken, err)
	}
	if !strings.Contains(holder, "mw bd close step-1") {
		t.Errorf("the holder should be named, it was %q", holder)
	}
	release()
	release, taken, _, err = second.TryTake(context.Background())
	if err != nil || !taken {
		t.Fatalf("the turn was not free after the release: taken %v, err %v", taken, err)
	}
	release()
}

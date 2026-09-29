package homemove

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveHomeRunsMwHomeMoveAndReportsHowItRan(t *testing.T) {
	dir := onPath(t, map[string]string{"mw": "echo 'Step 1 of 6: stopped'; exit 3"})
	outcome, err := Command{Mw: filepath.Join(dir, "mw")}.MoveHome(context.Background(), []string{"desktop", "--planned"})
	if err != nil {
		t.Fatalf("MoveHome: %v", err)
	}
	if outcome.Exit != 3 || !strings.Contains(outcome.Output, "Step 1 of 6: stopped") {
		t.Errorf("outcome = %+v", outcome)
	}
	if got := logOf(t, dir, "mw"); got != "home move desktop --planned\n" {
		t.Errorf("mw was run as %q", got)
	}
}

func TestMoveHomeThatRanCleanlyIsExitZero(t *testing.T) {
	dir := onPath(t, map[string]string{"mw": "echo moved"})
	outcome, err := Command{Mw: filepath.Join(dir, "mw")}.MoveHome(context.Background(), []string{"laptop", "--old-home-dead"})
	if err != nil || outcome.Exit != 0 || outcome.Output != "moved\n" {
		t.Errorf("outcome = %+v, %v", outcome, err)
	}
}

func TestMoveHomeThatCannotStartIsAnError(t *testing.T) {
	_, err := Command{Mw: filepath.Join(t.TempDir(), "no-such-mw")}.MoveHome(context.Background(), []string{"laptop", "--planned"})
	if err == nil {
		t.Error("expected an error")
	}
}

func TestSpendMarksATxidOnceAndKeepsItOnDisk(t *testing.T) {
	c := Command{Spent: filepath.Join(t.TempDir(), "state")}
	for n, want := range []bool{true, false} {
		fresh, err := c.Spend(context.Background(), "direct:ab/../cd")
		if err != nil || fresh != want {
			t.Fatalf("Spend #%d = %v, %v; wanted %v", n+1, fresh, err, want)
		}
	}
	if fresh, err := c.Spend(context.Background(), "direct:ef"); err != nil || !fresh {
		t.Errorf("another txid = %v, %v; wanted fresh", fresh, err)
	}
	if _, err := os.Stat(filepath.Join(c.Spent, "move-home.direct_ab____cd")); err != nil {
		t.Errorf("expected the mark kept inside %s: %v", c.Spent, err)
	}
}

func TestSpendWithNowhereToMarkIsAnError(t *testing.T) {
	if _, err := (Command{}).Spend(context.Background(), "tx"); err == nil {
		t.Error("expected an error")
	}
}

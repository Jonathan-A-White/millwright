package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const confirmLine = "New mail for millhand: 2 message(s). Run bd mail inbox."

func confirmWindows() *apptest.FakeWindows {
	w := apptest.NewFakeWindows()
	w.HoldsWithID("@1", "millhand-2026-10-01-02", time.Time{})
	return w
}

func TestTypeConfirmedAcceptedPressesEnterOnceAndSaysNothing(t *testing.T) {
	w := confirmWindows()
	got, err := application.TypeConfirmed(context.Background(), w, application.ReapWindow{ID: "@1", Name: "millhand-2026-10-01-02"}, confirmLine, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != application.TypeAccepted || got.Said("millhand-2026-10-01-02", confirmLine) != "" {
		t.Fatalf("outcome %v said %q, want accepted and nothing said", got, got.Said("w", confirmLine))
	}
	if n := w.Enters("@1"); n != 1 {
		t.Fatalf("Enter pressed %d times, want 1", n)
	}
}

func TestTypeConfirmedRetriesOnceWhenTheFirstEnterWasLost(t *testing.T) {
	w := confirmWindows()
	w.LoseEnters("@1", 1)
	got, err := application.TypeConfirmed(context.Background(), w, application.ReapWindow{ID: "@1", Name: "millhand-2026-10-01-02"}, confirmLine, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != application.TypeRetried {
		t.Fatalf("outcome %v, want retried", got)
	}
	if n := w.Enters("@1"); n != 2 {
		t.Fatalf("Enter pressed %d times, want 2", n)
	}
	if line := w.InputLineOf("@1"); line != "" {
		t.Fatalf("the line is still on the input line: %q", line)
	}
	said := got.Said("millhand-2026-10-01-02", confirmLine)
	if !strings.Contains(said, "again") || !strings.Contains(said, "millhand-2026-10-01-02") || strings.Contains(said, "\n") {
		t.Fatalf("said %q, want one line naming the window and that Enter was pressed again", said)
	}
}

func TestTypeConfirmedReportsAPaneThatNeverTakesEnterAsStuckAfterOneRetry(t *testing.T) {
	w := confirmWindows()
	w.LoseEnters("@1", -1)
	got, err := application.TypeConfirmed(context.Background(), w, application.ReapWindow{ID: "@1", Name: "millhand-2026-10-01-02"}, confirmLine, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != application.TypeStuck {
		t.Fatalf("outcome %v, want stuck", got)
	}
	if n := w.Enters("@1"); n != 2 {
		t.Fatalf("Enter pressed %d times, want 2 (one retry)", n)
	}
	said := got.Said("millhand-2026-10-01-02", confirmLine)
	if !strings.Contains(said, "still") || !strings.Contains(said, confirmLine) {
		t.Fatalf("said %q, want that the line is on the input line still", said)
	}
}

func TestTypeConfirmedLeavesAPaneItCannotReadAlone(t *testing.T) {
	w := confirmWindows()
	w.LoseEnters("@1", -1)
	w.PaneErr = errors.New("capture-pane failed")
	got, err := application.TypeConfirmed(context.Background(), w, application.ReapWindow{ID: "@1", Name: "m"}, confirmLine, 0)
	if err != nil || got != application.TypeAccepted {
		t.Fatalf("outcome %v, err %v: a pane that cannot be read is not pressed over", got, err)
	}
	if n := w.Enters("@1"); n != 1 {
		t.Fatalf("Enter pressed %d times, want 1", n)
	}
}

func TestTypeConfirmedFailsWhenTheLineCannotBeTyped(t *testing.T) {
	w := confirmWindows()
	if _, err := application.TypeConfirmed(context.Background(), w, application.ReapWindow{ID: "@9", Name: "gone"}, confirmLine, 0); err == nil {
		t.Fatal("want the error of typing into a window that is not open")
	}
}

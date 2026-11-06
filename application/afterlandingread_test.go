package application

import (
	"strings"
	"testing"
	"time"
)

func TestTheLogOfARunSaysWhenItsLineWasReadAndThatItChanged(t *testing.T) {
	at := time.Date(2026, 11, 5, 23, 49, 30, 0, time.UTC)

	same := Ran{Command: "make deploy", ReadAt: at}.ReadLines()
	if len(same) != 1 || same[0] != "after landing: make deploy: read at 2026-11-05T23:49:30Z" {
		t.Errorf("expected one line saying when the command was read, got %q", same)
	}

	changed := Ran{Command: "make deploy", ReadAt: at, Changed: true}.ReadLines()
	if len(changed) != 2 || changed[1] != "after_landing changed during the batch: running the new line" {
		t.Errorf("expected the change said after the read, got %q", changed)
	}

	failed := Ran{Command: "make deploy", ReadFailed: "permission denied"}.ReadLines()
	if len(failed) != 1 || !strings.Contains(failed[0], "could not be read again") || !strings.Contains(failed[0], "permission denied") {
		t.Errorf("expected the failed read said, got %q", failed)
	}

	if lines := (Ran{Command: "make build"}).ReadLines(); len(lines) != 0 {
		t.Errorf("expected nothing said of a command whose reading is not known, got %q", lines)
	}
}

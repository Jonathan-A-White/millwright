package application

import (
	"strings"
	"testing"
	"time"
)

func TestTheLogOfAGateCommandSaysWhenItsLineWasReadAndThatItChanged(t *testing.T) {
	at := time.Date(2026, 11, 6, 8, 30, 0, 0, time.UTC)

	same := GateRead{What: "tests", At: at}.Lines("make test")
	if len(same) != 1 || same[0] != "gate tests: make test: read at 2026-11-06T08:30:00Z" {
		t.Errorf("expected one line saying when the gate command was read, got %q", same)
	}

	changed := GateRead{What: "related_tests", At: at, Changed: true}.Lines("npx vitest related {files}")
	if len(changed) != 2 || changed[1] != "related_tests changed during the batch: running the new line" {
		t.Errorf("expected the change said after the read, got %q", changed)
	}

	failed := GateRead{What: "changed_specs", Failed: "permission denied"}.Lines("npx playwright test")
	if len(failed) != 1 || !strings.Contains(failed[0], "could not be read again") || !strings.Contains(failed[0], "permission denied") || !strings.Contains(failed[0], "changed_specs") {
		t.Errorf("expected the failed read said, got %q", failed)
	}

	if lines := (GateRead{}).Lines("make test"); len(lines) != 0 {
		t.Errorf("expected nothing said of a gate command whose reading is not known, got %q", lines)
	}
}

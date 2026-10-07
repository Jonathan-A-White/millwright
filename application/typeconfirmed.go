package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TypeSettle is how long the commands wait after a line is typed before reading
// the input line back: long enough for a busy session to take the Enter.
const TypeSettle = 2 * time.Second

// TypeOutcome is what came of a line typed into a pane and its Enter.
type TypeOutcome int

const (
	// TypeAccepted: the input line was empty once Settle had passed, or could
	// not be read, and nothing was pressed over it.
	TypeAccepted TypeOutcome = iota
	// TypeRetried: the line was still on the input line, Enter was pressed
	// once more and the line went.
	TypeRetried
	// TypeStuck: the line was on the input line after the second Enter too.
	TypeStuck
)

// TypeConfirmed types the line into the window and, once settle has passed,
// reads the input line: a line still on it was not submitted (a session hung
// mid-turn drops the Enter), so Enter is pressed once more and the line is read
// again. It is the one way a pane is typed into and checked: the Millhand's
// tick, the Deputy's wake, the reaper and the event follower all use it. A pane
// that cannot be read is left as it is. An error is the typing, or the second
// Enter, failing; a line that never goes is TypeStuck, which Said puts in words.
func TypeConfirmed(ctx context.Context, terminal ReapTerminal, window ReapWindow, line string, settle time.Duration) (TypeOutcome, error) {
	if err := terminal.Type(ctx, window.ID, line); err != nil {
		return TypeAccepted, err
	}
	if !lineStuck(ctx, terminal, window, line, settle) {
		return TypeAccepted, nil
	}
	if err := terminal.Enter(ctx, window.ID); err != nil {
		return TypeAccepted, fmt.Errorf("pressing Enter again in %s: %w", window.Name, err)
	}
	if lineStuck(ctx, terminal, window, line, settle) {
		return TypeStuck, nil
	}
	return TypeRetried, nil
}

// Said is the one line to log for the outcome, "" when Enter took the line the
// first time.
func (o TypeOutcome) Said(window, line string) string {
	switch o {
	case TypeRetried:
		return fmt.Sprintf("the line %q was not submitted by Enter in %s; pressed Enter again and it went", line, window)
	case TypeStuck:
		return fmt.Sprintf("pressed Enter again in %s and the line %q is on its input line still", window, line)
	}
	return ""
}

// lineStuck reports whether, after settle, the line typed is on the window's
// input line. A line the pane has not read is not stuck: nothing is pressed
// over a pane that cannot be seen.
func lineStuck(ctx context.Context, terminal ReapTerminal, window ReapWindow, line string, settle time.Duration) bool {
	if settle > 0 {
		select {
		case <-time.After(settle):
		case <-ctx.Done():
			return false
		}
	}
	held, err := terminal.InputLine(ctx, window.ID)
	if err != nil {
		return false
	}
	held = strings.TrimSpace(held)
	return held != "" && (strings.Contains(held, line) || strings.HasPrefix(line, held))
}

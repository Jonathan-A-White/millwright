package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// paneLimitChoice is a choice of the menu the harness puts up at the usage
// limit — "1. Stop and wait for limit to reset", "2. Switch to usage credits"
// — marked or not by its selection arrow, drawn in a box or not. The words
// quoted in prose, as a story's title quotes them, are not a choice.
var paneLimitChoice = regexp.MustCompile(`^[\s│|]*(?:[❯>]\s*)?\d+\.\s+(?:Stop and wait|Switch to usage credits)`)

// paneLimitNotice is the harness's own line at the limit, which begins with its
// words ("⎿  You've hit your weekly limit · resets …"); a sentence that only
// mentions them does not.
var paneLimitNotice = regexp.MustCompile(`^[\s│|⎿]*(?i:you(?:'|’)ve|you have) hit your\b`)

// PaneLimitTail is how far up from the bottom of a pane the limit notice is
// read when no limit menu stands under it: the input box and status lines are
// below it, and a session that went on working has scrolled it away.
const PaneLimitTail = 12

// PaneUsageLimit is the usage limit a pane's screen is parked on, when it shows
// one: the harness's notice and the choices of its limit menu, each line
// trimmed. With the menu up, the notice is read anywhere on the screen; without
// it, only in its last PaneLimitTail lines. It is the one reader of a limited
// pane: the terminal's PaneState, the seats' reapers and wakers and mw doctor's
// mayor-stuck all read the screen through it (mw-gq6.433, mw-gq6.473).
func PaneUsageLimit(screen string) (string, bool) {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(screen, " ", " "), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	menu := false
	for _, line := range lines {
		if paneLimitChoice.MatchString(line) {
			menu = true
			break
		}
	}
	var shown []string
	for i, line := range lines {
		switch {
		case paneLimitChoice.MatchString(line):
			shown = append(shown, line)
		case paneLimitNotice.MatchString(line) && AtUsageLimit(line) && (menu || i >= len(lines)-PaneLimitTail):
			shown = append(shown, line)
		}
	}
	return strings.Join(shown, "\n"), len(shown) > 0
}

// PaneLimit is a seat's pane stopped at the usage limit: the harness's notice,
// on one line, and when the limit resets.
type PaneLimit struct {
	Notice string
	Resets time.Time
}

// ReadPaneLimit reads what PaneUsageLimit found on a pane into the notice and
// its reset. since is the earliest the pane can have met the limit — when its
// window was opened, or when the limit was first seen there — so that a reset
// named without a date ("resets 3pm") is the first such time after it, and
// does not move on to tomorrow's as the pane sits there. A reset that cannot
// be read, or a menu whose notice has scrolled away, is UsageLimitWait after
// since, as a session's own is.
func ReadPaneLimit(shown string, since time.Time) PaneLimit {
	notice := ""
	for _, line := range strings.Split(shown, "\n") {
		if paneLimitNotice.MatchString(line) {
			notice = strings.Join(strings.Fields(strings.TrimLeft(strings.TrimSpace(line), "│|⎿ ")), " ")
			break
		}
	}
	return PaneLimit{Notice: notice, Resets: usageReset(notice, since)}
}

// Over reports whether the limit has reset by now: the seat may be restarted.
func (l PaneLimit) Over(now time.Time) bool { return !now.Before(l.Resets) }

// String is the limit as a seat's line says it: until when, and the harness's
// own words.
func (l PaneLimit) String() string {
	said := "stopped at the usage limit until " + l.Resets.UTC().Format(TicksTimeFormat)
	if l.Notice != "" {
		said += ": " + l.Notice
	}
	return said
}

// paneLimitOf is one look at whether the window's pane is stopped at the usage
// limit, read with the window's opening as the earliest it can have met it,
// and the time given when the terminal cannot date the window. A pane that
// cannot be read is not at the limit; a notice that cannot be read is a limit
// whose reset is not known (ReadPaneLimit).
func paneLimitOf(ctx context.Context, terminal ReapTerminal, window ReapWindow, undated time.Time) (PaneLimit, bool) {
	if terminal == nil {
		return PaneLimit{}, false
	}
	state, err := terminal.PaneState(ctx, window.ID)
	if err != nil || state != PaneLimited {
		return PaneLimit{}, false
	}
	shown, err := terminal.UsageLimit(ctx, window.ID)
	if err != nil {
		shown = ""
	}
	return ReadPaneLimit(shown, openedOr(window, undated)), true
}

// resetLine is what a seat's line says of a limit that has reset: when it did.
func (l PaneLimit) resetLine() string {
	return fmt.Sprintf("stopped at the usage limit, which reset at %s", l.Resets.UTC().Format(TicksTimeFormat))
}

// AtTheUsageLimitHeading heads the section of mw status that names each window
// on this host's terminal whose pane is stopped at the harness's usage limit,
// and until when (mw-gq6.473). The report leaves it out when none is.
const AtTheUsageLimitHeading = "AT THE USAGE LIMIT"

// WindowAtLimit is one window whose pane is stopped at the usage limit, as mw
// status shows it: the limit, and whether it had reset when it was read.
type WindowAtLimit struct {
	Window ReapWindow
	Limit  PaneLimit
	Over   bool
}

// WindowsAtLimit reads every window open on the terminal for a pane stopped at
// the usage limit. It writes nothing: a seat at the limit is read off its pane.
// A terminal that is not there, or cannot be read, has none.
func WindowsAtLimit(ctx context.Context, terminal ReapTerminal, now time.Time) []WindowAtLimit {
	if terminal == nil {
		return nil
	}
	open, err := terminal.OpenWindows(ctx)
	if err != nil {
		return nil
	}
	var at []WindowAtLimit
	for _, window := range open {
		if limit, on := paneLimitOf(ctx, terminal, window, now); on {
			at = append(at, WindowAtLimit{Window: window, Limit: limit, Over: limit.Over(now)})
		}
	}
	return at
}

// write is the window's two lines under AtTheUsageLimitHeading: its name and
// id with until when, or when the limit reset if it has, and then the
// harness's own words.
func (w WindowAtLimit) write(b *strings.Builder) {
	when := "until " + w.Limit.Resets.UTC().Format(TicksTimeFormat)
	if w.Over {
		when = "reset " + w.Limit.Resets.UTC().Format(TicksTimeFormat)
	}
	clip(b, fmt.Sprintf("  %s (%s) %s", w.Window.Name, w.Window.ID, when))
	if w.Limit.Notice != "" {
		clip(b, "    "+w.Limit.Notice)
	}
}

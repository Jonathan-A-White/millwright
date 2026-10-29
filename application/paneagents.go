package application

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// paneAgentsWaiting is Claude Code's line while a turn that has ended still
// waits on agents it started: "Waiting for 1 background agent to finish".
var paneAgentsWaiting = regexp.MustCompile(`^[\s│|⎿●·•]*(?i:waiting for \d+ (?:background|local) agents?\b)`)

// paneAgentsStatus is the agent count of Claude Code's status line under the
// input box: "2 background agents", "1 local agent", set off from what is
// before it by the line's start or one of the marks that divide the status
// line's parts, so that a sentence with "1 background agent" inside it is not
// one.
var paneAgentsStatus = regexp.MustCompile(`(?:^|[│|·•⏵])\s*\d+ (?:background|local) agents?\b`)

// PaneAgentsTail is how far up from the bottom of a pane the agent lines are
// read: the waiting line and the status line stand at the foot of the screen,
// and the same words further up are text, a story's title or a handoff, that
// only talks about them.
const PaneAgentsTail = 12

// PaneBackgroundAgents is the line a pane's screen shows of background agents
// still running, trimmed, when it shows one. It is the one reader of them: a
// session that has ended its turn with an agent running looks idle at an empty
// input line, and a reaper that closes its window kills the agent's work
// half done (mw-gq6.833).
func PaneBackgroundAgents(screen string) (string, bool) {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(screen, " ", " "), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > PaneAgentsTail {
		lines = lines[len(lines)-PaneAgentsTail:]
	}
	for _, line := range lines {
		if paneAgentsWaiting.MatchString(line) || paneAgentsStatus.MatchString(line) {
			return strings.Join(strings.Fields(strings.TrimLeft(line, "│|⎿●·•⏵ ")), " "), true
		}
	}
	return "", false
}

// holdsBackgroundAgents is the rule of the closers that look once and are
// looked again later (the Deputy's start, the Millhand's tick): a window whose
// pane shows a background agent still running is not closed, until the agent
// has run for DefaultReapAgentWait since the session was finished (since, the
// handoff's time; zero for a session with no handoff, which is held for as long
// as the agent shows). It gives the line that holds the window. A pane that
// cannot be read for agents may have one, so it holds.
func holdsBackgroundAgents(ctx context.Context, terminal ReapTerminal, window string, since, now time.Time) (string, bool) {
	line, err := terminal.BackgroundAgents(ctx, window)
	switch {
	case err != nil:
		return "the pane could not be read for background agents", true
	case line == "":
		return "", false
	case !since.IsZero() && now.Sub(since) >= DefaultReapAgentWait:
		return line, false
	}
	return line, true
}

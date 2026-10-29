package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// The two screens a Claude Code pane shows while a background agent still
// runs: its waiting line, as Mayor 660's pane showed it when the reaper closed
// it over an Opus Clerk (mw-gq6.833), and the agent status line under the input
// box. Both stand at an empty input line, which is why the pane reads idle.
const (
	aWaitingForAgentScreen = "● Handed over: the handoff is written, Clerk mw-6ww.148.1 is running.\n\n" +
		"  ⎿  Waiting for 1 background agent to finish\n\n" +
		"────────────────────────────────────────────\n" +
		"❯ \n" +
		"────────────────────────────────────────────\n" +
		"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

	anAgentStatusLineScreen = "● Handed over: the handoff is written.\n\n" +
		"────────────────────────────────────────────\n" +
		"❯ \n" +
		"────────────────────────────────────────────\n" +
		"  ⏵⏵ bypass permissions on · 2 background agents · ↓ to manage\n"

	aQuietScreen = "● Handed over: the handoff is written.\n\n" +
		"────────────────────────────────────────────\n" +
		"❯ \n" +
		"────────────────────────────────────────────\n" +
		"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"

	// A pane that only talks about the words, far above its input box.
	aScreenQuotingTheWords = "● The reaper closed it while the pane said 'Waiting for 1 background agent to finish', so I filed it.\n\n" +
		"────────────────────────────────────────────\n" +
		"❯ \n" +
		"────────────────────────────────────────────\n" +
		"  ⏵⏵ bypass permissions on (shift+tab to cycle)\n"
)

// The reap of a finished window, looking every 30 seconds, whose agent wait is
// ten minutes.
func aWatchOfAWindowShowing(t *testing.T, screen string) (application.SeatReap, *aClock, *memoryLog, func(string)) {
	t.Helper()
	windows, files := aFinishedWindow(t)
	if err := windows.Shows("@3", screen); err != nil {
		t.Fatal(err)
	}
	clock := &aClock{now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)}
	log := &memoryLog{}
	show := func(next string) {
		if err := windows.Shows("@3", next); err != nil {
			t.Fatal(err)
		}
	}
	return application.SeatReap{
		Seats: files, Terminal: windows, Log: log,
		Seat: "mayor", Host: "laptop", Window: "@3", WhenIdle: true,
		Interval: 30 * time.Second, Limit: 3 * time.Hour, AgentWait: 10 * time.Minute,
		Now: clock.Now, Sleep: clock.Sleep,
	}, clock, log, show
}

func TestAWindowWithABackgroundAgentRunningIsNotClosedUntilItEnds(t *testing.T) {
	for name, screen := range map[string]string{
		"the waiting line": aWaitingForAgentScreen,
		"the status line":  anAgentStatusLineScreen,
	} {
		t.Run(name, func(t *testing.T) {
			reap, clock, log, show := aWatchOfAWindowShowing(t, screen)
			start := clock.now
			// The agent ends five minutes in.
			reap.Sleep = func(ctx context.Context, d time.Duration) error {
				clock.now = clock.now.Add(d)
				if clock.now.Sub(start) >= 5*time.Minute {
					show(aQuietScreen)
				}
				return nil
			}

			report, err := reap.Run(context.Background())

			if err != nil || report.Outcome != application.ReapClosed {
				t.Fatalf("expected the window closed once the agent ended, it ended %q with %v (log %q)", report.Outcome, err, log.lines)
			}
			if held := clock.now.Sub(start); held < 5*time.Minute {
				t.Errorf("expected the window kept while the agent ran, it was closed after %s", held)
			}
			joined := strings.Join(log.lines, "\n")
			if !strings.Contains(joined, "waiting: ") || !strings.Contains(joined, "background agent") {
				t.Errorf("expected the log to say the reaper waits for a background agent, got %q", log.lines)
			}
			if strings.Contains(joined, "gave up waiting") {
				t.Errorf("expected no giving up, the agent ended in time: %q", log.lines)
			}
		})
	}
}

func TestAWindowWhoseBackgroundAgentRunsPastTheLimitIsClosedSayingWhatWasCut(t *testing.T) {
	for name, want := range map[string]string{
		aWaitingForAgentScreen:  "Waiting for 1 background agent to finish",
		anAgentStatusLineScreen: "2 background agents",
	} {
		t.Run(want, func(t *testing.T) {
			reap, clock, log, _ := aWatchOfAWindowShowing(t, name)
			start := clock.now

			report, err := reap.Run(context.Background())

			if err != nil || report.Outcome != application.ReapClosed {
				t.Fatalf("expected the window closed after the wait, it ended %q with %v (log %q)", report.Outcome, err, log.lines)
			}
			if held := clock.now.Sub(start); held < 10*time.Minute {
				t.Errorf("expected the window kept for the whole wait, it was closed after %s", held)
			}
			joined := strings.Join(log.lines, "\n")
			if !strings.Contains(joined, "gave up waiting for the background agent") || !strings.Contains(joined, want) {
				t.Errorf("expected the log to say what was cut (%q), got %q", want, log.lines)
			}
		})
	}
}

func TestWordsAboutBackgroundAgentsInTheTextOfAPaneAreNotOne(t *testing.T) {
	for name, screen := range map[string]string{"quiet": aQuietScreen, "quoting the words": aScreenQuotingTheWords} {
		t.Run(name, func(t *testing.T) {
			reap, clock, log, _ := aWatchOfAWindowShowing(t, screen)
			start := clock.now

			report, err := reap.Run(context.Background())

			if err != nil || report.Outcome != application.ReapClosed {
				t.Fatalf("expected the window closed at once, it ended %q with %v (log %q)", report.Outcome, err, log.lines)
			}
			if held := clock.now.Sub(start); held > 2*time.Minute {
				t.Errorf("expected no wait for an agent that is not there, it was closed after %s", held)
			}
		})
	}
}

func TestPaneBackgroundAgentsReadsTheLineOfEachKind(t *testing.T) {
	for name, tc := range map[string]struct {
		screen string
		want   string
		on     bool
	}{
		"the waiting line":     {aWaitingForAgentScreen, "Waiting for 1 background agent to finish", true},
		"the status line":      {anAgentStatusLineScreen, "bypass permissions on · 2 background agents · ↓ to manage", true},
		"a quiet pane":         {aQuietScreen, "", false},
		"the words in a quote": {aScreenQuotingTheWords, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, on := application.PaneBackgroundAgents(tc.screen)
			if on != tc.on || (on && !strings.Contains(got, strings.TrimPrefix(tc.want, "bypass permissions on · "))) {
				t.Errorf("expected %q (%v), got %q (%v)", tc.want, tc.on, got, on)
			}
		})
	}
}

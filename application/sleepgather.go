package application

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// SleepSession is one session's result in the vault's runs, and when it was
// written.
type SleepSession struct {
	Story  string
	At     time.Time
	Result SessionResult
}

// SleepRuns reads the sessions' results: the vault's runs/<story>/result.json.
type SleepRuns interface {
	// Sessions are the results written at or after since, oldest first.
	Sessions(ctx context.Context, since time.Time) ([]SleepSession, error)
}

// SleepEarlier finds the dump the last pass kept, for the diff to start from.
type SleepEarlier interface {
	// EarlierDump is the newest dump kept for a date before date (the passes'
	// plans/sleep-<date>/triage.jsonl, else the Mayor's own triage dump), and
	// its name; ok is false when there is none.
	EarlierDump(ctx context.Context, date string) (name, text string, ok bool, err error)
}

// SleepGather gathers the night's bundle (SleepGatherer): the triage dump and
// what moved since the last one, the ready-story queue, each rig's git log, the
// sessions' aggregates and mw report. A part that cannot be read, other than
// the dump, is said so in the bundle and the pass goes on: the agent can be
// told what it is missing, and a bundle short of one part is still worth the
// night.
type SleepGather struct {
	// Dump writes the dump of the live work beads (mw triage dump).
	Dump func(ctx context.Context, w io.Writer) error
	// Mw runs mw itself with these arguments and reports what it printed.
	Mw func(ctx context.Context, args ...string) (string, error)
	// Git reports the git log of the rig checked out at dir since days days
	// ago.
	Git func(ctx context.Context, dir string, days int) (string, error)
	// Rigs are the rigs checked out here, by name.
	Rigs    map[string]string
	Runs    SleepRuns
	Earlier SleepEarlier
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Gather implements SleepGatherer.
func (g SleepGather) Gather(ctx context.Context, window SleepWindow) (SleepBundle, error) {
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	var dump bytes.Buffer
	if err := g.Dump(ctx, &dump); err != nil {
		return nil, fmt.Errorf("the triage dump: %w", err)
	}
	bundle := SleepBundle{{Name: "triage dump", File: "triage.jsonl", Text: dump.String(), Keep: true}}
	bundle = append(bundle, SleepPart{Name: "triage diff", File: "triage-diff.txt", Text: g.diff(ctx, window, dump.String())})
	bundle = append(bundle, SleepPart{Name: "queue", File: "queue.json", Text: g.said(g.Mw(ctx, "queue", "--json"))})
	bundle = append(bundle, SleepPart{Name: "git log", File: "git-log.txt", Text: g.gitLogs(ctx, window)})
	bundle = append(bundle, SleepPart{Name: "runs", File: "runs.txt", Text: g.runs(ctx, window, now)})
	bundle = append(bundle, SleepPart{Name: "report", File: "report.json", Text: g.said(g.Mw(ctx, "report", "--since", window.ReportSince(), "--json"))})
	return bundle, nil
}

// said is a part's text, or the plain sentence that it could not be read.
func (g SleepGather) said(text string, err error) string {
	if err != nil {
		return fmt.Sprintf("(this part could not be read: %v)\n", err)
	}
	return text
}

// diff is what moved between the last kept dump and this one.
func (g SleepGather) diff(ctx context.Context, window SleepWindow, dump string) string {
	name, earlier, ok, err := g.Earlier.EarlierDump(ctx, window.Date)
	if err != nil {
		return fmt.Sprintf("(the earlier dump could not be read: %v)\n", err)
	}
	if !ok {
		return "(there is no earlier dump to diff against: this is the first pass)\n"
	}
	old, err := ReadTriageLines(strings.NewReader(earlier))
	if err != nil {
		return fmt.Sprintf("(the earlier dump %s could not be read: %v)\n", name, err)
	}
	fresh, err := ReadTriageLines(strings.NewReader(dump))
	if err != nil {
		return fmt.Sprintf("(today's dump could not be read: %v)\n", err)
	}
	return "Since " + name + ":\n" + DiffTriage(old, fresh).String()
}

// gitLogs is each rig's log of the last GitDays days, in the rigs' name order.
func (g SleepGather) gitLogs(ctx context.Context, window SleepWindow) string {
	names := make([]string, 0, len(g.Rigs))
	for name := range g.Rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	var logs strings.Builder
	for _, name := range names {
		fmt.Fprintf(&logs, "## %s\n", name)
		log, err := g.Git(ctx, g.Rigs[name], window.GitDays())
		switch {
		case err != nil:
			fmt.Fprintf(&logs, "(the log could not be read: %v)\n", err)
		case strings.TrimSpace(log) == "":
			fmt.Fprintf(&logs, "(no commit in %d days)\n", window.GitDays())
		default:
			logs.WriteString(withNewline(log))
		}
	}
	if len(names) == 0 {
		return "(no rig is checked out on this host)\n"
	}
	return logs.String()
}

// runs is the aggregate of the sessions' results of the window, for each model
// that ran: how many, how many failed, the turns they took, the cache read for
// each turn, the time spent on the model and the tokens.
func (g SleepGather) runs(ctx context.Context, window SleepWindow, now time.Time) string {
	days := window.RunsDays()
	sessions, err := g.Runs.Sessions(ctx, now.AddDate(0, 0, -days))
	if err != nil {
		return fmt.Sprintf("(the sessions could not be read: %v)\n", err)
	}
	return AggregateSleepSessions(sessions, days)
}

// AggregateSleepSessions is the sessions summed up by the model that ran them.
func AggregateSleepSessions(sessions []SleepSession, days int) string {
	if len(sessions) == 0 {
		return fmt.Sprintf("No session result was written in the last %d days.\n", days)
	}
	type sum struct {
		sessions, failed, turns, tokens, cacheRead int
		api                                        time.Duration
		subtypes                                   map[string]int
	}
	byModel := map[string]*sum{}
	for _, s := range sessions {
		model := firstOf(s.Result.ModelID, s.Result.Model, "unknown")
		total := byModel[model]
		if total == nil {
			total = &sum{subtypes: map[string]int{}}
			byModel[model] = total
		}
		total.sessions++
		if s.Result.IsError {
			total.failed++
		}
		total.turns += s.Result.Turns
		total.tokens += s.Result.Fuel.Total()
		total.cacheRead += s.Result.Fuel.CacheRead
		total.api += s.Result.APIDuration
		total.subtypes[firstOf(s.Result.Subtype, "unknown")]++
	}
	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}
	sort.Strings(models)
	var out strings.Builder
	fmt.Fprintf(&out, "Sessions with a result written in the last %d days, by the model that ran them:\n", days)
	for _, model := range models {
		total := byModel[model]
		perTurn := 0
		if total.turns > 0 {
			perTurn = total.cacheRead / total.turns
		}
		var ended []string
		for subtype, n := range total.subtypes {
			ended = append(ended, fmt.Sprintf("%s %d", subtype, n))
		}
		sort.Strings(ended)
		fmt.Fprintf(&out, "- %s: %d sessions (%d failed), %d turns, cache read %s a turn, %s on the model, %s tokens; ended: %s\n",
			model, total.sessions, total.failed, total.turns, Thousands(perTurn), Clock(total.api), Thousands(total.tokens), strings.Join(ended, ", "))
	}
	out.WriteString("The gate's seconds, what landed and the refusal causes are in the report.\n")
	return out.String()
}

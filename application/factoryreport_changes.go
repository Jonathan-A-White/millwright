package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// ChangeDays is how far back the report looks for factory changes,
// ChangeWindowDays how many days a snapshot of the key metrics and mw report
// --since a landing take in, and ChangeVerdictDays how many days before and
// after a landing a change is judged on: it is too soon to judge until that
// many days after it are over.
const (
	ChangeDays        = 30
	ChangeWindowDays  = 7
	ChangeVerdictDays = 14
)

// ChangeBand is how far a metric may move the wrong way, as a share of the days
// before, and not be called WORSE (PROVISIONAL, the Governor to confirm).
const ChangeBand = 0.10

// The verdicts of a factory change: KEPT when its metric moved the way its
// story expected or held inside the ChangeBand, WORSE when it went the other way
// past it, NONE when the story expected to move none. ChangeLevel is how mw
// report --since a landing says a measure held.
const (
	ChangeKept      = "KEPT"
	ChangeWorse     = "WORSE"
	ChangeNone      = "NONE"
	ChangeLevel     = "level"
	ChangeTooSoon   = "too soon"
	ChangeNoData    = "no data"
	ChangeNoExpect  = "no Expect"
	ChangeBadExpect = "unreadable Expect"
)

// MailFactoryChange starts the subject of the mail that tells the Mayor a factory
// change made things worse.
const MailFactoryChange = "Worse"

// changeMetric reads one metric of the health report from the entries of a
// stretch of days: its value, and whether those days had anything to measure
// it with.
type changeMetric struct {
	value  func([]reportEntry) (float64, bool)
	format func(float64) string
}

// snapshotMetric is a metric that is a count at a moment (his review queue, the
// demos waiting) and not a sum over days: it is read from the daily snapshots,
// and compared as the mean of those kept in each stretch of days (mw-gq6.677).
type snapshotMetric struct {
	of     func(MetricSnapshot) *float64
	format func(float64) string
}

var snapshotMetrics = map[string]snapshotMetric{
	"review queue":  {func(s MetricSnapshot) *float64 { return s.ReviewQueue }, oneDecimal},
	"demos waiting": {func(s MetricSnapshot) *float64 { return s.DemosWaiting }, oneDecimal},
}

// formatOf is how a metric's figure reads.
func formatOf(metric string) func(float64) string {
	if m, ok := snapshotMetrics[metric]; ok {
		return m.format
	}
	return changeMetrics[metric].format
}

var changeMetrics = map[string]changeMetric{
	"refusals": {func(es []reportEntry) (float64, bool) {
		q := sumQuality(es)
		return q.RefusalRate, q.Landed+q.Refused > 0
	}, func(v float64) string { return fmt.Sprintf("%.0f%%", v) }},
	"attempts/landing": {func(es []reportEntry) (float64, bool) {
		q := sumQuality(es)
		return q.AttemptsPerLanding, q.Landed > 0
	}, func(v float64) string { return fmt.Sprintf("%.2f", v) }},
	"bugs/landing": {func(es []reportEntry) (float64, bool) {
		q := sumQuality(es)
		return q.BugsPerLanding, q.Landed > 0
	}, func(v float64) string { return fmt.Sprintf("%.2f", v) }},
	// escaped bugs counts the bugs filed in the days that a landing let through,
	// over days in which anything landed, was refused or was filed.
	"escaped bugs": {func(es []reportEntry) (float64, bool) {
		q := sumQuality(es)
		return float64(q.EscapedBugs), !q.Empty()
	}, func(v float64) string { return fmt.Sprintf("%.0f", v) }},
	"dispatch-to-landing": {func(es []reportEntry) (float64, bool) {
		s := sumSpeed(es)
		return s.DispatchToLandingS, s.Measured > 0
	}, func(v float64) string { return gateClock(seconds(v)) }},
	"tokens/landing": {func(es []reportEntry) (float64, bool) {
		f := sumFuel(es)
		return f.TokensPerLanding, f.WithTokens > 0
	}, func(v float64) string { return Thousands(int(v)) }},
	"$/landing": {func(es []reportEntry) (float64, bool) {
		f := sumFuel(es)
		return f.USDPerLanding, f.WithCost > 0
	}, func(v float64) string { return fmt.Sprintf("$%.2f", v) }},
}

func oneDecimal(v float64) string { return fmt.Sprintf("%.1f", v) }

// snapshotMean is the mean of the metric over the snapshots of the days from up
// to, not including, to, and whether any of them had it.
func snapshotMean(metric snapshotMetric, kept []MetricSnapshot, from, to time.Time) (float64, bool) {
	sum, n := 0.0, 0
	for _, s := range kept {
		day, err := time.Parse(LedgerDate, s.Day)
		if err != nil || day.Before(from) || !day.Before(to) {
			continue
		}
		if v := metric.of(s); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// FactoryChange is one millwright landing of the last ChangeDays days: what its
// story expected, and how that metric stood in the ChangeVerdictDays days before
// the landing day and in the same number after it, factory-wide. The landing's
// own day is in neither. Before and After are nil where there was nothing to
// compare.
type FactoryChange struct {
	Story   string        `json:"story"`
	Title   string        `json:"title"`
	Landed  string        `json:"landed"`
	Expect  domain.Expect `json:"expect"`
	Problem string        `json:"problem,omitempty"`
	Before  *float64      `json:"before,omitempty"`
	After   *float64      `json:"after,omitempty"`
	Verdict string        `json:"verdict"`
	// AfterEnds is the last day the ChangeVerdictDays after the landing take in.
	AfterEnds string `json:"after_ends"`
	// EscapedBugs are the bugs that escaped the landing so far, EscapedIDs
	// their stories; EscapedTooSoon says the EscapeDays after it, which end
	// EscapedEnds, are not over yet.
	EscapedBugs    int      `json:"escaped_bugs"`
	EscapedIDs     []string `json:"escaped_ids,omitempty"`
	EscapedTooSoon bool     `json:"escaped_too_soon,omitempty"`
	EscapedEnds    string   `json:"escaped_ends"`
}

// judgeChange says whether a metric moved the way a story expected or held
// inside the ChangeBand of the days before, KEPT, or went the other way past it,
// WORSE.
func judgeChange(e domain.Expect, before, after float64) string {
	held := math.Abs(after-before) <= ChangeBand*math.Abs(before)+1e-9
	if held || (after > before) == (e.Direction == "up") {
		return ChangeKept
	}
	return ChangeWorse
}

// factoryChanges finds the millwright landings among entries, and measures
// each, with the bugs that escaped it by escaped; kept are the daily snapshots a
// metric that is a count at a moment is read from.
func factoryChanges(all []reportEntry, kept []MetricSnapshot, stories map[string]StoryDetail, escaped map[string][]string, now time.Time) []FactoryChange {
	today := reportDay(now)
	first := map[string]reportEntry{}
	for _, e := range all {
		if !e.landed || e.bug || e.id == "" || e.rig != FactoryRig || e.at.Before(today.AddDate(0, 0, -ChangeDays)) {
			continue
		}
		if was, seen := first[e.id]; !seen || e.at.Before(was.at) {
			first[e.id] = e
		}
	}
	var changes []FactoryChange
	for id, landing := range first {
		d, known := stories[id]
		change := FactoryChange{Story: id, Title: id, Landed: landing.at.Format(LedgerDate),
			AfterEnds:   landing.at.AddDate(0, 0, ChangeVerdictDays).Format(LedgerDate),
			EscapedBugs: len(escaped[id]), EscapedIDs: escaped[id],
			EscapedTooSoon: !escapeOver(landing.at, today), EscapedEnds: landing.at.AddDate(0, 0, EscapeDays).Format(LedgerDate)}
		if known && strings.TrimSpace(d.Story.Title) != "" {
			change.Title = strings.Join(strings.Fields(d.Story.Title), " ")
		}
		expect, err := domain.ParseExpect(d.Acceptance)
		switch {
		case errors.Is(err, domain.ErrNoExpect):
			change.Verdict = ChangeNoExpect
		case err != nil:
			change.Verdict, change.Problem = ChangeBadExpect, err.Error()
		case !today.After(landing.at.AddDate(0, 0, ChangeVerdictDays)):
			change.Expect, change.Verdict = expect, ChangeTooSoon
		case expect.Metric == domain.ExpectNone:
			change.Expect, change.Verdict = expect, ChangeNone
		default:
			change.Expect = expect
			change.measure(all, kept, landing.at)
		}
		changes = append(changes, change)
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Landed != changes[j].Landed {
			return changes[i].Landed < changes[j].Landed
		}
		return changes[i].Story < changes[j].Story
	})
	return changes
}

// measure sets the verdict of a change whose story named a metric, once the
// ChangeVerdictDays after the landing day are over.
func (c *FactoryChange) measure(all []reportEntry, kept []MetricSnapshot, landed time.Time) {
	beforeFrom, beforeTo := landed.AddDate(0, 0, -ChangeVerdictDays), landed
	afterFrom, afterTo := landed.AddDate(0, 0, 1), landed.AddDate(0, 0, ChangeVerdictDays+1)
	var before, after float64
	var haveBefore, haveAfter bool
	if metric, ok := snapshotMetrics[c.Expect.Metric]; ok {
		before, haveBefore = snapshotMean(metric, kept, beforeFrom, beforeTo)
		after, haveAfter = snapshotMean(metric, kept, afterFrom, afterTo)
	} else {
		metric := changeMetrics[c.Expect.Metric]
		before, haveBefore = metric.value(entriesBetween(all, beforeFrom, beforeTo))
		after, haveAfter = metric.value(entriesBetween(all, afterFrom, afterTo))
	}
	if !haveBefore || !haveAfter {
		c.Verdict = ChangeNoData
		return
	}
	c.Before, c.After = &before, &after
	c.Verdict = judgeChange(c.Expect, before, after)
}

// entriesBetween are the entries of the days from up to, not including, to.
func entriesBetween(all []reportEntry, from, to time.Time) []reportEntry {
	var in []reportEntry
	for _, e := range all {
		if !e.at.Before(from) && e.at.Before(to) {
			in = append(in, e)
		}
	}
	return in
}

// writeChanges prints FACTORY CHANGES: each change with its Expect, how the
// metric moved and its verdict, in at most ReportWidth columns.
func writeChanges(b *strings.Builder, changes []FactoryChange) {
	fmt.Fprintf(b, "\nFACTORY CHANGES (millwright landings, %d days)\n", ChangeDays)
	fmt.Fprintf(b, "  %d days before vs %d after; WORSE past a %.0f%% band\n", ChangeVerdictDays, ChangeVerdictDays, ChangeBand*100)
	if len(changes) == 0 {
		b.WriteString("  none landed\n")
		return
	}
	unnamed, unnamedEscaped := 0, 0
	for _, c := range changes {
		if c.Verdict == ChangeNoExpect {
			unnamed++
			unnamedEscaped += c.EscapedBugs
			continue
		}
		fmt.Fprintf(b, "%s\n", fitLine(fmt.Sprintf("%s (%s)", c.Title, c.Story), ReportWidth))
		switch c.Verdict {
		case ChangeBadExpect:
			fmt.Fprintf(b, "  landed %s, Expect: not readable\n", c.Landed)
		case ChangeNone:
			fmt.Fprintf(b, "  landed %s, Expect: none\n  %s: no metric to judge\n", c.Landed, ChangeNone)
		case ChangeTooSoon:
			fmt.Fprintf(b, "  landed %s, Expect: %s\n  too soon: the %d days after end %s\n", c.Landed, c.Expect, ChangeVerdictDays, c.AfterEnds)
		case ChangeNoData:
			fmt.Fprintf(b, "  landed %s, Expect: %s\n  no data in the %d days before or after\n", c.Landed, c.Expect, ChangeVerdictDays)
		default:
			fmt.Fprintf(b, "  landed %s, Expect: %s\n", c.Landed, c.Expect)
			format := formatOf(c.Expect.Metric)
			moved := ""
			if *c.Before != 0 {
				moved = fmt.Sprintf(" (%+.0f%%)", 100*fraction(*c.After-*c.Before, *c.Before))
			}
			fmt.Fprintf(b, "  %s\n", fitLine(fmt.Sprintf("%s → %s%s: %s", format(*c.Before), format(*c.After), moved, c.Verdict), ReportWidth-2))
		}
		if c.EscapedTooSoon {
			fmt.Fprintf(b, "  escaped bugs %d so far, too soon: %d days end %s\n", c.EscapedBugs, EscapeDays, c.EscapedEnds)
		} else {
			fmt.Fprintf(b, "  escaped bugs %d in the %d days after\n", c.EscapedBugs, EscapeDays)
		}
	}
	if unnamed > 0 {
		fmt.Fprintf(b, "  %d landed with no Expect named (--json lists them)\n", unnamed)
		fmt.Fprintf(b, "  escaped bugs of those %d\n", unnamedEscaped)
	}
}

// unfixed are the changes of worse that no story of stories names as its fix,
// by a line "Fixes: <id>" in its description or acceptance (read by
// domain.LinesBeginning). A story does not fix itself.
func unfixed(worse []FactoryChange, stories []StoryDetail) []FactoryChange {
	fixed := map[string]bool{}
	for _, d := range stories {
		for _, line := range domain.LinesBeginning(d.Description+"\n"+d.Acceptance, "Fixes") {
			for _, id := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
				if id = strings.TrimRight(id, ".;:"); id != d.Story.ID {
					fixed[id] = true
				}
			}
		}
	}
	var out []FactoryChange
	for _, c := range worse {
		if !fixed[c.Story] {
			out = append(out, c)
		}
	}
	return out
}

// worse are the changes that made things worse.
func worse(changes []FactoryChange) []FactoryChange {
	var out []FactoryChange
	for _, c := range changes {
		if c.Verdict == ChangeWorse {
			out = append(out, c)
		}
	}
	return out
}

// ChangeWorseKey is the key a WORSE change's telling of the Mayor is noted under.
func ChangeWorseKey(story string) string { return "factory-change-worse." + story }

// MetricSnapshot is the factory's key metrics over the week to a day, as mw
// report send keeps them once a day: a past week is compared with another by
// reading two of them. A metric with nothing to measure it is left out.
type MetricSnapshot struct {
	// Day is the UTC day the snapshot was taken, and the week is the seven days
	// before it and itself.
	Day                string   `json:"day"`
	Landed             int      `json:"landed"`
	Refused            int      `json:"refused"`
	RefusalRate        float64  `json:"refusals"`
	AttemptsPerLanding *float64 `json:"attempts_per_landing,omitempty"`
	BugsPerLanding     *float64 `json:"bugs_per_landing,omitempty"`
	EscapedBugs        *float64 `json:"escaped_bugs,omitempty"`
	DispatchToLandingS *float64 `json:"dispatch_to_landing_s,omitempty"`
	TokensPerLanding   *float64 `json:"tokens_per_landing,omitempty"`
	USDPerLanding      *float64 `json:"usd_per_landing,omitempty"`
	// ReviewQueue and DemosWaiting are his time as mw status DAMPERS counts it on
	// the day (not over the week): the open cards, open demos and unrun hands
	// steps, and the demos waiting DemoWaitingDays or more. Left out when the
	// report was not given the means to count them.
	ReviewQueue  *float64 `json:"review_queue,omitempty"`
	DemosWaiting *float64 `json:"demos_waiting,omitempty"`
}

// takeSnapshot is the key metrics of the whole factory over the week to today.
func takeSnapshot(all []reportEntry, review *ReviewTime, now time.Time) MetricSnapshot {
	today := reportDay(now)
	week := entriesBetween(all, today.AddDate(0, 0, -ChangeWindowDays+1), today.AddDate(0, 0, 1))
	q := sumQuality(week)
	s := MetricSnapshot{Day: today.Format(LedgerDate), Landed: q.Landed, Refused: q.Refused, RefusalRate: q.RefusalRate}
	set := func(into **float64, name string) {
		if v, ok := changeMetrics[name].value(week); ok {
			*into = &v
		}
	}
	set(&s.AttemptsPerLanding, "attempts/landing")
	set(&s.BugsPerLanding, "bugs/landing")
	set(&s.EscapedBugs, "escaped bugs")
	set(&s.DispatchToLandingS, "dispatch-to-landing")
	set(&s.TokensPerLanding, "tokens/landing")
	set(&s.USDPerLanding, "$/landing")
	if review != nil {
		queue, demos := float64(review.Queue), float64(review.DemosWaiting)
		s.ReviewQueue, s.DemosWaiting = &queue, &demos
	}
	return s
}

// record writes what a report that was given the means to write does: it tells
// the Mayor, once, of each change that is WORSE, and keeps the day's snapshot
// of the key metrics if today's is not kept yet. A report with none of the
// means, or one narrowed to a rig or host, writes nothing.
func (r FactoryReport) record(ctx context.Context, result FactoryReportResult, all []reportEntry, now time.Time) error {
	var problems []string
	if r.Notes != nil && r.Mailbox != nil {
		for _, c := range worse(result.Changes) {
			if err := r.tellMayor(ctx, c, result.Window.Until); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if r.Snapshots != nil && r.Rig == "" && r.Host == "" {
		if err := r.keepSnapshot(ctx, takeSnapshot(all, result.Review, now)); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("reporting quality: %s", strings.Join(problems, "; "))
	}
	return nil
}

// tellMayor mails the Mayor that a change is WORSE, unless a note says it did.
// The note is made first, so a mail that is sent and then not remembered is not
// sent twice; a mail that could not be sent clears the note, to be tried again.
func (r FactoryReport) tellMayor(ctx context.Context, c FactoryChange, at time.Time) error {
	key := ChangeWorseKey(c.Story)
	told, err := r.Notes.Note(ctx, key)
	if err != nil {
		return fmt.Errorf("%s: whether the Mayor was told: %w", c.Story, err)
	}
	if told != "" {
		return nil
	}
	if err := r.Notes.SetNote(ctx, key, at.UTC().Format(LedgerDate)); err != nil {
		return fmt.Errorf("%s: noting that the Mayor is told: %w", c.Story, err)
	}
	format := formatOf(c.Expect.Metric)
	body := fmt.Sprintf("%s (%s) landed %s expecting %s, and %s went the other way past the %.0f%% band: "+
		"%s in the %d days before, %s in the %d days after (the whole factory, %s to %s).\n\n"+
		"Look at it, and either fix it, take it back, or say the measure was noise: mw report shows FACTORY CHANGES.\n",
		c.Title, c.Story, c.Landed, c.Expect, c.Expect.Metric, ChangeBand*100,
		format(*c.Before), ChangeVerdictDays, format(*c.After), ChangeVerdictDays,
		c.Landed, c.AfterEnds)
	if _, err := r.Mailbox.Send(ctx, NewMessage{From: MwSeat, To: MayorMailbox,
		Subject: MailFactoryChange + ": " + c.Title + " made " + c.Expect.Metric + " worse", Body: body}); err != nil {
		r.Notes.SetNote(ctx, key, "")
		return fmt.Errorf("%s: the mail to %s could not be sent: %w", c.Story, MayorMailbox, err)
	}
	return nil
}

// keepSnapshot appends the snapshot, unless one of the same day is already kept.
func (r FactoryReport) keepSnapshot(ctx context.Context, s MetricSnapshot) error {
	kept, err := r.Snapshots.Snapshots(ctx)
	if err != nil {
		return fmt.Errorf("the key metrics kept so far: %w", err)
	}
	for _, was := range kept {
		if was.Day == s.Day {
			return nil
		}
	}
	if err := r.Snapshots.Append(ctx, s); err != nil {
		return fmt.Errorf("keeping the day's key metrics: %w", err)
	}
	return nil
}

package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ChangeDays is how far back the report looks for factory changes, and
// ChangeWindowDays how many days before and after a landing it compares.
const (
	ChangeDays       = 30
	ChangeWindowDays = 7
)

// ChangeBand is how far a metric may move the wrong way, as a share of the week
// before, and not be called WORSE (PROVISIONAL, the Governor to confirm).
const ChangeBand = 0.10

// ExpectNone is the metric a story names when it does not expect to move one.
const ExpectNone = "none"

// The verdicts of a factory change.
const (
	ChangeWorse     = "WORSE"
	ChangeBetter    = "better"
	ChangeLevel     = "level"
	ChangeTooSoon   = "too soon"
	ChangeNoData    = "no data"
	ChangeNone      = "none expected"
	ChangeNoExpect  = "no Expect"
	ChangeBadExpect = "unreadable Expect"
)

// MailFactoryChange starts the subject of the mail that tells the Mayor a factory
// change made things worse.
const MailFactoryChange = "Worse"

// ErrNoExpect says acceptance criteria carry no line beginning Expect:.
var ErrNoExpect = errors.New("no Expect: line")

// Expect is what a millwright story says it should move: one metric of the
// health report and the way it should go. The metric none has no direction.
type Expect struct {
	Metric    string `json:"metric"`
	Direction string `json:"direction,omitempty"`
}

// String is the Expect as a story spells it after the colon.
func (e Expect) String() string {
	if e.Direction == "" {
		return e.Metric
	}
	return e.Metric + " " + e.Direction
}

// expectLine is a line of acceptance criteria that names an Expect, after an
// optional list number or bullet.
var expectLine = regexp.MustCompile(`(?i)^\s*(?:[-*]\s+|\d+[.)]\s+)?expect:\s*(.*)$`)

// ParseExpect reads the 'Expect: <metric> <up|down>' line of a story's
// acceptance criteria, or 'Expect: none, because <reason>'. It reports
// ErrNoExpect when there is no such line and another error when the line cannot
// be read.
func ParseExpect(acceptance string) (Expect, error) {
	for _, line := range strings.Split(acceptance, "\n") {
		m := expectLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		words := strings.Fields(strings.ToLower(m[1]))
		trim := func(w string) string { return strings.TrimRight(w, ".,;:") }
		if len(words) > 0 && trim(words[0]) == ExpectNone {
			return Expect{Metric: ExpectNone}, nil
		}
		// A metric of two words, escaped bugs, is read as one.
		if len(words) >= 3 {
			if _, known := changeMetrics[words[0]+" "+words[1]]; known {
				words = append([]string{words[0] + " " + words[1]}, words[2:]...)
			}
		}
		if len(words) >= 2 {
			metric, direction := trim(words[0]), trim(words[1])
			if _, known := changeMetrics[metric]; known && (direction == "up" || direction == "down") {
				return Expect{Metric: metric, Direction: direction}, nil
			}
		}
		return Expect{}, fmt.Errorf("%q is not 'Expect: <metric> <up|down>' with a metric of %s, or 'Expect: none'", strings.TrimSpace(line), strings.Join(ExpectMetrics(), ", "))
	}
	return Expect{}, ErrNoExpect
}

// ExpectMetrics are the metrics a story may name, in the order the report shows them.
func ExpectMetrics() []string {
	return []string{"refusals", "attempts/landing", "bugs/landing", "escaped bugs", "dispatch-to-landing", "tokens/landing", "$/landing"}
}

// changeMetric reads one metric of the health report from the entries of a
// stretch of days: its value, and whether those days had anything to measure
// it with.
type changeMetric struct {
	value  func([]reportEntry) (float64, bool)
	format func(float64) string
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

// FactoryChange is one millwright landing of the last ChangeDays days: what its
// story expected, and how that metric stood in the ChangeWindowDays days before
// the landing day and in the same number after it, factory-wide. The landing's
// own day is in neither. Before and After are nil where there was nothing to
// compare.
type FactoryChange struct {
	Story   string   `json:"story"`
	Title   string   `json:"title"`
	Landed  string   `json:"landed"`
	Expect  Expect   `json:"expect"`
	Problem string   `json:"problem,omitempty"`
	Before  *float64 `json:"before,omitempty"`
	After   *float64 `json:"after,omitempty"`
	Verdict string   `json:"verdict"`
	// AfterEnds is the last day the week after takes in.
	AfterEnds string `json:"after_ends"`
	// EscapedBugs are the bugs that escaped the landing so far, EscapedIDs
	// their stories; EscapedTooSoon says the EscapeDays after it, which end
	// EscapedEnds, are not over yet.
	EscapedBugs    int      `json:"escaped_bugs"`
	EscapedIDs     []string `json:"escaped_ids,omitempty"`
	EscapedTooSoon bool     `json:"escaped_too_soon,omitempty"`
	EscapedEnds    string   `json:"escaped_ends"`
}

// judgeChange says whether a metric moved the way a story expected, inside the
// ChangeBand of the week before: better, level, or WORSE.
func judgeChange(e Expect, before, after float64) string {
	if math.Abs(after-before) <= ChangeBand*math.Abs(before)+1e-9 {
		return ChangeLevel
	}
	if (after > before) == (e.Direction == "up") {
		return ChangeBetter
	}
	return ChangeWorse
}

// factoryChanges finds the millwright landings among entries, and measures
// each, with the bugs that escaped it by escaped.
func factoryChanges(all []reportEntry, stories map[string]StoryDetail, escaped map[string][]string, now time.Time) []FactoryChange {
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
			AfterEnds:   landing.at.AddDate(0, 0, ChangeWindowDays).Format(LedgerDate),
			EscapedBugs: len(escaped[id]), EscapedIDs: escaped[id],
			EscapedTooSoon: !escapeOver(landing.at, today), EscapedEnds: landing.at.AddDate(0, 0, EscapeDays).Format(LedgerDate)}
		if known && strings.TrimSpace(d.Story.Title) != "" {
			change.Title = strings.Join(strings.Fields(d.Story.Title), " ")
		}
		expect, err := ParseExpect(d.Acceptance)
		switch {
		case errors.Is(err, ErrNoExpect):
			change.Verdict = ChangeNoExpect
		case err != nil:
			change.Verdict, change.Problem = ChangeBadExpect, err.Error()
		case expect.Metric == ExpectNone:
			change.Expect, change.Verdict = expect, ChangeNone
		default:
			change.Expect = expect
			change.measure(all, landing.at, today)
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

// measure sets the verdict of a change whose story named a metric: too soon
// until the week after the landing day is over.
func (c *FactoryChange) measure(all []reportEntry, landed, today time.Time) {
	if !today.After(landed.AddDate(0, 0, ChangeWindowDays)) {
		c.Verdict = ChangeTooSoon
		return
	}
	metric := changeMetrics[c.Expect.Metric]
	before, haveBefore := metric.value(entriesBetween(all, landed.AddDate(0, 0, -ChangeWindowDays), landed))
	after, haveAfter := metric.value(entriesBetween(all, landed.AddDate(0, 0, 1), landed.AddDate(0, 0, ChangeWindowDays+1)))
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

// writeChanges prints FACTORY CHANGES: each change with its Expect and how the
// metric moved, in at most ReportWidth columns.
func writeChanges(b *strings.Builder, changes []FactoryChange) {
	fmt.Fprintf(b, "\nFACTORY CHANGES (millwright landings, %d days)\n", ChangeDays)
	fmt.Fprintf(b, "  %d days before vs %d after; WORSE past a %.0f%% band\n", ChangeWindowDays, ChangeWindowDays, ChangeBand*100)
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
			fmt.Fprintf(b, "  landed %s, Expect: none\n", c.Landed)
		case ChangeTooSoon:
			fmt.Fprintf(b, "  landed %s, Expect: %s\n  too soon: the week after ends %s\n", c.Landed, c.Expect, c.AfterEnds)
		case ChangeNoData:
			fmt.Fprintf(b, "  landed %s, Expect: %s\n  no data in the week before or after\n", c.Landed, c.Expect)
		default:
			fmt.Fprintf(b, "  landed %s, Expect: %s\n", c.Landed, c.Expect)
			format := changeMetrics[c.Expect.Metric].format
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
}

// takeSnapshot is the key metrics of the whole factory over the week to today.
func takeSnapshot(all []reportEntry, now time.Time) MetricSnapshot {
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
		if err := r.keepSnapshot(ctx, takeSnapshot(all, now)); err != nil {
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
	format := changeMetrics[c.Expect.Metric].format
	body := fmt.Sprintf("%s (%s) landed %s expecting %s, and %s went the other way past the %.0f%% band: "+
		"%s in the %d days before, %s in the %d days after (the whole factory, %s to %s).\n\n"+
		"Look at it, and either fix it, take it back, or say the measure was noise: mw report shows FACTORY CHANGES.\n",
		c.Title, c.Story, c.Landed, c.Expect, c.Expect.Metric, ChangeBand*100,
		format(*c.Before), ChangeWindowDays, format(*c.After), ChangeWindowDays,
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

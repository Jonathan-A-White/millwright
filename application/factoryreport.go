package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Jonathan-A-White/millwright/domain"
)

// DefaultReportDays is how far back mw report looks when it is not told.
const DefaultReportDays = 7

// ReportWidth is the widest a line of mw report is, the phone width of mw status.
const ReportWidth = 60

// ReportUnknown is what a ledger row's rig, host or model is called when no
// story, benchmark or ledger column says.
const ReportUnknown = "unknown"

// CauseUnset counts the refusals whose story names no cause class.
const CauseUnset = "unset"

// ParseReportSince reads --since: "7d" is that many days before now, a day
// (2026-10-09) is that day's start, and anything RFC 3339 is that moment. Empty
// is DefaultReportDays days. The result is cut back to the start of its UTC
// day, because the ledger dates its lines by day.
func ParseReportSince(text string, now time.Time) (time.Time, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return reportDay(now.AddDate(0, 0, -DefaultReportDays)), nil
	case strings.HasSuffix(text, "d"):
		if days, err := strconv.Atoi(strings.TrimSuffix(text, "d")); err == nil && days > 0 {
			return reportDay(now.AddDate(0, 0, -days)), nil
		}
	}
	if at, err := time.Parse(time.RFC3339, text); err == nil {
		return reportDay(at), nil
	}
	if at, err := time.Parse(LedgerDate, text); err == nil {
		return reportDay(at), nil
	}
	return time.Time{}, fmt.Errorf("--since %q is neither a number of days (7d), a day (2026-10-09) nor a UTC time (2026-10-09T20:00:00Z)", text)
}

// reportDay is the start of t's UTC day.
func reportDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// FactoryReport sums the factory's quality over a window: from the ledger what
// landed and what was refused and why, from the stories the attempts, causes
// and bugs, and from the landed stories' comments what the Tester found. After
// quality it sums speed, from the close-outs' benchmarks, and fuel, from the
// ledger's tokens and the result files' cost, each against the window before.
// It reads and writes nothing else.
type FactoryReport struct {
	Tracker TesterStories
	Files   TesterRunFiles
	// Benchmarks, when set, names the rig and host of a ledger row whose story
	// is gone, from the close-out that measured it, and is where speed is read.
	Benchmarks BenchmarkBook
	// Seat is whose ledger is read.
	Seat string
	// Since is where the window starts; zero is DefaultReportDays before now.
	Since time.Time
	// Rig and Host, when set, narrow every block to that rig or host.
	Rig, Host string
	// JSON prints the result as one object instead of text.
	JSON bool
	Now  func() time.Time
	Out  io.Writer
}

// ReportWindow is a stretch of days, from the start of Since to Until.
type ReportWindow struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

// MarshalJSON writes the window as the days it spans.
func (w ReportWindow) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Since string `json:"since"`
		Until string `json:"until"`
	}{w.Since.UTC().Format(LedgerDate), w.Until.UTC().Format(LedgerDate)})
}

// ReasonCount is how many refusals were given one reason code.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// Quality is the measures of one window of one block.
type Quality struct {
	Landed  int `json:"landed"`
	Refused int `json:"refused"`
	// RefusalRate is the share of the ledger's rows that were refusals, in percent.
	RefusalRate float64       `json:"refusal_rate"`
	Reasons     []ReasonCount `json:"refused_by_reason"`
	// Attempts are the attempts the landings took in all, a landing counted at
	// least one; AttemptsPerLanding is their mean.
	Attempts           int            `json:"attempts"`
	AttemptsPerLanding float64        `json:"attempts_per_landing"`
	Causes             map[string]int `json:"causes"`
	// Uncaused are the ids of the refused stories counted under CauseUnset, once
	// each and sorted; cheap win R1 names them. They are not in the JSON.
	Uncaused           []string `json:"-"`
	BugsFiled          int      `json:"bugs_filed"`
	BugsPerLanding     float64  `json:"bugs_per_landing"`
	Findings           int      `json:"tester_findings"`
	FindingsPerLanding float64  `json:"findings_per_landing"`
}

// Empty says nothing landed, was refused or was filed as a bug.
func (q Quality) Empty() bool { return q.Landed == 0 && q.Refused == 0 && q.BugsFiled == 0 }

// ReportBlock is one rig, model or host: its quality, speed and fuel in the
// window and in the one before it, and how each number moved. Arrows holds a
// Trend for each number that both windows have, by measure.
type ReportBlock struct {
	Name          string           `json:"name"`
	Quality       Quality          `json:"quality"`
	Previous      Quality          `json:"previous"`
	Speed         Speed            `json:"speed"`
	SpeedPrevious Speed            `json:"speed_previous"`
	Fuel          FuelUse          `json:"fuel"`
	FuelPrevious  FuelUse          `json:"fuel_previous"`
	Arrows        map[string]Trend `json:"arrows"`
	// Gates are the gate times on the block's rigs or hosts; a model has none.
	Gates []GateTrend `json:"gates,omitempty"`
}

// FactoryReportResult is what mw report found.
type FactoryReportResult struct {
	Window         ReportWindow  `json:"window"`
	PreviousWindow ReportWindow  `json:"previous_window"`
	Rigs           []ReportBlock `json:"rigs"`
	Models         []ReportBlock `json:"models"`
	Hosts          []ReportBlock `json:"hosts"`
	// CheapWins are the suggestions every rule that held made, in precedence
	// order; the first MaxCheapWins are Shown.
	CheapWins []CheapWin `json:"cheap_wins"`
	// Whole is the whole factory as one block, what Summary reads; nil when
	// nothing landed, was refused or was filed. It is not in the JSON.
	Whole *ReportBlock `json:"-"`
}

// reportEntry is one thing the window saw: a landing, a refusal or a bug filed.
type reportEntry struct {
	id               string
	previous         bool
	rig, host, model string
	landed, refused  bool
	bug              bool
	reason           string
	attempts         int
	cause            string
	exhausted        bool
	findings         int
	// measured says a benchmark gave the landing a wall time, actualS, and
	// ratio is that over its par, 0 when it had none.
	measured bool
	actualS  float64
	ratio    float64
	// tokens and costUSD are the fuel of every attempt of a landed story.
	tokens  int
	costUSD float64
}

// Run reads the ledger and the tracker and prints the report.
func (r FactoryReport) Run(ctx context.Context) (FactoryReportResult, error) {
	if r.Tracker == nil || r.Files == nil {
		return FactoryReportResult{}, fmt.Errorf("reporting quality: it needs the tracker and the vault")
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	since := r.Since
	if since.IsZero() {
		since = now.AddDate(0, 0, -DefaultReportDays)
	}
	since = reportDay(since)
	if !since.Before(now) {
		return FactoryReportResult{}, fmt.Errorf("reporting quality: --since %s is not before now", since.Format(LedgerDate))
	}
	// The window before is as many whole days long as the one asked for.
	days := int(reportDay(now).Sub(since).Hours() / 24)
	if days < 1 {
		days = 1
	}
	result := FactoryReportResult{
		Window:         ReportWindow{Since: since, Until: now},
		PreviousWindow: ReportWindow{Since: since.AddDate(0, 0, -days), Until: since},
	}

	stories, err := r.Tracker.StoriesSince(ctx, result.PreviousWindow.Since)
	if err != nil {
		return result, fmt.Errorf("reporting quality: %w", err)
	}
	byID := make(map[string]StoryDetail, len(stories))
	for _, d := range stories {
		byID[d.Story.ID] = d
	}
	ledger, err := r.Files.ReadLedger(ctx, r.Seat)
	if err != nil {
		return result, fmt.Errorf("reporting quality: the %s seat's ledger: %w", r.Seat, err)
	}
	var recent []Benchmark
	if r.Benchmarks != nil {
		recent, err = r.Benchmarks.Recent(ctx)
		if err != nil {
			return result, fmt.Errorf("reporting quality: the benchmarks: %w", err)
		}
	}
	benchmarks := make(map[string]Benchmark, len(recent))
	landedIn := map[string]Benchmark{}
	for _, b := range recent {
		benchmarks[b.Story] = b
		if b.Landed && b.ActualS > 0 {
			landedIn[b.Story] = b
		}
	}
	// A story's fuel is every ledger row's, a landing's attempts before it too.
	tokensOf := map[string]int{}
	for _, line := range ledger {
		if row, ok := ParseLedgerLine(line); ok {
			tokensOf[row.StoryID] += row.Tokens
		}
	}

	var entries []reportEntry
	var landedIDs []string
	landedSeen := map[string]bool{}
	for _, line := range ledger {
		row, ok := ParseLedgerLine(line)
		if !ok {
			continue
		}
		previous, inside := result.windowOfDay(row.Date)
		if !inside {
			continue
		}
		reason, refused := row.Refusal()
		if !row.Landed() && !refused {
			continue
		}
		e := reportEntry{previous: previous, id: row.StoryID, rig: ReportUnknown, host: ReportUnknown, model: ReportUnknown,
			landed: row.Landed(), refused: refused, reason: reason, attempts: 1}
		if row.Model != "" {
			e.model = row.Model
		}
		if d, known := byID[row.StoryID]; known {
			if IsTesterStory(d) {
				continue
			}
			path := d.Merged()
			e.rig, e.host = orElse(path.Rig, e.rig), orElse(path.Host, e.host)
			e.model = orElse(string(path.Model), e.model)
			e.cause = d.Cause
			e.exhausted = d.Exhausted
			if d.Attempts > 1 {
				e.attempts = d.Attempts
			}
			if e.landed && !landedSeen[row.StoryID] {
				landedSeen[row.StoryID] = true
				landedIDs = append(landedIDs, row.StoryID)
			}
		} else if b, found := benchmarks[row.StoryID]; found {
			e.rig, e.host = orElse(b.Rig, e.rig), orElse(b.Host, e.host)
		}
		entries = append(entries, e)
	}

	findings := map[string]int{}
	if len(landedIDs) > 0 {
		comments, err := r.Tracker.StoriesComments(ctx, landedIDs)
		if err != nil {
			return result, fmt.Errorf("reporting quality: the comments on the landed stories: %w", err)
		}
		for _, id := range landedIDs {
			if found, ok := newestFindings(comments[id], time.Time{}); ok {
				findings[id] = found.Count()
			}
		}
	}
	// A story's findings, wall time and fuel count once per window, on its first
	// landing row.
	counted := map[string]bool{}
	for i, e := range entries {
		key := fmt.Sprintf("%t/%s", e.previous, e.id)
		if e.landed && e.id != "" && !counted[key] {
			counted[key] = true
			entries[i].findings = findings[e.id]
			entries[i].tokens = tokensOf[e.id]
			entries[i].costUSD = r.costOf(ctx, e.id, e.attempts)
			if b, ok := landedIn[e.id]; ok {
				entries[i].measured, entries[i].actualS, entries[i].ratio = true, b.ActualS, b.Ratio()
			}
		}
	}

	for _, d := range stories {
		if d.IsEpic || d.Created.IsZero() || !domain.IsBugStory(d.Type, d.Story.Title) {
			continue
		}
		previous, inside := result.windowOfTime(d.Created)
		if !inside {
			continue
		}
		path := d.Merged()
		entries = append(entries, reportEntry{previous: previous, bug: true,
			rig: orElse(path.Rig, ReportUnknown), host: orElse(path.Host, ReportUnknown), model: orElse(string(path.Model), ReportUnknown)})
	}

	gates := gateTrends(recent, result.Window.Since, r.Rig, r.Host)
	result.Rigs = r.blocks(entries, gates, func(e reportEntry) string { return e.rig }, func(g GateTrend) string { return g.Rig })
	result.Models = r.blocks(entries, nil, func(e reportEntry) string { return e.model }, nil)
	result.Hosts = r.blocks(entries, gates, func(e reportEntry) string { return e.host }, func(g GateTrend) string { return g.Host })
	if whole := r.blocks(entries, nil, func(reportEntry) string { return "factory" }, nil); len(whole) == 1 {
		result.Whole = &whole[0]
	}
	result.CheapWins = CheapWins(WinInputs{Rigs: result.Rigs, Gates: gates, Kinds: Calibrate(recent, BenchmarkSettings{}), Heavy: r.heavyStories(entries)})

	if r.Out != nil {
		if r.JSON {
			enc := json.NewEncoder(r.Out)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return result, err
			}
		} else {
			fmt.Fprint(r.Out, result.String())
		}
	}
	return result, nil
}

// heavyStories are the stories of the window that used up their attempts or
// needed as many as a story may have, each once, narrowed like the blocks.
func (r FactoryReport) heavyStories(entries []reportEntry) []HeavyStory {
	var heavy []HeavyStory
	seen := map[string]bool{}
	for _, e := range entries {
		if e.previous || e.bug || e.id == "" || seen[e.id] || (r.Rig != "" && e.rig != r.Rig) || (r.Host != "" && e.host != r.Host) {
			continue
		}
		if e.exhausted || e.attempts >= DefaultMaxAttempts {
			seen[e.id] = true
			heavy = append(heavy, HeavyStory{ID: e.id, Attempts: max(e.attempts, DefaultMaxAttempts)})
		}
	}
	return heavy
}

// costOf is what a story's result files report it cost, its attempts summed:
// 0 when no file says. A file that is missing or not a result is not an error,
// only a figure the report has not got.
func (r FactoryReport) costOf(ctx context.Context, id string, attempts int) float64 {
	var cost float64
	for attempt := 1; attempt <= max(attempts, 1); attempt++ {
		printed, err := r.Files.ReadRunFile(ctx, id, ResultFileNameForAttempt(attempt))
		if err != nil {
			continue
		}
		if result, err := ReadSessionResult(printed); err == nil {
			cost += result.CostUSD
		}
	}
	return cost
}

// windowOfDay says which window a ledger day falls in: previous or not, and
// whether it falls in either.
func (r FactoryReportResult) windowOfDay(day string) (previous, inside bool) {
	at, err := time.Parse(LedgerDate, day)
	if err != nil {
		return false, false
	}
	return r.windowOfTime(at)
}

// windowOfTime says which window a moment falls in; a ledger day counts from
// the start of its day.
func (r FactoryReportResult) windowOfTime(at time.Time) (previous, inside bool) {
	switch {
	case !at.Before(r.Window.Since) && !at.After(r.Window.Until):
		return false, true
	case !at.Before(r.PreviousWindow.Since) && at.Before(r.Window.Since):
		return true, true
	}
	return false, false
}

// orElse is text, or fallback when it is empty.
func orElse(text, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return fallback
	}
	return strings.TrimSpace(text)
}

// blocks groups what the windows saw by one of rig, model or host, narrowed by
// the report's Rig and Host, and sums each group: the blocks with something in
// this window, by name, with unknown last. gateKey, when set, names the block a
// gate line belongs to.
func (r FactoryReport) blocks(entries []reportEntry, gates []GateTrend, keyOf func(reportEntry) string, gateKey func(GateTrend) string) []ReportBlock {
	now, before := map[string][]reportEntry{}, map[string][]reportEntry{}
	for _, e := range entries {
		if (r.Rig != "" && e.rig != r.Rig) || (r.Host != "" && e.host != r.Host) {
			continue
		}
		key := keyOf(e)
		if e.previous {
			before[key] = append(before[key], e)
		} else {
			now[key] = append(now[key], e)
		}
	}
	var blocks []ReportBlock
	for name, mine := range now {
		b := ReportBlock{Name: name,
			Quality: sumQuality(mine), Previous: sumQuality(before[name]),
			Speed: sumSpeed(mine), SpeedPrevious: sumSpeed(before[name]),
			Fuel: sumFuel(mine), FuelPrevious: sumFuel(before[name])}
		b.Arrows = qualityArrows(b.Quality, b.Previous)
		for measure, t := range speedArrows(b.Speed, b.SpeedPrevious) {
			b.Arrows[measure] = t
		}
		for measure, t := range fuelArrows(b.Fuel, b.FuelPrevious) {
			b.Arrows[measure] = t
		}
		for _, g := range gates {
			if gateKey != nil && gateKey(g) == name {
				b.Gates = append(b.Gates, g)
			}
		}
		blocks = append(blocks, b)
	}
	sort.Slice(blocks, func(i, j int) bool {
		a, b := blocks[i].Name, blocks[j].Name
		if (a == ReportUnknown) != (b == ReportUnknown) {
			return b == ReportUnknown
		}
		return a < b
	})
	return blocks
}

// sumQuality sums the entries of one block and window.
func sumQuality(entries []reportEntry) Quality {
	q := Quality{Reasons: []ReasonCount{}, Causes: map[string]int{CauseUnset: 0}}
	for _, class := range CauseClasses {
		q.Causes[class] = 0
	}
	reasons := map[string]int{}
	uncaused := map[string]bool{}
	for _, e := range entries {
		switch {
		case e.bug:
			q.BugsFiled++
		case e.landed:
			q.Landed++
			q.Attempts += e.attempts
			q.Findings += e.findings
		case e.refused:
			q.Refused++
			reasons[e.reason]++
			if class, ok := validCauseClass(e.cause); ok {
				q.Causes[class]++
			} else {
				q.Causes[CauseUnset]++
				if !uncaused[e.id] {
					uncaused[e.id] = true
					q.Uncaused = append(q.Uncaused, e.id)
				}
			}
		}
	}
	sort.Strings(q.Uncaused)
	for reason, n := range reasons {
		q.Reasons = append(q.Reasons, ReasonCount{Reason: reason, Count: n})
	}
	sort.Slice(q.Reasons, func(i, j int) bool {
		if q.Reasons[i].Count != q.Reasons[j].Count {
			return q.Reasons[i].Count > q.Reasons[j].Count
		}
		return q.Reasons[i].Reason < q.Reasons[j].Reason
	})
	if rows := q.Landed + q.Refused; rows > 0 {
		q.RefusalRate = math.Round(float64(q.Refused)*1000/float64(rows)) / 10
	}
	if q.Landed > 0 {
		q.AttemptsPerLanding = twoPlaces(float64(q.Attempts) / float64(q.Landed))
		q.BugsPerLanding = twoPlaces(float64(q.BugsFiled) / float64(q.Landed))
		q.FindingsPerLanding = twoPlaces(float64(q.Findings) / float64(q.Landed))
	}
	return q
}

func twoPlaces(x float64) float64 { return math.Round(x*100) / 100 }

// String is the report as mw report prints it: the window, then QUALITY, SPEED
// and FUEL in that order, each with a block per rig, per model and per host,
// and CHEAP WINS at the end, none of it wider than ReportWidth columns.
func (r FactoryReportResult) String() string {
	var b strings.Builder
	r.writeParts(&b)
	writeCheapWins(&b, r.CheapWins)
	return b.String()
}

// writeParts prints the window and the QUALITY, SPEED and FUEL parts.
func (r FactoryReportResult) writeParts(b *strings.Builder) {
	days := int(reportDay(r.Window.Until).Sub(r.Window.Since).Hours() / 24)
	fmt.Fprintf(b, "QUALITY %s to %s (%d days)\n",
		r.Window.Since.UTC().Format(LedgerDate), r.Window.Until.UTC().Format(LedgerDate), days)
	if len(r.Rigs) == 0 {
		b.WriteString("\nnothing landed, was refused or was filed in it\n")
		return
	}
	for i, part := range []struct {
		heading string
		write   func(ReportBlock, *strings.Builder)
	}{{"", ReportBlock.writeQuality}, {"SPEED", ReportBlock.writeSpeed}, {"FUEL", ReportBlock.writeFuel}} {
		if i > 0 {
			fmt.Fprintf(b, "\n%s\n", part.heading)
		}
		for _, section := range []struct {
			kind   string
			blocks []ReportBlock
		}{{"rig", r.Rigs}, {"model", r.Models}, {"host", r.Hosts}} {
			fmt.Fprintf(b, "\nby %s\n", section.kind)
			for j, block := range section.blocks {
				if j > 0 {
					b.WriteString("\n")
				}
				part.write(block, b)
			}
		}
	}
	return
}

// arrow is the trend of a measure, as it follows the number in a line.
func (k ReportBlock) arrow(measure string) string { return k.Arrows[measure].Text() }

// writeQuality prints one block's quality.
func (k ReportBlock) writeQuality(b *strings.Builder) {
	q := k.Quality
	tail := fmt.Sprintf(": landed %d%s, refused %d (%.0f%%%s)", q.Landed, k.arrow("landed"), q.Refused, q.RefusalRate, k.arrow("refusal_rate"))
	fmt.Fprintf(b, "%s%s\n", fitLine(k.Name, ReportWidth-utf8.RuneCountInString(tail)), tail)
	for _, reason := range q.Reasons {
		fmt.Fprintf(b, "  %s\n", fitLine(fmt.Sprintf("%s %d", reason.Reason, reason.Count), ReportWidth-2))
	}
	if q.Landed > 0 {
		fmt.Fprintf(b, "  attempts per landing %.2f%s\n", q.AttemptsPerLanding, k.arrow("attempts_per_landing"))
	}
	if q.Refused > 0 {
		var causes []string
		for _, class := range append(append([]string{}, CauseClasses...), CauseUnset) {
			if n := q.Causes[class]; n > 0 || class == CauseUnset {
				causes = append(causes, fmt.Sprintf("%s %d", class, n))
			}
		}
		fmt.Fprintf(b, "  causes %s\n", strings.Join(causes, " · "))
	}
	fmt.Fprintf(b, "  bugs filed %d%s\n", q.BugsFiled, perLanding(q, q.BugsPerLanding, k.arrow("bugs_per_landing")))
	if q.Landed > 0 {
		fmt.Fprintf(b, "  Tester findings %d%s\n", q.Findings, perLanding(q, q.FindingsPerLanding, k.arrow("findings_per_landing")))
	}
}

// writeSpeed prints one block's speed: the median and p90 of actual over par,
// the wall time from dispatch to landing, and the gate of each host or rig.
func (k ReportBlock) writeSpeed(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n", fitLine(k.Name, ReportWidth))
	s := k.Speed
	if s.WithPar == 0 {
		b.WriteString("  no par yet\n")
	} else {
		fmt.Fprintf(b, "  par x%.1f median, x%.1f p90\n", s.ParMedian, s.ParP90)
		writeLine(b, vsBefore("  ", []string{"median", "p90"}, k.Arrows["par_median"], k.Arrows["par_p90"]))
	}
	if s.Measured > 0 {
		fmt.Fprintf(b, "  dispatch to landing %s median%s\n", gateClock(seconds(s.DispatchToLandingS)), k.arrow("dispatch_to_landing"))
	}
	for _, g := range k.Gates {
		// A rig's gates are told by host, and a host's by rig.
		name := g.Host
		if k.Name == g.Host {
			name = g.Rig
		}
		fmt.Fprintf(b, "  %s\n", fitLine(fmt.Sprintf("%s: gate usual %s, latest %s", name, gateClock(seconds(g.UsualS)), gateClock(seconds(g.LatestS))), ReportWidth-2))
		writeLine(b, vsBefore("    ", []string{"usual", "latest"}, g.Arrows["usual"], g.Arrows["latest"]))
	}
}

// writeFuel prints one block's fuel per landing.
func (k ReportBlock) writeFuel(b *strings.Builder) {
	fmt.Fprintf(b, "%s\n", fitLine(k.Name, ReportWidth))
	f := k.Fuel
	if f.WithTokens == 0 && f.WithCost == 0 {
		b.WriteString("  no fuel figures yet\n")
		return
	}
	if f.WithTokens > 0 {
		fmt.Fprintf(b, "  tokens/landing %s%s\n", Thousands(int(f.TokensPerLanding)), k.arrow("tokens_per_landing"))
	}
	if f.WithCost > 0 {
		fmt.Fprintf(b, "  $/landing %.2f%s\n", f.USDPerLanding, k.arrow("usd_per_landing"))
	}
}

// writeLine prints a line, if there is one.
func writeLine(b *strings.Builder, line string) {
	if line != "" {
		b.WriteString(line + "\n")
	}
}

// perLanding is " (0.25/landing)", or " (no landings)" when there are none; the
// arrow follows the rate.
func perLanding(q Quality, rate float64, arrow string) string {
	if q.Landed == 0 {
		return " (no landings)"
	}
	return fmt.Sprintf(" (%.2f/landing%s)", rate, arrow)
}

// fitLine cuts text to width columns, ending in an ellipsis when it had to.
func fitLine(text string, width int) string {
	if width < 1 || utf8.RuneCountInString(text) <= width {
		return text
	}
	runes := []rune(text)
	return string(runes[:width-1]) + "…"
}

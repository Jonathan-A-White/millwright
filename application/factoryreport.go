package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path"
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
	// Rules, when set, is where a rig's union_merge_files, version_files and
	// changelog_files are read from, to mark a conflicted file the rig merges
	// by a rule; left nil no file is marked.
	Rules EpicRules
	// Benchmarks, when set, names the rig and host of a ledger row whose story
	// is gone, from the close-out that measured it, and is where speed is read.
	Benchmarks BenchmarkBook
	// Seat is whose ledger is read.
	Seat string
	// Since is where the window starts; zero is DefaultReportDays before now.
	Since time.Time
	// Rig and Host, when set, narrow every block to that rig or host.
	Rig, Host string
	// Around, when set, is a story id or a commit of the factory's rig, and
	// the report is the days before that landing beside the days after it.
	// Commits reads such a commit in the checkout at FactoryDir.
	Around     string
	Commits    FactoryCommits
	FactoryDir string
	// Investments, when set, is the investments ledger the INVESTMENTS part
	// reads; left nil the report has no such part. A landing has none either.
	Investments InvestmentBook
	// InvestShare is the share of landings meant to be investments, a fraction
	// ([factory] invest_share); 0 is DefaultInvestShare.
	InvestShare float64
	// InvestShareMin and InvestShareMax are the bounds on that share, fractions
	// ([factory] invest_share_min and invest_share_max); 0 is the domain's default.
	InvestShareMin, InvestShareMax float64
	// JSON prints the result as one object instead of text.
	JSON bool
	// Notes, Mailbox and Snapshots, when all set, are what mw report send
	// writes with: a WORSE factory change is told to the Mayor once, and a day's
	// key metrics are kept. A plain mw report leaves them nil and writes nothing.
	Notes     ReportNotes
	Mailbox   Mailbox
	Snapshots MetricSnapshots
	// History, when set, is read for the daily snapshots a change on his time
	// is judged from (Snapshots is read for them too); it writes nothing.
	History MetricSnapshots
	// Review, when set, counts his review queue and the demos waiting for the
	// whole factory (a report narrowed to a rig or host, or to a landing, leaves
	// them out); left nil the report has no HIS TIME part.
	Review ReviewCounter
	Now    func() time.Time
	Out    io.Writer
}

// ReviewCounter counts what waits for him, as mw status DAMPERS does. The real
// adapter is DamperSurvey; DamperCounts reads the work and judges it.
type ReviewCounter interface {
	DamperCounts(ctx context.Context) (DampersReport, error)
}

// ReviewTime is his time at the moment of the report: ReviewQueue is the open
// cards, open demos and unrun hands steps, against the limit the DAMPERS alarm
// trips at, and DemosWaiting the demos waiting DemoDays or more.
type ReviewTime struct {
	Queue        int `json:"review_queue"`
	QueueLimit   int `json:"review_queue_limit"`
	DemosWaiting int `json:"demos_waiting"`
	DemoDays     int `json:"demo_days"`
}

// FactoryCommits reads a commit of a rig's checkout as the checkout has it,
// with no fetch: its full name, its subject and when it was committed. The
// adapter is the rig's git (infrastructure/rig).
type FactoryCommits interface {
	CommitOf(ctx context.Context, rigDir, rev string) (RigCommit, error)
}

// ReportNotes is the part of the tracker's key-value store that remembers which
// WORSE changes the Mayor has been told of.
type ReportNotes interface {
	Note(ctx context.Context, key string) (string, error)
	SetNote(ctx context.Context, key, value string) error
}

// MetricSnapshots is where mw keeps the factory's key metrics, one dated
// snapshot a day, in a file that is only ever appended to: Snapshots reads them
// oldest first, none and no error when there are none; Append adds one. The real
// adapter is infrastructure/vault; the fake is apptest.FakeMetricSnapshots.
type MetricSnapshots interface {
	Snapshots(ctx context.Context) ([]MetricSnapshot, error)
	Append(ctx context.Context, snapshot MetricSnapshot) error
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

// ConflictFile is a file the merge-conflict refusals of a block conflicted in,
// and how many refusals it was in. Mark says the rig merges that file by a rule
// that should have kept it from conflicting (ConflictMarkUnion, ...Version,
// ...Changelog), so a hit there is a machinery bug; empty for any other file.
type ConflictFile struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
	Mark  string `json:"mark,omitempty"`
}

// The marks of a ConflictFile, as the cheap win prints them in brackets.
const (
	ConflictMarkUnion     = "union"
	ConflictMarkVersion   = "version"
	ConflictMarkChangelog = "changelog"
)

// Quality is the measures of one window of one block.
type Quality struct {
	Landed  int `json:"landed"`
	Refused int `json:"refused"`
	// RefusalRate is the share of the ledger's rows that were refusals, in percent.
	RefusalRate float64       `json:"refusal_rate"`
	Reasons     []ReasonCount `json:"refused_by_reason"`
	// ConflictFiles are the files the merge-conflict refusals conflicted in,
	// commonest first, from the stories' conflict_files (ConflictFilesField);
	// none when no such refusal carries any.
	ConflictFiles []ConflictFile `json:"conflict_files,omitempty"`
	// Attempts are the attempts the landings took in all, a landing counted at
	// least one; AttemptsPerLanding is their mean.
	Attempts           int            `json:"attempts"`
	AttemptsPerLanding float64        `json:"attempts_per_landing"`
	Causes             map[string]int `json:"causes"`
	// Uncaused are the ids of the refused stories counted under CauseUnset, once
	// each and sorted; cheap win R1 names them. They are not in the JSON.
	Uncaused []string `json:"-"`
	// Invested are the landings whose story carries LabelInvestment.
	Invested       int     `json:"invested"`
	BugsFiled      int     `json:"bugs_filed"`
	BugsPerLanding float64 `json:"bugs_per_landing"`
	// EscapedBugs are the bugs filed in the window that a landing let through
	// (escapedBugs), counted under the rig, host and model of that landing;
	// EscapedTester are those the Tester found, the factory catching a bug on
	// purpose, so EscapedBugs less EscapedTester is the bugs that reached him.
	EscapedBugs        int     `json:"escaped_bugs"`
	EscapedTester      int     `json:"escaped_bugs_tester"`
	Findings           int     `json:"tester_findings"`
	FindingsPerLanding float64 `json:"findings_per_landing"`
}

// EscapedOthers are the escaped bugs the Tester did not find.
func (q Quality) EscapedOthers() int { return q.EscapedBugs - q.EscapedTester }

// escapedText is "escaped bugs N (Tester M)": all finders, then the Tester's.
func (q Quality) escapedText() string {
	return fmt.Sprintf("escaped bugs %d (Tester %d)", q.EscapedBugs, q.EscapedTester)
}

// Empty says nothing landed, was refused, was filed as a bug or escaped.
func (q Quality) Empty() bool {
	return q.Landed == 0 && q.Refused == 0 && q.BugsFiled == 0 && q.EscapedBugs == 0
}

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
	// Changes are the millwright landings of the last ChangeDays days, each
	// with the metric it expected to move and how it moved; oldest first.
	Changes []FactoryChange `json:"factory_changes"`
	// Whole is the whole factory as one block, what Summary reads; nil when
	// nothing landed, was refused or was filed. It is not in the JSON.
	Whole *ReportBlock `json:"-"`
	// Landing, for mw report --since <story|commit>, is that landing with the
	// days before it beside the days after, the PreviousWindow and the Window;
	// nil for a report of a window.
	Landing *ReportLanding `json:"landing,omitempty"`
	// Investments are the outcomes of the factory's investments the window
	// recorded and how many are due for review; nil when the report was not
	// given the ledger.
	Investments *InvestmentsSection `json:"investments,omitempty"`
	// InvestShare is the share of the window's landings that were investments,
	// against the window before and the target, the line that ends QUALITY;
	// nil for a landing.
	InvestShare *InvestShare `json:"invest_share,omitempty"`
	// ModelCompare is MODELS, the models side by side after FUEL; nil for a
	// landing.
	ModelCompare *ModelCompare `json:"model_compare,omitempty"`
	// Review is his time today, whole-factory; nil when the report counted none.
	Review *ReviewTime `json:"his_time,omitempty"`
	// EpicsMoved is how many epics had a story closed or claimed in the window;
	// the parent's note says it in words. It is not in the JSON.
	EpicsMoved int `json:"-"`
}

// reportEntry is one thing the window saw: a landing, a refusal, a bug filed
// or a bug escaped.
type reportEntry struct {
	id       string
	previous bool
	// at is the start of the day the entry belongs to; inWindow says it falls
	// in the window asked for or the one before it, and is false for an entry
	// read only to measure a factory change.
	at               time.Time
	inWindow         bool
	rig, host, model string
	landed, refused  bool
	// tester says a Tester's session: its "tested" row stands for a landing and
	// its "not landed" row for a refusal, in MODELS alone (modelCompare); every
	// other section leaves a Tester's rows out.
	tester     bool
	investment bool
	bug        bool
	// escaped is a bug a landing let through, under that landing's rig, host
	// and model, its id the bug's.
	escaped bool
	// byTester says an escaped bug was the Tester's finding.
	byTester bool
	reason   string
	// conflictFiles are the files a merge-conflict refusal's story last
	// conflicted in.
	conflictFiles []string
	attempts      int
	// own is how many of attempts count toward max_attempts (OwnAttempts):
	// what judges a story, where attempts numbers run files and measures cost.
	own       int
	cause     string
	exhausted bool
	findings  int
	// measured says a benchmark gave the landing a wall time, actualS, and
	// ratio is that over its par, 0 when it had none.
	measured bool
	actualS  float64
	ratio    float64
	// tokens and costUSD are the fuel of every attempt of a landed story.
	tokens  int
	costUSD float64
	// ran is the model the ledger row names, the one the session ran on, else
	// model; bugStory says the story is a bug. MODELS reads both.
	ran      string
	bugStory bool
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
	ledger, err := r.Files.ReadLedger(ctx, r.Seat)
	if err != nil {
		return FactoryReportResult{}, fmt.Errorf("reporting quality: the %s seat's ledger: %w", r.Seat, err)
	}
	var result FactoryReportResult
	if strings.TrimSpace(r.Around) != "" {
		landing, err := r.landingOf(ctx, ledger)
		if err != nil {
			return result, fmt.Errorf("reporting on a landing: %w", err)
		}
		result.PreviousWindow, result.Window = landing.windows(now)
		result.Landing = &landing
	} else {
		since := r.Since
		if since.IsZero() {
			since = now.AddDate(0, 0, -DefaultReportDays)
		}
		since = reportDay(since)
		if !since.Before(now) {
			return result, fmt.Errorf("reporting quality: --since %s is not before now", since.Format(LedgerDate))
		}
		// The window before is as many whole days long as the one asked for.
		days := int(reportDay(now).Sub(since).Hours() / 24)
		if days < 1 {
			days = 1
		}
		result.Window = ReportWindow{Since: since, Until: now}
		result.PreviousWindow = ReportWindow{Since: since.AddDate(0, 0, -days), Until: since}
	}

	// Entries are read back as far as a factory change needs them, a landing
	// of the last ChangeDays days and the ChangeVerdictDays before it, and as
	// far as a bug of the window before may have escaped from.
	earliest := result.PreviousWindow.Since.AddDate(0, 0, -EscapeDays)
	if wide := reportDay(now).AddDate(0, 0, -(ChangeDays + ChangeVerdictDays)); wide.Before(earliest) {
		earliest = wide
	}
	stories, err := r.Tracker.StoriesSince(ctx, earliest)
	if err != nil {
		return result, fmt.Errorf("reporting quality: %w", err)
	}
	byID := make(map[string]StoryDetail, len(stories))
	for _, d := range stories {
		byID[d.Story.ID] = d
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

	var all, testers []reportEntry
	var landedIDs []string
	landedSeen := map[string]bool{}
	for _, line := range ledger {
		row, ok := ParseLedgerLine(line)
		if !ok {
			continue
		}
		previous, inside := result.windowOfDay(row.Date)
		day, dayErr := time.Parse(LedgerDate, row.Date)
		if !inside && (dayErr != nil || day.Before(earliest) || day.After(now)) {
			continue
		}
		reason, refused := row.Refusal()
		tested := row.Tested()
		if !row.Landed() && !refused && !tested {
			continue
		}
		e := reportEntry{previous: previous, inWindow: inside, at: day, id: row.StoryID, rig: ReportUnknown, host: ReportUnknown, model: ReportUnknown,
			landed: row.Landed() || tested, refused: refused, reason: reason, attempts: 1, own: 1, tester: tested}
		if row.Model != "" {
			e.model = row.Model
		}
		if d, known := byID[row.StoryID]; known {
			e.tester = e.tester || IsTesterStory(d)
			path := d.Merged()
			e.rig, e.host = orElse(path.Rig, e.rig), orElse(path.Host, e.host)
			e.model = orElse(string(path.Model), e.model)
			e.cause = d.Cause
			if refused && reason == string(ReasonMergeConflict) {
				e.conflictFiles = d.ConflictFiles
			}
			e.exhausted = d.Exhausted
			e.investment = hasLabel(d.Labels, LabelInvestment)
			e.bugStory = domain.IsBugStory(d.Type, d.Story.Title)
			if d.Attempts > 1 {
				e.attempts = d.Attempts
				e.own, _ = OwnAttempts(d)
			}
			if e.landed && !e.tester && inside && !landedSeen[row.StoryID] {
				landedSeen[row.StoryID] = true
				landedIDs = append(landedIDs, row.StoryID)
			}
		} else if b, found := benchmarks[row.StoryID]; found {
			e.rig, e.host = orElse(b.Rig, e.rig), orElse(b.Host, e.host)
		}
		e.ran = orElse(row.Model, e.model)
		if e.tester {
			// A Tester's session is no landing of QUALITY, SPEED or FUEL: MODELS
			// alone counts it, with the fuel its own row gives.
			if inside {
				e.tokens, e.costUSD = row.Tokens, row.CostUSD
				testers = append(testers, e)
			}
			continue
		}
		all = append(all, e)
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
	for i, e := range all {
		key := fmt.Sprintf("%t/%t/%s", e.previous, e.inWindow, e.id)
		if e.landed && e.id != "" && !counted[key] {
			counted[key] = true
			all[i].findings = findings[e.id]
			all[i].tokens = tokensOf[e.id]
			all[i].costUSD = r.costOf(ctx, e.id, e.attempts)
			if b, ok := landedIn[e.id]; ok {
				all[i].measured, all[i].actualS, all[i].ratio = true, b.ActualS, b.Ratio()
			}
		}
	}

	for _, d := range stories {
		if d.IsEpic || d.Created.IsZero() || !domain.IsBugStory(d.Type, d.Story.Title) {
			continue
		}
		previous, inside := result.windowOfTime(d.Created)
		if !inside && d.Created.Before(earliest) {
			continue
		}
		path := d.Merged()
		all = append(all, reportEntry{previous: previous, inWindow: inside, at: reportDay(d.Created), bug: true,
			rig: orElse(path.Rig, ReportUnknown), host: orElse(path.Host, ReportUnknown), model: orElse(string(path.Model), ReportUnknown)})
	}

	escapes := escapedBugs(stories, firstLandings(all))
	all = append(all, result.escapedEntries(escapes)...)

	var entries []reportEntry
	for _, e := range all {
		if e.inWindow {
			entries = append(entries, e)
		}
	}
	var kept []MetricSnapshot
	if history := r.snapshotHistory(); history != nil {
		if kept, err = history.Snapshots(ctx); err != nil {
			return result, fmt.Errorf("reporting quality: the key metrics kept so far: %w", err)
		}
	}
	if r.Review != nil && r.Rig == "" && r.Host == "" && result.Landing == nil {
		counted, err := r.Review.DamperCounts(ctx)
		if err != nil {
			return result, fmt.Errorf("reporting quality: counting his review queue: %w", err)
		}
		limits := counted.Limits.orDefaults()
		result.Review = &ReviewTime{Queue: counted.Counts.ReviewQueue, QueueLimit: limits.ReviewQueue,
			DemosWaiting: counted.Counts.DemosWaiting, DemoDays: limits.DemoWaitingDays}
	}
	result.Changes = factoryChanges(all, kept, byID, escapedBy(escapes), now)
	result.EpicsMoved = result.epicsMoved(stories)
	if result.Landing != nil {
		if d, known := byID[result.Landing.Story]; known && strings.TrimSpace(d.Story.Title) != "" {
			result.Landing.Title = d.Story.Title
		}
		result.Landing.measure(entries, r.Rig, r.Host)
	}

	gates := gateTrends(recent, result.Window.Since, r.Rig, r.Host)
	result.Rigs = r.blocks(entries, gates, func(e reportEntry) string { return e.rig }, func(g GateTrend) string { return g.Rig })
	result.Models = r.blocks(entries, nil, func(e reportEntry) string { return e.model }, nil)
	r.markConflictFiles(ctx, result.Rigs)
	result.Hosts = r.blocks(entries, gates, func(e reportEntry) string { return e.host }, func(g GateTrend) string { return g.Host })
	if whole := r.blocks(entries, nil, func(reportEntry) string { return "factory" }, nil); len(whole) == 1 {
		result.Whole = &whole[0]
	}
	if result.Landing == nil {
		share := readInvestShare(result.Whole, result.Rigs, starvedP1Rig(stories, now, r.Rig, r.Host), r.InvestShare, r.InvestShareMin, r.InvestShareMax)
		result.InvestShare = &share
		result.ModelCompare = r.modelCompare(append(entries[:len(entries):len(entries)], testers...), escapedOfLanding(escapes))
	}
	if r.Investments != nil && result.Landing == nil {
		records, err := r.Investments.Investments(ctx)
		if err != nil {
			return result, fmt.Errorf("reporting investments: %w", err)
		}
		section := ReadInvestmentsSection(GroupInvestments(records), result.Window, now)
		result.Investments = &section
	}
	result.CheapWins = CheapWins(WinInputs{Rigs: result.Rigs, Gates: gates, Kinds: Calibrate(recent, BenchmarkSettings{}), Heavy: r.heavyStories(entries),
		Unfixed: unfixed(worse(result.Changes), stories), Share: result.InvestShare})

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
	return result, r.record(ctx, result, all, now)
}

// snapshotHistory is where the daily snapshots are read from, if anywhere.
func (r FactoryReport) snapshotHistory() MetricSnapshots {
	if r.Snapshots != nil {
		return r.Snapshots
	}
	return r.History
}

// heavyStories are the stories of the window that used up their attempts or
// needed as many of their own as a story may have, each once, narrowed like the
// blocks. An attempt mw cause named test, tooling or process is not the story's
// own, so it does not make a story heavy (mw-gq6.486).
func (r FactoryReport) heavyStories(entries []reportEntry) []HeavyStory {
	var heavy []HeavyStory
	seen := map[string]bool{}
	for _, e := range entries {
		if e.previous || e.bug || e.escaped || e.id == "" || seen[e.id] || (r.Rig != "" && e.rig != r.Rig) || (r.Host != "" && e.host != r.Host) {
			continue
		}
		if e.exhausted || e.own >= DefaultMaxAttempts {
			seen[e.id] = true
			heavy = append(heavy, HeavyStory{ID: e.id, Attempts: max(e.own, DefaultMaxAttempts)})
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
// the start of its day, and the window's last day counts whole.
func (r FactoryReportResult) windowOfTime(at time.Time) (previous, inside bool) {
	switch {
	case !at.Before(r.Window.Since) && !reportDay(at).After(reportDay(r.Window.Until)):
		return false, true
	case !at.Before(r.PreviousWindow.Since) && at.Before(r.PreviousWindow.Until):
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

// markConflictFiles marks, in each rig block, the conflicted files the rig
// merges by a rule: a file in union_merge_files, version_files or
// changelog_files should never conflict, so its being here is a bug in the
// machinery (mw-gq6.559). A rig whose rules cannot be read is left unmarked.
func (r FactoryReport) markConflictFiles(ctx context.Context, blocks []ReportBlock) {
	if r.Rules == nil {
		return
	}
	for i := range blocks {
		files := blocks[i].Quality.ConflictFiles
		if len(files) == 0 {
			continue
		}
		rules, err := r.Rules.EpicRequirements(ctx, blocks[i].Name)
		if err != nil {
			continue
		}
		for j := range files {
			switch {
			case conflictListed(rules.ChangelogFiles, files[j].Path):
				files[j].Mark = ConflictMarkChangelog
			case conflictListed(rules.VersionFiles, files[j].Path):
				files[j].Mark = ConflictMarkVersion
			case conflictListed(rules.UnionMergeFiles, files[j].Path):
				files[j].Mark = ConflictMarkUnion
			}
		}
	}
}

// conflictListed says file is one of names: named in full or matched by a
// path.Match glob, as the rig's file lists them.
func conflictListed(names []string, file string) bool {
	for _, name := range names {
		if name == file {
			return true
		}
		if ok, err := path.Match(name, file); err == nil && ok {
			return true
		}
	}
	return false
}

// sumQuality sums the entries of one block and window.
func sumQuality(entries []reportEntry) Quality {
	q := Quality{Reasons: []ReasonCount{}, Causes: map[string]int{CauseUnset: 0}}
	for _, class := range CauseClasses {
		q.Causes[class] = 0
	}
	reasons := map[string]int{}
	uncaused := map[string]bool{}
	conflicts := map[string]int{}
	for _, e := range entries {
		switch {
		case e.escaped:
			q.EscapedBugs++
			if e.byTester {
				q.EscapedTester++
			}
		case e.bug:
			q.BugsFiled++
		case e.landed:
			q.Landed++
			if e.investment {
				q.Invested++
			}
			q.Attempts += e.attempts
			q.Findings += e.findings
		case e.refused:
			q.Refused++
			reasons[e.reason]++
			for _, file := range e.conflictFiles {
				conflicts[file]++
			}
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
	for file, n := range conflicts {
		q.ConflictFiles = append(q.ConflictFiles, ConflictFile{Path: file, Count: n})
	}
	sort.Slice(q.ConflictFiles, func(i, j int) bool {
		if q.ConflictFiles[i].Count != q.ConflictFiles[j].Count {
			return q.ConflictFiles[i].Count > q.ConflictFiles[j].Count
		}
		return q.ConflictFiles[i].Path < q.ConflictFiles[j].Path
	})
	q.RefusalRate = percentOf(q.Refused, q.Landed+q.Refused)
	landed := float64(q.Landed)
	q.AttemptsPerLanding = twoPlaces(fraction(float64(q.Attempts), landed))
	q.BugsPerLanding = twoPlaces(fraction(float64(q.BugsFiled), landed))
	q.FindingsPerLanding = twoPlaces(fraction(float64(q.Findings), landed))
	return q
}

func twoPlaces(x float64) float64 { return math.Round(x*100) / 100 }

// fraction is part over whole, 0 when whole is 0: every ratio mw report
// prints is divided here.
func fraction(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return part / whole
}

// percentOf is part of whole in percent, to one place; 0 when whole is 0.
func percentOf(part, whole int) float64 {
	return math.Round(fraction(float64(part), float64(whole))*1000) / 10
}

// String is the report as mw report prints it: the window, then QUALITY, SPEED
// and FUEL in that order, each with a block per rig, per model and per host,
// then MODELS, and CHEAP WINS at the end, none of it wider than ReportWidth columns.
func (r FactoryReportResult) String() string {
	if r.Landing != nil {
		return r.Landing.String()
	}
	var b strings.Builder
	r.writeParts(&b)
	r.Review.write(&b)
	writeModels(&b, r.ModelCompare)
	writeChanges(&b, r.Changes)
	writeInvestments(&b, r.Investments)
	writeCheapWins(&b, r.CheapWins)
	return b.String()
}

// write prints HIS TIME, the counts of what waits for him; nothing when there
// are none.
func (t *ReviewTime) write(b *strings.Builder) {
	if t == nil {
		return
	}
	b.WriteString("\nHIS TIME\n")
	fmt.Fprintf(b, "  review queue %d (limit %d)\n", t.Queue, t.QueueLimit)
	fmt.Fprintf(b, "  demos waiting %d (%d days or more)\n", t.DemosWaiting, t.DemoDays)
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
		if i == 0 {
			writeInvestShare(b, r.InvestShare)
		}
	}
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
		fmt.Fprintf(b, "  causes %s\n", q.causesText())
	}
	fmt.Fprintf(b, "  bugs filed %d%s\n", q.BugsFiled, perLanding(q, q.BugsPerLanding, k.arrow("bugs_per_landing")))
	fmt.Fprintf(b, "  %s%s\n", q.escapedText(), k.arrow("escaped_bugs"))
	if q.Landed > 0 {
		fmt.Fprintf(b, "  Tester findings %d%s\n", q.Findings, perLanding(q, q.FindingsPerLanding, k.arrow("findings_per_landing")))
	}
}

// causesText is the refusals' causes as a line: each class that has one, then
// unset last, which is told even when it is 0. mw loop brief reads it too.
func (q Quality) causesText() string {
	var causes []string
	for _, class := range append(append([]string{}, CauseClasses...), CauseUnset) {
		if n := q.Causes[class]; n > 0 || class == CauseUnset {
			causes = append(causes, fmt.Sprintf("%s %d", class, n))
		}
	}
	return strings.Join(causes, " · ")
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

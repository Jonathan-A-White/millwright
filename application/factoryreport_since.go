package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// reportCommitRef is a commit named by its first characters or in full, and
// reportStoryRef a story id: a prefix, a hyphen, then the rest of the id.
var (
	reportCommitRef = regexp.MustCompile(`^[0-9a-fA-F]{4,40}$`)
	reportStoryRef  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-[A-Za-z0-9._-]+$`)
)

// subjectStory finds the story a commit of the factory's rig was made for: the
// branch mw next merged ("Merge branch 'mw/<story>' into ...") or the story a
// commit subject ends with in brackets ("... (<story>)").
var (
	mergeSubject = regexp.MustCompile(`^Merge branch '(?:[^'/]+/)?([^'/]+)'`)
	storySuffix  = regexp.MustCompile(`\(([A-Za-z][A-Za-z0-9_]*-[A-Za-z0-9._-]+)\)\s*$`)
)

// ReadReportSince reads --since: a window's start as ParseReportSince reads
// it, or else one landing to set the days before beside the days after, a
// story id or a commit of the factory's rig, returned as around.
func ReadReportSince(text string, now time.Time) (since time.Time, around string, err error) {
	since, err = ParseReportSince(text, now)
	if err == nil {
		return since, "", nil
	}
	text = strings.TrimSpace(text)
	if reportCommitRef.MatchString(text) || reportStoryRef.MatchString(text) {
		return time.Time{}, text, nil
	}
	return time.Time{}, "", fmt.Errorf("--since %q is neither a number of days (7d), a day (2026-10-09), a UTC time (2026-10-09T20:00:00Z), a story id nor a commit of the %s rig", text, FactoryRig)
}

// SubjectStory is the story a commit subject of the factory's rig names, or
// empty when it names none.
func SubjectStory(subject string) string {
	if m := mergeSubject.FindStringSubmatch(subject); m != nil {
		if reportStoryRef.MatchString(m[1]) {
			return m[1]
		}
		return ""
	}
	if m := storySuffix.FindStringSubmatch(subject); m != nil {
		return m[1]
	}
	return ""
}

// ReportLanding is the landing mw report --since <story|commit> is about: the
// ChangeWindowDays days before its day beside as many of the days after as
// have begun, the landing's own day in neither, the way FACTORY CHANGES
// measures one.
type ReportLanding struct {
	// Story is the story that landed, empty for a commit that names none.
	Story string `json:"story,omitempty"`
	Title string `json:"title,omitempty"`
	// Commit is the commit --since named, as its first seven characters.
	Commit string `json:"commit,omitempty"`
	// Landed is the day of the landing: the story's first landing in the
	// ledger, or the day a commit naming no landed story was made.
	// LandedBy says which: "ledger" or "commit".
	Landed     string `json:"landed"`
	LandedBy   string `json:"landed_by"`
	BeforeDays int    `json:"before_days"`
	AfterDays  int    `json:"after_days"`
	// Scope is what the rows sum: the factory, or the rig and host asked for.
	Scope string       `json:"scope"`
	Rows  []LandingRow `json:"rows"`

	// day is the start of the landing's day.
	day time.Time
}

// LandingRow is one measure in the days before a landing and in the days
// after it, and how it moved. Before or After is nil when those days had
// nothing to measure it with. Counts are compared per day, the two stretches
// being of different lengths until the week after is over.
type LandingRow struct {
	Part    string   `json:"part"`
	Measure string   `json:"measure"`
	Before  *float64 `json:"before,omitempty"`
	After   *float64 `json:"after,omitempty"`
	Change  string   `json:"change"`
}

// landingOf finds the landing r.Around names. A story's is its first landed
// row in the ledger; a commit's is the story its subject names when that
// landed, or else the day the commit was made.
func (r FactoryReport) landingOf(ctx context.Context, ledger []string) (ReportLanding, error) {
	around := strings.TrimSpace(r.Around)
	landing := ReportLanding{Story: around}
	var commit RigCommit
	if reportCommitRef.MatchString(around) {
		if r.Commits == nil || r.FactoryDir == "" {
			return landing, fmt.Errorf("--since %s: the %s rig is not checked out here to read the commit in", around, FactoryRig)
		}
		var err error
		if commit, err = r.Commits.CommitOf(ctx, r.FactoryDir, around); err != nil {
			return landing, fmt.Errorf("--since %s is not a commit of the %s rig: %w", around, FactoryRig, err)
		}
		landing.Story, landing.Title = SubjectStory(commit.Subject), strings.TrimSpace(commit.Subject)
		landing.Commit = commit.Commit[:min(7, len(commit.Commit))]
	}
	if landing.Story != "" {
		for _, line := range ledger {
			row, ok := ParseLedgerLine(line)
			if !ok || row.StoryID != landing.Story || !row.Landed() {
				continue
			}
			if day, err := time.Parse(LedgerDate, row.Date); err == nil && (landing.day.IsZero() || day.Before(landing.day)) {
				landing.day = day
			}
		}
	}
	if landing.day.IsZero() {
		if commit.At.IsZero() {
			return landing, fmt.Errorf("--since %s: %s has no landing in the %s ledger", around, around, r.Seat)
		}
		landing.day, landing.LandedBy = reportDay(commit.At), "commit"
	}
	landing.Landed = landing.day.Format(LedgerDate)
	if landing.LandedBy == "" {
		landing.LandedBy = "ledger"
	}
	return landing, nil
}

// windows are the days before the landing and the days after it that are
// over, as the report's previous window and window: today is left out until it
// is over, for half a day's landings set beside a whole day's read as a fall.
func (l *ReportLanding) windows(now time.Time) (before, after ReportWindow) {
	yesterday := reportDay(now).AddDate(0, 0, -1)
	before = ReportWindow{Since: l.day.AddDate(0, 0, -ChangeWindowDays), Until: l.day}
	end := l.day.AddDate(0, 0, ChangeWindowDays)
	if end.After(yesterday) {
		end = yesterday
	}
	after = ReportWindow{Since: l.day.AddDate(0, 0, 1), Until: end}
	l.BeforeDays = ChangeWindowDays
	l.AfterDays = max(0, int(end.Sub(l.day).Hours()/24))
	return before, after
}

// landingMeasure is one row of the side by side: its value in a stretch of
// days, and whether there was anything to measure it with.
type landingMeasure struct {
	part, name    string
	lowerIsBetter bool
	// perDay compares the measure per day: it is a count.
	perDay bool
	value  func(Quality, Speed, FuelUse, int) (float64, bool)
	format func(float64) string
}

var landingMeasures = []landingMeasure{
	{"QUALITY", "landed", false, true, func(q Quality, _ Speed, _ FuelUse, days int) (float64, bool) {
		return float64(q.Landed), days > 0
	}, func(v float64) string { return fmt.Sprintf("%.0f", v) }},
	{"QUALITY", "refused", true, true, func(q Quality, _ Speed, _ FuelUse, days int) (float64, bool) {
		return float64(q.Refused), days > 0
	}, func(v float64) string { return fmt.Sprintf("%.0f", v) }},
	{"QUALITY", "refusal rate", true, false, func(q Quality, _ Speed, _ FuelUse, _ int) (float64, bool) {
		return q.RefusalRate, q.Landed+q.Refused > 0
	}, func(v float64) string { return fmt.Sprintf("%.0f%%", v) }},
	{"QUALITY", "attempts/landing", true, false, func(q Quality, _ Speed, _ FuelUse, _ int) (float64, bool) {
		return q.AttemptsPerLanding, q.Landed > 0
	}, func(v float64) string { return fmt.Sprintf("%.2f", v) }},
	{"QUALITY", "bugs/landing", true, false, func(q Quality, _ Speed, _ FuelUse, _ int) (float64, bool) {
		return q.BugsPerLanding, q.Landed > 0
	}, func(v float64) string { return fmt.Sprintf("%.2f", v) }},
	{"QUALITY", "findings/landing", true, false, func(q Quality, _ Speed, _ FuelUse, _ int) (float64, bool) {
		return q.FindingsPerLanding, q.Landed > 0
	}, func(v float64) string { return fmt.Sprintf("%.2f", v) }},
	{"SPEED", "par median", true, false, func(_ Quality, s Speed, _ FuelUse, _ int) (float64, bool) {
		return s.ParMedian, s.WithPar > 0
	}, func(v float64) string { return fmt.Sprintf("x%.1f", v) }},
	{"SPEED", "par p90", true, false, func(_ Quality, s Speed, _ FuelUse, _ int) (float64, bool) {
		return s.ParP90, s.WithPar > 0
	}, func(v float64) string { return fmt.Sprintf("x%.1f", v) }},
	{"SPEED", "dispatch to landing", true, false, func(_ Quality, s Speed, _ FuelUse, _ int) (float64, bool) {
		return s.DispatchToLandingS, s.Measured > 0
	}, func(v float64) string { return gateClock(seconds(v)) }},
	{"FUEL", "tokens/landing", true, false, func(_ Quality, _ Speed, f FuelUse, _ int) (float64, bool) {
		return f.TokensPerLanding, f.WithTokens > 0
	}, func(v float64) string { return Thousands(int(v)) }},
	{"FUEL", "$/landing", true, false, func(_ Quality, _ Speed, f FuelUse, _ int) (float64, bool) {
		return f.USDPerLanding, f.WithCost > 0
	}, func(v float64) string { return fmt.Sprintf("$%.2f", v) }},
}

// measure sums the entries of the windows, narrowed like the blocks, into the
// rows of the side by side.
func (l *ReportLanding) measure(entries []reportEntry, rig, host string) {
	var before, after []reportEntry
	for _, e := range entries {
		if (rig != "" && e.rig != rig) || (host != "" && e.host != host) {
			continue
		}
		if e.previous {
			before = append(before, e)
		} else {
			after = append(after, e)
		}
	}
	var narrowed []string
	if rig != "" {
		narrowed = append(narrowed, "rig "+rig)
	}
	if host != "" {
		narrowed = append(narrowed, "host "+host)
	}
	l.Scope = "factory"
	if len(narrowed) > 0 {
		l.Scope = strings.Join(narrowed, ", ")
	}
	qb, sb, fb := sumQuality(before), sumSpeed(before), sumFuel(before)
	qa, sa, fa := sumQuality(after), sumSpeed(after), sumFuel(after)
	l.Rows = nil
	for _, m := range landingMeasures {
		row := LandingRow{Part: m.part, Measure: m.name, Change: "-"}
		if v, ok := m.value(qb, sb, fb, l.BeforeDays); ok {
			row.Before = &v
		}
		if v, ok := m.value(qa, sa, fa, l.AfterDays); ok {
			row.After = &v
		}
		if row.Before != nil && row.After != nil {
			was, is := *row.Before, *row.After
			if m.perDay {
				was, is = was/float64(l.BeforeDays), is/float64(l.AfterDays)
			}
			row.Change = landingChange(was, is, m.lowerIsBetter)
		}
		l.Rows = append(l.Rows, row)
	}
}

// landingChange is how a measure moved: "level" inside the ArrowBand, else
// the change as a share of the days before with better or worse by the
// measure, or only better or worse when the days before had none of it.
func landingChange(before, after float64, lowerIsBetter bool) string {
	t := NewTrend(after, before, lowerIsBetter)
	switch {
	case t.Verdict == "":
		return ChangeLevel
	case before == 0:
		return t.Verdict
	}
	return fmt.Sprintf("%+.0f%% %s", (after-before)*100/before, t.Verdict)
}

// String is mw report --since <landing> as it prints: what landed and when,
// the two stretches of days, then QUALITY, SPEED and FUEL with the days
// before beside the days after, in at most ReportWidth columns.
func (l ReportLanding) String() string {
	var b strings.Builder
	switch {
	case l.LandedBy == "commit" && l.Story != "":
		fmt.Fprintf(&b, "%s\n", fitLine(fmt.Sprintf("commit %s (%s) made %s", l.Commit, l.Story, l.Landed), ReportWidth))
	case l.Story != "" && l.Commit != "":
		fmt.Fprintf(&b, "%s\n", fitLine(fmt.Sprintf("%s landed %s (commit %s)", l.Story, l.Landed, l.Commit), ReportWidth))
	case l.Story != "":
		fmt.Fprintf(&b, "%s\n", fitLine(fmt.Sprintf("%s landed %s", l.Story, l.Landed), ReportWidth))
	default:
		fmt.Fprintf(&b, "commit %s made %s\n", l.Commit, l.Landed)
	}
	if title := strings.Join(strings.Fields(l.Title), " "); title != "" {
		fmt.Fprintf(&b, "%s\n", fitLine(title, ReportWidth))
	}
	day := func(offset int) string { return l.day.AddDate(0, 0, offset).Format(LedgerDate) }
	fmt.Fprintf(&b, "before: %d %s, %s to %s\n", l.BeforeDays, plural(l.BeforeDays, "day", "days"), day(-l.BeforeDays), day(-1))
	if l.AfterDays > 0 {
		fmt.Fprintf(&b, "after: %d %s, %s to %s\n", l.AfterDays, plural(l.AfterDays, "day", "days"), day(1), day(l.AfterDays))
	} else {
		b.WriteString("after: no day over yet\n")
	}
	b.WriteString("the landing's own day is in neither\n")
	if l.AfterDays < l.BeforeDays {
		b.WriteString("today counts once it is over\n")
	}
	if l.AfterDays > 0 && l.AfterDays != l.BeforeDays {
		b.WriteString("landed and refused are compared per day\n")
	}
	fmt.Fprintf(&b, "\n%s\n", landingLine(l.Scope, "before", "after", "change"))
	part := ""
	for _, row := range l.Rows {
		if row.Part != part {
			part = row.Part
			fmt.Fprintf(&b, "%s\n", part)
		}
		fmt.Fprintf(&b, "%s\n", landingLine(row.Measure, row.text(row.Before), row.text(row.After), row.Change))
	}
	return b.String()
}

// text is one of the row's values as the side by side prints it: "-" for none.
func (row LandingRow) text(v *float64) string {
	if v == nil {
		return "-"
	}
	for _, m := range landingMeasures {
		if m.name == row.Measure {
			return m.format(*v)
		}
	}
	return fmt.Sprint(*v)
}

// landingLine is one line of the side by side: the measure in 20 columns after
// two, then before and after in 9 columns each, then the change.
func landingLine(measure, before, after, change string) string {
	return strings.TrimRight(fmt.Sprintf("  %-20s%9s %9s  %s", fitLine(measure, 20), before, after, change), " ")
}

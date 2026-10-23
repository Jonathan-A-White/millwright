package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// The reserve and the resume time of mw pause (mw-gq6.707). PROVISIONAL: the
// usage is what the Governor reports with mw usage set, from his screenshot,
// until a source for it exists.
const (
	// UsageWeek is the length of the limit's week.
	UsageWeek = 7 * 24 * time.Hour
	// UsageReserveMax is the share of the week's quota kept back when the whole
	// week is left, in percent.
	UsageReserveMax = 10.0
	// UsageReserveEnds is the time left at which nothing is kept back.
	UsageReserveEnds = time.Hour
	// UsageResumeMargin is how long before the reset the quota should run out
	// at full throttle.
	UsageResumeMargin = 15 * time.Minute
	// UsageReadingsKey is the note the readings are kept in.
	UsageReadingsKey = "usage.readings"
	// usageReadingsKept is how many readings the note keeps.
	usageReadingsKept = 20
	// usageFullThrottleDays is how many of the busiest recent days the full
	// throttle burn is averaged over.
	usageFullThrottleDays = 3
	usageLookbackDays     = 7
)

// UsageReserve is R(t), in percent of the quota: UsageReserveMax with a whole
// week left, 0 with UsageReserveEnds left or less, and a straight line
// between.
func UsageReserve(left time.Duration) float64 {
	switch {
	case left <= UsageReserveEnds:
		return 0
	case left >= UsageWeek:
		return UsageReserveMax
	}
	return UsageReserveMax * float64(left-UsageReserveEnds) / float64(UsageWeek-UsageReserveEnds)
}

// UsageTripped reports whether a usage of percent has reached 100% less the
// reserve, with resets as far off as it is from now, and the reserve it read.
func UsageTripped(percent float64, resets, now time.Time) (tripped bool, reserve float64) {
	reserve = UsageReserve(resets.Sub(now))
	return percent >= 100-reserve, reserve
}

// UsageReading is one report of the quota used, and when the week resets.
type UsageReading struct {
	Percent float64   `json:"percent"`
	At      time.Time `json:"at"`
	Resets  time.Time `json:"resets"`
}

// UsageReadings keeps the readings in a note of the tracker.
type UsageReadings struct{ Notes SweepNotes }

// All are the readings kept, oldest first.
func (u UsageReadings) All(ctx context.Context) ([]UsageReading, error) {
	text, err := u.Notes.Note(ctx, UsageReadingsKey)
	if err != nil || strings.TrimSpace(text) == "" {
		return nil, err
	}
	var all []UsageReading
	if err := json.Unmarshal([]byte(text), &all); err != nil {
		return nil, fmt.Errorf("the note %s does not read: %w", UsageReadingsKey, err)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })
	return all, nil
}

// Add keeps one more reading.
func (u UsageReadings) Add(ctx context.Context, r UsageReading) error {
	all, err := u.All(ctx)
	if err != nil {
		return err
	}
	all = append(all, r)
	if len(all) > usageReadingsKept {
		all = all[len(all)-usageReadingsKept:]
	}
	text, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return u.Notes.SetNote(ctx, UsageReadingsKey, string(text))
}

// NextWeeklyReset is the first Friday 09:00 in the zone after now: when the
// weekly limit resets, until the Governor says otherwise with --resets.
func NextWeeklyReset(now time.Time, zone *time.Location) time.Time {
	local := now.In(zone)
	at := time.Date(local.Year(), local.Month(), local.Day(), 9, 0, 0, 0, zone)
	for at.Weekday() != time.Friday || !at.After(now) {
		at = at.AddDate(0, 0, 1)
	}
	return at
}

// UsageSet is mw usage set: it keeps the Governor's reading and, when the
// reading has reached 100% less the reserve, runs the pause.
type UsageSet struct {
	Readings UsageReadings
	// Zone is where the weekly reset is read in when none is named.
	Zone *time.Location
	// Pause runs the pause with the reason given; nil only reports.
	Pause func(ctx context.Context, reason string) error
	Now   func() time.Time
	Out   io.Writer
}

// Run keeps a reading of percent; resets is when the week resets, the
// previous reading's when still ahead, else the next Friday 09:00.
func (u UsageSet) Run(ctx context.Context, percent float64, resets time.Time) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("a usage of %v%% is not between 0 and 100", percent)
	}
	now := time.Now()
	if u.Now != nil {
		now = u.Now()
	}
	now = now.UTC()
	if resets.IsZero() {
		if all, err := u.Readings.All(ctx); err == nil && len(all) > 0 && all[len(all)-1].Resets.After(now) {
			resets = all[len(all)-1].Resets
		} else {
			resets = NextWeeklyReset(now, u.Zone)
		}
	}
	if !resets.After(now) {
		return fmt.Errorf("the reset %s is not in the future", resets.UTC().Format(time.RFC3339))
	}
	reading := UsageReading{Percent: percent, At: now, Resets: resets.UTC()}
	if err := u.Readings.Add(ctx, reading); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "usage %.1f%% kept at %s; the week resets %s\n", percent, now.Format(time.RFC3339), reading.Resets.Format(time.RFC3339))
	return UsageCheck{Reading: reading, Pause: u.Pause, Out: u.Out}.Run(ctx, now)
}

// UsageCheck reads a reading against the reserve.
type UsageCheck struct {
	Reading UsageReading
	Pause   func(ctx context.Context, reason string) error
	Out     io.Writer
}

// Run says where the usage stands against 100% less the reserve and, when it
// has reached it, pauses the factory.
func (c UsageCheck) Run(ctx context.Context, now time.Time) error {
	tripped, reserve := UsageTripped(c.Reading.Percent, c.Reading.Resets, now)
	line := fmt.Sprintf("reserve R(t) %.2f%% with %s left: the pause trips at %.2f%%", reserve,
		c.Reading.Resets.Sub(now).Round(time.Minute), 100-reserve)
	if !tripped {
		fmt.Fprintf(c.Out, "%s; usage %.1f%% is under it\n", line, c.Reading.Percent)
		return nil
	}
	fmt.Fprintf(c.Out, "%s; usage %.1f%% has reached it\n", line, c.Reading.Percent)
	if c.Pause == nil {
		return nil
	}
	return c.Pause(ctx, fmt.Sprintf("usage %.1f%% reached 100%% less the reserve (%.2f%%, %s before the reset)",
		c.Reading.Percent, reserve, c.Reading.Resets.Sub(now).Round(time.Minute)))
}

// Burn is where the tokens the factory spent each day are read.
type Burn interface {
	// TokensByDay are the tokens spent, by ledger date (2006-01-02).
	TokensByDay(ctx context.Context) (map[string]int, error)
}

// LedgerBurn reads the seat ledgers: the same fuel column mw status adds.
type LedgerBurn struct {
	Vault Vault
	Seats []string
}

// TokensByDay implements Burn.
func (b LedgerBurn) TokensByDay(ctx context.Context) (map[string]int, error) {
	days := map[string]int{}
	for _, seat := range b.Seats {
		lines, err := b.Vault.ReadLedger(ctx, seat)
		if err != nil {
			return nil, fmt.Errorf("the ledger of %s: %w", seat, err)
		}
		for _, line := range lines {
			if row, ok := ParseLedgerRow(line); ok {
				days[row.Date] += row.Tokens
			}
		}
	}
	return days, nil
}

// ResumePlan is when the pause should end, and the sum that says so.
type ResumePlan struct {
	At  time.Time
	Sum string
}

// ResumePlanner computes the resume time (mw-gq6.707): the moment from which
// the quota left, burned at full throttle, runs out UsageResumeMargin before
// the reset.
type ResumePlanner struct {
	Readings UsageReadings
	Burn     Burn
	// Rate, when above zero, is the burn at full throttle in percent of the
	// quota an hour, given by hand in place of the measured one.
	Rate float64
	Now  func() time.Time
}

// Plan reads the newest reading and the burn. It fails, saying what is
// missing, when it cannot measure the rate; the caller then has no time.
func (p ResumePlanner) Plan(ctx context.Context) (ResumePlan, error) {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	now = now.UTC()
	all, err := p.Readings.All(ctx)
	if err != nil {
		return ResumePlan{}, err
	}
	if len(all) == 0 {
		return ResumePlan{}, errors.New("no usage was reported: mw usage set <percent>")
	}
	last := all[len(all)-1]
	if !last.Resets.After(now) {
		return ResumePlan{}, fmt.Errorf("the reset of the last reading, %s, has passed: report the usage again", last.Resets.Format(time.RFC3339))
	}
	rate, how, err := p.rate(ctx, all)
	if err != nil {
		return ResumePlan{}, err
	}
	remaining := 100 - last.Percent
	if remaining < 0 {
		remaining = 0
	}
	run := time.Duration(remaining / rate * float64(time.Hour)).Round(time.Second)
	at := last.Resets.Add(-UsageResumeMargin - run)
	sum := fmt.Sprintf("resume sum: usage %.1f%% (read %s), %.1f%% left; burn %s; %.1f%% at that rate runs out in %s; "+
		"reset %s less %s less %s = %s",
		last.Percent, last.At.UTC().Format(time.RFC3339), remaining, how, remaining, run.Round(time.Minute),
		last.Resets.Format(time.RFC3339), UsageResumeMargin, run.Round(time.Minute), at.Format(time.RFC3339))
	if at.Before(now) {
		sum += fmt.Sprintf("; that is already past, so the resume is now (%s)", now.Format(time.RFC3339))
		at = now
	}
	return ResumePlan{At: at, Sum: sum}, nil
}

// rate is the burn at full throttle, in percent of the quota an hour, and how
// it was got.
func (p ResumePlanner) rate(ctx context.Context, all []UsageReading) (float64, string, error) {
	if p.Rate > 0 {
		return p.Rate, fmt.Sprintf("%.3f%%/h (given)", p.Rate), nil
	}
	if p.Burn == nil {
		return 0, "", errors.New("no burn source: pass the rate of the burn by hand")
	}
	days, err := p.Burn.TokensByDay(ctx)
	if err != nil {
		return 0, "", err
	}
	last := all[len(all)-1]
	// What a percent costs in tokens: the last reading against the nearest
	// earlier one of the same week that is lower.
	var first *UsageReading
	for i := len(all) - 2; i >= 0; i-- {
		if all[i].Resets.Equal(last.Resets) && all[i].Percent < last.Percent {
			first = &all[i]
			break
		}
	}
	if first == nil {
		return 0, "", errors.New("the burn cannot be measured from one reading: report the usage again after the factory has run, or pass the rate by hand")
	}
	var spent int
	for d := first.At.UTC().Truncate(24 * time.Hour); !d.After(last.At.UTC()); d = d.AddDate(0, 0, 1) {
		spent += days[d.Format(LedgerDate)]
	}
	if spent <= 0 {
		return 0, "", fmt.Errorf("the ledgers show no tokens between %s and %s", first.At.Format(time.RFC3339), last.At.Format(time.RFC3339))
	}
	perToken := (last.Percent - first.Percent) / float64(spent)

	var recent []int
	for i := 0; i < usageLookbackDays; i++ {
		recent = append(recent, days[last.At.UTC().AddDate(0, 0, -i).Format(LedgerDate)])
	}
	sort.Sort(sort.Reverse(sort.IntSlice(recent)))
	var busy int
	n := 0
	for _, t := range recent {
		if n == usageFullThrottleDays {
			break
		}
		busy += t
		n++
	}
	perHour := float64(busy) / float64(n) / 24
	if perHour <= 0 {
		return 0, "", errors.New("the ledgers show no tokens spent in the last week")
	}
	rate := perHour * perToken
	if rate <= 0 {
		return 0, "", errors.New("the measured burn is nothing")
	}
	return rate, fmt.Sprintf("%.3f%%/h at full throttle (%.0f tokens an hour, the busiest %d of %d days; %.2e%% a token from %.1f%% in %d tokens)",
		rate, perHour, n, usageLookbackDays, perToken, last.Percent-first.Percent, spent), nil
}

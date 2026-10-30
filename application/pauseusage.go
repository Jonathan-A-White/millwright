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
	// UsageFloorKey is the note the floor is kept in.
	UsageFloorKey = "usage.floor"
	// UsageStaleAfter is how old the newest reading may be before it is called
	// stale: two missed :04/:34 runs of the mw usage read timer, and a margin
	// (mw-gq6.855).
	UsageStaleAfter = 70 * time.Minute
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

// UsageFloor is a floor under the reserve, in percent of the quota, kept until
// a time (mw-gq6.832): while now is before Until, the reserve read is the
// larger of R(t) and Percent; from Until on, R(t) alone. The zero value is no
// floor.
type UsageFloor struct {
	Percent float64   `json:"percent"`
	Until   time.Time `json:"until"`
}

// Active reports whether the floor holds at now.
func (f UsageFloor) Active(now time.Time) bool {
	return f.Percent > 0 && now.Before(f.Until)
}

// Reserve is the reserve in force at now: R(t) for the time left until resets,
// or the floor when it is active and higher. The second value says the floor
// is what set it.
func (f UsageFloor) Reserve(resets, now time.Time) (reserve float64, byFloor bool) {
	reserve = UsageReserve(resets.Sub(now))
	if f.Active(now) && f.Percent > reserve {
		return f.Percent, true
	}
	return reserve, false
}

// UsageTripped reports whether a usage of percent has reached 100% less the
// reserve, with resets as far off as it is from now and floor under the
// reserve, and the reserve it read. A reset that has passed leaves the percent
// belonging to a finished week: it never trips (mw-gq6.855).
func UsageTripped(percent float64, resets, now time.Time, floor UsageFloor) (tripped bool, reserve float64) {
	if !resets.After(now) {
		// The week the percent was read in is over, and so is what it says.
		return false, 0
	}
	reserve, _ = floor.Reserve(resets, now)
	return percent >= 100-reserve, reserve
}

// UsageFloors keeps the floor in a note of the tracker.
type UsageFloors struct{ Notes SweepNotes }

// Get is the floor kept; the zero floor when none is, or when no notes are
// given.
func (u UsageFloors) Get(ctx context.Context) (UsageFloor, error) {
	if u.Notes == nil {
		return UsageFloor{}, nil
	}
	text, err := u.Notes.Note(ctx, UsageFloorKey)
	if err != nil || strings.TrimSpace(text) == "" {
		return UsageFloor{}, err
	}
	var f UsageFloor
	if err := json.Unmarshal([]byte(text), &f); err != nil {
		return UsageFloor{}, fmt.Errorf("the note %s does not read: %w", UsageFloorKey, err)
	}
	return f, nil
}

// Set keeps floor in place of any earlier one.
func (u UsageFloors) Set(ctx context.Context, floor UsageFloor) error {
	text, err := json.Marshal(floor)
	if err != nil {
		return err
	}
	return u.Notes.SetNote(ctx, UsageFloorKey, string(text))
}

// Clear takes the floor away.
func (u UsageFloors) Clear(ctx context.Context) error {
	return u.Notes.ClearNote(ctx, UsageFloorKey)
}

// UsageFloorSet is mw usage floor <percent> --until <time>, and mw usage floor
// off.
type UsageFloorSet struct {
	Floors UsageFloors
	Now    func() time.Time
	Out    io.Writer
}

// Run keeps a floor of percent until the given time.
func (u UsageFloorSet) Run(ctx context.Context, percent float64, until time.Time) error {
	if percent <= 0 || percent >= 100 {
		return fmt.Errorf("a floor of %v%% is not between 0 and 100", percent)
	}
	now := time.Now()
	if u.Now != nil {
		now = u.Now()
	}
	if !until.After(now) {
		return fmt.Errorf("the end of the floor, %s, is not in the future", until.UTC().Format(time.RFC3339))
	}
	floor := UsageFloor{Percent: percent, Until: until.UTC()}
	if err := u.Floors.Set(ctx, floor); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "floor %.1f%% kept until %s; after that the reserve is R(t) alone\n", floor.Percent, floor.Until.Format(time.RFC3339))
	return nil
}

// Off clears the floor.
func (u UsageFloorSet) Off(ctx context.Context) error {
	if err := u.Floors.Clear(ctx); err != nil {
		return err
	}
	fmt.Fprintln(u.Out, "floor cleared; the reserve is R(t) alone")
	return nil
}

// UsageReading is one report of the quota used, and when the week resets.
type UsageReading struct {
	Percent float64   `json:"percent"`
	At      time.Time `json:"at"`
	Resets  time.Time `json:"resets"`
	// Fable is the Fable's own share of the week used, in percent, kept beside
	// the reading for the Mayor's choice of model (mw-gq6.834); nil when the
	// reading did not give one.
	Fable *float64 `json:"fable,omitempty"`
}

// Stale says why the reading is no longer the current usage at now, and is
// empty when it is current (mw-gq6.855). A reading is stale when the week it
// was taken in has reset since (what it says belongs to the week before), or
// when it is older than UsageStaleAfter.
func (r UsageReading) Stale(now time.Time) string {
	if r.BeforeReset(now) {
		return "taken before the week reset at " + r.Resets.UTC().Format(time.RFC3339)
	}
	if age := now.Sub(r.At); age > UsageStaleAfter {
		return "the newest reading is " + usageAge(age) + " old"
	}
	return ""
}

// usageAge is d in whole minutes as a person says it: 11h2m, 3h, 5m.
func usageAge(d time.Duration) string {
	text := d.Round(time.Minute).String()
	text = strings.TrimSuffix(text, "m0s") + "m"
	if strings.HasSuffix(text, "h0m") {
		return strings.TrimSuffix(text, "0m")
	}
	return text
}

// BeforeReset is whether the reading was taken before a reset that has since
// passed: it says nothing of the week now running.
func (r UsageReading) BeforeReset(now time.Time) bool {
	return !r.Resets.After(now)
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
	// Floors is the floor under the reserve; no Notes, no floor.
	Floors UsageFloors
	// Zone is where the weekly reset is read in when none is named.
	Zone *time.Location
	// Pause runs the pause with the reason given; nil only reports.
	Pause func(ctx context.Context, reason string) error
	// Fable, when set, is the Fable's share of the week, kept with the reading.
	Fable *float64
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
	reading := UsageReading{Percent: percent, At: now, Resets: resets.UTC(), Fable: u.Fable}
	if err := u.Readings.Add(ctx, reading); err != nil {
		return err
	}
	fmt.Fprintf(u.Out, "usage %.1f%% kept at %s; the week resets %s%s\n", percent, now.Format(time.RFC3339), reading.Resets.Format(time.RFC3339), fableNote(reading))
	floor, err := u.Floors.Get(ctx)
	if err != nil {
		return err
	}
	return UsageCheck{Reading: reading, Floor: floor, Pause: u.Pause, Out: u.Out}.Run(ctx, now)
}

// fableNote is the Fable's share beside a reading, or nothing.
func fableNote(r UsageReading) string {
	if r.Fable == nil {
		return ""
	}
	return fmt.Sprintf("; Fable %.1f%%", *r.Fable)
}

// UsageCheck reads a reading against the reserve.
type UsageCheck struct {
	Reading UsageReading
	// Floor is the floor under the reserve; the zero value is none.
	Floor UsageFloor
	Pause func(ctx context.Context, reason string) error
	Out   io.Writer
}

// Run says where the usage stands against 100% less the reserve and, when it
// has reached it, pauses the factory.
func (c UsageCheck) Run(ctx context.Context, now time.Time) error {
	if c.Reading.BeforeReset(now) {
		fmt.Fprintf(c.Out, "usage %.1f%% was read before the week reset at %s and is not counted\n", c.Reading.Percent, c.Reading.Resets.UTC().Format(time.RFC3339))
		return nil
	}
	tripped, reserve := UsageTripped(c.Reading.Percent, c.Reading.Resets, now, c.Floor)
	_, byFloor := c.Floor.Reserve(c.Reading.Resets, now)
	line := fmt.Sprintf("reserve R(t) %.2f%% with %s left: the pause trips at %.2f%%", UsageReserve(c.Reading.Resets.Sub(now)),
		c.Reading.Resets.Sub(now).Round(time.Minute), 100-reserve)
	if byFloor {
		line = fmt.Sprintf("reserve R(t) %.2f%% with %s left, held up by the floor %.2f%% until %s: the pause trips at %.2f%%",
			UsageReserve(c.Reading.Resets.Sub(now)), c.Reading.Resets.Sub(now).Round(time.Minute), reserve,
			c.Floor.Until.UTC().Format(time.RFC3339), 100-reserve)
	}
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

// UsageShow is mw usage show: the last reading and the floor, against the
// reserve; it changes nothing.
type UsageShow struct {
	Readings UsageReadings
	Floors   UsageFloors
	Now      func() time.Time
	Out      io.Writer
}

// Run prints the floor, the last reading and where it stands.
func (u UsageShow) Run(ctx context.Context) error {
	now := time.Now()
	if u.Now != nil {
		now = u.Now()
	}
	now = now.UTC()
	floor, err := u.Floors.Get(ctx)
	if err != nil {
		return err
	}
	switch {
	case floor.Active(now):
		fmt.Fprintf(u.Out, "floor %.1f%% until %s\n", floor.Percent, floor.Until.UTC().Format(time.RFC3339))
	case floor.Percent > 0:
		fmt.Fprintf(u.Out, "no floor in force (floor %.1f%% ended %s)\n", floor.Percent, floor.Until.UTC().Format(time.RFC3339))
	default:
		fmt.Fprintln(u.Out, "no floor kept")
	}
	all, err := u.Readings.All(ctx)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(u.Out, "no usage was reported: mw usage set <percent>")
		return nil
	}
	last := all[len(all)-1]
	fmt.Fprintf(u.Out, "last reading: %.1f%% at %s; the week resets %s%s\n",
		last.Percent, last.At.UTC().Format(time.RFC3339), last.Resets.UTC().Format(time.RFC3339), fableNote(last))
	if why := last.Stale(now); why != "" {
		fmt.Fprintf(u.Out, "STALE: %s\n", why)
	}
	return UsageCheck{Reading: last, Floor: floor, Out: u.Out}.Run(ctx, now)
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

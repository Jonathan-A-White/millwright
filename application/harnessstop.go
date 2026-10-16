package application

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The reasons a close-out lands nothing because the harness stopped the session
// for something that is not the work's (mw-gq6.434): the account's usage limit,
// the API's rate limit, an overloaded API, or the API failing outright. None of
// them says anything of the story, so the attempt is not counted, and the host
// claims nothing new until the time the stop names.
const (
	ReasonUsageLimit  Reason = "usage-limit"
	ReasonRateLimit   Reason = "rate-limit"
	ReasonOverloaded  Reason = "overloaded"
	ReasonServerError Reason = "server-error"
)

// HarnessStopWait is how long after the session ended a host waits on a stop
// that names no time of its own: a rate limit, an overloaded or failing API. The
// harness has already tried again on its own before it gave up, so the next tick
// is too soon.
const HarnessStopWait = 15 * time.Minute

// UsageLimitWait is how long a host waits on a usage limit whose reset time
// could not be read: long enough that it is not tried every tick, short enough
// that a misread costs an hour, not a day.
const UsageLimitWait = time.Hour

// HarnessStop is a session the harness stopped for something that is not the
// work's: Reason says which, Said is the harness's own words, and Until is when
// it is worth starting a session again.
type HarnessStop struct {
	Reason Reason
	Said   string
	Until  time.Time
}

// String is the stop as a person reads it.
func (s HarnessStop) String() string {
	return fmt.Sprintf("%s until %s (%s)", s.Reason, s.Until.UTC().Format(time.RFC3339), s.Said)
}

var (
	// usageLimited is the harness's word that the account's usage limit is
	// reached: "You've hit your weekly limit", "Claude AI usage limit reached",
	// "5-hour limit reached".
	usageLimited = regexp.MustCompile(`(?i)(you've hit your\b.*\blimit|usage limit reached|\d+-hour limit reached)`)
	// usageEpoch is the older form, which ends in the reset as Unix seconds.
	usageEpoch = regexp.MustCompile(`limit reached\|(\d{9,})`)
	// usageResets is the reset time the newer form names: "resets Oct 16, 9am
	// (America/New_York)", or with no date, "resets 3:30pm (Europe/London)".
	usageResets = regexp.MustCompile(`(?i)resets\s+(?:([a-z]{3})[a-z]*\.?\s+(\d{1,2}),?\s+(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)(?:\s*\(([^)]+)\))?`)
	// apiError is the harness's report of the API's own answer: "API Error: 529
	// {...overloaded_error...}".
	apiError = regexp.MustCompile(`(?i)^api error:?\s*(\d{3})?`)
)

// HarnessStopOf reads whether a session the harness ended without finishing was
// stopped for something that is not the work's, and when to try again. ended is
// when the session ended: a reset named without a date is the first such time
// after it, and a stop that names no time waits HarnessStopWait from it. A
// finished session, or one whose result names anything else, is no harness
// stop.
func HarnessStopOf(result SessionResult, ended time.Time) (HarnessStop, bool) {
	if result.Finished() {
		return HarnessStop{}, false
	}
	said := firstLine(result.Said)
	stop := HarnessStop{Said: said}
	lower := strings.ToLower(said)
	switch {
	case usageLimited.MatchString(said) && !strings.Contains(lower, "rate limit"):
		stop.Reason = ReasonUsageLimit
		stop.Until = usageReset(said, ended)
		return stop, true
	case strings.Contains(lower, "rate_limit_error"), strings.Contains(lower, "overloaded_error"), apiError.MatchString(said):
	default:
		return HarnessStop{}, false
	}
	status := ""
	if m := apiError.FindStringSubmatch(said); m != nil {
		status = m[1]
	}
	switch {
	case status == "429" || strings.Contains(lower, "rate_limit_error"):
		stop.Reason = ReasonRateLimit
	case status == "529" || strings.Contains(lower, "overloaded"):
		stop.Reason = ReasonOverloaded
	case strings.HasPrefix(status, "5") || strings.Contains(lower, "api_error"):
		stop.Reason = ReasonServerError
	default:
		// Another answer of the API's (a 400, a refused key) is the factory's to
		// look at, not a stop that waits.
		return HarnessStop{}, false
	}
	stop.Until = ended.Add(HarnessStopWait)
	return stop, true
}

// usageReset is the time a usage limit resets, as the harness named it, or
// UsageLimitWait after ended when it could not be read.
func usageReset(said string, ended time.Time) time.Time {
	if m := usageEpoch.FindStringSubmatch(said); m != nil {
		if secs, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return time.Unix(secs, 0)
		}
	}
	m := usageResets.FindStringSubmatch(said)
	if m == nil {
		return ended.Add(UsageLimitWait)
	}
	zone := time.UTC
	if name := strings.TrimSpace(m[6]); name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			zone = loc
		}
	}
	hour, _ := strconv.Atoi(m[3])
	minute, _ := strconv.Atoi(m[4])
	if hour < 1 || hour > 12 || minute > 59 {
		return ended.Add(UsageLimitWait)
	}
	hour %= 12
	if strings.EqualFold(m[5], "pm") {
		hour += 12
	}
	from := ended.In(zone)
	if m[1] == "" {
		at := time.Date(from.Year(), from.Month(), from.Day(), hour, minute, 0, 0, zone)
		if !at.After(ended) {
			at = at.AddDate(0, 0, 1)
		}
		return at
	}
	month, err := time.Parse("Jan", strings.ToUpper(m[1][:1])+strings.ToLower(m[1][1:]))
	day, _ := strconv.Atoi(m[2])
	if err != nil || day < 1 || day > 31 {
		return ended.Add(UsageLimitWait)
	}
	at := time.Date(from.Year(), month.Month(), day, hour, minute, 0, 0, zone)
	// A reset is never long before the stop it ends: one a day or more before it
	// is next year's (a stop on 31 December that resets on 2 January).
	if at.Before(ended.Add(-24 * time.Hour)) {
		at = at.AddDate(1, 0, 0)
	}
	return at
}

// sessionEnded is when a story's last session ended, as near as can be read
// without the harness saying: when it started (the claim, or a send-back after
// it) and the time it ran. A story with neither time written reads as having
// ended a day before now, so that a stop with no date of its own is not held on
// a guess.
func sessionEnded(detail StoryDetail, result SessionResult, now time.Time) time.Time {
	started := detail.ClaimedAt
	if detail.SentBackAt.After(started) {
		started = detail.SentBackAt
	}
	if started.IsZero() {
		return now.Add(-24 * time.Hour)
	}
	ran := result.Duration
	if result.APIDuration > ran {
		ran = result.APIDuration
	}
	return started.Add(ran)
}

// harnessStopOfAttempt reads the result of a story's last attempt from the vault
// and whether the harness stopped it for something that is not the work's. A
// result that is missing or cannot be read is no harness stop.
func harnessStopOfAttempt(ctx context.Context, vault Vault, detail StoryDetail, now time.Time) (HarnessStop, bool) {
	if vault == nil || detail.Attempts < 1 {
		return HarnessStop{}, false
	}
	printed, err := vault.ReadRunFile(ctx, detail.Story.ID, ResultFileNameForAttempt(detail.Attempts))
	if err != nil {
		return HarnessStop{}, false
	}
	result, err := ReadSessionResult(printed)
	if err != nil {
		return HarnessStop{}, false
	}
	return HarnessStopOf(result, sessionEnded(detail, result, now))
}

// HarnessHoldKey is the note a host's hold on new claims is kept under: the time
// it lasts until, the reason and the harness's words.
func HarnessHoldKey(host string) string { return "host." + host + ".harness_hold" }

// HarnessHold is a host's hold on new claims after a harness stop.
type HarnessHold = HarnessStop

// readHarnessHold reads the hold kept in the host's note. A note that is not
// there, cannot be read or does not parse holds nothing.
func readHarnessHold(ctx context.Context, notes SweepNotes, host string) (HarnessHold, bool) {
	if notes == nil {
		return HarnessHold{}, false
	}
	value, err := notes.Note(ctx, HarnessHoldKey(host))
	if err != nil || strings.TrimSpace(value) == "" {
		return HarnessHold{}, false
	}
	fields := strings.SplitN(strings.TrimSpace(value), " ", 3)
	until, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return HarnessHold{}, false
	}
	hold := HarnessHold{Until: until}
	if len(fields) > 1 {
		hold.Reason = Reason(fields[1])
	}
	if len(fields) > 2 {
		hold.Said = fields[2]
	}
	return hold, true
}

// keepHarnessHold writes the hold to the host's note, unless the note already
// holds the host as long or longer.
func keepHarnessHold(ctx context.Context, notes SweepNotes, host string, hold HarnessHold) error {
	if notes == nil {
		return nil
	}
	if kept, ok := readHarnessHold(ctx, notes, host); ok && !kept.Until.Before(hold.Until) {
		return nil
	}
	value := fmt.Sprintf("%s %s %s", hold.Until.UTC().Format(time.RFC3339), hold.Reason, hold.Said)
	return notes.SetNote(ctx, HarnessHoldKey(host), value)
}

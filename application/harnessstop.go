package application

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	// usageLimitNotice is the harness's notice that the account's usage limit
	// stopped it: "You've hit your weekly limit · resets …", or the session,
	// 5-hour or monthly limit, with either apostrophe; "Claude AI usage limit
	// reached"; "5-hour limit reached". It is the one reader of that notice, for
	// mw next, status, dispatch, retry and the mill alike (mw-gq6.471).
	usageLimitNotice = regexp.MustCompile(`(?i)(\b(?:you(?:'|’)ve|you have) hit your\b[^\n]{0,40}?\blimit\b|usage limit reached|\d+-hour limit reached)`)
	// usageEpoch is the older form, which ends in the reset as Unix seconds.
	usageEpoch = regexp.MustCompile(`limit reached\|(\d{9,})`)
	// usageResets is the reset time the newer form names: "resets Oct 16, 9am
	// (America/New_York)", or with no date, "resets 3:30pm (Europe/London)".
	usageResets = regexp.MustCompile(`(?i)resets\s+(?:([a-z]{3})[a-z]*\.?\s+(\d{1,2}),?\s+(?:at\s+)?)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)(?:\s*\(([^)]+)\))?`)
	// apiError is the harness's report of the API's own answer: "API Error: 529
	// {...overloaded_error...}".
	apiError = regexp.MustCompile(`(?i)^api error:?\s*(\d{3})?`)
)

// AtUsageLimit reports whether text holds the harness's notice that the usage
// limit stopped it (mw-gq6.433).
func AtUsageLimit(text string) bool { return usageLimitNotice.MatchString(text) }

// usageLimitLine is the line of text that holds the usage limit's notice, the
// first that names its reset when more than one holds it, and empty when none
// does. A line that speaks of a rate limit is the API's, not the account's.
func usageLimitLine(text string) string {
	found := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !usageLimitNotice.MatchString(line) || strings.Contains(strings.ToLower(line), "rate limit") {
			continue
		}
		if usageEpoch.MatchString(line) || usageResets.MatchString(line) {
			return line
		}
		if found == "" {
			found = line
		}
	}
	return found
}

// usageLimitStop is the stop a usage limit's line names.
func usageLimitStop(line string, ended time.Time) HarnessStop {
	return HarnessStop{Reason: ReasonUsageLimit, Said: line, Until: usageReset(line, ended)}
}

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
	if line := usageLimitLine(result.Said); line != "" {
		return usageLimitStop(line, ended), true
	}
	said := firstLine(result.Said)
	stop := HarnessStop{Said: said}
	lower := strings.ToLower(said)
	switch {
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

// HarnessStopOfWords reads whether words the harness printed that are not a
// result — what it wrote where its JSON belonged, the tail of its pane, an
// error that carries its last words — say it stopped at the usage limit
// (mw-gq6.471). Only the usage limit's own line is read in them: the rest of
// such words may be mw's or the work's, and an API's error among them may be
// one the harness went on past.
func HarnessStopOfWords(words string, ended time.Time) (HarnessStop, bool) {
	line := usageLimitLine(words)
	if line == "" {
		return HarnessStop{}, false
	}
	return usageLimitStop(line, ended), true
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

// sessionStarted is when a story's last session started: its claim, or a
// send-back after it. Zero when neither time is written.
func sessionStarted(detail StoryDetail) time.Time {
	if detail.SentBackAt.After(detail.ClaimedAt) {
		return detail.SentBackAt
	}
	return detail.ClaimedAt
}

// sessionEnded is when a story's last session ended, as near as can be read
// without the harness saying: when it started (the claim, or a send-back after
// it) and the time it ran. A story with neither time written reads as having
// ended a day before now, so that a stop with no date of its own is not held on
// a guess.
func sessionEnded(detail StoryDetail, result SessionResult, now time.Time) time.Time {
	started := sessionStarted(detail)
	if started.IsZero() {
		return now.Add(-24 * time.Hour)
	}
	ran := result.Duration
	if result.APIDuration > ran {
		ran = result.APIDuration
	}
	return started.Add(ran)
}

// HarnessStopFileName is the run file mw next keeps a harness stop in when it
// read it from words that are not a result (mw-gq6.471): the session left no
// JSON, and what it printed instead, or its pane, named the usage limit. It
// holds when the session ended and the harness's line, so that mw dispatch,
// retry and status read the same stop from the run once the pane is gone.
const HarnessStopFileName = "harness-stop.txt"

// HarnessStopFileNameForAttempt is HarnessStopFileName for one attempt, named
// as the result it stands beside.
func HarnessStopFileNameForAttempt(attempt int) string {
	return attemptRunFileName(HarnessStopFileName, attempt)
}

// HarnessStopKept is what HarnessStopFileName holds: when the session ended, on
// a line of its own, then the harness's words.
func HarnessStopKept(ended time.Time, said string) string {
	return ended.UTC().Format(time.RFC3339) + "\n" + said + "\n"
}

// harnessStopOfRun is the one reader of how attempt of a story's session
// ended, from what its run holds: its result, when that is the JSON a harness
// writes; else the stop mw next kept beside it from the pane or the words the
// harness printed (HarnessStopFileName); else those words themselves. A
// result that cannot be read is no harness stop.
func harnessStopOfRun(ctx context.Context, vault Vault, detail StoryDetail, attempt int, now time.Time) (HarnessStop, bool) {
	if vault == nil {
		return HarnessStop{}, false
	}
	printed, err := vault.ReadRunFile(ctx, detail.Story.ID, ResultFileNameForAttempt(attempt))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return HarnessStop{}, false
	}
	if result, err := ReadSessionResult(printed); err == nil {
		return HarnessStopOf(result, sessionEnded(detail, result, now))
	}
	if stop, ok := keptHarnessStop(ctx, vault, detail, attempt); ok {
		return stop, true
	}
	return HarnessStopOfWords(printed, sessionEnded(detail, SessionResult{}, now))
}

// keptHarnessStop reads the stop mw next kept for attempt. One kept before the
// story's last session started is an earlier session's, of an attempt given
// back uncounted and started again under the same number: it says nothing of
// this one.
func keptHarnessStop(ctx context.Context, vault Vault, detail StoryDetail, attempt int) (HarnessStop, bool) {
	kept, err := vault.ReadRunFile(ctx, detail.Story.ID, HarnessStopFileNameForAttempt(attempt))
	if err != nil {
		return HarnessStop{}, false
	}
	at, words, _ := strings.Cut(strings.TrimSpace(kept), "\n")
	ended, err := time.Parse(time.RFC3339, strings.TrimSpace(at))
	if err != nil || ended.Before(sessionStarted(detail)) {
		return HarnessStop{}, false
	}
	return HarnessStopOfWords(words, ended)
}

// harnessStopOfAttempt reads whether the harness stopped a story's last
// attempt for something that is not the work's (harnessStopOfRun). A story
// never attempted has no stop to read.
func harnessStopOfAttempt(ctx context.Context, vault Vault, detail StoryDetail, now time.Time) (HarnessStop, bool) {
	if detail.Attempts < 1 {
		return HarnessStop{}, false
	}
	return harnessStopOfRun(ctx, vault, detail, detail.Attempts, now)
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

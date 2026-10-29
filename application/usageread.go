package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The pauses of mw usage read, as the first reading by hand (mw-gq6.834) found
// them: Claude takes about ten seconds to be ready to be typed to, and the
// /usage screen about eight to be drawn.
const (
	UsageReadStartWait  = 10 * time.Second
	UsageReadScreenWait = 8 * time.Second
	// usageReadLooks is how many times the screen is captured before it is
	// given up as unreadable; the looks after the first are UsageReadLookWait
	// apart.
	usageReadLooks    = 3
	UsageReadLookWait = 5 * time.Second
	// usageProbeCols and usageProbeRows are the size of the probe's terminal:
	// wide enough that no line of the screen wraps.
	usageProbeCols = 120
	usageProbeRows = 40
)

// UsageScreen is what the /usage screen said of the week.
type UsageScreen struct {
	// Percent is "Current week (all models)" used, and Resets when that week
	// resets.
	Percent float64
	Resets  time.Time
	// Fable is "Current week (Fable)" used, when the screen showed one.
	Fable *float64
}

var (
	usagePercentRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%\s*used`)
	usageResetsRe  = regexp.MustCompile(`(?i)\bResets\s+(?:([A-Za-z]{3})[A-Za-z]*\.?\s+(\d{1,2}),?\s+)?(\d{1,2})(?::(\d{2}))?\s*([ap])\.?m\.?(?:\s*\(([^)]+)\))?`)
	usageWeekRe    = regexp.MustCompile(`(?i)current week \(all models\)`)
	usageFableRe   = regexp.MustCompile(`(?i)current week \(fable\)`)
	usageLabelRe   = regexp.MustCompile(`(?i)\bcurrent (?:week|session)\b`)
)

// ParseUsageScreen reads the text of Claude's /usage screen. zone is where a
// reset is read in when the screen names none. It fails, saying what it could
// not find, on a screen with no Usage tab, or a week with no percent or no
// reset of its own.
func ParseUsageScreen(text string, now time.Time, zone *time.Location) (UsageScreen, error) {
	lines := strings.Split(text, "\n")
	week, ok := usageSection(lines, usageWeekRe)
	if !ok {
		return UsageScreen{}, errors.New("the screen shows no 'Current week (all models)': it is not the Usage tab")
	}
	percent, ok := usageSectionPercent(week)
	if !ok {
		return UsageScreen{}, errors.New("'Current week (all models)' has no 'N% used' on the screen")
	}
	resets, err := usageSectionResets(week, now, zone)
	if err != nil {
		return UsageScreen{}, fmt.Errorf("'Current week (all models)': %w", err)
	}
	screen := UsageScreen{Percent: percent, Resets: resets}
	if fable, ok := usageSection(lines, usageFableRe); ok {
		if p, ok := usageSectionPercent(fable); ok {
			screen.Fable = &p
		}
	}
	return screen, nil
}

// usageSection is the lines of the block a label heads: from the label to the
// next "Current ..." label.
func usageSection(lines []string, label *regexp.Regexp) ([]string, bool) {
	for i, line := range lines {
		if !label.MatchString(line) {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if usageLabelRe.MatchString(lines[j]) {
				end = j
				break
			}
		}
		return lines[i:end], true
	}
	return nil, false
}

func usageSectionPercent(section []string) (float64, bool) {
	for _, line := range section {
		if m := usagePercentRe.FindStringSubmatch(line); m != nil {
			p, err := strconv.ParseFloat(m[1], 64)
			return p, err == nil
		}
	}
	return 0, false
}

func usageSectionResets(section []string, now time.Time, zone *time.Location) (time.Time, error) {
	for _, line := range section {
		m := usageResetsRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		where := zone
		if m[6] != "" {
			loc, err := time.LoadLocation(strings.TrimSpace(m[6]))
			if err != nil {
				return time.Time{}, fmt.Errorf("the zone %q of the reset is not known: %w", m[6], err)
			}
			where = loc
		}
		if where == nil {
			where = time.UTC
		}
		hour, _ := strconv.Atoi(m[3])
		minute := 0
		if m[4] != "" {
			minute, _ = strconv.Atoi(m[4])
		}
		if hour < 1 || hour > 12 || minute > 59 {
			return time.Time{}, fmt.Errorf("the reset %q is not a time", strings.TrimSpace(line))
		}
		hour %= 12
		if strings.EqualFold(m[5], "p") {
			hour += 12
		}
		local := now.In(where)
		if m[1] == "" {
			at := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, where)
			if !at.After(now) {
				at = time.Date(local.Year(), local.Month(), local.Day()+1, hour, minute, 0, 0, where)
			}
			return at, nil
		}
		month, ok := usageMonths[strings.ToLower(m[1])]
		if !ok {
			return time.Time{}, fmt.Errorf("the month of the reset %q is not known", strings.TrimSpace(line))
		}
		day, _ := strconv.Atoi(m[2])
		at := time.Date(local.Year(), month, day, hour, minute, 0, 0, where)
		if at.Before(now.AddDate(0, 0, -180)) {
			at = at.AddDate(1, 0, 0)
		}
		return at, nil
	}
	return time.Time{}, errors.New("has no 'Resets ...' on the screen")
}

var usageMonths = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

// UsageTerminal is a terminal the factory types into and reads, apart from
// every session a seat works in: the port behind mw usage read.
type UsageTerminal interface {
	// Open starts a detached terminal named name, running a login shell in dir.
	Open(ctx context.Context, name, dir string, cols, rows int) error
	// Type types text literally.
	Type(ctx context.Context, name, text string) error
	// Press presses one key by its tmux name: Enter, Escape.
	Press(ctx context.Context, name, key string) error
	// Capture is the text on the terminal's screen.
	Capture(ctx context.Context, name string) (string, error)
	// Close ends the terminal; one already gone is not an error.
	Close(ctx context.Context, name string) error
}

// UsageRead is mw usage read: it reads the week's quota off Claude's /usage
// screen through a throwaway terminal and keeps it as mw usage set would.
type UsageRead struct {
	Terminal UsageTerminal
	// Lock keeps two reads from running at once.
	Lock GristLock
	// Dir is where claude is started.
	Dir  string
	Zone *time.Location
	// Set keeps the reading; its Fable is filled in from the screen.
	Set UsageSet
	// Keep saves the capture for a look and says where.
	Keep func(text string) (string, error)
	// Wait sleeps; nil is time.After.
	Wait func(ctx context.Context, d time.Duration) error
	// Name is the probe terminal's name, unique to a run and apart from every
	// seat's.
	Name func() string
	Now  func() time.Time
	Out  io.Writer
}

// Run reads the screen and sets the usage; on a screen it cannot read it
// changes nothing, keeps the capture and fails.
func (u UsageRead) Run(ctx context.Context) error {
	release, taken, err := u.Lock.TryTake(ctx)
	if err != nil {
		return err
	}
	if !taken {
		fmt.Fprintln(u.Out, "another usage read is under way; nothing done")
		return nil
	}
	defer release()

	name := u.Name()
	// The terminal is closed even when the run is cancelled.
	defer u.Terminal.Close(context.WithoutCancel(ctx), name)
	if err := u.Terminal.Open(ctx, name, u.Dir, usageProbeCols, usageProbeRows); err != nil {
		return fmt.Errorf("opening the probe terminal: %w", err)
	}
	for _, step := range []func() error{
		func() error { return u.Terminal.Type(ctx, name, "claude") },
		func() error { return u.Terminal.Press(ctx, name, "Enter") },
		func() error { return u.wait(ctx, UsageReadStartWait) },
		func() error { return u.Terminal.Type(ctx, name, "/usage") },
		func() error { return u.Terminal.Press(ctx, name, "Enter") },
		func() error { return u.wait(ctx, UsageReadScreenWait) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	now := time.Now()
	if u.Now != nil {
		now = u.Now()
	}
	var screen UsageScreen
	var text string
	for look := 1; ; look++ {
		if text, err = u.Terminal.Capture(ctx, name); err != nil {
			return fmt.Errorf("capturing the probe terminal: %w", err)
		}
		if screen, err = ParseUsageScreen(text, now, u.Zone); err == nil || look == usageReadLooks {
			break
		}
		if err := u.wait(ctx, UsageReadLookWait); err != nil {
			return err
		}
	}
	// Leave the Usage tab and the program before the terminal is closed.
	u.Terminal.Press(ctx, name, "Escape")
	where, keepErr := u.Keep(text)
	if err != nil {
		note := "the capture is at " + where
		if keepErr != nil {
			note = "the capture could not be kept: " + keepErr.Error()
		}
		return fmt.Errorf("the /usage screen could not be read, so the usage is unchanged (%s): %w", note, err)
	}
	set := u.Set
	set.Fable = screen.Fable
	return set.Run(ctx, screen.Percent, screen.Resets)
}

func (u UsageRead) wait(ctx context.Context, d time.Duration) error {
	if u.Wait != nil {
		return u.Wait(ctx, d)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

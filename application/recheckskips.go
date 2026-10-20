package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RecheckSkipsHeading heads the section of mw status that names the re-runs of a
// suite bug's spec on main that were not made (mw-gq6.640). The report leaves it
// out when there were none.
const RecheckSkipsHeading = "RECHECKS NOT MADE"

// RecheckSkipsWindow is how far back a skipped re-run is still shown and mailed.
const RecheckSkipsWindow = 24 * time.Hour

// RecheckSkipName is the mw doctor check that mails the Mayor the skipped re-runs.
const RecheckSkipName = "recheck-skipped"

// MailRecheckSkipped starts the subject of that mail.
const MailRecheckSkipped = "Re-run not made"

// RecheckSkip is one pre-dispatch re-run of a suite bug's spec that was not made,
// or could not be acted on: a guard that cannot run is not a guard, so it is
// kept where mw status and mw doctor read it, and not only in the dispatch's
// printed report. Said is the words the started line carries.
type RecheckSkip struct {
	At    time.Time
	Story string
	Said  string
}

// recheckSkipLine is the line a dispatch leaves in the recheck log.
func recheckSkipLine(at time.Time, story, said string) string {
	return fmt.Sprintf("%s %s %s", at.UTC().Format(time.RFC3339), story, oneLine(said))
}

// ReadRecheckSkips reads the recheck log's lines no older than RecheckSkipsWindow
// before now, oldest first, a story's same words once. A log that is nil or cannot be read has none, and a
// line that is not a story and its words after a time is passed over.
func ReadRecheckSkips(ctx context.Context, log TickLog, now time.Time) []RecheckSkip {
	if log == nil {
		return nil
	}
	lines, err := log.Read(ctx)
	if err != nil {
		return nil
	}
	var skips []RecheckSkip
	for _, line := range lines {
		at, words, ok := splitTickLine(line)
		if !ok || now.Sub(at) > RecheckSkipsWindow {
			continue
		}
		story, said, found := strings.Cut(words, " ")
		if !found || story == "" {
			continue
		}
		skip := RecheckSkip{At: at, Story: story, Said: said}
		// A story dispatched again says the same again: it is shown once, at its
		// newest time.
		for i, kept := range skips {
			if kept.Story == skip.Story && kept.Said == skip.Said {
				skips = append(skips[:i], skips[i+1:]...)
				break
			}
		}
		skips = append(skips, skip)
	}
	return skips
}

// Line is the skip as mw status and the mail show it.
func (s RecheckSkip) Line() string { return s.Story + " · " + s.Said }

// noteRecheckSkip keeps what a re-run of id's spec came to when it was not made,
// in the recheck log. A log that cannot be written is said in the report's notes.
func (d Dispatch) noteRecheckSkip(ctx context.Context, report *DispatchReport, id, said string) {
	if d.RecheckSkips == nil {
		return
	}
	if err := d.RecheckSkips.Append(ctx, recheckSkipLine(d.now(), id, said)); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("%s: the re-run not made could not be kept in the recheck log: %v", id, err))
	}
}

// RecheckSkipCheck is the mw doctor check that tells the Mayor a pre-dispatch
// re-run was not made: faulty while the recheck log holds a skip from the last
// 24 hours, its cure one mail, and a day between mails.
type RecheckSkipCheck struct {
	// Host is this host, named in the mail.
	Host string
	// Log is the recheck log the host's dispatch keeps.
	Log TickLog
	// Mail sends the Mayor the message.
	Mail func(ctx context.Context, message NewMessage) error
	// Now is the clock the window is judged by; nil is time.Now.
	Now func() time.Time

	found []RecheckSkip
}

var _ DoctorCheck = (*RecheckSkipCheck)(nil)
var _ DoctorCoolingQuiet = (*RecheckSkipCheck)(nil)

func (c *RecheckSkipCheck) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// Name implements DoctorCheck.
func (c *RecheckSkipCheck) Name() string { return RecheckSkipName }

// Probe implements DoctorCheck.
func (c *RecheckSkipCheck) Probe(ctx context.Context) (Verdict, string) {
	c.found = ReadRecheckSkips(ctx, c.Log, c.now())
	if len(c.found) == 0 {
		return DoctorOK, ""
	}
	lines := make([]string, len(c.found))
	for i, s := range c.found {
		lines[i] = s.Line()
	}
	return DoctorFaulty, fmt.Sprintf("%d %s not made in the last 24h: %s", len(c.found),
		plural(len(c.found), "re-run", "re-runs"), strings.Join(lines, "; "))
}

// Cure implements DoctorCheck: it mails the Mayor, the skips one line each.
func (c *RecheckSkipCheck) Cure(ctx context.Context) error {
	if len(c.found) == 0 {
		return fmt.Errorf("no skipped re-run was found to tell the Mayor of")
	}
	body := make([]string, 0, len(c.found)+2)
	for _, s := range c.found {
		body = append(body, s.At.UTC().Format(time.RFC3339)+" "+s.Line())
	}
	body = append(body, "", "mw dispatch re-runs a suite bug's spec on its rig's main before dispatching the bug; a re-run that cannot be made means a fixed bug is dispatched all the same. `mw status` shows the same lines under "+RecheckSkipsHeading+".")
	return c.Mail(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, c.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: %d on %s in the last 24h", MailRecheckSkipped, len(c.found), c.Host),
		Body:    strings.Join(body, "\n"),
	})
}

// Damper implements DoctorCheck: one mail a day, with no cap on the days.
func (c *RecheckSkipCheck) Damper() (time.Duration, int) { return RecheckSkipsWindow, 0 }

// QuietWhileCooling implements DoctorCoolingQuiet: the day between two mails is
// expected, and wakes nobody.
func (c *RecheckSkipCheck) QuietWhileCooling() bool { return true }

// WayBack implements DoctorCheck: nothing is changed but the mail.
func (c *RecheckSkipCheck) WayBack() string {
	return "none: the cure only mails the Mayor; make the re-run possible (mw dispatch prints why it was not made) and the check clears a day after the last skip"
}

package application

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ClosedEpicsName is the mw doctor check that names each closed epic with open
// children.
const ClosedEpicsName = "closed-epic-open-children"

// MailClosedEpics starts the subject of the mail that check sends.
const MailClosedEpics = "Open work under a closed epic"

// ClosedEpicsDay is the wait between two of that mail.
const ClosedEpicsDay = 24 * time.Hour

// ClosedEpic is a closed epic that still has stories not closed under it.
type ClosedEpic struct {
	ID string
	// Open is how many of its stories are not closed (held ones too: a held
	// story is off the map as much as a ready one).
	Open int
	// Reason is the epic's close reason, which may say where its work moved.
	Reason string
}

// Line is the epic as the check and its mail name it.
func (e ClosedEpic) Line() string {
	line := fmt.Sprintf("%s (%d open %s)", e.ID, e.Open, plural(e.Open, "child", "children"))
	if reason := oneLine(e.Reason); reason != "" {
		line += ", closed: " + reason
	}
	return line
}

// ClosedEpicsWithOpenChildren lists each closed epic with stories not closed
// under it, the epic with most first. Open children of a closed epic drop out
// of mw brief and the weighing by vision (mw-gq6.1720). An epic the tracker
// does not know is passed over.
func ClosedEpicsWithOpenChildren(ctx context.Context, stories TesterStories, tracker WorkTracker) ([]ClosedEpic, error) {
	all, err := stories.StoriesSince(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	open := map[string]int{}
	var parents []string
	for _, s := range all {
		if s.Closed() || s.IsEpic || s.EpicID == "" {
			continue
		}
		if open[s.EpicID] == 0 {
			parents = append(parents, s.EpicID)
		}
		open[s.EpicID]++
	}
	if len(parents) == 0 {
		return nil, nil
	}
	beads, err := tracker.ShowBeads(ctx, parents)
	if err != nil {
		return nil, err
	}
	var found []ClosedEpic
	for _, b := range beads {
		if b.Closed() {
			found = append(found, ClosedEpic{ID: b.Story.ID, Open: open[b.Story.ID], Reason: b.CloseReason})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Open != found[j].Open {
			return found[i].Open > found[j].Open
		}
		return found[i].ID < found[j].ID
	})
	return found, nil
}

// ClosedEpicsCheck is the mw doctor check that names each closed epic with open
// children: faulty while any is, its cure one mail a day to the Mayor, who moves
// the children to the epic the work moved to.
type ClosedEpicsCheck struct {
	// Host is this host, named in the mail.
	Host    string
	Stories TesterStories
	Tracker WorkTracker
	// Mail sends the Mayor the message.
	Mail func(ctx context.Context, message NewMessage) error

	found []ClosedEpic
}

var _ DoctorCheck = (*ClosedEpicsCheck)(nil)
var _ DoctorCoolingQuiet = (*ClosedEpicsCheck)(nil)
var _ DoctorStandsAfterCure = (*ClosedEpicsCheck)(nil)

// Name implements DoctorCheck.
func (c *ClosedEpicsCheck) Name() string { return ClosedEpicsName }

// Probe implements DoctorCheck.
func (c *ClosedEpicsCheck) Probe(ctx context.Context) (Verdict, string) {
	found, err := ClosedEpicsWithOpenChildren(ctx, c.Stories, c.Tracker)
	if err != nil {
		return DoctorCannotTell, fmt.Sprintf("the stories could not be read: %v", err)
	}
	c.found = found
	if len(found) == 0 {
		return DoctorOK, ""
	}
	lines := make([]string, len(found))
	for i, e := range found {
		lines[i] = e.Line()
	}
	return DoctorFaulty, fmt.Sprintf("%d closed %s with open children: %s", len(found), plural(len(found), "epic", "epics"), strings.Join(lines, "; "))
}

// CureLeavesFault implements DoctorStandsAfterCure: the mail moves no child.
func (c *ClosedEpicsCheck) CureLeavesFault() bool { return true }

// Cure implements DoctorCheck: one mail to the Mayor, an epic to a line.
func (c *ClosedEpicsCheck) Cure(ctx context.Context) error {
	if len(c.found) == 0 {
		return fmt.Errorf("no closed epic with open children was found to tell the Mayor of")
	}
	body := make([]string, 0, len(c.found)+2)
	for _, e := range c.found {
		body = append(body, e.Line())
	}
	body = append(body, "", "A story under a closed epic is off mw brief and the weighing by vision. Move each to the epic its work went to (bd update <id> --parent <epic>), or close it. mw next files a finder's bug under the nearest open epic itself.")
	return c.Mail(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, c.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: %d on %s", MailClosedEpics, len(c.found), c.Host),
		Body:    strings.Join(body, "\n"),
	})
}

// Damper implements DoctorCheck: one mail a day, with no cap on the days.
func (c *ClosedEpicsCheck) Damper() (time.Duration, int) { return ClosedEpicsDay, 0 }

// QuietWhileCooling implements DoctorCoolingQuiet.
func (c *ClosedEpicsCheck) QuietWhileCooling() bool { return true }

// WayBack implements DoctorCheck.
func (c *ClosedEpicsCheck) WayBack() string {
	return "none: the cure only mails the Mayor; move or close the children (bd update <id> --parent <epic>) and the check clears"
}

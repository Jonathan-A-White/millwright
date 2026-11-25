package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// IdleHostAfter is how long a host may have had room, with nothing started, before
// its dispatch tells the Mayor why (mw-gq6.1645): the sleep pass of 2026-11-22
// found four ready stories waiting on files, one running and six queued to land,
// and nothing had told the Mayor.
const IdleHostAfter = 15 * time.Minute

// MailIdle starts the subject of the mail that tells the Mayor a host with room
// has nothing to start.
const MailIdle = "Idle"

// IdleSpellKey is the note a host's dispatch keeps of its idle spell: the time
// of the first pass that had room and started nothing, RFC 3339, followed by
// " told" once the Mayor has been mailed for the spell.
func IdleSpellKey(host string) string { return "host." + host + ".idle_spell" }

const idleSpellTold = " told"

// waitingOnHimGates are the gates that pass a ready story over until the
// Mayor or the Governor does something: no session of this host can start it.
var waitingOnHimGates = []string{"hitl", "needs-files-key", "handed-back", "attempts-exhausted", "no-sweep", "tester-no-landed-story"}

// idleCounts are what a host that has room and starts nothing is waiting for.
type idleCounts struct {
	// files are the ready stories held on a file a running story names, and
	// holders says which file each waits on and who holds it, sorted and without
	// repeats.
	files   int
	holders []string
	// onHim are the ready stories that wait on the Mayor or the Governor.
	onHim int
	// thawable are the held stories past their reason: StuckReport.PastReasonCount,
	// the number mw stuck prints as 'past their reason'.
	thawable int
}

func (c idleCounts) none() bool { return c.files+c.onHim+c.thawable == 0 }

// idleCountsOf reads the counts off the pass: its Passed list for the gates, and
// the pass's shared-files holds for the file and holder. It leaves thawable to
// pastReason, which reads the tracker.
func idleCountsOf(passed []Passed, held filesHeldPass) idleCounts {
	var c idleCounts
	for _, p := range passed {
		switch {
		case p.Gate == "shared-files":
			c.files++
			if share, ok := held.shares[p.StoryID]; ok {
				holder := share.File + " held by " + share.With
				if share.Host != "" {
					holder += " on " + share.Host
				}
				if !slices.Contains(c.holders, holder) {
					c.holders = append(c.holders, holder)
				}
			}
		case slices.Contains(waitingOnHimGates, p.Gate):
			c.onHim++
		}
	}
	slices.Sort(c.holders)
	return c
}

// pastReason is how many held stories are past their reason, counted by the
// survey mw stuck runs (StuckSurvey.Find, StuckReport.PastReasonCount): one
// source for the mail and for mw stuck. A tracker that cannot read the graph, or
// a read that fails, counts none and says so as a note.
func (d Dispatch) pastReason(ctx context.Context, report *DispatchReport) int {
	graph, ok := d.Tracker.(BeadGraph)
	if !ok {
		return 0
	}
	found, err := StuckSurvey{Graph: graph, Comments: d.Tracker, Now: d.Now}.Find(ctx)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the holds past their reason could not be counted for the idle-host mail: %v", err))
		return 0
	}
	return found.PastReasonCount()
}

// idleHolderLines is how many files and holders the line names before it says
// how many more there are.
const idleHolderLines = 3

// line is the one line of counts the Mayor is mailed.
func (c idleCounts) line(host string, idle time.Duration) string {
	files := fmt.Sprintf("%d held on files", c.files)
	if len(c.holders) > 0 {
		named := c.holders
		more := ""
		if len(named) > idleHolderLines {
			named, more = named[:idleHolderLines], fmt.Sprintf("; +%d more", len(c.holders)-idleHolderLines)
		}
		files += " (" + strings.Join(named, "; ") + more + ")"
	}
	return fmt.Sprintf("%s has had room for %s and started nothing: %s, %d waiting on him, %d thawable",
		host, elapsed(idle), files, c.onHim, c.thawable)
}

// tellIdleHost mails the Mayor one line of counts when this host has had room for
// IdleHostAfter and nothing to start, once for the spell. Room is the room the
// pass read (noRoom is empty, and the cap leaves a free place): the same words
// the no-room and cap gates use. A pass that started a story or failed to, one
// that finds a harness hold or a give-way, or one with no room ends the spell. A
// spell is told only once it has something to count, and the mail is sent
// only when Mailbox and Memory are set and IdleHostMail is on; a dry run reads and writes
// nothing. A note or mail that cannot be written is a note on the report, and
// the next pass tries again.
func (d Dispatch) tellIdleHost(ctx context.Context, held filesHeldPass, noRoom string, free int, report *DispatchReport) {
	if !d.IdleHostMail || d.DryRun || d.Memory == nil || d.Mailbox == nil {
		return
	}
	key := IdleSpellKey(d.Host)
	kept, err := d.Memory.Note(ctx, key)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("this host's idle spell could not be read: %v", err))
		return
	}
	if noRoom != "" || free <= 0 || len(report.Started)+len(report.Failed) > 0 || report.HarnessHold != nil || report.GivesWay != nil {
		if kept != "" {
			if err := d.Memory.ClearNote(ctx, key); err != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("this host's idle spell could not be ended: %v", err))
			}
		}
		return
	}
	stamp, told := strings.CutSuffix(kept, idleSpellTold)
	since, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		since = d.now()
		if err := d.Memory.SetNote(ctx, key, since.UTC().Format(time.RFC3339)); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("this host's idle spell could not be kept: %v", err))
		}
		return
	}
	if told || d.now().Sub(since) < IdleHostAfter {
		return
	}
	counts := idleCountsOf(report.Passed, held)
	counts.thawable = d.pastReason(ctx, report)
	if counts.none() {
		return
	}
	idle := d.now().Sub(since)
	if _, err := d.Mailbox.Send(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, d.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: %s has had room for %s with nothing to start", MailIdle, d.Host, elapsed(idle)),
		Body:    counts.line(d.Host, idle) + "\n",
	}); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the idle-host mail to %s could not be sent: %v", MayorMailbox, err))
		return
	}
	if err := d.Memory.SetNote(ctx, key, since.UTC().Format(time.RFC3339)+idleSpellTold); err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the idle-host mail was sent, but that could not be remembered: %v", err))
	}
}

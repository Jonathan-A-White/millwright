package application

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// EndLostSessions is what a host does at each dispatch tick, once its tracker is
// level, with the sessions of stories it no longer holds (mw-6ww.185.10): a Boost
// that stopped answering had its claims given back by the home and the stories
// taken up by another host, and its sessions went on working them. A session of a
// story whose claim is now another host's is ended; its worktree is left for the
// sweep, which bundles what it holds. A session of a story still this host's, of
// one nobody holds, of a closed story and any session that is no story's are left
// as they are.
type EndLostSessions struct {
	Tracker  LostSessionStories
	Sessions SweepSessions
	// Host is this host's name.
	Host string
	// DryRun names what would be ended and closes nothing.
	DryRun bool
	// Out is where each ended session is said. A nil Out says nothing.
	Out io.Writer
}

// LostSessionStories is the part of the tracker the returning host reads its
// stories' claims through.
type LostSessionStories interface {
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)
}

// EndedSession is one session ended, or, on a dry run, to be ended.
type EndedSession struct {
	StoryID string
	Session string
	// HeldBy is the host that holds the story now.
	HeldBy string
}

// String is the line the tick says: 'ended: <story> is now held by <host>'.
func (e EndedSession) String() string {
	return fmt.Sprintf("ended: %s is now held by %s", e.StoryID, e.HeldBy)
}

// EndLostSessionsReport is what one pass did.
type EndLostSessionsReport struct {
	Ended []EndedSession
	// Notes are what could not be read or closed, and went on all the same.
	Notes []string
}

// Run ends this host's sessions of stories held elsewhere.
func (e EndLostSessions) Run(ctx context.Context) (EndLostSessionsReport, error) {
	var report EndLostSessionsReport
	if e.Tracker == nil || e.Sessions == nil || strings.TrimSpace(e.Host) == "" {
		return report, fmt.Errorf("ending the sessions of stories this host no longer holds: it needs a tracker, the host's sessions and its name")
	}
	live, err := e.Sessions.Sessions(ctx)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the sessions on %s could not be listed, so none was ended for a story held elsewhere: %v", e.Host, err))
		return report, nil
	}
	var ids []string
	storyOf := map[string]string{}
	asked := map[string]bool{}
	for _, session := range live {
		if id, ok := storyOfSession(session.Name); ok {
			storyOf[session.Name] = id
			if !asked[id] {
				asked[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return report, nil
	}
	beads, err := e.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the stories of the sessions on %s could not be read, so none was ended for a story held elsewhere: %v", e.Host, err))
		return report, nil
	}
	heldBy := map[string]string{}
	for _, bead := range beads {
		if bead.IsEpic || bead.Closed() {
			continue
		}
		if holder := ClaimHost(bead.Assignee); holder != "" && holder != e.Host {
			heldBy[bead.Story.ID] = holder
		}
	}
	for _, session := range live {
		id, ok := storyOf[session.Name]
		holder := heldBy[id]
		if !ok || holder == "" {
			continue
		}
		ended := EndedSession{StoryID: id, Session: session.Name, HeldBy: holder}
		if !e.DryRun {
			if err := e.Sessions.Close(ctx, session.Name); err != nil {
				report.Notes = append(report.Notes, fmt.Sprintf("the session %s of %s, now held by %s, could not be ended: %v", session.Name, id, holder, err))
				continue
			}
		}
		report.Ended = append(report.Ended, ended)
		if e.Out != nil {
			line := ended.String()
			if e.DryRun {
				line = "would end" + strings.TrimPrefix(line, "ended")
			}
			fmt.Fprintln(e.Out, line)
		}
	}
	return report, nil
}

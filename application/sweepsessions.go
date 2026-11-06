package application

import (
	"context"
	"fmt"
	"strings"
)

// HostSession is one tmux session on a host, as the sweep sees it: its name,
// and whether its pane is idle — the command in it has ended, or what is in
// the foreground is a shell waiting for a line, so that nothing is lost by
// closing it. A session with a pane at work (a Builder's harness, a gate, a
// merge) is not idle.
type HostSession struct {
	Name string
	Idle bool
}

// SweepSessions is the part of a session runner the sweep closes finished
// stories' sessions through: every session on the host, and Runner's own Close.
// A host with no tmux, or no sessions, lists none and is no failure.
type SweepSessions interface {
	Sessions(ctx context.Context) ([]HostSession, error)
	Close(ctx context.Context, name string) error
}

// storyOfSession is the id of the story a session of that name may be the
// session of: SessionName turns every character an id cannot have in a
// session's name into an underscore, and a story's id has only its dots to
// turn, so the way back is one underscore to one dot. A name with no
// underscore is no story's (a seat's session, a tester's, whisper-server). A
// session a close-out sent back to be rebased carries asideSuffix after the
// story's own name, and is the story's all the same. False when the name is
// no story's.
func storyOfSession(name string) (string, bool) {
	name = strings.TrimSuffix(name, SessionName(asideSuffix))
	if !strings.HasPrefix(name, "mw-") || !strings.Contains(name, "_") {
		return "", false
	}
	id := strings.ReplaceAll(name, "_", ".")
	return id, SessionName(id) == name
}

// sessions closes, on this host, the idle session of every story the tracker
// holds closed — landed and verified — and says what it kept: the session of a
// closed story whose pane is not idle, and (by not naming them) every session
// of a story still open, claimed or running, and every session that is no
// story's. A session it cannot close is noted and the others go on.
func (s Sweep) sessions(ctx context.Context, report *SweepReport) {
	if s.Sessions == nil {
		return
	}
	live, err := s.Sessions.Sessions(ctx)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the sessions on %s could not be listed: %v", s.Host, err))
		return
	}
	report.sessionsAsked, report.SessionsSeen = true, len(live)

	var ids []string
	asked := map[string]bool{}
	storyOf := map[string]string{}
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
		return
	}
	beads, err := s.Tracker.ShowBeads(ctx, ids)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the stories of the sessions on %s could not be read: %v", s.Host, err))
		return
	}
	closed := map[string]bool{}
	for _, bead := range beads {
		if bead.Closed() && !bead.IsEpic {
			closed[bead.Story.ID] = true
		}
	}

	for _, session := range live {
		id, ok := storyOf[session.Name]
		if !ok || !closed[id] {
			continue
		}
		if !session.Idle {
			report.SessionsKept = append(report.SessionsKept, session.Name)
			continue
		}
		if err := s.Sessions.Close(ctx, session.Name); err != nil {
			report.Notes = append(report.Notes, fmt.Sprintf("the session %s of closed %s could not be closed: %v", session.Name, id, err))
			continue
		}
		report.SessionsClosed = append(report.SessionsClosed, session.Name)
	}
}

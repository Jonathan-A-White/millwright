package application

import (
	"fmt"

	"github.com/Jonathan-A-White/millwright/domain"
)

// Launch is one story's session, described in the way every harness needs it
// and none of them owns: the story it works, the Path it is worked by, the seat
// it boots into and the host it boots on, the worktree it works in, the boot
// file it is primed with, the first thing it is told, and the file its result
// must be left in.
//
// Everything in a Launch is ready before a harness sees it: the boot file is
// written and the directory the result belongs in exists.
type Launch struct {
	StoryID    string
	Path       domain.Path
	Seat       string
	Host       string
	Dir        string
	BootFile   string
	Kickoff    string
	ResultFile string

	// After is the command run when the harness exits, however it exits: mw
	// closing the story out. It is chained onto the harness's own command line
	// rather than hung off a hook, because a session that dies fires no hooks
	// and a story nobody closes out is a story nobody hears about again (ADR
	// 0004). An empty After runs nothing afterwards.
	After []string

	// Heartbeat is the command that renews this story's claim's lease for as
	// long as the harness is running: started in the background before the
	// harness and killed the moment the harness exits, before After runs. It
	// is chained into the same command line as After is, for the same reason
	// — nothing else is running once the harness has died to start it any
	// other way. An empty Heartbeat starts nothing beside the harness.
	Heartbeat []string
}

// Validate reports the first reason a launch could not be turned into a
// session.
func (l Launch) Validate() error {
	switch {
	case l.StoryID == "":
		return fmt.Errorf("a launch needs a story")
	case l.Seat == "":
		return fmt.Errorf("launching %s: a session boots into a seat", l.StoryID)
	case l.BootFile == "":
		return fmt.Errorf("launching %s: a session is primed from a boot file", l.StoryID)
	case l.ResultFile == "":
		return fmt.Errorf("launching %s: a session's result has nowhere to go", l.StoryID)
	case l.Kickoff == "":
		return fmt.Errorf("launching %s: a session needs to be told what to do", l.StoryID)
	}
	return l.Path.Validate()
}

// Harness is the port a Launch becomes a runnable session through: the
// harness's command line, its environment and the directory it runs in. One
// adapter speaks Claude Code; a story's Path says which harness it wants, and
// the factory picks the adapter that answers to that name.
//
// A Harness assembles and nothing more: it starts no session and spends no
// fuel. What it returns is a SessionSpec, which a Runner starts.
type Harness interface {
	// Name is the harness this adapter is, as a Path names it.
	Name() domain.Harness

	// Session is the session that works the launch. It fails if the launch is
	// incomplete or asks for a harness this adapter is not.
	Session(l Launch) (SessionSpec, error)
}

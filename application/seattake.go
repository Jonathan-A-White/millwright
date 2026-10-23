package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// SessionEnv is the variable Claude Code sets in a session's environment to
// its session id: how `mw seat take` learns the id of the session it runs in.
const SessionEnv = "CLAUDE_CODE_SESSION_ID"

// SeatTakeTerminal is the port `mw seat take` finds its window and names it
// through: the window the calling process runs in, and a window's new name.
type SeatTakeTerminal interface {
	// ThisWindow reports the window the calling process is running in, false
	// when it is running in none.
	ThisWindow(ctx context.Context) (ReapWindow, bool, error)

	// Rename calls the window, by id, name from now on.
	Rename(ctx context.Context, window, name string) error
}

// SeatTake writes a seat's acting file for the session that took the seat
// without a handover — one that bin/mayor-up started, or that sat down after a
// timeout — so that no session writes it by hand. A hand-written file named a
// Mayor but not its window or its session, and the doctor's mayor-gone probe,
// finding no open window by the name, reported the Mayor gone every five
// minutes (mw-gq6.708).
//
// The file it writes names the seat's session by name, the id of the window it
// runs in, and its session id, in the forms the probe looks for. bin/mayor-up
// names a window from the handoff it booted from, not from the Mayor in it, so
// a name given with Name renames the window to it.
type SeatTake struct {
	Seat     string
	Terminal SeatTakeTerminal
	Acting   ActingFile
	// Session is the id of the session running the command, empty when the
	// environment did not say.
	Session string
	Now     func() time.Time
	Out     io.Writer
}

// SeatTakeRequest is what one take is asked to do. Name is the name the
// session and its window are to have, which must begin with the seat's name and
// a hyphen; empty keeps the window's own name. Note is free text kept in the
// acting file for whoever reads it.
type SeatTakeRequest struct {
	Name string
	Note string
}

// Run takes the seat: the acting file is written last, so a refusal or a
// failure to rename leaves it as it was.
func (t SeatTake) Run(ctx context.Context, req SeatTakeRequest) error {
	if t.Seat == "" || t.Terminal == nil || t.Acting == nil || t.Now == nil {
		return fmt.Errorf("taking a seat needs a seat, its terminal and its acting file")
	}
	if strings.TrimSpace(t.Session) == "" {
		return fmt.Errorf("the session id is not known (the harness sets $%s in a session's environment): "+
			"run this inside the %s's own session, so the acting file can name it", SessionEnv, t.Seat)
	}
	here, in, err := t.Terminal.ThisWindow(ctx)
	if err != nil {
		return err
	}
	if !in {
		return fmt.Errorf("this is not running in a tmux window, so there is no window for the acting file to name: " +
			"run it in the pane of the session that takes the seat")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = here.Name
		if !strings.HasPrefix(name, t.Seat+"-") {
			return fmt.Errorf("this window is called %q, which is not a %s window (its name must begin with %s-): "+
				"pass --as <name> to call it by the name of this session", here.Name, t.Seat, t.Seat)
		}
	}
	if !strings.HasPrefix(name, t.Seat+"-") || strings.ContainsAny(name, " \t\r\n'\"") {
		return fmt.Errorf("the name %q is not one a %s session can take: it must begin with %s- and hold no space or quote",
			name, t.Seat, t.Seat)
	}

	if name != here.Name {
		if err := t.Terminal.Rename(ctx, here.ID, name); err != nil {
			return fmt.Errorf("renaming window %s to %s: %w", here.ID, name, err)
		}
	}
	if err := t.Acting.WriteActing(ctx, t.Seat, SeatTakeText(t.Seat, name, here.ID, t.Session, req.Note, t.Now())); err != nil {
		return fmt.Errorf("window %s is now called %s, but the %s seat's acting file was not written: %w", here.ID, name, t.Seat, err)
	}
	if t.Out != nil {
		fmt.Fprintf(t.Out, "%s holds the %s seat: window %s (%s), session %s. "+
			"The acting file names all three, so mw doctor mayor-gone finds this window.\n",
			name, t.Seat, here.ID, name, t.Session)
	}
	return nil
}

// SeatTakeText is what a take writes into the seat's acting file: the seat's
// session by name, the id of its window and its session id (the last in the
// form `session <id>` that bin/mayor-context reads), the time, and the note
// if there is one.
func SeatTakeText(seat, name, window, session, note string, now time.Time) string {
	text := fmt.Sprintf("%s %s (taken by hand; window %s, session %s), since %s",
		strings.ToUpper(seat[:1])+seat[1:], name, window, session, now.UTC().Format(time.RFC3339))
	if note = strings.Join(strings.Fields(note), " "); note != "" {
		text += "; " + note
	}
	return text + "\n"
}

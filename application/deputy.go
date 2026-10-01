package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// DeputySeat is the seat `mw deputy` brings up, and the prefix of the name of
// every one of its windows: a window named deputy-<date>-<nn> is the Deputy's.
const DeputySeat = "deputy"

// DeputyUpExit is the status mw leaves with when it started nothing because a
// Deputy is already up. It is a status of its own so that the Mayor, reading
// nothing but the number, can tell "already up: mail it and go" from a failure.
const DeputyUpExit = 8

// DeputyEffort is how hard the Deputy is asked to think.
const DeputyEffort = domain.EffortHigh

// DeputyMailbox is the mailbox the Deputy's events wait is armed on.
const DeputyMailbox = "deputy"

// DeputyNudgeFormat is the line typed into an idle Deputy's window when its box
// holds unread mail: the words the Mayor's notifier (contrib/mail-notify) types
// for its own box, naming the Deputy. Type presses Enter of its own.
const DeputyNudgeFormat = "New mail for " + DeputyMailbox + ": %d message(s). Run bd mail inbox."

// DeputyAlreadyUp is a Deputy bring-up that started nothing because a window of
// the Deputy's is already open. It is not a fault: it carries DeputyUpExit and
// says so in one line. Busy says the pane was at work and unread mail was left
// waiting for it.
type DeputyAlreadyUp struct {
	Window string
	Busy   bool
}

// Error is the one line that says nothing was started and why.
func (u *DeputyAlreadyUp) Error() string {
	if u.Busy {
		return fmt.Sprintf("the Deputy is busy in window %s; the mail waits", u.Window)
	}
	return fmt.Sprintf("the Deputy is already up in the window %s: nothing was started", u.Window)
}

// DeputyIsUp reports whether err is a bring-up that found the Deputy already
// up, and in which window.
func DeputyIsUp(err error) (*DeputyAlreadyUp, bool) {
	var up *DeputyAlreadyUp
	return up, errors.As(err, &up)
}

// Deputy brings up the Deputy: `mw seat up deputy`, on the model config names,
// at high effort, with the reaper armed to close the window once the session
// has handed off. The kickoff is told to arm mw events wait on the Deputy's own
// mailbox, work what comes, report by mail and hand off when idle, and then the
// reason it was brought up.
//
// It starts nothing, and says so with DeputyAlreadyUp, when a window of the
// Deputy's is already open: the Mayor mails a Deputy that is up and fires one
// only when none is. Given a Terminal and a Mail, an open window whose pane is
// idle at an empty input line while its box holds unread mail is typed the
// mail nudge instead, and Run says it nudged; a pane at work is left alone and
// the refusal says the mail waits.
type Deputy struct {
	Seats    SeatFiles
	Windows  Windows
	Harness  SeatHarness
	Terminal ReapTerminal
	Armer    ReapArmer

	// Mail is the box the Deputy's unread mail is counted in, to wake an idle
	// Deputy with. Without it a Deputy that is up is only said to be.
	Mail Mailbox

	// Host is the host the Deputy is brought up on.
	Host string

	// Reason is why, told to the session after its standing instructions.
	Reason string

	// Model is what the Deputy runs on.
	Model domain.Model

	// Now is the clock the window's date is taken from; nil is time.Now.
	Now func() time.Time

	// Out is where what seat up says is printed. A nil Out prints nothing.
	Out io.Writer
}

// DeputyStanding is what the Deputy's kickoff is always told, ahead of the
// reason it was brought up.
const DeputyStanding = "Standing instructions: arm `mw events wait --for " + DeputyMailbox + " --kinds mail`" +
	" in the background (it blocks at no cost until mail for the Deputy arrives) so that mail wakes you; work the mail; report by mail, never by typing " +
	"into another seat's window; and hand off when you are idle."

// Run brings the Deputy up, or says that one is. Every refusal happens before
// anything is opened.
func (d Deputy) Run(ctx context.Context) (SeatUpReport, error) {
	if d.Seats == nil || d.Windows == nil {
		return SeatUpReport{}, fmt.Errorf("bringing up the Deputy needs its files and a terminal to look for its window in")
	}
	up, err := seatWindowOpen(ctx, d.Windows, DeputySeat)
	if err != nil {
		return SeatUpReport{}, err
	}
	if up != "" {
		return d.wakeIdle(ctx, up)
	}

	told := DeputyStanding
	if reason := strings.TrimSpace(d.Reason); reason != "" {
		told += " Woken for: " + reason
	}
	return SeatUp{
		Seats:   d.Seats,
		Windows: d.Windows,
		Harness: d.Harness,
		Seat:    DeputySeat,
		Host:    d.Host,
		Model:   d.Model,
		Effort:  DeputyEffort,
		Reason:  told,
		Now:     d.Now,
		Out:     d.Out,

		Terminal:     d.Terminal,
		Armer:        d.Armer,
		ReapWhenIdle: true,
	}.Run(ctx)
}

// wakeIdle is what Run does with a Deputy that is up. When its box holds unread
// mail and its pane sits idle at an empty input line, it types the nudge and
// says so; when the pane is not idle it leaves it alone and says the mail
// waits. Anything it cannot tell — no terminal or mail wired, no mail, a pane
// or box that cannot be read — is the plain "already up" refusal it always was.
func (d Deputy) wakeIdle(ctx context.Context, name string) (SeatUpReport, error) {
	up := &DeputyAlreadyUp{Window: name}
	if d.Terminal == nil || d.Mail == nil {
		return SeatUpReport{}, up
	}
	open, err := d.Terminal.OpenWindows(ctx)
	if err != nil {
		return SeatUpReport{}, up
	}
	id := ""
	for _, window := range open {
		if window.Name == name {
			id = window.ID
		}
	}
	if id == "" {
		return SeatUpReport{}, up
	}
	inbox, err := d.Mail.Inbox(ctx, DeputyMailbox)
	if err != nil || len(inbox) == 0 {
		return SeatUpReport{}, up
	}
	state, err := d.Terminal.PaneState(ctx, id)
	if err != nil {
		return SeatUpReport{}, up
	}
	if state != PaneIdle {
		return SeatUpReport{}, &DeputyAlreadyUp{Window: name, Busy: true}
	}
	if err := d.Terminal.Type(ctx, id, fmt.Sprintf(DeputyNudgeFormat, len(inbox))); err != nil {
		return SeatUpReport{}, fmt.Errorf("typing the mail nudge into the window %s: %w", name, err)
	}
	if d.Out != nil {
		fmt.Fprintf(d.Out, "nudged the Deputy in window %s\n", name)
	}
	return SeatUpReport{}, nil
}

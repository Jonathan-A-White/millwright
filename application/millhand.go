package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// MillhandSeat is the seat `mw millhand` brings up, and the prefix of the name
// of every one of its windows: a window named millhand-<date>-<nn> is the
// Millhand's.
const MillhandSeat = "millhand"

// Wake is the kind of wake the Millhand is brought up for: by hand, when the
// Governor is here; by routine, when something on the host needs it; or for a
// review of what happened since the last one. What it is decides the model the
// session runs on and what the session is told.
type Wake string

const (
	WakeHand    Wake = "hand"
	WakeRoutine Wake = "routine"
	WakeReview  Wake = "review"
)

// ReviewMarkFileName is the file, beside the Millhand's handoffs on a host, that
// says how far its last review reached. mw only reads it: the Millhand moves it
// in its handoff.
const ReviewMarkFileName = "review-since"

// MillhandUpExit is the status mw leaves with when it started nothing because a
// Millhand is already up. It is a status of its own so that a timer reading
// nothing but the number can tell "already up" from a failure.
const MillhandUpExit = 5

// MillhandEffort is how hard every wake asks the Millhand to think.
const MillhandEffort = domain.EffortHigh

// AlreadyUp is a Millhand wake that started nothing because a window of the
// Millhand's is already open on this host. It is not a fault, and a timer should
// not treat it as one: it carries MillhandUpExit and says so in one line.
type AlreadyUp struct {
	Window string
}

// Error is the one line that says nothing was started and why.
func (u *AlreadyUp) Error() string {
	return fmt.Sprintf("the Millhand is already up in the window %s: nothing was started", u.Window)
}

// MillhandIsUp reports whether err is a wake that found the Millhand already up,
// and in which window.
func MillhandIsUp(err error) (*AlreadyUp, bool) {
	var up *AlreadyUp
	return up, errors.As(err, &up)
}

// Millhand brings up this host's Millhand: `mw seat up millhand`, on the model
// the kind of wake calls for, at high effort, with the reaper armed to close the
// window once the session has handed off. The kickoff is told the kind of wake
// and the reason for it, and for a review wake the review mark, when the seat
// keeps one.
//
// It starts nothing, and says so with AlreadyUp, when a window of the
// Millhand's is already open: a timer that wakes it every so often must not
// pile a second session on the first.
type Millhand struct {
	Seats    SeatFiles
	Windows  Windows
	Harness  SeatHarness
	Terminal ReapTerminal
	Armer    ReapArmer

	// Host is the host the Millhand is brought up on.
	Host string

	// Wake is the kind of wake, WakeHand when empty. Reason is why, told to the
	// session after the kind.
	Wake   Wake
	Reason string

	// RoutineModel is what a routine wake and a wake by hand run on, and
	// ReviewModel what a review wake runs on.
	RoutineModel domain.Model
	ReviewModel  domain.Model

	// Now is the clock the window's date is taken from; nil is time.Now.
	Now func() time.Time

	// Out is where what seat up says is printed. A nil Out prints nothing.
	Out io.Writer
}

// MillhandWindow is the name of the window of the Millhand's that is open in the
// terminal, or "" when there is none. The Millhand is up when there is one.
func MillhandWindow(ctx context.Context, windows Windows) (string, error) {
	open, err := windows.List(ctx)
	if err != nil {
		return "", err
	}
	for _, window := range open {
		if strings.HasPrefix(window.Name, MillhandSeat+"-") {
			return window.Name, nil
		}
	}
	return "", nil
}

// Run brings the Millhand up, or says that one is. Every refusal happens before
// anything is opened.
func (m Millhand) Run(ctx context.Context) (SeatUpReport, error) {
	if m.Seats == nil || m.Windows == nil {
		return SeatUpReport{}, fmt.Errorf("waking the Millhand needs its files and a terminal to look for its window in")
	}
	wake := m.Wake
	if wake == "" {
		wake = WakeHand
	}
	model, err := m.modelFor(wake)
	if err != nil {
		return SeatUpReport{}, err
	}

	up, err := MillhandWindow(ctx, m.Windows)
	if err != nil {
		return SeatUpReport{}, err
	}
	if up != "" {
		return SeatUpReport{}, &AlreadyUp{Window: up}
	}

	told, err := m.told(ctx, wake)
	if err != nil {
		return SeatUpReport{}, err
	}
	return SeatUp{
		Seats:   m.Seats,
		Windows: m.Windows,
		Harness: m.Harness,
		Seat:    MillhandSeat,
		Host:    m.Host,
		Model:   model,
		Effort:  MillhandEffort,
		Reason:  told,
		Now:     m.Now,
		Out:     m.Out,

		Terminal:     m.Terminal,
		Armer:        m.Armer,
		ReapWhenIdle: true,
	}.Run(ctx)
}

// modelFor is the model a kind of wake runs on, or a refusal naming the kind if
// mw does not know it.
func (m Millhand) modelFor(wake Wake) (domain.Model, error) {
	switch wake {
	case WakeHand, WakeRoutine:
		return m.RoutineModel, nil
	case WakeReview:
		return m.ReviewModel, nil
	}
	return "", fmt.Errorf("%q is not a kind of wake: the Millhand is woken %s, %s or %s", wake, WakeHand, WakeRoutine, WakeReview)
}

// told is what the kickoff is told after its own text: the kind of wake, the
// reason for it, and for a review wake what the review mark says. Without a mark
// a review wake is told nothing about one.
func (m Millhand) told(ctx context.Context, wake Wake) (string, error) {
	kind := map[Wake]string{
		WakeHand:    "a wake by hand",
		WakeRoutine: "a routine wake",
		WakeReview:  "a review wake",
	}[wake]

	told := kind
	if reason := strings.TrimSpace(m.Reason); reason != "" {
		told += ": " + reason
	}
	if wake != WakeReview {
		return told, nil
	}

	mark, err := m.Seats.HostFile(ctx, MillhandSeat, m.Host, ReviewMarkFileName)
	if err != nil {
		return "", err
	}
	if mark = strings.TrimSpace(mark); mark != "" {
		told += fmt.Sprintf(". The review mark (%s) says: %s",
			path.Join(SeatsDir, MillhandSeat, SeatHostsDir, m.Host, ReviewMarkFileName), mark)
	}
	return told, nil
}

package application

import (
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The parts of a seat `mw seat up` starts a session from, by name: the charter
// it boots into, the seat's own kickoff text when it keeps one, and the
// directory its handoffs are written in — under hosts/<host>/ for a seat whose
// work is per host, and beside the charter for a seat whose work is not.
const (
	CharterFileName = "charter.md"
	KickoffFileName = "kickoff.md"
	HandoffsDir     = "handoffs"
	SeatHostsDir    = "hosts"
	HandoffExt      = ".md"
)

// ActingFileName is the file a seat's live session writes to say it holds the
// seat: `.<seat>-acting` in the vault, host-local and untracked. mw never
// writes it — the session does, at boot, and that is the hand-over signal —
// and mw only ever reads it, to see whether the seat is taken.
func ActingFileName(seat string) string {
	return "." + seat + "-acting"
}

// Handoff is one handoff a seat has written: the file its successor is booted
// from. The name is what a seat numbers its handoffs by — 2026-09-19-12 — and
// the number at the end of it is what the next session's window is numbered
// past.
type Handoff struct {
	// Name is the file's name without its extension.
	Name string
	// Path is where it is, from the vault's root, which is where the session
	// that reads it runs.
	Path string
	// Number is the number the name ends with, 0 when it ends with none.
	Number int
	// Written is when the file was last written.
	Written time.Time
}

// SeatStart is what a seat's next session on this host is started from, as the
// vault holds it. A part the seat does not have reads as empty rather than as
// an error: which of them is worth refusing over is the use case's to say, not
// the vault's.
type SeatStart struct {
	Seat string
	// Dir is the vault on this host: where the session runs.
	Dir string
	// Charter is the full path of the seat's charter, empty when it has none.
	// It is a path and not the text because the session is primed from the file
	// itself, so that no charter has to survive a command line.
	Charter string
	// Kickoff is the seat's own kickoff text, empty when it keeps none.
	Kickoff string
	// HandoffDir is the directory the handoffs were read from, from the vault's
	// root, whether or not any are there.
	HandoffDir string
	// Handoffs is what the seat has written there, oldest name first.
	Handoffs []Handoff
	// Acting is what the seat's acting file says, empty when there is none. It
	// is the live session's own words, so it is searched for the name of a
	// window rather than read as one.
	Acting string
}

// SeatFiles is the port a seat's own files are read through: everything
// starting its next session takes from the vault, and nothing else the vault
// holds. One adapter is the vault directory on disk.
type SeatFiles interface {
	// SeatStart reads what a seat's next session on this host is started from.
	// A seat with no charter, no handoff or no acting file is not an error
	// here: what is missing comes back empty.
	SeatStart(ctx context.Context, seat, host string) (SeatStart, error)
}

// Window is one window of the terminal this host's seats run in.
type Window struct {
	Name string
	// Opened is when the window was opened, zero when the terminal could not
	// say. A window nothing can date is treated as older than any handoff:
	// refusing to start a seat takes a fact, not the absence of one.
	Opened time.Time
}

// WindowSpec is one interactive window to open: what to call it, where to run,
// what environment to run with, and the command to run. Command is an argv —
// the program and its arguments, not a line for a shell to read.
//
// It is not a SessionSpec: a SessionSpec is a session of its own with nobody
// at the keyboard, and this is a window beside the ones a person already has
// open, holding a session they can type into.
type WindowSpec struct {
	Name    string
	Dir     string
	Env     map[string]string
	Command []string
}

// Validate reports the first reason a window could not be opened.
func (w WindowSpec) Validate() error {
	switch {
	case w.Name == "":
		return fmt.Errorf("a window needs a name")
	case strings.ContainsAny(w.Name, " \t:."):
		return fmt.Errorf("a window cannot be named %q: a space, a colon or a dot is how a terminal reads one name from the next", w.Name)
	case len(w.Command) == 0:
		return fmt.Errorf("window %q needs a command to run", w.Name)
	}
	return nil
}

// Windows is the port a seat's own session is opened through: the terminal
// this host's seats run in, one window a session, each window holding a
// session a person can watch and type into.
//
// It is not the Runner. A Runner session is unattended work with a result
// file; a window here is a seat's live session, started beside whatever else
// the person has open and left running when mw exits.
type Windows interface {
	// Open runs spec's command in a new window of that terminal, making the
	// terminal itself when there is none yet. It returns once the window is
	// open: what runs in it outlives mw.
	Open(ctx context.Context, spec WindowSpec) error

	// List reports every window open in that terminal, each with when it was
	// opened — which says both whether a seat's window is still there and how
	// old it is. A terminal that is not there yet holds no windows and is not
	// an error.
	List(ctx context.Context) ([]Window, error)
}

// SeatLaunch is a seat's own session, described the way every harness needs it
// and none of them owns: which seat it boots into, what the session and its
// window are called, where it runs, the charter file it is primed from, the
// model and effort it runs at, and the first thing it is told.
//
// Model and Effort may be empty, and then the harness is left to its own.
type SeatLaunch struct {
	Seat    string
	Name    string
	Dir     string
	Charter string
	Model   domain.Model
	Effort  domain.Effort
	Kickoff string
}

// Validate reports the first reason a seat launch could not be turned into a
// window.
func (l SeatLaunch) Validate() error {
	switch {
	case l.Seat == "":
		return fmt.Errorf("a seat's session boots into a seat")
	case l.Name == "":
		return fmt.Errorf("starting the %s seat: its session needs a name", l.Seat)
	case l.Charter == "":
		return fmt.Errorf("starting the %s seat: a session is primed from a charter", l.Seat)
	case l.Kickoff == "":
		return fmt.Errorf("starting the %s seat: a session needs to be told what to do", l.Seat)
	}
	return nil
}

// SeatHarness is the port a seat's own session is assembled through: the
// window that runs it. It is the interactive twin of Harness, which assembles
// the unattended session that works one story.
type SeatHarness interface {
	// SeatSession is the window that runs the launch. It fails if the launch
	// is incomplete.
	SeatSession(l SeatLaunch) (WindowSpec, error)
}

// SeatUp starts a seat's next session: one interactive session in a window of
// its own, primed with the seat's charter and told to carry on from the seat's
// newest handoff. Nothing else the vault holds is read or passed — a session
// that wants its ledger, its memories or its work can ask for them itself, and
// a session primed with them pays for every line at boot (ADR 0003).
//
// It refuses, and starts nothing, when there is no charter to boot into, no
// handoff to boot from, or the seat is already held: its acting file names a
// window that is still open and it has written no handoff since that window
// was opened.
type SeatUp struct {
	Seats   SeatFiles
	Windows Windows
	Harness SeatHarness

	// Seat is the seat to start, and Host the host it is started on: which of
	// the seat's handoffs are its own.
	Seat string
	Host string

	// Model and Effort are what the session runs at, empty for the harness's
	// own default.
	Model  domain.Model
	Effort domain.Effort

	// Reason is why the session is being started, told to it after the
	// kickoff. Empty says nothing.
	Reason string

	// Now is the clock the window's date is taken from; nil is time.Now.
	Now func() time.Time

	// Out is where the line is printed. A nil Out prints nothing.
	Out io.Writer
}

// SeatUpReport is what starting a seat did.
type SeatUpReport struct {
	Seat string
	// Window is what the new session's window is called.
	Window string
	// Handoff is the handoff it was booted from, from the vault's root.
	Handoff string
}

// String is the report as `mw seat up` prints it: one line.
func (r SeatUpReport) String() string {
	return fmt.Sprintf("started the %s seat in the window %s, booting from %s", r.Seat, r.Window, r.Handoff)
}

// Run starts the seat's next session and reports what it started. Every
// refusal happens before anything is opened, so a refused seat up has changed
// nothing at all.
func (s SeatUp) Run(ctx context.Context) (SeatUpReport, error) {
	switch {
	case s.Seats == nil || s.Windows == nil || s.Harness == nil:
		return SeatUpReport{}, fmt.Errorf("starting a seat needs its files, a terminal to open a window in and a harness")
	case s.Seat == "":
		return SeatUpReport{}, fmt.Errorf("which seat is to be started?")
	case !plainSeatName(s.Seat):
		return SeatUpReport{}, fmt.Errorf("%q is not a seat: a seat is named in letters, digits, dashes and underscores", s.Seat)
	}

	start, err := s.Seats.SeatStart(ctx, s.Seat, s.Host)
	if err != nil {
		return SeatUpReport{}, err
	}
	if start.Charter == "" {
		return SeatUpReport{}, fmt.Errorf("the %s seat has no %s in %s: there is nothing to boot into",
			s.Seat, CharterFileName, path.Join(start.Dir, SeatsDir, s.Seat))
	}
	if len(start.Handoffs) == 0 {
		return SeatUpReport{}, fmt.Errorf("the %s seat has written no handoff in %s: there is nothing to boot from",
			s.Seat, path.Join(start.Dir, start.HandoffDir))
	}
	newest := start.Handoffs[len(start.Handoffs)-1]

	open, err := s.Windows.List(ctx)
	if err != nil {
		return SeatUpReport{}, err
	}
	if held, ok := actingWindow(start.Acting, open); ok && !handedOffSince(start.Handoffs, held.Opened) {
		return SeatUpReport{}, fmt.Errorf("the %s seat is already acting in the window %s, and has written no handoff since it was opened: "+
			"close that window, or hand off from it, before starting another", s.Seat, held.Name)
	}

	spec, err := s.Harness.SeatSession(SeatLaunch{
		Seat:    s.Seat,
		Name:    s.windowName(start.Handoffs, open),
		Dir:     start.Dir,
		Charter: start.Charter,
		Model:   s.Model,
		Effort:  s.Effort,
		Kickoff: SeatKickoff(s.Seat, start.Kickoff, newest.Path, s.Reason),
	})
	if err != nil {
		return SeatUpReport{}, err
	}
	if err := s.Windows.Open(ctx, spec); err != nil {
		return SeatUpReport{}, err
	}

	report := SeatUpReport{Seat: s.Seat, Window: spec.Name, Handoff: newest.Path}
	if s.Out != nil {
		fmt.Fprintln(s.Out, report)
	}
	return report, nil
}

// windowName is what the new session's window is called: the seat, today's
// date in UTC, and a number one past the highest the seat has used — for a
// handoff or for a window already open. The two are counted together because
// they are the same count: one session, one handoff, one window.
func (s SeatUp) windowName(handoffs []Handoff, open []Window) string {
	highest := 0
	for _, handoff := range handoffs {
		if handoff.Number > highest {
			highest = handoff.Number
		}
	}
	for _, window := range open {
		if number, ok := seatWindowNumber(s.Seat, window.Name); ok && number > highest {
			highest = number
		}
	}
	return fmt.Sprintf("%s-%s-%02d", s.Seat, s.now().UTC().Format(time.DateOnly), highest+1)
}

func (s SeatUp) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// SeatKickoff is the first thing a seat's session is told: the seat's own
// kickoff text when it keeps one, else a default that sends it to its charter
// and its newest handoff. Either way it ends with the handoff to boot from and
// why the session was started, which are the two things no file in the vault
// can know in advance.
func SeatKickoff(seat, own, handoff, reason string) string {
	told := strings.TrimSpace(own)
	if told == "" {
		told = fmt.Sprintf("You are booting into the %s seat of millwright. Your charter is in your system prompt. "+
			"Boot by it: read the newest handoff named below, and carry on from where your predecessor left off.", seat)
	}
	if handoff != "" {
		told += fmt.Sprintf(" Newest handoff: %s", handoff)
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		told += fmt.Sprintf(" You were started now for this reason: %s", reason)
	}
	return told
}

// actingWindow is the window a seat's acting file names, if it is still open.
// The acting file is written by the session itself, in its own words, so it is
// searched for the name of an open window rather than read as one; the longest
// name that occurs in it wins, so that a window whose name ends in another's
// cannot be mistaken for it.
func actingWindow(acting string, open []Window) (Window, bool) {
	if strings.TrimSpace(acting) == "" {
		return Window{}, false
	}
	named := make([]Window, len(open))
	copy(named, open)
	sort.SliceStable(named, func(i, j int) bool { return len(named[i].Name) > len(named[j].Name) })
	for _, window := range named {
		if window.Name != "" && strings.Contains(acting, window.Name) {
			return window, true
		}
	}
	return Window{}, false
}

// handedOffSince reports whether any handoff was written after a window was
// opened, which is what says the session in it is done and its successor is
// due. A window nothing could date counts as older than every handoff: the
// refusal is for a seat that is demonstrably still held.
func handedOffSince(handoffs []Handoff, opened time.Time) bool {
	for _, handoff := range handoffs {
		if handoff.Written.After(opened) {
			return true
		}
	}
	return false
}

// seatWindowNumber is the number a seat's window is numbered with — 13 of
// mayor-2026-09-19-13 — and whether the name is one of that seat's windows at
// all.
func seatWindowNumber(seat, window string) (int, bool) {
	rest, isSeats := strings.CutPrefix(window, seat+"-")
	if !isSeats {
		return 0, false
	}
	digits := rest[strings.LastIndex(rest, "-")+1:]
	if digits == "" {
		return 0, false
	}
	number := 0
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, false
		}
		number = number*10 + int(r-'0')
	}
	return number, true
}

// HandoffNumber is the number a handoff's name ends with — 12 of
// 2026-09-19-12 — and 0 for a name that ends with none. It is what a seat's
// next window is numbered past.
func HandoffNumber(name string) int {
	digits := name[strings.LastIndex(name, "-")+1:]
	number := 0
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0
		}
		number = number*10 + int(r-'0')
	}
	return number
}

// plainSeatName reports whether a name can be a seat's: it ends up in a path
// in the vault and in the name of a window, and it comes from a command line.
func plainSeatName(seat string) bool {
	for _, r := range seat {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return seat != ""
}

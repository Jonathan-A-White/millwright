package application

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// TalkLogFileName is the file `mw talk model` writes what it did to:
// `.<seat>-talk.log` in the vault, host-local and untracked.
func TalkLogFileName(seat string) string {
	return "." + seat + "-talk.log"
}

// TalkModels are the models `mw talk model` switches the Mayor between: the
// chips the Governor can pick in a talk.
var TalkModels = []domain.Model{domain.ModelOpus, domain.ModelSonnet, domain.ModelFable, domain.ModelHaiku}

// TalkRespawnEffort is the effort the fresh Mayor is started at.
const TalkRespawnEffort = "high"

// TalkLog is the port `mw talk model` says what it did through: one line,
// appended to the seat's talk log. The line arrives whole and dated; the log is
// only where it goes.
type TalkLog interface {
	// NoteTalk appends one line to the seat's talk log.
	NoteTalk(ctx context.Context, seat, line string) error
}

// TalkSpeaker is what speaks on the talk: TalkSay, in the command.
type TalkSpeaker interface {
	Run(ctx context.Context, req TalkSayRequest) (TalkSayReport, error)
}

// CheckTalkModel says whether a model is one `mw talk model` switches to, and
// which ones it does when it is not.
func CheckTalkModel(model domain.Model) error {
	for _, known := range TalkModels {
		if model == known {
			return nil
		}
	}
	return fmt.Errorf("%q is not a model to switch to: say opus, sonnet, fable or haiku", model)
}

// respawnMayorLine is the line that tells the Mayor how to hand the talk to a
// fresh Mayor on the model, and false for a model the factory has no id for.
func respawnMayorLine(model domain.Model) (string, bool) {
	id := domain.FullModelID(model)
	if id == "" {
		return "", false
	}
	return fmt.Sprintf("hand off, then: bin/respawn-mayor %s %s", TalkRespawnEffort, id), true
}

// TalkModel answers a model switch in a talk. A session cannot change its own
// model, and typing /model into a live window failed, so the switch is a fresh
// Mayor on the chosen model: this speaks that on the talk, logs it, and prints
// the line the Mayor runs to hand off and start the fresh one. It types into no
// window and starts nothing.
type TalkModel struct {
	Say TalkSpeaker
	Log TalkLog

	// Seat is the seat whose model is switched, and Model what it is switched
	// to.
	Seat  string
	Model domain.Model

	// Now is the clock; nil is time.Now.
	Now func() time.Time

	// Out is where the line for the Mayor is printed. A nil Out prints nothing.
	Out io.Writer
}

// TalkModelRequest names the Governor's turn the switch answers, as mw talk
// wait printed it.
type TalkModelRequest struct {
	TalkID string
	Turn   int
}

// TalkModelReport is what a switch did.
type TalkModelReport struct {
	Seat  string
	Model domain.Model
	// Spoken is what was said on the talk.
	Spoken string
	// Said is the line logged, and Respawn the line printed for the Mayor.
	Said    string
	Respawn string
}

// String is the report as `mw talk model` prints it: the line for the Mayor.
func (r TalkModelReport) String() string { return r.Respawn }

// Run speaks the switch, logs it and prints the respawn line. A model it does
// not switch to is refused before anything is said. If the answer cannot be
// sent, that is logged and returned, and no respawn line is printed.
func (t TalkModel) Run(ctx context.Context, req TalkModelRequest) (TalkModelReport, error) {
	switch {
	case t.Say == nil || t.Log == nil:
		return TalkModelReport{}, fmt.Errorf("switching a model needs something to speak on the talk and a log to write")
	case t.Seat == "":
		return TalkModelReport{}, fmt.Errorf("whose model is to be switched?")
	case !plainSeatName(t.Seat):
		return TalkModelReport{}, fmt.Errorf("%q is not a seat: a seat is named in letters, digits, dashes and underscores", t.Seat)
	}
	if err := CheckTalkModel(t.Model); err != nil {
		return TalkModelReport{}, err
	}
	respawn, _ := respawnMayorLine(t.Model)
	name := strings.ToUpper(string(t.Model[:1])) + string(t.Model[1:])
	spoken := fmt.Sprintf("Switching to %s: a fresh Mayor takes the line in about a minute", name)
	report := TalkModelReport{Seat: t.Seat, Model: t.Model, Spoken: spoken, Respawn: respawn}

	if _, err := t.Say.Run(ctx, TalkSayRequest{Text: spoken, TalkID: req.TalkID, Turn: req.Turn}); err != nil {
		report.Said, _ = t.log(ctx, fmt.Sprintf("could not speak the switch on talk %s turn %d: %s", req.TalkID, req.Turn, oneLine(err.Error())))
		return report, err
	}
	said, err := t.log(ctx, fmt.Sprintf("spoke the switch on talk %s turn %d; %s", req.TalkID, req.Turn, respawn))
	report.Said = said
	if err != nil {
		return report, err
	}
	if t.Out != nil {
		fmt.Fprintln(t.Out, respawn)
	}
	return report, nil
}

// log appends one dated line to the talk log and returns it.
func (t TalkModel) log(ctx context.Context, text string) (string, error) {
	line := fmt.Sprintf("%s talk model %s: %s", t.now().UTC().Format(time.RFC3339), t.Model, strings.TrimSpace(text))
	if err := t.Log.NoteTalk(ctx, t.Seat, line); err != nil {
		return line, fmt.Errorf("mw talk model could not log %q: %w", line, err)
	}
	return line, nil
}

func (t TalkModel) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}

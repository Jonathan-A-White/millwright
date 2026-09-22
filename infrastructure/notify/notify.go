// Package notify is the one desktop notice a host may be able to raise: a
// call to notify-send, when this host has one on its PATH. A phoneless VPS
// simply has none, and Notify then does nothing more — there is no config
// knob for it, on purpose: a host either has a notifier or it does not.
package notify

import (
	"context"
	"os/exec"

	"github.com/Jonathan-A-White/millwright/application"
)

// Program is the notifier's name on the PATH.
const Program = "notify-send"

// Title is what every notice this sends is titled.
const Title = "mw"

// Desktop sends a notice through notify-send when this host has one on its
// PATH. Its zero value is ready to use.
type Desktop struct{}

// Desktop satisfies the port.
var _ application.Notifier = Desktop{}

// New is the desktop notifier.
func New() Desktop { return Desktop{} }

// Notify implements application.Notifier. A host with no notify-send is not a
// fault: Notify does nothing more.
func (Desktop) Notify(ctx context.Context, line string) error {
	if _, err := exec.LookPath(Program); err != nil {
		return nil
	}
	return exec.CommandContext(ctx, Program, Title, line).Run()
}

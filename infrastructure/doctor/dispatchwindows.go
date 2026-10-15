package doctor

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*DispatchWindows)(nil)

// DispatchWindowsName is what the check is called: in the log, and on the
// command line as `mw doctor dispatch-windows`.
const DispatchWindowsName = "dispatch-windows"

// The dispatch-windows check's damper: it has no cure to retry, so one failed
// cure attempt is all an episode ever spends.
const (
	DispatchWindowsDamperWait = 0 * time.Second
	DispatchWindowsDamperCap  = 1
)

var errDispatchWindowsNoCure = errors.New(
	"no cure: a person corrects the [[dispatch.windows]] entry in ~/.config/mw/config.toml")

// DispatchWindows is the check that every [[dispatch.windows]] entry of the
// config file is well formed (mw-gq6.429). A malformed one is skipped when mw
// reads the file and the root values apply, so without this check nothing would
// say the window is not in force. It never cures.
type DispatchWindows struct {
	// Problems is what config.DispatchWindowProblems reads: one error for each
	// malformed entry, naming its key.
	Problems []error
}

// NewDispatchWindows is the check over problems, as this host's config reads them.
func NewDispatchWindows(problems []error) *DispatchWindows {
	return &DispatchWindows{Problems: problems}
}

// Name implements application.DoctorCheck.
func (d *DispatchWindows) Name() string { return DispatchWindowsName }

// Probe implements application.DoctorCheck: ok when every window reads, faulty
// naming each one that does not.
func (d *DispatchWindows) Probe(context.Context) (application.Verdict, string) {
	if len(d.Problems) == 0 {
		return application.DoctorOK, ""
	}
	said := make([]string, 0, len(d.Problems))
	for _, problem := range d.Problems {
		said = append(said, problem.Error())
	}
	return application.DoctorFaulty, strings.Join(said, "; ") + " (the root values apply meanwhile)"
}

// Cure implements application.DoctorCheck: there is none.
func (d *DispatchWindows) Cure(context.Context) error { return errDispatchWindowsNoCure }

// Damper implements application.DoctorCheck.
func (d *DispatchWindows) Damper() (time.Duration, int) {
	return DispatchWindowsDamperWait, DispatchWindowsDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (d *DispatchWindows) WayBack() string {
	return "none: no cure runs; a person corrects the window by hand"
}

package doctor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*HarnessLevel)(nil)

// HarnessLevelName is what the check is called: in the log, and on the command
// line as `mw doctor harness-level`.
const HarnessLevelName = "harness-level"

// The harness-level check's damper: there is no cure to retry, so one failed
// cure attempt is all an episode ever spends, as with boost-asleep.
const (
	HarnessLevelDamperWait = 0 * time.Second
	HarnessLevelDamperCap  = 1
)

// HarnessHost is a Builder host the check looks at: Box is a cloud box, whose
// Claude Code is an npm package under ~/.local, not the native install.
type HarnessHost struct {
	Name string
	Box  bool
}

// HarnessLevel is the check that no Builder host runs an older Claude Code than
// the home (mw-gq6.1059). A host that falls behind is unseen: a new model
// needs a newer Claude Code (claude-fable-5-2 needs 2.1.321), and a session
// there fails. Each host leaves its version as application.HarnessKey on every
// sync, as it leaves its mw commit; the home reads those, never the host
// itself, and judges them against its own `claude --version`.
//
// Behind is faulty, one line per host naming it, both versions and the cure for
// its kind of install. A host that left no version, or one that is no version,
// is said unread and never guessed; with none behind, that is cannot-tell. It
// runs only on the home. It changes nothing: the cure is a person's, or
// contrib/vultr-boost set-harness for a box.
type HarnessLevel struct {
	// Home and Host are the vault's home file and this host's name.
	Home application.HomeFile
	Host string
	// Notes is where each host leaves its version.
	Notes interface {
		Note(ctx context.Context, key string) (string, error)
	}
	// Version is the Claude Code this host runs, as `claude --version` prints
	// it. Hosts are the Builder hosts to look at besides this one.
	Version func(ctx context.Context) (string, error)
	Hosts   func(ctx context.Context) ([]HarnessHost, error)

	// faulty is what Probe last found, for Cure to carry.
	faulty string
}

// NewHarnessLevel is the check for the host named host, against the given home
// file, reading versions from notes.
func NewHarnessLevel(home application.HomeFile, host string, notes interface {
	Note(ctx context.Context, key string) (string, error)
}) *HarnessLevel {
	return &HarnessLevel{Home: home, Host: host, Notes: notes}
}

// Name implements application.DoctorCheck.
func (h *HarnessLevel) Name() string { return HarnessLevelName }

// Probe implements application.DoctorCheck: ok, n/a, on a host that is not
// home; cannot-tell when the home cannot be told, cannot say its own version or
// list the hosts; faulty for each host behind the home; cannot-tell for hosts
// unread when none is behind; ok otherwise.
func (h *HarnessLevel) Probe(ctx context.Context) (application.Verdict, string) {
	h.faulty = ""
	if h.Home == nil {
		return application.DoctorOK, "n/a: no home file to say whether this host is home"
	}
	home, err := application.IsHome(ctx, h.Home, h.Host)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if !home {
		return application.DoctorOK, "n/a: this host is not home"
	}
	printed, err := h.Version(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("the home's Claude Code version cannot be read: %v", err)
	}
	want := application.HarnessNumber(printed)
	if want == "" {
		return application.DoctorCannotTell, fmt.Sprintf("the home's Claude Code version cannot be read: %q is no version", printed)
	}
	hosts, err := h.Hosts(ctx)
	if err != nil {
		return application.DoctorCannotTell, fmt.Sprintf("the Builder hosts cannot be listed: %v", err)
	}

	var behind, unread []string
	for _, host := range hosts {
		if host.Name == h.Host {
			continue
		}
		said, err := h.Notes.Note(ctx, application.HarnessKey(host.Name))
		if err != nil {
			unread = append(unread, fmt.Sprintf("%s unread (%v)", host.Name, err))
			continue
		}
		have := application.HarnessNumber(said)
		if have == "" {
			why := "it has left no version"
			if said = strings.TrimSpace(said); said != "" {
				why = fmt.Sprintf("it left %q", said)
			}
			unread = append(unread, fmt.Sprintf("%s unread (%s)", host.Name, why))
			continue
		}
		if application.HarnessBehind(have, want) {
			behind = append(behind, fmt.Sprintf("%s has claude %s, behind the home's %s: %s", host.Name, have, want, harnessCure(host, want)))
		}
	}
	if len(behind) == 0 && len(unread) == 0 {
		return application.DoctorOK, fmt.Sprintf("every Builder host has claude %s or later", want)
	}
	reason := strings.Join(append(behind, unread...), "; ")
	if len(behind) == 0 {
		return application.DoctorCannotTell, reason
	}
	h.faulty = reason
	return application.DoctorFaulty, reason
}

// harnessCure is the line that brings a host level, for its kind of install.
func harnessCure(host HarnessHost, version string) string {
	if host.Box {
		return fmt.Sprintf("from the home run `contrib/vultr-boost set-harness --name %s --version %s` (docs/harness-level.md)", host.Name, version)
	}
	return fmt.Sprintf("on %s run `claude install %s` (docs/harness-level.md)", host.Name, version)
}

// Cure implements application.DoctorCheck: there is none. It fails, saying what
// is behind, so the doctor's log and note carry it.
func (h *HarnessLevel) Cure(context.Context) error {
	if h.faulty == "" {
		return errors.New("no cure: the check only reports; bring the host level by hand (docs/harness-level.md)")
	}
	return fmt.Errorf("no cure: the check only reports: %s", h.faulty)
}

// Damper implements application.DoctorCheck.
func (h *HarnessLevel) Damper() (time.Duration, int) {
	return HarnessLevelDamperWait, HarnessLevelDamperCap
}

// WayBack implements application.DoctorCheck: nothing ever changes.
func (h *HarnessLevel) WayBack() string {
	return "none: no cure runs; docs/harness-level.md says how each kind of host is brought level by hand, and the way back"
}

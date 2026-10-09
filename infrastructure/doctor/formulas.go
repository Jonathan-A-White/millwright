package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.DoctorCheck = (*Formulas)(nil)

// FormulasName is what the check is called: in the log, and on the command line
// as `mw doctor formulas`.
const FormulasName = "formulas"

// The formulas check's damper. It never cures, so the numbers only bound how
// often a person is told.
const (
	FormulasDamperWait = time.Hour
	FormulasDamperCap  = 1
)

// formulasSeenStateName is where the check remembers, between runs, the factory
// commit at which it first found the vault's formulas out of step.
const formulasSeenStateName = "formulas-seen"

const formulasNoCure = "no cure runs: the home's self-update installs the formulas (docs/formulas.md); a person looks at why it has not"

// Formulas is the check that the vault's .beads/formulas, the copies bd pours,
// match the factory checkout's formulas/ (mw-gq6.314). The home's self-update
// installs a changed formula right after the checkout moves, so a vault found
// out of step once is only noted: it is faulty when it is still out of step at
// a later commit of the factory rig, that is after more than one self-update.
// Only the home is judged; the other host's vault is whatever sync brought.
type Formulas struct {
	// Installer compares the vault's copies with the checkout's.
	Installer application.FormulaInstaller
	// FactoryDir is the factory rig's checkout on this host, empty when none.
	FactoryDir string
	// Home and Host name the vault's home file and this host.
	Home application.HomeFile
	Host string
	// State is where the commit first seen out of step is kept.
	State application.DoctorState
	// Program is the program run for git. Empty reads "git".
	Program string

	faulty string
}

// NewFormulas is the check over the factory checkout at factoryDir.
func NewFormulas(installer application.FormulaInstaller, factoryDir string, home application.HomeFile, host string, state application.DoctorState) *Formulas {
	return &Formulas{Installer: installer, FactoryDir: factoryDir, Home: home, Host: host, State: state}
}

// Name implements application.DoctorCheck.
func (f *Formulas) Name() string { return FormulasName }

// Probe implements application.DoctorCheck.
func (f *Formulas) Probe(ctx context.Context) (application.Verdict, string) {
	f.faulty = ""
	if f.FactoryDir == "" {
		return application.DoctorOK, "n/a: this host has no millwright rig to take formulas from"
	}
	home, err := application.IsHome(ctx, f.Home, f.Host)
	if err != nil {
		return application.DoctorCannotTell, err.Error()
	}
	if !home {
		return application.DoctorOK, "n/a: this host is not home, whose vault is judged"
	}
	stale, err := f.Installer.OutOfStep(ctx, f.FactoryDir)
	if err != nil {
		return application.DoctorCannotTell, "comparing the formulas: " + err.Error()
	}
	if len(stale) == 0 {
		if err := f.State.Reset(ctx, formulasSeenStateName); err != nil {
			return application.DoctorCannotTell, "forgetting this check's own state: " + err.Error()
		}
		return application.DoctorOK, ""
	}
	head, err := f.head(ctx)
	if err != nil {
		return application.DoctorCannotTell, "reading the factory checkout's commit: " + err.Error()
	}
	seen, err := f.State.Load(ctx, formulasSeenStateName)
	if err != nil {
		return application.DoctorCannotTell, "reading this check's own state: " + err.Error()
	}
	if len(seen.SeenPaths) == 0 {
		if err := f.State.Save(ctx, formulasSeenStateName, application.DoctorEpisode{SeenPaths: []string{head}}); err != nil {
			return application.DoctorCannotTell, "saving this check's own state: " + err.Error()
		}
		return application.DoctorOK, ""
	}
	if seen.SeenPaths[0] == head {
		// The self-update that moved the checkout has not had another since.
		return application.DoctorOK, ""
	}
	f.faulty = fmt.Sprintf("the vault's formulas are out of step with the factory checkout's after more than one self-update (first seen at %s, now %s): %s",
		short(seen.SeenPaths[0]), short(head), strings.Join(stale, ", "))
	return application.DoctorFaulty, f.faulty
}

// Cure implements application.DoctorCheck: there is none.
func (f *Formulas) Cure(context.Context) error {
	if f.faulty == "" {
		return errors.New(formulasNoCure)
	}
	return fmt.Errorf("%s: %s", formulasNoCure, f.faulty)
}

// Damper implements application.DoctorCheck.
func (f *Formulas) Damper() (time.Duration, int) { return FormulasDamperWait, FormulasDamperCap }

// WayBack implements application.DoctorCheck: nothing ever changes.
func (f *Formulas) WayBack() string {
	return "none: no cure runs; a person installs the formulas by hand (docs/formulas.md)"
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func (f *Formulas) head(ctx context.Context) (string, error) {
	program := f.Program
	if program == "" {
		program = "git"
	}
	cmd := exec.CommandContext(ctx, program, "-C", f.FactoryDir, "rev-parse", "HEAD")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errs.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

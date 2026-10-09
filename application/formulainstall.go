package application

import (
	"context"
	"fmt"
	"strings"
)

// FormulasDir is where, in the vault, bd looks first for a formula to pour
// (`bd formula --help`), and so the one copy of each of millwright's formulas
// that a Builder is ever given. FactoryFormulasDir is where they are written, in
// the factory rig.
const (
	FormulasDir        = ".beads/formulas"
	FactoryFormulasDir = "formulas"
)

// FormulaInstaller is the port the factory's formulas are put into the vault
// through, once the home's checkout of the factory rig has moved (mw-gq6.314).
// bd pours the vault's copy, so a change landed in formulas/ is live only when
// that copy is level with it.
type FormulaInstaller interface {
	// OutOfStep lists the formula files of the factory checkout at factoryDir
	// (formulas/*.formula.json) whose copy in the vault is missing or differs,
	// by file name. Empty means the vault is level.
	OutOfStep(ctx context.Context, factoryDir string) ([]string, error)

	// Held lists the paths under the vault's .beads/formulas that are changed
	// and not committed: somebody's work in progress, not to be overwritten.
	Held(ctx context.Context) ([]string, error)

	// Install copies the files OutOfStep names into the vault, commits exactly
	// those paths under message and pushes the commit. It reports the paths
	// committed. A push that fails after the commit is made is not an error:
	// the next mw sync publishes it, and the returned string says so.
	Install(ctx context.Context, factoryDir, message string) (committed []string, pushNote string, err error)
}

// installFormulasMessage is the vault commit's message: it names the factory
// commit the copies were taken from, and is the line a person git-reverts to go
// back (docs/formulas.md, "Installing formulas into the vault").
func installFormulasMessage(commit string) string {
	return "Install formulas from millwright " + updatedRevision(commit)
}

// installFormulas puts the factory checkout's formulas into the vault when the
// vault's copies differ, and says what it did as notes for the tick's line:
// none when they are level. The vault's copies that somebody has changed and
// not committed are left alone, and said.
func (s SelfUpdate) installFormulas(ctx context.Context, dir, head string) []string {
	if s.Formulas == nil {
		return nil
	}
	if s.Home != nil {
		if home, err := IsHome(ctx, s.Home, s.Host); err != nil || !home {
			return nil
		}
	}
	words := selfUpdateWords + " formulas"
	stale, err := s.Formulas.OutOfStep(ctx, dir)
	if err != nil {
		return []string{fmt.Sprintf("%s could not be compared with the vault's: %s", words, firstLine(err.Error()))}
	}
	if len(stale) == 0 {
		return nil
	}
	held, err := s.Formulas.Held(ctx)
	if err != nil {
		return []string{fmt.Sprintf("%s could not be installed: reading the vault: %s", words, firstLine(err.Error()))}
	}
	if len(held) > 0 {
		return []string{fmt.Sprintf("%s left alone: the vault has %d uncommitted path(s) under %s: %s",
			words, len(held), FormulasDir, strings.Join(held, ", "))}
	}
	committed, pushNote, err := s.Formulas.Install(ctx, dir, installFormulasMessage(head))
	if err != nil {
		return []string{fmt.Sprintf("%s could not be installed: %s", words, firstLine(err.Error()))}
	}
	note := fmt.Sprintf("%s installed in the vault from %s: %s", words, updatedRevision(head), strings.Join(committed, ", "))
	if pushNote != "" {
		note += " (" + pushNote + ")"
	}
	return []string{note}
}

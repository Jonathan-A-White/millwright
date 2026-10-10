package rig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// BuiltBinary is where a rig's after-landing build leaves its mw, under the rig's
// checkout.
const BuiltBinary = "bin/mw"

// BuiltSmoker makes a grist smoke with the mw a landing just built, in a process
// of its own: `bin/mw grist smoke <app> --json` in the rig's checkout. The mw a
// landing runs in was started before the merge and holds the code from before
// it, so a landing that changes the smoke is smoked by the old one when it is
// made in that process (mw-gq6.339). It is the adapter behind
// application.GristSmokeAfter's Built.
type BuiltSmoker struct {
	binary   string
	fallback application.GristAppSmoker
}

var _ application.GristAppSmoker = BuiltSmoker{}

// NewBuiltSmoker is the smoker of the mw built in the checkout rigDir. When the
// build left none there, which is a build that failed on a host that had never
// built, it smokes with fallback, the mw it is running in.
func NewBuiltSmoker(rigDir string, fallback application.GristAppSmoker) BuiltSmoker {
	return BuiltSmoker{binary: filepath.Join(rigDir, BuiltBinary), fallback: fallback}
}

// Run implements application.GristAppSmoker. The built mw is asked for the whole
// smoke as JSON and records nothing itself: the landing records what it finds,
// against the rig whose stories a failure holds.
func (b BuiltSmoker) Run(ctx context.Context, app, kind string) (application.GristSmokeReport, error) {
	if info, err := os.Stat(b.binary); err != nil || info.IsDir() {
		if b.fallback == nil {
			return application.GristSmokeReport{App: app}, fmt.Errorf("there is no built mw at %s", b.binary)
		}
		return b.fallback.Run(ctx, app, kind)
	}
	args := []string{"grist", "smoke", app, "--json"}
	if kind != "" {
		args = append(args, "--kind", kind)
	}
	cmd := exec.CommandContext(ctx, b.binary, args...)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	runErr := cmd.Run()
	var report application.GristSmokeReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.App == "" {
		said := strings.TrimSpace(errs.String())
		if said == "" {
			said = strings.TrimSpace(out.String())
		}
		if said == "" && runErr != nil {
			said = runErr.Error()
		}
		return application.GristSmokeReport{App: app}, errors.New("the built mw's smoke said nothing that can be read: " + firstLineOf(said))
	}
	return report, nil
}

func firstLineOf(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

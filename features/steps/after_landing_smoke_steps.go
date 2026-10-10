package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// runningSmoker is the mw a landing runs in: it judges as the code it was built
// from before the landing did, and counts the smokes it was asked to make.
type runningSmoker struct{ ran []string }

func (s *runningSmoker) Run(_ context.Context, app, _ string) (application.GristSmokeReport, error) {
	s.ran = append(s.ran, app)
	return application.GristSmokeReport{App: app, Examples: 1, Failures: []string{app + "/sweep/one: the request has no schemaVersion"}}, nil
}

// smokeAfterLanding is the state of the scenarios that ask what a landing smokes
// with: the rig checkout whose bin/mw is the build the landing made, a stand-in
// script that says what the new code answers.
type smokeAfterLanding struct {
	running *runningSmoker
	book    *apptest.FakeTracker
	rigName string
	rigDir  string
	rigs    map[string]string
	lines   []string
	failed  []string
}

func registerAfterLandingSmokeSteps(ctx *godog.ScenarioContext, c *afterLandingContext) {
	s := &smokeAfterLanding{}
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*s = smokeAfterLanding{running: &runningSmoker{}, book: apptest.NewFakeTracker()}
		return ctx, nil
	})

	ctx.Given(`^a rig "([^"]*)" whose landing changes how the grist smoke judges the app "([^"]*)"$`, func(name, app string) error {
		return s.rig(c, name, app, true)
	})
	ctx.Given(`^a rig "([^"]*)" whose build left no mw and whose running mw fails the smoke of "([^"]*)"$`, func(name, app string) error {
		return s.rig(c, name, app, false)
	})
	ctx.When(`^the landing of "([^"]*)" makes its grist smoke of "([^"]*)"$`, s.landingSmokes)
	ctx.Then(`^the mw the landing built made the smoke of "([^"]*)"$`, func(app string) error {
		return s.madeBy(c, app, true)
	})
	ctx.Then(`^the mw the landing runs in made the smoke of "([^"]*)"$`, func(app string) error {
		return s.madeBy(c, app, false)
	})
	ctx.Then(`^the grist smoke of "([^"]*)" passed$`, func(app string) error { return s.outcome(app, true) })
	ctx.Then(`^the grist smoke of "([^"]*)" failed$`, func(app string) error { return s.outcome(app, false) })
	ctx.Then(`^the mw the landing runs in smoked nothing itself$`, func() error {
		if len(s.running.ran) != 0 {
			return fmt.Errorf("the mw the landing runs in smoked %v itself", s.running.ran)
		}
		return nil
	})
}

// rig makes the rig checkout; built says the landing's build left a bin/mw that
// answers the smoke as the changed code does, and otherwise no bin/mw is there.
func (s *smokeAfterLanding) rig(c *afterLandingContext, name, app string, built bool) error {
	if c.root == "" {
		root, err := os.MkdirTemp("", "mw-after-landing-*")
		if err != nil {
			return err
		}
		c.root = root
	}
	s.rigName, s.rigDir = name, filepath.Join(c.root, "rigs", name)
	s.rigs = map[string]string{name: s.rigDir}
	if err := os.MkdirAll(filepath.Join(s.rigDir, "bin"), 0o755); err != nil || !built {
		return err
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" > \"$(dirname \"$0\")/asked\"\n" +
		"echo '{\"app\":\"" + app + "\",\"examples\":1}'\n"
	return os.WriteFile(filepath.Join(s.rigDir, "bin", "mw"), []byte(script), 0o755)
}

func (s *smokeAfterLanding) landingSmokes(name, app string) error {
	after := application.GristSmokeAfter{
		Touches: allTouched{}, Smoke: s.running, Book: application.GristSmokeBook{Notes: s.book, Host: "laptop"},
		Apps: map[string]string{app: s.rigDir}, Rigs: s.rigs,
		Built: rig.NewBuiltSmoker(s.rigDir, s.running),
	}
	apps, notes := after.Wants(context.Background(), name, s.rigDir, "origin/main", "mw/story")
	if len(notes) > 0 {
		return fmt.Errorf("the landing could not be read: %v", notes)
	}
	s.lines, s.failed = after.Run(context.Background(), name, apps)
	return nil
}

func (s *smokeAfterLanding) madeBy(c *afterLandingContext, app string, built bool) error {
	asked, err := os.ReadFile(filepath.Join(s.rigDir, "bin", "asked"))
	if built {
		if err != nil {
			return fmt.Errorf("the mw the landing built was never run: %v", err)
		}
		if want := "grist smoke " + app; !strings.HasPrefix(string(asked), want) {
			return fmt.Errorf("the built mw was run as %q, not %q", strings.TrimSpace(string(asked)), want)
		}
		return nil
	}
	if err == nil {
		return fmt.Errorf("the mw the landing built was run: %s", asked)
	}
	if len(s.running.ran) != 1 {
		return fmt.Errorf("the mw the landing runs in smoked %v", s.running.ran)
	}
	return nil
}

func (s *smokeAfterLanding) outcome(app string, passed bool) error {
	if passed && len(s.failed) > 0 {
		return fmt.Errorf("the smoke of %s failed: %v", app, s.failed)
	}
	if !passed && len(s.failed) == 0 {
		return fmt.Errorf("the smoke of %s passed: %v", app, s.lines)
	}
	if len(s.lines) == 0 {
		return fmt.Errorf("the landing made no smoke of %s", app)
	}
	return nil
}

// allTouched says every pathspec a landing is asked about changed.
type allTouched struct{}

func (allTouched) Changed(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}

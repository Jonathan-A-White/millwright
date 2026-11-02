package steps

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"

	"github.com/cucumber/godog"
)

// registerStaleLandingsSteps registers the steps of features/doctor_stale_landings.feature.
func (c *doctorContext) registerStaleLandingsSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^a rig with a landing worktree left (\d+) hours ago, clean and level with main$`, c.aRigWithALandingLeftHoursAgo)
	ctx.Given(`^the landing worktree holds an uncommitted file$`, c.theLandingHoldsAnUncommittedFile)
	ctx.Then(`^the landing worktree is gone and git no longer lists it$`, c.theLandingIsGone)
	ctx.Then(`^the landing worktree is still there$`, c.theLandingIsStillThere)
	ctx.Then(`^the doctor log and the report both name the landing worktree's path$`, c.theLogAndReportNameTheLanding)
}

func (c *doctorContext) landingPath() string {
	return filepath.Join(c.landingsDir, application.LandingPrefix+"2950915166")
}

func (c *doctorContext) aRigWithALandingLeftHoursAgo(hours int) error {
	root, err := os.MkdirTemp("", "mw-doctor-stale-landings")
	if err != nil {
		return err
	}
	c.landingsDir = root
	rig := filepath.Join(root, "rig")
	if err := os.MkdirAll(rig, 0o755); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "millwright test"},
		{"config", "user.email", "test@millwright.invalid"},
	} {
		if _, err := runGit(rig, args...); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(rig, "a.txt"), []byte("a\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "first"}, {"worktree", "add", "-q", "--detach", c.landingPath(), "main"}} {
		if _, err := runGit(rig, args...); err != nil {
			return err
		}
	}
	c.landingHours = hours
	if err := c.ageTheLanding(); err != nil {
		return err
	}
	c.real = &application.StaleLandingsCheck{
		Dirs:  []string{root},
		Trees: doctor.LandingTrees{},
		Now:   func() time.Time { return c.now },
	}
	return nil
}

func (c *doctorContext) theLandingHoldsAnUncommittedFile() error {
	if err := os.WriteFile(filepath.Join(c.landingPath(), "mine.txt"), []byte("someone's work\n"), 0o644); err != nil {
		return err
	}
	// Writing the file moved the directory's modification time on; it is put
	// back, as the file was left hours ago.
	return c.ageTheLanding()
}

// ageTheLanding sets the landing directory's modification time to landingHours
// before the scenario's clock.
func (c *doctorContext) ageTheLanding() error {
	then := c.now.Add(-time.Duration(c.landingHours) * time.Hour)
	return os.Chtimes(c.landingPath(), then, then)
}

func (c *doctorContext) theLandingIsGone() error {
	if _, err := os.Stat(c.landingPath()); !os.IsNotExist(err) {
		return fmt.Errorf("expected %s to be gone, stat said %v", c.landingPath(), err)
	}
	listed, err := runGit(filepath.Join(c.landingsDir, "rig"), "worktree", "list")
	if err != nil {
		return err
	}
	if strings.Contains(listed, application.LandingPrefix) {
		return fmt.Errorf("expected git to have pruned the landing, it lists %q", listed)
	}
	return nil
}

func (c *doctorContext) theLandingIsStillThere() error {
	if _, err := os.Stat(c.landingPath()); err != nil {
		return fmt.Errorf("expected %s to be left in place: %v", c.landingPath(), err)
	}
	return nil
}

func (c *doctorContext) theLogAndReportNameTheLanding() error {
	path := c.landingPath()
	removed := "removed " + path
	if !strings.Contains(c.out.String(), removed) {
		return fmt.Errorf("expected the report to say %q, got %q", removed, c.out.String())
	}
	for _, line := range c.log.Lines() {
		if strings.Contains(line, removed) {
			return nil
		}
	}
	return fmt.Errorf("expected the doctor log to say %q, got %v", removed, c.log.Lines())
}

package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// The steps of features/self_update.feature. They run a real Millhand tick,
// with everything it reads stood in for but the part under test: a real git
// rig with a bare origin of its own, and a real shell running the build
// command, a script that writes the commit it was run at into bin/mw.

// selfUpdateContext is one scenario's world: a checkout of the factory rig
// behind its origin, and the build command the host names for it.
type selfUpdateContext struct {
	root     string
	rig      string
	origin   string
	oldHead  string
	newHead  string
	commands map[string]string
	line     string
}

// quietSync is a sync that finds the host level.
type quietSync struct{}

func (quietSync) Run(context.Context) (application.SyncReport, error) {
	return application.SyncReport{Host: "laptop"}, nil
}

// InitializeSelfUpdateScenario registers the steps of features/self_update.feature.
func InitializeSelfUpdateScenario(ctx *godog.ScenarioContext) {
	c := &selfUpdateContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = selfUpdateContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a millwright rig whose origin's main has moved on since this host's checkout$`, c.aRigBehindItsOrigin)
	ctx.Given(`^the host's build command makes bin/mw$`, c.theBuildMakesBinMw)
	ctx.When(`^the host's build command makes bin/mw$`, c.theBuildMakesBinMw)
	ctx.Given(`^the host's build command prints "([^"]*)" and fails$`, c.theBuildFails)
	ctx.Given(`^the host names no build command$`, func() error { c.commands = nil; return nil })
	ctx.Given(`^the checkout has an uncommitted change$`, c.theCheckoutIsDirty)
	ctx.Given(`^the checkout has a commit of its own$`, c.theCheckoutHasACommit)
	ctx.Given(`^the host's tick has looked at its mw$`, c.theTickLooks)
	ctx.When(`^the host's tick looks at its mw$`, c.theTickLooks)

	ctx.Then(`^the checkout is at origin's main$`, func() error { return c.checkoutIs(c.newHead) })
	ctx.Then(`^the checkout is still at its old commit$`, func() error { return c.checkoutIs(c.oldHead) })
	ctx.Then(`^the checkout is still at its old commit, with its uncommitted change$`, c.stillDirty)
	ctx.Then(`^the checkout is still at its own commit$`, c.stillItsOwn)
	ctx.Then(`^the build ran once, in the checkout, at origin's main$`, c.theBuildRanOnce)
	ctx.Then(`^the build did not run$`, c.theBuildDidNotRun)
	ctx.Then(`^bin/mw was rebuilt$`, c.binMwWasRebuilt)
	ctx.Then(`^bin/mw is still the old one$`, c.binMwIsOld)
	ctx.Then(`^the tick's line says: (.+)$`, c.theLineSays)
	ctx.Then(`^the tick's line says nothing$`, c.theLineSaysNothing)
}

func (c *selfUpdateContext) git(dir string, args ...string) error { return gitRun(dir, "git", args...) }

func (c *selfUpdateContext) aRigBehindItsOrigin() error {
	if !rig.Available() {
		return fmt.Errorf("%s is not on PATH", rig.Program)
	}
	root, err := os.MkdirTemp("", "mw-selfupdate-")
	if err != nil {
		return err
	}
	c.root = root

	c.origin = filepath.Join(root, "origin.git")
	if err := c.git(root, "init", "--bare", "-q", "-b", "main", c.origin); err != nil {
		return err
	}
	other := filepath.Join(root, "other-host")
	if err := c.git(root, "clone", "-q", c.origin, other); err != nil {
		return err
	}
	if err := gitIdentify(other); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("# millwright\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(other, ".gitignore"), []byte("bin/\n"), 0o644); err != nil {
		return err
	}
	if err := c.commitAndPush(other, "The rig opens"); err != nil {
		return err
	}

	c.rig = filepath.Join(root, "rigs", application.FactoryRig)
	if err := c.git(root, "clone", "-q", c.origin, c.rig); err != nil {
		return err
	}
	if err := gitIdentify(c.rig); err != nil {
		return err
	}
	if c.oldHead, err = gitSay(c.rig, "rev-parse", "HEAD"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(c.rig, "bin"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(c.rig, "bin", "mw"), []byte("old\n"), 0o755); err != nil {
		return err
	}

	// The other host lands a change on main.
	if err := os.WriteFile(filepath.Join(other, "fix.md"), []byte("a fix\n"), 0o644); err != nil {
		return err
	}
	if err := c.commitAndPush(other, "A fix lands"); err != nil {
		return err
	}
	c.newHead, err = gitSay(other, "rev-parse", "HEAD")
	return err
}

func (c *selfUpdateContext) commitAndPush(dir, message string) error {
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", message}, {"push", "-q", "-u", "origin", "main"}} {
		if err := c.git(dir, args...); err != nil {
			return err
		}
	}
	return nil
}

// buildScript writes the script the host names for the rig: it logs where it
// ran and at what commit, then does what the body says.
func (c *selfUpdateContext) buildScript(body string) error {
	script := filepath.Join(c.root, "build.sh")
	text := "echo \"$PWD $(git rev-parse HEAD)\" >> \"$(dirname \"$0\")/build.log\"\n" + body + "\n"
	if err := os.WriteFile(script, []byte(text), 0o755); err != nil {
		return err
	}
	c.commands = map[string]string{application.FactoryRig: "sh " + script}
	return nil
}

func (c *selfUpdateContext) theBuildMakesBinMw() error {
	return c.buildScript("git rev-parse HEAD > bin/mw")
}

func (c *selfUpdateContext) theBuildFails(saying string) error {
	return c.buildScript(fmt.Sprintf("echo %q\nexit 2", saying))
}

func (c *selfUpdateContext) theCheckoutIsDirty() error {
	return os.WriteFile(filepath.Join(c.rig, "README.md"), []byte("half-done edit\n"), 0o644)
}

func (c *selfUpdateContext) theCheckoutHasACommit() error {
	if err := os.WriteFile(filepath.Join(c.rig, "mine.md"), []byte("mine\n"), 0o644); err != nil {
		return err
	}
	if err := c.git(c.rig, "add", "-A"); err != nil {
		return err
	}
	if err := c.git(c.rig, "commit", "-qm", "A commit of this host's own"); err != nil {
		return err
	}
	var err error
	c.oldHead, err = gitSay(c.rig, "rev-parse", "HEAD")
	return err
}

// theTickLooks runs a real Millhand tick, quiet in every respect but the one
// under test, and keeps the line it printed.
func (c *selfUpdateContext) theTickLooks() error {
	worktrees := rig.New()
	var out bytes.Buffer
	tick := application.MillhandTick{
		Millhand: application.Millhand{Windows: apptest.NewFakeWindows()},
		Sync:     quietSync{},
		Mail:     apptest.NewFakeMailbox(),
		Sweep:    application.Sweep{Tracker: apptest.NewFakeTracker(), Host: "laptop"},
		SelfUpdate: application.SelfUpdate{
			Rigs:     map[string]string{application.FactoryRig: c.rig},
			Checkout: worktrees,
			After:    rig.NewAfterLanding(rig.WithAfterCommands(c.commands)),
			Built:    rig.NewBuiltMarks(filepath.Join(c.root, "state")),
		},
		Host: "laptop",
		Now:  func() time.Time { return time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC) },
		Out:  &out,
	}
	if _, err := tick.Run(context.Background()); err != nil {
		return fmt.Errorf("the tick failed: %w", err)
	}
	c.line = strings.TrimSpace(out.String())
	return nil
}

func (c *selfUpdateContext) checkoutIs(want string) error {
	got, err := gitSay(c.rig, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected the checkout at %s, it is at %s", want, got)
	}
	return nil
}

func (c *selfUpdateContext) stillDirty() error {
	if err := c.checkoutIs(c.oldHead); err != nil {
		return err
	}
	held, err := os.ReadFile(filepath.Join(c.rig, "README.md"))
	if err != nil || string(held) != "half-done edit\n" {
		return fmt.Errorf("expected the uncommitted change kept, got %q (%v)", held, err)
	}
	return nil
}

func (c *selfUpdateContext) stillItsOwn() error {
	if err := c.checkoutIs(c.oldHead); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(c.rig, "fix.md")); err == nil {
		return fmt.Errorf("expected origin's fix not to be in the checkout")
	}
	return nil
}

func (c *selfUpdateContext) buildLog() ([]string, error) {
	held, err := os.ReadFile(filepath.Join(c.root, "build.log"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(held)), "\n"), nil
}

func (c *selfUpdateContext) theBuildRanOnce() error {
	runs, err := c.buildLog()
	if err != nil {
		return err
	}
	if len(runs) != 1 {
		return fmt.Errorf("expected the build to run once, got %d runs: %q", len(runs), runs)
	}
	wantDir, err := filepath.EvalSymlinks(c.rig)
	if err != nil {
		return err
	}
	fields := strings.Fields(runs[0])
	if len(fields) != 2 {
		return fmt.Errorf("expected a directory and a commit in the build's log, got %q", runs[0])
	}
	gotDir, err := filepath.EvalSymlinks(fields[0])
	if err != nil {
		return err
	}
	if gotDir != wantDir || fields[1] != c.newHead {
		return fmt.Errorf("expected the build in %s at %s, got %s at %s", wantDir, c.newHead, gotDir, fields[1])
	}
	return nil
}

func (c *selfUpdateContext) theBuildDidNotRun() error {
	runs, err := c.buildLog()
	if err != nil {
		return err
	}
	if len(runs) != 0 {
		return fmt.Errorf("expected no build, got %q", runs)
	}
	return nil
}

func (c *selfUpdateContext) binMw() (string, error) {
	held, err := os.ReadFile(filepath.Join(c.rig, "bin", "mw"))
	return strings.TrimSpace(string(held)), err
}

func (c *selfUpdateContext) binMwWasRebuilt() error {
	got, err := c.binMw()
	if err != nil {
		return err
	}
	if got != c.newHead {
		return fmt.Errorf("expected bin/mw built at %s, it holds %q", c.newHead, got)
	}
	return nil
}

func (c *selfUpdateContext) binMwIsOld() error {
	got, err := c.binMw()
	if err != nil {
		return err
	}
	if got != "old" {
		return fmt.Errorf("expected bin/mw left as it was, it holds %q", got)
	}
	return nil
}

func (c *selfUpdateContext) theLineSays(want string) error {
	short := func(commit string) string { return commit[:7] }
	want = strings.NewReplacer("<old>", short(c.oldHead), "<new>", short(c.newHead)).Replace(want)
	if !strings.Contains(c.line, want) {
		return fmt.Errorf("expected the tick's line to say %q, got:\n%s", want, c.line)
	}
	return nil
}

func (c *selfUpdateContext) theLineSaysNothing() error {
	if strings.Contains(c.line, "self-update") {
		return fmt.Errorf("expected the tick's line to say nothing of an update, got:\n%s", c.line)
	}
	return nil
}

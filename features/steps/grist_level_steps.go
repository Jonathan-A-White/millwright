package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain/events"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// The steps of features/grist_level.feature. They run a real follower cycle over
// a fake event log, with the real git adapters over real rigs (a bare origin of
// its own, a clone standing for the other host that lands, a clone for the
// home), and a smoke that counts what it was asked.

// levelSmoker counts the apps it was asked to smoke and finds each well.
type levelSmoker struct{ ran []string }

func (s *levelSmoker) Run(_ context.Context, app, _ string) (application.GristSmokeReport, error) {
	s.ran = append(s.ran, app)
	return application.GristSmokeReport{App: app, Examples: 2}, nil
}

// levelRig is one rig of the scenario: its origin, the clone another host lands
// from, and the home's checkout.
type levelRig struct {
	name    string
	origin  string
	other   string
	home    string
	oldHead string
}

type gristLevelContext struct {
	root    string
	rigs    map[string]*levelRig
	apps    map[string]string
	notes   *apptest.FakeTracker
	mail    *apptest.FakeMailbox
	smoker  *levelSmoker
	log     *apptest.FakeEventLog
	spring  *application.EventSpring
	ctx     context.Context
	cancel  context.CancelFunc
	home    bool
	preLeve bool
	lands   int
}

// InitializeGristLevelScenario registers the steps of features/grist_level.feature.
func InitializeGristLevelScenario(ctx *godog.ScenarioContext) {
	c := &gristLevelContext{}

	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*c = gristLevelContext{
			rigs: map[string]*levelRig{}, apps: map[string]string{},
			notes: apptest.NewFakeTracker(), mail: apptest.NewFakeMailbox(), smoker: &levelSmoker{},
			log: &apptest.FakeEventLog{}, home: true,
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if c.cancel != nil {
			c.cancel()
		}
		if c.spring != nil {
			c.spring.Wait()
		}
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^the home names the app "([^"]*)" in \[grist-apps\], checked out in the rig "([^"]*)"$`, c.theHomeNamesTheApp)
	ctx.Given(`^the rig "([^"]*)", which is not in \[grist-apps\], is checked out on the home$`, func(name string) error {
		_, err := c.makeRig(name)
		return err
	})
	ctx.Given(`^the home's follower is running on the home$`, func() error { c.home = true; return c.start() })
	ctx.Given(`^the home's follower is running on a host that is not home$`, func() error { c.home = false; return c.start() })
	ctx.Given(`^the home's checkout of "([^"]*)" has an uncommitted change$`, c.isDirty)
	ctx.Given(`^the home's checkout of "([^"]*)" has a commit of its own$`, c.hasACommit)
	ctx.Given(`^the home has already brought its checkout of "([^"]*)" level$`, func(string) error { c.preLeve = true; return nil })
	ctx.Given(`^another host lands a story on "([^"]*)" that changes "([^"]*)"$`, c.anotherHostLands)
	ctx.Given(`^the home's follower runs a cycle$`, c.cycle)
	ctx.When(`^another host lands a story on "([^"]*)" that changes "([^"]*)"$`, c.anotherHostLands)
	ctx.When(`^the home's follower runs a cycle$`, c.cycle)
	ctx.When(`^the home's checkout of "([^"]*)" is cleaned$`, c.clean)

	ctx.Then(`^the home's checkout of "([^"]*)" is at the rig's main$`, c.isAtMain)
	ctx.Then(`^the home's checkout of "([^"]*)" is still at its old commit$`, c.stillOld)
	ctx.Then(`^the home's checkout of "([^"]*)" is still at its old commit, with its uncommitted change$`, c.stillDirty)
	ctx.Then(`^the home's checkout of "([^"]*)" is still at its own commit$`, c.stillItsOwn)
	ctx.Then(`^the grist smoke of "([^"]*)" was made once$`, func(app string) error { return c.smokes(app, 1) })
	ctx.Then(`^the grist smoke of "([^"]*)" was made (\d+) times$`, c.smokes)
	ctx.Then(`^the home's mw status shows "([^"]*)"$`, c.statusShows)
	ctx.Then(`^the home's mw status does not show "([^"]*)"$`, c.statusDoesNotShow)
	ctx.Then(`^the Mayor has (\d+) mail about the checkout of "([^"]*)"$`, c.mayorHas)
}

func (c *gristLevelContext) makeRig(name string) (*levelRig, error) {
	if !rig.Available() {
		return nil, fmt.Errorf("%s is not on PATH", rig.Program)
	}
	if c.root == "" {
		root, err := os.MkdirTemp("", "mw-grist-level-")
		if err != nil {
			return nil, err
		}
		c.root = root
	}
	r := &levelRig{
		name:   name,
		origin: filepath.Join(c.root, name+"-origin.git"),
		other:  filepath.Join(c.root, name+"-other-host"),
		home:   filepath.Join(c.root, "rigs", name),
	}
	if err := gitRun(c.root, "git", "init", "--bare", "-q", "-b", "main", r.origin); err != nil {
		return nil, err
	}
	if err := gitRun(c.root, "git", "clone", "-q", r.origin, r.other); err != nil {
		return nil, err
	}
	if err := gitIdentify(r.other); err != nil {
		return nil, err
	}
	if err := c.write(r.other, "README.md", "# "+name+"\n"); err != nil {
		return nil, err
	}
	if err := c.write(r.other, "grinds/bible-talk.json", "{}\n"); err != nil {
		return nil, err
	}
	if err := c.commitAndPush(r.other, "The rig opens"); err != nil {
		return nil, err
	}
	if err := gitRun(c.root, "git", "clone", "-q", r.origin, r.home); err != nil {
		return nil, err
	}
	if err := gitIdentify(r.home); err != nil {
		return nil, err
	}
	var err error
	if r.oldHead, err = gitSay(r.home, "rev-parse", "HEAD"); err != nil {
		return nil, err
	}
	c.rigs[name] = r
	return r, nil
}

func (c *gristLevelContext) write(dir, name, text string) error {
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func (c *gristLevelContext) commitAndPush(dir, message string) error {
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", message}, {"push", "-q", "-u", "origin", "main"}} {
		if err := gitRun(dir, "git", args...); err != nil {
			return err
		}
	}
	return nil
}

func (c *gristLevelContext) theHomeNamesTheApp(app, name string) error {
	r, err := c.makeRig(name)
	if err != nil {
		return err
	}
	c.apps[app] = r.home
	return nil
}

func (c *gristLevelContext) rig(name string) (*levelRig, error) {
	r, ok := c.rigs[name]
	if !ok {
		return nil, fmt.Errorf("the scenario has no rig %q", name)
	}
	return r, nil
}

// start makes the follower, whose first cycle only reads where the log stands.
func (c *gristLevelContext) start() error {
	rigs := map[string]string{}
	for name, r := range c.rigs {
		rigs[name] = r.home
	}
	worktrees := rig.New()
	book := application.GristSmokeBook{Notes: c.notes, Host: "laptop"}
	level := application.GristLevel{
		Apps: c.apps, Rigs: rigs, Checkout: worktrees, Mailbox: c.mail, Book: book, Host: "laptop",
		Home: func(context.Context) (bool, error) { return c.home, nil },
		Smoke: application.GristSmokeAfter{
			Touches: worktrees, Smoke: c.smoker, Book: book, Apps: c.apps, Rigs: rigs,
		},
	}
	job := application.GristLevelJob(0, level.Run)
	c.spring = &application.EventSpring{Log: c.log, Host: "laptop", Jobs: []application.SpringJob{job}}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c.cycle()
}

func (c *gristLevelContext) cycle() error {
	follow := application.EventFollow{
		Head:     &apptest.FakeHead{Heads: []string{"quiet"}},
		Feed:     apptest.NewFakeTracker(),
		Log:      c.log,
		Cursors:  &apptest.FakeFollowCursors{},
		Springer: c.spring,
		Publish:  func(context.Context) error { return nil },
		Sleep:    func(context.Context, time.Duration) error { return errors.New("one cycle") },
	}
	if err := follow.Run(c.ctx); err != nil {
		return fmt.Errorf("the follower ended with %w", err)
	}
	// A pass the cycle began runs in a goroutine: wait for it.
	c.spring.Wait()
	return nil
}

// anotherHostLands is a landing made on another host: its commit is pushed to
// the rig's origin, and the story's landed state reaches the home's event log.
func (c *gristLevelContext) anotherHostLands(name, file string) error {
	r, err := c.rig(name)
	if err != nil {
		return err
	}
	c.lands++
	if err := c.write(r.other, file, fmt.Sprintf("landing %d\n", c.lands)); err != nil {
		return err
	}
	if err := c.commitAndPush(r.other, fmt.Sprintf("Landing %d", c.lands)); err != nil {
		return err
	}
	if c.preLeve {
		c.preLeve = false
		for _, args := range [][]string{{"fetch", "-q", "origin"}, {"merge", "--ff-only", "-q", "origin/main"}} {
			if err := gitRun(r.home, "git", args...); err != nil {
				return err
			}
		}
	}
	_, err = c.log.Append(context.Background(), []events.Event{{
		Ts: time.Date(2026, 10, 10, 9, 33, 0, 0, time.UTC), Kind: events.KindBeadChanged,
		Bead: fmt.Sprintf("mw-%s.%d", name, c.lands), Actor: "mw@desktop",
		From: events.BeadRunning, To: events.BeadLanded, Detail: "status", Lane: events.LaneNormal,
	}})
	return err
}

func (c *gristLevelContext) isDirty(name string) error {
	r, err := c.rig(name)
	if err != nil {
		return err
	}
	return c.write(r.home, "README.md", "half-done edit\n")
}

func (c *gristLevelContext) clean(name string) error {
	r, err := c.rig(name)
	if err != nil {
		return err
	}
	return gitRun(r.home, "git", "checkout", "--", ".")
}

func (c *gristLevelContext) hasACommit(name string) error {
	r, err := c.rig(name)
	if err != nil {
		return err
	}
	if err := c.write(r.home, "mine.md", "mine\n"); err != nil {
		return err
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-qm", "A commit of this host's own"}} {
		if err := gitRun(r.home, "git", args...); err != nil {
			return err
		}
	}
	r.oldHead, err = gitSay(r.home, "rev-parse", "HEAD")
	return err
}

func (c *gristLevelContext) isAtMain(name string) error {
	r, err := c.rig(name)
	if err != nil {
		return err
	}
	want, err := gitSay(r.origin, "rev-parse", "main")
	if err != nil {
		return err
	}
	got, err := gitSay(r.home, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected the home's checkout of %s at the rig's main %s, it is at %s", name, want, got)
	}
	return nil
}

func (c *gristLevelContext) stillOld(name string) error {
	r, err := c.rig(name)
	if err != nil {
		return err
	}
	got, err := gitSay(r.home, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if got != r.oldHead {
		return fmt.Errorf("expected the home's checkout of %s still at %s, it is at %s", name, r.oldHead, got)
	}
	return nil
}

func (c *gristLevelContext) stillDirty(name string) error {
	if err := c.stillOld(name); err != nil {
		return err
	}
	r, _ := c.rig(name)
	held, err := os.ReadFile(filepath.Join(r.home, "README.md"))
	if err != nil || string(held) != "half-done edit\n" {
		return fmt.Errorf("expected the uncommitted change kept, got %q (%v)", held, err)
	}
	return nil
}

func (c *gristLevelContext) stillItsOwn(name string) error {
	if err := c.stillOld(name); err != nil {
		return err
	}
	r, _ := c.rig(name)
	if _, err := os.Stat(filepath.Join(r.home, "grinds", "other.json")); err == nil {
		return fmt.Errorf("expected the landed commit not to be merged into the checkout")
	}
	return nil
}

func (c *gristLevelContext) smokes(app string, n int) error {
	made := 0
	for _, ran := range c.smoker.ran {
		if ran == app {
			made++
		}
	}
	if made != n {
		return fmt.Errorf("the grist smoke of %s was made %d times, want %d", app, made, n)
	}
	return nil
}

func (c *gristLevelContext) status() (string, error) {
	records, err := application.GristSmokeBook{Notes: c.notes, Host: "laptop"}.Records(context.Background())
	if err != nil {
		return "", err
	}
	return application.StatusReport{GristSmoke: records}.String(), nil
}

func (c *gristLevelContext) statusShows(said string) error {
	shown, err := c.status()
	if err != nil {
		return err
	}
	if !strings.Contains(shown, said) {
		return fmt.Errorf("mw status did not show %q:\n%s", said, shown)
	}
	return nil
}

func (c *gristLevelContext) statusDoesNotShow(said string) error {
	shown, err := c.status()
	if err != nil {
		return err
	}
	if strings.Contains(shown, said) {
		return fmt.Errorf("mw status showed %q:\n%s", said, shown)
	}
	return nil
}

func (c *gristLevelContext) mayorHas(n int, app string) error {
	inbox, err := c.mail.Inbox(context.Background(), application.MayorMailbox)
	if err != nil {
		return err
	}
	about := 0
	for _, m := range inbox {
		if strings.Contains(m.Subject, app) {
			about++
		}
	}
	if about != n {
		return fmt.Errorf("the Mayor has %d mail about the checkout of %s, want %d: %v", about, app, n, inbox)
	}
	return nil
}

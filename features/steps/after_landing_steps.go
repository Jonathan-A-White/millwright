package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

// afterLandingContext holds one mw after-landing scenario: a rig checkout that
// is only a directory (the command is a script that logs where it ran), the
// host's [after_landing] table, and the lock a landing can hold.
type afterLandingContext struct {
	root     string
	rigDir   string
	commands map[string]string
	retry    time.Duration

	// The deploy step: the step the host names as the one after which the rig is
	// deployed (zero when it names none), where the rig is remembered as deployed,
	// and the commit its checkout is at (mw-gq6.866).
	deployStep int
	marks      application.BuiltMarks
	head       string

	holding   application.Holding
	ranBeside *atomic.Bool
	letGo     sync.Once
	released  chan struct{} // closed when the landing lets go

	printed bytes.Buffer
	err     error
}

// InitializeAfterLandingScenario registers the steps of features/after_landing.feature.
func InitializeAfterLandingScenario(ctx *godog.ScenarioContext) {
	c := &afterLandingContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = afterLandingContext{ranBeside: &atomic.Bool{}, head: "abc1234def5678"}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.holding != nil {
			_ = c.holding.Release(context.Background())
		}
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a rig "([^"]*)" whose after-landing command succeeds$`, c.aRigThatSucceeds)
	ctx.Given(`^a rig "([^"]*)" whose after-landing command prints "([^"]*)" and exits (\d+)$`, c.aRigThatFails)
	ctx.Given(`^a rig "([^"]*)" whose after-landing command fails the first time on a name-resolution fault, exit 255, and then succeeds$`, c.aRigThatIsFlaky)
	ctx.Given(`^a rig "([^"]*)" with no after-landing command$`, c.aRigWithNoCommand)
	ctx.Given(`^a rig "([^"]*)" whose after-landing command has (\d+) steps, of which step (\d+) fails$`, c.aRigWithSteps)
	ctx.Given(`^a rig "([^"]*)" whose after-landing command has (\d+) steps, all of which pass$`, func(name string, steps int) error { return c.aRigWithSteps(name, steps, 0) })
	ctx.Given(`^the host names step (\d+) as the one after which the rig is deployed$`, c.theHostNamesTheDeployStep)
	ctx.Given(`^the rig is remembered as deployed at an older commit and as having failed to deploy$`, c.theRigHasOldMarks)
	ctx.Given(`^the command is run again after (\d+) milliseconds$`, c.theCommandIsRetriedAfter)
	ctx.Given(`^a landing holds the rig's after-landing lock until mw after-landing is waiting for it$`, c.aLandingHoldsTheLock)

	ctx.When(`^mw after-landing is run for "([^"]*)"$`, c.mwAfterLandingIsRun)

	ctx.Then(`^the command ran once, in the rig checkout$`, func() error { return c.theCommandRan(1) })
	ctx.Then(`^the command ran twice, in the rig checkout$`, func() error { return c.theCommandRan(2) })
	ctx.Then(`^the command did not run$`, c.theCommandDidNotRun)
	ctx.Then(`^the command did not start until the landing let go$`, c.theCommandWaited)
	ctx.Then(`^mw after-landing printed: (.+)$`, c.itPrinted)
	ctx.Then(`^the rig "([^"]*)" is remembered as deployed at the checkout's commit$`, c.rememberedDeployed)
	ctx.Then(`^the rig "([^"]*)" is remembered as deployed at the older commit still$`, c.rememberedDeployedOlder)
	ctx.Then(`^the rig "([^"]*)" is not remembered as having failed to deploy$`, c.notRememberedFailed)
	ctx.Then(`^the rig "([^"]*)" is remembered as having failed to deploy$`, c.rememberedFailed)
	ctx.Then(`^mw after-landing succeeded$`, c.itSucceeded)
	ctx.Then(`^mw after-landing failed, saying: (.+)$`, c.itFailed)

	registerAfterLandingSmokeSteps(ctx, c)
}

// script writes the command a scenario's rig names: it logs the directory it ran
// in to after.log and then does what body says.
func (c *afterLandingContext) script(name, body string) error {
	if c.root == "" {
		root, err := os.MkdirTemp("", "mw-after-landing-*")
		if err != nil {
			return err
		}
		c.root = root
	}
	c.rigDir = filepath.Join(c.root, "rigs", name)
	if err := os.MkdirAll(c.rigDir, 0o755); err != nil {
		return err
	}
	file := filepath.Join(c.root, "after.sh")
	text := "echo \"$PWD\" >> \"$(dirname \"$0\")/after.log\"\n" + body + "\n"
	if err := os.WriteFile(file, []byte(text), 0o755); err != nil {
		return err
	}
	c.commands = map[string]string{name: "sh " + file}
	return nil
}

func (c *afterLandingContext) aRigThatSucceeds(name string) error { return c.script(name, "exit 0") }

func (c *afterLandingContext) aRigThatFails(name, prints string, status int) error {
	return c.script(name, fmt.Sprintf("echo %q\nexit %d", prints, status))
}

func (c *afterLandingContext) aRigThatIsFlaky(name string) error {
	return c.script(name, `seen="$(dirname "$0")/seen"
if [ ! -e "$seen" ]; then
  touch "$seen"
  echo "ssh: Could not resolve hostname allmymind.org: Temporary failure in name resolution"
  exit 255
fi
exit 0`)
}

func (c *afterLandingContext) aRigWithNoCommand(name string) error {
	if err := c.script(name, "exit 0"); err != nil {
		return err
	}
	c.commands = map[string]string{}
	return nil
}

func (c *afterLandingContext) theCommandIsRetriedAfter(milliseconds int) error {
	c.retry = time.Duration(milliseconds) * time.Millisecond
	return nil
}

// slotPatience is how long a step waits on something that must happen,
// the landing letting go or the command being waited for: only a limit, for a
// scenario that has hung, and never the way the scenario learns that it has
// happened. It is far longer than any pause a loaded host has been seen to make
// (mw-gq6.316: a 5 s lock wait ran out under a parallel make test, and the command never started), so that a slow
// host is slow rather than wrong.
const slotPatience = 2 * time.Minute

func (c *afterLandingContext) slots(opts ...rig.SlotOption) *rig.Slots {
	opts = append([]rig.SlotOption{rig.WithSlotSuffix(rig.AfterLandingSlotSuffix), rig.WithSlotWait(slotPatience), rig.WithSlotPoll(20 * time.Millisecond)}, opts...)
	return rig.NewSlots(opts...)
}

// aLandingHoldsTheLock takes the lock now and gives it back the moment mw
// after-landing says it is waiting for it, noting whether the command had
// started by then. It lets go on that event and not on a timer, so that no host
// is too slow for it.
func (c *afterLandingContext) aLandingHoldsTheLock() error {
	held, err := c.slots().Take(context.Background(), c.rigDir, "mw next closing out a story")
	if err != nil {
		return err
	}
	c.holding = held
	c.released = make(chan struct{})
	return nil
}

// letTheLandingGo is told by the lock that mw after-landing is waiting for it:
// the landing gives the lock back, once.
func (c *afterLandingContext) letTheLandingGo(string) {
	if c.holding == nil {
		return
	}
	c.letGo.Do(func() {
		_, err := os.Stat(filepath.Join(c.root, "after.log"))
		c.ranBeside.Store(err == nil)
		_ = c.holding.Release(context.Background())
		close(c.released)
	})
}

func (c *afterLandingContext) mwAfterLandingIsRun(name string) error {
	c.printed.Reset()
	c.err = application.AfterLandingRun{
		AfterLanding: rig.NewAfterLanding(rig.WithAfterCommands(c.commands), rig.WithAfterDeploySteps(map[string]int{name: c.deployStep})),
		Slot:         c.slots(rig.WithSlotNotice(c.letTheLandingGo)),
		Rigs:         c.checkouts(name),
		Host:         "laptop",
		RetryWait:    c.retry,
		Built:        c.builtMarks(),
		Checkout:     headAt(c.head),
		Out:          &c.printed,
	}.Run(context.Background(), name)
	return nil
}

// spoken puts the command line in where the feature names it.
func (c *afterLandingContext) spoken(text string) string {
	return strings.ReplaceAll(text, afterCommandToken, c.commands["millwright"])
}

func (c *afterLandingContext) theCommandRan(times int) error {
	held, err := os.ReadFile(filepath.Join(c.root, "after.log"))
	if err != nil {
		return fmt.Errorf("expected the command to have run: %w", err)
	}
	want, err := filepath.EvalSymlinks(c.rigDir)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(held)), "\n")
	if len(lines) != times {
		return fmt.Errorf("expected %d runs, got %d: %q", times, len(lines), lines)
	}
	for _, line := range lines {
		got, err := filepath.EvalSymlinks(line)
		if err != nil || got != want {
			return fmt.Errorf("expected the command to run in %s, ran in %s", want, line)
		}
	}
	return nil
}

func (c *afterLandingContext) theCommandDidNotRun() error {
	if held, err := os.ReadFile(filepath.Join(c.root, "after.log")); err == nil {
		return fmt.Errorf("expected the command not to run, but it did: %q", held)
	}
	return nil
}

func (c *afterLandingContext) theCommandWaited() error {
	select {
	case <-c.released:
	case <-time.After(slotPatience):
		return fmt.Errorf("the landing never let go of the lock")
	}
	if c.ranBeside.Load() {
		return fmt.Errorf("the command had already started while the landing held the lock")
	}
	return nil
}

func (c *afterLandingContext) itPrinted(line string) error {
	if want := c.spoken(line); !strings.Contains(c.printed.String(), want) {
		return fmt.Errorf("expected mw after-landing to print %q, got:\n%s", want, c.printed.String())
	}
	return nil
}

func (c *afterLandingContext) itSucceeded() error {
	if c.err != nil {
		return fmt.Errorf("expected mw after-landing to succeed, got: %w", c.err)
	}
	return nil
}

func (c *afterLandingContext) itFailed(line string) error {
	want := c.spoken(line)
	if c.err == nil {
		return fmt.Errorf("expected mw after-landing to fail saying %q, but it succeeded", want)
	}
	if !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected mw after-landing to fail saying %q, got: %v", want, c.err)
	}
	return nil
}

// aRigWithSteps names a command of the given number of steps, each a plain `true`
// but the failing one, a `false`: the shell runs them as the &&s join them.
func (c *afterLandingContext) aRigWithSteps(name string, steps, failing int) error {
	if err := c.script(name, "exit 0"); err != nil {
		return err
	}
	words := make([]string, steps)
	for i := range words {
		words[i] = "true"
		if i+1 == failing {
			words[i] = "false"
		}
	}
	c.commands = map[string]string{name: strings.Join(words, " && ")}
	return nil
}

func (c *afterLandingContext) theHostNamesTheDeployStep(step int) error {
	c.deployStep = step
	return nil
}

func (c *afterLandingContext) builtMarks() application.BuiltMarks {
	if c.marks == nil {
		if c.root == "" {
			root, err := os.MkdirTemp("", "mw-after-landing-*")
			if err != nil {
				return nil
			}
			c.root = root
		}
		c.marks = rig.NewBuiltMarks(filepath.Join(c.root, "state"))
	}
	return c.marks
}

func (c *afterLandingContext) theRigHasOldMarks() error {
	if err := c.marks0("postern.deployed", "0ld0ld0ld0ld"); err != nil {
		return err
	}
	return c.marks0("postern.deploy-failed", c.head+" 1")
}

func (c *afterLandingContext) marks0(key, value string) error {
	return c.builtMarks().MarkBuilt(context.Background(), key, value)
}

func (c *afterLandingContext) mark(key string) string {
	held, _ := c.builtMarks().Built(context.Background(), key)
	return held
}

func (c *afterLandingContext) rememberedDeployed(name string) error {
	if got := c.mark(name + ".deployed"); got != c.head {
		return fmt.Errorf("expected %s to be remembered as deployed at %s, got %q", name, c.head, got)
	}
	return nil
}

func (c *afterLandingContext) rememberedDeployedOlder(name string) error {
	if got := c.mark(name + ".deployed"); got != "" && got != "0ld0ld0ld0ld" {
		return fmt.Errorf("expected %s to be remembered as deployed at the older commit still or at none, got %q", name, got)
	}
	return nil
}

func (c *afterLandingContext) notRememberedFailed(name string) error {
	if got := c.mark(name + ".deploy-failed"); got != "" {
		return fmt.Errorf("expected %s not to be remembered as having failed to deploy, got %q", name, got)
	}
	return nil
}

func (c *afterLandingContext) rememberedFailed(name string) error {
	if got := c.mark(name + ".deploy-failed"); got == "" {
		return fmt.Errorf("expected %s to be remembered as having failed to deploy", name)
	}
	return nil
}

// headAt is a checkout that is at a commit and does nothing else.
type headAt string

func (h headAt) Fetch(context.Context, string) error { return nil }
func (h headAt) Tip(context.Context, string, string, string) (string, error) {
	return string(h), nil
}
func (h headAt) Head(context.Context, string) (string, error)          { return string(h), nil }
func (h headAt) Uncommitted(context.Context, string) ([]string, error) { return nil, nil }
func (h headAt) Advance(context.Context, string, string, string) (application.Advanced, error) {
	return application.Advanced{}, nil
}

// checkouts is where the rigs are checked out: millwright always, and the rig
// asked for when the host names a command for it.
func (c *afterLandingContext) checkouts(name string) map[string]string {
	rigs := map[string]string{"millwright": c.rigDir}
	if _, named := c.commands[name]; named {
		rigs[name] = c.rigDir
	}
	return rigs
}

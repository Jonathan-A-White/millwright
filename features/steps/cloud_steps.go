package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/domain/events"

	"github.com/cucumber/godog"
)

// cloudContext is features/cloud.feature: the cloud check over a fake tracker,
// a fake provider, a fake book in the vault, a fake event log and a clock the
// scenario moves. It is built by the Background's first Given, never in a
// Before hook, which would run for every feature's scenarios.
type cloudContext struct {
	plan     application.CloudPlan
	tracker  *apptest.FakeTracker
	provider *apptest.FakeCloud
	book     *apptest.FakeCloudBook
	log      *apptest.FakeEventLog
	notice   *apptest.FakeCloudNotice
	now      time.Time
	stories  int
	said     strings.Builder
	report   string
}

// InitializeCloudScenario registers the steps of features/cloud.feature.
func InitializeCloudScenario(ctx *godog.ScenarioContext) {
	c := &cloudContext{}

	ctx.Given(`^a cloud of at most (\d+) boxes, capped at \$([0-9.]+) a month, idle after (\d+) minutes, at \$([0-9.]+) an hour$`, c.aCloud)
	ctx.Given(`^the hosts "([^"]*)" and "([^"]*)" run (\d+) sessions each, and a cloud box runs (\d+)$`, c.theHosts)
	ctx.Given(`^the cloud's clock reads "([^"]*)"$`, func(at string) (err error) {
		c.now, err = time.Parse(time.RFC3339, at)
		return err
	})
	ctx.Given(`^every host is full$`, func() error {
		for host, n := range c.plan.HostCaps {
			c.run(host, n)
		}
		return nil
	})
	ctx.Given(`^"([^"]*)" runs (\d+) story and "([^"]*)" runs (\d+)$`, func(a string, n int, b string, m int) error {
		c.run(a, n)
		c.run(b, m)
		return nil
	})
	ctx.Given(`^(\d+) stor(?:y|ies) waits? for any host$`, func(n int) error {
		c.wait(domain.HostAuto, n)
		return nil
	})
	ctx.Given(`^(\d+) stories wait for "([^"]*)"$`, func(n int, host string) error {
		c.wait(host, n)
		return nil
	})
	ctx.Given(`^"([^"]*)" runs (\d+) stories$`, c.boxTakes)
	ctx.Given(`^the stories on "([^"]*)" are finished$`, c.finished)
	ctx.Given(`^the vault counts (\d+) box hours this month$`, func(hours int) error {
		c.book.State.Month = c.now.UTC().Format("2006-01")
		c.book.State.Hours = hours
		return nil
	})
	ctx.Given(`^the provider's billing says \$([0-9.]+) this month$`, func(usd float64) error {
		c.provider.BilledUSD, c.provider.BilledKnown = usd, true
		return nil
	})
	ctx.Given(`^the provider fails to make a box (\d+) times?$`, func(n int) error {
		c.provider.FailUps = n
		return nil
	})

	ctx.Given(`^Vultr holds "([^"]*)" that no state knows$`, func(label string) error {
		c.provider.Orphans = append(c.provider.Orphans, application.CloudInstance{ID: "id-" + label, Label: label, Status: "active"})
		return nil
	})
	ctx.Given(`^Vultr will not destroy an instance yet, for it is locked$`, func() error {
		c.provider.ReapErr = application.ClassifyCloudError(fmt.Errorf("vultr-boost down: exit status 1: vultr-boost: Vultr would not delete instance (locked, HTTP 409)"), "")
		return nil
	})
	ctx.When(`^Vultr can destroy instances again$`, func() error {
		c.provider.ReapErr = nil
		return nil
	})
	ctx.Given(`^Vultr refuses to make a box: "([^"]*)"$`, func(said string) error {
		c.provider.FailUps = 99
		c.provider.FailUpWith = application.ClassifyCloudError(fmt.Errorf("vultr-boost up: exit status 1: Error: %s", said), said)
		return nil
	})
	ctx.Given(`^a box is given (\d+) minutes to come up$`, func(n int) error {
		c.plan.Up = time.Duration(n) * time.Minute
		return nil
	})
	ctx.Given(`^Vultr is still restoring the box when the make's deadline comes$`, func() error {
		c.provider.DeadlineUps = 1
		return nil
	})
	ctx.When(`^Vultr finishes restoring "([^"]*)"$`, func(label string) error {
		c.provider.FinishRestore(label)
		return nil
	})
	ctx.When(`^Vultr loses "([^"]*)"$`, func(label string) error {
		return c.provider.Lose(label)
	})
	ctx.Given(`^the wrapper finds no state for a box it is asked to destroy$`, func() error {
		c.provider.DownKeeps = true
		return nil
	})
	ctx.Given(`^the failed make leaves its instance on Vultr$`, func() error {
		c.provider.UpLeaves = true
		return nil
	})
	ctx.Given(`^the wrapper says a make is done though Vultr holds no box, (\d+) times?$`, func(n int) error {
		c.provider.VanishingUps = n
		return nil
	})
	ctx.Given(`^the cloud check runs and the box is still being made$`, c.checkOnly)
	ctx.When(`^the cloud check runs and the box is still being made$`, c.checkOnly)
	ctx.When(`^the cloud check runs and its caller is cancelled (\d+) seconds into the apply$`, c.checkCancelled)
	ctx.When(`^the box being made comes up$`, c.boxesComeUp)
	ctx.When(`^the process making the box dies$`, func() error {
		c.provider.TakeBegun()
		return nil
	})
	ctx.When(`^a minute passes and the cloud check runs and the box is still being made$`, func() error {
		c.now = c.now.Add(time.Minute)
		return c.checkOnly()
	})
	ctx.Given(`^the process making the box dies$`, func() error {
		c.provider.TakeBegun()
		return nil
	})
	ctx.When(`^(\d+) minutes pass and the cloud check runs and the box is still being made$`, func(n int) error {
		c.now = c.now.Add(time.Duration(n) * time.Minute)
		return c.checkOnly()
	})
	ctx.Given(`^the cloud check runs$`, c.check)
	ctx.When(`^the cloud check runs$`, c.check)
	ctx.When(`^the cloud check runs (\d+) times, a minute apart$`, func(n int) error {
		for i := 0; i < n; i++ {
			if i > 0 {
				c.now = c.now.Add(time.Minute)
			}
			if err := c.check(); err != nil {
				return err
			}
		}
		return nil
	})
	passAndCheck := func(n int) error {
		c.now = c.now.Add(time.Duration(n) * time.Minute)
		return c.check()
	}
	ctx.Given(`^(\d+) minutes pass and the cloud check runs$`, passAndCheck)
	ctx.When(`^(\d+) minutes pass and the cloud check runs$`, passAndCheck)
	ctx.When(`^a minute passes and the cloud check runs$`, func() error { return passAndCheck(1) })
	pass := func(n int) error {
		c.now = c.now.Add(time.Duration(n) * time.Minute)
		return nil
	}
	ctx.Given(`^(\d+) minutes pass on the cloud's clock$`, pass)
	ctx.When(`^(\d+) minutes pass on the cloud's clock$`, pass)

	ctx.Then(`^the provider was asked nothing$`, func() error { return c.asked(nil) })
	ctx.Then(`^the provider was asked to make "([^"]*)" and nothing else$`, func(name string) error {
		return c.asked([]string{"up " + name})
	})
	ctx.Then(`^the provider was asked to make "([^"]*)", "([^"]*)" and nothing else$`, func(a, b string) error {
		return c.asked([]string{"up " + a, "up " + b})
	})
	ctx.Then(`^the provider was asked to (make|destroy|adopt) "([^"]*)", (make|destroy|adopt) "([^"]*)"$`, func(v1, n1, v2, n2 string) error {
		return c.asked([]string{c.verb(v1) + n1, c.verb(v2) + n2})
	})
	ctx.Then(`^the provider was asked to (make|destroy) "([^"]*)", (make|destroy) "([^"]*)", (make|destroy) "([^"]*)"$`, func(v1, n1, v2, n2, v3, n3 string) error {
		return c.asked([]string{c.verb(v1) + n1, c.verb(v2) + n2, c.verb(v3) + n3})
	})
	ctx.Then(`^the provider was asked to (make|destroy) "([^"]*)", (make|destroy) "([^"]*)", (make|destroy) "([^"]*)", (make|destroy) "([^"]*)"$`, func(v1, n1, v2, n2, v3, n3, v4, n4 string) error {
		return c.asked([]string{c.verb(v1) + n1, c.verb(v2) + n2, c.verb(v3) + n3, c.verb(v4) + n4})
	})
	ctx.Then(`^the provider was asked to destroy "([^"]*)"$`, func(name string) error {
		for _, call := range c.provider.Calls() {
			if call == "down "+name {
				return nil
			}
		}
		return fmt.Errorf("the provider was asked %q, with no down %s", c.provider.Calls(), name)
	})
	ctx.Then(`^Vultr's "([^"]*)" was destroyed$`, func(label string) error {
		for _, call := range c.provider.Calls() {
			if call == "reap "+label {
				return nil
			}
		}
		return fmt.Errorf("the provider was asked %q, with no reap %s", c.provider.Calls(), label)
	})
	ctx.Then(`^the provider was asked to destroy "([^"]*)" by its id (\d+) times?$`, func(label string, n int) error {
		got := 0
		for _, call := range c.provider.Calls() {
			if call == "reap "+label {
				got++
			}
		}
		if got != n {
			return fmt.Errorf("reap %s asked %d times, want %d: %q", label, got, n, c.provider.Calls())
		}
		return nil
	})
	ctx.Then(`^the provider made "([^"]*)" (\d+) times?$`, func(name string, n int) error {
		got := 0
		for _, call := range c.provider.Calls() {
			if call == "up "+name {
				got++
			}
		}
		if got != n {
			return fmt.Errorf("up %s asked %d times, want %d: %q", name, got, n, c.provider.Calls())
		}
		return nil
	})
	ctx.Then(`^Vultr holds (\d+) instances? labelled "([^"]*)"$`, func(n int, label string) error {
		if got := c.provider.Held(label); got != n {
			return fmt.Errorf("Vultr holds %d instances labelled %s, want %d\n%s", got, label, n, c.said.String())
		}
		return nil
	})
	ctx.Then(`^the book holds "([^"]*)" restoring$`, func(name string) error {
		for _, box := range c.book.State.Boxes {
			if box.Name == name && box.Restoring && box.InstanceID != "" && !box.RestoringSince.IsZero() {
				return nil
			}
		}
		return fmt.Errorf("the book holds no %s restoring, with its id and since: %+v", name, c.book.State.Boxes)
	})
	ctx.Then(`^the book holds no box restoring$`, func() error {
		for _, box := range c.book.State.Boxes {
			if box.Restoring {
				return fmt.Errorf("the book holds %s restoring: %+v", box.Name, c.book.State.Boxes)
			}
		}
		return nil
	})
	ctx.Then(`^the Governor was sent (\d+) cards?$`, func(n int) error {
		if got := len(c.notice.Cards); got != n {
			return fmt.Errorf("%d cards were sent, want %d: %q", got, n, c.notice.Cards)
		}
		return nil
	})
	ctx.Then(`^the Governor's card says "([^"]*)"$`, func(text string) error {
		for _, card := range c.notice.Cards {
			if strings.Contains(card, text) {
				return nil
			}
		}
		return fmt.Errorf("no card says %q: %q", text, c.notice.Cards)
	})
	ctx.Then(`^(\d+) cloud box(?:es)? (?:is|are) up$`, func(n int) error {
		got := 0
		for _, box := range c.book.State.Boxes {
			if !box.Making && !box.Restoring {
				got++
			}
		}
		if got != n {
			return fmt.Errorf("%d cloud boxes are up, want %d: %+v\n%s", got, n, c.book.State.Boxes, c.said.String())
		}
		return nil
	})
	ctx.Then(`^a cloud event says "([^"]*)"$`, func(text string) error {
		if c.cloudEvents(text) == 0 {
			return fmt.Errorf("no cloud event says %q; the log holds %s", text, c.events())
		}
		return nil
	})
	ctx.Then(`^(\d+) cloud events? says? "([^"]*)"$`, func(n int, text string) error {
		if got := c.cloudEvents(text); got != n {
			return fmt.Errorf("%d cloud events say %q, want %d; the log holds %s", got, text, n, c.events())
		}
		return nil
	})
	ctx.Then(`^the cloud check said "([^"]*)"$`, func(text string) error {
		if !strings.Contains(c.report, text) {
			return fmt.Errorf("the cloud check did not say %q:\n%s", text, c.report)
		}
		return nil
	})
	ctx.Then(`^the book holds "([^"]*)" being made$`, func(name string) error {
		for _, box := range c.book.State.Boxes {
			if box.Name == name && box.Making {
				return nil
			}
		}
		return fmt.Errorf("the book holds no %s being made: %+v", name, c.book.State.Boxes)
	})
	ctx.Then(`^the book holds no box being made$`, func() error {
		for _, box := range c.book.State.Boxes {
			if box.Making {
				return fmt.Errorf("the book holds %s being made: %+v", box.Name, c.book.State.Boxes)
			}
		}
		return nil
	})
	ctx.Then(`^mw status says "([^"]*)"$`, c.statusSays)
}

func (c *cloudContext) aCloud(boxes int, capUSD float64, idle int, hourly float64) error {
	c.plan = application.CloudPlan{
		MaxBoxes:      boxes,
		MonthlyCapUSD: capUSD,
		Idle:          time.Duration(idle) * time.Minute,
		HourlyUSD:     hourly,
	}
	c.tracker = apptest.NewFakeTracker()
	c.tracker.AddEpic("mw-c", domain.Path{Rig: "millwright", Branch: "main", Host: domain.HostAuto})
	c.provider = &apptest.FakeCloud{}
	c.book = &apptest.FakeCloudBook{}
	c.log = &apptest.FakeEventLog{}
	c.notice = &apptest.FakeCloudNotice{}
	c.stories = 0
	c.said.Reset()
	return nil
}

func (c *cloudContext) theHosts(a, b string, each, box int) error {
	c.plan.HostCaps = map[string]int{a: each, b: each}
	c.plan.BoxCap = box
	return nil
}

// story files one more story on the epic, pathed to host.
func (c *cloudContext) story(host string) string {
	c.stories++
	id := fmt.Sprintf("mw-c.%d", c.stories)
	c.tracker.AddStory("mw-c", domain.Story{ID: id, Title: "story " + id})
	_ = c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{"host": host})
	return id
}

// run claims n new stories on host, as its dispatch would.
func (c *cloudContext) run(host string, n int) {
	for i := 0; i < n; i++ {
		_ = c.tracker.ClaimAs(c.story(host), "mw@"+host, c.now.Add(5*time.Minute))
	}
}

func (c *cloudContext) wait(host string, n int) {
	for i := 0; i < n; i++ {
		c.story(host)
	}
}

// boxTakes has the box claim n of the stories waiting for any host.
func (c *cloudContext) boxTakes(box string, n int) error {
	work, err := c.tracker.WorkInHand(context.Background())
	if err != nil {
		return err
	}
	for _, d := range work.Ready {
		if n == 0 {
			break
		}
		if d.Merged().Host != domain.HostAuto {
			continue
		}
		id := d.Story.ID
		_ = c.tracker.SetStoryMetadata(context.Background(), id, map[string]string{"host": box})
		_ = c.tracker.ClaimAs(id, "mw@"+box, c.now.Add(5*time.Minute))
		n--
	}
	if n > 0 {
		return fmt.Errorf("too few stories wait for %s to take", box)
	}
	return nil
}

func (c *cloudContext) finished(box string) error {
	work, err := c.tracker.WorkInHand(context.Background())
	if err != nil {
		return err
	}
	for _, d := range work.RunningOn(box) {
		if err := c.tracker.SetStatus(d.Story.ID, apptest.StatusClosed); err != nil {
			return err
		}
	}
	return nil
}

// check is a cloud check whose box, if one is begun, comes up before the
// scenario goes on, as the detached make would meanwhile.
func (c *cloudContext) check() error {
	if err := c.checkOnly(); err != nil {
		return err
	}
	return c.boxesComeUp()
}

// checkOnly is a cloud check that leaves the box it begins still being made.
func (c *cloudContext) checkOnly() error {
	return c.checkIn(context.Background())
}

func (c *cloudContext) checkIn(ctx context.Context) error {
	report, err := application.CloudCheck{
		Work:     c.tracker,
		Provider: c.provider,
		Book:     c.book,
		Log:      c.log,
		Notice:   c.notice,
		Host:     "desktop",
		Plan:     c.plan,
		Now:      func() time.Time { return c.now },
		Out:      &c.said,
	}.Run(ctx)
	c.report = report.String()
	if err != nil {
		return fmt.Errorf("the cloud check failed: %v\n%s", err, c.said.String())
	}
	return nil
}

// checkCancelled is a cloud check whose caller is cancelled secs seconds after
// it begins a box: the make, detached from it, goes on and finishes.
func (c *cloudContext) checkCancelled(secs int) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.checkIn(ctx); err != nil {
		return err
	}
	c.now = c.now.Add(time.Duration(secs) * time.Second)
	cancel()
	return c.boxesComeUp()
}

// boxesComeUp runs each make the provider was asked to begin, as the process
// it detached would, with a context of its own.
func (c *cloudContext) boxesComeUp() error {
	for _, name := range c.provider.TakeBegun() {
		err := application.CloudMake{
			Provider: c.provider,
			Book:     c.book,
			Log:      c.log,
			Notice:   c.notice,
			Host:     "desktop",
			Plan:     c.plan,
			Now:      func() time.Time { return c.now },
			Out:      &c.said,
		}.Run(context.Background(), name)
		if err != nil {
			return fmt.Errorf("making %s failed: %v\n%s", name, err, c.said.String())
		}
	}
	return nil
}

func (c *cloudContext) verb(v string) string {
	switch v {
	case "make":
		return "up "
	case "adopt":
		return "adopt "
	}
	return "down "
}

func (c *cloudContext) asked(want []string) error {
	got := c.provider.Calls()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		return fmt.Errorf("the provider was asked %q, want %q\n%s", got, want, c.said.String())
	}
	return nil
}

func (c *cloudContext) cloudEvents(text string) int {
	n := 0
	evs, _ := c.log.Since(context.Background(), 0)
	for _, e := range evs {
		if e.Kind == events.KindCloud && strings.Contains(e.Detail, text) {
			n++
		}
	}
	return n
}

func (c *cloudContext) events() string {
	evs, _ := c.log.Since(context.Background(), 0)
	var lines []string
	for _, e := range evs {
		lines = append(lines, application.EventLine(e))
	}
	return "\n" + strings.Join(lines, "\n")
}

func (c *cloudContext) statusSays(text string) error {
	reading, err := application.ReadCloud(context.Background(), c.book, c.plan, c.now)
	if err != nil {
		return err
	}
	report := application.StatusReport{Host: "desktop", Cloud: reading}.String()
	if !strings.Contains(report, text) {
		return fmt.Errorf("mw status does not say %q:\n%s", text, report)
	}
	return nil
}

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// registerCapSteps adds the steps of features/cloud_cap.feature to the cloud
// context: a changed box_cap reaching the boxes already up.
func (c *cloudContext) registerCapSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^the cloud boxes "([^"]*)" and "([^"]*)" are up at a cap of (\d+), dispatching$`, func(a, b string, cap int) error {
		for _, name := range []string{a, b} {
			c.book.State.Boxes = append(c.book.State.Boxes, application.CloudBox{
				Name: name, Up: c.now.Add(-time.Hour), Made: c.now.Add(-time.Hour), Dispatched: c.now.Add(-30 * time.Minute),
				IdleSince: c.now, Cap: cap,
			})
		}
		c.book.State.Month = c.now.UTC().Format("2006-01")
		return nil
	})
	ctx.Given(`^a box "([^"]*)" that is made and has not dispatched, at a cap of (\d+)$`, func(name string, cap int) error {
		c.book.State.Boxes = append(c.book.State.Boxes, application.CloudBox{
			Name: name, Up: c.now.Add(-time.Minute), Made: c.now.Add(-time.Minute), IdleSince: c.now, Cap: cap,
		})
		c.book.State.Month = c.now.UTC().Format("2006-01")
		return nil
	})
	ctx.Given(`^"([^"]*)" cannot be reached$`, func(name string) error {
		if c.provider.Unreachable == nil {
			c.provider.Unreachable = map[string]bool{}
		}
		c.provider.Unreachable[name] = true
		return nil
	})
	ctx.When(`^"([^"]*)" can be reached again and a minute passes and the cloud check runs$`, func(name string) error {
		delete(c.provider.Unreachable, name)
		c.now = c.now.Add(time.Minute)
		return c.check()
	})
	ctx.When(`^box_cap is changed to (\d+) and the cloud check runs$`, func(n int) error {
		c.plan.BoxCap = n
		return c.check()
	})

	ctx.Then(`^the provider set the cap of "([^"]*)" to (\d+) and the cap of "([^"]*)" to (\d+)$`, func(a string, n int, b string, m int) error {
		return c.capCalls([]string{fmt.Sprintf("%s %d", a, n), fmt.Sprintf("%s %d", b, m)})
	})
	ctx.Then(`^the provider was asked to set the cap of nothing$`, func() error { return c.capCalls(nil) })
	ctx.Then(`^the provider has been asked to set (\d+) caps in all$`, func(n int) error {
		if got := c.provider.CapCalls(); len(got) != n {
			return fmt.Errorf("the provider was asked to set caps %q, want %d in all", got, n)
		}
		return nil
	})
	ctx.Then(`^the book holds a move for each of "([^"]*)" and "([^"]*)" saying its cap went from (\d+) to (\d+)$`, func(a, b string, was, now int) error {
		for _, name := range []string{a, b} {
			want := fmt.Sprintf("%s: cap %d to %d", name, was, now)
			found := false
			for _, m := range c.book.State.Moves {
				found = found || m.What == want
			}
			if !found {
				return fmt.Errorf("the book holds no move %q: %+v", want, c.book.State.Moves)
			}
		}
		return nil
	})
	ctx.Then(`^the book says "([^"]*)" runs (\d+) sessions at once$`, func(name string, n int) error {
		for _, box := range c.book.State.Boxes {
			if box.Name == name {
				if box.Cap != n {
					return fmt.Errorf("the book says %s runs %d sessions at once, want %d", name, box.Cap, n)
				}
				return nil
			}
		}
		return fmt.Errorf("the book holds no %s: %+v", name, c.book.State.Boxes)
	})
	ctx.Then(`^"([^"]*)" still runs (\d+) stories$`, func(box string, n int) error {
		work, err := c.tracker.WorkInHand(context.Background())
		if err != nil {
			return err
		}
		if got := len(work.RunningOn(box)); got != n {
			return fmt.Errorf("%s runs %d stories, want %d", box, got, n)
		}
		return nil
	})
	ctx.Then(`^mw status does not say "([^"]*)"$`, func(text string) error {
		reading, err := application.ReadCloud(context.Background(), c.book, c.plan, c.now)
		if err != nil {
			return err
		}
		if report := (application.StatusReport{Host: "desktop", Cloud: reading}).String(); strings.Contains(report, text) {
			return fmt.Errorf("mw status says %q:\n%s", text, report)
		}
		return nil
	})
}

func (c *cloudContext) capCalls(want []string) error {
	if got := c.provider.CapCalls(); strings.Join(got, "|") != strings.Join(want, "|") {
		return fmt.Errorf("the provider was asked to set caps %q, want %q\n%s", got, want, c.said.String())
	}
	return nil
}

// cloudCapContext is features/cloud_cap_command.feature: mw cloud cap over a
// fake home file and fake files.
type cloudCapContext struct {
	files *apptest.FakeCloudCapFiles
	host  string
	home  string
	out   string
	err   error
}

// InitializeCloudCapScenario registers the steps of features/cloud_cap_command.feature.
func InitializeCloudCapScenario(ctx *godog.ScenarioContext) {
	c := &cloudCapContext{}
	ctx.Given(`^the cap command runs on the home "([^"]*)", where box_cap is (\d+)$`, func(host string, was int) error {
		c.files, c.host, c.home = &apptest.FakeCloudCapFiles{Was: was}, host, host
		return nil
	})
	ctx.Given(`^the cap command runs on "([^"]*)", where "([^"]*)" is home and box_cap is (\d+)$`, func(host, home string, was int) error {
		c.files, c.host, c.home = &apptest.FakeCloudCapFiles{Was: was}, host, home
		return nil
	})
	ctx.When(`^mw cloud cap (\d+) is run$`, func(n int) error {
		report, err := application.CloudCap{
			Files: c.files,
			Home:  &apptest.FakeHomeFile{Text: c.home + " 2026-10-15T17:03:00Z mw@" + c.home},
			Host:  c.host,
		}.Run(context.Background(), n)
		c.out, c.err = report.String(), err
		return nil
	})
	ctx.Then(`^box_cap was written as (\d+) in each place the cloud keeps it$`, func(n int) error {
		if c.err != nil || len(c.files.Written) != 1 || c.files.Written[0] != n {
			return fmt.Errorf("box_cap was written as %v, want [%d] (error %v)", c.files.Written, n, c.err)
		}
		return nil
	})
	ctx.Then(`^nothing was written as box_cap$`, func() error {
		if len(c.files.Written) != 0 {
			return fmt.Errorf("box_cap was written as %v", c.files.Written)
		}
		return nil
	})
	ctx.Then(`^the cap command printed "([^"]*)"$`, func(text string) error {
		if c.err != nil || !strings.Contains(c.out, text) {
			return fmt.Errorf("the cap command did not print %q (error %v):\n%s", text, c.err, c.out)
		}
		return nil
	})
	ctx.Then(`^the cap command was refused saying "([^"]*)"$`, func(text string) error {
		if c.err == nil || !strings.Contains(c.err.Error(), text) {
			return fmt.Errorf("the cap command was not refused saying %q: %v", text, c.err)
		}
		return nil
	})
}

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

// smokeWatch is the grist smoke of a batch landing, standing in for the real
// one: where the real smoke sends every example for minutes, this one looks at
// the world while it would be sending (mw-gq6.626) — the Landed mails already
// sent, whether the rig's merge slot can be taken, and what mw land had printed.
type smokeWatch struct {
	failing bool
	ran     bool

	mails   []string // the subjects of the Landed mails sent when the smoke began
	slotErr error    // why the merge slot could not be taken while the smoke ran, nil when it could
	printed string   // what mw land had printed when the smoke began
}

func (w *smokeWatch) Run(_ context.Context, app, _ string) (application.GristSmokeReport, error) {
	w.ran = true
	report := application.GristSmokeReport{App: app, Examples: 1}
	if w.failing {
		report.Failures = []string{app + "/tutor/hello: the answer has no name"}
	}
	return report, nil
}

// watching is the grist smoke the close-out is given, which looks at the world
// as it runs.
type watchingSmoker struct {
	w *smokeWatch
	c *nextContext
}

func (s watchingSmoker) Run(ctx context.Context, app, kind string) (application.GristSmokeReport, error) {
	c, w := s.c, s.w
	sent, err := c.mailSent()
	if err == nil {
		for _, m := range sent {
			if strings.HasPrefix(m.Subject, application.MailLanded+":") {
				w.mails = append(w.mails, m.Subject)
			}
		}
	}
	w.printed = c.printed.String()[c.batch.printedFrom:]
	taking, cancel := context.WithTimeout(ctx, 3*time.Second)
	holding, err := c.mergeSlot().Take(taking, c.rig, "a second mw land, while the first one's smoke runs")
	cancel()
	if err != nil {
		w.slotErr = err
	} else {
		_ = holding.Release(ctx)
	}
	return w.Run(ctx, app, kind)
}

// smokeAfter is the smoke a batch landing makes with the watcher in it: the rig
// is the app's own, and every landing is taken to have changed its grist.
func (c *nextContext) smokeAfter() application.GristSmokeAfter {
	return application.GristSmokeAfter{
		Touches: allTouched{}, Smoke: watchingSmoker{w: c.smokeWatch, c: c},
		Book: application.GristSmokeBook{Notes: apptest.NewFakeTracker(), Host: nextHost},
		Apps: map[string]string{c.rigKey(): c.rig}, Rigs: map[string]string{c.rigKey(): c.rig},
	}
}

// homeReader is the home file a scenario set, nil when it set none.
func (c *nextContext) homeReader() application.HomeFile {
	if c.home == "" {
		return nil
	}
	return c.home
}

// apartSmokes stands in for the process mw land hands the grist smoke of a
// landed batch to: it is begun and goes on, not ended, until the scenario ends
// it (mw-apvyg8j).
type apartSmokes struct {
	begun []application.LandedSmoke
	ended int
}

func (a *apartSmokes) Begin(_ context.Context, smoke application.LandedSmoke) error {
	a.begun = append(a.begun, smoke)
	return nil
}

func registerBatchLandSmokeSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the batch's landing changes the rig's grist, and its smoke fails, begun apart and not ended until the scenario ends it$`, func() error {
		c.smokeWatch = &smokeWatch{failing: true}
		c.apartSmokes = &apartSmokes{}
		return nil
	})
	ctx.Then(`^the smoke of batch (\d+) was begun apart and had not run when mw land ended$`, func(number int) error {
		a, w := c.apartSmokes, c.smokeWatch
		if a == nil || len(a.begun) < number || a.begun[number-1].Number != number {
			return fmt.Errorf("expected the smoke of batch %d begun apart, got %+v\n%s", number, c.apartSmokes, c.said())
		}
		if w.ran {
			return fmt.Errorf("expected the smoke to be going on apart, not made by mw land\n%s", c.said())
		}
		return nil
	})
	ctx.Then(`^mw land said the smoke was begun apart$`, func() error {
		printed := c.printed.String()[c.batch.printedFrom:]
		if !strings.Contains(printed, "grist smoke: begun apart") {
			return fmt.Errorf("expected mw land to say the smoke was begun apart, it said:\n%s", printed)
		}
		return nil
	})
	ctx.When(`^the smokes begun apart end$`, func() error {
		a := c.apartSmokes
		if a == nil {
			return fmt.Errorf("the scenario begun no smoke apart")
		}
		for ; a.ended < len(a.begun); a.ended++ {
			err := application.BatchLand{Next: c.nextUseCase()}.SmokeLanded(context.Background(), a.begun[a.ended])
			if err != nil {
				return err
			}
		}
		return nil
	})
	ctx.Then(`^the smoke of each batch was made and the Mayor was mailed that the smoke of batch (\d+) failed$`, func(number int) error {
		w := c.smokeWatch
		if w == nil || !w.ran {
			return fmt.Errorf("the grist smoke never ran\n%s", c.said())
		}
		sent, err := c.mailSent()
		if err != nil {
			return err
		}
		want := application.MailSmokeFailed + ": " + application.BatchLabel(c.rigKey(), number)
		for _, m := range sent {
			if m.Subject == want {
				return nil
			}
		}
		return fmt.Errorf("expected a %q mail, got %+v", want, sent)
	})
	ctx.Given(`^the batch's landing changes the rig's grist, and this host names no \[grist-apps\]$`, func() error {
		c.smokeUnhosted = true
		return nil
	})
	ctx.Given(`^the batch's vault names "([^"]*)" as the home$`, func(host string) error {
		c.home = homeFile(host + " 2026-10-01T05:00:00Z mayor\n")
		return nil
	})
	ctx.Given(`^the batch's landing changes the rig's grist, and its smoke looks at the world as it runs$`, func() error {
		c.smokeWatch = &smokeWatch{}
		return nil
	})
	ctx.Given(`^the batch's landing changes the rig's grist, and its smoke fails$`, func() error {
		c.smokeWatch = &smokeWatch{failing: true}
		return nil
	})
	ctx.Then(`^the Landed mail of each of these stories was already sent when the smoke began:$`, func(table *godog.Table) error {
		w := c.smokeWatch
		if w == nil || !w.ran {
			return fmt.Errorf("the grist smoke never ran\n%s", c.said())
		}
		for _, id := range tableIDs(table) {
			found := false
			for _, subject := range w.mails {
				found = found || strings.Contains(subject, id)
			}
			if !found {
				return fmt.Errorf("expected the Landed mail of %s sent before the smoke began, got %v\n%s", id, w.mails, c.said())
			}
		}
		return nil
	})
	ctx.Then(`^the rig's merge slot was free while the smoke ran$`, func() error {
		w := c.smokeWatch
		if w == nil || !w.ran {
			return fmt.Errorf("the grist smoke never ran\n%s", c.said())
		}
		if w.slotErr != nil {
			return fmt.Errorf("expected a second mw land to take the merge slot while the smoke ran: %v", w.slotErr)
		}
		return nil
	})
	ctx.Then(`^mw land had printed a line for each step before the smoke began: "([^"]*)", "([^"]*)", "([^"]*)"$`, func(landed, closed, mailed string) error {
		w := c.smokeWatch
		if w == nil || !w.ran {
			return fmt.Errorf("the grist smoke never ran\n%s", c.said())
		}
		for _, words := range []string{landed, closed, mailed} {
			if !strings.Contains(w.printed, words) {
				return fmt.Errorf("expected mw land to have printed %q before the smoke began, it had printed:\n%s", words, w.printed)
			}
		}
		return nil
	})
	ctx.Then(`^mw land prints the smoke's result after the mails$`, func() error {
		printed := c.printed.String()[c.batch.printedFrom:]
		mailed, result := strings.LastIndex(printed, application.BatchLogPrefix+"mailed "), strings.Index(printed, application.BatchLogPrefix+"grist smoke:")
		if mailed < 0 || result < 0 || result < mailed {
			return fmt.Errorf("expected a line for the smoke's result after the mails\n%s", printed)
		}
		return nil
	})
	ctx.Then(`^the Mayor is mailed that the grist smoke of the batch failed$`, func() error {
		sent, err := c.mailSent()
		if err != nil {
			return err
		}
		for _, m := range sent {
			if strings.HasPrefix(m.Subject, application.MailSmokeFailed+":") && strings.Contains(m.Subject+m.Body, c.batch.report.Label()) {
				return nil
			}
		}
		return fmt.Errorf("expected a %q mail naming %s, got %+v", application.MailSmokeFailed, c.batch.report.Label(), sent)
	})
}

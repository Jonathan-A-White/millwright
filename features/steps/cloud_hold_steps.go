package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// registerHoldSteps adds the steps of the scenarios on a hold that a box's name
// kept from an older box, and on mw cloud drain and undrain (mw-gq6.1394).
func (c *cloudContext) registerHoldSteps(ctx *godog.ScenarioContext) {
	var drain *application.CloudDrain
	var drainFiles *apptest.FakeCloudDrainFiles

	ctx.Given(`^the note "([^"]*)" holds "([^"]*)"$`, func(key, value string) error {
		return c.tracker.SetNote(context.Background(), key, value)
	})
	ctx.Then(`^the note "([^"]*)" holds nothing$`, func(key string) error {
		if got, _ := c.tracker.Note(context.Background(), key); got != "" {
			return fmt.Errorf("the note %s holds %q, want nothing", key, got)
		}
		return nil
	})
	ctx.Then(`^the note "([^"]*)" still holds "([^"]*)"$`, func(key, want string) error {
		if got, _ := c.tracker.Note(context.Background(), key); got != want {
			return fmt.Errorf("the note %s holds %q, want %q", key, got, want)
		}
		return nil
	})

	ctx.Given(`^the home "([^"]*)" is drained with "([^"]*)" and "([^"]*)" up and max_boxes (\d+)$`, func(home, a, b string, max int) error {
		state := application.CloudState{}
		for _, name := range []string{a, b} {
			state.Boxes = append(state.Boxes, application.CloudBox{Name: name, Up: c.now.Add(-time.Hour)})
		}
		c.book.State = state
		drainFiles = &apptest.FakeCloudDrainFiles{MaxBoxes: max}
		drain = &application.CloudDrain{
			Files: drainFiles,
			Book:  c.book,
			Notes: c.tracker,
			Home:  &apptest.FakeHomeFile{Text: home + " 2026-10-15T17:03:00Z mw@" + home},
			Host:  home,
			Now:   func() time.Time { return c.now },
		}
		return nil
	})
	ctx.When(`^mw cloud drain is run$`, func() error {
		_, err := drain.Drain(context.Background())
		return err
	})
	ctx.When(`^"([^"]*)" is destroyed and mw cloud undrain is run$`, func(name string) error {
		var left []application.CloudBox
		for _, box := range c.book.State.Boxes {
			if box.Name != name {
				left = append(left, box)
			}
		}
		c.book.State.Boxes = left
		_, err := drain.Undrain(context.Background())
		return err
	})
	ctx.Then(`^"([^"]*)" and "([^"]*)" claim nothing new, held by the drain$`, func(a, b string) error {
		for _, name := range []string{a, b} {
			hold, held := application.ReadHostHold(context.Background(), c.tracker, name, c.now)
			if !held || hold.Reason != application.ReasonDrained {
				return fmt.Errorf("%s is not held by the drain: %+v (held %v)", name, hold, held)
			}
		}
		return nil
	})
	ctx.Then(`^max_boxes is put back at (\d+)$`, func(want int) error {
		if drainFiles.MaxBoxes != want {
			return fmt.Errorf("max_boxes is %d, want %d (writes %v)", drainFiles.MaxBoxes, want, drainFiles.Writes)
		}
		return nil
	})
	ctx.Then(`^"([^"]*)" and "([^"]*)" claim what they are given$`, func(a, b string) error {
		for _, name := range []string{a, b} {
			if hold, held := application.ReadHostHold(context.Background(), c.tracker, name, c.now); held {
				return fmt.Errorf("%s is still held after undrain: %+v", name, hold)
			}
		}
		return nil
	})
}

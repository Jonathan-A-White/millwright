package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"

	"github.com/cucumber/godog"
)

// posternBeadContext holds the fake tracker a scenario reads a bead from, and
// what the last read produced.
type posternBeadContext struct {
	tracker *apptest.FakeTracker
	cipher  *apptest.FakeCipher
	writes  int
	out     strings.Builder
	detail  application.PosternBeadDetail
	err     error
}

// InitializePosternBeadScenario registers the steps of
// features/postern_bead.feature.
func InitializePosternBeadScenario(ctx *godog.ScenarioContext) {
	c := &posternBeadContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = posternBeadContext{tracker: apptest.NewFakeTracker(), cipher: apptest.NewFakeCipher()}
		return ctx, nil
	})

	ctx.Given(`^the detailed epic "([^"]*)" holds "([^"]*)", "([^"]*)" and "([^"]*)"$`, c.theDetailedEpicHolds)
	ctx.Given(`^the detailed bead "([^"]*)" waits on "([^"]*)" and "([^"]*)" waits on "([^"]*)"$`, c.theDetailedBeadWaits)
	ctx.Given(`^the detailed bead "([^"]*)" carries the comments "([^"]*)" and "([^"]*)"$`, c.theDetailedBeadCarriesComments)

	ctx.When(`^mw postern bead "([^"]*)" is read$`, c.mwPosternBeadIsRead)
	ctx.When(`^mw postern bead "([^"]*)" is run for the backend$`, c.mwPosternBeadIsRunForTheBackend)

	ctx.Then(`^the detail waits on "([^"]*)" and blocks "([^"]*)"$`, c.theDetailWaitsOnAndBlocks)
	ctx.Then(`^the detail carries the comments "([^"]*)", oldest first$`, c.theDetailCarriesTheComments)
	ctx.Then(`^the detail read wrote nothing$`, c.theDetailReadWroteNothing)
	ctx.Then(`^the detail's children are "([^"]*)"$`, c.theDetailsChildrenAre)
	ctx.Then(`^the detail is refused as missing, status (\d+)$`, c.theDetailIsRefusedAsMissing)
	ctx.Then(`^the printed detail opens, from gzip, to "([^"]*)"$`, c.thePrintedDetailOpens)
}

func (c *posternBeadContext) theDetailedEpicHolds(epic, a, b, d string) error {
	c.tracker.AddEpic(epic, domain.Path{Rig: "postern", Branch: "main"})
	c.tracker.DescribeEpic(epic, "Epic "+epic, apptest.StatusOpen, 1)
	for _, id := range []string{a, b, d} {
		c.tracker.AddStory(epic, domain.Story{ID: id, Title: "Story " + id})
	}
	return nil
}

func (c *posternBeadContext) theDetailedBeadWaits(id, on, other, otherOn string) error {
	c.tracker.Needs(id, on)
	c.tracker.Needs(other, otherOn)
	return nil
}

func (c *posternBeadContext) theDetailedBeadCarriesComments(id, first, second string) error {
	for _, text := range []string{first, second} {
		if err := c.tracker.CommentOnStory(context.Background(), id, text); err != nil {
			return err
		}
	}
	return nil
}

func (c *posternBeadContext) mwPosternBeadIsRead(id string) error {
	c.writes = c.tracker.Writes()
	c.detail, c.err = application.PosternBead{Tracker: c.tracker}.Build(context.Background(), id)
	return nil
}

func (c *posternBeadContext) mwPosternBeadIsRunForTheBackend(id string) error {
	c.detail, c.err = application.PosternBead{
		Tracker: c.tracker, Cipher: c.cipher, GovernorKey: "governor-pubkey-hex", Out: &c.out,
	}.Run(context.Background(), id)
	return c.err
}

func (c *posternBeadContext) theDetailWaitsOnAndBlocks(waits, blocks string) error {
	if c.err != nil {
		return c.err
	}
	if strings.Join(c.detail.Waits, ", ") != waits || strings.Join(c.detail.Blocks, ", ") != blocks {
		return fmt.Errorf("expected waits %q and blocks %q, got %v and %v", waits, blocks, c.detail.Waits, c.detail.Blocks)
	}
	return nil
}

func (c *posternBeadContext) theDetailCarriesTheComments(want string) error {
	var got []string
	for _, comment := range c.detail.Comments {
		got = append(got, comment.Text)
	}
	if strings.Join(got, ", ") != want {
		return fmt.Errorf("expected the comments %q, got %q", want, strings.Join(got, ", "))
	}
	return nil
}

func (c *posternBeadContext) theDetailReadWroteNothing() error {
	if c.tracker.Writes() != c.writes {
		return fmt.Errorf("expected the read to write nothing, got %d write(s)", c.tracker.Writes()-c.writes)
	}
	return nil
}

func (c *posternBeadContext) theDetailsChildrenAre(want string) error {
	if c.err != nil {
		return c.err
	}
	if strings.Join(c.detail.Children, ", ") != want {
		return fmt.Errorf("expected children %q, got %v", want, c.detail.Children)
	}
	return nil
}

func (c *posternBeadContext) theDetailIsRefusedAsMissing(status int) error {
	if !application.PosternBeadIsMissing(c.err) {
		return fmt.Errorf("expected the bead reported missing, got %v", c.err)
	}
	if status != application.PosternBeadMissingExit {
		return fmt.Errorf("expected status %d for a missing bead, the feature says %d", application.PosternBeadMissingExit, status)
	}
	return nil
}

func (c *posternBeadContext) thePrintedDetailOpens(id string) error {
	text, _, err := c.cipher.Decrypt("any", strings.TrimSpace(c.out.String()))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(text, "\x1f\x8b") {
		return fmt.Errorf("expected the sealed plaintext to be gzip")
	}
	var opened application.PosternBeadDetail
	if err := application.OpenPosternDoc(c.cipher, "any", c.out.String(), &opened); err != nil {
		return err
	}
	if opened.ID != id {
		return fmt.Errorf("expected the printed detail of %s, got %s", id, opened.ID)
	}
	return nil
}

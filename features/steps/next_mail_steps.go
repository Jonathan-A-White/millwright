package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// The steps of the mail a close-out sends the Mayor. They hang off nextContext:
// the mailbox and stderr they read are the ones mwClosesOut hands to Next.

// registerNextMailSteps registers the mail steps of features/next.feature.
func registerNextMailSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the mailbox refuses every message, saying: (.+)$`, c.theMailboxRefuses)

	ctx.Then(`^exactly one mail was sent, to "([^"]*)" from "([^"]*)"$`, c.exactlyOneMailWasSent)
	ctx.Then(`^that mail's subject is "([^"]*)"$`, c.thatMailsSubjectIs)
	ctx.Then(`^that mail's body names the commit that landed on "([^"]*)"$`, c.thatMailsBodyNamesTheCommit)
	ctx.Then(`^that mail's body holds:$`, c.thatMailsBodyHolds)
	ctx.Then(`^no mail was sent$`, c.noMailWasSent)
	ctx.Then(`^the close-out returned no error$`, c.theCloseOutReturnedNoError)
	ctx.Then(`^stderr says the Mayor could not be mailed, quoting: (.+)$`, c.stderrSaysTheMayorCouldNotBeMailed)
}

func (c *nextContext) theMailboxRefuses(saying string) error {
	c.mailbox.Err = errors.New(saying)
	return nil
}

// mailSent is every message the close-out put in the mailbox: the fake keeps
// them by recipient, so the Mayor's inbox and the Governor's are both asked.
func (c *nextContext) mailSent() ([]application.Message, error) {
	var sent []application.Message
	for _, to := range []string{"mayor", "builder", "governor"} {
		messages, err := c.mailbox.Inbox(context.Background(), to)
		if err != nil {
			return nil, err
		}
		sent = append(sent, messages...)
	}
	return sent, nil
}

func (c *nextContext) theOneMail() (application.Message, error) {
	sent, err := c.mailSent()
	if err != nil {
		return application.Message{}, err
	}
	if len(sent) != 1 {
		return application.Message{}, fmt.Errorf("expected exactly one mail, got %d: %+v", len(sent), sent)
	}
	return sent[0], nil
}

func (c *nextContext) exactlyOneMailWasSent(to, from string) error {
	mail, err := c.theOneMail()
	if err != nil {
		return err
	}
	if mail.To != to || mail.From != from {
		return fmt.Errorf("expected the mail to go to %q from %q, got to %q from %q", to, from, mail.To, mail.From)
	}
	return nil
}

func (c *nextContext) thatMailsSubjectIs(want string) error {
	mail, err := c.theOneMail()
	if err != nil {
		return err
	}
	if mail.Subject != want {
		return fmt.Errorf("expected the subject %q, got %q", want, mail.Subject)
	}
	return nil
}

// thatMailsBodyNamesTheCommit reads the commit the landing put on the branch
// from the origin, and looks for it as the report shortens it.
func (c *nextContext) thatMailsBodyNamesTheCommit(branch string) error {
	mail, err := c.theOneMail()
	if err != nil {
		return err
	}
	commit, err := gitSay(c.origin(), "rev-parse", branch)
	if err != nil {
		return err
	}
	if short := strings.TrimSpace(commit)[:12]; !strings.Contains(mail.Body, short) {
		return fmt.Errorf("expected the body to name the commit %s, got:\n%s", short, mail.Body)
	}
	return nil
}

func (c *nextContext) thatMailsBodyHolds(table *godog.Table) error {
	mail, err := c.theOneMail()
	if err != nil {
		return err
	}
	for _, row := range table.Rows {
		want := strings.TrimSpace(row.Cells[0].Value)
		if !strings.Contains(mail.Body, want) {
			return fmt.Errorf("expected the body to hold %q, got:\n%s", want, mail.Body)
		}
	}
	return nil
}

func (c *nextContext) noMailWasSent() error {
	sent, err := c.mailSent()
	if err != nil {
		return err
	}
	if len(sent) > 0 || c.mailbox.Writes() > 0 {
		return fmt.Errorf("expected no mail, got %d message(s) and %d write(s)", len(sent), c.mailbox.Writes())
	}
	return nil
}

func (c *nextContext) theCloseOutReturnedNoError() error {
	if c.err != nil {
		return fmt.Errorf("expected the close-out to return no error, got: %w", c.err)
	}
	return nil
}

func (c *nextContext) stderrSaysTheMayorCouldNotBeMailed(quoting string) error {
	said := c.stderr.String()
	if !strings.Contains(said, "mayor") || !strings.Contains(said, quoting) {
		return fmt.Errorf("expected stderr to say the mayor could not be mailed, quoting %q, got %q", quoting, said)
	}
	return nil
}

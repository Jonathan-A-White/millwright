package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"

	"github.com/cucumber/godog"
)

// mailContext holds the fake mailbox mail is sent through, the seat the session
// says it is, and what the last thing done printed. Nothing here reaches the
// factory's own beads database.
type mailContext struct {
	mailbox *apptest.FakeMailbox

	// seat is what $MW_SEAT is; "" is unset.
	seat string
	// ids is the id each message was given, by its subject.
	ids map[string]string

	printed string
	err     error
}

// InitializeMailScenario registers the steps of features/mail.feature.
func InitializeMailScenario(ctx *godog.ScenarioContext) {
	c := &mailContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = mailContext{mailbox: apptest.NewFakeMailbox(), ids: map[string]string{}}
		return ctx, nil
	})

	ctx.Given(`^MW_SEAT is "([^"]*)"$`, c.mwSeatIs)
	ctx.Given(`^MW_SEAT is not set$`, c.mwSeatIsNotSet)
	ctx.Given(`^mail was sent to "([^"]*)" with the subject "([^"]*)" and the body "([^"]*)"$`, c.mailWasSent)
	ctx.Given(`^mail was sent to "([^"]*)" with the subject "([^"]*)" and the body:$`, c.mailWasSentWithBody)
	ctx.Given(`^mail was sent to "([^"]*)" with the subject "([^"]*)" and no body$`, c.mailWasSentWithNoBody)
	ctx.Given(`^the message with the subject "([^"]*)" was replied to with the body "([^"]*)"$`, c.theMessageWasRepliedTo)

	ctx.When(`^mail is sent to "([^"]*)" with the subject "([^"]*)" and the body "([^"]*)"$`, c.mailIsSent)
	ctx.When(`^the message with the subject "([^"]*)" is read$`, c.theMessageWithTheSubjectIsRead)
	ctx.When(`^the message "([^"]*)" is read$`, c.theMessageIsRead)
	ctx.When(`^the message with the subject "([^"]*)" is replied to with the body "([^"]*)"$`, c.theMessageWithTheSubjectIsRepliedTo)
	ctx.When(`^the message "([^"]*)" is replied to with the body "([^"]*)"$`, c.theMessageIsRepliedTo)
	ctx.When(`^the inbox is listed$`, c.theInboxIsListed)
	ctx.When(`^the inbox is listed with --as "([^"]*)"$`, c.theInboxIsListedAs)

	ctx.Then(`^sending mail succeeds$`, c.sendingMailSucceeds)
	ctx.Then(`^reading mail succeeds$`, c.sendingMailSucceeds)
	ctx.Then(`^listing the inbox succeeds$`, c.sendingMailSucceeds)
	ctx.Then(`^replying to mail succeeds$`, c.sendingMailSucceeds)
	ctx.Then(`^the message with the subject "([^"]*)" says it answers the message with the subject "([^"]*)"$`, c.theMessageAnswers)
	ctx.Then(`^mail is refused, naming "([^"]*)"$`, c.mailIsRefusedNaming)
	ctx.Then(`^the mailbox recorded no writes$`, c.theMailboxRecordedNoWrites)
	ctx.Then(`^mail says it went to "([^"]*)"$`, c.mailSaysItWentTo)
	ctx.Then(`^the inbox of "([^"]*)" is empty$`, c.theInboxOfIsEmpty)
	ctx.Then(`^the inbox of "([^"]*)" lists a message from "([^"]*)" with the subject "([^"]*)"$`, c.theInboxOfLists)
	ctx.Then(`^the mail printed says "([^"]*)"$`, c.thePrintedSays)
	ctx.Then(`^the mail printed says the body:$`, c.thePrintedSaysTheBody)
	ctx.Then(`^the inbox printed says "([^"]*)"$`, c.thePrintedSays)
	ctx.Then(`^the inbox printed lists the subject "([^"]*)"$`, c.theInboxPrintedLists)
	ctx.Then(`^the inbox printed does not list the subject "([^"]*)"$`, c.theInboxPrintedDoesNotList)
	ctx.Then(`^the inbox printed names "([^"]*)" before "([^"]*)"$`, c.theInboxPrintedNamesBefore)
}

func (c *mailContext) mwSeatIs(seat string) error {
	c.seat = seat
	return nil
}

func (c *mailContext) mwSeatIsNotSet() error {
	c.seat = ""
	return nil
}

// mail is the use case as this session would run it, printing into out.
func (c *mailContext) mail(out *strings.Builder) application.Mail {
	return application.Mail{Mailbox: c.mailbox, Seat: c.seat, Out: out}
}

func (c *mailContext) mailWasSent(to, subject, body string) error {
	if err := c.mailIsSent(to, subject, body); err != nil {
		return err
	}
	if c.err != nil {
		return fmt.Errorf("sending mail to %s failed: %w", to, c.err)
	}
	return nil
}

func (c *mailContext) mailWasSentWithBody(to, subject string, body *godog.DocString) error {
	return c.mailWasSent(to, subject, body.Content)
}

func (c *mailContext) mailWasSentWithNoBody(to, subject string) error {
	return c.mailWasSent(to, subject, "")
}

func (c *mailContext) mailIsSent(to, subject, body string) error {
	var out strings.Builder
	var id string
	id, c.err = c.mail(&out).Send(context.Background(), to, subject, body)
	c.printed = out.String()
	if c.err == nil {
		c.ids[subject] = id
	}
	return nil
}

func (c *mailContext) theMessageWithTheSubjectIsRead(subject string) error {
	id, ok := c.ids[subject]
	if !ok {
		return fmt.Errorf("no message with the subject %q was sent", subject)
	}
	return c.theMessageIsRead(id)
}

func (c *mailContext) theMessageWasRepliedTo(subject, body string) error {
	if err := c.theMessageWithTheSubjectIsRepliedTo(subject, body); err != nil {
		return err
	}
	if c.err != nil {
		return fmt.Errorf("replying to %q failed: %w", subject, c.err)
	}
	return nil
}

func (c *mailContext) theMessageWithTheSubjectIsRepliedTo(subject, body string) error {
	id, ok := c.ids[subject]
	if !ok {
		return fmt.Errorf("no message with the subject %q was sent", subject)
	}
	return c.theMessageIsRepliedTo(id, body)
}

// theMessageIsRepliedTo replies to a message by id, and remembers the reply by
// the subject it was given.
func (c *mailContext) theMessageIsRepliedTo(id, body string) error {
	var out strings.Builder
	var replyID string
	replyID, c.err = c.mail(&out).Reply(context.Background(), id, body)
	c.printed = out.String()
	if c.err != nil {
		return nil
	}
	reply, err := c.mailbox.Get(context.Background(), replyID)
	if err != nil {
		return err
	}
	c.ids[reply.Subject] = replyID
	return nil
}

func (c *mailContext) theMessageAnswers(subject, answered string) error {
	id, ok := c.ids[subject]
	if !ok {
		return fmt.Errorf("no message with the subject %q was sent", subject)
	}
	original, ok := c.ids[answered]
	if !ok {
		return fmt.Errorf("no message with the subject %q was sent", answered)
	}
	message, err := c.mailbox.Get(context.Background(), id)
	if err != nil {
		return err
	}
	if message.ReplyTo != original {
		return fmt.Errorf("expected %q to answer %s (%q), it answers %q", subject, original, answered, message.ReplyTo)
	}
	return nil
}

func (c *mailContext) theMessageIsRead(id string) error {
	var out strings.Builder
	_, c.err = c.mail(&out).Read(context.Background(), id, "")
	c.printed = out.String()
	return nil
}

func (c *mailContext) theInboxIsListed() error {
	return c.theInboxIsListedAs("")
}

func (c *mailContext) theInboxIsListedAs(as string) error {
	var out strings.Builder
	_, c.err = c.mail(&out).Inbox(context.Background(), as)
	c.printed = out.String()
	return nil
}

func (c *mailContext) sendingMailSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, it failed: %w", c.err)
	}
	return nil
}

func (c *mailContext) mailIsRefusedNaming(name string) error {
	if c.err == nil {
		return fmt.Errorf("expected mail to be refused, naming %q; it succeeded, printing:\n%s", name, c.printed)
	}
	if !strings.Contains(c.err.Error(), name) {
		return fmt.Errorf("expected the refusal to name %q, got: %s", name, c.err)
	}
	return nil
}

func (c *mailContext) theMailboxRecordedNoWrites() error {
	if got := c.mailbox.Writes(); got != 0 {
		return fmt.Errorf("expected the mailbox to take no writes, it took %d", got)
	}
	return nil
}

func (c *mailContext) mailSaysItWentTo(to string) error {
	if !strings.Contains(c.printed, " -> "+to) {
		return fmt.Errorf("expected mail to say it went to %s, got:\n%s", to, c.printed)
	}
	return nil
}

// inboxOf lists a mailbox without disturbing what the last step printed.
func (c *mailContext) inboxOf(mailbox string) ([]application.Message, error) {
	var out strings.Builder
	return c.mail(&out).Inbox(context.Background(), mailbox)
}

func (c *mailContext) theInboxOfIsEmpty(mailbox string) error {
	unread, err := c.inboxOf(mailbox)
	if err != nil {
		return err
	}
	if len(unread) != 0 {
		return fmt.Errorf("expected the inbox of %s to be empty, it holds %d messages, first %q", mailbox, len(unread), unread[0].Subject)
	}
	return nil
}

func (c *mailContext) theInboxOfLists(mailbox, from, subject string) error {
	unread, err := c.inboxOf(mailbox)
	if err != nil {
		return err
	}
	for _, message := range unread {
		if message.From == from && message.Subject == subject {
			return nil
		}
	}
	return fmt.Errorf("expected the inbox of %s to list %q from %s, got %+v", mailbox, subject, from, unread)
}

func (c *mailContext) thePrintedSays(text string) error {
	if !strings.Contains(c.printed, text) {
		return fmt.Errorf("expected %q in:\n%s", text, c.printed)
	}
	return nil
}

func (c *mailContext) thePrintedSaysTheBody(body *godog.DocString) error {
	if !strings.HasSuffix(strings.TrimRight(c.printed, "\n"), "\n\n"+body.Content) {
		return fmt.Errorf("expected the message to end with a blank line and the body:\n%s\ngot:\n%s", body.Content, c.printed)
	}
	return nil
}

func (c *mailContext) theInboxPrintedLists(subject string) error {
	return c.thePrintedSays(subject)
}

func (c *mailContext) theInboxPrintedDoesNotList(subject string) error {
	if strings.Contains(c.printed, subject) {
		return fmt.Errorf("expected the inbox not to list %q, got:\n%s", subject, c.printed)
	}
	return nil
}

func (c *mailContext) theInboxPrintedNamesBefore(first, second string) error {
	i, j := strings.Index(c.printed, first), strings.Index(c.printed, second)
	if i < 0 || j < 0 || i >= j {
		return fmt.Errorf("expected %q before %q, got:\n%s", first, second, c.printed)
	}
	return nil
}

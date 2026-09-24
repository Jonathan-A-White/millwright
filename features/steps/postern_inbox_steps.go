package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// posternInboxContext holds a throwaway postern key, a fake backend and
// cipher, and a fake tracker standing in for the note store, so that mw
// postern inbox can be exercised with no network and no real bd.
type posternInboxContext struct {
	home    string
	keys    *postern.KeyFile
	pubKey  string
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	memory  *apptest.FakeTracker
	out     *bytes.Buffer

	messages    []application.PosternInboxMessage
	unreadCount int
	err         error
}

// InitializePosternInboxScenario registers the steps of
// features/postern_inbox.feature.
func InitializePosternInboxScenario(ctx *godog.ScenarioContext) {
	c := &posternInboxContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = posternInboxContext{
			backend: apptest.NewFakePostern(),
			cipher:  apptest.NewFakeCipher(),
			memory:  apptest.NewFakeTracker(),
			out:     &bytes.Buffer{},
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway postern key$`, c.aThrowawayPosternKey)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to this key$`, c.aPosternRecordAddressedToThisKey)
	ctx.Given(`^a postern record of class "([^"]*)" addressed to another key$`, c.aPosternRecordAddressedToAnotherKey)

	ctx.When(`^mw postern inbox is run$`, c.mwPosternInboxIsRun)
	ctx.When(`^mw postern inbox --unread-count is run$`, c.mwPosternInboxUnreadCountIsRun)

	ctx.Then(`^reading succeeds$`, c.itSucceeds)
	ctx.Then(`^(\d+) messages? (?:is|are) printed$`, c.nMessagesArePrinted)
	ctx.Then(`^the first message printed is classed "([^"]*)"$`, c.theFirstMessagePrintedIsClassed)
	ctx.Then(`^the second message printed is classed "([^"]*)"$`, c.theSecondMessagePrintedIsClassed)
	ctx.Then(`^the unread count is (\d+)$`, c.theUnreadCountIs)
	ctx.Then(`^the postern inbox cursor is saved as a note$`, c.thePosternInboxCursorIsSavedAsANote)
	ctx.Then(`^no story state was set$`, c.noStoryStateWasSet)
}

func (c *posternInboxContext) aThrowawayPosternKey() error {
	home, err := os.MkdirTemp("", "mw-postern-inbox-")
	if err != nil {
		return err
	}
	c.home = home
	c.keys = postern.New(filepath.Join(home, "postern.key"))
	if err := c.keys.Generate(); err != nil {
		return err
	}
	pubKey, _, err := c.keys.PublicKey()
	if err != nil {
		return err
	}
	c.pubKey = pubKey
	return nil
}

func (c *posternInboxContext) addRecord(class, to string) error {
	ciphertext, err := c.cipher.Encrypt(to, fmt.Sprintf("%s text", class))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Class:      class,
		From:       "governor-pubkey-hex",
		To:         to,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aPosternRecordAddressedToThisKey(class string) error {
	return c.addRecord(class, c.pubKey)
}

func (c *posternInboxContext) aPosternRecordAddressedToAnotherKey(class string) error {
	return c.addRecord(class, "another-key-pubkey-hex")
}

func (c *posternInboxContext) inbox() application.PosternInbox {
	return application.PosternInbox{
		Postern: c.backend,
		Cipher:  c.cipher,
		Keys:    c.keys,
		Memory:  c.memory,
		Out:     c.out,
	}
}

func (c *posternInboxContext) mwPosternInboxIsRun() error {
	c.messages, c.err = c.inbox().Run(context.Background())
	return nil
}

func (c *posternInboxContext) mwPosternInboxUnreadCountIsRun() error {
	c.unreadCount, c.err = c.inbox().UnreadCount(context.Background())
	return nil
}

func (c *posternInboxContext) itSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	return nil
}

func (c *posternInboxContext) nMessagesArePrinted(want int) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	if len(c.messages) != want {
		return fmt.Errorf("expected %d messages, got %d: %+v", want, len(c.messages), c.messages)
	}
	return nil
}

func (c *posternInboxContext) theFirstMessagePrintedIsClassed(class string) error {
	if len(c.messages) < 1 {
		return fmt.Errorf("no first message: only %d printed", len(c.messages))
	}
	if c.messages[0].Class != class {
		return fmt.Errorf("expected the first message classed %q, got %q", class, c.messages[0].Class)
	}
	return nil
}

func (c *posternInboxContext) theSecondMessagePrintedIsClassed(class string) error {
	if len(c.messages) < 2 {
		return fmt.Errorf("no second message: only %d printed", len(c.messages))
	}
	if c.messages[1].Class != class {
		return fmt.Errorf("expected the second message classed %q, got %q", class, c.messages[1].Class)
	}
	return nil
}

func (c *posternInboxContext) theUnreadCountIs(want int) error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	if c.unreadCount != want {
		return fmt.Errorf("expected an unread count of %d, got %d", want, c.unreadCount)
	}
	return nil
}

func (c *posternInboxContext) thePosternInboxCursorIsSavedAsANote() error {
	saved, err := c.memory.Note(context.Background(), application.PosternCursorKey)
	if err != nil {
		return err
	}
	if strings.TrimSpace(saved) == "" {
		return fmt.Errorf("no postern inbox cursor note was saved")
	}
	if _, err := strconv.ParseInt(saved, 10, 64); err != nil {
		return fmt.Errorf("the saved cursor %q is not a whole number: %w", saved, err)
	}
	return nil
}

func (c *posternInboxContext) noStoryStateWasSet() error {
	for _, asked := range c.memory.Asked() {
		if asked == "SetStoryState" {
			return fmt.Errorf("expected no SetStoryState call, but one was made: %v", c.memory.Asked())
		}
	}
	return nil
}

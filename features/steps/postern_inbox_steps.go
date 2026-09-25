package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// posternReplyStamp is when every fake reply record in this feature's
// scenarios is stamped, so the ANSWER comment's timestamp is predictable.
var posternReplyStamp = time.Date(2026, time.September, 25, 10, 0, 0, 0, time.UTC)

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
	mailbox *apptest.FakeMailbox
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
			mailbox: apptest.NewFakeMailbox(),
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
	ctx.Given(`^bead "([^"]*)" is known to the tracker$`, c.beadIsKnownToTheTracker)
	ctx.Given(`^bead "([^"]*)" has an open question, txid "([^"]*)"$`, c.beadHasAnOpenQuestion)
	ctx.Given(`^a postern reply for bead "([^"]*)" with answer "([^"]*)" and txid "([^"]*)" addressed to this key$`,
		c.aPosternReplyAddressedToThisKey)
	ctx.Given(`^a plain text postern record with text "([^"]*)" addressed to this key$`, c.aPlainTextRecordAddressedToThisKey)

	ctx.When(`^mw postern inbox is run$`, c.mwPosternInboxIsRun)
	ctx.When(`^mw postern inbox --unread-count is run$`, c.mwPosternInboxUnreadCountIsRun)

	ctx.Then(`^reading succeeds$`, c.itSucceeds)
	ctx.Then(`^(\d+) messages? (?:is|are) printed$`, c.nMessagesArePrinted)
	ctx.Then(`^the first message printed is classed "([^"]*)"$`, c.theFirstMessagePrintedIsClassed)
	ctx.Then(`^the second message printed is classed "([^"]*)"$`, c.theSecondMessagePrintedIsClassed)
	ctx.Then(`^the unread count is (\d+)$`, c.theUnreadCountIs)
	ctx.Then(`^the postern inbox cursor is saved as a note$`, c.thePosternInboxCursorIsSavedAsANote)
	ctx.Then(`^no story state was set$`, c.noStoryStateWasSet)
	ctx.Then(`^bead "([^"]*)" is commented an ANSWER with txid "([^"]*)" from "([^"]*)" saying "([^"]*)"$`, c.beadIsCommentedTheAnswer)
	ctx.Then(`^bead "([^"]*)"'s question note is cleared$`, c.beadsQuestionNoteIsCleared)
	ctx.Then(`^mail "([^"]*)" was sent to mayor$`, c.mailWasSentToMayor)
	ctx.Then(`^bead "([^"]*)" has no comment$`, c.beadHasNoComment)
	ctx.Then(`^no mail was sent for the reply$`, c.noMailWasSent)
	ctx.Then(`^it printed "([^"]*)"$`, c.itPrintedText)
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

func (c *posternInboxContext) beadIsKnownToTheTracker(id string) error {
	c.memory.AddStory("epic", domain.Story{ID: id})
	return nil
}

func (c *posternInboxContext) beadHasAnOpenQuestion(id, txid string) error {
	return c.memory.SetNote(context.Background(), application.PosternQuestionKey(id), txid)
}

func (c *posternInboxContext) aPosternReplyAddressedToThisKey(bead, answer, txid string) error {
	text, err := json.Marshal(application.PosternReply{Bead: bead, Answer: answer})
	if err != nil {
		return err
	}
	ciphertext, err := c.cipher.Encrypt(c.pubKey, string(text))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Txid:       txid,
		Class:      "message",
		From:       "governor-pubkey-hex",
		To:         c.pubKey,
		Ts:         posternReplyStamp,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) aPlainTextRecordAddressedToThisKey(text string) error {
	ciphertext, err := c.cipher.Encrypt(c.pubKey, text)
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{
		Class:      "message",
		From:       "governor-pubkey-hex",
		To:         c.pubKey,
		Ciphertext: ciphertext,
	})
	return nil
}

func (c *posternInboxContext) inbox() application.PosternInbox {
	return application.PosternInbox{
		Postern: c.backend,
		Cipher:  c.cipher,
		Keys:    c.keys,
		Memory:  c.memory,
		Tracker: c.memory,
		Mailbox: c.mailbox,
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

func (c *posternInboxContext) beadIsCommentedTheAnswer(bead, txid, from, answer string) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return err
	}
	if len(comments) == 0 {
		return fmt.Errorf("expected a comment on %s, found none", bead)
	}
	want := fmt.Sprintf("ANSWER %s from %s, txid %s: %s", posternReplyStamp.UTC().Format(time.RFC3339), from, txid, answer)
	got := comments[len(comments)-1].Text
	if got != want {
		return fmt.Errorf("expected the comment\n%s\ngot\n%s", want, got)
	}
	return nil
}

func (c *posternInboxContext) beadsQuestionNoteIsCleared(bead string) error {
	saved, err := c.memory.Note(context.Background(), application.PosternQuestionKey(bead))
	if err != nil {
		return err
	}
	if saved != "" {
		return fmt.Errorf("expected %s's question note to be cleared, still holds %q", bead, saved)
	}
	return nil
}

func (c *posternInboxContext) mailWasSentToMayor(subject string) error {
	unread, err := c.mailbox.Inbox(context.Background(), "mayor")
	if err != nil {
		return err
	}
	for _, message := range unread {
		if message.Subject == subject {
			return nil
		}
	}
	return fmt.Errorf("expected mail %q to mayor, got: %+v", subject, unread)
}

// beadHasNoComment checks that bead carries no comment; a bead the tracker
// has never heard of — as an unknown bead in these scenarios is — counts as
// having none.
func (c *posternInboxContext) beadHasNoComment(bead string) error {
	comments, err := c.memory.StoryComments(context.Background(), bead)
	if err != nil {
		return nil
	}
	if len(comments) != 0 {
		return fmt.Errorf("expected no comment on %s, got: %+v", bead, comments)
	}
	return nil
}

func (c *posternInboxContext) noMailWasSent() error {
	if c.mailbox.Writes() != 0 {
		return fmt.Errorf("expected no mail sent, but %d write(s) were made", c.mailbox.Writes())
	}
	return nil
}

func (c *posternInboxContext) itPrintedText(want string) error {
	if !strings.Contains(c.out.String(), want) {
		return fmt.Errorf("expected the output to contain %q, got:\n%s", want, c.out.String())
	}
	return nil
}

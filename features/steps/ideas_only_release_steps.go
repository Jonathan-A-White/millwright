package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// ideasReleaseRig is what mw ideas-only release needs beside the files: the
// Mayor's key, a cipher, a tracker standing in for the notes and the beads, and
// the inbox they make.
type ideasReleaseRig struct {
	keys    *postern.KeyFile
	mayor   string
	cipher  *apptest.FakeCipher
	memory  *apptest.FakeTracker
	mailbox *apptest.FakeMailbox
	inbox   bytes.Buffer
}

// rig builds the release rig on first use, in the scenario's own directory.
func (c *ideasOnlyContext) rig() (*ideasReleaseRig, error) {
	if c.release != nil {
		return c.release, nil
	}
	keys := postern.New(filepath.Join(c.dir, "postern.key"))
	if err := keys.Generate(); err != nil {
		return nil, err
	}
	mayor, _, err := keys.PublicKey()
	if err != nil {
		return nil, err
	}
	cipher := apptest.NewFakeCipher()
	cipher.From = ideasOnlyGovernorKey
	c.release = &ideasReleaseRig{
		keys: keys, mayor: mayor, cipher: cipher,
		memory: apptest.NewFakeTracker(), mailbox: apptest.NewFakeMailbox(),
	}
	return c.release, nil
}

func (r *ideasReleaseRig) cards(c *ideasOnlyContext) application.Cards {
	return application.Cards{
		Postern: c.backend, Chain: apptest.NewFakeChain(c.backend, r.keys),
		Cipher: r.cipher, Keys: r.keys, GovernorKey: ideasOnlyGovernorKey,
		Beads: r.memory, Now: func() time.Time { return c.now },
	}
}

// inboxFor is the inbox the release and the plain read run through.
func (r *ideasReleaseRig) inboxFor(c *ideasOnlyContext, out *bytes.Buffer) application.PosternInbox {
	mode := c.mode()
	cards := r.cards(c)
	return application.PosternInbox{
		Postern: c.backend, Cipher: r.cipher, Keys: r.keys, Memory: r.memory,
		Tracker: r.memory, Mailbox: r.mailbox, GovernorKey: ideasOnlyGovernorKey,
		Ideas: &mode, Cards: &cards, Out: out, Now: func() time.Time { return c.now },
		Sender: &application.PosternSend{
			Postern: c.backend, Chain: apptest.NewFakeChain(c.backend, r.keys),
			Cipher: r.cipher, Keys: r.keys, GovernorKey: ideasOnlyGovernorKey,
		},
	}
}

// holdRecord adds the record the Governor sent to the index, keeps it as the
// hook would have while the mode was on, and moves the inbox's cursor past it,
// as the Mayor's read does of what it passes over.
func (c *ideasOnlyContext) holdRecord(txid, channel, plaintext, words string) error {
	r, err := c.rig()
	if err != nil {
		return err
	}
	ciphertext, err := r.cipher.Encrypt(r.mayor, plaintext)
	if err != nil {
		return err
	}
	record := c.backend.AddRecord(application.PosternRecord{
		Txid: txid, Class: "message", From: ideasOnlyGovernorKey, To: r.mayor, Signer: ideasOnlyGovernorKey,
		Ts: c.now.Add(-time.Hour), Ciphertext: ciphertext,
	})
	ctx := context.Background()
	if err := c.files.Hold(ctx, application.IdeasOnlyHeld{
		Txid: txid, Seq: record.Seq, Class: "message", Channel: channel, Text: words, When: c.now.Add(-time.Hour).Format(time.RFC3339),
	}); err != nil {
		return err
	}
	if err := r.memory.SetNote(ctx, application.PosternHeldKey(txid), "held for after 21:00 UTC: "+txid); err != nil {
		return err
	}
	return r.memory.SetNote(ctx, application.PosternCursorKey, fmt.Sprint(record.Seq))
}

// delivered is every record the Mayor delivered, decrypted.
func (c *ideasOnlyContext) cardsSent() ([]domain.Card, error) {
	r, err := c.rig()
	if err != nil {
		return nil, err
	}
	var cards []domain.Card
	for _, raw := range c.backend.Delivered() {
		var payload application.PosternPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		if payload.Class != application.CardClass {
			continue
		}
		text, _, err := r.cipher.Decrypt("mayor", payload.Ct)
		if err != nil {
			return nil, err
		}
		var card domain.Card
		if err := json.Unmarshal([]byte(text), &card); err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func (c *ideasOnlyContext) registerReleaseSteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^a rig that can release the Governor's ideas$`, func() error {
		_, err := c.rig()
		return err
	})
	ctx.Given(`^the story "([^"]*)" is held$`, func(id string) error {
		r, err := c.rig()
		if err != nil {
			return err
		}
		epic := strings.SplitN(id, ".", 2)[0]
		r.memory.AddEpic(epic, domain.Path{})
		r.memory.AddStory(epic, domain.Story{ID: id, Title: id})
		return r.memory.SetStatus(id, apptest.StatusDeferred)
	})
	ctx.Given(`^Ideas only is holding the Governor's idea "([^"]*)" in "([^"]*)" saying "([^"]*)"$`, func(txid, channel, text string) error {
		return c.holdRecord(txid, channel, text, text)
	})
	ctx.Given(`^Ideas only is holding the Governor's tap "([^"]*)" to release "([^"]*)"$`, func(txid, bead string) error {
		raw, err := json.Marshal(map[string]any{"action": "release", "bead": bead})
		if err != nil {
			return err
		}
		return c.holdRecord(txid, "general", string(raw), "release "+bead)
	})

	ctx.Given(`^an earlier release died after it sent the card "([^"]*)"$`, func(txid string) error {
		state, err := c.files.State(context.Background())
		if err != nil {
			return err
		}
		state.Releasing = txid
		return c.files.SetState(context.Background(), state)
	})
	ctx.Given(`^the backend will not take the card$`, func() error {
		c.backend.DeliverErr = errors.New("the postern backend said 500")
		return nil
	})
	ctx.Then(`^the release failed saying "([^"]*)"$`, func(text string) error {
		if c.err == nil || !strings.Contains(c.err.Error(), text) {
			return fmt.Errorf("expected the release to fail saying %q, it said %v", text, c.err)
		}
		return nil
	})
	ctx.When(`^mw ideas-only release is run$`, func() error {
		r, err := c.rig()
		if err != nil {
			return err
		}
		c.out.Reset()
		c.errOut.Reset()
		var report application.IdeasOnlyReleased
		report, c.err = application.IdeasOnlyRelease{Inbox: r.inboxFor(c, &c.errOut)}.Run(context.Background())
		if c.err == nil {
			c.out.WriteString(report.String())
		}
		return nil
	})
	ctx.When(`^a plain mw postern inbox is run$`, func() error {
		r, err := c.rig()
		if err != nil {
			return err
		}
		r.inbox.Reset()
		_, err = r.inboxFor(c, &r.inbox).Run(context.Background())
		return err
	})

	ctx.Then(`^exactly one card was sent to the Governor, titled "([^"]*)", with (\d+) items$`, func(title string, n int) error {
		cards, err := c.cardsSent()
		if err != nil {
			return err
		}
		if len(cards) != 1 {
			return fmt.Errorf("expected exactly one card sent, %d were: %+v\n%s", len(cards), cards, c.errOut.String())
		}
		if cards[0].Title != title || len(cards[0].Items) != n {
			return fmt.Errorf("expected the card %q with %d items, it is %q with %d: %+v", title, n, cards[0].Title, len(cards[0].Items), cards[0].Items)
		}
		return nil
	})
	ctx.Then(`^no card was sent to the Governor$`, func() error {
		cards, err := c.cardsSent()
		if err != nil {
			return err
		}
		if len(cards) != 0 {
			return fmt.Errorf("expected no card, %d were sent: %+v", len(cards), cards)
		}
		return nil
	})
	ctx.Then(`^card item (\d+) reads "([^"]*)"$`, func(n int, text string) error { return c.cardItem(n, text, "") })
	ctx.Then(`^card item (\d+) reads "([^"]*)" and says "([^"]*)"$`, c.cardItem)
	ctx.Then(`^the story "([^"]*)" stands "([^"]*)"$`, func(id, status string) error {
		r, err := c.rig()
		if err != nil {
			return err
		}
		found, err := r.memory.ShowBeads(context.Background(), []string{id})
		if err != nil || len(found) == 0 {
			return fmt.Errorf("no bead %s: %v", id, err)
		}
		if found[0].Status != status {
			return fmt.Errorf("expected %s to stand %s, it stands %s", id, status, found[0].Status)
		}
		return nil
	})
	ctx.Then(`^the last comment on "([^"]*)" starts "([^"]*)"$`, func(id, want string) error {
		r, err := c.rig()
		if err != nil {
			return err
		}
		comments, err := r.memory.StoryComments(context.Background(), id)
		if err != nil {
			return err
		}
		if len(comments) == 0 || !strings.HasPrefix(comments[len(comments)-1].Text, want) {
			return fmt.Errorf("expected %s's last comment to start %q, got %+v", id, want, comments)
		}
		return nil
	})
	ctx.Then(`^Ideas only is holding nothing, and the held ideas were kept in the released folder$`, func() error {
		held, err := c.files.Held(context.Background())
		if err != nil {
			return err
		}
		if len(held) != 0 {
			return fmt.Errorf("expected nothing held, %+v is", held)
		}
		r, err := c.rig()
		if err != nil {
			return err
		}
		notes, err := r.memory.NotesWithPrefix(context.Background(), application.PosternHeldPrefix)
		if err != nil || len(notes) != 0 {
			return fmt.Errorf("expected no held marks, got %+v (%v)", notes, err)
		}
		kept, _ := filepath.Glob(filepath.Join(c.dir, "ideas-only", "released", "*.jsonl"))
		if len(kept) != 1 {
			return fmt.Errorf("expected one file in the released folder, found %v", kept)
		}
		raw, err := os.ReadFile(kept[0])
		if err != nil || strings.Count(string(raw), "\n") != 3 {
			return fmt.Errorf("expected three ideas in %s, it holds %q (%v)", kept[0], raw, err)
		}
		return nil
	})
	ctx.Then(`^Ideas only is holding (\d+) ideas?$`, func(n int) error {
		held, err := c.files.Held(context.Background())
		if err != nil {
			return err
		}
		if len(held) != n {
			return fmt.Errorf("expected %d held, %+v is", n, held)
		}
		return nil
	})
	ctx.Then(`^the postern inbox counts (\d+) unread$`, func(want int) error {
		r, err := c.rig()
		if err != nil {
			return err
		}
		var out bytes.Buffer
		got, err := r.inboxFor(c, &out).UnreadCount(context.Background())
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("expected %d unread, the inbox counts %d", want, got)
		}
		return nil
	})
	ctx.Then(`^the postern inbox printed "([^"]*)"$`, func(text string) error { return c.inboxPrinted(text, true) })
	ctx.Then(`^the postern inbox did not print "([^"]*)"$`, func(text string) error { return c.inboxPrinted(text, false) })
}

func (c *ideasOnlyContext) inboxPrinted(text string, want bool) error {
	r, err := c.rig()
	if err != nil {
		return err
	}
	if got := strings.Contains(r.inbox.String(), text); got != want {
		return fmt.Errorf("expected the postern inbox to print %q: %t, it printed:\n%s", text, want, r.inbox.String())
	}
	return nil
}

func (c *ideasOnlyContext) cardItem(n int, text, says string) error {
	cards, err := c.cardsSent()
	if err != nil {
		return err
	}
	if len(cards) == 0 || n < 1 || n > len(cards[0].Items) {
		return fmt.Errorf("no item %d on the card sent: %+v", n, cards)
	}
	item := cards[0].Items[n-1]
	if !strings.HasPrefix(item.Text, text) || !strings.Contains(item.Text, says) {
		return fmt.Errorf("expected item %d to read %q and say %q, it reads %q", n, text, says, item.Text)
	}
	return nil
}

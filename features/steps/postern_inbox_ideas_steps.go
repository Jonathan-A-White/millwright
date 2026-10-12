package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/ideasonly"

	"github.com/cucumber/godog"
)

// posternIdeasClock is the time Ideas only is read at in the postern inbox's
// scenarios: two hours after every fake record's stamp.
var posternIdeasClock = posternReplyStamp.Add(2 * time.Hour)

// ideasMode is the Ideas only the inbox is wired with, nil when a scenario
// does not use it. Its files live under the scenario's throwaway home.
func (c *posternInboxContext) ideasMode() application.IdeasOnlyMode {
	return application.IdeasOnlyMode{
		Reader: c.backend, Files: ideasonly.New(filepath.Join(c.home, "ideas-only")),
		GovernorKey: c.governorKey, Now: func() time.Time { return posternIdeasClock },
	}
}

func (c *posternInboxContext) ideasOnlyIsOn(until string) error {
	at, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return err
	}
	c.backend.IdeasOnlyNow = application.IdeasOnlyNow{On: true, Until: at.Unix(), By: application.IdeasOnlyByUntil}
	c.ideas = true
	return nil
}

func (c *posternInboxContext) heldIdeas() ([]application.IdeasOnlyHeld, error) {
	return c.ideasMode().Files.Held(context.Background())
}

func (c *posternInboxContext) registerPosternIdeasOnlySteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^Ideas only is on until "([^"]*)" for the postern inbox$`, c.ideasOnlyIsOn)
	ctx.Given(`^Ideas only is off for the postern inbox$`, func() error {
		c.backend.IdeasOnlyNow = application.IdeasOnlyNow{}
		c.ideas = true
		return nil
	})
	ctx.Given(`^the note of Ideas only says it is on until "([^"]*)" for the postern inbox$`, func(until string) error {
		at, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return err
		}
		c.ideas = true
		return c.ideasMode().Files.SetState(context.Background(), application.IdeasOnlyState{On: true, Until: at.Unix()})
	})
	ctx.Given(`^the backend does not answer the Ideas only route for the postern inbox$`, func() error {
		c.backend.IdeasOnlyErr = errors.New("the postern backend said 404")
		return nil
	})
	ctx.When(`^Ideas only ends for the postern inbox$`, func() error {
		c.backend.IdeasOnlyNow = application.IdeasOnlyNow{}
		return nil
	})

	ctx.Given(`^Ideas only is holding "([^"]*)" for the postern inbox$`, func(txid string) error {
		c.ideas = true
		records, err := c.backend.Messages(context.Background(), 0)
		if err != nil {
			return err
		}
		for _, r := range records {
			if r.Txid != txid {
				continue
			}
			if err := c.ideasMode().Files.Hold(context.Background(), application.IdeasOnlyHeld{
				Txid: txid, Seq: r.Seq, Class: r.Class, Channel: "general", Text: "kept", When: posternReplyStamp.Format(time.RFC3339),
			}); err != nil {
				return err
			}
			return c.memory.SetNote(context.Background(), application.PosternHeldKey(txid), "held for after 21:00 UTC: "+txid)
		}
		return fmt.Errorf("no record %s has been added", txid)
	})
	ctx.Then(`^one card titled "([^"]*)" was sent to the Governor by the hook$`, func(title string) error {
		var cards []string
		for _, raw := range c.backend.Delivered() {
			var payload application.PosternPayload
			if err := json.Unmarshal(raw, &payload); err != nil {
				return err
			}
			if payload.Class != application.CardClass {
				continue
			}
			text, _, err := c.cipher.Decrypt("governor", payload.Ct)
			if err != nil {
				return err
			}
			var card struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal([]byte(text), &card); err != nil {
				return err
			}
			cards = append(cards, card.Title)
		}
		if len(cards) != 1 || cards[0] != title {
			return fmt.Errorf("expected one card titled %q, got %q", title, cards)
		}
		return nil
	})
	ctx.Then(`^the idea "([^"]*)" is held in "([^"]*)" saying "([^"]*)"$`, func(txid, channel, text string) error {
		if err := c.itSucceeds(); err != nil {
			return err
		}
		held, err := c.heldIdeas()
		if err != nil {
			return err
		}
		for _, idea := range held {
			if idea.Txid == txid {
				if idea.Channel != channel || idea.Text != text {
					return fmt.Errorf("expected %s held in %q saying %q, it is held in %q saying %q", txid, channel, text, idea.Channel, idea.Text)
				}
				return nil
			}
		}
		return fmt.Errorf("expected %s to be held, the held file has %+v", txid, held)
	})
	ctx.Then(`^the idea "([^"]*)" kept the file it carried$`, func(txid string) error {
		held, err := c.heldIdeas()
		if err != nil {
			return err
		}
		for _, idea := range held {
			if idea.Txid == txid {
				if idea.Attachment == "" || !strings.HasPrefix(idea.Attachment, c.attachDir) {
					return fmt.Errorf("expected %s to keep a file under %s, it keeps %q", txid, c.attachDir, idea.Attachment)
				}
				if _, err := os.Stat(idea.Attachment); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("expected %s to be held", txid)
	})
	ctx.Then(`^no idea is held$`, func() error { return c.ideasHeld(0) })
	ctx.Then(`^(\d+) ideas? (?:is|are) held$`, c.ideasHeld)
	ctx.Then(`^the txid "([^"]*)" is not marked applied$`, func(txid string) error {
		note, err := c.memory.Note(context.Background(), application.PosternAppliedKey(txid))
		if err != nil {
			return err
		}
		if note != "" {
			return fmt.Errorf("expected %s not to be marked applied, it is: %q", txid, note)
		}
		return nil
	})
	ctx.Then(`^the Governor was told "([^"]*)" re "([^"]*)"$`, func(want, re string) error {
		return c.toldTheGovernor(want, re, "")
	})
	ctx.Then(`^the Governor was sent (\d+) messages? in all$`, func(n int) error {
		if got := len(c.backend.Delivered()); got != n {
			return fmt.Errorf("expected %d message sent to the Governor, %d were", n, got)
		}
		return nil
	})
}

func (c *posternInboxContext) ideasHeld(n int) error {
	if err := c.itSucceeds(); err != nil {
		return err
	}
	held, err := c.heldIdeas()
	if err != nil {
		return err
	}
	if len(held) != n {
		return fmt.Errorf("expected %d ideas held, the file holds %d: %+v", n, len(held), held)
	}
	return nil
}

// toldTheGovernor checks that one message sent back to the Governor says want
// and answers the post re.
func (c *posternInboxContext) toldTheGovernor(want, re, bead string) error {
	var told []string
	for _, raw := range c.backend.Delivered() {
		var payload application.PosternPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		text, _, err := c.cipher.Decrypt("governor", payload.Ct)
		if err != nil {
			return err
		}
		var body application.PosternThreadedMessage
		if err := json.Unmarshal([]byte(text), &body); err == nil && body.Re == re && strings.HasPrefix(body.Text, want) {
			return nil
		}
		told = append(told, text)
	}
	return fmt.Errorf("expected the Governor told %q re %s, got %q", want, re, told)
}

package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/ideasonly"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// talkIdeasClock is the time Ideas only is read at in mw talk wait's scenarios: an injected clock, so a
// scenario's end of the mode is placed on it ("9 hours on"), never on a date the real clock can pass.
var talkIdeasClock = time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)

// talkIdeasBackend is the backend's Ideas only: what GET /api/ideas-only/{key}
// answers, set by a scenario, and the records the Mayor delivered.
type talkIdeasBackend struct {
	on        bool
	until     int64
	delivered [][]byte
}

// ideasRoute is GET /api/ideas-only/{pubkey}, docs/api.md's shape.
func (b *talkBackend) ideasRoute(w http.ResponseWriter, _ *http.Request) {
	b.mu.Lock()
	on, until := b.ideas.on, b.ideas.until
	b.mu.Unlock()
	by := ""
	if on {
		by = "until"
	}
	json.NewEncoder(w).Encode(map[string]any{
		"setting": map[string]any{"until": until, "reaches": map[string]any{}},
		"state":   map[string]any{"on": on, "until": until, "by": by},
	})
}

// deliverRoute is POST /api/messages: a record the Mayor delivers, kept for a
// step to read. It is not indexed, so nothing wakes the wait for it.
func (b *talkBackend) deliverRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScriptHex string `json:"scriptHex"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	payload, ok := postern.DecodeRecordScript(req.ScriptHex)
	if !ok {
		http.Error(w, "not a record script", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.ideas.delivered = append(b.ideas.delivered, payload)
	n := len(b.ideas.delivered)
	b.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, `{"txid":"direct:out-%d","seq":0}`, n)
}

// ideasEvent tells every open stream a key's Ideas only changed.
func (b *talkBackend) ideasEvent(key string, on bool, until int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for conn := range b.conns {
		conn.out <- fmt.Sprintf("event: ideas-only\ndata: {\"key\":%q,\"on\":%t,\"until\":%d}\n\n", key, on, until)
	}
}

func (c *talkWaitContext) ideasFiles() *ideasonly.Files {
	return ideasonly.New(filepath.Join(c.home, "ideas-only"))
}

// ideasMode is the Ideas only mw talk wait is wired with.
func (c *talkWaitContext) ideasMode() application.IdeasOnlyMode {
	return application.IdeasOnlyMode{
		Reader: c.client, Files: c.ideasFiles(), GovernorKey: c.governorKey,
		Now: func() time.Time {
			if at := c.clockAt.Load(); at != 0 {
				return time.Unix(at, 0)
			}
			return talkIdeasClock
		},
	}
}

// ideasRelease is how the wait gives the ideas back: an inbox that can send the
// card, wired as mw talk wait's command wires it.
func (c *talkWaitContext) ideasRelease(mode *application.IdeasOnlyMode) *application.IdeasOnlyRelease {
	cipher := apptest.NewFakeCipher()
	cards := &application.Cards{
		Postern: c.client, Cipher: cipher, Keys: c.keys, GovernorKey: c.governorKey,
		Now: func() time.Time { return talkIdeasClock },
	}
	return &application.IdeasOnlyRelease{Inbox: application.PosternInbox{
		Postern: c.client, Cipher: cipher, Keys: c.keys, Memory: c.memory, Tracker: c.memory,
		GovernorKey: c.governorKey, Ideas: mode, Cards: cards,
		Sender: &application.PosternSend{Postern: c.client, Cipher: cipher, Keys: c.keys, GovernorKey: c.governorKey},
	}}
}

func (c *talkWaitContext) delivered() ([]string, []application.PosternPayload, error) {
	c.backend.mu.Lock()
	raw := append([][]byte(nil), c.backend.ideas.delivered...)
	c.backend.mu.Unlock()
	var texts []string
	var payloads []application.PosternPayload
	for _, r := range raw {
		var payload application.PosternPayload
		if err := json.Unmarshal(r, &payload); err != nil {
			return nil, nil, err
		}
		text, _, err := apptest.NewFakeCipher().Decrypt("mayor", payload.Ct)
		if err != nil {
			return nil, nil, err
		}
		texts = append(texts, text)
		payloads = append(payloads, payload)
	}
	return texts, payloads, nil
}

// cardsSent is every card the Mayor delivered, opened.
func (c *talkWaitContext) cardsSent() ([]domain.Card, error) {
	texts, payloads, err := c.delivered()
	if err != nil {
		return nil, err
	}
	var cards []domain.Card
	for n, text := range texts {
		if payloads[n].Class != application.CardClass {
			continue
		}
		var card domain.Card
		if err := json.Unmarshal([]byte(text), &card); err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func (c *talkWaitContext) registerIdeasOnlySteps(ctx *godog.ScenarioContext) {
	ctx.Given(`^Ideas only is on until (\d+) hours on for mw talk wait$`, func(hours int) error {
		c.backend.mu.Lock()
		c.backend.ideas.on, c.backend.ideas.until = true, talkIdeasClock.Add(time.Duration(hours)*time.Hour).Unix()
		c.backend.mu.Unlock()
		c.ideas = true
		return nil
	})
	ctx.Given(`^Ideas only is off for mw talk wait$`, func() error {
		c.ideas = true
		return nil
	})
	ctx.Given(`^the hook has already kept "([^"]*)"$`, func(txid string) error {
		mode := c.ideasMode()
		state, err := mode.Refresh(context.Background())
		if err != nil {
			return err
		}
		if err := c.ideasFiles().Hold(context.Background(), application.IdeasOnlyHeld{Txid: txid, Seq: 1, Class: "message", Channel: "general", Text: "kept"}); err != nil {
			return err
		}
		return c.memory.SetNote(context.Background(), application.PosternHeldKey(txid), "held for after "+application.IdeasOnlyUntilLabel(state, talkIdeasClock)+": "+txid)
	})
	ctx.When(`^a "([^"]*)" record from "([^"]*)" to the Mayor is indexed$`, func(class, from string) error {
		return c.record(class, c.mayor, from, class+" text", true)
	})
	ctx.When(`^the backend says the Governor's Ideas only has ended$`, func() error {
		c.backend.mu.Lock()
		c.backend.ideas.on, c.backend.ideas.until = false, 0
		c.backend.mu.Unlock()
		c.lastEvent = time.Now()
		c.backend.ideasEvent(c.governorKey, false, 0)
		return nil
	})
	ctx.When(`^the backend says the Ideas only of "([^"]*)" has ended$`, func(key string) error {
		c.lastEvent = time.Now()
		c.backend.ideasEvent(key, false, 0)
		return nil
	})
	ctx.When(`^the wait comes to its time limit$`, func() error {
		select {
		case <-c.run.done:
			return c.run.err
		case <-time.After(15 * time.Second):
			return fmt.Errorf("mw talk wait had not come to its time limit; so far it printed:\n%s", c.run.out.String())
		}
	})

	ctx.When(`^(\d+) milliseconds go by on the host running mw talk wait$`, func(ms int) error {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return nil
	})

	ctx.When(`^the clock of Ideas only passes (\d+) hours and (\d+) seconds? on for mw talk wait$`, func(hours, seconds int) error {
		c.lastEvent = time.Now()
		c.clockAt.Store(talkIdeasClock.Add(time.Duration(hours)*time.Hour + time.Duration(seconds)*time.Second).Unix())
		return nil
	})
	ctx.Then(`^the wait has kept (\d+) ideas?$`, func(n int) error {
		return c.waitFor(fmt.Sprintf("the wait to keep %d ideas", n), func() bool {
			held, err := c.ideasFiles().Held(context.Background())
			return err == nil && len(held) == n
		})
	})
	// The wait answers a kept idea after it writes it to the held file, so a
	// step that is to find the wait idle again waits for the answer too.
	ctx.Then(`^the wait has answered the Governor (\d+) times?$`, func(n int) error {
		return c.waitFor(fmt.Sprintf("the wait to answer the Governor %d times", n), func() bool {
			texts, _, err := c.delivered()
			return err == nil && len(texts) == n
		})
	})
	ctx.Then(`^one card titled "([^"]*)" was sent to the Governor by the wait$`, func(title string) error {
		cards, err := c.cardsSent()
		if err != nil {
			return err
		}
		if len(cards) != 1 || cards[0].Title != title {
			return fmt.Errorf("expected one card titled %q, got %+v", title, cards)
		}
		return nil
	})
	ctx.Then(`^no card was sent to the Governor by the wait$`, func() error {
		cards, err := c.cardsSent()
		if err != nil {
			return err
		}
		if len(cards) != 0 {
			return fmt.Errorf("expected no card, got %+v", cards)
		}
		return nil
	})
	ctx.Then(`^the note of Ideas only comes to say it is off$`, func() error {
		return c.waitFor("the note of Ideas only to say off", func() bool {
			state, err := c.ideasFiles().State(context.Background())
			return err == nil && !state.On
		})
	})
	ctx.Then(`^Ideas only kept "([^"]*)" from the (talk|call|general), saying "([^"]*)"$`, func(txid, channel, text string) error {
		held, err := c.ideasFiles().Held(context.Background())
		if err != nil {
			return err
		}
		for _, idea := range held {
			if idea.Txid == txid {
				if idea.Channel != channel || idea.Text != text {
					return fmt.Errorf("expected %s kept from %q saying %q, it is %q saying %q", txid, channel, text, idea.Channel, idea.Text)
				}
				return nil
			}
		}
		return fmt.Errorf("expected %s to be kept, the held file has %+v", txid, held)
	})
	ctx.Then(`^Ideas only is keeping (\d+) ideas?$`, func(n int) error {
		held, err := c.ideasFiles().Held(context.Background())
		if err != nil {
			return err
		}
		if len(held) != n {
			return fmt.Errorf("expected %d ideas kept, the file holds %d: %+v", n, len(held), held)
		}
		return nil
	})
	ctx.Then(`^the Governor was answered (\d+) times? in all$`, func(n int) error {
		texts, _, err := c.delivered()
		if err != nil {
			return err
		}
		if len(texts) != n {
			return fmt.Errorf("expected %d answers sent to the Governor, %d were: %q", n, len(texts), texts)
		}
		return nil
	})
	ctx.Then(`^the Governor was answered in talk "([^"]*)" turn (\d+) saying "([^"]*)"$`, func(id string, turn int, want string) error {
		texts, payloads, err := c.delivered()
		if err != nil {
			return err
		}
		for n, text := range texts {
			var answer application.TalkTurn
			if payloads[n].Class == "talk" && json.Unmarshal([]byte(text), &answer) == nil &&
				answer.Talk.ID == id && answer.Talk.Turn == turn && answer.Role == application.TalkRoleAnswer && strings.HasPrefix(answer.Text, want) {
				return nil
			}
		}
		return fmt.Errorf("expected a talk answer to turn %d of %s saying %q, got %q", turn, id, want, texts)
	})
	ctx.Then(`^mw talk wait told the Governor "([^"]*)" re "([^"]*)"$`, func(want, re string) error {
		texts, _, err := c.delivered()
		if err != nil {
			return err
		}
		for _, text := range texts {
			var body application.PosternThreadedMessage
			if json.Unmarshal([]byte(text), &body) == nil && body.Re == re && strings.HasPrefix(body.Text, want) {
				return nil
			}
		}
		return fmt.Errorf("expected the Governor told %q re %s, got %q", want, re, texts)
	})
}

package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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

const realtimeGovernorKey = "realtime-governor-pubkey-hex"

// verifyRealtimeContext is the follower's pass over a fake postern backend, with
// the hook that runs mw verify match for the pictures it saves, the match
// standing on the stand-in agent.
type verifyRealtimeContext struct {
	dir     string
	keys    *postern.KeyFile
	pubKey  string
	backend *apptest.FakePostern
	cipher  *apptest.FakeCipher
	tracker *apptest.FakeTracker
	beads   *apptest.FakeVerifyBeads
	grinder *apptest.FakeGrinder
	mail    *apptest.FakeMailbox
	hook    *application.VerifyHook
	said    bytes.Buffer
	hold    chan struct{}
	entered chan struct{}
	answers []map[string]any
	passErr error
}

// InitializeVerifyRealtimeScenario registers the steps of features/verify_realtime.feature.
func InitializeVerifyRealtimeScenario(ctx *godog.ScenarioContext) {
	c := &verifyRealtimeContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = verifyRealtimeContext{
			backend: apptest.NewFakePostern(),
			cipher:  &apptest.FakeCipher{From: realtimeGovernorKey},
			tracker: apptest.NewFakeTracker(),
			beads:   &apptest.FakeVerifyBeads{Comments: map[string][]application.Comment{}},
			grinder: &apptest.FakeGrinder{},
			mail:    apptest.NewFakeMailbox(),
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.hold != nil {
			close(c.hold)
			c.hook.Wait()
		}
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a realtime fixture with the demo "([^"]*)"$`, c.fixture)
	ctx.Given(`^he sent the picture "([^"]*)" with the words "([^"]*)"$`, c.sentPicture)
	ctx.Given(`^he sent the words "([^"]*)" with no picture as "([^"]*)"$`, func(words, txid string) error { return c.post(txid, map[string]any{"text": words}) })
	ctx.Given(`^the realtime stand-in answers "([^"]*)" from "([^"]*)" showing "([^"]*)" at (\d+\.\d+) and waits$`, c.standInWaits)
	ctx.Given(`^the realtime stand-in answers "([^"]*)" from "([^"]*)" showing "([^"]*)" at (\d+\.\d+) and answers in a moment$`, c.standInAnswersSoon)
	ctx.When(`^the follower's pass runs$`, c.pass)
	ctx.When(`^the apply pass runs$`, c.applyPass)
	ctx.When(`^the realtime stand-in is let go$`, c.letGo)
	ctx.Then(`^the pass has returned while the realtime stand-in is still waiting$`, c.passReturnedFirst)
	ctx.Then(`^the realtime stand-in was called once$`, func() error { return c.calls(1) })
	ctx.Then(`^the realtime stand-in was not called$`, func() error { return c.calls(0) })
	ctx.Then(`^the realtime bead "([^"]*)" has one comment opening PROPOSED that names "([^"]*)" and "([^"]*)"$`, c.hasOneProposal)
	ctx.Then(`^the realtime follower said "([^"]*)"$`, func(want string) error {
		if !strings.Contains(c.said.String(), want) {
			return fmt.Errorf("the follower said %q, not %q", c.said.String(), want)
		}
		return nil
	})
	ctx.Then(`^no realtime bead was closed$`, func() error {
		if len(c.beads.Shut) != 0 {
			return fmt.Errorf("closed: %+v", c.beads.Shut)
		}
		return nil
	})
}

func (c *verifyRealtimeContext) fixture(demo string) error {
	dir, err := os.MkdirTemp("", "mw-verify-realtime-")
	if err != nil {
		return err
	}
	c.dir = dir
	c.keys = postern.New(filepath.Join(dir, "postern.key"))
	if err := c.keys.Generate(); err != nil {
		return err
	}
	if c.pubKey, _, err = c.keys.PublicKey(); err != nil {
		return err
	}
	c.beads.Work.Ready = append(c.beads.Work.Ready, application.StoryDetail{
		Story: domain.Story{ID: demo, Title: "Demo " + demo}, Status: application.StatusOpen, EpicID: "e-1",
		Labels: []string{application.LabelDemo, "hitl"}, Description: "Open the app, tap Save and look for the green tick",
	})
	return nil
}

func (c *verifyRealtimeContext) post(txid string, body map[string]any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	sealed, err := c.cipher.Encrypt(c.pubKey, string(raw))
	if err != nil {
		return err
	}
	c.backend.AddRecord(application.PosternRecord{Txid: txid, Class: "message", From: realtimeGovernorKey, To: c.pubKey, Ciphertext: sealed})
	return nil
}

func (c *verifyRealtimeContext) sentPicture(txid, words string) error {
	picture := bytes.Repeat([]byte{0x89, 0x50, 0x4e, 0x47}, 300)
	sealed, err := c.cipher.Encrypt(c.pubKey, string(picture))
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	c.backend.SetBlob(hash, raw)
	return c.post(txid, map[string]any{
		"text": words, "attachments": []any{application.PosternAttachment{Hash: hash, Size: int64(len(raw)), Mime: "image/png"}},
	})
}

func (c *verifyRealtimeContext) standInWaits(bead, txid, shows, confidence string) error {
	var level float64
	if _, err := fmt.Sscanf(confidence, "%g", &level); err != nil {
		return err
	}
	answers, err := json.Marshal(map[string]any{"matches": []map[string]any{{"bead": bead, "txid": txid, "shows": shows, "confidence": level}}})
	if err != nil {
		return err
	}
	c.hold, c.entered = make(chan struct{}), make(chan struct{}, 4)
	c.grinder.Result = application.SessionResult{Answer: answers}
	c.grinder.Hold, c.grinder.Entered = c.hold, c.entered
	return nil
}

// standInAnswersSoon is standInWaits that lets itself go a moment after the
// grind begins, for a pass that waits on the match.
func (c *verifyRealtimeContext) standInAnswersSoon(bead, txid, shows, confidence string) error {
	if err := c.standInWaits(bead, txid, shows, confidence); err != nil {
		return err
	}
	hold, entered := c.hold, c.entered
	c.hold = nil
	go func() {
		<-entered
		time.Sleep(200 * time.Millisecond)
		close(hold)
	}()
	return nil
}

func (c *verifyRealtimeContext) wiredInbox() application.PosternInbox {
	inboxDir := filepath.Join(c.dir, "postern", "inbox")
	if c.hook == nil {
		c.hook = c.newHook(inboxDir)
	}
	return application.PosternInbox{
		Postern: c.backend, Cipher: c.cipher, Keys: c.keys,
		Memory: c.tracker, Tracker: c.tracker, GovernorKey: realtimeGovernorKey, AttachmentDir: inboxDir,
		Pictures: c.hook, Out: &bytes.Buffer{},
	}
}

// applyPass is mw postern inbox --apply, as the backend's on-message hook runs it.
func (c *verifyRealtimeContext) applyPass() error {
	_, c.passErr = c.wiredInbox().Apply(context.Background())
	return c.passErr
}

func (c *verifyRealtimeContext) newHook(inboxDir string) *application.VerifyHook {
	return &application.VerifyHook{
		Err: &c.said,
		Match: func(ctx context.Context, txids []string) (application.VerifyResult, error) {
			return application.VerifyMatch{
				Beads: c.beads, Grinder: c.grinder, Mailbox: c.mail,
				Settings: application.VerifySettings{Model: "haiku", Effort: "medium", Confidence: 0.8},
				InboxDir: inboxDir, Host: "laptop", Out: &bytes.Buffer{},
			}.Run(ctx, application.VerifyOptions{Txids: txids})
		},
	}
}

// pass is one pass of the follower's, over the hook the scenario shares with the apply pass.
func (c *verifyRealtimeContext) pass() error {
	taps := &application.PosternTaps{Inbox: c.wiredInbox()}
	c.passErr = taps.Tap(context.Background())
	return c.passErr
}

func (c *verifyRealtimeContext) passReturnedFirst() error {
	select {
	case <-c.entered:
	case <-time.After(10 * time.Second):
		return fmt.Errorf("the match never reached the stand-in")
	}
	if c.passErr != nil {
		return c.passErr
	}
	if c.written("d-green") != nil {
		return fmt.Errorf("the match had finished when the pass returned")
	}
	return nil
}

func (c *verifyRealtimeContext) letGo() error {
	close(c.hold)
	c.hold = nil
	c.hook.Wait()
	return nil
}

func (c *verifyRealtimeContext) written(bead string) []string {
	var texts []string
	for _, w := range c.beads.Written {
		if w.ID == bead {
			texts = append(texts, w.Text)
		}
	}
	return texts
}

func (c *verifyRealtimeContext) hasOneProposal(bead, txid, shows string) error {
	texts := c.written(bead)
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "PROPOSED ") || !strings.Contains(texts[0], txid) || !strings.Contains(texts[0], shows) {
		return fmt.Errorf("expected one PROPOSED comment on %s naming %s and %q, got %q", bead, txid, shows, texts)
	}
	return nil
}

func (c *verifyRealtimeContext) calls(want int) error {
	if c.hook != nil && c.hold == nil {
		c.hook.Wait()
	}
	if got := len(c.grinder.Calls()); got != want {
		return fmt.Errorf("expected the stand-in called %d times, got %d", want, got)
	}
	return nil
}

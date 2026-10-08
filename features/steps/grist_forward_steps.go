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

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// forwardContext is what features/grist_forward.feature adds to a mill: a fake
// mailbox for the Mayor's mail, and the real store the pictures are kept in, on
// a temp directory.
type forwardContext struct {
	c       *gristContext
	mailbox *apptest.FakeMailbox
	store   *grist.Forwards
}

// mailboxPort is the mailbox, or nothing (a nil port, not a nil *FakeMailbox)
// before the scenario made one.
func (f *forwardContext) mailboxPort() application.Mailbox {
	if f.mailbox == nil {
		return nil
	}
	return f.mailbox
}

func (f *forwardContext) keeper() application.GristForwardStore {
	if f.store == nil {
		return nil
	}
	return f.store
}

func registerGristForward(ctx *godog.ScenarioContext, c *gristContext) {
	f := &c.forward

	// After the reset of the grist feature's own Before, which clears f.c.
	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*f = forwardContext{c: c}
		return ctx, nil
	})

	ctx.Given(`^the mill keeps its mail and the pictures it forwards under its state directory$`, f.keepsMail)
	ctx.Given(`^the app's grind "([^"]*)" forwards to "([^"]*)" and takes up to (\d+) pictures$`, f.grindForwards)
	ctx.Given(`^the mayor's mail cannot be sent$`, f.mailFails)
	ctx.Given(`^the phone sends a "([^"]*)" "([^"]*)" grist, version "([^"]*)", saying "([^"]*)" with (\d+) photos?$`, f.phoneSays)

	ctx.Then(`^one mail went to "([^"]*)" titled "([^"]*)"$`, f.oneMail)
	ctx.Then(`^that mail has the text "([^"]*)", the sender's key and the paths of (\d+) kept pictures$`, f.mailHas)
	ctx.Then(`^the (\d+) kept pictures are the photos as they were sent$`, f.keptAreThePhotos)
	ctx.Then(`^the app's decrypted reply says "([^"]*)" with the answer (.+)$`, f.replySays)
	ctx.Then(`^the mill's record holds one line for the grist with no tokens$`, f.recordHasNoTokens)
	ctx.Then(`^the mill sent no mail$`, f.noMail)
	ctx.Then(`^no kept pictures exist$`, f.noKeptPictures)
}

func (f *forwardContext) keepsMail() error {
	f.mailbox = apptest.NewFakeMailbox()
	f.store = grist.NewForwards(filepath.Join(f.c.home, "state"))
	return nil
}

func (f *forwardContext) grindForwards(kind, to string, most int) error {
	file := map[string]any{
		"grind": 1, "app": "cairn", "kind": kind, "versions": []string{"1"},
		"forward":     to,
		"attachments": map[string]any{"min": 0, "max": most, "mime": []string{"image/jpeg", "image/png"}, "maxBytes": 4194304},
	}
	raw, err := json.Marshal(file)
	if err != nil {
		return err
	}
	f.c.grinds.SetFile(f.c.checkout("cairn"), f.c.commit, "grinds/"+kind+".json", raw)
	return nil
}

func (f *forwardContext) mailFails() error {
	f.mailbox.Err = errors.New("the mailbox is out of reach")
	return nil
}

func (f *forwardContext) phoneSays(app, kind, v, text string, photos int) error {
	input, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return err
	}
	f.c.askInput = input
	return f.c.thePhoneSends(app, kind, v, photos)
}

// sent is the mail the mill sent, the one message waiting for the mayor.
func (f *forwardContext) sent() (application.Message, error) {
	messages, err := f.mailbox.Inbox(context.Background(), "mayor")
	if err != nil {
		return application.Message{}, err
	}
	if len(messages) != 1 || f.mailbox.Writes() != 1 {
		return application.Message{}, fmt.Errorf("expected one mail to the mayor, got %d (%d writes):\n%s", len(messages), f.mailbox.Writes(), f.c.out.String())
	}
	return messages[0], nil
}

func (f *forwardContext) oneMail(to, subject string) error {
	m, err := f.sent()
	if err != nil {
		return err
	}
	if m.To != to || m.Subject != subject {
		return fmt.Errorf("expected a mail to %s titled %q, got one to %s titled %q", to, subject, m.To, m.Subject)
	}
	if want := application.SeatIdentity(application.MwSeat, f.c.host); m.From != want {
		return fmt.Errorf("expected the mail from %s, got %s", want, m.From)
	}
	return nil
}

// pathsIn is the picture paths a mail body names: its lines under the state
// directory.
func (f *forwardContext) pathsIn(body string) []string {
	root := filepath.Join(f.c.home, "state", "forwarded")
	var paths []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, root+string(filepath.Separator)) {
			paths = append(paths, line)
		}
	}
	return paths
}

func (f *forwardContext) mailHas(text string, pictures int) error {
	m, err := f.sent()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(m.Body, text+"\n") {
		return fmt.Errorf("expected the mail to start with the text %q, got:\n%s", text, m.Body)
	}
	if !strings.Contains(m.Body, f.c.phoneKey) {
		return fmt.Errorf("expected the mail to hold the sender's key %s, got:\n%s", f.c.phoneKey, m.Body)
	}
	paths := f.pathsIn(m.Body)
	if len(paths) != pictures {
		return fmt.Errorf("expected %d picture paths in the mail, got %d:\n%s", pictures, len(paths), m.Body)
	}
	for _, p := range paths {
		if !strings.Contains(p, f.c.grist.Txid) {
			return fmt.Errorf("expected the picture %s kept in a directory named for the grist %s", p, f.c.grist.Txid)
		}
	}
	return nil
}

func (f *forwardContext) keptAreThePhotos(n int) error {
	m, err := f.sent()
	if err != nil {
		return err
	}
	paths := f.pathsIn(m.Body)
	if len(paths) != n {
		return fmt.Errorf("expected %d kept pictures, the mail names %d", n, len(paths))
	}
	want := map[string]bool{}
	for _, body := range f.c.photos {
		want[string(body)] = true
	}
	for _, p := range paths {
		got, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !want[string(got)] {
			return fmt.Errorf("the kept picture %s is not a photo that was sent: %q", p, got)
		}
	}
	return nil
}

func (f *forwardContext) replySays(status, answer string) error {
	_, _, got, err := f.c.onlyAnswer()
	if err != nil {
		return err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(answer)); err != nil {
		return err
	}
	if got.Status != status || string(got.Answer) != compact.String() {
		return fmt.Errorf("expected %s with the answer %s, got %s with %s", status, compact.String(), got.Status, got.Answer)
	}
	return nil
}

func (f *forwardContext) recordHasNoTokens() error {
	lines, err := f.c.state.Lines(context.Background())
	if err != nil {
		return err
	}
	if len(lines) != 1 {
		return fmt.Errorf("expected one line in the record, got %d", len(lines))
	}
	if line := lines[0]; line.Tokens != 0 || line.Fuel != nil || line.Status != application.GristAnswered {
		return fmt.Errorf("expected an answered line with no tokens, got %+v", line)
	}
	return nil
}

func (f *forwardContext) noMail() error {
	if n := f.mailbox.Writes(); n != 0 {
		return fmt.Errorf("expected no mail, %d were sent", n)
	}
	return nil
}

func (f *forwardContext) noKeptPictures() error {
	if _, err := os.Stat(filepath.Join(f.c.home, "state", "forwarded")); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("expected no pictures kept, stat said: %v", err)
	}
	return nil
}

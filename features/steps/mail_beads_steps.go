package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/beads"

	"github.com/cucumber/godog"
)

// mailBeadsContext is the world of the scenarios that say what mail does among
// real beads: throwaway beads databases in temp directories, driven by a real
// bd through the same Gateway mw uses. What mail is never offered as, and how it
// travels between two clones, are facts about bd, so a fake could only agree
// with itself. The scenarios skip when bd or git is not on PATH. Nothing here
// reaches the factory's own database.
type mailBeadsContext struct {
	root string

	// laptop and vps are the two clones of one database; a scenario with one
	// database uses laptop.
	laptop, vps *beads.Gateway

	epic    string
	stories map[string]string // title -> id
	mail    string            // id of the last mail filed
}

// InitializeMailBeadsScenario registers the steps of features/mail.feature that
// run against real beads.
func InitializeMailBeadsScenario(ctx *godog.ScenarioContext) {
	c := &mailBeadsContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = mailBeadsContext{stories: map[string]string{}}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.root != "" {
			os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a beads database that declares the mail type$`, c.aBeadsDatabase)
	ctx.Given(`^a story ready on "([^"]*)" called "([^"]*)" and a story ready on "([^"]*)" called "([^"]*)"$`, c.storiesReadyOn)
	ctx.Given(`^beads holds mail from "([^"]*)" to "([^"]*)" with the subject "([^"]*)"$`, c.beadsHoldsMail)
	ctx.Given(`^beads holds a reply from "([^"]*)" to "([^"]*)" to it$`, c.beadsHoldsAReply)
	ctx.Given(`^two clones of one beads database, the laptop's and the vps's$`, c.twoClones)

	ctx.When(`^the laptop clone sends mail from "([^"]*)" to "([^"]*)" with the subject "([^"]*)"$`, c.theLaptopSends)
	ctx.When(`^the laptop clone syncs$`, func() error { return c.sync(c.laptop) })
	ctx.When(`^the vps clone syncs$`, func() error { return c.sync(c.vps) })

	ctx.Then(`^the ready stories of "([^"]*)" are exactly "([^"]*)"$`, c.readyOnAre)
	ctx.Then(`^the stories ready elsewhere than "([^"]*)" are exactly "([^"]*)"$`, c.readyElsewhereAre)
	ctx.Then(`^the inbox of "([^"]*)" in the vps clone is empty$`, func(mailbox string) error { return c.inboxIsEmpty(c.vps, mailbox) })
	ctx.Then(`^the inbox of "([^"]*)" in the vps clone lists a message from "([^"]*)" with the subject "([^"]*)"$`,
		func(mailbox, from, subject string) error { return c.inboxLists(c.vps, mailbox, from, subject) })
	ctx.Then(`^the inbox of "([^"]*)" in the laptop clone lists a message from "([^"]*)" with the subject "([^"]*)"$`,
		func(mailbox, from, subject string) error { return c.inboxLists(c.laptop, mailbox, from, subject) })
}

// bdIn runs one command in a directory and says what it printed if it failed.
// The variables that point bd at a database of its own are left out, so that a
// session that carries them cannot steer a test at its own vault.
func bdIn(dir, program string, args ...string) error {
	cmd := exec.Command(program, args...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "BEADS_DIR=") && !strings.HasPrefix(entry, "BEADS_DB=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s in %s: %w\n%s", program, strings.Join(args, " "), dir, err, out)
	}
	return nil
}

// newClone makes a beads database in a directory of its own under the root, a
// clone of the database at remote when there is one, and reports its Gateway.
func (c *mailBeadsContext) newClone(name, remote string) (*beads.Gateway, error) {
	dir := filepath.Join(c.root, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, err
	}
	if err := bdIn(dir, "git", "init", "-q"); err != nil {
		return nil, err
	}
	args := []string{"init", "-p", "t", "--non-interactive", "--role", "maintainer", "--skip-agents", "--skip-hooks", "-q"}
	if remote != "" {
		args = append(args, "--remote", remote)
	}
	if err := bdIn(dir, beads.Program, args...); err != nil {
		return nil, err
	}
	return beads.New(dir), nil
}

// start makes the directory the scenario's databases live in, or skips the
// scenario when there is no bd or git to run them with.
func (c *mailBeadsContext) start() error {
	for _, program := range []string{beads.Program, "git"} {
		if _, err := exec.LookPath(program); err != nil {
			return godog.ErrSkip
		}
	}
	root, err := os.MkdirTemp("", "mw-mail-beads-")
	if err != nil {
		return err
	}
	c.root = root
	return nil
}

func (c *mailBeadsContext) aBeadsDatabase() error {
	if err := c.start(); err != nil {
		return err
	}
	gateway, err := c.newClone("laptop", "")
	if err != nil {
		return err
	}
	c.laptop = gateway
	return bdIn(gateway.Vault(), beads.Program, "config", "set", "types.custom", beads.TypeMail)
}

func (c *mailBeadsContext) storiesReadyOn(hostOne, titleOne, hostTwo, titleTwo string) error {
	ctx := context.Background()
	defaults := domain.Path{
		Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
		Model: domain.ModelSonnet, Effort: domain.EffortHigh, Formula: "tdd-feature", Host: hostOne,
	}
	epic, err := c.laptop.CreateEpic(ctx, application.NewEpic{Title: "Work", Defaults: defaults})
	if err != nil {
		return err
	}
	c.epic = epic
	for host, title := range map[string]string{hostOne: titleOne, hostTwo: titleTwo} {
		id, err := c.laptop.CreateStory(ctx, application.NewStory{
			EpicID: epic, Title: title, Overrides: domain.Path{Host: host},
		})
		if err != nil {
			return err
		}
		if err := c.laptop.ReleaseStory(ctx, id); err != nil {
			return err
		}
		c.stories[title] = id
	}
	return nil
}

func (c *mailBeadsContext) beadsHoldsMail(from, to, subject string) error {
	id, err := c.laptop.Send(context.Background(), application.NewMessage{From: from, To: to, Subject: subject, Body: "Nothing to work."})
	c.mail = id
	return err
}

func (c *mailBeadsContext) beadsHoldsAReply(from, to string) error {
	_, err := c.laptop.Send(context.Background(), application.NewMessage{
		From: from, To: to, Subject: "Re: reply", Body: "Nothing to work.", ReplyTo: c.mail,
	})
	return err
}

// titles is what a list of stories is called, sorted.
func titles(stories []application.StoryDetail) []string {
	found := make([]string, 0, len(stories))
	for _, story := range stories {
		found = append(found, story.Story.Title)
	}
	sort.Strings(found)
	return found
}

func (c *mailBeadsContext) expectExactly(what string, got []application.StoryDetail, want string) error {
	if have := titles(got); len(have) != 1 || have[0] != want {
		return fmt.Errorf("expected %s to be exactly %q, got %q", what, want, have)
	}
	return nil
}

// readyOnAre asks both ways a host is offered stories: the dispatcher's, which
// is everything ready and unclaimed, and the epic's own.
func (c *mailBeadsContext) readyOnAre(host, title string) error {
	ctx := context.Background()
	ready, err := c.laptop.ReadyForHost(ctx, host)
	if err != nil {
		return err
	}
	if err := c.expectExactly("what "+host+" is offered", ready, title); err != nil {
		return err
	}
	inEpic, err := c.laptop.ReadyStories(ctx, c.epic, host)
	if err != nil {
		return err
	}
	return c.expectExactly("what "+host+" is offered from the epic", inEpic, title)
}

func (c *mailBeadsContext) readyElsewhereAre(host, title string) error {
	elsewhere, err := c.laptop.WorkElsewhere(context.Background(), host)
	if err != nil {
		return err
	}
	return c.expectExactly("the work elsewhere than "+host, elsewhere, title)
}

// twoClones makes the laptop's database, gives it a remote to sync through, and
// clones it into the vps's, as two hosts share the factory's own.
func (c *mailBeadsContext) twoClones() error {
	if err := c.start(); err != nil {
		return err
	}
	remote := filepath.Join(c.root, "remote")
	if err := os.Mkdir(remote, 0o755); err != nil {
		return err
	}
	url := "file://" + remote

	laptop, err := c.newClone("laptop", "")
	if err != nil {
		return err
	}
	c.laptop = laptop
	for _, args := range [][]string{
		{"config", "set", "types.custom", beads.TypeMail},
		{"dolt", "remote", "add", "origin", url},
		{"dolt", "push"},
	} {
		if err := bdIn(laptop.Vault(), beads.Program, args...); err != nil {
			return err
		}
	}
	vps, err := c.newClone("vps", url)
	if err != nil {
		return err
	}
	c.vps = vps
	return nil
}

func (c *mailBeadsContext) theLaptopSends(from, to, subject string) error {
	_, err := c.laptop.Send(context.Background(), application.NewMessage{From: from, To: to, Subject: subject, Body: "The branch is up."})
	return err
}

func (c *mailBeadsContext) sync(clone *beads.Gateway) error {
	return clone.Sync(context.Background())
}

func (c *mailBeadsContext) inboxIsEmpty(clone *beads.Gateway, mailbox string) error {
	unread, err := clone.Inbox(context.Background(), mailbox)
	if err != nil {
		return err
	}
	if len(unread) != 0 {
		return fmt.Errorf("expected the inbox of %s to be empty, it holds %d messages, first %q", mailbox, len(unread), unread[0].Subject)
	}
	return nil
}

func (c *mailBeadsContext) inboxLists(clone *beads.Gateway, mailbox, from, subject string) error {
	unread, err := clone.Inbox(context.Background(), mailbox)
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

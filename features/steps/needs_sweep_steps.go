package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"

	"github.com/cucumber/godog"
)

var needsSweepNow = time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)

// needsSweepContext holds a real temp git repository for the rig, a fake
// tracker and a fake mailbox mw needs sweep runs against.
type needsSweepContext struct {
	dir     string
	runDir  string
	beads   *apptest.FakeNeedsSweep
	mail    *apptest.FakeMailbox
	out     bytes.Buffer
	err     error
	reverts map[string]string
}

// InitializeNeedsSweepScenario registers the steps of features/needs_sweep.feature.
func InitializeNeedsSweepScenario(ctx *godog.ScenarioContext) {
	c := &needsSweepContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = needsSweepContext{
			beads:   &apptest.FakeNeedsSweep{Comments: map[string][]application.Comment{}, Now: needsSweepNow},
			mail:    apptest.NewFakeMailbox(),
			reverts: map[string]string{},
		}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(filepath.Dir(c.dir))
		}
		return ctx, nil
	})

	ctx.Given(`^a sweep rig with a git history, a tracker and a mailbox$`, c.rig)
	ctx.Given(`^the sweep story "([^"]*)" landed (\d+) (days|hours) ago with an internal closing step and was merged$`, func(id string, n int, unit string) error {
		return c.story(id, n, unit, true, false)
	})
	ctx.Given(`^the sweep story "([^"]*)" landed (\d+) (days|hours) ago with steps for the Governor and was merged$`, func(id string, n int, unit string) error {
		return c.story(id, n, unit, false, false)
	})
	ctx.Given(`^the sweep story "([^"]*)" landed (\d+) (days|hours) ago with an internal closing step and was merged and then reverted$`, func(id string, n int, unit string) error {
		return c.story(id, n, unit, true, true)
	})
	ctx.Given(`^the tracker refuses to comment on "([^"]*)"$`, func(id string) error { c.beads.FailOn = id; return nil })
	ctx.When(`^mw needs sweep runs without --write$`, func() error { return c.sweep(false) })
	ctx.When(`^mw needs sweep runs with --write$`, func() error { return c.sweep(true) })
	ctx.Then(`^the sweep prints a line for "([^"]*)" of class "([^"]*)" that says it would write$`, func(id, class string) error {
		line, err := c.line(id, class)
		if err == nil && !strings.Contains(line, "would") {
			err = fmt.Errorf("the line %q does not say what it would write", line)
		}
		return err
	})
	ctx.Then(`^the sweep prints a line for "([^"]*)" of class "([^"]*)"$`, func(id, class string) error {
		_, err := c.line(id, class)
		return err
	})
	ctx.Then(`^the sweep wrote no comment and sent no mail$`, func() error {
		if len(c.beads.Written) != 0 || len(c.sent()) != 0 {
			return fmt.Errorf("wrote %+v and sent %d mails", c.beads.Written, len(c.sent()))
		}
		return nil
	})
	ctx.Then(`^the sweep commented only "([^"]*)", and its comment opens VERIFIED and says it is not his use$`, c.commentedOnly)
	ctx.Then(`^the Mayor's mail names "([^"]*)" and the commit that reverted it$`, c.mailNames)
	ctx.Then(`^the sweep wrote (\d+) comment in all and sent (\d+) mail in all$`, func(comments, mails int) error {
		if len(c.beads.Written) != comments || len(c.sent()) != mails {
			return fmt.Errorf("wrote %d comments and sent %d mails, want %d and %d", len(c.beads.Written), len(c.sent()), comments, mails)
		}
		return nil
	})
	ctx.Then(`^the sweep stopped with an error naming "([^"]*)"$`, func(id string) error {
		if c.err == nil || !strings.Contains(c.err.Error(), id) || !strings.Contains(c.err.Error(), "stopped") {
			return fmt.Errorf("expected a stop naming %s, got %v", id, c.err)
		}
		return nil
	})
}

func (c *needsSweepContext) rig() error {
	root, err := os.MkdirTemp("", "mw-sweep-")
	if err != nil {
		return err
	}
	c.dir = filepath.Join(root, "rig")
	c.runDir = filepath.Join(root, "needs")
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}} {
		if err := gitRun(c.dir, "git", args...); err != nil {
			return err
		}
	}
	if err := gitIdentify(c.dir); err != nil {
		return err
	}
	return c.commit("README.md", "the rig opens", "The rig opens")
}

func (c *needsSweepContext) commit(file, body, message string) error {
	if err := os.WriteFile(filepath.Join(c.dir, file), []byte(body), 0o644); err != nil {
		return err
	}
	if err := gitRun(c.dir, "git", "add", "-A"); err != nil {
		return err
	}
	return gitRun(c.dir, "git", "commit", "-qm", message)
}

// story merges a branch for the story into main, as a landing does, and files the
// closed bead whose close reason names the branch's tip.
func (c *needsSweepContext) story(id string, n int, unit string, internal, reverted bool) error {
	if err := gitRun(c.dir, "git", "checkout", "-qb", "mw/"+id); err != nil {
		return err
	}
	if err := c.commit(id+".txt", id, "work for "+id); err != nil {
		return err
	}
	tip, err := gitSay(c.dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if err := gitRun(c.dir, "git", "checkout", "-q", "main"); err != nil {
		return err
	}
	if err := gitRun(c.dir, "git", "merge", "-q", "--no-ff", "-m", "Merge branch 'mw/"+id+"' into HEAD", "mw/"+id); err != nil {
		return err
	}
	if reverted {
		merge, err := gitSay(c.dir, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		if err := gitRun(c.dir, "git", "revert", "-m", "1", "--no-edit", merge); err != nil {
			return err
		}
		if c.reverts[id], err = gitSay(c.dir, "rev-parse", "HEAD"); err != nil {
			return err
		}
	}
	age := time.Duration(n) * time.Hour
	if unit == "days" {
		age *= 24
	}
	closed := needsSweepNow.Add(-age)
	closing := "Built it.\n\nHOW TO CHECK IT, for the Governor:\n1. Open the app.\n\nFor the rig memory: nothing"
	if internal {
		closing = "Built it.\n\nInternal: nothing for the Governor to look at\n\nFor the rig memory: nothing"
	}
	c.beads.Beads = append(c.beads.Beads, application.TriageBead{
		ID: id, Title: "Story " + id, Type: "task", Status: "closed", Labels: []string{"run:landed"}, Rig: "sw-rig",
		ClosedAt: closed, CloseReason: "landed on main at " + tip + " (tests: pass)",
	})
	c.beads.Comments[id] = []application.Comment{{Author: "builder", Created: closed.Add(-time.Minute), Text: closing}}
	return nil
}

func (c *needsSweepContext) sweep(write bool) error {
	c.out.Reset()
	_, c.err = application.NeedsSweep{
		Beads: c.beads, Git: rig.New(), Mailbox: c.mail,
		Dirs: map[string]string{"sw-rig": c.dir}, RunDir: c.runDir, Host: "laptop",
		Now: func() time.Time { return needsSweepNow }, Out: &c.out,
	}.Run(context.Background(), application.NeedsSweepOptions{Write: write})
	if c.err != nil && c.beads.FailOn == "" {
		return c.err
	}
	return nil
}

func (c *needsSweepContext) sent() []application.Message {
	msgs, _ := c.mail.Inbox(context.Background(), application.MayorMailbox)
	return msgs
}

func (c *needsSweepContext) line(id, class string) (string, error) {
	for _, line := range strings.Split(c.out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == id {
			if fields[1] != class {
				return line, fmt.Errorf("%s is class %q, want %q: %s", id, fields[1], class, line)
			}
			return line, nil
		}
	}
	return "", fmt.Errorf("no line for %s in:\n%s", id, c.out.String())
}

func (c *needsSweepContext) commentedOnly(id string) error {
	if len(c.beads.Written) != 1 || c.beads.Written[0].ID != id {
		return fmt.Errorf("expected one comment, on %s; got %+v", id, c.beads.Written)
	}
	text := c.beads.Written[0].Text
	if !strings.HasPrefix(text, "VERIFIED") || !strings.Contains(text, "not by his use") {
		return fmt.Errorf("the comment is %q", text)
	}
	return nil
}

func (c *needsSweepContext) mailNames(id string) error {
	sent := c.sent()
	if len(sent) != 1 {
		return fmt.Errorf("expected one mail to the Mayor, got %d", len(sent))
	}
	revert := c.reverts[id]
	if revert == "" {
		return fmt.Errorf("%s was not reverted in this scenario", id)
	}
	body := sent[0].Body
	if !strings.Contains(body, id) || !strings.Contains(body, revert[:12]) {
		return fmt.Errorf("the mail does not name %s and %s:\n%s", id, revert[:12], body)
	}
	return nil
}

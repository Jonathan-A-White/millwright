package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// gristKeyContext is a throwaway mill key file and a fake home file: nothing
// here reaches a real ~/.config/mw.
type gristKeyContext struct {
	dir  string
	path string
	keys *postern.KeyFile
	home *apptest.FakeHomeFile
	host string
	out  bytes.Buffer
	err  error
	// before is the key file's text before the run, when there was a key.
	before string
}

// InitializeGristKeyScenario registers the steps of features/grist_key.feature.
func InitializeGristKeyScenario(ctx *godog.ScenarioContext) {
	c := &gristKeyContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = gristKeyContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway mill key file$`, c.aThrowawayMillKeyFile)
	ctx.Given(`^this host is "([^"]*)" and the home is "([^"]*)"$`, c.thisHostIsAndTheHomeIs)
	ctx.Given(`^this host is "([^"]*)" and the vault has no home file$`, c.thisHostHasNoHomeFile)
	ctx.Given(`^the mill key was made$`, c.theMillKeyWasMade)
	ctx.When(`^mw grist key is run$`, func() error { return c.run(false, false) })
	ctx.When(`^mw grist key is run with --create$`, func() error { return c.run(true, false) })
	ctx.When(`^mw grist key is run with --create and --force-host$`, func() error { return c.run(true, true) })
	ctx.Then(`^mw grist key was refused saying "([^"]*)" and "([^"]*)"$`, c.refusedSaying)
	ctx.Then(`^mw grist key printed the public key and fingerprint, and never the private key$`, c.printedThePublicHalf)
	ctx.Then(`^mw grist key made no new key$`, c.madeNoNewKey)
	ctx.Then(`^the mill key file does not exist$`, c.theKeyFileDoesNotExist)
	ctx.Then(`^the mill key file exists, mode 0600$`, c.theKeyFileExists)
}

func (c *gristKeyContext) aThrowawayMillKeyFile() error {
	dir, err := os.MkdirTemp("", "mw-grist-key-")
	if err != nil {
		return err
	}
	c.dir = dir
	c.path = filepath.Join(dir, "mill.key")
	c.keys = postern.New(c.path)
	return nil
}

func (c *gristKeyContext) thisHostIsAndTheHomeIs(host, home string) error {
	c.host = host
	c.home = &apptest.FakeHomeFile{Text: home + " 2026-09-29T00:10:00Z mw@" + home + "\n"}
	return nil
}

func (c *gristKeyContext) thisHostHasNoHomeFile(host string) error {
	c.host = host
	c.home = &apptest.FakeHomeFile{Missing: true}
	return nil
}

func (c *gristKeyContext) theMillKeyWasMade() error {
	if err := c.keys.Generate(); err != nil {
		return err
	}
	raw, err := os.ReadFile(c.path)
	c.before = string(raw)
	return err
}

func (c *gristKeyContext) run(create, force bool) error {
	_, c.err = application.GristKey{
		Keys: c.keys, Out: &c.out, Create: create, ForceHost: force, Home: c.home, Host: c.host,
	}.Run(context.Background())
	return nil
}

func (c *gristKeyContext) refusedSaying(first, second string) error {
	if c.err == nil {
		return fmt.Errorf("expected a refusal, got none; it printed:\n%s", c.out.String())
	}
	for _, want := range []string{first, second} {
		if !strings.Contains(c.err.Error(), want) {
			return fmt.Errorf("expected the refusal to say %q, got: %v", want, c.err)
		}
	}
	return nil
}

func (c *gristKeyContext) printedThePublicHalf() error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	said := c.out.String()
	public, _, err := c.keys.PublicKey()
	if err != nil {
		return err
	}
	for _, want := range []string{"public key: " + public, "fingerprint: " + application.KeyFingerprint(public)} {
		if !strings.Contains(said, want) {
			return fmt.Errorf("expected %q in:\n%s", want, said)
		}
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	if strings.Contains(said, strings.TrimSpace(string(raw))) {
		return fmt.Errorf("the private key was printed:\n%s", said)
	}
	return nil
}

func (c *gristKeyContext) madeNoNewKey() error {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	if string(raw) != c.before || strings.Contains(c.out.String(), "Wrote a new") {
		return fmt.Errorf("expected the key left as it was, it printed:\n%s", c.out.String())
	}
	return nil
}

func (c *gristKeyContext) theKeyFileDoesNotExist() error {
	if _, err := os.Lstat(c.path); err == nil {
		return fmt.Errorf("%s exists", c.path)
	}
	return nil
}

func (c *gristKeyContext) theKeyFileExists() error {
	info, err := os.Stat(c.path)
	if err != nil {
		return fmt.Errorf("the mill key file is not there: %w", err)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("the mill key file's mode is %o, not 0600", info.Mode().Perm())
	}
	return nil
}

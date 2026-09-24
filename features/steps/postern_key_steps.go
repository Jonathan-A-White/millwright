package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// posternKeyContext holds a throwaway home in which the postern key file
// lives: nothing here reaches a real ~/.config/mw.
type posternKeyContext struct {
	home string
	path string
	keys *postern.KeyFile
	out  *bytes.Buffer

	initReport application.PosternKeyInitReport
	showReport application.PosternKeyShowReport
	err        error

	before string
}

// InitializePosternKeyScenario registers the steps of features/postern_key.feature.
func InitializePosternKeyScenario(ctx *godog.ScenarioContext) {
	c := &posternKeyContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = posternKeyContext{out: &bytes.Buffer{}}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway postern key file$`, c.aThrowawayPosternKeyFile)
	ctx.Given(`^a postern key already exists$`, c.aPosternKeyAlreadyExists)

	ctx.When(`^mw postern key init is run$`, c.mwPosternKeyInitIsRun)
	ctx.When(`^mw postern key show is run$`, c.mwPosternKeyShowIsRun)

	ctx.Then(`^it succeeds$`, c.itSucceeds)
	ctx.Then(`^the postern key file holds a testnet WIF key, mode 0600$`, c.thePosternKeyFileHoldsATestnetWIFKey)
	ctx.Then(`^it is refused, saying the key already exists$`, c.refusedKeyAlreadyExists)
	ctx.Then(`^the postern key file still holds the key it had before$`, c.stillHoldsTheKeyItHadBefore)
	ctx.Then(`^it prints the key's compressed public key$`, c.itPrintsTheCompressedPublicKey)
	ctx.Then(`^it prints the key's testnet address$`, c.itPrintsTheTestnetAddress)
	ctx.Then(`^it never prints the private key$`, c.itNeverPrintsThePrivateKey)
	ctx.Then(`^it is refused, saying there is no postern key$`, c.refusedNoPosternKey)
}

func (c *posternKeyContext) aThrowawayPosternKeyFile() error {
	home, err := os.MkdirTemp("", "mw-postern-key-")
	if err != nil {
		return err
	}
	c.home = home
	c.path = filepath.Join(home, "postern.key")
	c.keys = postern.New(c.path)
	return nil
}

func (c *posternKeyContext) aPosternKeyAlreadyExists() error {
	if _, err := (application.PosternKeyInit{Keys: c.keys, Out: c.out}).Run(context.Background()); err != nil {
		return err
	}
	c.out.Reset()
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	c.before = string(raw)
	return nil
}

func (c *posternKeyContext) mwPosternKeyInitIsRun() error {
	c.initReport, c.err = application.PosternKeyInit{Keys: c.keys, Out: c.out}.Run(context.Background())
	return nil
}

func (c *posternKeyContext) mwPosternKeyShowIsRun() error {
	c.showReport, c.err = application.PosternKeyShow{Keys: c.keys, Out: c.out}.Run(context.Background())
	return nil
}

func (c *posternKeyContext) itSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("expected it to succeed, got: %w", c.err)
	}
	return nil
}

func (c *posternKeyContext) thePosternKeyFileHoldsATestnetWIFKey() error {
	info, err := os.Stat(c.path)
	if err != nil {
		return fmt.Errorf("the postern key file is not there: %w", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		return fmt.Errorf("the postern key file's mode is %o, not 0600", mode)
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	wif := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(wif, "c") && !strings.HasPrefix(wif, "9") {
		return fmt.Errorf("the postern key %q does not look like a testnet WIF key", wif)
	}
	return nil
}

func (c *posternKeyContext) refusedKeyAlreadyExists() error {
	if c.err == nil {
		return fmt.Errorf("expected init to refuse overwriting an existing key")
	}
	if said := c.err.Error(); !strings.Contains(said, c.path) || !strings.Contains(said, "already exists") {
		return fmt.Errorf("expected the refusal to name %s and say it already exists, got: %q", c.path, said)
	}
	return nil
}

func (c *posternKeyContext) stillHoldsTheKeyItHadBefore() error {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	if string(raw) != c.before {
		return fmt.Errorf("the postern key file changed: was %q, now %q", c.before, string(raw))
	}
	return nil
}

func (c *posternKeyContext) itPrintsTheCompressedPublicKey() error {
	if c.showReport.PublicKey == "" {
		return fmt.Errorf("the report holds no public key")
	}
	if !strings.Contains(c.out.String(), c.showReport.PublicKey) {
		return fmt.Errorf("expected the output to hold the public key %q, got: %q", c.showReport.PublicKey, c.out.String())
	}
	if len(c.showReport.PublicKey) != 66 {
		return fmt.Errorf("expected a 33-byte compressed public key (66 hex characters), got %d: %q", len(c.showReport.PublicKey), c.showReport.PublicKey)
	}
	return nil
}

func (c *posternKeyContext) itPrintsTheTestnetAddress() error {
	if c.showReport.Address == "" {
		return fmt.Errorf("the report holds no address")
	}
	if !strings.Contains(c.out.String(), c.showReport.Address) {
		return fmt.Errorf("expected the output to hold the address %q, got: %q", c.showReport.Address, c.out.String())
	}
	if !strings.HasPrefix(c.showReport.Address, "m") && !strings.HasPrefix(c.showReport.Address, "n") {
		return fmt.Errorf("expected a testnet address (starting m or n), got %q", c.showReport.Address)
	}
	return nil
}

func (c *posternKeyContext) itNeverPrintsThePrivateKey() error {
	wif := strings.TrimSpace(c.before)
	if wif == "" {
		return fmt.Errorf("no private key was recorded to check against")
	}
	if strings.Contains(c.out.String(), wif) {
		return fmt.Errorf("the output held the private key: %q", c.out.String())
	}
	return nil
}

func (c *posternKeyContext) refusedNoPosternKey() error {
	if c.err == nil {
		return fmt.Errorf("expected show to refuse when there is no key")
	}
	if said := c.err.Error(); !strings.Contains(said, c.path) || !strings.Contains(said, "no postern key") {
		return fmt.Errorf("expected the refusal to name %s and say there is no postern key, got: %q", c.path, said)
	}
	return nil
}

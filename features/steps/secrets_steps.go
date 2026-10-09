package steps

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/sops"

	"github.com/cucumber/godog"
)

// secretsContext holds a temp vault and a throwaway age key made for one
// scenario, and the real sops store over them. Every value put is made here
// at random, so no fixture holds one.
type secretsContext struct {
	dir       string
	vault     string
	recipient string
	store     *sops.Store

	// values is the last value put under each name; every is each value ever
	// put, for the checks that none is printed or in the clear.
	values map[string]string
	every  []string

	out    bytes.Buffer // what mw secrets put and list printed
	got    bytes.Buffer // what mw secrets get wrote
	putErr error
	getErr error
}

// InitializeSecretsScenario registers the steps of features/secrets.feature.
func InitializeSecretsScenario(ctx *godog.ScenarioContext) {
	c := &secretsContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = secretsContext{values: map[string]string{}}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a temp vault whose \.sops\.yaml names a throwaway age key$`, c.aTempVaultWithAThrowawayAgeKey)
	ctx.Given(`^a fresh value is put as the secret "([^"]*)"$`, func(name string) error {
		if err := c.putFresh(name); err != nil {
			return err
		}
		return c.putErr
	})

	ctx.When(`^a fresh value is put as the secret "([^"]*)"$`, c.putFresh)
	ctx.When(`^the value "([^"]*)" and a newline is put as the secret "([^"]*)"$`, func(value, name string) error {
		return c.put(name, value, value+"\n")
	})
	ctx.When(`^an empty value is put as the secret "([^"]*)"$`, func(name string) error {
		return c.put(name, "", "\n")
	})
	ctx.When(`^the secret "([^"]*)" is got into a pipe$`, func(name string) error { return c.get(name, false) })
	ctx.When(`^the secret "([^"]*)" is got onto a terminal$`, func(name string) error { return c.get(name, true) })
	ctx.When(`^the secrets are listed$`, c.theSecretsAreListed)

	ctx.Then(`^what was got is the value put as "([^"]*)", and nothing more$`, c.whatWasGotIsTheValuePutAs)
	ctx.Then(`^what was got is "([^"]*)", and nothing more$`, c.whatWasGotIs)
	ctx.Then(`^mw secrets printed no value$`, c.mwSecretsPrintedNoValue)
	ctx.Then(`^the list is exactly:$`, c.theListIsExactly)
	ctx.Then(`^the get is refused, saying "([^"]*)"$`, func(want string) error { return refusedSaying("get", c.getErr, want) })
	ctx.Then(`^the put is refused, saying "([^"]*)"$`, func(want string) error { return refusedSaying("put", c.putErr, want) })
	ctx.Then(`^nothing was written to the (?:terminal|pipe)$`, c.nothingWasWritten)
	ctx.Then(`^grep finds no value put in secrets\.enc\.yaml$`, c.grepFindsNoValue)
	ctx.Then(`^secrets\.enc\.yaml names the throwaway age recipient$`, c.theFileNamesTheRecipient)
	ctx.Then(`^the vault holds no secrets\.enc\.yaml$`, c.theVaultHoldsNoSecretsFile)
}

// aTempVaultWithAThrowawayAgeKey makes a temp vault, an age key made now by
// the real age-keygen, and a .sops.yaml naming its recipient. It skips the
// scenario where sops or age-keygen is not installed.
func (c *secretsContext) aTempVaultWithAThrowawayAgeKey() error {
	for _, program := range []string{sops.Program, "age-keygen"} {
		if _, err := exec.LookPath(program); err != nil {
			return godog.ErrSkip
		}
	}
	dir, err := os.MkdirTemp("", "mw-secrets-")
	if err != nil {
		return err
	}
	c.dir = dir
	c.vault = filepath.Join(dir, "vault")
	if err := os.MkdirAll(c.vault, 0o755); err != nil {
		return err
	}
	key := filepath.Join(dir, "age.key")
	if out, err := exec.Command("age-keygen", "-o", key).CombinedOutput(); err != nil {
		return fmt.Errorf("age-keygen: %v: %s", err, out)
	}
	recipient, err := exec.Command("age-keygen", "-y", key).Output()
	if err != nil {
		return fmt.Errorf("age-keygen -y: %v", err)
	}
	c.recipient = strings.TrimSpace(string(recipient))
	rules := "creation_rules:\n  - path_regex: secrets\\.enc\\.yaml$\n    age: " + c.recipient + "\n"
	if err := os.WriteFile(filepath.Join(c.vault, sops.ConfigFile), []byte(rules), 0o644); err != nil {
		return err
	}
	c.store = sops.New(c.vault, key)
	return nil
}

// putFresh puts a value made now at random under name.
func (c *secretsContext) putFresh(name string) error {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	value := "fresh-" + hex.EncodeToString(raw)
	return c.put(name, value, value+"\n")
}

// put runs mw secrets put with stdin as its input, remembering value as what
// name should now hold when the put is not refused.
func (c *secretsContext) put(name, value, stdin string) error {
	if value != "" {
		c.every = append(c.every, value)
	}
	c.putErr = application.SecretsPut{
		Store: c.store,
		In:    strings.NewReader(stdin),
		Out:   &c.out,
	}.Run(context.Background(), name)
	if c.putErr == nil {
		c.values[name] = value
	}
	return nil
}

func (c *secretsContext) get(name string, terminal bool) error {
	c.got.Reset()
	c.getErr = application.SecretsGet{
		Store:         c.store,
		Out:           &c.got,
		OutIsTerminal: terminal,
	}.Run(context.Background(), name)
	return nil
}

func (c *secretsContext) theSecretsAreListed() error {
	c.out.Reset()
	return application.SecretsList{Store: c.store, Out: &c.out}.Run(context.Background())
}

func (c *secretsContext) whatWasGotIsTheValuePutAs(name string) error {
	want, ok := c.values[name]
	if !ok {
		return fmt.Errorf("no value was put as %q in this scenario", name)
	}
	return c.whatWasGotIs(want)
}

func (c *secretsContext) whatWasGotIs(want string) error {
	if c.getErr != nil {
		return fmt.Errorf("expected the get to succeed, it failed: %v", c.getErr)
	}
	if got := c.got.String(); got != want {
		return fmt.Errorf("expected exactly the value put (%d bytes), got %d bytes", len(want), len(got))
	}
	return nil
}

func (c *secretsContext) mwSecretsPrintedNoValue() error {
	said := c.out.String()
	for _, err := range []error{c.putErr, c.getErr} {
		if err != nil {
			said += err.Error()
		}
	}
	for _, value := range c.every {
		if strings.Contains(said, value) {
			return errors.New("mw secrets printed a value it was given")
		}
	}
	return nil
}

func (c *secretsContext) theListIsExactly(doc *godog.DocString) error {
	if got, want := strings.TrimSpace(c.out.String()), strings.TrimSpace(doc.Content); got != want {
		return fmt.Errorf("expected the list:\n%s\ngot:\n%s", want, got)
	}
	return nil
}

func refusedSaying(what string, err error, want string) error {
	if err == nil {
		return fmt.Errorf("expected the %s to be refused, it was not", what)
	}
	if !strings.Contains(err.Error(), want) {
		return fmt.Errorf("expected the %s's refusal to say %q, it says %q", what, want, err)
	}
	return nil
}

func (c *secretsContext) nothingWasWritten() error {
	if n := c.got.Len(); n != 0 {
		return fmt.Errorf("expected nothing written, got %d bytes", n)
	}
	return nil
}

// grepFindsNoValue runs the real grep over the file on disk for each value put:
// it must find none of them.
func (c *secretsContext) grepFindsNoValue() error {
	path := filepath.Join(c.vault, sops.SecretsFile)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("expected %s to have been written: %v", path, err)
	}
	if len(c.every) == 0 {
		return errors.New("no value was put in this scenario")
	}
	for _, value := range c.every {
		err := exec.Command("grep", "-q", "-F", "--", value, path).Run()
		var exit *exec.ExitError
		if err == nil {
			return fmt.Errorf("grep found a value put in the clear in %s", path)
		}
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return fmt.Errorf("grep could not read %s: %v", path, err)
		}
	}
	return nil
}

func (c *secretsContext) theFileNamesTheRecipient() error {
	data, err := os.ReadFile(filepath.Join(c.vault, sops.SecretsFile))
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), c.recipient) {
		return fmt.Errorf("expected %s to name the recipient %s:\n%s", sops.SecretsFile, c.recipient, data)
	}
	return nil
}

func (c *secretsContext) theVaultHoldsNoSecretsFile() error {
	path := filepath.Join(c.vault, sops.SecretsFile)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("expected no %s, stat says %v", path, err)
	}
	return nil
}

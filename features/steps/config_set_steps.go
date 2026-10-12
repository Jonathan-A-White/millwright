package steps

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"

	"github.com/cucumber/godog"
)

// configSetContext holds a config file in a temp directory of its own and what
// came of changing or reading it.
type configSetContext struct {
	dir string
	now time.Time

	// before is the file as it was when the command ran, and listing the
	// directory then, name to bytes.
	before  string
	listing map[string]string

	printed bytes.Buffer
	err     error
}

// InitializeConfigSetScenario registers the steps of features/config_set.feature.
func InitializeConfigSetScenario(ctx *godog.ScenarioContext) {
	c := &configSetContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = configSetContext{}
		return ctx, nil
	})

	ctx.Given(`^a config file holding:$`, c.aConfigFileHolding)
	ctx.Given(`^the config clock reads (\S+)$`, c.theClockReads)
	ctx.When(`^mw config set is run for "([^"]*)" with "(.*)"$`, c.setWithQuoted)
	ctx.When(`^mw config set is run for "([^"]*)" with '(.*)'$`, c.setWithSingleQuoted)
	ctx.When(`^mw config set is dry-run for "([^"]*)" with "(.*)"$`, c.dryRun)
	ctx.When(`^the printed way back is run$`, c.theWayBackIsRun)
	ctx.When(`^mw config get is run for "([^"]*)"$`, c.get)
	ctx.Then(`^mw config set succeeded$`, c.succeeded)
	ctx.Then(`^mw config set was refused, saying: (.+)$`, c.refusedSaying)
	ctx.Then(`^mw config get failed, saying: (.+)$`, c.refusedSaying)
	ctx.Then(`^the config set printed: (.+)$`, c.printedLine)
	ctx.Then(`^mw config get printed: (.+)$`, c.printedLine)
	ctx.Then(`^the config file differs from before in exactly one line$`, c.differsInOneLine)
	ctx.Then(`^the config line "(.*)" is in the table "([^"]*)"$`, c.lineIsInTable)
	ctx.Then(`^the config file reads:$`, c.fileReads)
	ctx.Then(`^the config file is the old file followed by a blank line and:$`, c.fileIsOldFollowedBy)
	ctx.Then(`^the config file equals the old file$`, c.fileEqualsOld)
	ctx.Then(`^the backup (\S+) exists and equals the old file$`, c.backupEqualsOld)
	ctx.Then(`^the config directory is as it was$`, c.directoryAsItWas)
}

func (c *configSetContext) path() string { return filepath.Join(c.dir, "config.toml") }

func (c *configSetContext) aConfigFileHolding(doc *godog.DocString) error {
	if c.dir == "" {
		dir, err := os.MkdirTemp("", "mw-config-set-")
		if err != nil {
			return err
		}
		c.dir = dir
	}
	return os.WriteFile(c.path(), []byte(doc.Content+"\n"), 0o600)
}

func (c *configSetContext) theClockReads(stamp string) error {
	now, err := time.Parse(time.RFC3339, stamp)
	c.now = now
	return err
}

func (c *configSetContext) snapshot() error {
	data, err := os.ReadFile(c.path())
	if err != nil {
		return err
	}
	c.before = string(data)
	c.listing = map[string]string{}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(c.dir, entry.Name()))
		if err != nil {
			return err
		}
		c.listing[entry.Name()] = string(body)
	}
	return nil
}

func (c *configSetContext) setWithQuoted(key, value string) error {
	return c.set(key, value, false)
}

func (c *configSetContext) setWithSingleQuoted(key, value string) error {
	return c.set(key, value, false)
}

func (c *configSetContext) dryRun(key, value string) error {
	return c.set(key, value, true)
}

func (c *configSetContext) set(key, value string, dryRun bool) error {
	if err := c.snapshot(); err != nil {
		return err
	}
	c.printed.Reset()
	c.err = application.ConfigSet{
		File: configfile.New(c.path()),
		Now:  func() time.Time { return c.now },
		Out:  &c.printed,
	}.Run(context.Background(), key, value, dryRun)
	return nil
}

func (c *configSetContext) get(key string) error {
	c.printed.Reset()
	c.err = application.ConfigGet{File: configfile.New(c.path()), Out: &c.printed}.Run(context.Background(), key)
	return nil
}

func (c *configSetContext) succeeded() error {
	if c.err != nil {
		return fmt.Errorf("mw config set failed: %v", c.err)
	}
	return nil
}

func (c *configSetContext) refusedSaying(said string) error {
	if c.err == nil {
		return fmt.Errorf("the command succeeded, printing %q", c.printed.String())
	}
	if !strings.Contains(c.err.Error(), said) {
		return fmt.Errorf("the command failed with %q, which does not say %q", c.err, said)
	}
	return nil
}

func (c *configSetContext) printedLine(want string) error {
	if c.err != nil {
		return fmt.Errorf("the command failed: %v", c.err)
	}
	want = strings.ReplaceAll(want, "<the config>", c.path())
	for _, line := range strings.Split(c.printed.String(), "\n") {
		if line == want {
			return nil
		}
	}
	return fmt.Errorf("no printed line %q in:\n%s", want, c.printed.String())
}

func (c *configSetContext) read() (string, error) {
	data, err := os.ReadFile(c.path())
	return string(data), err
}

func (c *configSetContext) differsInOneLine() error {
	now, err := c.read()
	if err != nil {
		return err
	}
	count := map[string]int{}
	for _, line := range strings.Split(c.before, "\n") {
		count[line]--
	}
	for _, line := range strings.Split(now, "\n") {
		count[line]++
	}
	added, removed := 0, 0
	for _, n := range count {
		if n > 0 {
			added += n
		} else {
			removed -= n
		}
	}
	if added > 1 || removed > 1 || added+removed == 0 {
		return fmt.Errorf("%d lines added and %d removed; the file is now:\n%s", added, removed, now)
	}
	return nil
}

func (c *configSetContext) lineIsInTable(line, table string) error {
	// Gherkin does not unescape \" in a step.
	line = strings.ReplaceAll(line, `\"`, `"`)
	key, literal, found := strings.Cut(line, " = ")
	if !found {
		return fmt.Errorf("%q is not a key = value line", line)
	}
	now, err := c.read()
	if err != nil {
		return err
	}
	file, err := domain.ParseConfigTOML(now)
	if err != nil {
		return err
	}
	got, ok := file.Lookup(table, key)
	if !ok || got != literal {
		return fmt.Errorf("[%s] %s is %q (found %v), want %q; the file is:\n%s", table, key, got, ok, literal, now)
	}
	return nil
}

func (c *configSetContext) fileReads(doc *godog.DocString) error {
	now, err := c.read()
	if err != nil {
		return err
	}
	if want := doc.Content + "\n"; now != want {
		return fmt.Errorf("the config file is:\n%s\nwant:\n%s", now, want)
	}
	return nil
}

func (c *configSetContext) fileIsOldFollowedBy(doc *godog.DocString) error {
	now, err := c.read()
	if err != nil {
		return err
	}
	if want := c.before + "\n" + doc.Content + "\n"; now != want {
		return fmt.Errorf("the config file is:\n%s\nwant:\n%s", now, want)
	}
	return nil
}

func (c *configSetContext) fileEqualsOld() error {
	now, err := c.read()
	if err != nil {
		return err
	}
	if now != c.before {
		return fmt.Errorf("the config file is:\n%s\nwant the old file:\n%s", now, c.before)
	}
	return nil
}

func (c *configSetContext) backupEqualsOld(name string) error {
	data, err := os.ReadFile(filepath.Join(c.dir, name))
	if err != nil {
		return err
	}
	if string(data) != c.before {
		return fmt.Errorf("the backup holds:\n%s\nwant the old file:\n%s", data, c.before)
	}
	return nil
}

func (c *configSetContext) theWayBackIsRun() error {
	if c.err != nil {
		return fmt.Errorf("the command failed: %v", c.err)
	}
	for _, line := range strings.Split(c.printed.String(), "\n") {
		if way, found := strings.CutPrefix(line, "way back: "); found {
			if out, err := exec.Command("sh", "-c", way).CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %v: %s", way, err, out)
			}
			return nil
		}
	}
	return fmt.Errorf("no way back was printed:\n%s", c.printed.String())
}

func (c *configSetContext) directoryAsItWas() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
		body, err := os.ReadFile(filepath.Join(c.dir, entry.Name()))
		if err != nil {
			return err
		}
		if was, ok := c.listing[entry.Name()]; !ok || was != string(body) {
			return fmt.Errorf("%s is new or changed", entry.Name())
		}
	}
	if len(names) != len(c.listing) {
		sort.Strings(names)
		return fmt.Errorf("the directory holds %v, not as it was", names)
	}
	return nil
}

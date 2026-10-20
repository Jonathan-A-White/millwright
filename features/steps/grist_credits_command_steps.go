package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"
	"github.com/Jonathan-A-White/millwright/infrastructure/grist"
)

// creditsCommandContext is a throwaway config file, key file and credits state,
// a fake record and a fake home file: nothing here reaches a real
// ~/.config/mw.
type creditsCommandContext struct {
	dir     string
	host    string
	now     time.Time
	config  string
	keyFile string
	state   string
	record  *apptest.FakeGristState
	home    *apptest.FakeHomeFile

	out         bytes.Buffer
	err         error
	read        int
	configAtRun string
}

// InitializeGristCreditsCommandScenario registers the steps of
// features/grist_credits_command.feature.
func InitializeGristCreditsCommandScenario(ctx *godog.ScenarioContext) {
	c := &creditsCommandContext{}

	ctx.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*c = creditsCommandContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if c.dir != "" {
			os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a credits workbench on the host "([^"]*)", the home being "([^"]*)"$`, c.aWorkbench)
	ctx.Given(`^the credits workbench moves to the host "([^"]*)"$`, c.movesTo)
	ctx.Given(`^the credits clock reads (\S+)$`, c.clockReads)
	ctx.Given(`^\[grist\] api_credits is (true|false) in the config$`, c.configSwitch)
	ctx.Given(`^the config holds a \[grist\] table with a daily_limit of (\d+)$`, c.configDailyLimit)
	ctx.Given(`^the key file holds "([^"]*)" at mode (\d+)$`, c.keyFileHolds)
	ctx.Given(`^the key was found spent at (\S+)$`, c.foundSpent)
	ctx.Given(`^the mill's record holds a grist billed to "([^"]*)" at (\S+) costing ([\d.]+) dollars$`, c.recordHolds)

	ctx.When(`^mw grist credits(?: status)? is run$`, func() error { return c.run("status", false) })
	ctx.When(`^mw grist credits is run with --json$`, func() error { return c.run("status", true) })
	ctx.When(`^mw grist credits (on|off|clear) is run$`, func(verb string) error { return c.run(verb, false) })
	ctx.When(`^mw grist credits key is run with the line "([^"]*)"$`, c.runKey)

	ctx.Then(`^mw grist credits printed the line "([^"]*)"$`, c.printedLine)
	ctx.Then(`^mw grist credits printed a line starting "([^"]*)"$`, c.printedLineStarting)
	ctx.Then(`^"([^"]*)" is in nothing mw grist credits printed$`, c.inNothingPrinted)
	ctx.Then(`^"([^"]*)" is in no file of the config or the credits state$`, c.inNoFile)
	ctx.Then(`^the JSON printed says api_credits is (true|false), key_present is (true|false), grists is (\d+), cost_usd is ([\d.]+), budget_usd is (\d+)$`, c.jsonSaysCounts)
	ctx.Then(`^the JSON printed says spent_at is "([^"]*)" and last_api is "([^"]*)"$`, c.jsonSaysTimes)
	ctx.Then(`^the config has api_credits = (true|false) in its \[grist\] table(, and daily_limit = 5 still)?$`, c.configHas)
	ctx.Then(`^the config was not changed$`, c.configUnchanged)
	ctx.Then(`^the key file holds "([^"]*)" at mode (\d+)$`, c.keyFileIs)
	ctx.Then(`^there is no key file$`, c.noKeyFile)
	ctx.Then(`^mw grist credits was refused saying "([^"]*)"$`, c.refusedSaying)
	ctx.Then(`^no line was read from stdin$`, c.noLineRead)
	ctx.Then(`^there is no spent mark$`, c.noSpentMark)
}

func (c *creditsCommandContext) aWorkbench(host, home string) error {
	dir, err := os.MkdirTemp("", "mw-grist-credits-")
	if err != nil {
		return err
	}
	c.dir, c.host = dir, host
	c.config = filepath.Join(dir, "config.toml")
	c.keyFile = filepath.Join(dir, "anthropic-api.key")
	c.state = filepath.Join(dir, "state")
	if err := os.MkdirAll(c.state, 0o700); err != nil {
		return err
	}
	c.record = &apptest.FakeGristState{}
	c.home = &apptest.FakeHomeFile{Text: home + " 2026-09-29T00:10:00Z mw@" + home + "\n"}
	return nil
}

func (c *creditsCommandContext) movesTo(host string) error {
	c.host = host
	return nil
}

func (c *creditsCommandContext) clockReads(at string) error {
	t, err := time.Parse(time.RFC3339, at)
	c.now = t
	return err
}

// configText is the config file as it stands, "" when there is none.
func (c *creditsCommandContext) configText() string {
	raw, _ := os.ReadFile(c.config)
	return string(raw)
}

func (c *creditsCommandContext) configSwitch(value string) error {
	return os.WriteFile(c.config, []byte("# the home's config\n[grist]\ndaily_limit = 5\napi_credits = "+value+"\n"), 0o644)
}

func (c *creditsCommandContext) configDailyLimit(n int) error {
	return os.WriteFile(c.config, []byte(fmt.Sprintf("# the home's config\n[grist]\ndaily_limit = %d\n", n)), 0o644)
}

func (c *creditsCommandContext) keyFileHolds(key, mode string) error {
	perm, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.keyFile, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(c.keyFile, os.FileMode(perm))
}

func (c *creditsCommandContext) foundSpent(at string) error {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	return (&grist.CreditState{Dir: c.state}).Spend(context.Background(), application.GristCreditMark{SpentAt: t})
}

func (c *creditsCommandContext) recordHolds(billing, at, cost string) error {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return err
	}
	usd, err := strconv.ParseFloat(cost, 64)
	if err != nil {
		return err
	}
	return c.record.Append(context.Background(), application.GrindLine{Time: t, Billing: billing, CostUSD: usd, Status: "answered"})
}

// admin is the use case as the command wires it, on the settings the config
// file says now.
func (c *creditsCommandContext) admin() application.GristCreditsAdmin {
	on := regexp.MustCompile(`(?m)^api_credits\s*=\s*true`).MatchString(c.configText())
	now := c.now
	if now.IsZero() {
		now = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	}
	return application.GristCreditsAdmin{
		On: on, KeyPath: c.keyFile, Keys: grist.CreditKeyFile{Path: c.keyFile},
		State: &grist.CreditState{Dir: c.state}, Record: c.record,
		Config: configfile.New(c.config), Home: c.home, Host: c.host,
		Now: func() time.Time { return now }, Out: &c.out,
	}
}

func (c *creditsCommandContext) run(verb string, asJSON bool) error {
	c.configAtRun = c.configText()
	admin, ctx := c.admin(), context.Background()
	switch verb {
	case "status":
		c.err = admin.Status(ctx, asJSON)
	case "on", "off":
		c.err = admin.Switch(ctx, verb == "on")
	case "clear":
		c.err = admin.Clear(ctx)
	}
	return nil
}

func (c *creditsCommandContext) runKey(line string) error {
	c.configAtRun = c.configText()
	c.err = c.admin().SetKey(context.Background(), func() (string, error) {
		c.read++
		return line + "\n", nil
	})
	return nil
}

func (c *creditsCommandContext) said() string {
	said := c.out.String()
	if c.err != nil {
		said += c.err.Error() + "\n"
	}
	return strings.ReplaceAll(said, c.keyFile, "<key file>")
}

func (c *creditsCommandContext) lines() ([]string, error) {
	if c.err != nil {
		return nil, fmt.Errorf("mw grist credits failed: %w", c.err)
	}
	return strings.Split(strings.TrimRight(c.said(), "\n"), "\n"), nil
}

func (c *creditsCommandContext) printedLine(want string) error {
	lines, err := c.lines()
	if err != nil {
		return err
	}
	for _, line := range lines {
		if line == want {
			return nil
		}
	}
	return fmt.Errorf("expected the line %q in:\n%s", want, strings.Join(lines, "\n"))
}

func (c *creditsCommandContext) printedLineStarting(want string) error {
	lines, err := c.lines()
	if err != nil {
		return err
	}
	for _, line := range lines {
		if strings.HasPrefix(line, want) {
			return nil
		}
	}
	return fmt.Errorf("expected a line starting %q in:\n%s", want, strings.Join(lines, "\n"))
}

func (c *creditsCommandContext) inNothingPrinted(secret string) error {
	if strings.Contains(c.out.String(), secret) || (c.err != nil && strings.Contains(c.err.Error(), secret)) {
		return fmt.Errorf("%q was printed:\n%s", secret, c.said())
	}
	return nil
}

func (c *creditsCommandContext) inNoFile(secret string) error {
	return filepath.Walk(c.dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || path == c.keyFile {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), secret) {
			return fmt.Errorf("%q is in %s", secret, path)
		}
		return nil
	})
}

func (c *creditsCommandContext) printedJSON() (map[string]any, error) {
	if c.err != nil {
		return nil, c.err
	}
	var got map[string]any
	if err := json.Unmarshal(c.out.Bytes(), &got); err != nil {
		return nil, fmt.Errorf("the output is not one JSON object: %w\n%s", err, c.out.String())
	}
	return got, nil
}

func (c *creditsCommandContext) jsonSaysCounts(on, present string, grists int, cost string, budget int) error {
	got, err := c.printedJSON()
	if err != nil {
		return err
	}
	usd, _ := strconv.ParseFloat(cost, 64)
	want := map[string]any{"api_credits": on == "true", "key_present": present == "true", "grists": float64(grists), "cost_usd": usd, "budget_usd": float64(budget)}
	for key, value := range want {
		if got[key] != value {
			return fmt.Errorf("expected %s = %v, the JSON says %v:\n%s", key, value, got[key], c.out.String())
		}
	}
	return nil
}

func (c *creditsCommandContext) jsonSaysTimes(spent, last string) error {
	got, err := c.printedJSON()
	if err != nil {
		return err
	}
	if got["spent_at"] != spent || got["last_api"] != last {
		return fmt.Errorf("expected spent_at %q and last_api %q:\n%s", spent, last, c.out.String())
	}
	return nil
}

func (c *creditsCommandContext) configHas(value, dailyLimit string) error {
	if c.err != nil {
		return c.err
	}
	text := c.configText()
	table := text[strings.Index(text, "[grist]"):]
	if !regexp.MustCompile(`(?m)^api_credits\s*=\s*` + value + `\s*$`).MatchString(table) {
		return fmt.Errorf("expected api_credits = %s in [grist]:\n%s", value, text)
	}
	if dailyLimit != "" && !regexp.MustCompile(`(?m)^daily_limit\s*=\s*5\s*$`).MatchString(table) {
		return fmt.Errorf("the daily_limit was not kept:\n%s", text)
	}
	return nil
}

func (c *creditsCommandContext) configUnchanged() error {
	if c.configText() != c.configAtRun {
		return fmt.Errorf("the config changed:\n%s", c.configText())
	}
	return nil
}

func (c *creditsCommandContext) keyFileIs(key, mode string) error {
	if c.err != nil {
		return c.err
	}
	info, err := os.Stat(c.keyFile)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(c.keyFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(raw)) != key || fmt.Sprintf("%04o", info.Mode().Perm()) != mode {
		return fmt.Errorf("the key file holds %q at mode %04o", strings.TrimSpace(string(raw)), info.Mode().Perm())
	}
	return nil
}

func (c *creditsCommandContext) noKeyFile() error {
	if _, err := os.Lstat(c.keyFile); err == nil {
		return fmt.Errorf("%s exists", c.keyFile)
	}
	return nil
}

func (c *creditsCommandContext) refusedSaying(words string) error {
	if c.err == nil {
		return fmt.Errorf("expected a refusal saying %q, got none; it printed:\n%s", words, c.out.String())
	}
	if !strings.Contains(c.err.Error(), words) {
		return fmt.Errorf("expected a refusal saying %q, got: %v", words, c.err)
	}
	return nil
}

func (c *creditsCommandContext) noLineRead() error {
	if c.read != 0 {
		return fmt.Errorf("%d line(s) were read from stdin", c.read)
	}
	return nil
}

func (c *creditsCommandContext) noSpentMark() error {
	if _, found, err := (&grist.CreditState{Dir: c.state}).Mark(context.Background()); err != nil || found {
		return fmt.Errorf("a spent mark stands (found %v, err %v)", found, err)
	}
	return nil
}

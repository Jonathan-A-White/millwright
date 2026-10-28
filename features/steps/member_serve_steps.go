package steps

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"

	"github.com/cucumber/godog"
)

// registerServe registers the steps of features/member_serve.feature, which
// share the throwaway home of features/member.feature.
func (c *memberContext) registerServe(ctx *godog.ScenarioContext) {
	ctx.Given(`^the tools mw, bd, git, tmux and codex are found$`, func() error { return nil })
	ctx.Given(`^the tool (\w+) cannot be found$`, func(tool string) error {
		if c.host.noTool == nil {
			c.host.noTool = map[string]bool{}
		}
		c.host.noTool[tool] = true
		return nil
	})
	ctx.Given(`^the member already has a dispatch\.env holding "([^"]*)"$`, func(text string) error {
		return writeFileIn(c.path(".config", "mw", "dispatch.env"), text+"\n")
	})
	ctx.Given(`^the member's config file is removed$`, func() error { return os.Remove(c.path(".config", "mw", "config.toml")) })
	ctx.Given(`^mw member serve has run$`, func() error {
		c.mwMemberServe(false)
		return c.serveSucceeds()
	})

	ctx.When(`^mw member serve runs$`, func() error { c.mwMemberServe(false); return nil })
	ctx.When(`^mw member serve --way-back runs$`, func() error {
		_ = os.Remove(c.systemctlLog())
		c.mwMemberServe(true)
		return nil
	})

	ctx.Then(`^member serve succeeds$`, c.serveSucceeds)
	ctx.Then(`^member serve is refused, saying "([^"]*)"$`, c.serveRefusedSaying)
	ctx.Then(`^the member's mw-dispatch\.service and mw-dispatch\.timer are the holder's units$`, c.unitsAreTheHolders)
	ctx.Then(`^systemctl read the units again and enabled and started mw-dispatch\.timer$`, c.timerEnabled)
	ctx.Then(`^the member's dispatch\.env has a PATH line naming the directories of mw, bd, git, tmux and codex$`, c.pathNamesTools)
	ctx.Then(`^the member's dispatch\.env still holds "([^"]*)"$`, c.envStillHolds)
	ctx.Then(`^the report says the dispatch\.env was left as it was$`, func() error {
		if !strings.Contains(c.served.String(), "left as it was") {
			return fmt.Errorf("the report does not say dispatch.env was left as it was:\n%s", c.served.String())
		}
		return nil
	})
	ctx.Then(`^no dispatch unit or dispatch\.env was written$`, c.noDispatchFiles)
	ctx.Then(`^systemctl was not run to enable anything$`, func() error {
		log, _ := os.ReadFile(c.systemctlLog())
		if strings.Contains(string(log), "mw-dispatch") {
			return fmt.Errorf("systemctl was run for the dispatch units:\n%s", log)
		}
		return nil
	})
	ctx.Then(`^the way back says to disable mw-dispatch\.timer and to remove the two unit files$`, c.wayBackSays)
	ctx.Then(`^the units are still there$`, func() error {
		for _, name := range []string{"mw-dispatch.service", "mw-dispatch.timer"} {
			if _, err := os.Stat(c.path(".config", "systemd", "user", name)); err != nil {
				return fmt.Errorf("%s is gone: %w", name, err)
			}
		}
		return nil
	})
	ctx.Then(`^systemctl was not run again$`, c.systemctlNotRun)
}

func writeFileIn(path, text string) error {
	return os.WriteFile(path, []byte(text), 0o644)
}

func (c *memberContext) mwMemberServe(wayBack bool) {
	req := application.MemberServeRequest{Home: c.home, User: "luke"}
	if wayBack {
		c.wayBack = application.MemberServeWayBack(req.Home)
		c.err = nil
		return
	}
	c.served, c.err = application.MemberServe{
		Host:    c.host,
		Service: millwright.DispatchService,
		Timer:   millwright.DispatchTimer,
	}.Run(context.Background(), req)
}

func (c *memberContext) serveSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("member serve failed: %w", c.err)
	}
	return nil
}

func (c *memberContext) serveRefusedSaying(said string) error {
	if c.err == nil {
		return fmt.Errorf("member serve succeeded; wanted a refusal saying %q", said)
	}
	if !strings.Contains(c.err.Error(), said) {
		return fmt.Errorf("member serve was refused saying %q; wanted it to say %q", c.err, said)
	}
	return nil
}

func (c *memberContext) unitsAreTheHolders() error {
	for name, want := range map[string]string{
		"mw-dispatch.service": millwright.DispatchService,
		"mw-dispatch.timer":   millwright.DispatchTimer,
	} {
		got, err := os.ReadFile(c.path(".config", "systemd", "user", name))
		if err != nil {
			return err
		}
		if string(got) != want {
			return fmt.Errorf("%s is not the holder's unit:\n%s", name, got)
		}
	}
	for _, line := range []string{"ExecStart=/usr/bin/env mw dispatch", "EnvironmentFile=-%h/.config/mw/dispatch.env", "KillMode=process"} {
		if !strings.Contains(millwright.DispatchService, line) {
			return fmt.Errorf("the holder's service has no %q", line)
		}
	}
	return nil
}

func (c *memberContext) timerEnabled() error {
	log, _ := os.ReadFile(c.systemctlLog())
	for _, want := range []string{"--user daemon-reload", "--user enable --now mw-dispatch.timer"} {
		if !strings.Contains(string(log), want) {
			return fmt.Errorf("systemctl was not run with %q; it ran:\n%s", want, log)
		}
	}
	return nil
}

func (c *memberContext) pathNamesTools() error {
	data, err := os.ReadFile(c.path(".config", "mw", "dispatch.env"))
	if err != nil {
		return err
	}
	var path string
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "PATH="); ok {
			path = v
		}
	}
	if path == "" {
		return fmt.Errorf("dispatch.env has no PATH line:\n%s", data)
	}
	dirs := strings.Split(path, ":")
	for _, tool := range []string{"mw", "bd", "git", "tmux", "codex"} {
		want := "/opt/" + tool + "/bin"
		found := false
		for _, dir := range dirs {
			found = found || dir == want
		}
		if !found {
			return fmt.Errorf("the PATH line %q does not name %s", path, want)
		}
	}
	return nil
}

func (c *memberContext) envStillHolds(text string) error {
	data, err := os.ReadFile(c.path(".config", "mw", "dispatch.env"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != text {
		return fmt.Errorf("dispatch.env holds %q; wanted %q", data, text)
	}
	return nil
}

func (c *memberContext) noDispatchFiles() error {
	for _, path := range []string{
		c.path(".config", "mw", "dispatch.env"),
		c.path(".config", "systemd", "user", "mw-dispatch.service"),
		c.path(".config", "systemd", "user", "mw-dispatch.timer"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return fmt.Errorf("%s was written (err %v)", path, err)
		}
	}
	return nil
}

func (c *memberContext) wayBackSays() error {
	for _, want := range []string{
		"systemctl --user disable --now mw-dispatch.timer",
		"rm " + c.path(".config", "systemd", "user", "mw-dispatch.service"),
		c.path(".config", "systemd", "user", "mw-dispatch.timer"),
	} {
		if !strings.Contains(c.wayBack, want) {
			return fmt.Errorf("the way back lacks %q:\n%s", want, c.wayBack)
		}
	}
	return nil
}

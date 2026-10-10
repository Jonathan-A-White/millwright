package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// vpsNginxContext is one scenario of features/vps_nginx.feature: the home, the
// VPS's upstream, the standby's vps_health, and what the guard read.
type vpsNginxContext struct {
	home    string
	servers []string
	health  string
	reading application.VPSNginxReading
}

// guardVPS is a VPS that answers with a fixed nginx site text.
type guardVPS struct{ site string }

func (v guardVPS) NginxSite(context.Context) (string, error)         { return v.site, nil }
func (v guardVPS) BinaryLacks(context.Context, string) (bool, error) { return false, nil }

// InitializeVPSNginxScenario registers the steps of features/vps_nginx.feature.
func InitializeVPSNginxScenario(ctx *godog.ScenarioContext) {
	c := &vpsNginxContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = vpsNginxContext{}
		return ctx, nil
	})

	ctx.Given(`^the home is "([^"]*)"$`, func(host string) error { c.home = host; return nil })
	ctx.Given(`^the VPS's upstream holds ((?:"[^"]*"(?:, | and )?)+)$`, c.theUpstreamHolds)
	ctx.Given(`^a backend names the standby's vps_health "([^"]*)"$`, func(url string) error { c.health = url; return nil })

	ctx.When(`^the VPS nginx guard reads the upstream$`, c.read)

	ctx.Then(`^the VPS nginx guard finds the upstream ok$`, func() error { return c.finds(application.VPSNginxOK) })
	ctx.Then(`^the VPS nginx guard finds a fault$`, func() error { return c.finds(application.VPSNginxFault) })
	ctx.Then(`^the VPS nginx guard's fault says "([^"]*)"$`, c.faultSays)
	ctx.Then(`^the VPS nginx guard's line is "([^"]*)"$`, c.lineIs)
}

func (c *vpsNginxContext) theUpstreamHolds(list string) error {
	c.servers = nil
	for _, part := range strings.Split(list, `"`) {
		if strings.HasPrefix(part, "server ") {
			c.servers = append(c.servers, part)
		}
	}
	return nil
}

func (c *vpsNginxContext) read() error {
	site := "# mw-api-upstream\nupstream postern_api {\n    " + strings.Join(c.servers, "\n    ") + "\n}\n"
	c.reading = application.VPSNginx{
		Home:          &apptest.FakeHomeFile{Text: c.home + " 2026-09-29T00:10:00Z mw@" + c.home + "\n"},
		VPS:           guardVPS{site: site},
		StandbyHealth: c.health,
	}.Read(context.Background())
	return nil
}

func (c *vpsNginxContext) finds(want application.VPSNginxState) error {
	if c.reading.State != want {
		return fmt.Errorf("the guard found state %d (%+v), want %d", c.reading.State, c.reading, want)
	}
	return nil
}

func (c *vpsNginxContext) faultSays(want string) error {
	if !strings.Contains(c.reading.Why, want) {
		return fmt.Errorf("the fault %q does not say %q", c.reading.Why, want)
	}
	return nil
}

func (c *vpsNginxContext) lineIs(want string) error {
	if got := c.reading.Line(); got != want {
		return fmt.Errorf("the line is %q, want %q", got, want)
	}
	return nil
}

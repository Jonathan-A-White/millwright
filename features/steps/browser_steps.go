package steps

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/browser"

	"github.com/cucumber/godog"
)

// browserContext is mw browser over a fake launcher: the programs it would
// start answer on free ports of 127.0.0.1, and the temp root and state
// directory are the scenario's own.
type browserContext struct {
	fake    *browser.Fake
	b       browser.Browser
	root    string
	opened  browser.Opened
	status  browser.Status
	closed  browser.Closed
	openErr error
	profile string
}

// InitializeBrowserScenario registers the steps of features/browser.feature.
func InitializeBrowserScenario(ctx *godog.ScenarioContext) {
	c := &browserContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = browserContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.fake != nil {
			c.fake.StopAll()
		}
		if c.root != "" {
			_ = os.RemoveAll(c.root)
		}
		return ctx, nil
	})

	ctx.Given(`^a fake launcher for the browser$`, c.aFakeLauncher)
	ctx.Step(`^the browser is opened at "([^"]*)"$`, c.opened_)
	ctx.When(`^the browser is opened again at "([^"]*)"$`, c.openedAgain)
	ctx.Given(`^Chromium shows a tab at "([^"]*)"$`, c.showsATab)
	ctx.Given(`^Chromium wrote a cookie and local storage into its profile$`, c.wroteACookie)
	ctx.When(`^the browser's status is asked$`, c.statusAsked)
	ctx.When(`^the browser is closed$`, c.closedNow)
	ctx.Then(`^the VNC display was started listening on 127\.0\.0\.1 only with no password$`, c.vncLocalOnly)
	ctx.Then(`^Chromium was started on that display at "([^"]*)"$`, c.chromiumOnDisplay)
	ctx.Then(`^Chromium's DevTools port is on 127\.0\.0\.1 only$`, c.devToolsLocal)
	ctx.Then(`^Chromium's profile is a new directory of mode 700$`, c.profileMode)
	ctx.Then(`^the browser said the line "([^"]*)"$`, c.saidTheLine)
	ctx.Then(`^the browser said the VNC address "([^"]*)"$`, c.saidTheVNCAddress)
	ctx.Then(`^the status says the browser is up$`, c.statusUp)
	ctx.Then(`^the status names the tab "([^"]*)"$`, c.statusNamesTab)
	ctx.Then(`^the status names the CDP endpoint for chromium\.connectOverCDP$`, c.statusNamesCDP)
	ctx.Then(`^the status says the browser is not open$`, c.statusNotOpen)
	ctx.Then(`^Chromium and the VNC display were stopped$`, c.bothStopped)
	ctx.Then(`^Chromium's profile directory is gone$`, c.profileGone)
	ctx.Then(`^no cookie or storage file is left anywhere mw writes$`, c.noCookieLeft)
	ctx.Then(`^the browser's status says it is not open$`, c.statusAfterCloseNotOpen)
	ctx.Then(`^the second open is refused saying: (.+)$`, c.secondOpenRefused)
	ctx.Then(`^only one Chromium was started$`, c.onlyOneChromium)
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (c *browserContext) aFakeLauncher() error {
	root, err := os.MkdirTemp("", "mw-browser-feature-")
	if err != nil {
		return err
	}
	c.root = root
	vnc, err := freePort()
	if err != nil {
		return err
	}
	devtools, err := freePort()
	if err != nil {
		return err
	}
	tmp := filepath.Join(root, "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return err
	}
	c.fake = browser.NewFake()
	c.b = browser.Browser{
		Launcher:     c.fake,
		StateDir:     filepath.Join(root, "state", "mw", "browser"),
		TempRoot:     tmp,
		Display:      5,
		VNCPort:      vnc,
		DevToolsPort: devtools,
		Xtigervnc:    "/usr/bin/Xtigervnc",
		Chromium:     "/opt/chromium/chrome",
		Ready:        5 * time.Second,
	}
	return nil
}

func (c *browserContext) opened_(url string) error {
	o, err := c.b.Open(context.Background(), url)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	c.opened = o
	c.profile = o.State.Profile
	return nil
}

func (c *browserContext) openedAgain(url string) error {
	_, c.openErr = c.b.Open(context.Background(), url)
	return nil
}

func (c *browserContext) showsATab(url string) error {
	return c.fake.AddTab(url)
}

func (c *browserContext) wroteACookie() error {
	files := map[string]string{
		filepath.Join("Default", "Cookies"):                                "cookie",
		filepath.Join("Default", "Local Storage", "leveldb", "000003.log"): "storage",
		filepath.Join("Default", "Login Data"):                             "login",
	}
	for name, body := range files {
		p := filepath.Join(c.profile, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (c *browserContext) statusAsked() error {
	s, err := c.b.Status(context.Background())
	if err != nil {
		return err
	}
	c.status = s
	return nil
}

func (c *browserContext) closedNow() error {
	cl, err := c.b.Close(context.Background())
	if err != nil {
		return err
	}
	c.closed = cl
	return nil
}

func (c *browserContext) started(name string) (browser.Program, error) {
	for _, p := range c.fake.Started() {
		if p.Name == name {
			return p, nil
		}
	}
	return browser.Program{}, fmt.Errorf("%s was not started; started: %v", name, c.fake.Started())
}

func hasArgs(args []string, want ...string) bool {
	for i := range args {
		if i+len(want) <= len(args) && slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func (c *browserContext) vncLocalOnly() error {
	p, err := c.started(browser.VNCName)
	if err != nil {
		return err
	}
	if p.Path != "/usr/bin/Xtigervnc" || len(p.Args) == 0 || p.Args[0] != ":5" {
		return fmt.Errorf("the VNC display was started as %s %v, want Xtigervnc :5", p.Path, p.Args)
	}
	for _, want := range [][]string{{"-localhost"}, {"-interface", "127.0.0.1"}, {"-SecurityTypes", "None"}, {"-rfbport", fmt.Sprint(c.b.VNCPort)}} {
		if !hasArgs(p.Args, want...) {
			return fmt.Errorf("Xtigervnc's arguments %v lack %v", p.Args, want)
		}
	}
	if slices.Contains(p.Args, "-PasswordFile") || slices.Contains(p.Args, "-rfbauth") {
		return fmt.Errorf("Xtigervnc was given a password file: %v", p.Args)
	}
	return nil
}

func (c *browserContext) chromiumOnDisplay(url string) error {
	p, err := c.started(browser.ChromiumName)
	if err != nil {
		return err
	}
	if p.Path != "/opt/chromium/chrome" {
		return fmt.Errorf("Chromium was started as %s", p.Path)
	}
	if !slices.Contains(p.Env, "DISPLAY=:5") {
		return fmt.Errorf("Chromium's environment lacks DISPLAY=:5: %v", p.Env)
	}
	for _, kv := range p.Env {
		if strings.HasPrefix(kv, "WAYLAND_DISPLAY=") {
			return fmt.Errorf("Chromium's environment keeps %s, so it could open on another screen", kv)
		}
	}
	if p.Args[len(p.Args)-1] != url {
		return fmt.Errorf("Chromium's last argument is %q, want %q", p.Args[len(p.Args)-1], url)
	}
	return nil
}

func (c *browserContext) devToolsLocal() error {
	p, err := c.started(browser.ChromiumName)
	if err != nil {
		return err
	}
	for _, want := range []string{"--remote-debugging-address=127.0.0.1", fmt.Sprintf("--remote-debugging-port=%d", c.b.DevToolsPort)} {
		if !slices.Contains(p.Args, want) {
			return fmt.Errorf("Chromium's arguments %v lack %s", p.Args, want)
		}
	}
	return nil
}

func (c *browserContext) profileMode() error {
	if filepath.Dir(c.profile) != c.b.TempRoot || !strings.HasPrefix(filepath.Base(c.profile), browser.ProfilePrefix) {
		return fmt.Errorf("the profile %s is not a new directory in %s", c.profile, c.b.TempRoot)
	}
	fi, err := os.Stat(c.profile)
	if err != nil {
		return err
	}
	if fi.Mode().Perm() != 0o700 {
		return fmt.Errorf("the profile's mode is %v, want 700", fi.Mode().Perm())
	}
	p, _ := c.started(browser.ChromiumName)
	if !slices.Contains(p.Args, "--user-data-dir="+c.profile) {
		return fmt.Errorf("Chromium was not given the profile %s: %v", c.profile, p.Args)
	}
	return nil
}

func (c *browserContext) said() string {
	return c.opened.Say("lap")
}

// saidTheLine reads 5905, display :5's port, as the fake's own free VNC port.
func (c *browserContext) saidTheLine(line string) error {
	line = strings.ReplaceAll(line, "5905", fmt.Sprint(c.b.VNCPort))
	for _, l := range strings.Split(c.said(), "\n") {
		if strings.Contains(l, line) {
			return nil
		}
	}
	return fmt.Errorf("the browser did not say %q:\n%s", line, c.said())
}

func (c *browserContext) saidTheVNCAddress(addr string) error {
	// The fake's VNC port is a free one; the address it names is that port.
	want := strings.Replace(addr, "5905", fmt.Sprint(c.b.VNCPort), 1)
	if c.b.VNCPort != 5905 && strings.Contains(c.said(), "5905") {
		return fmt.Errorf("the browser named 5905, not its own port %d:\n%s", c.b.VNCPort, c.said())
	}
	if !strings.Contains(c.said(), want) {
		return fmt.Errorf("the browser did not name the VNC address %s:\n%s", want, c.said())
	}
	return nil
}

func (c *browserContext) statusUp() error {
	if !c.status.Up() || !strings.Contains(c.status.String(), "up") {
		return fmt.Errorf("the status is not up: %s", c.status)
	}
	return nil
}

func (c *browserContext) statusNamesTab(url string) error {
	for _, t := range c.status.Tabs {
		if t.URL == url && strings.Contains(c.status.String(), url) {
			return nil
		}
	}
	return fmt.Errorf("the status names no tab %s: %s", url, c.status)
}

func (c *browserContext) statusNamesCDP() error {
	want := fmt.Sprintf("http://127.0.0.1:%d", c.b.DevToolsPort)
	if c.status.Endpoint != want || !strings.Contains(c.status.String(), want) || !strings.Contains(c.status.String(), "connectOverCDP") {
		return fmt.Errorf("the status does not name the CDP endpoint %s: %s", want, c.status)
	}
	return nil
}

func (c *browserContext) statusNotOpen() error {
	if c.status.Open || !strings.Contains(c.status.String(), "not open") {
		return fmt.Errorf("the status is not 'not open': %s", c.status)
	}
	return nil
}

func (c *browserContext) bothStopped() error {
	for _, name := range []string{browser.ChromiumName, browser.VNCName} {
		if c.fake.Up(name) {
			return fmt.Errorf("%s still runs after close", name)
		}
	}
	return nil
}

func (c *browserContext) profileGone() error {
	if _, err := os.Stat(c.profile); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the profile %s is still there (%v)", c.profile, err)
	}
	return nil
}

func (c *browserContext) noCookieLeft() error {
	left, err := browser.SessionFilesUnder(c.root)
	if err != nil {
		return err
	}
	if len(left) > 0 {
		return fmt.Errorf("session files survive the close: %v", left)
	}
	return nil
}

func (c *browserContext) statusAfterCloseNotOpen() error {
	if err := c.statusAsked(); err != nil {
		return err
	}
	return c.statusNotOpen()
}

func (c *browserContext) secondOpenRefused(words string) error {
	if c.openErr == nil || !strings.Contains(c.openErr.Error(), words) {
		return fmt.Errorf("the second open was not refused saying %q: %v", words, c.openErr)
	}
	return nil
}

func (c *browserContext) onlyOneChromium() error {
	n := 0
	for _, p := range c.fake.Started() {
		if p.Name == browser.ChromiumName {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("%d Chromiums were started, want 1", n)
	}
	return nil
}

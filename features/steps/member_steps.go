package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/member"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// memberHost is the real member.Host (files under a throwaway home, git, a
// systemctl and a loginctl that are scripts of the scenario's own) with the
// five things that need a live Dolt server, the network or the real bd
// replaced by stand-ins that record what they were asked. The real Dolt and
// bd are tried by hand, in docs/family.md.
type memberHost struct {
	*member.Host

	noDolt    bool
	portTaken bool
	noTool    map[string]bool

	users []string
	inits []memberBeadsInit
}

type memberBeadsInit struct {
	dir, prefix, user string
	port              int
}

func (h *memberHost) DoltProgram(given string) (string, error) {
	if h.noDolt {
		return "", fmt.Errorf("dolt was not found on the PATH: --dolt names it")
	}
	return "/opt/dolt/bin/dolt", nil
}

// Program finds a tool at a place of its own, /opt/<name>/bin/<name>, so a PATH
// line built from them names five different directories.
func (h *memberHost) Program(name string) (string, error) {
	if h.noTool[name] {
		return "", fmt.Errorf("%s was not found on the PATH", name)
	}
	return "/opt/" + name + "/bin/" + name, nil
}

func (h *memberHost) PortFree(_ context.Context, port int) error {
	if h.portTaken {
		return fmt.Errorf("port %d is taken by another server", port)
	}
	return nil
}

func (h *memberHost) ServerAnswers(context.Context, int) error { return nil }

func (h *memberHost) CreateBeadsUser(_ context.Context, port int, user, password string) error {
	h.users = append(h.users, fmt.Sprintf("%s:%d", user, port))
	return nil
}

func (h *memberHost) InitBeads(_ context.Context, dir, prefix string, port int, user, password string) error {
	h.inits = append(h.inits, memberBeadsInit{dir: dir, prefix: prefix, user: user, port: port})
	return os.MkdirAll(filepath.Join(dir, ".beads"), 0o755)
}

type memberContext struct {
	home    string
	host    *memberHost
	linger  string
	report  application.MemberReport
	err     error
	removed application.MemberRemoveReport
	served  application.MemberServeReport
	// wayBack is what mw member serve --way-back printed.
	wayBack string
}

// InitializeMemberScenario registers the steps of features/member.feature.
func InitializeMemberScenario(ctx *godog.ScenarioContext) {
	c := &memberContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = memberContext{linger: "yes"}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway home for mw member$`, c.aThrowawayHome)
	ctx.Given(`^a dolt that is found and a port that is free$`, func() error { return nil })
	ctx.Given(`^no dolt can be found$`, func() error { c.host.noDolt = true; return nil })
	ctx.Given(`^the Dolt port is taken$`, func() error { c.host.portTaken = true; return nil })
	ctx.Given(`^loginctl says linger is not set$`, func() error { c.setLinger("no"); return nil })
	ctx.Given(`^loginctl says linger is set$`, func() error { c.setLinger("yes"); return nil })
	ctx.Given(`^a member directory "([^"]*)" holding the file "([^"]*)"$`, c.aDirectoryHolding)
	ctx.Given(`^mw member init has made "([^"]*)" with the vault "([^"]*)"$`, c.hasMade)

	ctx.When(`^mw member init makes "([^"]*)" with the vault "([^"]*)", the Dolt port (\d+), the holder "([^"]*)" and the backend "([^"]*)"$`, c.mwMemberInit)
	ctx.When(`^mw member remove runs$`, c.mwMemberRemove)
	c.registerServe(ctx)

	ctx.Then(`^member init succeeds$`, c.initSucceeds)
	ctx.Then(`^member init is refused, saying "([^"]*)"$`, c.refusedSaying)
	ctx.Then(`^member init is refused, saying port (\d+) is taken$`, func(port string) error {
		return c.refusedSaying("port " + port + " is taken")
	})
	ctx.Then(`^member init is refused, saying dolt was not found and --dolt names it$`, func() error {
		return c.refusedSaying("--dolt names it")
	})
	ctx.Then(`^member init is refused, saying the name will not do$`, func() error { return c.refusedSaying("the name") })
	ctx.Then(`^member init is refused, saying the port will not do$`, func() error { return c.refusedSaying("the Dolt port") })
	ctx.Then(`^member init is refused, saying the holder is needed$`, func() error { return c.refusedSaying("holder") })
	ctx.Then(`^the member's config file names the vault "([^"]*)", the host "([^"]*)" and the postern backend "([^"]*)"$`, c.configNames)
	ctx.Then(`^the member's config file still names the vault "([^"]*)"$`, c.configStillNames)
	ctx.Then(`^the member's beads.env points bd at 127\.0\.0\.1 port (\d+) with a generated password that only its owner can read$`, c.beadsEnv)
	ctx.Then(`^the member's Dolt unit serves port (\d+) and was enabled and started$`, c.unitServes)
	ctx.Then(`^the member's vault is a git repository holding the starter kit's seat files$`, c.vaultHoldsKit)
	ctx.Then(`^the beads database was made in the vault with the prefix "([^"]*)" on port (\d+)$`, c.databaseMade)
	ctx.Then(`^the vault's first commit carries no attribution$`, c.noAttribution)
	ctx.Then(`^the Mayor's charter has a Family section after Who you serve$`, c.familyAfterWhoYouServe)
	ctx.Then(`^the Family section names "([^"]*)"$`, c.familyNames)
	ctx.Then(`^the Family section says every epic is released only on the holder's approval, a decision card tapped from their account$`, c.familyApproval)
	ctx.Then(`^the Family section says the holder's key has the cockpit role here$`, c.familyCockpit)
	ctx.Then(`^there is no member directory "([^"]*)"$`, c.noDirectory)
	ctx.Then(`^the report names each file it wrote$`, c.reportNamesFiles)
	ctx.Then(`^the report names mw member remove as the way back, which deletes nothing$`, c.reportNamesWayBack)
	ctx.Then(`^the report names loginctl enable-linger as a hand step$`, c.reportNamesLinger)
	ctx.Then(`^the report does not name loginctl enable-linger$`, c.reportNoLinger)
	ctx.Then(`^no member config file, beads\.env or Dolt unit was written$`, c.nothingWritten)
	ctx.Then(`^systemctl was not run$`, c.systemctlNotRun)
	ctx.Then(`^member remove succeeds$`, func() error {
		if c.err != nil {
			return fmt.Errorf("member remove failed: %w", c.err)
		}
		return nil
	})
	ctx.Then(`^systemctl disabled and stopped the Dolt unit$`, c.systemctlDisabled)
	ctx.Then(`^the member's config file, beads\.env, Dolt unit file and vault are all still there$`, c.allStillThere)
}

func (c *memberContext) aThrowawayHome() error {
	home, err := os.MkdirTemp("", "mw-member-")
	if err != nil {
		return err
	}
	c.home = home
	c.host = &memberHost{Host: &member.Host{
		Systemctl: c.script("systemctl", `echo "$@" >> `+c.systemctlLog()),
		Loginctl:  c.script("loginctl", `echo "Linger=`+c.linger+`"`),
	}}
	return nil
}

func (c *memberContext) systemctlLog() string { return filepath.Join(c.home, "systemctl.log") }

// script writes a stand-in program into the throwaway home and returns its path.
func (c *memberContext) script(name, body string) string {
	dir := filepath.Join(c.home, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		panic(err)
	}
	return path
}

func (c *memberContext) setLinger(state string) {
	c.linger = state
	c.host.Loginctl = c.script("loginctl", `echo "Linger=`+state+`"`)
}

func (c *memberContext) path(parts ...string) string {
	return filepath.Join(append([]string{c.home}, parts...)...)
}

func (c *memberContext) aDirectoryHolding(name, file string) error {
	dir := c.path(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, file), []byte("mine\n"), 0o644)
}

func (c *memberContext) hasMade(name, vaultName string) error {
	if err := c.mwMemberInit(name, vaultName, "3308", "Jonathan (the Governor)", "http://10.88.0.2:8788"); err != nil {
		return err
	}
	return c.initSucceeds()
}

func (c *memberContext) request(name, vaultName string, port int, holder, backend string) application.MemberRequest {
	return application.MemberRequest{
		Name:     name,
		Home:     c.home,
		Dir:      c.path(vaultName),
		DoltPort: port,
		Holder:   holder,
		Backend:  backend,
		Host:     "laptop",
	}
}

func (c *memberContext) mwMemberInit(name, vaultName, port, holder, backend string) error {
	var n int
	fmt.Sscanf(port, "%d", &n)
	birth := vault.NewBirth("mw@laptop", "HOME="+c.home, "XDG_CONFIG_HOME="+c.path(".config"), "GIT_CONFIG_NOSYSTEM=1")
	c.report, c.err = application.Member{Vault: birth, Host: c.host, Template: millwright.Template()}.
		Run(context.Background(), c.request(name, vaultName, n, holder, backend))
	return nil
}

func (c *memberContext) mwMemberRemove() error {
	c.removed, c.err = application.MemberRemove{Host: c.host}.Run(context.Background(), application.MemberRemoveRequest{Home: c.home})
	return nil
}

func (c *memberContext) initSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("member init failed: %w", c.err)
	}
	return nil
}

func (c *memberContext) refusedSaying(said string) error {
	if c.err == nil {
		return fmt.Errorf("member init succeeded; wanted a refusal saying %q", said)
	}
	if !strings.Contains(c.err.Error(), said) {
		return fmt.Errorf("member init was refused saying %q; wanted it to say %q", c.err, said)
	}
	return nil
}

func (c *memberContext) configFile() (string, error) {
	data, err := os.ReadFile(c.path(".config", "mw", "config.toml"))
	return string(data), err
}

func (c *memberContext) configNames(vaultName, host, backend string) error {
	text, err := c.configFile()
	if err != nil {
		return err
	}
	for _, line := range []string{
		fmt.Sprintf("vault = %q", c.path(vaultName)),
		fmt.Sprintf("host  = %q", host),
		fmt.Sprintf("postern_backend = %q", backend),
	} {
		if !strings.Contains(text, line) {
			return fmt.Errorf("config.toml lacks %s:\n%s", line, text)
		}
	}
	return nil
}

func (c *memberContext) configStillNames(vaultName string) error {
	text, err := c.configFile()
	if err != nil {
		return err
	}
	if !strings.Contains(text, fmt.Sprintf("vault = %q", c.path(vaultName))) {
		return fmt.Errorf("config.toml no longer names the vault %q:\n%s", vaultName, text)
	}
	return nil
}

func (c *memberContext) beadsEnv(port string) error {
	path := c.path(".config", "mw", "beads.env")
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("beads.env is mode %o; only its owner may read it (600)", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, line := range []string{
		"BEADS_DOLT_SERVER_MODE=1",
		"BEADS_DOLT_SERVER_HOST=127.0.0.1",
		"BEADS_DOLT_SERVER_PORT=" + port,
		"BEADS_DOLT_SERVER_USER=beads",
		"BEADS_DOLT_AUTO_START=0",
	} {
		if !strings.Contains(text, line+"\n") {
			return fmt.Errorf("beads.env lacks %s:\n%s", line, text)
		}
	}
	password := ""
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "BEADS_DOLT_PASSWORD="); ok {
			password = v
		}
	}
	if len(password) < 16 {
		return fmt.Errorf("beads.env has no generated password (got %q)", password)
	}
	if strings.Contains(c.report.String(), password) {
		return fmt.Errorf("the report prints the generated password")
	}
	return nil
}

func (c *memberContext) unitServes(port string) error {
	data, err := os.ReadFile(c.path(".config", "systemd", "user", "dolt-beads.service"))
	if err != nil {
		return err
	}
	text := string(data)
	want := "ExecStart=/opt/dolt/bin/dolt sql-server --host 127.0.0.1 --port " + port + "\n"
	if !strings.Contains(text, want) {
		return fmt.Errorf("the unit lacks %q:\n%s", want, text)
	}
	if !strings.Contains(text, "WorkingDirectory="+c.path(".local", "share", "mw", "dolt-beads")+"\n") {
		return fmt.Errorf("the unit does not work in the Dolt data directory:\n%s", text)
	}
	if !strings.Contains(text, "WantedBy=default.target") {
		return fmt.Errorf("the unit is not wanted by default.target:\n%s", text)
	}
	log, _ := os.ReadFile(c.systemctlLog())
	for _, want := range []string{"--user daemon-reload", "--user enable --now dolt-beads.service"} {
		if !strings.Contains(string(log), want) {
			return fmt.Errorf("systemctl was not run with %q; it ran:\n%s", want, log)
		}
	}
	return nil
}

func (c *memberContext) vaultDir() string { return c.path("luke-vault") }

func (c *memberContext) vaultHoldsKit() error {
	for _, file := range []string{"seats/mayor/charter.md", "seats/builder/charter.md", "seats/deputy/charter.md", "seats/millhand/charter.md", "seats/README.md"} {
		if _, err := os.Stat(filepath.Join(c.vaultDir(), file)); err != nil {
			return fmt.Errorf("the vault lacks %s: %w", file, err)
		}
	}
	out, err := runGitIn(c.vaultDir(), "rev-list", "--count", "HEAD")
	if err != nil {
		return fmt.Errorf("the vault is not a git repository with a commit: %w: %s", err, out)
	}
	if strings.TrimSpace(out) != "1" {
		return fmt.Errorf("the vault has %s commits; wanted one", strings.TrimSpace(out))
	}
	return nil
}

func (c *memberContext) databaseMade(prefix, port string) error {
	if len(c.host.users) != 1 || c.host.users[0] != "beads:"+port {
		return fmt.Errorf("the beads user was made as %v; wanted beads:%s", c.host.users, port)
	}
	if len(c.host.inits) != 1 {
		return fmt.Errorf("bd init ran %d times; wanted once", len(c.host.inits))
	}
	got := c.host.inits[0]
	if got.dir != c.vaultDir() || got.prefix != prefix || fmt.Sprint(got.port) != port || got.user != "beads" {
		return fmt.Errorf("bd init ran as %+v", got)
	}
	return nil
}

func (c *memberContext) noAttribution() error {
	out, err := runGitIn(c.vaultDir(), "log", "-1", "--format=%B")
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	for _, bad := range []string{"Co-Authored-By", "Generated with"} {
		if strings.Contains(out, bad) {
			return fmt.Errorf("the first commit carries %q:\n%s", bad, out)
		}
	}
	return nil
}

func (c *memberContext) charter() (string, error) {
	data, err := os.ReadFile(filepath.Join(c.vaultDir(), "seats", "mayor", "charter.md"))
	return string(data), err
}

func (c *memberContext) familyAfterWhoYouServe() error {
	text, err := c.charter()
	if err != nil {
		return err
	}
	serve := strings.Index(text, "**Who you serve.**")
	family := strings.Index(text, "\n## Family\n")
	what := strings.Index(text, "**What you do.**")
	if serve < 0 || family < 0 || what < 0 {
		return fmt.Errorf("the charter lacks a part (serve %d, family %d, what %d):\n%s", serve, family, what, text)
	}
	if !(serve < family && family < what) {
		return fmt.Errorf("the Family section is not between Who you serve and What you do (%d, %d, %d)", serve, family, what)
	}
	return nil
}

// family is the Family section's own text: from its heading to the next
// paragraph that is not part of it.
func (c *memberContext) family() (string, error) {
	text, err := c.charter()
	if err != nil {
		return "", err
	}
	_, after, found := strings.Cut(text, "\n## Family\n")
	if !found {
		return "", fmt.Errorf("the charter has no Family section")
	}
	before, _, _ := strings.Cut(after, "**What you do.**")
	return before, nil
}

func (c *memberContext) familyHas(want string) error {
	family, err := c.family()
	if err != nil {
		return err
	}
	if !strings.Contains(family, want) {
		return fmt.Errorf("the Family section lacks %q:\n%s", want, family)
	}
	return nil
}

func (c *memberContext) familyNames(holder string) error { return c.familyHas(holder) }

func (c *memberContext) familyApproval() error {
	return c.familyHas("Every epic is released only on the holder's approval (a decision card they tap from their own account)")
}

func (c *memberContext) familyCockpit() error {
	return c.familyHas("The holder's key has the cockpit role here")
}

func (c *memberContext) noDirectory(name string) error {
	if _, err := os.Stat(c.path(name)); !os.IsNotExist(err) {
		return fmt.Errorf("%s is there (err %v)", name, err)
	}
	return nil
}

func (c *memberContext) reportNamesFiles() error {
	report := c.report.String()
	for _, want := range []string{
		c.path(".config", "mw", "config.toml"),
		c.path(".config", "mw", "beads.env"),
		c.path(".config", "systemd", "user", "dolt-beads.service"),
		c.vaultDir(),
	} {
		if !strings.Contains(report, want) {
			return fmt.Errorf("the report does not name %s:\n%s", want, report)
		}
	}
	return nil
}

func (c *memberContext) reportNamesWayBack() error {
	report := c.report.String()
	if !strings.Contains(report, "mw member remove") || !strings.Contains(report, "deletes nothing") {
		return fmt.Errorf("the report does not name mw member remove as the way back that deletes nothing:\n%s", report)
	}
	return nil
}

func (c *memberContext) reportNamesLinger() error {
	if !strings.Contains(c.report.String(), "loginctl enable-linger") {
		return fmt.Errorf("the report does not name loginctl enable-linger:\n%s", c.report.String())
	}
	return nil
}

func (c *memberContext) reportNoLinger() error {
	if strings.Contains(c.report.String(), "loginctl enable-linger") {
		return fmt.Errorf("the report names loginctl enable-linger though linger is set:\n%s", c.report.String())
	}
	return nil
}

func (c *memberContext) nothingWritten() error {
	for _, path := range []string{
		c.path(".config", "mw", "config.toml"),
		c.path(".config", "mw", "beads.env"),
		c.path(".config", "systemd", "user", "dolt-beads.service"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return fmt.Errorf("%s was written (err %v)", path, err)
		}
	}
	return nil
}

func (c *memberContext) systemctlNotRun() error {
	if log, err := os.ReadFile(c.systemctlLog()); err == nil {
		return fmt.Errorf("systemctl was run:\n%s", log)
	}
	return nil
}

func (c *memberContext) systemctlDisabled() error {
	log, _ := os.ReadFile(c.systemctlLog())
	if !strings.Contains(string(log), "--user disable --now dolt-beads.service") {
		return fmt.Errorf("systemctl did not disable and stop the unit; it ran:\n%s", log)
	}
	return nil
}

func (c *memberContext) allStillThere() error {
	for _, path := range []string{
		c.path(".config", "mw", "config.toml"),
		c.path(".config", "mw", "beads.env"),
		c.path(".config", "systemd", "user", "dolt-beads.service"),
		c.path("luke-vault", "seats", "mayor", "charter.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("member remove deleted %s: %w", path, err)
		}
	}
	return nil
}

package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/claude"

	"github.com/cucumber/godog"
)

// The made-up values beads.env holds in these scenarios. They are checked for
// in the lines and the logs and must never appear there.
const (
	sessionEnvHost     = "stand-in"
	sessionEnvPassword = "not-a-real-value"
)

// sessionEnvKickoff is a kickoff that only survives a run that no shell
// re-reads: quotes of both kinds, a newline and a dollar sign.
const sessionEnvKickoff = "it's \"quoted\"\nsecond line $HOME $(echo x) `id`"

// sessionEnvContext holds a scratch home directory, the stand-ins in it, and
// what running the assembled sessions left in the log.
type sessionEnvContext struct {
	dir     string // scratch directory holding the home and the stand-ins
	envFile string // the path handed to the Harness
	log     string // names the stand-ins recorded, one "who NAME" per line
	kickoff string // the last argument the seat stand-in was given
	line    string // the story session's shell line
	seat    []string

	homeFile *apptest.FakeHomeFile // the vault's home file, as the scenario says it
	thisHost string                // this host's name
	vaultDir string                // the vault the scenario made, where .beads/ may be
	hostOut  string                // where the stand-ins write the host they were started with
}

// InitializeSessionEnvScenario registers the steps of features/session_env.feature.
func InitializeSessionEnvScenario(ctx *godog.ScenarioContext) {
	c := &sessionEnvContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = sessionEnvContext{}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.dir != "" {
			_ = os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a home directory whose beads\.env sets BEADS_DOLT_SERVER_HOST and BEADS_DOLT_PASSWORD to made-up values$`, c.aHomeWithBeadsEnv)
	ctx.Given(`^a home directory with no beads\.env$`, c.aHomeWithNoBeadsEnv)
	ctx.Given(`^a home directory whose beads\.env is empty$`, c.aHomeWithAnEmptyBeadsEnv)

	ctx.Given(`^a home directory whose beads\.env names the server host "([^"]*)"$`, c.aHomeWhoseBeadsEnvNames)
	ctx.Given(`^the vault's home file names "([^"]*)" as the home and this host is "([^"]*)"$`, c.theHomeFileNames)
	ctx.Given(`^the vault holds \.beads/dolt$`, c.theVaultHoldsDolt)
	ctx.Given(`^the vault holds no \.beads/dolt$`, c.theVaultHoldsNoDolt)

	ctx.When(`^a Builder session is started and its line runs, with a stand-in for the harness$`, c.aBuilderSessionRuns)
	ctx.When(`^a seat window is started and its command runs, with a stand-in for the harness$`, c.aSeatWindowRuns)
	ctx.When(`^the story session's line runs, with stand-ins for the harness, the heartbeat and the close-out$`, c.theStoryLineRuns)
	ctx.When(`^the seat window's command runs, with a stand-in for the harness and a kickoff full of quotes$`, c.theSeatCommandRuns)
	ctx.When(`^the story session and the seat window are assembled$`, c.bothAreAssembled)

	ctx.Then(`^the harness saw the server host "([^"]*)"$`, c.theHarnessSawHost)
	ctx.Then(`^the harness, the heartbeat and the close-out each recorded the name ([A-Z_]+)$`, c.eachRecorded)
	ctx.Then(`^the harness recorded the name ([A-Z_]+)$`, c.theHarnessRecorded)
	ctx.Then(`^the harness was handed the kickoff exactly as it was written$`, c.theKickoffArrived)
	ctx.Then(`^the log holds no value from beads\.env$`, c.theLogHoldsNoValue)
	ctx.Then(`^the story line holds the path of beads\.env and no value from it$`, c.theStoryLineHoldsPathOnly)
	ctx.Then(`^the seat window's command holds the path of beads\.env and no value from it$`, c.theSeatCommandHoldsPathOnly)
	ctx.Then(`^the stand-ins all ran$`, c.theStandInsRan)
	ctx.Then(`^no name starting BEADS_DOLT_ was recorded$`, c.noBeadsNameRecorded)
}

// aHome makes the scratch directory and its home, and says where beads.env
// would be under it: where cmd/mw looks for it.
func (c *sessionEnvContext) aHome() error {
	dir, err := os.MkdirTemp("", "mw-session-env")
	if err != nil {
		return fmt.Errorf("making a scratch directory: %w", err)
	}
	c.dir = dir
	c.log = filepath.Join(dir, "names.log")
	c.envFile = filepath.Join(dir, "home", ".config", "mw", "beads.env")
	return os.MkdirAll(filepath.Dir(c.envFile), 0o755)
}

func (c *sessionEnvContext) aHomeWithBeadsEnv() error {
	if err := c.aHome(); err != nil {
		return err
	}
	body := "BEADS_DOLT_SERVER_HOST=" + sessionEnvHost + "\nBEADS_DOLT_PASSWORD=" + sessionEnvPassword + "\n"
	return os.WriteFile(c.envFile, []byte(body), 0o600)
}

func (c *sessionEnvContext) aHomeWithNoBeadsEnv() error { return c.aHome() }

func (c *sessionEnvContext) aHomeWithAnEmptyBeadsEnv() error {
	if err := c.aHome(); err != nil {
		return err
	}
	return os.WriteFile(c.envFile, nil, 0o600)
}

// standIn writes a script beside the log that records only the NAMES of its
// environment, prefixed with who it is, and then does what body says.
func (c *sessionEnvContext) standIn(name, body string) (string, error) {
	path := filepath.Join(c.dir, name)
	script := "#!/bin/sh\nenv | cut -d= -f1 | sed 's/^/" + name + " /' >> " + c.log + "\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("writing the stand-in %s: %w", name, err)
	}
	return path, nil
}

// storySession assembles the story's session with the real Harness and the
// stand-ins. The heartbeat is left sleeping, for the line's own kill to end.
func (c *sessionEnvContext) storySession() (application.SessionSpec, error) {
	harness, err := c.standIn("harness", "sleep 0.5\nprintf '{\"ok\":true}'\n")
	if err != nil {
		return application.SessionSpec{}, err
	}
	heartbeat, err := c.standIn("heartbeat", "exec sleep 30\n")
	if err != nil {
		return application.SessionSpec{}, err
	}
	closeOut, err := c.standIn("closeout", "")
	if err != nil {
		return application.SessionSpec{}, err
	}
	return claude.New(claude.WithProgram(harness), claude.WithEnvFile(c.envFile)).Session(application.Launch{
		StoryID: "mw-gq6.1",
		Path: domain.Path{
			Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
			Model: domain.ModelSonnet, Effort: domain.EffortHigh,
			Formula: "tdd-feature", Host: "laptop",
		},
		Seat:       "builder",
		BootFile:   filepath.Join(c.dir, "boot.md"),
		ResultFile: filepath.Join(c.dir, "result.json"),
		Kickoff:    "kickoff",
		Heartbeat:  []string{heartbeat},
		After:      []string{closeOut},
	})
}

// seatWindow assembles a seat's window with the real Harness and a stand-in
// that also keeps the last argument it was given, the kickoff, byte for byte.
func (c *sessionEnvContext) seatWindow() (application.WindowSpec, error) {
	kickoffOut := filepath.Join(c.dir, "kickoff.out")
	harness, err := c.standIn("seatharness", "for a; do last=$a; done\nprintf %s \"$last\" > "+kickoffOut+"\n")
	if err != nil {
		return application.WindowSpec{}, err
	}
	return claude.New(claude.WithProgram(harness), claude.WithEnvFile(c.envFile)).SeatSession(application.SeatLaunch{
		Seat:     "mayor",
		Name:     "mayor",
		Dir:      c.dir,
		Charter:  filepath.Join(c.dir, "charter.md"),
		Kickoff:  sessionEnvKickoff,
		Attended: true,
	})
}

func (c *sessionEnvContext) theStoryLineRuns() error {
	spec, err := c.storySession()
	if err != nil {
		return fmt.Errorf("assembling the session: %w", err)
	}
	c.line = spec.Command[len(spec.Command)-1]
	if err := exec.Command(spec.Command[0], spec.Command[1:]...).Run(); err != nil {
		return fmt.Errorf("running the assembled shell line: %w", err)
	}
	return nil
}

func (c *sessionEnvContext) theSeatCommandRuns() error {
	spec, err := c.seatWindow()
	if err != nil {
		return fmt.Errorf("assembling the seat window: %w", err)
	}
	c.seat = spec.Command
	if err := exec.Command(spec.Command[0], spec.Command[1:]...).Run(); err != nil {
		return fmt.Errorf("running the seat window's command: %w", err)
	}
	got, err := os.ReadFile(filepath.Join(c.dir, "kickoff.out"))
	if err != nil {
		return fmt.Errorf("reading what the harness was handed: %w", err)
	}
	c.kickoff = string(got)
	return nil
}

func (c *sessionEnvContext) bothAreAssembled() error {
	session, err := c.storySession()
	if err != nil {
		return fmt.Errorf("assembling the session: %w", err)
	}
	c.line = session.Command[len(session.Command)-1]
	window, err := c.seatWindow()
	if err != nil {
		return fmt.Errorf("assembling the seat window: %w", err)
	}
	c.seat = window.Command
	// Anything tmux is given as the window's environment counts too.
	for k, v := range window.Env {
		c.seat = append(c.seat, k+"="+v)
	}
	for k, v := range session.Env {
		c.line += " " + k + "=" + v
	}
	return nil
}

// recorded reports whether who's environment held the name.
func (c *sessionEnvContext) recorded(who, name string) (bool, error) {
	got, err := os.ReadFile(c.log)
	if err != nil {
		return false, fmt.Errorf("reading the log of names: %w", err)
	}
	for _, line := range strings.Split(string(got), "\n") {
		if line == who+" "+name {
			return true, nil
		}
	}
	return false, nil
}

func (c *sessionEnvContext) eachRecorded(name string) error {
	for _, who := range []string{"harness", "heartbeat", "closeout"} {
		if err := c.oneRecorded(who, name); err != nil {
			return err
		}
	}
	return nil
}

func (c *sessionEnvContext) theHarnessRecorded(name string) error {
	return c.oneRecorded("seatharness", name)
}

func (c *sessionEnvContext) oneRecorded(who, name string) error {
	ok, err := c.recorded(who, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the %s did not record the name %s", who, name)
	}
	return nil
}

func (c *sessionEnvContext) theKickoffArrived() error {
	if c.kickoff != sessionEnvKickoff {
		return fmt.Errorf("the harness was handed %q, not the kickoff %q", c.kickoff, sessionEnvKickoff)
	}
	return nil
}

func (c *sessionEnvContext) theLogHoldsNoValue() error {
	got, err := os.ReadFile(c.log)
	if err != nil {
		return fmt.Errorf("reading the log of names: %w", err)
	}
	return holdsNoValue("the log", string(got))
}

// holdsNoValue fails naming only which value leaked, never printing where.
func holdsNoValue(what, text string) error {
	for name, value := range map[string]string{"BEADS_DOLT_SERVER_HOST": sessionEnvHost, "BEADS_DOLT_PASSWORD": sessionEnvPassword} {
		if strings.Contains(text, value) {
			return fmt.Errorf("%s holds the value of %s", what, name)
		}
	}
	return nil
}

func (c *sessionEnvContext) theStoryLineHoldsPathOnly() error {
	if !strings.Contains(c.line, c.envFile) {
		return fmt.Errorf("the story line does not hold the path of beads.env")
	}
	return holdsNoValue("the story line", c.line)
}

func (c *sessionEnvContext) theSeatCommandHoldsPathOnly() error {
	joined := strings.Join(c.seat, "\x00")
	if !strings.Contains(joined, c.envFile) {
		return fmt.Errorf("the seat window's command does not hold the path of beads.env")
	}
	return holdsNoValue("the seat window's command", joined)
}

func (c *sessionEnvContext) theStandInsRan() error {
	for _, who := range []string{"harness", "heartbeat", "closeout", "seatharness"} {
		got, err := os.ReadFile(c.log)
		if err != nil {
			return fmt.Errorf("reading the log of names: %w", err)
		}
		if !strings.Contains(string(got), "\n"+who+" ") && !strings.HasPrefix(string(got), who+" ") {
			return fmt.Errorf("the stand-in %s did not run", who)
		}
	}
	return nil
}

func (c *sessionEnvContext) noBeadsNameRecorded() error {
	got, err := os.ReadFile(c.log)
	if err != nil {
		return fmt.Errorf("reading the log of names: %w", err)
	}
	for _, line := range strings.Split(string(got), "\n") {
		if _, name, _ := strings.Cut(line, " "); strings.HasPrefix(name, "BEADS_DOLT_") {
			return fmt.Errorf("%s recorded a BEADS_DOLT_ name", strings.SplitN(line, " ", 2)[0])
		}
	}
	return nil
}

// aHomeWhoseBeadsEnvNames writes a beads.env that says host, as it did when
// the home was somewhere else, and a password that must never be seen.
func (c *sessionEnvContext) aHomeWhoseBeadsEnvNames(host string) error {
	if err := c.aHome(); err != nil {
		return err
	}
	c.hostOut = filepath.Join(c.dir, "host.out")
	body := "BEADS_DOLT_SERVER_HOST=" + host + "\nBEADS_DOLT_PASSWORD=" + sessionEnvPassword + "\n"
	return os.WriteFile(c.envFile, []byte(body), 0o600)
}

// theHomeFileNames resolves, as cmd/mw does for a host whose beads_sync is
// auto, the host the session is to be given from the home file the vault holds.
// Whether the vault holds .beads/dolt is said by the steps after this one, so
// the resolution waits until the session is started.
func (c *sessionEnvContext) theHomeFileNames(home, host string) error {
	c.homeFile = &apptest.FakeHomeFile{Text: home + " 2026-09-30T12:00:00Z mayor@" + home + "\n"}
	c.thisHost = host
	return nil
}

func (c *sessionEnvContext) theVaultHoldsDolt() error {
	c.vaultDir = filepath.Join(c.dir, "vault")
	return os.MkdirAll(filepath.Join(c.vaultDir, ".beads", "dolt"), 0o755)
}

func (c *sessionEnvContext) theVaultHoldsNoDolt() error {
	c.vaultDir = filepath.Join(c.dir, "vault")
	return os.MkdirAll(filepath.Join(c.vaultDir, ".beads", "embeddeddolt"), 0o755)
}

// resolveHost is the host the harness under test is given.
func (c *sessionEnvContext) resolveHost() string {
	info, err := os.Stat(filepath.Join(c.vaultDir, ".beads", "dolt"))
	return application.SessionServerHost(context.Background(), c.homeFile, c.thisHost,
		application.BeadsSyncAuto, "", err == nil && info.IsDir())
}

func (c *sessionEnvContext) hostStandIn() (string, error) {
	return c.standIn("hostharness", "for a; do last=$a; done\nprintf %s \"$BEADS_DOLT_SERVER_HOST\" > "+c.hostOut+"\n")
}

func (c *sessionEnvContext) aBuilderSessionRuns() error {
	harness, err := c.hostStandIn()
	if err != nil {
		return err
	}
	spec, err := claude.New(claude.WithProgram(harness), claude.WithEnvFile(c.envFile),
		claude.WithBeadsServerHost(c.resolveHost())).Session(application.Launch{
		StoryID: "mw-gq6.1",
		Path: domain.Path{
			Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude,
			Model: domain.ModelSonnet, Effort: domain.EffortHigh,
			Formula: "tdd-feature", Host: c.thisHost,
		},
		Seat:       "builder",
		BootFile:   filepath.Join(c.dir, "boot.md"),
		ResultFile: filepath.Join(c.dir, "result.json"),
		Kickoff:    "kickoff",
	})
	if err != nil {
		return fmt.Errorf("assembling the session: %w", err)
	}
	c.line = spec.Command[len(spec.Command)-1]
	if err := exec.Command(spec.Command[0], spec.Command[1:]...).Run(); err != nil {
		return fmt.Errorf("running the assembled shell line: %w", err)
	}
	return nil
}

func (c *sessionEnvContext) aSeatWindowRuns() error {
	harness, err := c.hostStandIn()
	if err != nil {
		return err
	}
	spec, err := claude.New(claude.WithProgram(harness), claude.WithEnvFile(c.envFile),
		claude.WithBeadsServerHost(c.resolveHost())).SeatSession(application.SeatLaunch{
		Seat: "mayor", Name: "mayor", Dir: c.dir,
		Charter: filepath.Join(c.dir, "charter.md"), Kickoff: "kickoff", Attended: true,
	})
	if err != nil {
		return fmt.Errorf("assembling the seat window: %w", err)
	}
	c.seat = spec.Command
	if err := exec.Command(spec.Command[0], spec.Command[1:]...).Run(); err != nil {
		return fmt.Errorf("running the seat window's command: %w", err)
	}
	return nil
}

func (c *sessionEnvContext) theHarnessSawHost(want string) error {
	got, err := os.ReadFile(c.hostOut)
	if err != nil {
		return fmt.Errorf("reading the host the harness saw: %w", err)
	}
	if string(got) != want {
		return fmt.Errorf("the harness saw the server host %q, not %q", got, want)
	}
	return nil
}

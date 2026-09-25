package steps

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// initHost is the host name a scenario's config file carries unless it says another.
const initHost = "testhost"

// initTracker stands in for beads: it records the prefixes it was asked to
// make a database with, and how many times it was asked to pick one up
// instead. The real `bd init` and `bd bootstrap` are tried in
// infrastructure/beads.
type initTracker struct {
	prefixes     []string
	bootstrapped int

	// dir is the vault directory BootstrapTracker rewrites .beads/config.yaml
	// in, set by runRequest before Run: bd bootstrap works on the vault it
	// was just asked to pick up, and it is the only file this stand-in
	// touches.
	dir string
	// dropNewline stands in for bd 1.3.0's own bd bootstrap, which rewrites
	// .beads/config.yaml and drops its trailing newline.
	dropNewline bool
	// rewrite, when set, is bd bootstrap changing .beads/config.yaml some
	// other way, for the scenario where that is left alone rather than
	// restored.
	rewrite string
}

func (t *initTracker) InitTracker(_ context.Context, prefix string) error {
	t.prefixes = append(t.prefixes, prefix)
	return nil
}

func (t *initTracker) BootstrapTracker(_ context.Context) error {
	t.bootstrapped++
	switch {
	case t.dropNewline:
		path := filepath.Join(t.dir, ".beads", "config.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(strings.TrimRight(string(data), "\n")), 0o644)
	case t.rewrite != "":
		return os.WriteFile(filepath.Join(t.dir, ".beads", "config.yaml"), []byte(t.rewrite), 0o644)
	}
	return nil
}

// initContext holds a throwaway home, in which the vault, the config file and
// what git knows of the committer all live: nothing here reaches the real
// ~/.config/mw, the real vault or the real git config. remoteRoot and
// bareRemote are outside the home, so the one vault a join scenario makes
// under the home is still the only one vaultDir's glob finds.
type initContext struct {
	home    string
	tracker *initTracker

	remoteRoot string
	bareRemote string

	report application.InitReport
	err    error
}

// InitializeInitScenario registers the steps of features/init.feature.
func InitializeInitScenario(ctx *godog.ScenarioContext) {
	c := &initContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = initContext{tracker: &initTracker{}}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		if c.remoteRoot != "" {
			os.RemoveAll(c.remoteRoot)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway home for mw init$`, c.aThrowawayHome)
	ctx.Given(`^an empty directory "([^"]*)"$`, c.anEmptyDirectory)
	ctx.Given(`^a directory "([^"]*)" holding the file "([^"]*)"$`, c.aDirectoryHolding)
	ctx.Given(`^git knows the name "([^"]*)" in the throwaway home$`, c.gitKnowsTheName)
	ctx.Given(`^a config file that says:$`, c.aConfigFileThatSays)
	ctx.Given(`^a bare git remote holding a vault to join$`, c.aBareGitRemoteHoldingAVault)
	ctx.Given(`^bd bootstrap will drop the trailing newline from \.beads/config\.yaml$`, c.bdBootstrapDropsTheNewline)
	ctx.Given(`^bd bootstrap will rewrite \.beads/config\.yaml to "([^"]*)"$`, c.bdBootstrapRewritesConfigTo)

	ctx.When(`^mw init makes the vault "([^"]*)" with the prefix "([^"]*)"$`, c.mwInitMakes)
	ctx.When(`^mw init makes the vault "([^"]*)" with the prefix "([^"]*)", the host "([^"]*)" and the rigs:$`, c.mwInitMakesWithRigs)
	ctx.When(`^mw init joins the vault as "([^"]*)"$`, c.mwInitJoins)
	ctx.When(`^mw init makes the vault "([^"]*)" with the prefix "([^"]*)" and joins "([^"]*)"$`, c.mwInitMakesAndJoins)

	ctx.Then(`^initialising succeeds$`, c.initialisingSucceeds)
	ctx.Then(`^initialising is refused, saying "([^"]*)" is not empty$`, c.refusedNotEmpty)
	ctx.Then(`^initialising is refused, saying the prefix will not do$`, c.refusedPrefix)
	ctx.Then(`^initialising is refused, saying --join and --prefix cannot both be given$`, c.refusedJoinAndPrefix)
	ctx.Then(`^the beads database was picked up rather than made$`, c.databasePickedUp)
	ctx.Then(`^the vault holds the three seat charters$`, c.theVaultHoldsTheCharters)
	ctx.Then(`^the vault's vision and ledger are the template's blank ones$`, c.blankVisionAndLedger)
	ctx.Then(`^the vault is a git repository with one commit$`, c.oneCommit)
	ctx.Then(`^the beads database was made with the prefix "([^"]*)"$`, c.databaseMadeWith)
	ctx.Then(`^no beads database was made$`, c.noDatabaseMade)
	ctx.Then(`^no config file was written$`, c.noConfigWritten)
	ctx.Then(`^the directory "([^"]*)" holds only "([^"]*)"$`, c.holdsOnly)
	ctx.Then(`^there is no directory "([^"]*)"$`, c.noDirectory)
	ctx.Then(`^the first commit's message has no Co-Authored-By and no Generated with line$`, c.noAttribution)
	ctx.Then(`^the first commit was made by "([^"]*)"$`, c.firstCommitBy)
	ctx.Then(`^the config file says:$`, c.theConfigFileSays)
	ctx.Then(`^the config file still says exactly:$`, c.theConfigFileSays)
	ctx.Then(`^the report says what is owed: a private remote, git push and scripts/install-units.sh$`, c.reportSaysWhatIsOwed)
	ctx.Then(`^the report says the config file was written$`, c.reportSaysWritten)
	ctx.Then(`^the report does not say the config file was written$`, c.reportSaysNotWritten)
	ctx.Then(`^the report shows the lines the config file would have had, with the host "([^"]*)" and the rig "([^"]*)"$`, c.reportShowsLines)
	ctx.Then(`^\.beads/config\.yaml holds no uncommitted changes$`, c.configYAMLClean)
	ctx.Then(`^\.beads/config\.yaml holds uncommitted changes$`, c.configYAMLDirty)
	ctx.Then(`^the report says bd bootstrap's dropped trailing newline was restored$`, c.reportSaysNewlineRestored)
	ctx.Then(`^the report does not mention a restored trailing newline$`, c.reportDoesNotMentionRestoredNewline)
}

func (c *initContext) configPath() string { return filepath.Join(c.home, config.File) }

func (c *initContext) aThrowawayHome() error {
	home, err := os.MkdirTemp("", "mw-init-")
	if err != nil {
		return err
	}
	c.home = home
	return nil
}

func (c *initContext) anEmptyDirectory(name string) error {
	return os.MkdirAll(filepath.Join(c.home, name), 0o755)
}

func (c *initContext) aDirectoryHolding(name, file string) error {
	dir := filepath.Join(c.home, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, file), []byte("mine\n"), 0o644)
}

func (c *initContext) gitKnowsTheName(name string) error {
	return os.WriteFile(filepath.Join(c.home, ".gitconfig"),
		[]byte(fmt.Sprintf("[user]\n\tname = %s\n\temail = ada@example.test\n", name)), 0o644)
}

func (c *initContext) aConfigFileThatSays(text *godog.DocString) error {
	if err := os.MkdirAll(filepath.Dir(c.configPath()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.configPath(), []byte(text.Content+"\n"), 0o644)
}

func (c *initContext) mwInitMakes(name, prefix string) error {
	return c.run(name, prefix, initHost, nil)
}

func (c *initContext) mwInitMakesWithRigs(name, prefix, host string, table *godog.Table) error {
	rigs := map[string]string{}
	for _, row := range table.Rows[1:] {
		rigs[row.Cells[0].Value] = row.Cells[1].Value
	}
	return c.run(name, prefix, host, rigs)
}

func (c *initContext) run(name, prefix, host string, rigs map[string]string) error {
	return c.runRequest(application.InitRequest{
		Dir:        filepath.Join(c.home, name),
		Prefix:     prefix,
		Host:       host,
		Rigs:       rigs,
		ConfigPath: c.configPath(),
	})
}

func (c *initContext) runRequest(req application.InitRequest) error {
	birth := vault.NewBirth("mw@"+initHost,
		"HOME="+c.home, "XDG_CONFIG_HOME="+filepath.Join(c.home, ".config"), "GIT_CONFIG_NOSYSTEM=1")
	c.tracker.dir = req.Dir
	c.report, c.err = application.Init{Vault: birth, Tracker: c.tracker, Template: millwright.Template()}.
		Run(context.Background(), req)
	return nil
}

// aBareGitRemoteHoldingAVault makes a vault the way mw init makes a fresh
// one, outside the throwaway home so it is never what vaultDir's glob finds,
// then pushes it to a bare repository: a stand-in for a vault that already
// exists on another host, to clone from in a join scenario.
func (c *initContext) aBareGitRemoteHoldingAVault() error {
	root, err := os.MkdirTemp("", "mw-init-remote-")
	if err != nil {
		return err
	}
	c.remoteRoot = root

	source := filepath.Join(root, "source")
	birth := vault.NewBirth("mw@origin",
		"HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, ".config"), "GIT_CONFIG_NOSYSTEM=1")
	if err := birth.Lay(context.Background(), source, millwright.Template()); err != nil {
		return err
	}
	if err := birth.Commit(context.Background(), source, application.InitFirstCommit); err != nil {
		return err
	}

	// A stand-in for bd init's own config file, so a join scenario has
	// something in .beads for bd bootstrap (initTracker, here) to rewrite.
	if err := os.MkdirAll(filepath.Join(source, ".beads"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(source, ".beads", "config.yaml"), []byte("enabled: false\n"), 0o644); err != nil {
		return err
	}
	if err := birth.Commit(context.Background(), source, application.InitFirstCommit); err != nil {
		return err
	}

	c.bareRemote = filepath.Join(root, "origin.git")
	if out, err := runGitIn(root, "init", "-q", "--bare", "-b", "main", c.bareRemote); err != nil {
		return fmt.Errorf("making the bare remote: %w: %s", err, out)
	}
	if out, err := runGitIn(source, "remote", "add", "origin", c.bareRemote); err != nil {
		return fmt.Errorf("adding the origin remote: %w: %s", err, out)
	}
	if out, err := runGitIn(source, "push", "-q", "origin", "main"); err != nil {
		return fmt.Errorf("pushing the source vault: %w: %s", err, out)
	}
	return nil
}

func runGitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *initContext) bdBootstrapDropsTheNewline() error {
	c.tracker.dropNewline = true
	return nil
}

func (c *initContext) bdBootstrapRewritesConfigTo(content string) error {
	c.tracker.rewrite = content
	return nil
}

func (c *initContext) mwInitJoins(name string) error {
	return c.runRequest(application.InitRequest{
		Dir:        filepath.Join(c.home, name),
		URL:        c.bareRemote,
		Host:       initHost,
		ConfigPath: c.configPath(),
	})
}

func (c *initContext) mwInitMakesAndJoins(name, prefix, url string) error {
	return c.runRequest(application.InitRequest{
		Dir:        filepath.Join(c.home, name),
		Prefix:     prefix,
		URL:        url,
		Host:       initHost,
		ConfigPath: c.configPath(),
	})
}

func (c *initContext) initialisingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("mw init failed: %w", c.err)
	}
	return nil
}

func (c *initContext) refusedNotEmpty(name string) error {
	if c.err == nil {
		return fmt.Errorf("mw init succeeded on a directory with something in it")
	}
	if said := c.err.Error(); !strings.Contains(said, filepath.Join(c.home, name)) || !strings.Contains(said, "not empty") {
		return fmt.Errorf("mw init refused, but said %q rather than that %s is not empty", said, name)
	}
	return nil
}

func (c *initContext) refusedPrefix() error {
	if c.err == nil {
		return fmt.Errorf("mw init took a prefix it should have refused")
	}
	if !strings.Contains(c.err.Error(), "prefix") {
		return fmt.Errorf("mw init refused, but not for the prefix: %v", c.err)
	}
	return nil
}

func (c *initContext) refusedJoinAndPrefix() error {
	if c.err == nil {
		return fmt.Errorf("mw init took both --join and --prefix, which it should have refused")
	}
	if said := c.err.Error(); !strings.Contains(said, "--join") || !strings.Contains(said, "--prefix") {
		return fmt.Errorf("mw init refused, but not for taking both --join and --prefix: %v", c.err)
	}
	return nil
}

func (c *initContext) databasePickedUp() error {
	if c.tracker.bootstrapped != 1 {
		return fmt.Errorf("beads was asked to pick up a database %d times, not once", c.tracker.bootstrapped)
	}
	if len(c.tracker.prefixes) != 0 {
		return fmt.Errorf("beads was asked to make a fresh database with the prefixes %v, rather than picking one up", c.tracker.prefixes)
	}
	return nil
}

func (c *initContext) theVaultHoldsTheCharters() error {
	dir := c.vaultDir()
	for _, seat := range []string{"mayor", "builder", "millhand"} {
		charter := filepath.Join(dir, "seats", seat, "charter.md")
		if written, err := os.ReadFile(charter); err != nil || len(written) == 0 {
			return fmt.Errorf("the %s's charter is not in the vault at %s (%v)", seat, charter, err)
		}
	}
	return nil
}

func (c *initContext) blankVisionAndLedger() error {
	for _, file := range []string{"seats/mayor/vision.md", "seats/builder/ledger.md"} {
		want, err := fs.ReadFile(millwright.Template(), file)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(c.vaultDir(), file))
		if err != nil {
			return err
		}
		if string(got) != string(want) {
			return fmt.Errorf("%s in the vault is not the template's:\n%s", file, got)
		}
	}
	return nil
}

// vaultDir is the one vault a scenario makes, which is the one directory under
// the home that holds a .git.
func (c *initContext) vaultDir() string {
	found, _ := filepath.Glob(filepath.Join(c.home, "*", ".git"))
	if len(found) == 0 {
		return filepath.Join(c.home, "fresh")
	}
	return filepath.Dir(found[0])
}

func (c *initContext) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", c.vaultDir()}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *initContext) oneCommit() error {
	out, err := c.git("rev-list", "--count", "HEAD")
	if err != nil {
		return fmt.Errorf("the vault is not a git repository with a commit: %v: %s", err, out)
	}
	if strings.TrimSpace(out) != "1" {
		return fmt.Errorf("the vault has %s commits, not one", strings.TrimSpace(out))
	}
	if left, err := c.git("status", "--porcelain"); err != nil || strings.TrimSpace(left) != "" {
		return fmt.Errorf("the first commit left files out: %s%v", left, err)
	}
	return nil
}

func (c *initContext) databaseMadeWith(prefix string) error {
	if len(c.tracker.prefixes) != 1 || c.tracker.prefixes[0] != prefix {
		return fmt.Errorf("beads was asked for databases with the prefixes %v, not just %q", c.tracker.prefixes, prefix)
	}
	return nil
}

func (c *initContext) noDatabaseMade() error {
	if len(c.tracker.prefixes) != 0 {
		return fmt.Errorf("beads was asked for a database, with the prefixes %v", c.tracker.prefixes)
	}
	return nil
}

func (c *initContext) noConfigWritten() error {
	if _, err := os.Stat(c.configPath()); !os.IsNotExist(err) {
		return fmt.Errorf("a config file was written at %s (%v)", c.configPath(), err)
	}
	return nil
}

func (c *initContext) holdsOnly(name, file string) error {
	entries, err := os.ReadDir(filepath.Join(c.home, name))
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != file {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return fmt.Errorf("%s holds %v, not only %s", name, names, file)
	}
	return nil
}

func (c *initContext) noDirectory(name string) error {
	if _, err := os.Stat(filepath.Join(c.home, name)); !os.IsNotExist(err) {
		return fmt.Errorf("%s is there (%v)", name, err)
	}
	return nil
}

func (c *initContext) noAttribution() error {
	out, err := c.git("log", "--format=%B")
	if err != nil {
		return fmt.Errorf("reading the vault's log: %v: %s", err, out)
	}
	lower := strings.ToLower(out)
	for _, mark := range []string{"co-authored-by", "generated with"} {
		if strings.Contains(lower, mark) {
			return fmt.Errorf("the first commit carries %q:\n%s", mark, out)
		}
	}
	return nil
}

func (c *initContext) firstCommitBy(name string) error {
	out, err := c.git("log", "--format=%an|%cn|%ae")
	if err != nil {
		return fmt.Errorf("reading the vault's log: %v: %s", err, out)
	}
	if got := strings.TrimSpace(out); !strings.HasPrefix(got, name+"|"+name+"|") {
		return fmt.Errorf("the first commit was made by %q, not %q", got, name)
	}
	return nil
}

func (c *initContext) theConfigFileSays(text *godog.DocString) error {
	want := strings.ReplaceAll(text.Content, "<home>", c.home)
	got, err := os.ReadFile(c.configPath())
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(want) {
		return fmt.Errorf("the config file says:\n%s\nnot:\n%s", got, want)
	}
	return nil
}

func (c *initContext) reportSaysWhatIsOwed() error {
	said := c.report.String()
	for _, owed := range []string{"private remote", "git -C " + c.vaultDir() + " push", "scripts/install-units.sh"} {
		if !strings.Contains(said, owed) {
			return fmt.Errorf("the report does not say %q:\n%s", owed, said)
		}
	}
	return nil
}

func (c *initContext) reportSaysWritten() error {
	if !c.report.ConfigWritten || !strings.Contains(c.report.String(), "Wrote "+c.configPath()) {
		return fmt.Errorf("the report does not say the config file was written:\n%s", c.report)
	}
	return nil
}

func (c *initContext) reportSaysNotWritten() error {
	if c.report.ConfigWritten || strings.Contains(c.report.String(), "Wrote ") {
		return fmt.Errorf("the report says the config file was written:\n%s", c.report)
	}
	return nil
}

func (c *initContext) configYAMLClean() error {
	out, err := c.git("status", "--porcelain", "--", ".beads/config.yaml")
	if err != nil {
		return fmt.Errorf("git status: %v: %s", err, out)
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf(".beads/config.yaml still holds uncommitted changes:\n%s", out)
	}
	return nil
}

func (c *initContext) configYAMLDirty() error {
	out, err := c.git("status", "--porcelain", "--", ".beads/config.yaml")
	if err != nil {
		return fmt.Errorf("git status: %v: %s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf(".beads/config.yaml holds no uncommitted changes, want it left alone with some")
	}
	return nil
}

func (c *initContext) reportSaysNewlineRestored() error {
	if !strings.Contains(c.report.String(), "trailing newline") {
		return fmt.Errorf("the report does not mention the restored trailing newline:\n%s", c.report)
	}
	return nil
}

func (c *initContext) reportDoesNotMentionRestoredNewline() error {
	if strings.Contains(c.report.String(), "trailing newline") {
		return fmt.Errorf("the report mentions a restored trailing newline, but should have left the file alone:\n%s", c.report)
	}
	return nil
}

func (c *initContext) reportShowsLines(host, rig string) error {
	said := c.report.String()
	for _, line := range []string{fmt.Sprintf("host  = %q", host), "cap   = 1", rig + " = "} {
		if !strings.Contains(said, line) {
			return fmt.Errorf("the report does not show the line %q:\n%s", line, said)
		}
	}
	return nil
}

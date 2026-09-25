package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/postern"

	"github.com/cucumber/godog"
)

// posternServeContext holds a throwaway directory tree, in which the config
// file, the nginx site file and both commands' backup directories live:
// nothing here reaches the real ~/.config/mw or a real nginx. The file edits
// run through the real infrastructure/postern.HandFile, straight against
// this tree; only nginx -t and systemctl reload nginx run through a fake, so
// a scenario never touches the real nginx.
type posternServeContext struct {
	home string

	configPath    string
	nginxConfPath string
	nginxOriginal string

	serveBackupDir string
	nginxBackupDir string

	snapshotPath string
	runner       *apptest.FakeNginxRunner

	lastServeReq application.PosternServeRequest
	serveReport  application.PosternServeReport

	lastNginxReq application.PosternNginxRequest
	nginxReport  application.PosternNginxReport

	err error
}

// InitializePosternServeScenario registers the steps of
// features/postern_serve.feature.
func InitializePosternServeScenario(ctx *godog.ScenarioContext) {
	c := &posternServeContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = posternServeContext{runner: apptest.NewFakeNginxRunner()}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.home != "" {
			os.RemoveAll(c.home)
		}
		return ctx, nil
	})

	ctx.Given(`^a throwaway home for the postern hand commands$`, c.aThrowawayHome)
	ctx.Given(`^a postern config file that says:$`, c.aConfigFileThatSays)
	ctx.Given(`^an nginx site file that says:$`, c.anNginxSiteFileThatSays)
	ctx.Given(`^the postern snapshot path is "([^"]*)"$`, c.thePosternSnapshotPathIs)
	ctx.Given(`^the fake nginx test will fail, saying "([^"]*)"$`, c.theFakeNginxTestWillFail)

	ctx.When(`^mw postern serve is run with:$`, c.mwPosternServeIsRunWith)
	ctx.When(`^mw postern serve is run with --dry-run and:$`, c.mwPosternServeIsRunWithDryRun)
	ctx.When(`^mw postern serve is run again with the same values$`, c.mwPosternServeIsRunAgain)
	ctx.When(`^mw postern nginx is run with the backend "([^"]*)"$`, c.mwPosternNginxIsRunWithBackend)
	ctx.When(`^mw postern nginx is run with --dry-run and the backend "([^"]*)"$`, c.mwPosternNginxIsRunWithDryRunBackend)
	ctx.When(`^mw postern nginx is run again with the backend "([^"]*)"$`, c.mwPosternNginxIsRunWithBackend)

	ctx.Then(`^serving succeeds$`, c.servingSucceeds)
	ctx.Then(`^nginxing succeeds$`, c.nginxingSucceeds)
	ctx.Then(`^nginxing fails, saying nginx -t failed$`, c.nginxingFailsNginxT)
	ctx.Then(`^the postern config file says:$`, c.theConfigFileSays)
	ctx.Then(`^there is no config file$`, c.thereIsNoConfigFile)
	ctx.Then(`^the snapshot directory exists$`, c.theSnapshotDirectoryExists)
	ctx.Then(`^there is no snapshot directory$`, c.thereIsNoSnapshotDirectory)
	ctx.Then(`^the config file was backed up exactly (\d+) times?$`, c.theConfigFileWasBackedUpExactly)
	ctx.Then(`^the serve report says it would write postern_backend$`, c.theServeReportSaysItWouldWrite)
	ctx.Then(`^the nginx site file holds:$`, c.theNginxSiteFileHolds)
	ctx.Then(`^the nginx site file is unchanged$`, c.theNginxSiteFileIsUnchanged)
	ctx.Then(`^nginx was tested (\d+) times? and reloaded (\d+) times?$`, c.nginxWasTestedAndReloaded)
	ctx.Then(`^the nginx site file was backed up exactly (\d+) times?$`, c.theNginxSiteFileWasBackedUpExactly)
}

func (c *posternServeContext) expand(text string) string {
	return strings.ReplaceAll(text, "<home>", c.home)
}

func (c *posternServeContext) aThrowawayHome() error {
	home, err := os.MkdirTemp("", "mw-postern-serve-")
	if err != nil {
		return err
	}
	c.home = home
	c.configPath = filepath.Join(home, "config.toml")
	c.nginxConfPath = filepath.Join(home, "nginx", "postern.conf")
	c.serveBackupDir = filepath.Join(home, "backup-serve")
	c.nginxBackupDir = filepath.Join(home, "backup-nginx")
	return nil
}

func (c *posternServeContext) aConfigFileThatSays(text *godog.DocString) error {
	if err := os.MkdirAll(filepath.Dir(c.configPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.configPath, []byte(c.expand(text.Content)+"\n"), 0o644)
}

func (c *posternServeContext) anNginxSiteFileThatSays(text *godog.DocString) error {
	if err := os.MkdirAll(filepath.Dir(c.nginxConfPath), 0o755); err != nil {
		return err
	}
	c.nginxOriginal = text.Content + "\n"
	return os.WriteFile(c.nginxConfPath, []byte(c.nginxOriginal), 0o644)
}

func (c *posternServeContext) thePosternSnapshotPathIs(path string) error {
	c.snapshotPath = path
	return nil
}

func (c *posternServeContext) theFakeNginxTestWillFail(message string) error {
	c.runner.TestOK = false
	c.runner.TestOutput = message
	return nil
}

func (c *posternServeContext) requestFromTable(table *godog.Table) application.PosternServeRequest {
	req := application.PosternServeRequest{}
	for _, row := range table.Rows {
		value := c.expand(row.Cells[1].Value)
		switch row.Cells[0].Value {
		case "backend":
			req.Backend = value
		case "snapshot-path":
			req.SnapshotPath = value
		case "governor-key":
			req.GovernorKey = value
		}
	}
	return req
}

func (c *posternServeContext) runServe(req application.PosternServeRequest) {
	c.lastServeReq = req
	serve := application.PosternServe{
		Files:      postern.NewHandFile(),
		ConfigPath: c.configPath,
		BackupDir:  c.serveBackupDir,
	}
	c.serveReport, c.err = serve.Run(context.Background(), req)
}

func (c *posternServeContext) mwPosternServeIsRunWith(table *godog.Table) error {
	c.runServe(c.requestFromTable(table))
	return nil
}

func (c *posternServeContext) mwPosternServeIsRunWithDryRun(table *godog.Table) error {
	req := c.requestFromTable(table)
	req.DryRun = true
	c.runServe(req)
	return nil
}

func (c *posternServeContext) mwPosternServeIsRunAgain() error {
	c.runServe(c.lastServeReq)
	return nil
}

func (c *posternServeContext) runNginx(req application.PosternNginxRequest) {
	req.SnapshotPath = c.snapshotPath
	c.lastNginxReq = req
	nginx := application.PosternNginx{
		Conf:      postern.NewHandFile(),
		ConfPath:  c.nginxConfPath,
		BackupDir: c.nginxBackupDir,
		Runner:    c.runner,
	}
	c.nginxReport, c.err = nginx.Run(context.Background(), req)
}

func (c *posternServeContext) mwPosternNginxIsRunWithBackend(backend string) error {
	c.runNginx(application.PosternNginxRequest{Backend: backend})
	return nil
}

func (c *posternServeContext) mwPosternNginxIsRunWithDryRunBackend(backend string) error {
	c.runNginx(application.PosternNginxRequest{Backend: backend, DryRun: true})
	return nil
}

func (c *posternServeContext) servingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("serving failed: %w", c.err)
	}
	return nil
}

func (c *posternServeContext) nginxingSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("nginxing failed: %w", c.err)
	}
	return nil
}

func (c *posternServeContext) nginxingFailsNginxT() error {
	if c.err == nil {
		return fmt.Errorf("nginxing succeeded; wanted it to fail on nginx -t")
	}
	if !strings.Contains(c.err.Error(), "nginx -t failed") {
		return fmt.Errorf("nginxing failed with %q, not naming nginx -t", c.err)
	}
	return nil
}

func (c *posternServeContext) theConfigFileSays(text *godog.DocString) error {
	want := c.expand(text.Content)
	got, err := os.ReadFile(c.configPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(want) {
		return fmt.Errorf("the config file says:\n%s\nnot:\n%s", got, want)
	}
	return nil
}

func (c *posternServeContext) thereIsNoConfigFile() error {
	if _, err := os.Stat(c.configPath); !os.IsNotExist(err) {
		return fmt.Errorf("wanted no config file at %s", c.configPath)
	}
	return nil
}

func (c *posternServeContext) theSnapshotDirectoryExists() error {
	info, err := os.Stat(filepath.Dir(c.lastServeReq.SnapshotPath))
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", filepath.Dir(c.lastServeReq.SnapshotPath))
	}
	return nil
}

func (c *posternServeContext) thereIsNoSnapshotDirectory() error {
	if _, err := os.Stat(filepath.Dir(c.lastServeReq.SnapshotPath)); !os.IsNotExist(err) {
		return fmt.Errorf("wanted no snapshot directory at %s", filepath.Dir(c.lastServeReq.SnapshotPath))
	}
	return nil
}

func (c *posternServeContext) theConfigFileWasBackedUpExactly(count string) error {
	return backupCount(c.serveBackupDir, count)
}

func (c *posternServeContext) theNginxSiteFileWasBackedUpExactly(count string) error {
	return backupCount(c.nginxBackupDir, count)
}

func backupCount(dir, want string) error {
	n, err := strconv.Atoi(want)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) && n == 0 {
			return nil
		}
		return err
	}
	if len(entries) != n {
		return fmt.Errorf("%s holds %d backup(s), want %d", dir, len(entries), n)
	}
	return nil
}

func (c *posternServeContext) theServeReportSaysItWouldWrite() error {
	if !c.serveReport.DryRun {
		return fmt.Errorf("the report is not a --dry-run report")
	}
	if !strings.Contains(c.serveReport.NewText, "postern_backend") {
		return fmt.Errorf("the report's text does not mention postern_backend:\n%s", c.serveReport.NewText)
	}
	return nil
}

func (c *posternServeContext) theNginxSiteFileHolds(text *godog.DocString) error {
	want := c.expand(text.Content)
	got, err := os.ReadFile(c.nginxConfPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(want) {
		return fmt.Errorf("the nginx site file holds:\n%s\nnot:\n%s", got, want)
	}
	return nil
}

func (c *posternServeContext) theNginxSiteFileIsUnchanged() error {
	got, err := os.ReadFile(c.nginxConfPath)
	if err != nil {
		return err
	}
	if string(got) != c.nginxOriginal {
		return fmt.Errorf("the nginx site file changed:\n%s\nwant:\n%s", got, c.nginxOriginal)
	}
	return nil
}

func (c *posternServeContext) nginxWasTestedAndReloaded(tests, reloads string) error {
	wantTests, err := strconv.Atoi(tests)
	if err != nil {
		return err
	}
	wantReloads, err := strconv.Atoi(reloads)
	if err != nil {
		return err
	}
	if c.runner.Tests() != wantTests {
		return fmt.Errorf("nginx was tested %d time(s), want %d", c.runner.Tests(), wantTests)
	}
	if c.runner.Reloads() != wantReloads {
		return fmt.Errorf("nginx was reloaded %d time(s), want %d", c.runner.Reloads(), wantReloads)
	}
	return nil
}

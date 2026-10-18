package application_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// memConfig is a config file held in memory.
type memConfig struct {
	path   string
	data   string
	exists bool
	writes int
}

func (m *memConfig) Path() string { return m.path }
func (m *memConfig) Read() ([]byte, bool, error) {
	return []byte(m.data), m.exists, nil
}
func (m *memConfig) Backup(string) (string, error) { return m.path + ".bak", nil }
func (m *memConfig) Write(data []byte) error {
	m.data, m.exists = string(data), true
	m.writes++
	return nil
}

const boostVaultTables = `[tests]
lampas = "npm ci && npm test"

[test_parts.lampas]
"unit" = "npm test"

[test_parts.postern]
"gate" = "make gate"
`

const boostConfigBefore = "host = \"cloud1\"\n\n[rigs]\nlampas = \"/l\"\npostern = \"/p\"\n\n[tests]\nlampas = \"npm ci && npm test\"\n\n[test_parts.lampas]\n\"unit\" = \"npm test\"\n"

func boostSync(t *testing.T) (application.Sync, *memConfig, *memConfig) {
	t.Helper()
	sync, _, _ := syncing(t)
	sync.Host = "cloud1"
	sync.Home = homeIs("laptop")
	config := &memConfig{path: "/h/.config/mw/config.toml", data: boostConfigBefore, exists: true}
	target := &memConfig{path: "/v/hosts/boost-config.toml", data: boostVaultTables, exists: true}
	sync.BoostConfig, sync.BoostTarget = config, target
	return sync, config, target
}

func TestSyncOnABoostAppendsTheTableItLacksAndSaysSo(t *testing.T) {
	sync, config, _ := boostSync(t)
	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := boostConfigBefore + "\n[test_parts.postern]\n\"gate\" = \"make gate\"\n"
	if config.data != want {
		t.Errorf("config =\n%q\nwant\n%q", config.data, want)
	}
	if !strings.HasPrefix(config.data, boostConfigBefore) {
		t.Errorf("a line the Boost had changed")
	}
	if !strings.Contains(report.String(), "config.toml: appended [test_parts.postern]") {
		t.Errorf("the sync line does not say it: %q", report.String())
	}
	writes := config.writes

	report, err = sync.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.writes != writes || strings.Contains(report.String(), "appended") {
		t.Errorf("a second run appended again: %d writes, %q", config.writes, report.String())
	}
}

func TestSyncOnABoostSaysADifferingLineAndLeavesIt(t *testing.T) {
	sync, config, target := boostSync(t)
	target.data = strings.Replace(boostVaultTables, `"unit" = "npm test"`, `"unit" = "npm run unit"`, 1)
	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(config.data, "npm run unit") {
		t.Errorf("the differing table was overwritten:\n%s", config.data)
	}
	if !strings.Contains(report.String(), "[test_parts.lampas]: differs") {
		t.Errorf("the sync line does not say it differs: %q", report.String())
	}
}

func TestSyncOnTheHomeLeavesItsConfigAlone(t *testing.T) {
	sync, config, _ := boostSync(t)
	sync.Host = "laptop"
	if _, err := sync.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if config.data != boostConfigBefore {
		t.Errorf("the home's config was changed:\n%s", config.data)
	}
}

func TestSyncWithAConfigItCannotReadStillSucceedsAndSaysSo(t *testing.T) {
	sync, config, _ := boostSync(t)
	config.data = "this is [not toml"
	report, err := sync.Run(context.Background())
	if err != nil {
		t.Fatalf("a bad config failed the sync: %v", err)
	}
	if !strings.Contains(report.String(), "config.toml") {
		t.Errorf("nothing said of the config: %q", report.String())
	}
}

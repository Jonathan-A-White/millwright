package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/config"
)

// writeConfig puts a config file under a fresh home directory and makes it the
// home directory of this test.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the config directory: %v", err)
	}
	if contents != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(contents), 0o644); err != nil {
			t.Fatalf("writing the config file: %v", err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_HOST", "")
	t.Setenv("MW_CAP", "")
	t.Setenv("MW_STALE_HOURS", "")
	t.Setenv("MW_HOST_SILENT_HOURS", "")
	return home
}

// The VPS's own config file, as the README gives it, so that what the factory
// is told to write is what the factory can read.
const vpsConfig = `vault = "/root/millwright-vault"
host = "vps"
cap = 1

[rigs]
millwright = "/root/millwright"
`

func TestTheVPSConfigFileReadsBack(t *testing.T) {
	writeConfig(t, vpsConfig)

	dir, err := config.Vault()
	if err != nil || dir != "/root/millwright-vault" {
		t.Fatalf("expected the vault, got %q: %v", dir, err)
	}
	host, err := config.Host()
	if err != nil || host != "vps" {
		t.Fatalf("expected the host, got %q: %v", host, err)
	}
	atOnce, err := config.Cap()
	if err != nil || atOnce != 1 {
		t.Fatalf("expected a cap of 1, got %d: %v", atOnce, err)
	}
	rigs, err := config.Rigs()
	if err != nil {
		t.Fatalf("reading the rigs: %v", err)
	}
	if len(rigs) != 1 || rigs["millwright"] != "/root/millwright" {
		t.Fatalf("expected the millwright rig's checkout, got %+v", rigs)
	}
}

func TestCapIsOneUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	atOnce, err := config.Cap()
	if err != nil {
		t.Fatalf("reading the cap: %v", err)
	}
	if atOnce != config.DefaultCap {
		t.Fatalf("expected the default cap %d, got %d", config.DefaultCap, atOnce)
	}

	t.Setenv("MW_CAP", "3")
	if atOnce, err = config.Cap(); err != nil || atOnce != 3 {
		t.Fatalf("expected MW_CAP to win with 3, got %d: %v", atOnce, err)
	}
}

func TestCapRefusesWhatWouldStartNothingOrIsNotANumber(t *testing.T) {
	writeConfig(t, "cap = 0\n")
	if _, err := config.Cap(); err == nil {
		t.Fatal("expected a cap of 0 to be refused")
	}

	writeConfig(t, "cap = \"lots\"\n")
	if _, err := config.Cap(); err == nil {
		t.Fatal("expected a cap that is not a number to be refused")
	}
}

func TestStaleHoursIsTwoUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	hours, err := config.StaleHours()
	if err != nil {
		t.Fatalf("reading the stale threshold: %v", err)
	}
	if hours != config.DefaultStaleHours {
		t.Fatalf("expected the default of %d hours, got %d", config.DefaultStaleHours, hours)
	}

	t.Setenv("MW_STALE_HOURS", "6")
	if hours, err = config.StaleHours(); err != nil || hours != 6 {
		t.Fatalf("expected MW_STALE_HOURS to win with 6, got %d: %v", hours, err)
	}

	writeConfig(t, "stale_hours = 4\n")
	if hours, err = config.StaleHours(); err != nil || hours != 4 {
		t.Fatalf("expected the config file's stale_hours to read back as 4, got %d: %v", hours, err)
	}
}

func TestStaleHoursRefusesWhatWouldNeverGiveASessionAChanceOrIsNotANumber(t *testing.T) {
	writeConfig(t, "stale_hours = 0\n")
	if _, err := config.StaleHours(); err == nil {
		t.Fatal("expected a stale threshold of 0 to be refused")
	}

	writeConfig(t, "stale_hours = \"soon\"\n")
	if _, err := config.StaleHours(); err == nil {
		t.Fatal("expected a stale threshold that is not a number to be refused")
	}
}

func TestHostSilentHoursIsTwoUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	hours, err := config.HostSilentHours()
	if err != nil {
		t.Fatalf("reading how long a host may be silent: %v", err)
	}
	if hours != config.DefaultHostSilentHours {
		t.Fatalf("expected the default of %d hours, got %d", config.DefaultHostSilentHours, hours)
	}

	t.Setenv(config.HostSilenceEnv, "6")
	if hours, err = config.HostSilentHours(); err != nil || hours != 6 {
		t.Fatalf("expected %s to win with 6, got %d: %v", config.HostSilenceEnv, hours, err)
	}

	writeConfig(t, "host_silent_hours = 4\n")
	if hours, err = config.HostSilentHours(); err != nil || hours != 4 {
		t.Fatalf("expected the config file's host_silent_hours to read back as 4, got %d: %v", hours, err)
	}
}

func TestHostSilentHoursRefusesWhatWouldCallEveryHostAsleepOrIsNotANumber(t *testing.T) {
	writeConfig(t, "host_silent_hours = 0\n")
	if _, err := config.HostSilentHours(); err == nil {
		t.Fatal("expected a host silence threshold of 0 to be refused")
	}

	writeConfig(t, "host_silent_hours = \"a while\"\n")
	if _, err := config.HostSilentHours(); err == nil {
		t.Fatal("expected a host silence threshold that is not a number to be refused")
	}
}

func TestRigsAreEmptyWhenNoneAreCheckedOutAndMustBeFullPaths(t *testing.T) {
	writeConfig(t, "host = \"vps\"\n")
	rigs, err := config.Rigs()
	if err != nil {
		t.Fatalf("reading the rigs of a machine with none: %v", err)
	}
	if len(rigs) != 0 {
		t.Fatalf("expected no rigs, got %+v", rigs)
	}

	writeConfig(t, "[rigs]\nmillwright = \"../millwright\"\n")
	if _, err := config.Rigs(); err == nil {
		t.Fatal("expected a rig named by a relative path to be refused")
	}
}

func TestRigsReadOnlyTheirOwnTable(t *testing.T) {
	writeConfig(t, "[rigs]\nmillwright = \"/root/millwright\"  # the factory itself\n\n[elsewhere]\nfellowship = \"/root/fellowship\"\n")

	rigs, err := config.Rigs()
	if err != nil {
		t.Fatalf("reading the rigs: %v", err)
	}
	if len(rigs) != 1 || rigs["millwright"] != "/root/millwright" {
		t.Fatalf("expected only the rigs table to be read, got %+v", rigs)
	}
	if names := config.RigNames(rigs); len(names) != 1 || names[0] != "millwright" {
		t.Fatalf("expected the rig names, got %q", names)
	}
}

func TestVaultPrefersTheEnvironment(t *testing.T) {
	writeConfig(t, "vault = \"/from/the/file\"\n")
	t.Setenv("MW_VAULT", "/from/the/environment")

	got, err := config.Vault()
	if err != nil {
		t.Fatalf("finding the vault: %v", err)
	}
	if got != "/from/the/environment" {
		t.Fatalf("expected MW_VAULT to win, got %q", got)
	}
}

func TestVaultFallsBackToTheConfigFile(t *testing.T) {
	writeConfig(t, "# where the beads live\nvault = \"/root/millwright-vault\"  # the one database\n")

	got, err := config.Vault()
	if err != nil {
		t.Fatalf("finding the vault: %v", err)
	}
	if got != "/root/millwright-vault" {
		t.Fatalf("expected the vault from the config file, got %q", got)
	}
}

func TestVaultReadsSingleQuotesAndBareValues(t *testing.T) {
	for _, line := range []string{"vault = '/root/millwright-vault'", "vault=/root/millwright-vault"} {
		writeConfig(t, line+"\n")
		got, err := config.Vault()
		if err != nil {
			t.Fatalf("finding the vault from %q: %v", line, err)
		}
		if got != "/root/millwright-vault" {
			t.Fatalf("expected the vault from %q, got %q", line, got)
		}
	}
}

func TestVaultIgnoresKeysInsideATable(t *testing.T) {
	writeConfig(t, "[laptop]\nvault = \"/elsewhere\"\n")

	if _, err := config.Vault(); err == nil {
		t.Fatal("expected a vault set inside a table to be ignored")
	}
}

func TestHostIsWhichOfTheFactorysHostsThisMachineIs(t *testing.T) {
	writeConfig(t, "vault = \"/root/millwright-vault\"\nhost = \"vps\"\n")

	got, err := config.Host()
	if err != nil {
		t.Fatalf("finding the host: %v", err)
	}
	if got != "vps" {
		t.Fatalf("expected the host from the config file, got %q", got)
	}

	t.Setenv("MW_HOST", "laptop")
	got, err = config.Host()
	if err != nil {
		t.Fatalf("finding the host: %v", err)
	}
	if got != "laptop" {
		t.Fatalf("expected MW_HOST to win, got %q", got)
	}
}

func TestHostSaysHowToSetItWhenItIsNotSet(t *testing.T) {
	writeConfig(t, "vault = \"/root/millwright-vault\"\n")

	_, err := config.Host()
	if err == nil {
		t.Fatal("expected an error when the host is set nowhere")
	}
	for _, want := range []string{"MW_HOST", "config.toml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got %q", want, err.Error())
		}
	}
}

func TestVaultSaysHowToSetItWhenItIsNotSet(t *testing.T) {
	writeConfig(t, "")

	_, err := config.Vault()
	if err == nil {
		t.Fatal("expected an error when the vault is set nowhere")
	}
	for _, want := range []string{"MW_VAULT", "config.toml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got %q", want, err.Error())
		}
	}
}

func TestTestsSayHowEachRigIsChecked(t *testing.T) {
	writeConfig(t, `host = "vps"

[rigs]
millwright = "/root/millwright"

[tests]
millwright = "make test"
fellowship = "go test -tags integration ./..."
`)

	tests, err := config.Tests()
	if err != nil {
		t.Fatalf("reading how the rigs are tested: %v", err)
	}
	if len(tests) != 2 {
		t.Fatalf("expected both rigs, got %+v", tests)
	}
	if tests["millwright"] != "make test" || tests["fellowship"] != "go test -tags integration ./..." {
		t.Errorf("expected each rig's own command line, got %+v", tests)
	}
}

func TestAHostThatSaysNothingAboutTestsIsNotAnError(t *testing.T) {
	writeConfig(t, vpsConfig)

	tests, err := config.Tests()
	if err != nil {
		t.Fatalf("reading how the rigs are tested: %v", err)
	}
	if len(tests) != 0 {
		t.Errorf("expected no rig to say how it is tested, got %+v", tests)
	}
}

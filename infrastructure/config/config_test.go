package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	t.Setenv("MW_HANDOFF_AT", "")
	t.Setenv("MW_RIG_MEMORY_BYTES", "")
	t.Setenv("MW_MILLHAND_ROUTINE_MODEL", "")
	t.Setenv("MW_MILLHAND_REVIEW_MODEL", "")
	t.Setenv("MW_DISPATCH_SYNC_TRIES", "")
	t.Setenv("MW_DISPATCH_SYNC_WAIT", "")
	t.Setenv("MW_MAX_ATTEMPTS", "")
	t.Setenv("MW_NUDGE_AFTER_MINUTES", "")
	t.Setenv("MW_NUDGE_SYNC_STALE_MINUTES", "")
	t.Setenv("MW_POSTERN_BACKEND", "")
	t.Setenv("MW_POSTERN_FLOAT_SATS", "")
	t.Setenv("MW_POSTERN_GOVERNOR_KEY", "")
	t.Setenv("MW_POSTERN_KEY_FILE", "")
	t.Setenv("MW_POSTERN_SNAPSHOT_PATH", "")
	t.Setenv("MW_POSTERN_VIEW_PATH", "")
	t.Setenv("MW_BEADS_SYNC", "")
	t.Setenv("MW_BEADS_BACKUP_MINUTES", "")
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	t.Setenv("BEADS_DOLT_SERVER_PORT", "")
	t.Setenv("MW_POSTERN_CHANNEL", "")
	t.Setenv("MW_POSTERN_TRANSCRIBE_CMD", "")
	t.Setenv("MW_HANDS_ROOT_HELPER", "")
	t.Setenv("MW_GRIST_KEY_FILE", "")
	t.Setenv("MW_GRIST_STATE_DIR", "")
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

func TestTickRecheckSecondsIsThirtyUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	seconds, err := config.TickRecheckSeconds()
	if err != nil || seconds != config.DefaultTickRecheckSeconds {
		t.Fatalf("expected the default of %d seconds, got %d: %v", config.DefaultTickRecheckSeconds, seconds, err)
	}

	writeConfig(t, "tick_recheck_seconds = 10\n")
	if seconds, err = config.TickRecheckSeconds(); err != nil || seconds != 10 {
		t.Fatalf("expected the config file's tick_recheck_seconds to read back as 10, got %d: %v", seconds, err)
	}

	t.Setenv("MW_TICK_RECHECK_SECONDS", "0")
	if seconds, err = config.TickRecheckSeconds(); err != nil || seconds != 0 {
		t.Fatalf("expected MW_TICK_RECHECK_SECONDS to win with 0, got %d: %v", seconds, err)
	}

	t.Setenv("MW_TICK_RECHECK_SECONDS", "-1")
	if _, err = config.TickRecheckSeconds(); err == nil {
		t.Fatal("expected a negative wait to be refused")
	}
	t.Setenv("MW_TICK_RECHECK_SECONDS", "soon")
	if _, err = config.TickRecheckSeconds(); err == nil {
		t.Fatal("expected a wait that is not a number to be refused")
	}
}

func TestHandoffAtIs180000UntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	tokens, err := config.HandoffAt()
	if err != nil {
		t.Fatalf("reading the handoff limit: %v", err)
	}
	if config.DefaultHandoffAt != 180000 || tokens != config.DefaultHandoffAt {
		t.Fatalf("expected the default of 180000 tokens, got %d", tokens)
	}

	writeConfig(t, "handoff_at = 120000\n")
	if tokens, err = config.HandoffAt(); err != nil || tokens != 120000 {
		t.Fatalf("expected the config file's handoff_at to read back as 120000, got %d: %v", tokens, err)
	}

	t.Setenv("MW_HANDOFF_AT", "90000")
	if tokens, err = config.HandoffAt(); err != nil || tokens != 90000 {
		t.Fatalf("expected MW_HANDOFF_AT to win with 90000, got %d: %v", tokens, err)
	}
}

func TestHandoffAtRefusesWhatWouldHandEveryoneOffOrIsNotANumber(t *testing.T) {
	writeConfig(t, "handoff_at = 0\n")
	if _, err := config.HandoffAt(); err == nil {
		t.Fatal("expected a handoff limit of 0 to be refused")
	}

	writeConfig(t, "handoff_at = \"soon\"\n")
	if _, err := config.HandoffAt(); err == nil {
		t.Fatal("expected a handoff limit that is not a number to be refused")
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

func TestNudgeAfterMinutesIsSixtyUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	minutes, err := config.NudgeAfterMinutes()
	if err != nil {
		t.Fatalf("reading how long a story may run before the quiet alarm names it: %v", err)
	}
	if minutes != config.DefaultNudgeAfterMinutes {
		t.Fatalf("expected the default of %d minutes, got %d", config.DefaultNudgeAfterMinutes, minutes)
	}

	t.Setenv(config.NudgeAfterMinutesEnv, "90")
	if minutes, err = config.NudgeAfterMinutes(); err != nil || minutes != 90 {
		t.Fatalf("expected %s to win with 90, got %d: %v", config.NudgeAfterMinutesEnv, minutes, err)
	}

	writeConfig(t, "nudge_after_minutes = 45\n")
	if minutes, err = config.NudgeAfterMinutes(); err != nil || minutes != 45 {
		t.Fatalf("expected the config file's nudge_after_minutes to read back as 45, got %d: %v", minutes, err)
	}
}

func TestNudgeAfterMinutesRefusesWhatWouldNameEveryStoryAtOnceOrIsNotANumber(t *testing.T) {
	writeConfig(t, "nudge_after_minutes = 0\n")
	if _, err := config.NudgeAfterMinutes(); err == nil {
		t.Fatal("expected a story limit of 0 minutes to be refused")
	}

	writeConfig(t, "nudge_after_minutes = \"a while\"\n")
	if _, err := config.NudgeAfterMinutes(); err == nil {
		t.Fatal("expected a story limit that is not a number to be refused")
	}
}

func TestNudgeSyncStaleMinutesIsTwentyUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	minutes, err := config.NudgeSyncStaleMinutes()
	if err != nil {
		t.Fatalf("reading how stale another host's sync may be before the quiet alarm names it: %v", err)
	}
	if minutes != config.DefaultNudgeSyncStaleMinutes {
		t.Fatalf("expected the default of %d minutes, got %d", config.DefaultNudgeSyncStaleMinutes, minutes)
	}

	t.Setenv(config.NudgeSyncStaleMinutesEnv, "10")
	if minutes, err = config.NudgeSyncStaleMinutes(); err != nil || minutes != 10 {
		t.Fatalf("expected %s to win with 10, got %d: %v", config.NudgeSyncStaleMinutesEnv, minutes, err)
	}

	writeConfig(t, "nudge_sync_stale_minutes = 30\n")
	if minutes, err = config.NudgeSyncStaleMinutes(); err != nil || minutes != 30 {
		t.Fatalf("expected the config file's nudge_sync_stale_minutes to read back as 30, got %d: %v", minutes, err)
	}
}

func TestNudgeSyncStaleMinutesRefusesWhatWouldNameEveryHostAtOnceOrIsNotANumber(t *testing.T) {
	writeConfig(t, "nudge_sync_stale_minutes = 0\n")
	if _, err := config.NudgeSyncStaleMinutes(); err == nil {
		t.Fatal("expected a sync staleness limit of 0 minutes to be refused")
	}

	writeConfig(t, "nudge_sync_stale_minutes = \"a while\"\n")
	if _, err := config.NudgeSyncStaleMinutes(); err == nil {
		t.Fatal("expected a sync staleness limit that is not a number to be refused")
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

func TestAfterLandingSaysWhatEachRigRunsOnceALandingMovedIt(t *testing.T) {
	writeConfig(t, `host = "laptop"

[tests]
millwright = "make test"

[after_landing]
millwright = "make build"
`)

	after, err := config.AfterLanding()
	if err != nil {
		t.Fatalf("reading what runs after a landing: %v", err)
	}
	if len(after) != 1 || after["millwright"] != "make build" {
		t.Errorf("expected only the rig's own command line, not the tests', got %+v", after)
	}
}

func TestAfterLandingLimitSaysHowLongEachRigsCommandMayRun(t *testing.T) {
	writeConfig(t, `host = "laptop"

[after_landing]
postern = "deploy"

[after_landing_limit]
postern = "20m"
millwright = "90s"
`)

	limits, err := config.AfterLandingLimits()
	if err != nil {
		t.Fatalf("reading the after-landing limits: %v", err)
	}
	if len(limits) != 2 || limits["postern"] != 20*time.Minute || limits["millwright"] != 90*time.Second {
		t.Errorf("expected each rig's own limit, got %+v", limits)
	}
}

func TestAHostThatNamesNoAfterLandingLimitLeavesTheDefault(t *testing.T) {
	writeConfig(t, vpsConfig)

	limits, err := config.AfterLandingLimits()
	if err != nil {
		t.Fatalf("reading the after-landing limits: %v", err)
	}
	if len(limits) != 0 {
		t.Errorf("expected no limit, got %+v", limits)
	}
}

func TestAnAfterLandingLimitThatIsNotADurationIsRefusedByName(t *testing.T) {
	writeConfig(t, "[after_landing_limit]\npostern = \"soon\"\n")

	_, err := config.AfterLandingLimits()
	if err == nil || !strings.Contains(err.Error(), "postern") {
		t.Errorf("expected an error naming the rig whose limit is not a duration, got %v", err)
	}
}

func TestAHostThatSaysNothingAboutAfterALandingRunsNothing(t *testing.T) {
	writeConfig(t, vpsConfig)

	after, err := config.AfterLanding()
	if err != nil {
		t.Fatalf("reading what runs after a landing: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected no rig to have a command, got %+v", after)
	}
}

func TestRigMemoryBytesIsEightThousandUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	bytes, err := config.RigMemoryBytes()
	if err != nil {
		t.Fatalf("reading how large a rig's memory may be: %v", err)
	}
	if config.DefaultRigMemoryBytes != 8000 || bytes != config.DefaultRigMemoryBytes {
		t.Fatalf("expected the default of 8000 bytes, got %d", bytes)
	}

	t.Setenv(config.RigMemoryEnv, "500")
	if bytes, err = config.RigMemoryBytes(); err != nil || bytes != 500 {
		t.Fatalf("expected %s to win with 500, got %d: %v", config.RigMemoryEnv, bytes, err)
	}

	t.Setenv(config.RigMemoryEnv, "")
	writeConfig(t, "rig_memory_bytes = 6000\n")
	if bytes, err = config.RigMemoryBytes(); err != nil || bytes != 6000 {
		t.Fatalf("expected the config file's rig_memory_bytes to read back as 6000, got %d: %v", bytes, err)
	}
}

func TestRigMemoryBytesRefusesWhatWouldCallEveryRigOverBudgetOrIsNotANumber(t *testing.T) {
	writeConfig(t, "rig_memory_bytes = 0\n")
	if _, err := config.RigMemoryBytes(); err == nil {
		t.Fatal("expected a rig memory budget of 0 to be refused")
	}

	writeConfig(t, "rig_memory_bytes = \"plenty\"\n")
	if _, err := config.RigMemoryBytes(); err == nil {
		t.Fatal("expected a rig memory budget that is not a number to be refused")
	}
}

func TestBeadsBudgetBytesIsOneAndAHalfGigabytesUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	bytes, err := config.BeadsBudgetBytes()
	if err != nil {
		t.Fatalf("reading how large the beads database may be: %v", err)
	}
	if config.DefaultBeadsBudgetBytes != 1_500_000_000 || bytes != config.DefaultBeadsBudgetBytes {
		t.Fatalf("expected the default of 1.5 GB, got %d", bytes)
	}

	t.Setenv(config.BeadsBudgetEnv, "500")
	if bytes, err = config.BeadsBudgetBytes(); err != nil || bytes != 500 {
		t.Fatalf("expected %s to win with 500, got %d: %v", config.BeadsBudgetEnv, bytes, err)
	}

	t.Setenv(config.BeadsBudgetEnv, "")
	writeConfig(t, "beads_budget_bytes = 3000000000\n")
	if bytes, err = config.BeadsBudgetBytes(); err != nil || bytes != 3_000_000_000 {
		t.Fatalf("expected the config file's beads_budget_bytes to read back as 3000000000, got %d: %v", bytes, err)
	}
}

func TestBeadsBudgetBytesRefusesZeroOrANonNumberNamingTheKey(t *testing.T) {
	for _, text := range []string{"0", "\"plenty\"", "-5", "1.5"} {
		writeConfig(t, "beads_budget_bytes = "+text+"\n")
		_, err := config.BeadsBudgetBytes()
		if err == nil {
			t.Fatalf("expected a beads budget of %s to be refused", text)
		}
		if !strings.Contains(err.Error(), "beads_budget_bytes") {
			t.Fatalf("expected the error for %s to name beads_budget_bytes, got: %v", text, err)
		}
	}

	writeConfig(t, "")
	t.Setenv(config.BeadsBudgetEnv, "lots")
	if _, err := config.BeadsBudgetBytes(); err == nil || !strings.Contains(err.Error(), "beads_budget_bytes") {
		t.Fatalf("expected a bad %s to be refused naming beads_budget_bytes, got: %v", config.BeadsBudgetEnv, err)
	}
}

func TestMaxAttemptsIsThreeUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\nhost = \"vps\"\n")

	tries, err := config.MaxAttempts()
	if err != nil {
		t.Fatalf("reading how many times a story is tried: %v", err)
	}
	if config.DefaultMaxAttempts != 3 || tries != config.DefaultMaxAttempts {
		t.Fatalf("expected the default of 3 attempts, got %d", tries)
	}

	writeConfig(t, "max_attempts = 5\n")
	if tries, err = config.MaxAttempts(); err != nil || tries != 5 {
		t.Fatalf("expected the config file's max_attempts to read back as 5, got %d: %v", tries, err)
	}

	t.Setenv(config.MaxAttemptsEnv, "2")
	if tries, err = config.MaxAttempts(); err != nil || tries != 2 {
		t.Fatalf("expected %s to win with 2, got %d: %v", config.MaxAttemptsEnv, tries, err)
	}
}

func TestMaxAttemptsRefusesWhatWouldTryNothingOrIsNotANumber(t *testing.T) {
	writeConfig(t, "max_attempts = 0\n")
	if _, err := config.MaxAttempts(); err == nil {
		t.Fatal("expected a max_attempts of 0 to be refused")
	}

	writeConfig(t, "max_attempts = \"many\"\n")
	if _, err := config.MaxAttempts(); err == nil {
		t.Fatal("expected a max_attempts that is not a number to be refused")
	}
}

func TestTheMillhandsModelsAreSonnetForRoutineAndOpusForReviewUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\n")
	routine, err := config.MillhandRoutineModel()
	if err != nil || routine != "sonnet" {
		t.Errorf("expected sonnet for a routine wake, got %q, %v", routine, err)
	}
	review, err := config.MillhandReviewModel()
	if err != nil || review != "opus" {
		t.Errorf("expected opus for a review wake, got %q, %v", review, err)
	}
}

func TestTheMillhandsModelsAreReadFromTheEnvironmentAheadOfTheFile(t *testing.T) {
	writeConfig(t, "millhand_routine_model = \"haiku\"\nmillhand_review_model = 'fable'\n\n[rigs]\nmillhand_review_model = \"nothing\"\n")
	routine, err := config.MillhandRoutineModel()
	if err != nil || routine != "haiku" {
		t.Errorf("expected the file's haiku for a routine wake, got %q, %v", routine, err)
	}
	review, err := config.MillhandReviewModel()
	if err != nil || review != "fable" {
		t.Errorf("expected the file's fable for a review wake, got %q, %v", review, err)
	}

	t.Setenv("MW_MILLHAND_ROUTINE_MODEL", "opus")
	t.Setenv("MW_MILLHAND_REVIEW_MODEL", "sonnet")
	if routine, _ := config.MillhandRoutineModel(); routine != "opus" {
		t.Errorf("expected the environment to win for a routine wake, got %q", routine)
	}
	if review, _ := config.MillhandReviewModel(); review != "sonnet" {
		t.Errorf("expected the environment to win for a review wake, got %q", review)
	}
}

func TestAMachineWithNoWatchTableWatchesNothing(t *testing.T) {
	writeConfig(t, vpsConfig)

	watch, err := config.Watch()
	if err != nil {
		t.Fatalf("expected no table not to be an error, got %v", err)
	}
	if watch.SSH != "" || watch.Host != "" || len(watch.Outside) != 0 || watch.Blog != "" {
		t.Errorf("expected nothing to watch, got %+v", watch)
	}
}

func TestTheWatchTableReadsBack(t *testing.T) {
	writeConfig(t, vpsConfig+`
[watch]
ssh     = "vps-ssh"   # what ssh calls it
host    = "vps"
outside = ["https://one.example", 'https://two.example']
blog    = "https://blog.example"
`)

	watch, err := config.Watch()
	if err != nil {
		t.Fatalf("expected the table to read, got %v", err)
	}
	if watch.SSH != "vps-ssh" || watch.Host != "vps" || watch.Blog != "https://blog.example" {
		t.Errorf("expected the watch table read back, got %+v", watch)
	}
	if got := strings.Join(watch.Outside, " "); got != "https://one.example https://two.example" {
		t.Errorf("expected the two outside places, got %q", got)
	}
}

func TestTheWatchTableReadsOnlyItself(t *testing.T) {
	writeConfig(t, `[watch]
ssh = "vps-ssh"
host = "vps"
outside = "https://one.example, https://two.example"

[rigs]
ssh = "/not/this"
`)

	watch, err := config.Watch()
	if err != nil {
		t.Fatalf("expected the table to read, got %v", err)
	}
	if watch.SSH != "vps-ssh" || len(watch.Outside) != 2 || watch.Blog != "" {
		t.Errorf("expected only the watch table, with no blog, got %+v", watch)
	}
}

func TestAWatchTableThatCannotBeUsedSaysWhatIsMissing(t *testing.T) {
	writeConfig(t, "[watch]\nblog = \"https://blog.example\"\n")

	_, err := config.Watch()
	if err == nil || !strings.Contains(err.Error(), "ssh, host, outside") {
		t.Fatalf("expected the refusal to name what is missing, got %v", err)
	}
}

func TestDoctorUnitsIsTheShippedFourUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)

	units, err := config.DoctorUnits()
	if err != nil {
		t.Fatalf("reading the doctor's units: %v", err)
	}
	if strings.Join(units, ",") != strings.Join(config.DefaultDoctorUnits, ",") {
		t.Fatalf("expected the default units, got %+v", units)
	}

	writeConfig(t, "[doctor]\nunits = [\"mw-dispatch.service\", \"mw-doctor.service\"]\n")
	units, err = config.DoctorUnits()
	if err != nil {
		t.Fatalf("reading the doctor's units: %v", err)
	}
	if got := strings.Join(units, " "); got != "mw-dispatch.service mw-doctor.service" {
		t.Fatalf("expected the file's units, got %q", got)
	}
}

func TestDoctorStateDirIsUnderHomeUntilAHostSaysOtherwise(t *testing.T) {
	home := writeConfig(t, vpsConfig)

	dir, err := config.DoctorStateDir()
	if err != nil {
		t.Fatalf("reading the doctor's state dir: %v", err)
	}
	if want := filepath.Join(home, config.DefaultDoctorStateDir); dir != want {
		t.Fatalf("expected %q, got %q", want, dir)
	}

	writeConfig(t, "[doctor]\nstate_dir = \"/var/lib/mw-doctor\"\n")
	if dir, err = config.DoctorStateDir(); err != nil || dir != "/var/lib/mw-doctor" {
		t.Fatalf("expected the file's state_dir, got %q: %v", dir, err)
	}
}

func TestDoctorStateDirRefusesARelativePath(t *testing.T) {
	writeConfig(t, "[doctor]\nstate_dir = \"relative/path\"\n")
	if _, err := config.DoctorStateDir(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative state_dir to be refused, got %v", err)
	}
}

func TestDoctorReachIsTheShippedTwoUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)

	reach, err := config.DoctorReach()
	if err != nil {
		t.Fatalf("reading the doctor's reach: %v", err)
	}
	if strings.Join(reach, ",") != strings.Join(config.DefaultDoctorReach, ",") {
		t.Fatalf("expected the default reach, got %+v", reach)
	}

	writeConfig(t, "[doctor]\nreach = [\"example.com:443\"]\n")
	reach, err = config.DoctorReach()
	if err != nil {
		t.Fatalf("reading the doctor's reach: %v", err)
	}
	if got := strings.Join(reach, " "); got != "example.com:443" {
		t.Fatalf("expected the file's reach, got %q", got)
	}
}

func TestDoctorPowershellIsTheShippedPathUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)

	powershell, err := config.DoctorPowershell()
	if err != nil {
		t.Fatalf("reading the doctor's powershell path: %v", err)
	}
	if powershell != config.DefaultDoctorPowershell {
		t.Fatalf("expected the default powershell path, got %q", powershell)
	}

	writeConfig(t, "[doctor]\npowershell = \"/mnt/c/somewhere/powershell.exe\"\n")
	if powershell, err = config.DoctorPowershell(); err != nil || powershell != "/mnt/c/somewhere/powershell.exe" {
		t.Fatalf("expected the file's powershell path, got %q: %v", powershell, err)
	}
}

func TestDoctorTunnelSettingsAreTheShippedDefaultsUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "[watch]\nssh = \"vps-ssh\"\nhost = \"vps\"\noutside = [\"https://one.example\"]\n")

	unit, err := config.DoctorTunnelUnit()
	if err != nil || unit != config.DefaultDoctorTunnelUnit {
		t.Fatalf("expected the default tunnel unit, got %q: %v", unit, err)
	}
	probe, err := config.DoctorTunnelProbe()
	if err != nil || probe != config.DefaultDoctorTunnelProbe {
		t.Fatalf("expected the default tunnel probe, got %q: %v", probe, err)
	}
	host, err := config.DoctorTunnelHost()
	if err != nil || host != "vps" {
		t.Fatalf("expected the [watch] table's host, got %q: %v", host, err)
	}

	writeConfig(t, "[watch]\nssh = \"vps-ssh\"\nhost = \"vps\"\noutside = [\"https://one.example\"]\n\n"+
		"[doctor]\ntunnel_unit = \"other-tunnel.service\"\ntunnel_host = \"other-vps\"\ntunnel_probe = \"ss -ltn\"\n")

	if unit, err = config.DoctorTunnelUnit(); err != nil || unit != "other-tunnel.service" {
		t.Fatalf("expected the file's tunnel_unit, got %q: %v", unit, err)
	}
	if probe, err = config.DoctorTunnelProbe(); err != nil || probe != "ss -ltn" {
		t.Fatalf("expected the file's tunnel_probe, got %q: %v", probe, err)
	}
	if host, err = config.DoctorTunnelHost(); err != nil || host != "other-vps" {
		t.Fatalf("expected the file's tunnel_host, got %q: %v", host, err)
	}
}

func TestDoctorWgSettingsAreTheShippedDefaultsUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")

	hub, err := config.DoctorWgHub()
	if err != nil || hub != config.DefaultDoctorWgHub {
		t.Fatalf("expected the default wg hub, got %q: %v", hub, err)
	}
	unit, err := config.DoctorWgUnit()
	if err != nil || unit != config.DefaultDoctorWgUnit {
		t.Fatalf("expected the default wg unit, got %q: %v", unit, err)
	}

	writeConfig(t, "[doctor]\ndoctor_wg_hub = \"10.88.0.1:2222\"\ndoctor_wg_unit = \"other-wg.service\"\n")

	if hub, err = config.DoctorWgHub(); err != nil || hub != "10.88.0.1:2222" {
		t.Fatalf("expected the file's doctor_wg_hub, got %q: %v", hub, err)
	}
	if unit, err = config.DoctorWgUnit(); err != nil || unit != "other-wg.service" {
		t.Fatalf("expected the file's doctor_wg_unit, got %q: %v", unit, err)
	}
}

func TestDoctorTmpLeftoversBudgetIsTheShippedDefaultUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")

	budget, err := config.DoctorTmpLeftoversBudgetBytes()
	if err != nil || budget != config.DefaultDoctorTmpLeftoversBudgetBytes {
		t.Fatalf("expected the default tmp-leftovers budget, got %d: %v", budget, err)
	}

	writeConfig(t, "[doctor]\ntmp_leftovers_budget_bytes = 500000000\n")
	if budget, err = config.DoctorTmpLeftoversBudgetBytes(); err != nil || budget != 500_000_000 {
		t.Fatalf("expected the file's tmp_leftovers_budget_bytes, got %d: %v", budget, err)
	}
}

func TestDoctorTmpLeftoversBudgetRefusesANonNumberOrLessThanOne(t *testing.T) {
	writeConfig(t, "[doctor]\ntmp_leftovers_budget_bytes = \"a lot\"\n")
	if _, err := config.DoctorTmpLeftoversBudgetBytes(); err == nil || !strings.Contains(err.Error(), "whole number") {
		t.Fatalf("expected a non-number to be refused, got %v", err)
	}

	writeConfig(t, "[doctor]\ntmp_leftovers_budget_bytes = 0\n")
	if _, err := config.DoctorTmpLeftoversBudgetBytes(); err == nil || !strings.Contains(err.Error(), "over budget") {
		t.Fatalf("expected zero to be refused, got %v", err)
	}
}

func TestDoctorRootDiskBudgetIsZeroUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")

	budget, err := config.DoctorRootDiskBudgetBytes()
	if err != nil || budget != 0 {
		t.Fatalf("expected the default root-disk budget of 0 (inert), got %d: %v", budget, err)
	}

	writeConfig(t, "[doctor]\nroot_disk_budget_bytes = 280000000000\n")
	if budget, err = config.DoctorRootDiskBudgetBytes(); err != nil || budget != 280_000_000_000 {
		t.Fatalf("expected the file's root_disk_budget_bytes, got %d: %v", budget, err)
	}
}

func TestDoctorRootDiskBudgetRefusesANonNumberOrANegative(t *testing.T) {
	writeConfig(t, "[doctor]\nroot_disk_budget_bytes = \"a lot\"\n")
	if _, err := config.DoctorRootDiskBudgetBytes(); err == nil || !strings.Contains(err.Error(), "whole number") {
		t.Fatalf("expected a non-number to be refused, got %v", err)
	}

	writeConfig(t, "[doctor]\nroot_disk_budget_bytes = -1\n")
	if _, err := config.DoctorRootDiskBudgetBytes(); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("expected a negative to be refused, got %v", err)
	}
}

func TestDoctorRootDiskMarginIsTenGigabytesUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")

	margin, err := config.DoctorRootDiskMarginBytes()
	if err != nil || margin != 10_000_000_000 {
		t.Fatalf("expected the default root-disk margin of 10 GB, got %d: %v", margin, err)
	}

	writeConfig(t, "[doctor]\nroot_disk_margin_bytes = 5000000000\n")
	if margin, err = config.DoctorRootDiskMarginBytes(); err != nil || margin != 5_000_000_000 {
		t.Fatalf("expected the file's root_disk_margin_bytes, got %d: %v", margin, err)
	}
}

func TestDoctorRootDiskMarginRefusesANonNumberOrANegative(t *testing.T) {
	writeConfig(t, "[doctor]\nroot_disk_margin_bytes = \"plenty\"\n")
	if _, err := config.DoctorRootDiskMarginBytes(); err == nil || !strings.Contains(err.Error(), "whole number") {
		t.Fatalf("expected a non-number to be refused, got %v", err)
	}

	writeConfig(t, "[doctor]\nroot_disk_margin_bytes = -5\n")
	if _, err := config.DoctorRootDiskMarginBytes(); err == nil || !strings.Contains(err.Error(), "negative") {
		t.Fatalf("expected a negative to be refused, got %v", err)
	}
}

func TestDoctorTunnelHostWithNoWatchTableIsEmpty(t *testing.T) {
	writeConfig(t, vpsConfig)

	host, err := config.DoctorTunnelHost()
	if err != nil || host != "" {
		t.Fatalf("expected no host with no [watch] table to fall back to, got %q: %v", host, err)
	}
}

func TestDispatchWaitsThreeTriesFifteenSecondsApartUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")
	tries, err := config.DispatchSyncTries()
	if err != nil || tries != 3 {
		t.Fatalf("expected 3 tries, got %d: %v", tries, err)
	}
	wait, err := config.DispatchSyncWait()
	if err != nil || wait != 15*time.Second {
		t.Fatalf("expected a wait of 15s, got %s: %v", wait, err)
	}

	writeConfig(t, "dispatch_sync_tries = 4\ndispatch_sync_wait = \"20s\"\n")
	if tries, err = config.DispatchSyncTries(); err != nil || tries != 4 {
		t.Fatalf("expected the file's 4 tries, got %d: %v", tries, err)
	}
	if wait, err = config.DispatchSyncWait(); err != nil || wait != 20*time.Second {
		t.Fatalf("expected the file's wait of 20s, got %s: %v", wait, err)
	}

	t.Setenv("MW_DISPATCH_SYNC_TRIES", "2")
	t.Setenv("MW_DISPATCH_SYNC_WAIT", "5s")
	if tries, err = config.DispatchSyncTries(); err != nil || tries != 2 {
		t.Fatalf("expected the environment's 2 tries ahead of the file, got %d: %v", tries, err)
	}
	if wait, err = config.DispatchSyncWait(); err != nil || wait != 5*time.Second {
		t.Fatalf("expected the environment's wait of 5s ahead of the file, got %s: %v", wait, err)
	}
}

func TestDispatchSyncKnobsRefuseWhatIsNotANumberOrWouldRunPastTheUnit(t *testing.T) {
	for _, bad := range []struct{ file, want string }{
		{"dispatch_sync_tries = 0\n", "1 or more"},
		{"dispatch_sync_tries = many\n", "not a whole number"},
		{"dispatch_sync_wait = soon\n", "not a duration"},
		{"dispatch_sync_wait = \"-5s\"\n", "negative"},
		{"dispatch_sync_tries = 10\ndispatch_sync_wait = \"15s\"\n", "2 minutes"},
	} {
		writeConfig(t, bad.file)
		_, triesErr := config.DispatchSyncTries()
		_, waitErr := config.DispatchSyncWait()
		err := triesErr
		if err == nil {
			err = waitErr
		}
		if err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("expected %q to be refused, saying %q, got %v", bad.file, bad.want, err)
		}
	}
}

func TestDispatchSyncWaitOfZeroIsAllowedSoATestNeverSleeps(t *testing.T) {
	writeConfig(t, "dispatch_sync_wait = \"0s\"\n")
	wait, err := config.DispatchSyncWait()
	if err != nil || wait != 0 {
		t.Fatalf("expected a wait of 0, got %s: %v", wait, err)
	}
}

func TestPushTriesThreeTwentySecondsApartUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")
	tries, err := config.PushTries()
	if err != nil || tries != 3 {
		t.Fatalf("expected 3 tries, got %d: %v", tries, err)
	}
	wait, err := config.PushWaitSeconds()
	if err != nil || wait != 20 {
		t.Fatalf("expected a wait of 20 seconds, got %d: %v", wait, err)
	}

	writeConfig(t, "push_tries = 5\npush_wait_seconds = 30\n")
	if tries, err = config.PushTries(); err != nil || tries != 5 {
		t.Fatalf("expected the file's 5 tries, got %d: %v", tries, err)
	}
	if wait, err = config.PushWaitSeconds(); err != nil || wait != 30 {
		t.Fatalf("expected the file's wait of 30s, got %d: %v", wait, err)
	}

	t.Setenv("MW_PUSH_TRIES", "2")
	t.Setenv("MW_PUSH_WAIT_SECONDS", "0")
	if tries, err = config.PushTries(); err != nil || tries != 2 {
		t.Fatalf("expected the environment's 2 tries ahead of the file, got %d: %v", tries, err)
	}
	if wait, err = config.PushWaitSeconds(); err != nil || wait != 0 {
		t.Fatalf("expected the environment's wait of 0 ahead of the file, got %d: %v", wait, err)
	}
}

func TestPushKnobsRefuseWhatIsNotANumber(t *testing.T) {
	for _, bad := range []struct{ file, want string }{
		{"push_tries = 0\n", "1 or more"},
		{"push_tries = many\n", "not a whole number"},
		{"push_wait_seconds = soon\n", "not a whole number"},
		{"push_wait_seconds = -5\n", "negative"},
	} {
		writeConfig(t, bad.file)
		_, triesErr := config.PushTries()
		_, waitErr := config.PushWaitSeconds()
		err := triesErr
		if err == nil {
			err = waitErr
		}
		if err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("expected %q to be refused, saying %q, got %v", bad.file, bad.want, err)
		}
	}
}

func TestPosternBackendIsTheDesktopUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)

	backend, err := config.PosternBackend()
	if err != nil {
		t.Fatalf("reading the postern backend: %v", err)
	}
	if config.DefaultPosternBackend != "http://desktop.mw:8787" || backend != config.DefaultPosternBackend {
		t.Fatalf("expected the default backend, got %q", backend)
	}

	t.Setenv(config.PosternBackendEnv, "http://elsewhere:1234")
	if backend, err = config.PosternBackend(); err != nil || backend != "http://elsewhere:1234" {
		t.Fatalf("expected %s to win, got %q: %v", config.PosternBackendEnv, backend, err)
	}

	t.Setenv(config.PosternBackendEnv, "")
	writeConfig(t, "postern_backend = \"http://file:8787\"\n")
	if backend, err = config.PosternBackend(); err != nil || backend != "http://file:8787" {
		t.Fatalf("expected the config file's postern_backend to read back, got %q: %v", backend, err)
	}
}

func TestPosternFloatSatsIsOneHundredThousandUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)

	sats, err := config.PosternFloatSats()
	if err != nil {
		t.Fatalf("reading the postern float cap: %v", err)
	}
	if config.DefaultPosternFloatSats != 100000 || sats != config.DefaultPosternFloatSats {
		t.Fatalf("expected the default of 100000 satoshis, got %d", sats)
	}

	t.Setenv(config.PosternFloatSatsEnv, "5000")
	if sats, err = config.PosternFloatSats(); err != nil || sats != 5000 {
		t.Fatalf("expected %s to win with 5000, got %d: %v", config.PosternFloatSatsEnv, sats, err)
	}

	t.Setenv(config.PosternFloatSatsEnv, "")
	writeConfig(t, "postern_float_sats = 2000\n")
	if sats, err = config.PosternFloatSats(); err != nil || sats != 2000 {
		t.Fatalf("expected the config file's postern_float_sats to read back as 2000, got %d: %v", sats, err)
	}
}

func TestPosternFloatSatsRefusesWhatIsNegativeOrIsNotANumber(t *testing.T) {
	writeConfig(t, "postern_float_sats = -1\n")
	if _, err := config.PosternFloatSats(); err == nil {
		t.Fatal("expected a negative postern float cap to be refused")
	}

	writeConfig(t, "postern_float_sats = \"plenty\"\n")
	if _, err := config.PosternFloatSats(); err == nil {
		t.Fatal("expected a postern float cap that is not a number to be refused")
	}
}

func TestPosternGovernorKeyIsEmptyUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)

	key, err := config.PosternGovernorKey()
	if err != nil {
		t.Fatalf("reading the postern governor key: %v", err)
	}
	if key != "" {
		t.Fatalf("expected no postern governor key by default, got %q", key)
	}

	t.Setenv(config.PosternGovernorKeyEnv, "02abc")
	if key, err = config.PosternGovernorKey(); err != nil || key != "02abc" {
		t.Fatalf("expected %s to win, got %q: %v", config.PosternGovernorKeyEnv, key, err)
	}

	t.Setenv(config.PosternGovernorKeyEnv, "")
	writeConfig(t, "postern_governor_key = \"03def\"\n")
	if key, err = config.PosternGovernorKey(); err != nil || key != "03def" {
		t.Fatalf("expected the config file's postern_governor_key to read back, got %q: %v", key, err)
	}
}

func TestPosternKeyFileIsUnderHomeUntilAHostSaysOtherwise(t *testing.T) {
	home := writeConfig(t, vpsConfig)

	path, err := config.PosternKeyFile()
	if err != nil {
		t.Fatalf("reading the postern key file: %v", err)
	}
	if want := filepath.Join(home, config.DefaultPosternKeyFile); path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}

	writeConfig(t, "postern_key_file = \"/etc/mw/postern.key\"\n")
	if path, err = config.PosternKeyFile(); err != nil || path != "/etc/mw/postern.key" {
		t.Fatalf("expected the file's postern_key_file, got %q: %v", path, err)
	}
}

func TestPosternKeyFileRefusesARelativePath(t *testing.T) {
	writeConfig(t, "postern_key_file = \"relative/path\"\n")
	if _, err := config.PosternKeyFile(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative postern_key_file to be refused, got %v", err)
	}
}

func TestPosternSnapshotPathIsUnderHomeUntilAHostSaysOtherwise(t *testing.T) {
	home := writeConfig(t, vpsConfig)

	path, err := config.PosternSnapshotPath()
	if err != nil {
		t.Fatalf("reading the postern snapshot path: %v", err)
	}
	if want := filepath.Join(home, config.DefaultPosternSnapshotPath); path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}

	t.Setenv(config.PosternSnapshotPathEnv, "/tmp/snapshot.bin")
	if path, err = config.PosternSnapshotPath(); err != nil || path != "/tmp/snapshot.bin" {
		t.Fatalf("expected %s to win, got %q: %v", config.PosternSnapshotPathEnv, path, err)
	}

	t.Setenv(config.PosternSnapshotPathEnv, "")
	writeConfig(t, "postern_snapshot_path = \"/etc/mw/snapshot.bin\"\n")
	if path, err = config.PosternSnapshotPath(); err != nil || path != "/etc/mw/snapshot.bin" {
		t.Fatalf("expected the file's postern_snapshot_path, got %q: %v", path, err)
	}
}

func TestPosternSnapshotPathRefusesARelativePath(t *testing.T) {
	writeConfig(t, "postern_snapshot_path = \"relative/path\"\n")
	if _, err := config.PosternSnapshotPath(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative postern_snapshot_path to be refused, got %v", err)
	}
}

func TestBeadsSyncIsRemoteUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)
	mode, err := config.BeadsSync()
	if err != nil || mode != config.BeadsSyncRemote {
		t.Fatalf("expected %q by default, got %q: %v", config.BeadsSyncRemote, mode, err)
	}

	writeConfig(t, "beads_sync = \"backup\"\n")
	if mode, err = config.BeadsSync(); err != nil || mode != config.BeadsSyncBackup {
		t.Fatalf("expected the file's backup, got %q: %v", mode, err)
	}

	t.Setenv(config.BeadsSyncEnv, "shared")
	if mode, err = config.BeadsSync(); err != nil || mode != config.BeadsSyncShared {
		t.Fatalf("expected %s's shared ahead of the file, got %q: %v", config.BeadsSyncEnv, mode, err)
	}
}

func TestBeadsSyncKnowsAutoAndTheServerHostAndBackupMinutesAreSaidOrNot(t *testing.T) {
	t.Setenv(config.BeadsServerHostEnv, "")
	t.Setenv(config.BeadsBackupMinutesEnv, "")
	writeConfig(t, "beads_sync = \"auto\"\n")
	if mode, err := config.BeadsSync(); err != nil || mode != config.BeadsSyncAuto {
		t.Fatalf("expected auto, got %q: %v", mode, err)
	}
	if host, err := config.BeadsServerHost(); err != nil || host != "" {
		t.Fatalf("expected no server host said, got %q: %v", host, err)
	}
	if _, said, err := config.BeadsBackupMinutesSaid(); err != nil || said {
		t.Fatalf("expected no backup minutes said, got said=%v: %v", said, err)
	}

	writeConfig(t, "beads_sync = \"auto\"\nbeads_server_host = \"10.88.0.2\"\nbeads_backup_minutes = 7\n")
	if host, err := config.BeadsServerHost(); err != nil || host != "10.88.0.2" {
		t.Fatalf("expected the file's server host, got %q: %v", host, err)
	}
	if minutes, said, err := config.BeadsBackupMinutesSaid(); err != nil || !said || minutes != 7 {
		t.Fatalf("expected 7 said, got %d %v: %v", minutes, said, err)
	}
}

func TestBeadsSyncRefusesAnythingButItsThreeChoicesNamingThem(t *testing.T) {
	writeConfig(t, "beads_sync = \"server\"\n")
	_, err := config.BeadsSync()
	if err == nil {
		t.Fatal("expected beads_sync = server to be refused")
	}
	for _, want := range []string{`"server"`, "remote", "backup", "shared", "beads_sync"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to say %q, got %q", want, err)
		}
	}
}

func TestBeadsBackupMinutesIsThirtyUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)
	minutes, err := config.BeadsBackupMinutes()
	if err != nil || minutes != 30 || config.DefaultBeadsBackupMinutes != 30 {
		t.Fatalf("expected 30 minutes by default, got %d: %v", minutes, err)
	}

	writeConfig(t, "beads_backup_minutes = 60\n")
	if minutes, err = config.BeadsBackupMinutes(); err != nil || minutes != 60 {
		t.Fatalf("expected the file's 60, got %d: %v", minutes, err)
	}

	t.Setenv(config.BeadsBackupMinutesEnv, "15")
	if minutes, err = config.BeadsBackupMinutes(); err != nil || minutes != 15 {
		t.Fatalf("expected %s's 15 ahead of the file, got %d: %v", config.BeadsBackupMinutesEnv, minutes, err)
	}
}

func TestBeadsBackupMinutesRefusesWhatIsNotANumberOrLessThanOne(t *testing.T) {
	for _, bad := range []struct{ file, want string }{
		{"beads_backup_minutes = 0\n", "1 or more"},
		{"beads_backup_minutes = often\n", "not a whole number"},
	} {
		writeConfig(t, bad.file)
		if _, err := config.BeadsBackupMinutes(); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("expected %q to be refused, saying %q, got %v", bad.file, bad.want, err)
		}
	}
}

func TestBeadsServerAddressIsBdsOwnHostAndPort(t *testing.T) {
	writeConfig(t, vpsConfig)
	if addr, err := config.BeadsServerAddress(); err != nil || addr != "" {
		t.Fatalf("expected no address with no %s, got %q: %v", config.BeadsDoltServerHostEnv, addr, err)
	}

	t.Setenv(config.BeadsDoltServerHostEnv, "10.88.0.3")
	if addr, err := config.BeadsServerAddress(); err != nil || addr != "10.88.0.3:"+config.DefaultBeadsDoltServerPort {
		t.Fatalf("expected the host on bd's default port, got %q: %v", addr, err)
	}

	t.Setenv(config.BeadsDoltServerPortEnv, "3306")
	if addr, err := config.BeadsServerAddress(); err != nil || addr != "10.88.0.3:3306" {
		t.Fatalf("expected 10.88.0.3:3306, got %q: %v", addr, err)
	}

	t.Setenv(config.BeadsDoltServerPortEnv, "a port")
	if _, err := config.BeadsServerAddress(); err == nil || !strings.Contains(err.Error(), config.BeadsDoltServerPortEnv) {
		t.Fatalf("expected a port that is not a number to be refused naming %s, got %v", config.BeadsDoltServerPortEnv, err)
	}
}

func TestPosternViewPathIsUnderHomeUntilAHostSaysOtherwise(t *testing.T) {
	home := writeConfig(t, vpsConfig)

	path, err := config.PosternViewPath()
	if err != nil {
		t.Fatalf("reading the postern view path: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "postern", "view.b64"); path != want || path != filepath.Join(home, config.DefaultPosternViewPath) {
		t.Fatalf("expected %q, got %q", want, path)
	}

	t.Setenv(config.PosternViewPathEnv, "/tmp/view.b64")
	if path, err = config.PosternViewPath(); err != nil || path != "/tmp/view.b64" {
		t.Fatalf("expected %s to win, got %q: %v", config.PosternViewPathEnv, path, err)
	}

	t.Setenv(config.PosternViewPathEnv, "")
	writeConfig(t, "postern_view_path = \"/srv/postern/view.b64\"\n")
	if path, err = config.PosternViewPath(); err != nil || path != "/srv/postern/view.b64" {
		t.Fatalf("expected the file's postern_view_path, got %q: %v", path, err)
	}

	writeConfig(t, "postern_view_path = \"relative/view.b64\"\n")
	if _, err := config.PosternViewPath(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative postern_view_path to be refused, got %v", err)
	}
}

func TestPosternChannelIsChainUntilAHostSaysDirect(t *testing.T) {
	writeConfig(t, vpsConfig)

	channel, err := config.PosternChannel()
	if err != nil {
		t.Fatalf("reading the postern channel: %v", err)
	}
	if channel != "chain" || config.DefaultPosternChannel != "chain" {
		t.Fatalf("expected the chain channel by default, got %q", channel)
	}

	t.Setenv(config.PosternChannelEnv, "direct")
	if channel, err = config.PosternChannel(); err != nil || channel != "direct" {
		t.Fatalf("expected %s to win with direct, got %q: %v", config.PosternChannelEnv, channel, err)
	}

	t.Setenv(config.PosternChannelEnv, "")
	writeConfig(t, "postern_channel = \"Direct\"\n")
	if channel, err = config.PosternChannel(); err != nil || channel != "direct" {
		t.Fatalf("expected the file's postern_channel read as direct, got %q: %v", channel, err)
	}

	writeConfig(t, "postern_channel = \"pigeon\"\n")
	if _, err := config.PosternChannel(); err == nil || !strings.Contains(err.Error(), "pigeon") {
		t.Fatalf("expected an unknown postern_channel to be refused, naming it, got %v", err)
	}
}

func TestPosternTranscribeCmdIsEmptyUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)

	cmd, err := config.PosternTranscribeCmd()
	if err != nil || cmd != "" {
		t.Fatalf("expected no transcriber by default, got %q: %v", cmd, err)
	}

	t.Setenv(config.PosternTranscribeCmdEnv, "/usr/local/bin/postern-transcribe")
	if cmd, err = config.PosternTranscribeCmd(); err != nil || cmd != "/usr/local/bin/postern-transcribe" {
		t.Fatalf("expected %s to win, got %q: %v", config.PosternTranscribeCmdEnv, cmd, err)
	}

	t.Setenv(config.PosternTranscribeCmdEnv, "")
	writeConfig(t, "postern_transcribe_cmd = \"postern-transcribe --quiet\"\n")
	if cmd, err = config.PosternTranscribeCmd(); err != nil || cmd != "postern-transcribe --quiet" {
		t.Fatalf("expected the file's postern_transcribe_cmd, got %q: %v", cmd, err)
	}
}

func TestHandsHostsIsTheHandsHostsTable(t *testing.T) {
	writeConfig(t, vpsConfig)
	hosts, err := config.HandsHosts()
	if err != nil || len(hosts) != 0 {
		t.Fatalf("expected no hands hosts by default, got %v: %v", hosts, err)
	}

	writeConfig(t, "host = \"desktop\"\n\n[hands_hosts]\nlaptop = \"ssh laptop\"\nvps = \"ssh root@allmymind.org\"  # the VPS\n")
	hosts, err = config.HandsHosts()
	if err != nil {
		t.Fatalf("reading the hands hosts: %v", err)
	}
	if len(hosts) != 2 || hosts["laptop"] != "ssh laptop" || hosts["vps"] != "ssh root@allmymind.org" {
		t.Fatalf("expected the laptop and the vps, got %v", hosts)
	}
}

func TestHandsRootHelperIsTheInstalledPathUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)
	path, err := config.HandsRootHelper()
	if err != nil || path != "/usr/local/sbin/mw-hands-root" || config.DefaultHandsRootHelper != "/usr/local/sbin/mw-hands-root" {
		t.Fatalf("expected /usr/local/sbin/mw-hands-root, got %q: %v", path, err)
	}

	writeConfig(t, "hands_root_helper = \"/opt/mw/mw-hands-root\"\n")
	if path, err = config.HandsRootHelper(); err != nil || path != "/opt/mw/mw-hands-root" {
		t.Fatalf("expected the file's hands_root_helper, got %q: %v", path, err)
	}

	writeConfig(t, "hands_root_helper = \"mw-hands-root\"\n")
	if _, err := config.HandsRootHelper(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative hands_root_helper refused, got %v", err)
	}
}

func TestPosternDataIsAFullPathAHostMustSay(t *testing.T) {
	t.Setenv(config.PosternDataEnv, "")
	writeConfig(t, "")
	if _, err := config.PosternData(); err == nil || !strings.Contains(err.Error(), "postern_data") {
		t.Fatalf("expected a missing postern_data to be refused, naming it, got %v", err)
	}

	writeConfig(t, "postern_data = \"/srv/postern/\"\n")
	if path, err := config.PosternData(); err != nil || path != "/srv/postern" {
		t.Fatalf("expected the file's postern_data without its trailing slash, got %q: %v", path, err)
	}

	t.Setenv(config.PosternDataEnv, "/tmp/data")
	if path, err := config.PosternData(); err != nil || path != "/tmp/data" {
		t.Fatalf("expected %s to win, got %q: %v", config.PosternDataEnv, path, err)
	}

	t.Setenv(config.PosternDataEnv, "relative")
	if _, err := config.PosternData(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative postern_data to be refused, got %v", err)
	}
}

func TestPosternLocalURLIsThisHostsOwnNameUnlessSaid(t *testing.T) {
	t.Setenv(config.PosternLocalURLEnv, "")
	writeConfig(t, "postern_backend = \"http://desktop.mw:8787\"\n")
	if url, err := config.PosternLocalURL("laptop"); err != nil || url != "http://laptop.mw:8787" {
		t.Fatalf("expected this host's own name, not postern_backend, got %q: %v", url, err)
	}
	writeConfig(t, "postern_local_url = \"http://10.88.0.2:8787/\"\n")
	if url, err := config.PosternLocalURL("laptop"); err != nil || url != "http://10.88.0.2:8787" {
		t.Fatalf("expected the file's url without its trailing slash, got %q: %v", url, err)
	}
	t.Setenv(config.PosternLocalURLEnv, "http://127.0.0.1:9000")
	if url, err := config.PosternLocalURL("laptop"); err != nil || url != "http://127.0.0.1:9000" {
		t.Fatalf("expected %s to win, got %q: %v", config.PosternLocalURLEnv, url, err)
	}
}

func TestHomeMoveBeadIsTheEpicUnlessSaid(t *testing.T) {
	t.Setenv(config.HomeMoveBeadEnv, "")
	writeConfig(t, "")
	if bead, err := config.HomeMoveBead(); err != nil || bead != config.DefaultHomeMoveBead {
		t.Fatalf("expected %s, got %q: %v", config.DefaultHomeMoveBead, bead, err)
	}
	writeConfig(t, "home_move_bead = \"mw-demo\"\n")
	if bead, err := config.HomeMoveBead(); err != nil || bead != "mw-demo" {
		t.Fatalf("expected the file's bead, got %q: %v", bead, err)
	}
	t.Setenv(config.HomeMoveBeadEnv, "mw-env")
	if bead, err := config.HomeMoveBead(); err != nil || bead != "mw-env" {
		t.Fatalf("expected %s to win, got %q: %v", config.HomeMoveBeadEnv, bead, err)
	}
}

func TestPosternWatchdogTargetIsEmptyUntilAHostSaysOne(t *testing.T) {
	t.Setenv(config.PosternWatchdogEnv, "")
	writeConfig(t, "")
	if target, err := config.PosternWatchdogTarget(); err != nil || target != "" {
		t.Fatalf("expected no target, got %q: %v", target, err)
	}
	writeConfig(t, "postern_watchdog_target = \"root@vps:/var/lib/postern-watchdog/\"\n")
	if target, err := config.PosternWatchdogTarget(); err != nil || target != "root@vps:/var/lib/postern-watchdog/" {
		t.Fatalf("expected the file's target, got %q: %v", target, err)
	}
}

// The mill key and the mill's state live under the home directory unless
// config or the environment says otherwise, and a path said is a full one.
func TestGristKeyFileAndStateDirDefaultUnderTheHome(t *testing.T) {
	home := writeConfig(t, "")
	key, err := config.GristKeyFile()
	if err != nil || key != filepath.Join(home, ".config", "mw", "mill.key") {
		t.Fatalf("expected ~/.config/mw/mill.key, got %q %v", key, err)
	}
	state, err := config.GristStateDir()
	if err != nil || state != filepath.Join(home, ".local", "state", "mw", "grist") {
		t.Fatalf("expected ~/.local/state/mw/grist, got %q %v", state, err)
	}
}

func TestGristKeyFileAndStateDirAreRead(t *testing.T) {
	writeConfig(t, "grist_key_file = \"/keys/mill.key\"\ngrist_state_dir = \"/state/grist\"\n")
	if key, err := config.GristKeyFile(); err != nil || key != "/keys/mill.key" {
		t.Fatalf("expected the config's key file, got %q %v", key, err)
	}
	if state, err := config.GristStateDir(); err != nil || state != "/state/grist" {
		t.Fatalf("expected the config's state dir, got %q %v", state, err)
	}
	t.Setenv("MW_GRIST_KEY_FILE", "/env/mill.key")
	if key, _ := config.GristKeyFile(); key != "/env/mill.key" {
		t.Fatalf("expected the environment ahead of the file, got %q", key)
	}
}

func TestGristKeyFileMustBeAFullPath(t *testing.T) {
	writeConfig(t, "grist_key_file = \"mill.key\"\n")
	if _, err := config.GristKeyFile(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative key file refused, got %v", err)
	}
}

// The mill key is never the Mayor's postern key: named the same, or through a link.
func TestGristKeyFileRefusesTheMayorsPosternKey(t *testing.T) {
	home := writeConfig(t, "")
	t.Setenv("MW_POSTERN_KEY_FILE", "")
	t.Setenv("MW_GRIST_KEY_FILE", filepath.Join(home, ".config", "mw", "postern.key"))
	if _, err := config.GristKeyFile(); err == nil || !strings.Contains(err.Error(), "Mayor's postern key") {
		t.Fatalf("expected the Mayor's key refused, got %v", err)
	}
	if err := os.Symlink(filepath.Join(home, ".config", "mw", "postern.key"), filepath.Join(home, "link.key")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MW_GRIST_KEY_FILE", filepath.Join(home, "link.key"))
	if _, err := config.GristKeyFile(); err == nil {
		t.Fatal("expected a link to the Mayor's key refused")
	}
	t.Setenv("MW_GRIST_KEY_FILE", filepath.Join(home, "elsewhere", "mill.key"))
	if _, err := config.GristKeyFile(); err != nil {
		t.Fatalf("expected a key of its own allowed, got %v", err)
	}
}

// With no [grist] table the ceilings are the defaults.
func TestGristCeilingsDefault(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	got, err := config.Grist()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Models, ",") != "haiku,sonnet,opus" || strings.Join(got.Efforts, ",") != "low,medium,high" || got.MaxAttachments != 4 || got.MaxAttachmentBytes != 8388608 ||
		got.DailyLimit != 50 || got.Concurrency != 2 || got.Timeout != 10*time.Minute {
		t.Fatalf("expected the default ceilings, got %+v", got)
	}
}

func TestGristCeilingsAreReadFromTheTable(t *testing.T) {
	writeConfig(t, `host = "laptop"

[grist]
models = ["haiku", "sonnet"]
efforts = "low,xhigh"
max_attachments = 2
max_attachment_bytes = 4194304
daily_limit = 20
concurrency = 3
timeout = "5m"

[grist-apps]
cairn = "/home/jwhite/rigs/Cairn"
`)
	got, err := config.Grist()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Models, ",") != "haiku,sonnet" || strings.Join(got.Efforts, ",") != "low,xhigh" || got.MaxAttachments != 2 || got.MaxAttachmentBytes != 4194304 ||
		got.DailyLimit != 20 || got.Concurrency != 3 || got.Timeout != 5*time.Minute {
		t.Fatalf("expected the table's ceilings, got %+v", got)
	}
	apps, err := config.GristApps()
	if err != nil || len(apps) != 1 || apps["cairn"] != "/home/jwhite/rigs/Cairn" {
		t.Fatalf("expected cairn's checkout, got %v %v", apps, err)
	}
}

func TestGristCeilingsThatAreNotNumbersAreRefused(t *testing.T) {
	for _, bad := range []string{"daily_limit = 0", "concurrency = 0", "concurrency = two", "max_attachments = four", `timeout = "soon"`} {
		writeConfig(t, "[grist]\n"+bad+"\n")
		if _, err := config.Grist(); err == nil {
			t.Errorf("expected %q refused", bad)
		}
	}
}

func TestGristAppsMustBeFullPaths(t *testing.T) {
	writeConfig(t, "[grist-apps]\ncairn = \"rigs/Cairn\"\n")
	if _, err := config.GristApps(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative checkout refused, got %v", err)
	}
	writeConfig(t, "")
	if apps, err := config.GristApps(); err != nil || len(apps) != 0 {
		t.Fatalf("expected no apps and no error, got %v %v", apps, err)
	}
}

// A host runs the mill only where the config file has a [grist] table, an
// empty one included; [grist-apps] alone is not it.
func TestGristConfiguredIsATableInTheConfigFile(t *testing.T) {
	for name, text := range map[string]string{
		"no file":       "",
		"no table":      "host = \"laptop\"\n",
		"only the apps": "[grist-apps]\ncairn = \"/rigs/cairn\"\n",
		"a comment":     "# [grist]\n",
	} {
		writeConfig(t, text)
		if got, err := config.GristConfigured(); err != nil || got {
			t.Errorf("%s: expected [grist] not configured, got %v %v", name, got, err)
		}
	}
	for name, text := range map[string]string{
		"empty":   "host = \"laptop\"\n[grist]\n",
		"filled":  "[grist]\nmodels = \"sonnet\"\n[grist-apps]\n",
		"spaced":  "  [ grist ]  \n",
		"trailer": "[grist]",
	} {
		writeConfig(t, text)
		if got, err := config.GristConfigured(); err != nil || !got {
			t.Errorf("%s: expected [grist] configured, got %v %v", name, got, err)
		}
	}
}

func TestBackendsReadsEachRigsBackendTable(t *testing.T) {
	writeConfig(t, `host = "laptop"

[rigs]
postern = "/home/j/postern"
millwright = "/home/j/millwright"

[backend.postern]
build = "go build -o {out} ./cmd/postern"
stage = "/home/j/.local/share/postern"
live = "/home/j/.local/bin/postern"
service = "postern-backend"
health = "https://postern.example.org/api/healthz"
check = "/home/j/.local/bin/mw postern inbox --unread-count"
`)

	backends, err := config.Backends()
	if err != nil {
		t.Fatalf("reading the backends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("expected only postern to have a backend, got %+v", backends)
	}
	got := backends["postern"]
	if got.Dir != "server" || got.Build != "go build -o {out} ./cmd/postern" || got.Service != "postern-backend" ||
		got.Live != "/home/j/.local/bin/postern" || got.Stage != "/home/j/.local/share/postern" ||
		got.Health != "https://postern.example.org/api/healthz" || got.Check != "/home/j/.local/bin/mw postern inbox --unread-count" {
		t.Errorf("expected the table read back with dir defaulted to server, got %+v", got)
	}
}

func TestABackendSwapsItselfUnlessItsTableSaysHands(t *testing.T) {
	base := `[rigs]
postern = "/home/j/postern"

[backend.postern]
build = "go build -o {out} ./cmd/postern"
stage = "/s"
live = "/l"
service = "u"
health = "http://h"
`
	for swap, want := range map[string]string{"": "auto", `swap = "auto"`: "auto", `swap = "hands"`: "hands"} {
		writeConfig(t, base+swap+"\n")
		backends, err := config.Backends()
		if err != nil || backends["postern"].Swap != want {
			t.Errorf("swap %q: expected %q, got %+v, %v", swap, want, backends["postern"], err)
		}
	}
	writeConfig(t, base+`swap = "sometimes"`+"\n")
	if _, err := config.Backends(); err == nil || !strings.Contains(err.Error(), "swap") {
		t.Errorf("expected a swap other than auto or hands refused, got %v", err)
	}
}

func TestAHostThatSaysNothingAboutBackendsHasNone(t *testing.T) {
	writeConfig(t, vpsConfig)
	backends, err := config.Backends()
	if err != nil || len(backends) != 0 {
		t.Fatalf("expected no backend, got %+v, %v", backends, err)
	}
}

func TestAHalfSetBackendTableIsRefusedNamingWhatIsMissing(t *testing.T) {
	base := `[rigs]
postern = "/home/j/postern"

[backend.postern]
build = "go build -o {out} ./cmd/postern"
stage = "/s"
live = "/l"
service = "u"
health = "http://h"
`
	for name, c := range map[string]struct{ from, to, want string }{
		"no service":    {"service = \"u\"\n", "", "no service"},
		"no health":     {"health = \"http://h\"\n", "", "no health"},
		"no {out}":      {"{out}", "x", "{out}"},
		"relative live": {"live = \"/l\"", "live = \"bin/l\"", "full path"},
	} {
		writeConfig(t, strings.Replace(base, c.from, c.to, 1))
		if _, err := config.Backends(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: expected a refusal naming %q, got %v", name, c.want, err)
		}
	}
}

func TestEventsLogPathIsUnderHomeUntilAHostSaysOtherwise(t *testing.T) {
	home := writeConfig(t, vpsConfig)
	t.Setenv(config.EventsLogPathEnv, "")
	path, err := config.EventsLogPath()
	if err != nil {
		t.Fatalf("reading the event log's path: %v", err)
	}
	if want := filepath.Join(home, ".local", "state", "mw", "events", "log.jsonl"); path != want {
		t.Fatalf("expected %q, got %q", want, path)
	}
	t.Setenv(config.EventsLogPathEnv, "/tmp/events/log.jsonl")
	if path, err = config.EventsLogPath(); err != nil || path != "/tmp/events/log.jsonl" {
		t.Fatalf("expected %s to win, got %q: %v", config.EventsLogPathEnv, path, err)
	}
	t.Setenv(config.EventsLogPathEnv, "")
	writeConfig(t, "events_log_path = \"relative/log.jsonl\"\n")
	if _, err := config.EventsLogPath(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative events_log_path to be refused, got %v", err)
	}
}

// With no [events] table the follower sends on chain, up to 500 records a day.
func TestEventsKnobsDefault(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	got, err := config.Events()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Chain || got.ChainDailyCap != 500 || got.EmergencyDailyCap != 20 {
		t.Fatalf("expected chain on, a cap of 500 and an emergency allowance of 20, got %+v", got)
	}
}

func TestEventsKnobsAreReadFromTheTable(t *testing.T) {
	writeConfig(t, "[events]\nchain = false\nchain_daily_cap = 20\n")
	got, err := config.Events()
	if err != nil {
		t.Fatal(err)
	}
	if got.Chain || got.ChainDailyCap != 20 {
		t.Fatalf("expected chain off and a cap of 20, got %+v", got)
	}
	writeConfig(t, "[events]\nemergency_daily_cap = 5\n")
	if got, err = config.Events(); err != nil || got.EmergencyDailyCap != 5 {
		t.Fatalf("expected an emergency allowance of 5, got %+v %v", got, err)
	}
	writeConfig(t, "[events]\nemergency_daily_cap = 0\n")
	if _, err = config.Events(); err == nil || !strings.Contains(err.Error(), "emergency_daily_cap") {
		t.Fatalf("expected an emergency_daily_cap of 0 to be refused, got %v", err)
	}
	writeConfig(t, "[events]\nchain = \"true\"\n")
	if got, err = config.Events(); err != nil || !got.Chain {
		t.Fatalf("expected chain on from the quoted word, got %+v %v", got, err)
	}
}

// An idle factory's jobs are sprung by events with an hour's heartbeat, the
// sync job runs on a clock of five minutes, and the factory is called idle
// after ten minutes with no event but its jobs'.
func TestEventsHeartbeatKnobsDefaultAndAreReadFromTheTable(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	got, err := config.Events()
	if err != nil {
		t.Fatal(err)
	}
	if got.Heartbeat != time.Hour || got.Clock != 5*time.Minute || got.IdleAfter != 10*time.Minute {
		t.Fatalf("expected an hour, 5m and 10m, got %v %v %v", got.Heartbeat, got.Clock, got.IdleAfter)
	}
	writeConfig(t, "[events]\nheartbeat = \"30m\"\nclock = 1m\nidle_after = \"20m\"\n")
	if got, err = config.Events(); err != nil {
		t.Fatal(err)
	}
	if got.Heartbeat != 30*time.Minute || got.Clock != time.Minute || got.IdleAfter != 20*time.Minute {
		t.Fatalf("expected 30m, 1m and 20m, got %v %v %v", got.Heartbeat, got.Clock, got.IdleAfter)
	}
}

func TestEventsHeartbeatKnobsThatAreNotUnderstoodAreRefused(t *testing.T) {
	for _, bad := range []string{"heartbeat = soon", "heartbeat = 0s", "heartbeat = -5m", "clock = 0", "idle_after = lots"} {
		writeConfig(t, "[events]\n"+bad+"\n")
		if _, err := config.Events(); err == nil {
			t.Errorf("expected %q refused", bad)
		}
	}
}

func TestEventsKnobsThatAreNotUnderstoodAreRefused(t *testing.T) {
	for _, bad := range []string{"chain = maybe", "chain_daily_cap = 0", "chain_daily_cap = lots"} {
		writeConfig(t, "[events]\n"+bad+"\n")
		if _, err := config.Events(); err == nil {
			t.Errorf("expected %q refused", bad)
		}
	}
}

func TestDoctorMayorStaleMinutesIsTheShippedDefaultUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")
	if minutes, err := config.DoctorMayorStaleMinutes(); err != nil || minutes != config.DefaultDoctorMayorStaleMinutes {
		t.Fatalf("expected the default, got %d: %v", minutes, err)
	}

	writeConfig(t, "[doctor]\nmayor_stale_minutes = 25\n")
	if minutes, err := config.DoctorMayorStaleMinutes(); err != nil || minutes != 25 {
		t.Fatalf("expected the file's mayor_stale_minutes, got %d: %v", minutes, err)
	}

	writeConfig(t, "[doctor]\nmayor_stale_minutes = 0\n")
	if _, err := config.DoctorMayorStaleMinutes(); err == nil {
		t.Fatalf("expected zero to be refused")
	}
	writeConfig(t, "[doctor]\nmayor_stale_minutes = \"soon\"\n")
	if _, err := config.DoctorMayorStaleMinutes(); err == nil {
		t.Fatalf("expected a non-number to be refused")
	}
}

func TestDoctorBatteryThresholdsAreTheShippedDefaultsUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, "")
	if low, critical, err := config.DoctorBatteryThresholds(); err != nil || low != 25 || critical != 10 {
		t.Fatalf("expected 25 and 10, got %d and %d: %v", low, critical, err)
	}

	writeConfig(t, "[doctor]\nbattery_low_percent = 30\nbattery_critical_percent = 15\n")
	if low, critical, err := config.DoctorBatteryThresholds(); err != nil || low != 30 || critical != 15 {
		t.Fatalf("expected the file's 30 and 15, got %d and %d: %v", low, critical, err)
	}

	for _, bad := range []string{
		"battery_low_percent = 0",
		"battery_low_percent = \"soon\"",
		"battery_critical_percent = 100",
		"battery_low_percent = 10\nbattery_critical_percent = 10",
	} {
		writeConfig(t, "[doctor]\n"+bad+"\n")
		if _, _, err := config.DoctorBatteryThresholds(); err == nil {
			t.Fatalf("expected %q to be refused", bad)
		}
	}
}

func TestMeteredIsAutoUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)
	t.Setenv(config.MeteredEnv, "")
	if got, err := config.Metered(); err != nil || got != "auto" {
		t.Fatalf("metered = %q, %v; want auto", got, err)
	}
	writeConfig(t, "metered = \"yes\"\n")
	if got, err := config.Metered(); err != nil || got != "yes" {
		t.Fatalf("metered = %q, %v; want yes", got, err)
	}
	t.Setenv(config.MeteredEnv, "No")
	if got, err := config.Metered(); err != nil || got != "no" {
		t.Fatalf("MW_METERED=No gave %q, %v; want no", got, err)
	}
	t.Setenv(config.MeteredEnv, "maybe")
	if _, err := config.Metered(); err == nil || !strings.Contains(err.Error(), "auto, yes or no") {
		t.Fatalf("metered = maybe gave %v, want a refusal naming auto, yes and no", err)
	}
}

func TestHeavyNetNamesTheRigsThatInstallDependencies(t *testing.T) {
	writeConfig(t, "[heavy_net]\npostern = true\ncairn = true\nmillwright = false\n")
	heavy, err := config.HeavyNet()
	if err != nil {
		t.Fatalf("reading heavy_net: %v", err)
	}
	if !heavy["postern"] || !heavy["cairn"] || heavy["millwright"] || heavy["argus"] {
		t.Fatalf("heavy_net read as %+v", heavy)
	}

	writeConfig(t, vpsConfig)
	if heavy, err := config.HeavyNet(); err != nil || len(heavy) != 0 {
		t.Fatalf("no table read as %+v, %v; want none and no error", heavy, err)
	}

	writeConfig(t, "[heavy_net]\npostern = sometimes\n")
	if _, err := config.HeavyNet(); err == nil || !strings.Contains(err.Error(), "postern") {
		t.Fatalf("a value that is not a bool gave %v, want an error naming the rig", err)
	}
}

func TestABackendTableMayNameTheVPSStandbyAndMustSetItWhole(t *testing.T) {
	base := `[rigs]
postern = "/home/j/postern"

[backend.postern]
build = "go build -o {out} ./cmd/postern"
stage = "/s"
live = "/l"
service = "u"
health = "http://h"
vps_host = "vps"
vps_stage = "/var/lib/postern"
vps_live = "/usr/local/bin/postern"
vps_service = "postern"
vps_health = "http://vps.mw:8787/healthz"
`
	writeConfig(t, base)
	backends, err := config.Backends()
	got := backends["postern"]
	if err != nil || got.VPSHost != "vps" || got.VPSStage != "/var/lib/postern" || got.VPSLive != "/usr/local/bin/postern" ||
		got.VPSService != "postern" || got.VPSHealth != "http://vps.mw:8787/healthz" {
		t.Fatalf("expected the standby read back, got %+v, %v", got, err)
	}
	for name, c := range map[string]struct{ from, to, want string }{
		"no vps_service": {"vps_service = \"postern\"\n", "", "no vps_service"},
		"no vps_health":  {"vps_health = \"http://vps.mw:8787/healthz\"\n", "", "no vps_health"},
		"relative stage": {"vps_stage = \"/var/lib/postern\"", "vps_stage = \"stage\"", "full path"},
	} {
		writeConfig(t, strings.Replace(base, c.from, c.to, 1))
		if _, err := config.Backends(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: expected a refusal naming %q, got %v", name, c.want, err)
		}
	}
}

func TestScorersDefaultToTheLocalEngineAtItsPort(t *testing.T) {
	writeConfig(t, "")
	s, err := config.Scorers()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Engines, ",") != "local" || s.LocalURL != "http://127.0.0.1:8765" || s.AzureKeyFile != "" || s.AzureRegion != "" {
		t.Fatalf("unexpected defaults %+v", s)
	}
}

func TestScorersAreReadFromTheirTable(t *testing.T) {
	writeConfig(t, "vault = \"/v\"\n\n[scorers]\nengines = [\"local\", \"azure\"]\nlocal_url = \"http://10.0.0.5:9000/\"\nazure_key_file = \"/keys/azure.key\"\nazure_region = \"westus2\"\n")
	s, err := config.Scorers()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Engines, ",") != "local,azure" || s.LocalURL != "http://10.0.0.5:9000" ||
		s.AzureKeyFile != "/keys/azure.key" || s.AzureRegion != "westus2" {
		t.Fatalf("unexpected settings %+v", s)
	}
}

func TestScorersAzureKeyFileMustBeAFullPath(t *testing.T) {
	writeConfig(t, "[scorers]\nazure_key_file = \"azure.key\"\n")
	if _, err := config.Scorers(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a refusal naming a full path, got %v", err)
	}
}

func TestScorersLocalURLMustBeAnHTTPAddress(t *testing.T) {
	writeConfig(t, "[scorers]\nlocal_url = \"127.0.0.1:8765\"\n")
	if _, err := config.Scorers(); err == nil || !strings.Contains(err.Error(), "local_url") {
		t.Fatalf("expected a refusal naming local_url, got %v", err)
	}
}

func TestBuilderNiceIsTenUntilAHostSaysOtherwise(t *testing.T) {
	writeConfig(t, vpsConfig)
	t.Setenv(config.BuilderNiceEnv, "")
	if got, err := config.BuilderNice(); err != nil || got != 10 {
		t.Fatalf("builder_nice = %d, %v; want 10", got, err)
	}
	writeConfig(t, "builder_nice = 5\n")
	if got, err := config.BuilderNice(); err != nil || got != 5 {
		t.Fatalf("builder_nice = %d, %v; want 5", got, err)
	}
	writeConfig(t, "builder_nice = 0\n")
	if got, err := config.BuilderNice(); err != nil || got != 0 {
		t.Fatalf("builder_nice = 0 gave %d, %v; want 0 (off)", got, err)
	}
	t.Setenv(config.BuilderNiceEnv, "15")
	if got, err := config.BuilderNice(); err != nil || got != 15 {
		t.Fatalf("MW_BUILDER_NICE=15 gave %d, %v; want 15", got, err)
	}
}

func TestBuilderNiceRefusesWhatNiceCannotTake(t *testing.T) {
	t.Setenv(config.BuilderNiceEnv, "")
	for _, bad := range []string{"-5", "20", "lots", "1.5"} {
		writeConfig(t, "builder_nice = "+bad+"\n")
		if _, err := config.BuilderNice(); err == nil || !strings.Contains(err.Error(), "builder_nice") {
			t.Errorf("builder_nice = %s gave %v, want a refusal naming builder_nice", bad, err)
		}
	}
}

func TestAgeKeyFileDefaultsUnderTheHomeAndIsRead(t *testing.T) {
	home := writeConfig(t, "")
	t.Setenv("MW_AGE_KEY_FILE", "")
	if key, err := config.AgeKeyFile(); err != nil || key != filepath.Join(home, ".config", "mw", "age.key") {
		t.Fatalf("expected ~/.config/mw/age.key, got %q %v", key, err)
	}
	writeConfig(t, "age_key_file = \"/keys/age.key\"\n")
	if key, err := config.AgeKeyFile(); err != nil || key != "/keys/age.key" {
		t.Fatalf("expected the config's key file, got %q %v", key, err)
	}
	t.Setenv("MW_AGE_KEY_FILE", "/env/age.key")
	if key, _ := config.AgeKeyFile(); key != "/env/age.key" {
		t.Fatalf("expected the environment ahead of the file, got %q", key)
	}
}

func TestAgeKeyFileMustBeAFullPath(t *testing.T) {
	writeConfig(t, "age_key_file = \"age.key\"\n")
	t.Setenv("MW_AGE_KEY_FILE", "")
	if _, err := config.AgeKeyFile(); err == nil || !strings.Contains(err.Error(), "full path") {
		t.Fatalf("expected a relative key file refused, got %v", err)
	}
}

func TestTesterReadsTheTrialAndRefusesAHalfSetTable(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n\n[tester]\nrigs = [\"lampas\"]\nuntil = \"2026-10-16T20:00:00Z\"  # a week from the landing\n")
	tester, err := config.Tester()
	if err != nil {
		t.Fatalf("reading [tester]: %v", err)
	}
	want := time.Date(2026, 10, 16, 20, 0, 0, 0, time.UTC)
	if len(tester.Rigs) != 1 || tester.Rigs[0] != "lampas" || !tester.Until.Equal(want) ||
		tester.Model != config.DefaultTesterModel || tester.Effort != config.DefaultTesterEffort {
		t.Errorf("expected lampas until %s on sonnet/high, got %+v", want, tester)
	}

	writeConfig(t, "host = \"laptop\"\n")
	if tester, err := config.Tester(); err != nil || len(tester.Rigs) != 0 || !tester.Until.IsZero() {
		t.Errorf("expected no table to be no trial, got %+v, %v", tester, err)
	}

	for _, table := range []string{
		"[tester]\nuntil = \"2026-10-16T20:00:00Z\"\n",
		"[tester]\nrigs = [\"lampas\"]\n",
		"[tester]\nrigs = [\"lampas\"]\nuntil = \"next friday\"\n",
	} {
		writeConfig(t, table)
		if _, err := config.Tester(); err == nil {
			t.Errorf("expected %q to be refused", table)
		}
	}
}

// With no [dispatch] table a host needs a core's worth of load and 2 GB.
func TestRoomDefaults(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	got, err := config.Room()
	if err != nil {
		t.Fatal(err)
	}
	if got.LoadPerCore != 1.0 || got.MinFreeMB != 2048 {
		t.Fatalf("expected 1.0 a core and 2048 MB, got %+v", got)
	}
}

func TestRoomIsReadFromTheTable(t *testing.T) {
	writeConfig(t, "[dispatch]\nroom_load_per_core = 0.75\nroom_min_free_mb = 4096\n")
	got, err := config.Room()
	if err != nil {
		t.Fatal(err)
	}
	if got.LoadPerCore != 0.75 || got.MinFreeMB != 4096 {
		t.Fatalf("expected 0.75 a core and 4096 MB, got %+v", got)
	}
	for _, bad := range []string{"room_load_per_core = 0\n", "room_load_per_core = lots\n", "room_min_free_mb = 0\n", "room_min_free_mb = 1.5\n"} {
		writeConfig(t, "[dispatch]\n"+bad)
		if _, err := config.Room(); err == nil || !strings.Contains(err.Error(), "room_") {
			t.Errorf("expected %q to be refused naming the key, got %v", bad, err)
		}
	}
}

func TestGristRoomIsReadFromTheDispatchTable(t *testing.T) {
	writeConfig(t, "host = \"laptop\"\n")
	got, err := config.Room()
	if err != nil {
		t.Fatal(err)
	}
	if got.GristCap != 0 || got.GristRecentSeconds != 600 || got.GristSlowFactor != 1.5 {
		t.Fatalf("expected the cap left to the host's less 2, 600 s and 1.5, got %+v", got)
	}
	writeConfig(t, "[dispatch]\ngrist_cap = 3\ngrist_recent_s = 300\ngrist_slow_factor = 2\n")
	if got, err = config.Room(); err != nil || got.GristCap != 3 || got.GristRecentSeconds != 300 || got.GristSlowFactor != 2 {
		t.Fatalf("expected 3, 300 s and 2, got %+v, %v", got, err)
	}
	for _, bad := range []string{"grist_cap = 0\n", "grist_cap = many\n", "grist_recent_s = -5\n", "grist_slow_factor = 1\n", "grist_slow_factor = fast\n"} {
		writeConfig(t, "[dispatch]\n"+bad)
		if _, err := config.Room(); err == nil || !strings.Contains(err.Error(), "grist_") {
			t.Errorf("expected %q to be refused naming the key, got %v", bad, err)
		}
	}
}

func TestCloudReadsTheTableWithItsDefaultsAndRefusesABadOne(t *testing.T) {
	writeConfig(t, "host = \"desktop\"\n\n[rigs]\nmillwright = \"/home/m/millwright\"\n\n[cloud]\nsnapshot = \"snap-1\"  # made by contrib/vultr-boost snapshot\nmonthly_cap_usd = 40.5\n\n[cloud.caps]\ndesktop = 2\nlaptop = 1\n")
	cloud, ok, err := config.Cloud()
	if err != nil || !ok {
		t.Fatalf("reading [cloud]: %v, %v", ok, err)
	}
	if cloud.Provider != "vultr" || cloud.MaxBoxes != 2 || cloud.MonthlyCapUSD != 40.5 || cloud.IdleMinutes != 30 ||
		cloud.HourlyUSD != config.DefaultCloudHourlyUSD || cloud.BoxCap != 2 || cloud.Snapshot != "snap-1" ||
		cloud.Command != "/home/m/millwright/contrib/vultr-boost" || cloud.HostCaps["desktop"] != 2 || cloud.HostCaps["laptop"] != 1 {
		t.Errorf("expected the defaults beside what the table says, got %+v", cloud)
	}

	writeConfig(t, "host = \"desktop\"\n")
	if _, ok, err := config.Cloud(); ok || err != nil {
		t.Errorf("expected no table to be no cloud, got %v, %v", ok, err)
	}

	for _, table := range []string{
		"[cloud]\nprovider = \"aws\"\ncommand = \"/x/vultr-boost\"\n",
		"[cloud]\nmonthly_cap_usd = 0\ncommand = \"/x/vultr-boost\"\n",
		"[cloud]\nidle_minutes = \"soon\"\ncommand = \"/x/vultr-boost\"\n",
		"[cloud]\nmax_boxes = -1\ncommand = \"/x/vultr-boost\"\n",
		"[cloud]\ncommand = \"contrib/vultr-boost\"\n",
		"[cloud]\nmax_boxes = 1\n",
	} {
		writeConfig(t, table)
		if _, _, err := config.Cloud(); err == nil {
			t.Errorf("expected %q to be refused", table)
		}
	}
}

func TestBenchmarkIsReadFromTheTableAndLeavesTheRestZero(t *testing.T) {
	writeConfig(t, "host = \"desktop\"\n")
	if got, err := config.Benchmark(); err != nil || got != (config.BenchmarkSettings{}) {
		t.Fatalf("expected zero settings, which read as the defaults, got %+v, %v", got, err)
	}

	writeConfig(t, "[benchmark]\nusual_gates = 7\npar_window = 12\npar_min = 3\ncalibration_window = 40\nerror_flag_percent = 25.5\nover_par_factor = 3\n")
	got, err := config.Benchmark()
	if err != nil {
		t.Fatal(err)
	}
	want := config.BenchmarkSettings{UsualGates: 7, ParWindow: 12, ParMin: 3, CalibrationWindow: 40, ErrorFlagPercent: 25.5, OverParFactor: 3}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}

	for _, bad := range []string{"usual_gates = 0\n", "par_window = many\n", "par_min = 1.5\n", "calibration_window = -1\n", "error_flag_percent = 0\n", "over_par_factor = lots\n"} {
		writeConfig(t, "[benchmark]\n"+bad)
		key, _, _ := strings.Cut(bad, " ")
		if _, err := config.Benchmark(); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("expected %q to be refused naming the key, got %v", bad, err)
		}
	}
}

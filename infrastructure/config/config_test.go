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

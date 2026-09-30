package homemove

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ranOnOld puts a stand-in ssh first on PATH that runs the command it is asked to
// run on "the old home", which is this machine, with stand-ins for what it calls.
// scripts are those stand-ins by name; MW_VAULT names a vault in a temp dir.
func ranOnOld(t *testing.T, scripts map[string]string) (vault, dir string) {
	t.Helper()
	scripts["ssh"] = `for last; do :; done; eval "$last"`
	dir = onPath(t, scripts)
	vault = t.TempDir()
	t.Setenv("MW_VAULT", vault)
	t.Setenv("HOME", t.TempDir())
	return vault, dir
}

func writeActing(t *testing.T, vault, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(vault, ".mayor-acting"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

var oldHome = []string{"ssh", "desktop"}

func TestMailMayorRunsMwMailSendOnTheOldHomeSignedAsTheMove(t *testing.T) {
	_, dir := ranOnOld(t, map[string]string{"mw": `echo "seat=$MW_SEAT" >> "$0.seat"`})

	err := Host{}.MailMayor(context.Background(), oldHome, "mw@laptop", "Hand off now: the home moves to laptop", "it's time; don't wait")

	if err != nil {
		t.Fatalf("mail: %v", err)
	}
	if got := strings.TrimSpace(logOf(t, dir, "mw")); got != "mail send mayor -s Hand off now: the home moves to laptop -m it's time; don't wait" {
		t.Errorf("mw was run as %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "mw.seat")); strings.TrimSpace(string(got)) != "seat=mw@laptop" {
		t.Errorf("mail was signed %q", got)
	}
}

func TestSyncAndMirrorRunMwOnTheOldHome(t *testing.T) {
	_, dir := ranOnOld(t, map[string]string{"mw": "exit 0"})

	if err := (Host{}).Sync(context.Background(), oldHome); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := (Host{}).Mirror(context.Background(), oldHome); err != nil {
		t.Fatalf("mirror: %v", err)
	}

	if got := logOf(t, dir, "mw"); got != "sync\npostern mirror\n" {
		t.Errorf("mw was run as %q", got)
	}
}

func TestOldBeadsCountRunsBDCountInTheOldHomesVault(t *testing.T) {
	vault, dir := ranOnOld(t, map[string]string{"bd": `echo 10; pwd >> "$(dirname "$0")/bd.pwd"`})

	count, err := Host{}.OldBeadsCount(context.Background(), oldHome)

	if err != nil || count != 10 {
		t.Fatalf("got %d, %v", count, err)
	}
	if got := strings.TrimSpace(logOf(t, dir, "bd")); got != "count" {
		t.Errorf("bd was run as %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "bd.pwd")); strings.TrimSpace(string(got)) != vault {
		t.Errorf("bd ran in %q, want the vault %s", got, vault)
	}
}

func TestOldBeadsCountThatIsNotANumberIsAnError(t *testing.T) {
	ranOnOld(t, map[string]string{"bd": "echo 'no database'"})
	if _, err := (Host{}).OldBeadsCount(context.Background(), oldHome); err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Fatalf("expected an error, got %v", err)
	}
}

func TestACommandThatFailsOnTheOldHomeSaysWhatItSaid(t *testing.T) {
	ranOnOld(t, map[string]string{"mw": "echo 'mw: the sync halt marker is set' >&2; exit 1"})

	err := Host{}.Sync(context.Background(), oldHome)

	if err == nil || !strings.Contains(err.Error(), "sync halt marker") {
		t.Fatalf("expected the failure with what mw said, got %v", err)
	}
}

func TestAnSSHThatCannotReachTheOldHomeIsAnError(t *testing.T) {
	onPath(t, map[string]string{"ssh": "echo 'ssh: connect to host desktop port 22: No route to host' >&2; exit 255"})

	err := Host{}.Sync(context.Background(), oldHome)

	if err == nil || !strings.Contains(err.Error(), "did not run the command") || !strings.Contains(err.Error(), "No route") {
		t.Fatalf("expected ssh's failure, got %v", err)
	}
}

func TestMayorGoneWhenActingIsEmptyOrMissing(t *testing.T) {
	for name, acting := range map[string]*string{"empty": ptr(" \n"), "no file": nil} {
		t.Run(name, func(t *testing.T) {
			vault, _ := ranOnOld(t, map[string]string{"pgrep": "exit 0"})
			if acting != nil {
				writeActing(t, vault, *acting)
			}
			gone, said, err := Host{Poll: time.Millisecond}.MayorGone(context.Background(), oldHome, time.Second)
			if err != nil || !gone || !strings.Contains(said, ".mayor-acting is empty") {
				t.Errorf("expected a Mayor gone, got %v, %q, %v", gone, said, err)
			}
		})
	}
}

func ptr(s string) *string { return &s }

func TestMayorGoneWhenNoMayorProcessRuns(t *testing.T) {
	vault, dir := ranOnOld(t, map[string]string{"pgrep": "exit 1"})
	writeActing(t, vault, "Mayor after handoff 98 (tmux window @5)\n")

	gone, said, err := Host{Poll: time.Millisecond}.MayorGone(context.Background(), oldHome, time.Second)

	if err != nil || !gone || !strings.Contains(said, "no Mayor process runs") {
		t.Fatalf("expected a Mayor gone, got %v, %q, %v", gone, said, err)
	}
	if got := strings.TrimSpace(logOf(t, dir, "pgrep")); got != `-f -- -n Mayor \(afte[r]` {
		t.Errorf("pgrep was run as %q", got)
	}
}

func TestMayorStillThereIsNotGoneWhenWaitIsUp(t *testing.T) {
	vault, dir := ranOnOld(t, map[string]string{"pgrep": "exit 0"})
	writeActing(t, vault, "Mayor after handoff 98 (tmux window @5)\n")

	// The wait is long beside one ask of the old home, so a loaded box that is
	// slow to run the stand-in still gets to ask it again before the wait is up.
	gone, said, err := Host{Poll: 5 * time.Millisecond}.MayorGone(context.Background(), oldHome, time.Second)

	if err != nil || gone {
		t.Fatalf("expected a Mayor still there, got %v, %v", gone, err)
	}
	if !strings.Contains(said, "Mayor after handoff 98") || !strings.Contains(said, "process is running") {
		t.Errorf("expected what it saw, got %q", said)
	}
	if n := strings.Count(logOf(t, dir, "pgrep"), "\n"); n < 2 {
		t.Errorf("expected the old home asked more than once, got %d", n)
	}
}

func TestMayorGoneOnceItHandsOffPartWayThroughTheWait(t *testing.T) {
	vault, _ := ranOnOld(t, map[string]string{"pgrep": `[ -s "$0.count" ] && exit 1; echo x > "$0.count"; exit 0`})
	writeActing(t, vault, "Mayor after handoff 98\n")

	gone, _, err := Host{Poll: time.Millisecond}.MayorGone(context.Background(), oldHome, 5*time.Second)

	if err != nil || !gone {
		t.Fatalf("expected the Mayor gone on the second look, got %v, %v", gone, err)
	}
}

func TestMayorGoneThatCannotBeToldIsAnError(t *testing.T) {
	vault, _ := ranOnOld(t, map[string]string{"pgrep": "exit 2"})
	writeActing(t, vault, "Mayor after handoff 98\n")

	gone, _, err := Host{Poll: time.Millisecond}.MayorGone(context.Background(), oldHome, time.Second)

	if err == nil || gone {
		t.Fatalf("expected an error, not a Mayor gone, got %v, %v", gone, err)
	}
}

func TestMayorGoneReadsTheVaultFromTheConfigFileWhenNoEnvSaysWhere(t *testing.T) {
	vault, _ := ranOnOld(t, map[string]string{"pgrep": "exit 0"})
	writeActing(t, vault, "Mayor after handoff 98\n")
	t.Setenv("MW_VAULT", "")
	config := filepath.Join(os.Getenv("HOME"), ".config", "mw")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "config.toml"), []byte("host = \"desktop\"\nvault = \""+vault+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gone, said, err := Host{Poll: time.Millisecond}.MayorGone(context.Background(), oldHome, 20*time.Millisecond)

	if err != nil || gone || !strings.Contains(said, "Mayor after handoff 98") {
		t.Fatalf("expected the Mayor found still there by the config's vault, got %v, %q, %v", gone, said, err)
	}
}

func TestOldUnitInstalledAndStopUnit(t *testing.T) {
	_, dir := ranOnOld(t, map[string]string{"systemctl": `case "$2" in
  list-unit-files) echo "$3 enabled enabled" ;;
  is-active) echo active ;;
esac`})

	installed, err := Host{}.OldUnitInstalled(context.Background(), oldHome, "postern-backend")
	if err != nil || !installed {
		t.Fatalf("expected the unit installed, got %v, %v", installed, err)
	}
	stopped, err := Host{}.OldStopUnit(context.Background(), oldHome, "postern-backend")
	if err != nil || !stopped {
		t.Fatalf("expected the unit stopped, got %v, %v", stopped, err)
	}
	if got := logOf(t, dir, "systemctl"); !strings.Contains(got, "--user stop postern-backend") {
		t.Errorf("systemctl was run as %q", got)
	}
}

func TestOldStopUnitLeavesAUnitThatIsNotRunning(t *testing.T) {
	_, dir := ranOnOld(t, map[string]string{"systemctl": `[ "$2" = is-active ] && echo inactive; exit 3`})

	stopped, err := Host{}.OldStopUnit(context.Background(), oldHome, "dolt-beads")

	if err != nil || stopped {
		t.Fatalf("expected nothing stopped, got %v, %v", stopped, err)
	}
	if got := logOf(t, dir, "systemctl"); strings.Contains(got, "stop dolt-beads") {
		t.Errorf("stopped a unit that was not running: %q", got)
	}
}

func TestOldUnitNotInstalledIsNoUnit(t *testing.T) {
	ranOnOld(t, map[string]string{"systemctl": "exit 0"})

	installed, err := Host{}.OldUnitInstalled(context.Background(), oldHome, "dolt-beads")

	if err != nil || installed {
		t.Fatalf("expected no unit, got %v, %v", installed, err)
	}
}

func TestOldDisableUnitDisablesAndStopsAnEnabledUnit(t *testing.T) {
	_, dir := ranOnOld(t, map[string]string{"systemctl": `case "$2" in
  is-enabled) echo enabled ;;
  is-active) echo active ;;
esac`})

	disabled, err := Host{}.OldDisableUnit(context.Background(), oldHome, "dolt-beads")

	if err != nil || !disabled {
		t.Fatalf("expected the unit disabled, got %v, %v", disabled, err)
	}
	if got := logOf(t, dir, "systemctl"); !strings.Contains(got, "--user disable --now dolt-beads") {
		t.Errorf("systemctl was run as %q", got)
	}
}

func TestOldDisableUnitDisablesAUnitThatIsEnabledButNotRunning(t *testing.T) {
	_, dir := ranOnOld(t, map[string]string{"systemctl": `case "$2" in
  is-enabled) echo enabled ;;
  is-active) echo inactive; exit 3 ;;
esac`})

	disabled, err := Host{}.OldDisableUnit(context.Background(), oldHome, "dolt-beads")

	if err != nil || !disabled {
		t.Fatalf("expected the unit disabled, got %v, %v", disabled, err)
	}
	if got := logOf(t, dir, "systemctl"); !strings.Contains(got, "--user disable --now dolt-beads") {
		t.Errorf("systemctl was run as %q", got)
	}
}

func TestOldDisableUnitLeavesAUnitThatIsNeitherEnabledNorRunning(t *testing.T) {
	_, dir := ranOnOld(t, map[string]string{"systemctl": `case "$2" in
  is-enabled) echo disabled ;;
  is-active) echo inactive ;;
esac
exit 3`})

	disabled, err := Host{}.OldDisableUnit(context.Background(), oldHome, "dolt-beads")

	if err != nil || disabled {
		t.Fatalf("expected nothing disabled, got %v, %v", disabled, err)
	}
	if got := logOf(t, dir, "systemctl"); strings.Contains(got, "disable") {
		t.Errorf("disabled a unit that was neither enabled nor running: %q", got)
	}
}

func TestOldDisableUnitThatFailsIsAnError(t *testing.T) {
	ranOnOld(t, map[string]string{"systemctl": `case "$2" in
  is-enabled) echo enabled ;;
  is-active) echo active ;;
  disable) echo 'Failed to connect to bus' >&2; exit 1 ;;
esac`})

	if _, err := (Host{}).OldDisableUnit(context.Background(), oldHome, "dolt-beads"); err == nil || !strings.Contains(err.Error(), "Failed to connect") {
		t.Fatalf("expected the failure with what systemctl said, got %v", err)
	}
}

func TestOldPushRunsBDDoltPushInTheOldHomesVaultWithBeadsEnvSourced(t *testing.T) {
	vault, dir := ranOnOld(t, map[string]string{"bd": `echo "fsck=$BEADS_FSCK_TIMEOUT" >> "$(dirname "$0")/bd.env"; pwd >> "$(dirname "$0")/bd.pwd"`})
	config := filepath.Join(os.Getenv("HOME"), ".config", "mw")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "beads.env"), []byte("BEADS_FSCK_TIMEOUT=10m\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := (Host{}).OldPush(context.Background(), oldHome); err != nil {
		t.Fatalf("push: %v", err)
	}

	if got := strings.TrimSpace(logOf(t, dir, "bd")); got != "dolt push" {
		t.Errorf("bd was run as %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "bd.env")); strings.TrimSpace(string(got)) != "fsck=10m" {
		t.Errorf("bd ran without beads.env sourced: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "bd.pwd")); strings.TrimSpace(string(got)) != vault {
		t.Errorf("bd ran in %q, want the vault %s", got, vault)
	}
}

func TestOldPushThatFailsSaysWhatBDSaid(t *testing.T) {
	ranOnOld(t, map[string]string{"bd": "echo 'fsck timed out' >&2; exit 1"})

	if err := (Host{}).OldPush(context.Background(), oldHome); err == nil || !strings.Contains(err.Error(), "fsck timed out") {
		t.Fatalf("expected the failure with what bd said, got %v", err)
	}
}

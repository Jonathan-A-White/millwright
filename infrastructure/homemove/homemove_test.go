package homemove

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// onPath puts stand-in programs first on PATH and returns the directory they
// write their arguments to, in a file named for each: `<name>.log`.
func onPath(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		script := "#!/bin/sh\necho \"$@\" >> '" + filepath.Join(dir, name+".log") + "'\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func logOf(t *testing.T, dir, name string) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, name+".log"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

func TestOldHomeAnswersWhenSSHRunsTheCommand(t *testing.T) {
	dir := onPath(t, map[string]string{"ssh": "exit 0"})

	up, err := Host{}.OldHomeAnswers(context.Background(), []string{"ssh", "desktop"}, 10*time.Second)

	if err != nil || !up {
		t.Fatalf("expected an answer, got %v, %v", up, err)
	}
	if got := strings.TrimSpace(logOf(t, dir, "ssh")); got != "-o BatchMode=yes -o ConnectTimeout=10 desktop true" {
		t.Errorf("ssh was run as %q", got)
	}
}

func TestOldHomeAnswersOnAnyLiveSSHD(t *testing.T) {
	for name, body := range map[string]string{
		"the command failed":   "exit 1",
		"the key was refused":  "echo 'jwhite@desktop: Permission denied (publickey).' >&2; exit 255",
		"the host key changed": "echo 'Host key verification failed.' >&2; exit 255",
	} {
		t.Run(name, func(t *testing.T) {
			onPath(t, map[string]string{"ssh": body})
			up, err := Host{}.OldHomeAnswers(context.Background(), []string{"ssh", "desktop"}, 10*time.Second)
			if err != nil || !up {
				t.Errorf("a live sshd answered, got %v, %v", up, err)
			}
		})
	}
}

func TestOldHomeDoesNotAnswerWhenTheConnectionNeverComesUp(t *testing.T) {
	for name, body := range map[string]string{
		"timed out":    "echo 'ssh: connect to host desktop port 22: Connection timed out' >&2; exit 255",
		"no route":     "echo 'ssh: connect to host desktop port 22: No route to host' >&2; exit 255",
		"no such name": "echo 'ssh: Could not resolve hostname desktop' >&2; exit 255",
		"refused":      "echo 'ssh: connect to host desktop port 22: Connection refused' >&2; exit 255",
	} {
		t.Run(name, func(t *testing.T) {
			onPath(t, map[string]string{"ssh": body})
			up, err := Host{}.OldHomeAnswers(context.Background(), []string{"ssh", "desktop"}, 10*time.Second)
			if err != nil || up {
				t.Errorf("expected no answer, got %v, %v", up, err)
			}
		})
	}
}

func TestOldHomeDoesNotAnswerWhenSSHItselfStalls(t *testing.T) {
	onPath(t, map[string]string{"ssh": "sleep 30"})
	saved := answerGrace
	answerGrace = 0
	defer func() { answerGrace = saved }()

	started := time.Now()
	up, err := Host{}.OldHomeAnswers(context.Background(), []string{"ssh", "desktop"}, time.Second)

	if err != nil || up {
		t.Errorf("expected no answer, got %v, %v", up, err)
	}
	if took := time.Since(started); took > 10*time.Second {
		t.Errorf("waited %s for a stalled ssh", took)
	}
}

func TestOldHomeWithNoSSHProgramIsAnErrorNotAnAbsence(t *testing.T) {
	if _, err := (Host{}).OldHomeAnswers(context.Background(), []string{"no-such-ssh-program", "desktop"}, time.Second); err == nil {
		t.Error("a host whose ssh could not be run must not be taken for a dead old home")
	}
}

// git runs git for a test fixture.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-09-29T09:37:19-04:00", "GIT_COMMITTER_DATE=2026-09-29T09:37:19-04:00")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// aVaultWithOrigin is a git vault whose origin is a bare repository holding, when
// dolt is true, a refs/dolt/data one commit deep.
func aVaultWithOrigin(t *testing.T, dolt bool) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	git(t, root, "init", "--bare", "--quiet", origin)
	vault := filepath.Join(root, "vault")
	git(t, root, "init", "--quiet", vault)
	git(t, vault, "remote", "add", "origin", origin)
	if dolt {
		seed := filepath.Join(root, "seed")
		git(t, root, "init", "--quiet", seed)
		if err := os.WriteFile(filepath.Join(seed, "manifest"), []byte("chunks"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, seed, "add", "manifest")
		git(t, seed, "commit", "--quiet", "-m", "gitblobstore: checkandput manifest")
		git(t, seed, "push", "--quiet", origin, "HEAD:refs/dolt/data")
	}
	return vault
}

func TestBackupTimeIsTheDateOfTheTipOfRefsDoltData(t *testing.T) {
	vault := aVaultWithOrigin(t, true)

	at, err := Host{Vault: vault}.BackupTime(context.Background())

	if err != nil {
		t.Fatalf("BackupTime: %v", err)
	}
	if want := time.Date(2026, 9, 29, 13, 37, 19, 0, time.UTC); !at.Equal(want) {
		t.Errorf("got %s, want %s", at, want)
	}
	if entries, _ := os.ReadDir(filepath.Join(vault, ".git", "refs", "heads")); len(entries) != 0 {
		t.Errorf("the vault was written to: %v", entries)
	}
}

func TestBackupTimeReadsAnOriginNamedByARelativePath(t *testing.T) {
	vault := aVaultWithOrigin(t, true)
	git(t, vault, "remote", "set-url", "origin", "../origin.git")

	at, err := Host{Vault: vault}.BackupTime(context.Background())

	if want := time.Date(2026, 9, 29, 13, 37, 19, 0, time.UTC); err != nil || !at.Equal(want) {
		t.Errorf("got %s, %v, want %s", at, err, want)
	}
}

func TestAbsoluteLeavesEveryUrlButARelativePathAlone(t *testing.T) {
	host := Host{Vault: "/home/jwhite/millwright-vault"}
	for url, want := range map[string]string{
		"git@github.com:Jonathan-A-White/millwright-vault.git": "git@github.com:Jonathan-A-White/millwright-vault.git",
		"https://github.com/Jonathan-A-White/x.git":            "https://github.com/Jonathan-A-White/x.git",
		"ssh://git@github.com/x.git":                           "ssh://git@github.com/x.git",
		"file:///srv/origin.git":                               "file:///srv/origin.git",
		"/srv/origin.git":                                      "/srv/origin.git",
		"../origin.git":                                        "/home/jwhite/origin.git",
		"origin.git":                                           "/home/jwhite/millwright-vault/origin.git",
	} {
		if got := host.absolute(url); got != want {
			t.Errorf("absolute(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestBackupTimeSaysWhenGitHubHasNoBackup(t *testing.T) {
	vault := aVaultWithOrigin(t, false)
	if _, err := (Host{Vault: vault}).BackupTime(context.Background()); err == nil || !strings.Contains(err.Error(), "refs/dolt/data") {
		t.Errorf("expected an error naming refs/dolt/data, got %v", err)
	}
}

func TestSetBeadsAsideMovesTheDatabaseAndKeepsIt(t *testing.T) {
	root := t.TempDir()
	vault, home := filepath.Join(root, "vault"), filepath.Join(root, "home")
	db := filepath.Join(vault, ".beads", "embeddeddolt")
	for _, dir := range []string{db, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(db, "data"), []byte("4422 beads"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := Host{Vault: vault, Home: home}.SetBeadsAside(context.Background(), "20260929T140000Z")

	if err != nil {
		t.Fatalf("SetBeadsAside: %v", err)
	}
	want := filepath.Join(home, "beads-embeddeddolt-aside-20260929T140000Z")
	if moved.From != db || moved.To != want {
		t.Errorf("got %+v, want from %s to %s", moved, db, want)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Errorf("the database is still where it was: %v", err)
	}
	if kept, err := os.ReadFile(filepath.Join(want, "data")); err != nil || string(kept) != "4422 beads" {
		t.Errorf("the database was not kept whole: %q, %v", kept, err)
	}
}

func TestSetBeadsAsideWithNoDatabaseIsNothingToDo(t *testing.T) {
	root := t.TempDir()
	moved, err := Host{Vault: filepath.Join(root, "vault"), Home: root}.SetBeadsAside(context.Background(), "20260929T140000Z")
	if err != nil || moved.From != "" {
		t.Errorf("got %+v, %v", moved, err)
	}
}

func TestSetBeadsAsideNeverWritesOverAnAsideThatIsThere(t *testing.T) {
	root := t.TempDir()
	vault, home := filepath.Join(root, "vault"), filepath.Join(root, "home")
	db := filepath.Join(vault, ".beads", "embeddeddolt")
	aside := filepath.Join(home, "beads-embeddeddolt-aside-20260929T140000Z")
	for _, dir := range []string{db, aside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Host{Vault: vault, Home: home}.SetBeadsAside(context.Background(), "20260929T140000Z")

	if err == nil || !strings.Contains(err.Error(), "already there") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if _, err := os.Stat(db); err != nil {
		t.Errorf("the database was moved anyway: %v", err)
	}
}

func TestBootstrapAndCountRunBDInTheVault(t *testing.T) {
	dir := onPath(t, map[string]string{"bd": `if [ "$1" = count ]; then echo 4422; fi; pwd >> "$(dirname "$0")/bd.pwd"`})
	vault := t.TempDir()
	host := Host{Vault: vault}

	if err := host.BootstrapBeads(context.Background()); err != nil {
		t.Fatalf("BootstrapBeads: %v", err)
	}
	count, err := host.BeadsCount(context.Background())
	if err != nil || count != 4422 {
		t.Errorf("BeadsCount = %d, %v", count, err)
	}
	if got := logOf(t, dir, "bd"); got != "bootstrap --yes\ncount\n" {
		t.Errorf("bd was run as %q", got)
	}
	if pwd, _ := os.ReadFile(filepath.Join(dir, "bd.pwd")); !strings.Contains(string(pwd), filepath.Base(vault)) {
		t.Errorf("bd ran in %q, not the vault %s", pwd, vault)
	}
}

func TestBootstrapThatFailsSaysWhatBDSaid(t *testing.T) {
	onPath(t, map[string]string{"bd": "echo 'no refs/dolt/data on origin' >&2; exit 1"})
	err := Host{Vault: t.TempDir()}.BootstrapBeads(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no refs/dolt/data on origin") {
		t.Errorf("expected bd's words, got %v", err)
	}
}

func TestBeadsCountThatIsNotANumberIsAnError(t *testing.T) {
	onPath(t, map[string]string{"bd": "echo nonsense"})
	if _, err := (Host{Vault: t.TempDir()}).BeadsCount(context.Background()); err == nil {
		t.Error("expected an error")
	}
}

func TestRestoreBeadsConfigPutsBackTheTrailingNewlineBootstrapDrops(t *testing.T) {
	vault := aVaultWithOrigin(t, false)
	config := filepath.Join(vault, ".beads", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("issue-prefix: mw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, vault, "add", ".beads/config.yaml")
	git(t, vault, "commit", "--quiet", "-m", "config")
	if err := os.WriteFile(config, []byte("issue-prefix: mw"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := (Host{Vault: vault}).RestoreBeadsConfig(context.Background()); err != nil {
		t.Fatalf("RestoreBeadsConfig: %v", err)
	}
	if got, _ := os.ReadFile(config); string(got) != "issue-prefix: mw\n" {
		t.Errorf("config.yaml reads %q", got)
	}
}

// systemctl is a stand-in that knows one installed unit, which is active or not.
func systemctl(installed, active string) string {
	return `case "$2" in
list-unit-files) if [ "$3" = "` + installed + `.service" ]; then echo "` + installed + `.service enabled enabled"; else exit 1; fi ;;
is-active) if [ "$3" = "` + active + `" ]; then echo active; else echo inactive; exit 3; fi ;;
esac`
}

func TestUnitInstalledReadsListUnitFiles(t *testing.T) {
	onPath(t, map[string]string{"systemctl": systemctl("dolt-beads", "")})
	if ok, err := (Host{}).UnitInstalled(context.Background(), "dolt-beads"); err != nil || !ok {
		t.Errorf("dolt-beads: %v, %v", ok, err)
	}
	if ok, err := (Host{}).UnitInstalled(context.Background(), "postern-backend"); err != nil || ok {
		t.Errorf("postern-backend is not installed: %v, %v", ok, err)
	}
}

func TestUnitInstalledThatCannotAskIsAnErrorNotANo(t *testing.T) {
	onPath(t, map[string]string{"systemctl": "echo 'Failed to connect to bus: No medium found' >&2; exit 1"})
	if ok, err := (Host{}).UnitInstalled(context.Background(), "dolt-beads"); err == nil || ok {
		t.Errorf("expected an error, got %v, %v", ok, err)
	}
}

func TestStartUnitStartsOnlyAUnitThatIsNotRunning(t *testing.T) {
	dir := onPath(t, map[string]string{"systemctl": systemctl("dolt-beads", "postern-backend")})

	started, err := Host{}.StartUnit(context.Background(), "postern-backend")
	if err != nil || started {
		t.Errorf("an active unit is left alone: %v, %v", started, err)
	}
	if strings.Contains(logOf(t, dir, "systemctl"), "start") {
		t.Errorf("started a unit that was running: %s", logOf(t, dir, "systemctl"))
	}

	started, err = Host{}.StartUnit(context.Background(), "dolt-beads")
	if err != nil || !started {
		t.Errorf("an inactive unit is started: %v, %v", started, err)
	}
	if !strings.Contains(logOf(t, dir, "systemctl"), "--user start dolt-beads") {
		t.Errorf("systemctl was run as %q", logOf(t, dir, "systemctl"))
	}
}

// backend answers /healthz in standby for the first standbys asks, then as home.
func backend(standbys int32) (*httptest.Server, *atomic.Int32) {
	var asked atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		if asked.Add(1) <= standbys {
			w.Write([]byte(`{"ok":true,"standby":true,"home":"desktop"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})), &asked
}

func TestBackendServingWaitsForStandbyToEnd(t *testing.T) {
	server, asked := backend(2)
	defer server.Close()

	if err := (Host{Poll: 5 * time.Millisecond}).BackendServing(context.Background(), server.URL, 5*time.Second); err != nil {
		t.Fatalf("BackendServing: %v", err)
	}
	if asked.Load() != 3 {
		t.Errorf("asked %d times, wanted 3", asked.Load())
	}
}

func TestBackendServingThatStaysInStandbySaysSo(t *testing.T) {
	server, _ := backend(1 << 20)
	defer server.Close()

	err := Host{Poll: 5 * time.Millisecond}.BackendServing(context.Background(), server.URL, 100*time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), "standby") {
		t.Errorf("expected the standby to be named, got %v", err)
	}
}

func TestBackendServingThatIsDownSaysWhatItGot(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusServiceUnavailable)
	}))
	defer down.Close()

	err := Host{Poll: 5 * time.Millisecond}.BackendServing(context.Background(), down.URL, 100*time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("expected HTTP 503 to be named, got %v", err)
	}
}

func aVaultWithMayorUp(t *testing.T, body string) Host {
	t.Helper()
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "bin", "mayor-up"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return Host{Vault: vault}
}

func TestMayorUpStartedIsItsLastLine(t *testing.T) {
	started, said, err := aVaultWithMayorUp(t, "echo 'started a Mayor'; echo '@12'").MayorUp(context.Background())
	if err != nil || !started || said != "@12" {
		t.Errorf("got %v, %q, %v", started, said, err)
	}
}

func TestMayorUpFindingAMayorAlreadyThereIsNotAFailure(t *testing.T) {
	started, said, err := aVaultWithMayorUp(t, "echo 'a Mayor is alive; nothing done'; exit 3").MayorUp(context.Background())
	if err != nil || started || !strings.Contains(said, "alive") {
		t.Errorf("got %v, %q, %v", started, said, err)
	}
}

func TestMayorUpThatCannotSaysWhy(t *testing.T) {
	for status, said := range map[string]string{"4": "cannot: no tmux on PATH", "5": "not home (home is desktop): no Mayor started"} {
		_, _, err := aVaultWithMayorUp(t, "echo '"+said+"'; exit "+status).MayorUp(context.Background())
		if err == nil || !strings.Contains(err.Error(), said) {
			t.Errorf("exit %s: expected %q, got %v", status, said, err)
		}
	}
}

func TestSetDoltAsideMovesTheServedDirectoryAndKeepsIt(t *testing.T) {
	root := t.TempDir()
	vault, home := filepath.Join(root, "vault"), filepath.Join(root, "home")
	db := filepath.Join(vault, ".beads", "dolt")
	for _, dir := range []string{db, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(db, "data"), []byte("7 beads, stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	moved, err := Host{Vault: vault, Home: home}.SetDoltAside(context.Background(), "20260929T140000Z")

	if err != nil {
		t.Fatalf("SetDoltAside: %v", err)
	}
	want := filepath.Join(home, "beads-dolt-aside-20260929T140000Z")
	if moved.From != db || moved.To != want {
		t.Errorf("got %+v, want from %s to %s", moved, db, want)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Errorf("the directory is still where it was: %v", err)
	}
	if kept, err := os.ReadFile(filepath.Join(want, "data")); err != nil || string(kept) != "7 beads, stale" {
		t.Errorf("the directory was not kept whole: %q, %v", kept, err)
	}
}

func TestSetDoltAsideWithNoDirectoryIsNothingToDo(t *testing.T) {
	root := t.TempDir()
	moved, err := Host{Vault: filepath.Join(root, "vault"), Home: root}.SetDoltAside(context.Background(), "20260929T140000Z")
	if err != nil || moved.From != "" {
		t.Errorf("got %+v, %v", moved, err)
	}
}

func TestSetDoltAsideNeverWritesOverAnAsideThatIsThere(t *testing.T) {
	root := t.TempDir()
	vault, home := filepath.Join(root, "vault"), filepath.Join(root, "home")
	db := filepath.Join(vault, ".beads", "dolt")
	aside := filepath.Join(home, "beads-dolt-aside-20260929T140000Z")
	for _, dir := range []string{db, aside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Host{Vault: vault, Home: home}.SetDoltAside(context.Background(), "20260929T140000Z")

	if err == nil || !strings.Contains(err.Error(), "already there") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if _, err := os.Stat(db); err != nil {
		t.Errorf("the directory was moved anyway: %v", err)
	}
}

func TestStopUnitStopsOnlyAUnitThatIsRunning(t *testing.T) {
	dir := onPath(t, map[string]string{"systemctl": systemctl("dolt-beads", "dolt-beads")})

	stopped, err := Host{}.StopUnit(context.Background(), "postern-backend")
	if err != nil || stopped {
		t.Errorf("a unit that is not running is left alone: %v, %v", stopped, err)
	}
	if strings.Contains(logOf(t, dir, "systemctl"), "stop") {
		t.Errorf("stopped a unit that was not running: %s", logOf(t, dir, "systemctl"))
	}

	stopped, err = Host{}.StopUnit(context.Background(), "dolt-beads")
	if err != nil || !stopped {
		t.Errorf("an active unit is stopped: %v, %v", stopped, err)
	}
	if !strings.Contains(logOf(t, dir, "systemctl"), "--user stop dolt-beads") {
		t.Errorf("systemctl was run as %q", logOf(t, dir, "systemctl"))
	}
}

// UnitWorkingDirectory is read from systemd, never assumed: `systemctl --user show
// <unit> -p WorkingDirectory` prints WorkingDirectory=<path>.
func TestUnitWorkingDirectoryIsReadFromSystemctlShow(t *testing.T) {
	dir := onPath(t, map[string]string{"systemctl": `echo "WorkingDirectory=/srv/beads/data"`})

	got, err := Host{}.UnitWorkingDirectory(context.Background(), "dolt-beads")

	if err != nil || got != "/srv/beads/data" {
		t.Errorf("got %q, %v", got, err)
	}
	if log := strings.TrimSpace(logOf(t, dir, "systemctl")); log != "--user show dolt-beads -p WorkingDirectory" {
		t.Errorf("systemctl was run as %q", log)
	}
}

func TestUnitWorkingDirectoryDropsSystemdsOptionalMark(t *testing.T) {
	onPath(t, map[string]string{"systemctl": `echo "WorkingDirectory=!/srv/beads/data"`})
	if got, err := (Host{}).UnitWorkingDirectory(context.Background(), "dolt-beads"); err != nil || got != "/srv/beads/data" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestUnitWorkingDirectoryThatIsNotAPathIsAnError(t *testing.T) {
	for _, said := range []string{"WorkingDirectory=", "WorkingDirectory=~", "WorkingDirectory=relative/dir", "nothing useful"} {
		onPath(t, map[string]string{"systemctl": `echo "` + said + `"`})
		if got, err := (Host{}).UnitWorkingDirectory(context.Background(), "dolt-beads"); err == nil {
			t.Errorf("%q: expected an error, got %q", said, got)
		}
	}
}

func TestUnitWorkingDirectoryThatSystemctlCannotAskIsAnError(t *testing.T) {
	onPath(t, map[string]string{"systemctl": "echo 'Failed to connect to bus' >&2; exit 1"})
	if _, err := (Host{}).UnitWorkingDirectory(context.Background(), "dolt-beads"); err == nil {
		t.Error("expected an error")
	}
}

func TestEmptyDataDirMakesTheDirectoryTheUnitWillServe(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".beads", "dolt")
	if err := (Host{}).EmptyDataDir(context.Background(), dir); err != nil {
		t.Fatalf("EmptyDataDir: %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("expected an empty directory, got %v, %v", entries, err)
	}
	if err := (Host{}).EmptyDataDir(context.Background(), dir); err != nil {
		t.Errorf("a directory that is already empty is fine: %v", err)
	}
}

// What is in the directory is somebody's database: it is never cleared.
func TestEmptyDataDirNeverClearsADirectoryWithSomethingInIt(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "beads")
	if err := os.WriteFile(keep, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (Host{}).EmptyDataDir(context.Background(), dir); err == nil {
		t.Error("expected a refusal")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("what was there was touched: %v", err)
	}
}

func TestBeadsServerAnswersWhenTheAddressTakesADial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("BEADS_DOLT_SERVER_HOST", host)
	t.Setenv("BEADS_DOLT_SERVER_PORT", port)

	if err := (Host{Poll: 10 * time.Millisecond}).BeadsServerAnswers(context.Background(), 2*time.Second); err != nil {
		t.Errorf("a listening server: %v", err)
	}
}

func TestBeadsServerThatNeverAnswersSaysSoWhenWaitIsUp(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	t.Setenv("BEADS_DOLT_SERVER_HOST", host)
	t.Setenv("BEADS_DOLT_SERVER_PORT", port)

	err = (Host{Poll: 10 * time.Millisecond}).BeadsServerAnswers(context.Background(), 200*time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), host+":"+port) {
		t.Errorf("expected the address in the error, got %v", err)
	}
}

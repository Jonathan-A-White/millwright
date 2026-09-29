package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw sync is never run against a real vault here: it would reach the factory's
// remote. What is checked is the wiring — that the command is there, that it
// stops plainly when this machine has not been told where the vault is, and
// that a sync beads stopped leaves mw with beads' own exit status.

func TestSyncCommandStopsWhenTheMachineDoesNotKnowItsVault(t *testing.T) {
	t.Setenv("MW_VAULT", "")
	t.Setenv("MW_HOST", "")
	t.Setenv("HOME", t.TempDir())

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"sync"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected mw sync to stop when it does not know where the vault is")
	}
	if !strings.Contains(err.Error(), "MW_VAULT") {
		t.Fatalf("expected the reason to say how to set the vault, got %q", err)
	}
	if exitCode(err) != 1 {
		t.Fatalf("expected mw to leave with 1, got %d", exitCode(err))
	}
}

func TestSyncCommandIsPartOfMw(t *testing.T) {
	for _, cmd := range newRootCmd().Commands() {
		if cmd.Name() == "sync" {
			return
		}
	}
	t.Fatal("expected mw to have a sync command")
}

func TestExitCodeIsBeadsOwnWhenBeadsStoppedTheSync(t *testing.T) {
	for code, want := range map[int]int{0: 1, 1: 1, 2: 2, 3: 3, 4: 4} {
		halted := &application.SyncHalt{Code: code}
		if got := exitCode(halted); got != want {
			t.Fatalf("expected a halt on %d to leave with %d, got %d", code, want, got)
		}
	}
	blocked := &application.VaultBlocked{Host: "vps", Files: []string{"seats/mayor/ledger.md"}}
	if got := exitCode(blocked); got != application.VaultBlockedExit {
		t.Fatalf("expected a vault nobody committed to leave with %d, got %d", application.VaultBlockedExit, got)
	}
	if got := exitCode(errors.New("something else went wrong")); got != 1 {
		t.Fatalf("expected an ordinary failure to leave with 1, got %d", got)
	}
	if got := exitCode(nil); got != 0 {
		t.Fatalf("expected nothing wrong to leave with 0, got %d", got)
	}
}

func TestHostSyncTakesTheModeAndBackupIntervalFromConfig(t *testing.T) {
	t.Setenv("MW_BEADS_SYNC", "")
	t.Setenv("MW_BEADS_BACKUP_MINUTES", "")
	mwConfig(t, "vault = \"/v\"\nhost = \"desktop\"\nbeads_sync = \"backup\"\nbeads_backup_minutes = 45\n")

	sync, err := hostSync(application.Sync{Host: "desktop"})
	if err != nil {
		t.Fatalf("reading the sync settings: %v", err)
	}
	if sync.Mode != application.BeadsSyncBackup || sync.BackupInterval != 45*time.Minute {
		t.Fatalf("expected backup every 45m, got %q every %s", sync.Mode, sync.BackupInterval)
	}

	mwConfig(t, "vault = \"/v\"\nhost = \"laptop\"\n")
	if sync, err = hostSync(application.Sync{Host: "laptop"}); err != nil || sync.Mode != application.BeadsSyncRemote {
		t.Fatalf("expected remote when the config says nothing, got %q: %v", sync.Mode, err)
	}
}

func TestSyncCommandRefusesABeadsSyncModeNamingTheThree(t *testing.T) {
	t.Setenv("MW_BEADS_SYNC", "")
	mwConfig(t, "vault = \""+t.TempDir()+"\"\nhost = \"desktop\"\nbeads_sync = \"server\"\n")

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"sync"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected mw sync to refuse beads_sync = server")
	}
	for _, want := range []string{"remote", "backup", "shared"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to name %q, got %q", want, err)
		}
	}
}

func homeText(host string) string { return host + " 2026-09-29T12:00:00Z mayor@laptop\n" }

func TestHostSyncOnAutoLeavesTheBackupIntervalToTheModeUnlessSaid(t *testing.T) {
	t.Setenv("MW_BEADS_SYNC", "")
	t.Setenv("MW_BEADS_BACKUP_MINUTES", "")
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	mwConfig(t, "vault = \"/v\"\nhost = \"laptop\"\nbeads_sync = \"auto\"\n")
	sync, err := hostSync(application.Sync{Host: "laptop"})
	if err != nil || sync.Mode != application.BeadsSyncAuto || sync.BackupInterval != 0 {
		t.Fatalf("expected auto with no interval said, got %q %s: %v", sync.Mode, sync.BackupInterval, err)
	}

	mwConfig(t, "vault = \"/v\"\nhost = \"laptop\"\nbeads_sync = \"auto\"\nbeads_backup_minutes = 10\n")
	if sync, err = hostSync(application.Sync{Host: "laptop"}); err != nil || sync.BackupInterval != 10*time.Minute {
		t.Fatalf("expected the said 10 minutes, got %s: %v", sync.BackupInterval, err)
	}
}

func TestHostBeadsOnABoostPointsBdAtTheHomesServer(t *testing.T) {
	t.Setenv("MW_BEADS_SYNC", "")
	t.Setenv("MW_BEADS_SERVER_HOST", "")
	t.Setenv("BEADS_DOLT_SERVER_HOST", "stale.example")
	mwConfig(t, "beads_sync = \"auto\"\n")
	files := &apptest.FakeHomeFile{Text: homeText("desktop")}

	setting, err := hostBeads(context.Background(), files, "laptop")
	if err != nil || setting.Mode() != application.BeadsSyncShared || setting.Resolved.Why != "auto: boost of desktop" {
		t.Fatalf("expected shared as a boost of desktop, got %+v: %v", setting, err)
	}
	if got := os.Getenv("BEADS_DOLT_SERVER_HOST"); got != "desktop.mw" {
		t.Errorf("expected bd pointed at desktop.mw, got %q", got)
	}

	mwConfig(t, "beads_sync = \"auto\"\nbeads_server_host = \"10.88.0.2\"\n")
	if _, err = hostBeads(context.Background(), files, "laptop"); err != nil || os.Getenv("BEADS_DOLT_SERVER_HOST") != "10.88.0.2" {
		t.Errorf("expected the configured server host, got %q: %v", os.Getenv("BEADS_DOLT_SERVER_HOST"), err)
	}
}

func TestHostBeadsOnTheHomeAndWithNoHomeLeavesBdsServerAlone(t *testing.T) {
	t.Setenv("MW_BEADS_SYNC", "")
	t.Setenv("BEADS_DOLT_SERVER_HOST", "elsewhere")
	mwConfig(t, "beads_sync = \"auto\"\n")

	setting, err := hostBeads(context.Background(), &apptest.FakeHomeFile{Text: homeText("laptop")}, "laptop")
	if err != nil || setting.Mode() != application.BeadsSyncBackup {
		t.Fatalf("expected backup on the home, got %+v: %v", setting, err)
	}
	setting, err = hostBeads(context.Background(), &apptest.FakeHomeFile{Missing: true}, "laptop")
	if err != nil || setting.Unknown == nil || setting.Mode() != application.BeadsSyncAuto {
		t.Fatalf("expected an unknown home and no guess, got %+v: %v", setting, err)
	}
	if got := os.Getenv("BEADS_DOLT_SERVER_HOST"); got != "elsewhere" {
		t.Errorf("expected the server host left alone, got %q", got)
	}
}

func TestSyncCommandOnAutoWithNoHomeFileRefusesSayingWhy(t *testing.T) {
	t.Setenv("MW_BEADS_SYNC", "")
	mwConfig(t, "vault = \""+t.TempDir()+"\"\nhost = \"laptop\"\nbeads_sync = \"auto\"\n")

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"sync"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "no home file") {
		t.Fatalf("expected mw sync on auto to refuse for want of a home file, got %v", err)
	}
}

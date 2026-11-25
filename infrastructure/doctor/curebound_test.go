package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// mw-gq6.1641: a cure neither outlasts the run nor sleeps through its ctx. A cut
// cure says so in its error, and says it will be tried again.

func curesSayCut(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the cure to say it was cut, got no error")
	}
	text := err.Error()
	if !strings.Contains(text, "cut") || !strings.Contains(text, "retried") {
		t.Fatalf("expected the error to say the cure was cut and will be retried, got %q", text)
	}
}

// TestMayorUpCutByTheRunIsSaidAndRetried: a bin/mayor-up slower than the run's
// end is cut, and the cure's error says so rather than only "signal: killed".
func TestMayorUpCutByTheRunIsSaidAndRetried(t *testing.T) {
	r := newStaleRig(t)
	slowMayorUp(r)
	r.run(0)

	ctx := cutWhenMayorUpStarts(t, r)
	began := time.Now()
	err := r.check.Cure(ctx)
	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("the cure outlasted the run's ctx: %v", took)
	}
	curesSayCut(t, err)
}

func TestMayorUpCutOfMayorGoneIsSaidAndRetried(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	vault := mayorGoneVault(t, "mayor-2026-10-01-156")
	mayorUp := filepath.Join(vault, "bin", "mayor-up")
	if err := os.WriteFile(mayorUp, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := &doctor.MayorGone{Vault: vault}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	curesSayCut(t, gone.Cure(ctx))
}

// TestTmpLeftoversCureWithinBudget: a go clean slower than the run's end is cut
// and said so, and a run already over starts no delete.
func TestTmpLeftoversCureWithinBudget(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	if err := os.MkdirAll(goBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goBuildDir, "blob"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		GoClean: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Second):
				return nil
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	began := time.Now()
	err := check.Cure(ctx)
	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("the cure outlasted the run's ctx: %v", took)
	}
	curesSayCut(t, err)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected the ctx's own error to be kept, got %v", err)
	}
}

func TestTmpLeftoversCureWithinBudgetDeletesNothingOnceTheRunEnded(t *testing.T) {
	tmp, proc := tlHost(t)
	spool := filepath.Join(tmp, "bd")
	if err := os.MkdirAll(spool, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spool, "blob"), make([]byte, 500), 0o644); err != nil {
		t.Fatal(err)
	}
	check := &doctor.TmpLeftovers{TmpDir: tmp, ProcDir: proc, Budget: 100}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	curesSayCut(t, check.Cure(ctx))
	if _, err := os.Stat(spool); err != nil {
		t.Errorf("a cure begun after the run's end must remove nothing: %v", err)
	}
}

// TestTunnelCureEndsWithItsCtx: the settle after the restart does not sleep
// through a ctx that ends.
func TestTunnelCureEndsWithItsCtx(t *testing.T) {
	sshProgram, _ := fakeTunnelSSH(t, tunnelNotListeningFixture, 0)
	systemctlProgram, _ := fakeTunnelSystemctl(t)
	check := &doctor.Tunnel{
		Reach: []string{localListener(t)}, Host: "vps",
		SSH: sshProgram, Systemctl: systemctlProgram,
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(500*time.Millisecond, cancel)
	defer cancel()

	began := time.Now()
	err := check.Cure(ctx)
	if took := time.Since(began); took > time.Second+500*time.Millisecond {
		t.Fatalf("the cure slept through its ctx: %v", took)
	}
	curesSayCut(t, err)
}

func TestWgCureEndsWithItsCtx(t *testing.T) {
	ip, _ := fakeIP(t, wg0AddrFixture, 0)
	sudo, _ := fakeSudo(t, false)
	check := &doctor.Wg{
		Reach: []string{localListener(t)}, Hub: "10.88.0.1:22", Unit: "wg-quick@wg0",
		IP: ip, Sudo: sudo, State: doctor.New(t.TempDir()), Dial: alwaysDial(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(500*time.Millisecond, cancel)
	defer cancel()

	began := time.Now()
	err := check.Cure(ctx)
	if took := time.Since(began); took > time.Second+500*time.Millisecond {
		t.Fatalf("the cure slept through its ctx: %v", took)
	}
	curesSayCut(t, err)
}

// TestWifiCureEndsWithItsCtxButStillRejoinsTheNetwork: the settle after the
// disconnect does not sleep through the run's end, and the host is not left
// off its network.
func TestWifiCureEndsWithItsCtxButStillRejoinsTheNetwork(t *testing.T) {
	program, calls := fakeNetsh(t, wifiInterfacesFixture)
	check := &doctor.Wifi{Netsh: program, Reach: []string{localListener(t)}}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(500*time.Millisecond, cancel)
	defer cancel()

	began := time.Now()
	err := check.Cure(ctx)
	if took := time.Since(began); took > time.Second+500*time.Millisecond {
		t.Fatalf("the cure slept through its ctx: %v", took)
	}
	curesSayCut(t, err)
	got := callLines(t, calls)
	if len(got) != 3 || got[2] != "wlan connect name=Whitehouse" {
		t.Fatalf("expected the network rejoined after the cut, got %v", got)
	}
}

// TestTmpLeftoversCureWithinBudgetHasADeadlineOfItsOwn: with a run that sets no
// end, the cure still stops at its own.
func TestTmpLeftoversCureWithinBudgetHasADeadlineOfItsOwn(t *testing.T) {
	tmp, proc := tlHost(t)
	goBuildDir := filepath.Join(t.TempDir(), "go-build")
	if err := os.MkdirAll(goBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goBuildDir, "blob"), make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	check := &doctor.TmpLeftovers{
		TmpDir: tmp, ProcDir: proc, GoBuildDir: goBuildDir, GoBuildBudget: 100,
		CureTimeout: 200 * time.Millisecond,
		GoClean: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	began := time.Now()
	curesSayCut(t, check.Cure(context.Background()))
	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("the cure outlasted its own deadline: %v", took)
	}
}

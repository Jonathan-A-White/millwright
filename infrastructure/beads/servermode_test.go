package beads_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

// serverModeGateway is a gateway over a temp vault, whose bd is a stand-in
// that writes a marker file when it is started. dolt says whether the vault
// holds a .beads/dolt store, which is what marks it as in server mode.
func serverModeGateway(t *testing.T, dolt bool) (*beads.Gateway, string, string) {
	t.Helper()
	vault := t.TempDir()
	if dolt {
		if err := os.MkdirAll(filepath.Join(vault, ".beads", "dolt"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(t.TempDir(), "ran")
	program := filepath.Join(t.TempDir(), "bd")
	script := "#!/bin/sh\ntouch " + marker + "\necho '[]'\n"
	if err := os.WriteFile(program, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return beads.New(vault, beads.WithProgram(program)), vault, marker
}

func markerExists(marker string) bool {
	_, err := os.Stat(marker)
	return err == nil
}

func TestServerModeVaultWithoutTheHostVariableIsRefusedAndBdNotStarted(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	gateway, vault, marker := serverModeGateway(t, true)

	_, err := gateway.LiveEpics(context.Background())
	if err == nil {
		t.Fatal("expected a server-mode vault with no BEADS_DOLT_SERVER_HOST to be refused")
	}
	for _, want := range []string{vault, "BEADS_DOLT_SERVER_HOST", "source ~/.config/mw/beads.env"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to name %q, got %q", want, err)
		}
	}
	if markerExists(marker) {
		t.Fatal("expected bd not to be started")
	}
}

func TestServerModeVaultWithOnlyBlanksForTheHostIsRefused(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "  ")
	gateway, _, marker := serverModeGateway(t, true)

	if _, err := gateway.LiveEpics(context.Background()); err == nil {
		t.Fatal("expected a blank BEADS_DOLT_SERVER_HOST to be refused")
	}
	if markerExists(marker) {
		t.Fatal("expected bd not to be started")
	}
}

func TestServerModeVaultWithTheHostVariableRunsBd(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "desktop.mw")
	gateway, _, marker := serverModeGateway(t, true)

	_, _ = gateway.LiveEpics(context.Background())
	if !markerExists(marker) {
		t.Fatal("expected bd to be started")
	}
}

func TestServerModeVaultWithoutDoltIsUnaffected(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	gateway, _, marker := serverModeGateway(t, false)

	_, _ = gateway.LiveEpics(context.Background())
	if !markerExists(marker) {
		t.Fatal("expected bd to be started in a vault with no .beads/dolt")
	}
}

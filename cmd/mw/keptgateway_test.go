package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestKeepBeadsConnectionGivesTheGatewaysABeadsServerConnectionUntilStopped(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, ".beads", "metadata.json"), []byte(`{"dolt_database":"beads"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DOLT_SERVER_HOST", "127.0.0.1")

	stop := keepBeadsConnection(vault, &bytes.Buffer{})
	if got := len(followerGatewayOptions); got != 2 {
		t.Errorf("expected the said writer and the connection among the options, got %d options", got)
	}
	stop()
	if followerGatewayOptions != nil {
		t.Errorf("expected the options cleared once stopped, got %d", len(followerGatewayOptions))
	}
}

func TestKeepBeadsConnectionLeavesGatewaysReadingByBdOnAHostWithNoServer(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")

	stop := keepBeadsConnection(t.TempDir(), &bytes.Buffer{})
	defer stop()
	if got := len(followerGatewayOptions); got != 1 {
		t.Errorf("expected only the said writer among the options, got %d options", got)
	}
}

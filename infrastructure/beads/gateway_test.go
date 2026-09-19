package beads_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

func TestFromConfigTakesTheVaultFromTheConfiguration(t *testing.T) {
	t.Setenv("MW_VAULT", "/somewhere/millwright-vault")
	t.Setenv("MW_HOST", "vps")

	gateway, err := beads.FromConfig()
	if err != nil {
		t.Fatalf("making a gateway from the configuration: %v", err)
	}
	if gateway.Vault() != "/somewhere/millwright-vault" {
		t.Fatalf("expected the configured vault, got %q", gateway.Vault())
	}
	if gateway.Actor() != "mw@vps" {
		t.Fatalf("expected the gateway to act as mw@vps, got %q", gateway.Actor())
	}
}

func TestFromConfigReportsAnUnsetVault(t *testing.T) {
	t.Setenv("MW_VAULT", "")
	t.Setenv("HOME", t.TempDir())

	if _, err := beads.FromConfig(); err == nil {
		t.Fatal("expected a gateway with nowhere to point to be refused")
	}
}

// A gateway that does not know which host it is on has no name to write under,
// and mw writes under one name or not at all: the refusal comes before any bd
// is started, rather than after a claim has been made under whatever name the
// environment happened to carry.
func TestFromConfigReportsAnUnsetHostBeforeAnythingIsWritten(t *testing.T) {
	t.Setenv("MW_VAULT", "/somewhere/millwright-vault")
	t.Setenv("MW_HOST", "")
	t.Setenv("HOME", t.TempDir())

	_, err := beads.FromConfig()
	if err == nil {
		t.Fatal("expected a gateway with no host to be refused")
	}
	if !strings.Contains(err.Error(), "MW_HOST") {
		t.Fatalf("expected the reason to say how to set the host, got %q", err)
	}
}

// recorder writes a stand-in bd that says nothing, exits well and writes down
// every argv it was given, and returns a Gateway that runs it and the file it
// writes to.
func recorder(t *testing.T, opts ...beads.Option) (*beads.Gateway, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for bd is a shell script")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "asked.log")
	path := filepath.Join(dir, "bd-recorder")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %s\nexit 0\n", log)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return beads.New(dir, append([]beads.Option{beads.WithProgram(path)}, opts...)...), log
}

// Every bd mw starts carries the name mw acts under, so that what mw writes
// does not depend on the BEADS_ACTOR of whatever shell, session or timer
// happened to start it. A claim made under one name and a close attempted
// under another is what bd 1.3.0 refuses.
func TestEveryCallCarriesTheNameMwActsUnder(t *testing.T) {
	gateway, log := recorder(t, beads.WithActor("mw@vps"))
	ctx := context.Background()

	if err := gateway.ClaimStory(ctx, "t-1"); err != nil {
		t.Fatalf("claiming: %v", err)
	}
	if err := gateway.SetStoryMetadata(ctx, "t-1", map[string]string{"molecule": "t-mol"}); err != nil {
		t.Fatalf("writing metadata: %v", err)
	}
	if err := gateway.SetStoryState(ctx, "t-1", "run", "running", "dispatched"); err != nil {
		t.Fatalf("setting state: %v", err)
	}
	if err := gateway.CommentOnStory(ctx, "t-1", "a note"); err != nil {
		t.Fatalf("commenting: %v", err)
	}
	if err := gateway.ReleaseStory(ctx, "t-1"); err != nil {
		t.Fatalf("releasing: %v", err)
	}
	if err := gateway.ReleaseClaim(ctx, "t-1"); err != nil {
		t.Fatalf("releasing the claim: %v", err)
	}
	if err := gateway.CloseStory(ctx, "t-1", "landed on main"); err != nil {
		t.Fatalf("closing: %v", err)
	}

	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading what bd was asked for: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(asked)), "\n")
	if len(lines) != 7 {
		t.Fatalf("expected seven bd calls, got %d:\n%s", len(lines), asked)
	}
	for _, line := range lines {
		if !strings.Contains(line, "--actor mw@vps") {
			t.Errorf("expected every bd call to carry --actor mw@vps, got %q", line)
		}
	}
}

// A gateway told no name leaves bd to its own default, which is what a person
// running bd by hand gets: nothing is silently written under "mw".
func TestAGatewayWithNoNameAddsNoActor(t *testing.T) {
	gateway, log := recorder(t)
	if err := gateway.CloseStory(context.Background(), "t-1", "by hand"); err != nil {
		t.Fatalf("closing: %v", err)
	}
	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("reading what bd was asked for: %v", err)
	}
	if strings.Contains(string(asked), "--actor") {
		t.Fatalf("expected no actor to be named, got %q", asked)
	}
}

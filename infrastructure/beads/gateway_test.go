package beads_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/beads"
)

func TestFromConfigTakesTheVaultFromTheConfiguration(t *testing.T) {
	t.Setenv("MW_VAULT", "/somewhere/millwright-vault")

	gateway, err := beads.FromConfig()
	if err != nil {
		t.Fatalf("making a gateway from the configuration: %v", err)
	}
	if gateway.Vault() != "/somewhere/millwright-vault" {
		t.Fatalf("expected the configured vault, got %q", gateway.Vault())
	}
}

func TestFromConfigReportsAnUnsetVault(t *testing.T) {
	t.Setenv("MW_VAULT", "")
	t.Setenv("HOME", t.TempDir())

	if _, err := beads.FromConfig(); err == nil {
		t.Fatal("expected a gateway with nowhere to point to be refused")
	}
}

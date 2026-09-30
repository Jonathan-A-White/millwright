package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// bstVault makes a temp dir standing in for a vault, with .beads holding the
// named store directories. Nothing here reads a real vault.
func bstVault(t *testing.T, stores ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, store := range stores {
		if err := os.MkdirAll(filepath.Join(dir, ".beads", store), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBeadsStoresProbeIsOKWithTheServerStoreAlone(t *testing.T) {
	check := doctor.NewBeadsStores(bstVault(t, "dolt"))

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestBeadsStoresProbeIsOKWithTheEmbeddedStoreAlone(t *testing.T) {
	check := doctor.NewBeadsStores(bstVault(t, "embeddeddolt"))

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestBeadsStoresProbeIsFaultyWithBothNamingTheStrayAndTheCure(t *testing.T) {
	dir := bstVault(t, "dolt", "embeddeddolt")
	check := doctor.NewBeadsStores(dir)

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, filepath.Join(dir, ".beads", "embeddeddolt")) {
		t.Fatalf("expected the reason to name the stray store, got %q", reason)
	}
	if !strings.Contains(reason, "beads.env") {
		t.Fatalf("expected the reason to name beads.env, got %q", reason)
	}

	if err := check.Cure(context.Background()); err == nil {
		t.Fatal("expected curing to fail: there is no cure")
	}
}

func TestBeadsStoresProbeIsCannotTellWithoutBeads(t *testing.T) {
	check := doctor.NewBeadsStores(t.TempDir())

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, ".beads") {
		t.Fatalf("expected the reason to name .beads, got %q", reason)
	}
}

func TestBeadsStoresProbeIgnoresTheEnvironment(t *testing.T) {
	t.Setenv("BEADS_DOLT_SERVER_HOST", "")
	check := doctor.NewBeadsStores(bstVault(t, "dolt", "embeddeddolt"))

	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty whatever the environment says, got %s", verdict)
	}
}

func TestBeadsStoresDamperIsZeroWaitCapOne(t *testing.T) {
	wait, capPerEpisode := doctor.NewBeadsStores("").Damper()
	if wait != 0 || capPerEpisode != 1 {
		t.Fatalf("expected 0/1, got %s/%d", wait, capPerEpisode)
	}
	if way := doctor.NewBeadsStores("").WayBack(); !strings.HasPrefix(way, "none: no cure runs") {
		t.Fatalf("expected the way back to say no cure runs, got %q", way)
	}
}

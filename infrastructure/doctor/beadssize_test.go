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

// bsVault makes a temp dir standing in for a vault, with a .beads directory
// holding one file of exactly size bytes. Nothing here reads a real vault.
func bsVault(t *testing.T, size int) string {
	t.Helper()
	dir := t.TempDir()
	beads := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beads, 0o755); err != nil {
		t.Fatalf("making %s: %v", beads, err)
	}
	if err := os.WriteFile(filepath.Join(beads, "data"), make([]byte, size), 0o644); err != nil {
		t.Fatalf("writing the .beads fixture: %v", err)
	}
	return dir
}

func TestBeadsSizeProbeIsOKUnderBudget(t *testing.T) {
	dir := bsVault(t, 50)
	check := &doctor.BeadsSize{Dir: dir, Budget: 100}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestBeadsSizeProbeIsFaultyPastBudgetNamingBytesAndBudget(t *testing.T) {
	dir := bsVault(t, 200)
	check := &doctor.BeadsSize{Dir: dir, Budget: 100}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "200") || !strings.Contains(reason, "100") {
		t.Fatalf("expected the reason to name the bytes and the budget, got %q", reason)
	}

	err := check.Cure(context.Background())
	if err == nil {
		t.Fatal("expected curing to fail: there is no cure")
	}
	if !strings.Contains(err.Error(), "repacks itself") {
		t.Fatalf("expected the cure's error to name the repack that already runs on the next sync, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "GC") {
		t.Fatalf("expected the cure's error to name the tracker's own GC, got %q", err.Error())
	}
	if way := check.WayBack(); !strings.Contains(way, "none") {
		t.Fatalf("expected the way back to say none, got %q", way)
	}
}

func TestBeadsSizeProbeIsCannotTellWhenBeadsIsAbsent(t *testing.T) {
	dir := t.TempDir()
	check := &doctor.BeadsSize{Dir: dir, Budget: 100}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, ".beads") {
		t.Fatalf("expected the reason to name .beads, got %q", reason)
	}
}

func TestBeadsSizeProbeIsCannotTellWhenTheVaultIsAbsent(t *testing.T) {
	check := &doctor.BeadsSize{Dir: filepath.Join(t.TempDir(), "no-such-vault"), Budget: 100}

	verdict, _ := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s", verdict)
	}
}

func TestBeadsSizeProbeReadsTheDefaultBudgetWhenNoneIsSet(t *testing.T) {
	dir := bsVault(t, 50)
	check := doctor.NewBeadsSize(dir)

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok well under the real budget, got %s (%s)", verdict, reason)
	}
}

func TestDefaultBeadsBudgetIsOneAndAHalfGigabytes(t *testing.T) {
	if application.DefaultBeadsBudgetBytes != 1_500_000_000 {
		t.Fatalf("expected the default beads budget to be 1.5 GB, got %d", application.DefaultBeadsBudgetBytes)
	}
}

func TestBeadsSizeDamperIsZeroWaitCapOne(t *testing.T) {
	check := doctor.NewBeadsSize("")
	wait, capPerEpisode := check.Damper()
	if wait != doctor.BeadsSizeDamperWait || capPerEpisode != doctor.BeadsSizeDamperCap {
		t.Fatalf("expected %s/%d, got %s/%d", doctor.BeadsSizeDamperWait, doctor.BeadsSizeDamperCap, wait, capPerEpisode)
	}
}

func TestBeadsSizeCureSaysItFoundNoCache(t *testing.T) {
	t.Parallel()
	check := &doctor.BeadsSize{Dir: bsVault(t, 200), Budget: 100}
	err := check.Cure(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no git-remote-cache found") {
		t.Fatalf("expected the cure to say no cache was found, got %v", err)
	}
}

func TestBeadsSizeCureSaysWhatCachesItFoundUnderEitherLayout(t *testing.T) {
	t.Parallel()
	for _, store := range []string{"dolt", "embeddeddolt"} {
		dir := bsVault(t, 10)
		repo := filepath.Join(dir, ".beads", store, "sf", ".dolt", "git-remote-cache", "h", "repo.git")
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "pack"), make([]byte, 300), 0o644); err != nil {
			t.Fatal(err)
		}
		err := (&doctor.BeadsSize{Dir: dir, Budget: 100}).Cure(context.Background())
		if err == nil || !strings.Contains(err.Error(), "found 1 git-remote-cache clone(s) holding 300 bytes") {
			t.Fatalf("%s: expected the cure to name the cache it found, got %v", store, err)
		}
	}
}

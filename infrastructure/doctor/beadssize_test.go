package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
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

// bsSparseVault is bsVault for sizes too large to write out: the .beads file
// is truncated to size, which reads as that many bytes without using the disk.
func bsSparseVault(t *testing.T, size int64) string {
	t.Helper()
	dir := bsVault(t, 0)
	if err := os.Truncate(filepath.Join(dir, ".beads", "data"), size); err != nil {
		t.Fatalf("sizing the .beads fixture: %v", err)
	}
	return dir
}

// bsConfigured points HOME at a config.toml holding text and returns the
// check over dir with the budget the config says, the way mw doctor builds it.
func bsConfigured(t *testing.T, text, dir string) *doctor.BeadsSize {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.BeadsBudgetEnv, "")
	if text != "" {
		path := filepath.Join(home, config.File)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making the config dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("writing the config file: %v", err)
		}
	}
	budget, err := config.BeadsBudgetBytes()
	if err != nil {
		t.Fatalf("reading the beads budget: %v", err)
	}
	return &doctor.BeadsSize{Dir: dir, Budget: budget}
}

func TestBeadsSizeHoldsToTheBudgetTheConfigSays(t *testing.T) {
	text := "beads_budget_bytes = 3000000000\n"

	verdict, reason := bsConfigured(t, text, bsSparseVault(t, 2_000_000_000)).Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok at 2 GB under a 3 GB budget, got %v: %s", verdict, reason)
	}

	verdict, reason = bsConfigured(t, text, bsSparseVault(t, 3_100_000_000)).Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "3000000000") {
		t.Fatalf("expected faulty at 3.1 GB naming the 3000000000 budget, got %v: %s", verdict, reason)
	}
}

func TestBeadsSizeKeepsOneAndAHalfGigabytesWhenTheConfigSaysNothing(t *testing.T) {
	verdict, reason := bsConfigured(t, "", bsSparseVault(t, 1_400_000_000)).Probe(context.Background())
	if verdict != application.DoctorOK {
		t.Fatalf("expected ok at 1.4 GB under the default budget, got %v: %s", verdict, reason)
	}

	verdict, _ = bsConfigured(t, "", bsSparseVault(t, 1_600_000_000)).Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty at 1.6 GB past the default budget, got %v", verdict)
	}
}

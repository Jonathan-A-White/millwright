package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// batchVault is a vault whose .beads/config.yaml holds config.
func batchVault(t *testing.T, config string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, ".beads", "config.yaml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func hostSays(answer string, err error) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return answer, err }
}

const vaultBatch = "# bd\nissue-prefix: \"mw\"\ndolt.auto-commit: \"batch\"\n"

func TestBeadsBatchIsOKWhileTheVaultSaysOnWhateverTheHostSays(t *testing.T) {
	for _, config := range []string{"", "# dolt.auto-commit: batch\n", "dolt.auto-commit: \"on\"\n"} {
		check := doctor.NewBeadsBatch(batchVault(t, config), hostSays("batch", nil))
		if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
			t.Fatalf("config %q: expected ok, got %s (%s)", config, verdict, reason)
		}
		check = doctor.NewBeadsBatch(batchVault(t, config), hostSays("on", nil))
		if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
			t.Fatalf("config %q, host on: expected ok, got %s (%s)", config, verdict, reason)
		}
	}
}

func TestBeadsBatchIsFaultyOnAHostWhoseBdSaysOnWhileTheVaultSaysBatch(t *testing.T) {
	check := doctor.NewBeadsBatch(batchVault(t, vaultBatch), hostSays("on", nil))

	verdict, reason := check.Probe(context.Background())

	if verdict != application.DoctorFaulty || !strings.Contains(reason, "BD_DOLT_AUTO_COMMIT") {
		t.Fatalf("expected faulty naming the likely cause, got %s (%s)", verdict, reason)
	}
}

func TestBeadsBatchIsOKOnAHostInBatchAndReadsTheNestedForm(t *testing.T) {
	for _, config := range []string{vaultBatch, "dolt:\n  auto-commit: batch\n"} {
		check := doctor.NewBeadsBatch(batchVault(t, config), hostSays("batch", nil))
		if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
			t.Fatalf("config %q: expected ok, got %s (%s)", config, verdict, reason)
		}
		check = doctor.NewBeadsBatch(batchVault(t, config), hostSays("on", nil))
		if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorFaulty {
			t.Fatalf("config %q: expected faulty on a host that says on", config)
		}
	}
}

func TestBeadsBatchCannotTellWhenTheHostsBdCannotBeAsked(t *testing.T) {
	check := doctor.NewBeadsBatch(batchVault(t, vaultBatch), hostSays("", errors.New("bd gone")))

	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s", verdict)
	}
}

func TestBeadsBatchHasNoCureAndNeverChangesAnything(t *testing.T) {
	check := doctor.NewBeadsBatch(batchVault(t, vaultBatch), hostSays("on", nil))

	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "no cure") {
		t.Fatalf("expected no cure, got %v", err)
	}
}

func flushCheck(t *testing.T, config string, home bool, record application.BeadsFlushRecord, now time.Time) *doctor.BeadsFlush {
	t.Helper()
	log := &apptest.FakeFlushLog{}
	if err := log.Record(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	check := doctor.NewBeadsFlush(batchVault(t, config), func(context.Context) (bool, error) { return home, nil }, log)
	check.Now = func() time.Time { return now }
	return check
}

func TestBeadsFlushIsOKWithBatchOnAndAFlushWithinHalfAnHour(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	check := flushCheck(t, vaultBatch, true, application.BeadsFlushRecord{At: now.Add(-29 * time.Minute)}, now)

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestBeadsFlushIsFaultyWhenTheLastFlushFailed(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	check := flushCheck(t, vaultBatch, true, application.BeadsFlushRecord{At: now.Add(-time.Minute), Failed: "server unreachable"}, now)

	verdict, reason := check.Probe(context.Background())

	if verdict != application.DoctorFaulty || !strings.Contains(reason, "server unreachable") {
		t.Fatalf("expected faulty with bd's words, got %s (%s)", verdict, reason)
	}
}

func TestBeadsFlushIsFaultyWhenTheLastFlushIsOverHalfAnHourOldOrThereIsNone(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for name, record := range map[string]application.BeadsFlushRecord{
		"old":  {At: now.Add(-31 * time.Minute)},
		"none": {},
	} {
		check := flushCheck(t, vaultBatch, true, record, now)
		if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
			t.Fatalf("%s: expected faulty, got %s (%s)", name, verdict, reason)
		}
	}
}

func TestBeadsFlushIsOKWhileTheVaultSaysOnAndOnAHostThatIsNotHome(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	stale := application.BeadsFlushRecord{At: now.Add(-5 * time.Hour), Failed: "old"}
	for name, check := range map[string]*doctor.BeadsFlush{
		"vault on":    flushCheck(t, "", true, stale, now),
		"not home":    flushCheck(t, vaultBatch, false, stale, now),
		"nested on":   flushCheck(t, "dolt:\n  auto-commit: on\n", true, stale, now),
		"no config.y": flushCheck(t, "# nothing\n", true, stale, now),
	} {
		if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
			t.Fatalf("%s: expected ok, got %s (%s)", name, verdict, reason)
		}
	}
}

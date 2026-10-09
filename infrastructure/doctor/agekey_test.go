package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

func TestTheAgeKeyProbeCannotTellWithNoHomeFile(t *testing.T) {
	dir := t.TempDir()
	check := doctor.NewAgeKey(&apptest.FakeHomeFile{Missing: true}, "laptop", dir, filepath.Join(dir, "age.key"))

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestTheAgeKeyProbeCannotTellWhenTheKeysPathCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	check := doctor.NewAgeKey(&apptest.FakeHomeFile{Text: "laptop 2026-10-09T00:00:00Z mw@laptop"}, "laptop", dir, "")
	check.PathErr = errors.New("age_key_file is \"age.key\": it must be a full path")

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "full path") {
		t.Fatalf("expected cannot-tell naming the setting, got %s (%s)", verdict, reason)
	}
}

func TestTheAgeKeyProbeIsFaultyOnTheHomeWhenTheKeyIsALink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".sops.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("# placeholder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "age.key")
	if err := os.Symlink(target, key); err != nil {
		t.Fatal(err)
	}
	check := doctor.NewAgeKey(&apptest.FakeHomeFile{Text: "laptop 2026-10-09T00:00:00Z mw@laptop"}, "laptop", dir, key)

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "not a plain file") {
		t.Fatalf("expected faulty, not a plain file, got %s (%s)", verdict, reason)
	}
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "not a plain file") {
		t.Fatalf("expected the cure to fail naming what the probe found, got %v", err)
	}
}

func TestTheAgeKeyProbeIsFaultyOnTheHomeWithNoKeyWhenOnlyTheSecretsFileIsThere(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secrets.enc.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	check := doctor.NewAgeKey(&apptest.FakeHomeFile{Text: "laptop 2026-10-09T00:00:00Z mw@laptop"}, "laptop", dir, filepath.Join(dir, "age.key"))

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
}

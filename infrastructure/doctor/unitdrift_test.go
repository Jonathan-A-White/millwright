package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// driftFixture is a rig checkout with deploy/laptop/<unit> and the directory
// the host's user units are installed in.
type driftFixture struct {
	t         *testing.T
	rig, unit string
	deploy    string
}

func newDriftFixture(t *testing.T) *driftFixture {
	t.Helper()
	root := t.TempDir()
	f := &driftFixture{t: t, rig: root + "/rig", unit: root + "/units"}
	f.deploy = filepath.Join(f.rig, "deploy", "laptop")
	for _, dir := range []string{f.deploy, f.unit} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *driftFixture) write(dir, name, text string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *driftFixture) check() *doctor.UnitDrift {
	return &doctor.UnitDrift{
		UnitDir: f.unit,
		Rigs:    func() (map[string]string, error) { return map[string]string{"sermon-index": f.rig}, nil },
	}
}

func TestADeployUnitWithALineTheInstalledCopyLacksIsOneWarnNamingRigUnitAndLine(t *testing.T) {
	f := newDriftFixture(t)
	f.write(f.deploy, "sermon-index.service", "[Service]\nExecStart=/bin/true\nEnvironment=SERMONS=/data\n")
	f.write(f.unit, "sermon-index.service", "[Service]\nExecStart=/bin/true\n")

	verdict, reason := f.check().Probe(context.Background())
	if verdict != application.DoctorCannotTell {
		t.Fatalf("expected the warn level (cannot-tell), got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"sermon-index", "sermon-index.service", "Environment=SERMONS=/data"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to name %q, got %q", want, reason)
		}
	}
	if strings.Contains(reason, "ExecStart") {
		t.Errorf("a line both copies share must not be named, got %q", reason)
	}
	if strings.Contains(strings.TrimSpace(reason), "\n") {
		t.Errorf("expected one line, got %q", reason)
	}
}

func TestADifferenceOnlyInCommentsAndBlankLinesIsQuiet(t *testing.T) {
	f := newDriftFixture(t)
	f.write(f.deploy, "a.service", "# installed by scripts/install-laptop.sh\n[Service]\nExecStart=/bin/true\n; old\n\n")
	f.write(f.unit, "a.service", "[Service]\n# another note\nExecStart=/bin/true\n")

	if verdict, reason := f.check().Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected ok and quiet, got %s (%s)", verdict, reason)
	}
}

func TestAUnitWithNoInstalledCopyIsQuiet(t *testing.T) {
	f := newDriftFixture(t)
	f.write(f.deploy, "b.service", "[Service]\nExecStart=/bin/true\n")

	if verdict, reason := f.check().Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Fatalf("expected ok and quiet, got %s (%s)", verdict, reason)
	}
}

func TestTheInstalledCopyOnlyHasToMatchOneOfSeveralDeployCopies(t *testing.T) {
	f := newDriftFixture(t)
	other := filepath.Join(f.rig, "deploy", "vps")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	f.write(f.deploy, "c.service", "[Service]\nExecStart=/laptop\n")
	f.write(other, "c.service", "[Service]\nExecStart=/vps\n")
	f.write(f.unit, "c.service", "[Service]\nExecStart=/laptop\n")

	if verdict, reason := f.check().Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestADroppedLineIsNamedAsInstalledOnly(t *testing.T) {
	f := newDriftFixture(t)
	f.write(f.deploy, "d.service", "[Service]\nExecStart=/new\n")
	f.write(f.unit, "d.service", "[Service]\nExecStart=/old\n")

	verdict, reason := f.check().Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "/new") || !strings.Contains(reason, "/old") {
		t.Fatalf("expected both differing lines named, got %s (%s)", verdict, reason)
	}
}

func TestTheDriftCheckChangesNothingAndItsCureRefuses(t *testing.T) {
	f := newDriftFixture(t)
	f.write(f.deploy, "e.service", "[Service]\nExecStart=/new\n")
	f.write(f.unit, "e.service", "[Service]\nExecStart=/old\n")
	check := f.check()
	check.Probe(context.Background())

	body, err := os.ReadFile(filepath.Join(f.unit, "e.service"))
	if err != nil || string(body) != "[Service]\nExecStart=/old\n" {
		t.Fatalf("the installed copy changed: %q %v", body, err)
	}
	if err := check.Cure(context.Background()); err == nil {
		t.Fatal("expected the cure to refuse: installing a unit is a person's step")
	}
}

func TestTheDriftCheckCannotTellWhenTheRigsCannotBeRead(t *testing.T) {
	check := &doctor.UnitDrift{UnitDir: t.TempDir(), Rigs: func() (map[string]string, error) { return nil, errors.New("no config") }}
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s", verdict)
	}
}

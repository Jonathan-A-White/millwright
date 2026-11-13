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
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

func rigTestsBoost(rigs, tests map[string]string) *doctor.RigTests {
	check := doctor.NewRigTests(rigs, tests, nil)
	check.Home = &apptest.FakeHomeFile{Text: "desktop 2026-09-29T00:10:00Z mw@desktop"}
	check.Host = "cloud1"
	return check
}

func TestRigTestsNamesARigWithNoTestsLineOnABoost(t *testing.T) {
	check := rigTestsBoost(
		map[string]string{"millwright": "/h/millwright", "lampas": "/h/lampas", "postern": "/h/postern", "argus": "/h/argus"},
		map[string]string{"argus": "npm test", "postern": "  "})

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, "lampas, postern") || strings.Contains(reason, "argus") || strings.Contains(reason, "millwright") {
		t.Fatalf("expected exactly lampas and postern named, got %q", reason)
	}
}

func TestRigTestsIsOKWhenEveryRigHasALine(t *testing.T) {
	check := rigTestsBoost(
		map[string]string{"millwright": "/h/millwright", "lampas": "/h/lampas"},
		map[string]string{"lampas": "npm test"})

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestRigTestsIsOKOnTheHome(t *testing.T) {
	check := rigTestsBoost(map[string]string{"lampas": "/h/lampas"}, nil)
	check.Host = "desktop"

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok on the home, got %s (%s)", verdict, reason)
	}
}

func TestRigTestsCannotTellWhenTheConfigDoesNotRead(t *testing.T) {
	check := rigTestsBoost(nil, nil)
	check.Err = errors.New("config.toml: bad table")

	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell, got %s", verdict)
	}
}

func TestRigTestsHasNoCureWithoutTheVaultsFile(t *testing.T) {
	if err := rigTestsBoost(nil, nil).Cure(context.Background()); err == nil {
		t.Fatal("expected no cure")
	}
}

// rigTestsCurable is a Boost whose config.toml has [tests] for argus only and a
// vault whose hosts/boost-config.toml names argus and lampas.
func rigTestsCurable(t *testing.T, config, vault string) (*doctor.RigTests, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	targetPath := filepath.Join(dir, "vault", application.BoostConfigFile)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte(vault), 0o644); err != nil {
		t.Fatal(err)
	}
	check := rigTestsBoost(
		map[string]string{"millwright": "/h/millwright", "lampas": "/h/lampas", "argus": "/h/argus"},
		map[string]string{"argus": "npm test"})
	check.Config, check.Target = configfile.New(configPath), configfile.New(targetPath)
	return check, configPath
}

func TestRigTestsCuresARigTheVaultNamesAndThenIsOK(t *testing.T) {
	check, configPath := rigTestsCurable(t,
		"[rigs]\nlampas = \"/h/lampas\"\n\n[tests]\nargus = \"npm test\"\n",
		"[tests]\nargus = \"npm test\"\nlampas = \"npm run check\"\n")
	ctx := context.Background()

	if verdict, reason := check.Probe(ctx); verdict != application.DoctorFaulty || !strings.Contains(reason, "lampas") {
		t.Fatalf("expected faulty naming lampas, got %s (%s)", verdict, reason)
	}
	if err := check.Cure(ctx); err != nil {
		t.Fatalf("expected the cure to set lampas's line, got %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `lampas = "npm run check"`) || !strings.Contains(string(data), `argus = "npm test"`) {
		t.Fatalf("expected lampas's line from the vault beside argus's, got:\n%s", data)
	}
	if verdict, reason := check.Probe(ctx); verdict != application.DoctorOK {
		t.Fatalf("expected ok after the cure, got %s (%s)", verdict, reason)
	}
}

func TestRigTestsCureNamesARigTheVaultDoesNotName(t *testing.T) {
	check, configPath := rigTestsCurable(t,
		"[tests]\nargus = \"npm test\"\n",
		"[tests]\nargus = \"npm test\"\n")
	ctx := context.Background()

	if verdict, _ := check.Probe(ctx); verdict != application.DoctorFaulty {
		t.Fatal("expected faulty")
	}
	err := check.Cure(ctx)
	if err == nil || !strings.Contains(err.Error(), "lampas") {
		t.Fatalf("expected no cure, naming lampas, got %v", err)
	}
	if data, _ := os.ReadFile(configPath); strings.Contains(string(data), "lampas") {
		t.Fatalf("expected the config left alone, got:\n%s", data)
	}
}

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

func boostConfigOnTheHome(t *testing.T, config, target string) *doctor.BoostConfig {
	t.Helper()
	return boostConfigOn(t, "laptop", config, target)
}

func boostConfigOnABoost(t *testing.T, config, target string) *doctor.BoostConfig {
	t.Helper()
	return boostConfigOn(t, "desktop", config, target)
}

// boostConfigOn is the check on the host named host, the home being "laptop".
func boostConfigOn(t *testing.T, host, config, target string) *doctor.BoostConfig {
	t.Helper()
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if text != "" {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	return doctor.NewBoostConfig(&apptest.FakeHomeFile{Text: "laptop 2026-10-15T00:00:00Z mw@laptop"}, host,
		configfile.New(write("config.toml", config)), configfile.New(write("vault/"+application.BoostConfigFile, target)))
}

func TestBoostConfigCureNamesWhatTheProbeFound(t *testing.T) {
	check := boostConfigOnTheHome(t, "[tests]\nargus = \"npm test\"\n", "[tests]\n")
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorFaulty || !strings.Contains(reason, "[tests] argus: missing") {
		t.Fatalf("expected faulty naming argus, got %s (%s)", verdict, reason)
	}
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "argus") || !strings.Contains(err.Error(), "mw config export-boost") {
		t.Fatalf("expected no cure, naming argus and the command, got %v", err)
	}
}

func TestBoostConfigCannotTellWithoutTheConfigOrTheHome(t *testing.T) {
	check := boostConfigOnTheHome(t, "[tests]\n", "[tests]\n")
	check.ConfigErr = errors.New("no home directory")
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell with no config path, got %s", verdict)
	}

	check = boostConfigOnTheHome(t, "", "[tests]\nargus = \"npm test\"\n")
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell with no config file, got %s (%s)", verdict, reason)
	}

	check = boostConfigOnTheHome(t, "[tests]\n", "[tests]\n")
	check.Home = &apptest.FakeHomeFile{Missing: true}
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell with no home file, got %s", verdict)
	}
}

func TestBoostConfigCannotTellWhenTheHomesConfigHoldsANonString(t *testing.T) {
	check := boostConfigOnTheHome(t, "[tests]\nargus = 5\n", "[tests]\n")
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(reason, "argus") {
		t.Fatalf("expected cannot-tell naming argus, got %s (%s)", verdict, reason)
	}
}

func TestBoostConfigOnABoostIsFaultyForADifferingTestsLine(t *testing.T) {
	check := boostConfigOnABoost(t, "[tests]\nmillwright = \"make test\"\n", "[tests]\nmillwright = \"make test && scripts/check-codemap.sh\"\n")
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{`tests.millwright`, `here "make test"`, `vault "make test && scripts/check-codemap.sh"`, `mw config set tests.millwright "make test && scripts/check-codemap.sh"`} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason lacks %q: %s", want, reason)
		}
	}
}

func TestBoostConfigOnABoostIsFaultyForADifferingChangedSpecsLine(t *testing.T) {
	check := boostConfigOnABoost(t,
		"[tests]\nlampas = \"npm test\"\n\n[changed_specs]\nlampas = \"npx playwright test {specs}\"\n",
		"[tests]\nlampas = \"npm test\"\n\n[changed_specs]\nlampas = \"npx playwright test --project=shots {specs}\"\n")
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "changed_specs.lampas") || strings.Contains(reason, "tests.lampas") {
		t.Fatalf("expected faulty naming only changed_specs.lampas, got %s (%s)", verdict, reason)
	}
	if !strings.Contains(reason, `mw config set changed_specs.lampas`) {
		t.Errorf("reason lacks the fix: %s", reason)
	}
}

func TestBoostConfigOnTheHomeCountsTheSessionDenyList(t *testing.T) {
	home := "[session]\ndeny = [\"Bash(scp *)\", \"Read(~/.ssh/**)\"]\n"
	for name, c := range map[string]struct{ target, want string }{
		"missing":       {"[tests]\n", "[session] deny: missing"},
		"differs":       {"[session]\ndeny = [\"Bash(scp *)\"]\n", "[session] deny: differs"},
		"order matters": {"[session]\ndeny = [\"Read(~/.ssh/**)\", \"Bash(scp *)\"]\n", "[session] deny: differs"},
	} {
		t.Run(name, func(t *testing.T) {
			verdict, reason := boostConfigOnTheHome(t, home, c.target).Probe(context.Background())
			if verdict != application.DoctorFaulty || !strings.Contains(reason, c.want) {
				t.Fatalf("expected faulty naming %q, got %s (%s)", c.want, verdict, reason)
			}
		})
	}
	if verdict, reason := boostConfigOnTheHome(t, home, home).Probe(context.Background()); verdict != application.DoctorOK {
		t.Errorf("a level deny list should be ok, got %s (%s)", verdict, reason)
	}
	if verdict, reason := boostConfigOnTheHome(t, "[session]\ndeny = 5\n", home).Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Errorf("a deny that is not an array should be cannot-tell, got %s (%s)", verdict, reason)
	}
}

func TestBoostConfigOnABoostIsFaultyForADifferingTestPartsTable(t *testing.T) {
	check := boostConfigOnABoost(t,
		"[test_parts.lampas]\n\"install\" = \"npm ci\"\n\"vitest 1/2\" = \"npx vitest run --shard 1/2\"\n",
		"[test_parts.lampas]\n\"install\" = \"npm ci\"\n\"vitest 1/6\" = \"npx vitest run --shard 1/6\"\n")
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"test_parts.lampas", "vitest 1/2", "vitest 1/6"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason lacks %q: %s", want, reason)
		}
	}
}

func TestBoostConfigOnABoostIsOkWhenLevelOrOnlyLackingALine(t *testing.T) {
	level := "[tests]\nargus = \"npm test\"\n\n[test_parts.lampas]\n\"install\" = \"npm ci\"\n"
	if verdict, reason := boostConfigOnABoost(t, level, level).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok when level, got %s (%s)", verdict, reason)
	}
	here := "[tests]\nlampas = \"x\"\n"
	vault := here + "argus = \"npm test\"\n\n[changed_specs]\nargus = \"npx playwright test {specs}\"\n\n[test_parts.argus]\n\"a\" = \"b\"\n"
	if verdict, reason := boostConfigOnABoost(t, here, vault).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok when the Boost only lacks lines, got %s (%s)", verdict, reason)
	}
}

func TestBoostConfigOnABoostIsOkWithNothingToCompare(t *testing.T) {
	if verdict, reason := boostConfigOnABoost(t, "", "[tests]\nargus = \"npm test\"\n").Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok with no config file, got %s (%s)", verdict, reason)
	}
	if verdict, reason := boostConfigOnABoost(t, "[tests]\nargus = \"npm test\"\n", "").Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok with no vault file, got %s (%s)", verdict, reason)
	}
}

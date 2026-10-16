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
	return doctor.NewBoostConfig(&apptest.FakeHomeFile{Text: "laptop 2026-10-15T00:00:00Z mw@laptop"}, "laptop",
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

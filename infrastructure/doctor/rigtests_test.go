package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
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

func TestRigTestsNeverCures(t *testing.T) {
	if err := rigTestsBoost(nil, nil).Cure(context.Background()); err == nil {
		t.Fatal("expected no cure")
	}
}

package doctor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

const (
	lampasWithSeptuagint    = "npm ci && npm run lxx:ship -- --out dist/data-lxx && npm run build"
	lampasWithoutSeptuagint = "npm ci && npm run build"
)

// alaCheck is the check on the desktop, which holds the lines in own, over the
// hosts laptop and cloud1, each with the lines in published as its last sync
// left them (a host not in published left none).
func alaCheck(own map[string]string, published map[string]map[string]string) *doctor.AfterLandingAgrees {
	notes := apptest.NewFakeDoctorNotes()
	for host, lines := range published {
		_ = notes.SetNote(context.Background(), application.AfterLandingKey(host), application.AfterLandingFingerprints(lines))
	}
	check := doctor.NewAfterLandingAgrees("desktop", notes)
	check.Own = func() (map[string]string, error) { return own, nil }
	check.Hosts = func(context.Context) ([]string, error) { return []string{"laptop", "cloud1"}, nil }
	return check
}

func TestAfterLandingAgreesIsFaultyWhenTwoHostsHoldDifferentLinesForARigNamingTheRigAndBothHosts(t *testing.T) {
	check := alaCheck(
		map[string]string{"lampas": lampasWithoutSeptuagint},
		map[string]map[string]string{"laptop": {"lampas": lampasWithSeptuagint}},
	)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"lampas", "desktop", "laptop", "[after_landing]"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to hold %q, got %q", want, reason)
		}
	}
	if strings.Contains(reason, "lxx:ship") || strings.Contains(reason, "npm") {
		t.Errorf("the line must never be said, only that it differs: %q", reason)
	}
}

func TestAfterLandingAgreesIsOkWhenTheHostsHoldTheSameLine(t *testing.T) {
	check := alaCheck(
		map[string]string{"lampas": lampasWithSeptuagint},
		map[string]map[string]string{"laptop": {"lampas": lampasWithSeptuagint}, "cloud1": {"lampas": lampasWithSeptuagint}},
	)
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestAfterLandingAgreesIsOkWhenAHostHasNoLineForTheRig(t *testing.T) {
	// The laptop holds a line for a rig the desktop has none for, and the
	// desktop one the laptop has none for: neither is compared.
	check := alaCheck(
		map[string]string{"lampas": lampasWithSeptuagint},
		map[string]map[string]string{"laptop": {"postern": "npm run build"}},
	)
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestAfterLandingAgreesIsOkWhenAHostHasLeftNoFingerprintsOrThisHostHasNoLine(t *testing.T) {
	if verdict, reason := alaCheck(map[string]string{"lampas": lampasWithSeptuagint}, nil).Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok when no other host has published, got %s (%s)", verdict, reason)
	}
	check := alaCheck(nil, map[string]map[string]string{"laptop": {"lampas": lampasWithSeptuagint}})
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok when this host has no line, got %s (%s)", verdict, reason)
	}
}

func TestAfterLandingAgreesNamesEachRigAndHostThatDiffers(t *testing.T) {
	check := alaCheck(
		map[string]string{"lampas": lampasWithoutSeptuagint, "postern": "same"},
		map[string]map[string]string{
			"laptop": {"lampas": lampasWithSeptuagint, "postern": "same"},
			"cloud1": {"lampas": lampasWithoutSeptuagint, "postern": "other"},
		},
	)
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, got %s (%s)", verdict, reason)
	}
	for _, want := range []string{"lampas", "laptop", "postern", "cloud1"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the reason to hold %q, got %q", want, reason)
		}
	}
}

func TestAfterLandingAgreesCannotTellWhenThisHostsLinesOrTheHostsCannotBeRead(t *testing.T) {
	check := alaCheck(nil, nil)
	check.Own = func() (map[string]string, error) { return nil, errors.New("config unreadable") }
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorCannotTell || !strings.Contains(reason, "config unreadable") {
		t.Fatalf("expected cannot-tell for an unreadable config, got %s (%s)", verdict, reason)
	}
	check = alaCheck(map[string]string{"lampas": "x"}, nil)
	check.Hosts = func(context.Context) ([]string, error) { return nil, errors.New("no hosts") }
	if verdict, _ := check.Probe(context.Background()); verdict != application.DoctorCannotTell {
		t.Fatalf("expected cannot-tell for hosts that cannot be listed, got %s", verdict)
	}
}

func TestAfterLandingAgreesIsFaultyForADifferingHostEvenWhenAnotherCannotBeRead(t *testing.T) {
	check := alaCheck(
		map[string]string{"lampas": lampasWithoutSeptuagint},
		map[string]map[string]string{"laptop": {"lampas": lampasWithSeptuagint}},
	)
	notes := apptest.NewFakeDoctorNotes()
	_ = notes.SetNote(context.Background(), application.AfterLandingKey("laptop"), application.AfterLandingFingerprints(map[string]string{"lampas": lampasWithSeptuagint}))
	check.Notes = erringFor{notes, application.AfterLandingKey("cloud1")}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "laptop") {
		t.Fatalf("expected faulty naming laptop, got %s (%s)", verdict, reason)
	}
}

// erringFor reads every note from its fake but fails on one key.
type erringFor struct {
	*apptest.FakeDoctorNotes
	key string
}

func (e erringFor) Note(ctx context.Context, key string) (string, error) {
	if key == e.key {
		return "", errors.New("unreadable")
	}
	return e.FakeDoctorNotes.Note(ctx, key)
}

func TestAfterLandingAgreesOnlyReportsAndNeverCuresAnything(t *testing.T) {
	check := alaCheck(map[string]string{"lampas": lampasWithoutSeptuagint}, map[string]map[string]string{"laptop": {"lampas": lampasWithSeptuagint}})
	check.Probe(context.Background())
	if err := check.Cure(context.Background()); err == nil || !strings.Contains(err.Error(), "lampas") {
		t.Fatalf("expected a cure that fails saying what differs, got %v", err)
	}
}

// A fingerprint a box that is gone left, or one written before the box now
// holding the name was made, is none of that box's: it is not compared.
func TestAfterLandingAgreesLeavesOutTheNoteOfAnOlderBox(t *testing.T) {
	ctx := context.Background()
	made := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name    string
		synced  time.Time
		verdict application.Verdict
	}{
		{"a note from before the box was made", made.Add(-time.Hour), application.DoctorOK},
		{"a note from after the box was made", made.Add(time.Hour), application.DoctorFaulty},
	} {
		t.Run(c.name, func(t *testing.T) {
			check := alaCheck(
				map[string]string{"lampas": lampasWithoutSeptuagint},
				map[string]map[string]string{"cloud1": {"lampas": lampasWithSeptuagint}},
			)
			notes := check.Notes.(*apptest.FakeDoctorNotes)
			_ = notes.SetNote(ctx, application.LastSyncKey("cloud1"), c.synced.Format(application.LastSyncFormat))
			check.Book = &apptest.FakeCloudBook{State: application.CloudState{Boxes: []application.CloudBox{{Name: "cloud1", Up: made}}}}
			check.Plan = application.CloudPlan{Prefix: "cloud"}
			if verdict, reason := check.Probe(ctx); verdict != c.verdict {
				t.Fatalf("expected %s, got %s (%s)", c.verdict, verdict, reason)
			}
		})
	}
}

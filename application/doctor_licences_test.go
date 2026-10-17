package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func licenceReport(t *testing.T, rightsJSON string) application.LicenceReport {
	t.Helper()
	entries, err := application.ParseRightsJSON([]byte(rightsJSON))
	if err != nil {
		t.Fatalf("reading the backend's licence map: %v", err)
	}
	return application.ReportLicences(entries)
}

const cleanRights = `{"collections":[
 {"collection":"postern","kind":"cockpit","routes":["GET /api/view","PUT /api/prompts/{name}"]},
 {"collection":"cairn","kind":"app","app":"cairn","routes":["GET /api/me","POST /api/blobs"]},
 {"collection":"spellforge-leaderboard-testnet","kind":"app","app":"spellforge","routes":["GET /api/me"]}
]}`

func TestACleanLicenceMapPrintsOneLinePerCollectionAndWarnsOfNothing(t *testing.T) {
	report := licenceReport(t, cleanRights)

	if report.Faulty() {
		t.Errorf("expected no warning, got %v", report.Warnings)
	}
	if len(report.Lines) != 3 {
		t.Fatalf("expected a line per collection (3), got %v", report.Lines)
	}
	postern, cairn := report.Lines[0], report.Lines[1]
	if !strings.HasPrefix(postern, "postern -> cockpit:") || !strings.Contains(postern, "PUT /api/prompts/{name}") {
		t.Errorf("expected the cockpit line to name the routes the backend gave, got %q", postern)
	}
	if !strings.HasPrefix(cairn, "cairn -> app cairn:") || !strings.Contains(cairn, "POST /api/blobs") {
		t.Errorf("expected the app line to name its app and what it opens, got %q", cairn)
	}
	if strings.Contains(cairn, "GET /api/view") {
		t.Errorf("a line names only the routes the backend gave, got %q", cairn)
	}
	if !strings.HasSuffix(report.String(), "ok: every licence collection opens what it should\n") {
		t.Errorf("expected the ok line, got %q", report.String())
	}
}

func TestACollectionListedAsCockpitAndAppIsWarned(t *testing.T) {
	// The backend lists the cockpit collections, then the app collections: a
	// collection in both shows twice, and as an app's both times.
	report := licenceReport(t, `{"collections":[
 {"collection":"postern","kind":"cockpit","routes":["GET /api/view"]},
 {"collection":"spell","kind":"app","app":"spellforge","routes":["GET /api/me"]},
 {"collection":"spell","kind":"app","app":"spellforge","routes":["GET /api/me"]}
]}`)

	if !report.Faulty() || len(report.Warnings) != 1 {
		t.Fatalf("expected one warning, got %v", report.Warnings)
	}
	warning := report.Warnings[0]
	if !strings.Contains(warning, "spell is a cockpit collection") || !strings.Contains(warning, "cockpit key") || !strings.Contains(warning, "spellforge") {
		t.Errorf("expected the warning to name the collection, the cockpit key and the app, got %q", warning)
	}
	if len(report.Lines) != 2 {
		t.Errorf("expected a line per collection, once each, got %v", report.Lines)
	}
}

func TestOneCollectionForTwoAppsIsWarned(t *testing.T) {
	report := licenceReport(t, `{"collections":[
 {"collection":"cairn","kind":"app","app":"cairn,spellforge","routes":["GET /api/me"]}
]}`)

	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "cairn is mapped to two apps (cairn and spellforge)") {
		t.Fatalf("expected one warning naming both apps, got %v", report.Warnings)
	}
}

func TestAMapOfNoCollectionsIsRefused(t *testing.T) {
	for _, text := range []string{`{"collections":[]}`, `{}`, `not json`, ``, `[1]`} {
		if _, err := application.ParseRightsJSON([]byte(text)); err == nil {
			t.Errorf("expected %q to be refused", text)
		}
	}
}

func TestAKindThatIsNeitherCockpitNorAppIsRefused(t *testing.T) {
	if _, err := application.ParseRightsJSON([]byte(`{"collections":[{"collection":"x","kind":"mill","routes":[]}]}`)); err == nil {
		t.Error("expected an unknown kind to be refused, not guessed at")
	}
}

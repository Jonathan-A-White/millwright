package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func licenceReport(t *testing.T, env string) application.LicenceReport {
	t.Helper()
	m, err := application.ParseLicenceMap(env)
	if err != nil {
		t.Fatalf("reading the backend's environment: %v", err)
	}
	return m.Report()
}

func TestACleanLicenceMapPrintsOneLinePerCollectionAndWarnsOfNothing(t *testing.T) {
	report := licenceReport(t, "POSTERN_COLLECTIONS=postern\nPOSTERN_APPS=cairn=cairn,spellforge-leaderboard-testnet=spellforge\n")

	if report.Faulty() {
		t.Errorf("expected no warning, got %v", report.Warnings)
	}
	if len(report.Lines) != 3 {
		t.Fatalf("expected a line per collection (3), got %v", report.Lines)
	}
	postern, cairn := report.Lines[0], report.Lines[1]
	if !strings.HasPrefix(postern, "postern -> cockpit:") || !strings.Contains(postern, "PUT /api/prompts/{name}") {
		t.Errorf("expected the cockpit line to open every route, got %q", postern)
	}
	if !strings.HasPrefix(cairn, "cairn -> app cairn:") || !strings.Contains(cairn, "POST /api/blobs") {
		t.Errorf("expected the app line to name its app and what it opens, got %q", cairn)
	}
	if strings.Contains(cairn, "GET /api/view") || strings.Contains(cairn, "POST /api/broadcast") {
		t.Errorf("an app key must not open the cockpit's routes, got %q", cairn)
	}
}

func TestACollectionOfTheCockpitMappedToAnAppIsWarned(t *testing.T) {
	report := licenceReport(t, "POSTERN_COLLECTIONS=postern,spellforge-leaderboard-testnet\nPOSTERN_APPS=spellforge-leaderboard-testnet=spellforge\n")

	if !report.Faulty() || len(report.Warnings) != 1 {
		t.Fatalf("expected one warning, got %v", report.Warnings)
	}
	warning := report.Warnings[0]
	if !strings.Contains(warning, "spellforge-leaderboard-testnet") || !strings.Contains(warning, "cockpit key") || !strings.Contains(warning, "spellforge") {
		t.Errorf("expected the warning to name the collection, the cockpit key and the app, got %q", warning)
	}
}

func TestTheDefaultCockpitCollectionsApplyWhenTheEnvironmentNamesNone(t *testing.T) {
	// The backend's own default is postern,spellforge-leaderboard-testnet.
	report := licenceReport(t, "POSTERN_APPS=spellforge-leaderboard-testnet=spellforge\n")

	if !report.Faulty() {
		t.Errorf("expected the default cockpit collection to be warned of, got lines %v", report.Lines)
	}
}

func TestTwoAppsMappedToOneCollectionAreWarned(t *testing.T) {
	report := licenceReport(t, "POSTERN_COLLECTIONS=postern\nPOSTERN_APPS=cairn=cairn,cairn=spellforge\n")

	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "cairn") || !strings.Contains(report.Warnings[0], "spellforge") {
		t.Fatalf("expected one warning naming both apps, got %v", report.Warnings)
	}
}

func TestTheEnvironmentIsReadAsASystemdFileIs(t *testing.T) {
	m, err := application.ParseLicenceMap("# a comment\nPOSTERN_ADDR=127.0.0.1:1\nPOSTERN_COLLECTIONS=\"postern\"\nexport POSTERN_APPS='cairn=cairn'\n")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := m.Report().Lines; len(got) != 2 {
		t.Errorf("expected postern and cairn, got %v", got)
	}
}

func TestAnAppPairThatIsNotCollectionEqualsAppIsRefused(t *testing.T) {
	if _, err := application.ParseLicenceMap("POSTERN_APPS=cairn\n"); err == nil {
		t.Error("expected POSTERN_APPS=cairn to be refused")
	}
}

func TestRightsFromCollectionsFollowTheBackendsRule(t *testing.T) {
	apps := map[string]string{"cairn": "cairn"}
	if r := application.RightsFromCollections([]string{"cairn"}, apps); r.Cockpit || len(r.Apps) != 1 {
		t.Errorf("a key holding only an app's collection is an app key, got %+v", r)
	}
	if r := application.RightsFromCollections([]string{"cairn", "postern"}, apps); !r.Cockpit {
		t.Errorf("a key holding a cockpit collection too is a cockpit key, got %+v", r)
	}
}

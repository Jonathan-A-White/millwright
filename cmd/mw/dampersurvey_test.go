package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// slowMakeVault is a vault whose cloud book holds a box that has been made for
// 56 minutes, and a config with a [cloud] table, as on a host that runs boxes.
func slowMakeVault(t *testing.T, now time.Time) string {
	t.Helper()
	mwConfig(t, "host = \"laptop\"\n\n[cloud]\ncommand = \"/opt/millwright/contrib/vultr-boost\"\n")
	dir := t.TempDir()
	book := filepath.Join(dir, vault.CloudBookPath)
	if err := os.MkdirAll(filepath.Dir(book), 0o755); err != nil {
		t.Fatal(err)
	}
	up := now.Add(-56 * time.Minute).Format(time.RFC3339)
	raw := `{"month":"2026-11","boxes":[{"name":"cloud1","up":"` + up + `","making":true,"vultr":"active locked"}]}`
	if err := os.WriteFile(book, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The cloud make alarm of mw-gq6.1113 shows wherever a DamperSurvey is judged,
// not in mw status alone (mw-gq6.1122): mw doctor, mw report, mw report send,
// mw stuck --dampers and the Postern view's review count each build their survey
// through damperSurvey, so the one constructor holds the book.
func TestEveryDamperSurveyHoldsTheCloudBookAndRingsTheSlowMake(t *testing.T) {
	now := time.Date(2026, 11, 12, 16, 53, 0, 0, time.UTC)
	dir := slowMakeVault(t, now)
	for _, surface := range []string{"mw doctor", "mw report", "mw report send", "mw stuck --dampers", "the Postern view"} {
		t.Run(surface, func(t *testing.T) {
			survey, err := damperSurvey(mwGateway(dir, "laptop"), dir, "laptop")
			if err != nil {
				t.Fatal(err)
			}
			if survey.Cloud == nil {
				t.Fatal("expected the survey to hold the cloud book, got none")
			}
			// The tracker is a fake: the alarm is judged from the book alone.
			fake := apptest.NewFakeTracker()
			survey.Tracker, survey.Notes, survey.Graph, survey.Proposals = fake, fake, nil, nil
			survey.Stuck, survey.NeedsCounts = nil, nil
			report, err := survey.Dampers(context.Background(), application.WorkInHand{}, now)
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, a := range report.Alarms {
				lines = append(lines, a.Line())
			}
			text := strings.Join(lines, "\n")
			if !strings.Contains(text, "cloud make: cloud1") || !strings.Contains(text, "Vultr says active locked") {
				t.Errorf("expected a cloud make line naming cloud1 and Vultr's status, got %q", text)
			}
		})
	}
}

// A home with no [cloud] table has no book to read and no alarm.
func TestADamperSurveyHoldsNoCloudBookWithoutACloudTable(t *testing.T) {
	mwConfig(t, "host = \"laptop\"\n")
	dir := t.TempDir()
	survey, err := damperSurvey(mwGateway(dir, "laptop"), dir, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if survey.Cloud != nil {
		t.Errorf("expected no cloud book without a [cloud] table, got %v", survey.Cloud)
	}
}

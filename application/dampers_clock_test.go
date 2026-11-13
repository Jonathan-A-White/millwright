package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// DamperCounts judges the alarms at the survey's injected clock, not the
// host's: a make begun 56 minutes before the survey's Now is slow whatever the
// real time is (mw-gq6.1219).
func TestDamperCountsJudgesAtTheSurveysInjectedClock(t *testing.T) {
	now := time.Date(2026, 11, 12, 16, 53, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11",
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-56 * time.Minute), Making: true, Vultr: "active locked"}}}}
	survey := application.DamperSurvey{Tracker: tracker, Notes: tracker, Cloud: book,
		Now: func() time.Time { return now }}
	report, err := survey.DamperCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, a := range report.Alarms {
		lines = append(lines, a.Line())
	}
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "cloud1 has been made for 56m") {
		t.Errorf("expected the make judged slow at 56m by the injected clock, got %q", text)
	}
}

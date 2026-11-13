package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1200: a box's integrity mark (a corrupt git object, or a session that
// died with a segmentation fault) was read only by the next fault; mw status
// now names a box whose mark is under an hour old.

type fakeIntegrity struct {
	marks map[string]string
	errs  map[string]error
}

func (f fakeIntegrity) IntegrityMark(_ context.Context, box string) (string, error) {
	return f.marks[box], f.errs[box]
}

func integrityStatus(t *testing.T, now time.Time, boxes []application.CloudBox, marks application.BoxIntegrity) string {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: boxes}}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder",
		Cloud: book, CloudPlan: cloudPlan, BoxIntegrity: marks, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report.String()
}

func TestStatusNamesABoxWithASegmentationFaultMarkUnderAnHourOld(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	up := now.Add(-5 * time.Hour)
	marks := fakeIntegrity{marks: map[string]string{
		"cloud1": now.Add(-20 * time.Minute).Format(time.RFC3339),
		"cloud2": now.Add(-2 * time.Hour).Format(time.RFC3339),
	}}
	text := integrityStatus(t, now, []application.CloudBox{
		{Name: "cloud1", Up: up}, {Name: "cloud2", Up: up}, {Name: "cloud3", Up: up},
	}, marks)
	words := strings.Join(strings.Fields(text), " ") // the line is wrapped on screen
	if !strings.Contains(words, "cloud1 met a corrupt git object or a segmentation fault at 11-13 11:40Z") ||
		!strings.Contains(words, "mw cloud down cloud1") {
		t.Errorf("expected cloud1 named for its fresh mark, with the step, in:\n%s", text)
	}
	for _, not := range []string{"cloud2 met a corrupt", "cloud3 met a corrupt"} {
		if strings.Contains(words, not) {
			t.Errorf("expected no %q (a mark over an hour old, or none) in:\n%s", not, text)
		}
	}
}

func TestAnIntegrityMarkThatCannotBeReadCostsOnlyItsBoxesLine(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	marks := fakeIntegrity{errs: map[string]error{"cloud1": errors.New("ssh: no route")}}
	text := integrityStatus(t, now, []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}, marks)
	if !strings.Contains(text, "its integrity mark could not be read: ssh: no route") {
		t.Errorf("expected why the mark could not be read in:\n%s", text)
	}
}

func TestABoxBeingMadeIsNotAskedForItsIntegrityMark(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	marks := fakeIntegrity{marks: map[string]string{"cloud1": now.Add(-time.Minute).Format(time.RFC3339)}}
	text := integrityStatus(t, now, []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Minute), Making: true}}, marks)
	if strings.Contains(text, "segmentation fault at") {
		t.Errorf("expected a box still being made left alone in:\n%s", text)
	}
}

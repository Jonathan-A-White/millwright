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

var cloudPlan = application.CloudPlan{MaxBoxes: 2, MonthlyCapUSD: 75, HourlyUSD: 0.132, Idle: 30 * time.Minute, BoxCap: 2}

func TestANewMonthCountsOnlyItsOwnHours(t *testing.T) {
	up := time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)
	now := time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Hours: 500, Boxes: []application.CloudBox{{Name: "cloud1", Up: up}}}}
	report, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: &apptest.FakeCloud{}, Book: book, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// October's 500 hours are gone; the box up since October is counted
	// from midnight: two hours begun.
	if book.State.Month != "2026-11" || book.State.Hours != 0 || report.SpentUSD != 2*0.132 {
		t.Errorf("expected November to count two hours, got %+v, $%.3f", book.State, report.SpentUSD)
	}
}

// stuckCloud destroys nothing.
type stuckCloud struct{ apptest.FakeCloud }

func (*stuckCloud) Down(context.Context, string) error { return errors.New("terraform destroy failed") }

func TestABoxThatCannotBeDestroyedStaysInTheBookAndFailsTheCheck(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	up := now.Add(-2 * time.Hour)
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Boxes: []application.CloudBox{{Name: "cloud1", Up: up, IdleSince: up}}}}
	log := &apptest.FakeEventLog{}
	_, err := application.CloudCheck{
		Work: apptest.NewFakeTracker(), Provider: &stuckCloud{}, Book: book, Log: log, Host: "desktop",
		Plan: cloudPlan, Now: func() time.Time { return now },
	}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "destroying cloud1") {
		t.Errorf("expected the check to fail destroying cloud1, got %v", err)
	}
	if len(book.State.Boxes) != 1 {
		t.Errorf("expected the box still in the book, got %+v", book.State.Boxes)
	}
	evs, _ := log.Since(context.Background(), 0)
	if len(evs) != 1 || !strings.Contains(evs[0].Detail, "failed to destroy cloud1") || evs[0].Actor != "cloud@desktop" {
		t.Errorf("expected one cloud event saying so, got %+v", evs)
	}
}

func TestStatusShowsTheCloudOnlyWhenItHasABook(t *testing.T) {
	now := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	tracker := apptest.NewFakeTracker()
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-10", Hours: 10,
		Boxes: []application.CloudBox{{Name: "cloud1", Up: now.Add(-90 * time.Minute)}},
		Moves: []application.CloudMove{{At: now.Add(-90 * time.Minute), What: "up cloud1: 3 stories wait for any host, 0 sessions free"}}}}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder",
		Cloud: book, CloudPlan: cloudPlan, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := report.String()
	for _, want := range []string{"CLOUD: $1.58 spent of $75.00 this month, 1 of 2 boxes", "cloud1  up 1h30m, working", "10-09 12:30Z up cloud1"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
	report, err = application.Status{Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder"}.Run(context.Background())
	if err != nil || strings.Contains(report.String(), "CLOUD") {
		t.Errorf("expected no CLOUD section without a book: %v\n%s", err, report.String())
	}
}

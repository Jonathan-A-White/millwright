package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A seat stopped at the usage limit writes nothing anywhere: mw status reads
// it off the panes, so that the Governor sees which seat waits on the limit,
// and until when (mw-gq6.473).
func TestStatusShowsEverySeatStoppedAtTheUsageLimit(t *testing.T) {
	tracker := aTrackerPathedToVPS(t)
	windows := apptest.NewFakeWindows()
	windows.HoldsWithID("@138", "mayor-2026-09-18-03", statusNow.Add(-3*time.Hour))
	windows.HoldsWithID("@140", "millhand-2026-09-18-07", statusNow.Add(-2*time.Hour))
	windows.HoldsWithID("@141", "deputy-2026-09-18-02", statusNow.Add(-5*time.Hour))
	for id, notice := range map[string]string{
		"@138": "You've hit your weekly limit · resets Sep 19, 7am (UTC)",
		"@141": "You've hit your session limit · resets 8am (UTC)",
	} {
		if err := windows.Shows(id, "● Filing it.\n\n  ⎿  "+notice+"\n\n❯ \n"); err != nil {
			t.Fatal(err)
		}
	}

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		Panes: windows, Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	printed := report.String()

	for _, want := range []string{
		application.AtTheUsageLimitHeading + " (2)\n",
		"  mayor-2026-09-18-03 (@138) until 2026-09-19 07:00Z\n",
		"    You've hit your weekly limit · resets Sep 19, 7am (UTC)\n",
		"  deputy-2026-09-18-02 (@141) reset 2026-09-18 08:00Z\n",
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("expected status to say %q, got:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "millhand-2026-09-18-07") {
		t.Errorf("expected the idle Millhand not to be shown at the limit, got:\n%s", printed)
	}
}

func TestStatusShowsNoUsageLimitSectionWhenNoPaneIsAtIt(t *testing.T) {
	windows := apptest.NewFakeWindows()
	windows.HoldsWithID("@140", "millhand-2026-09-18-07", statusNow.Add(-2*time.Hour))
	tracker := aTrackerPathedToVPS(t)

	report, err := application.Status{
		Tracker: tracker, Notes: tracker, Host: "vps", Seat: "builder",
		Panes: windows, Now: func() time.Time { return statusNow },
	}.Run(context.Background())
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if strings.Contains(report.String(), application.AtTheUsageLimitHeading) {
		t.Errorf("expected no usage-limit section, got:\n%s", report.String())
	}
}

func TestAPaneLimitIsReadOffTheNoticeAndTheFirstResetAfterItsWindowOpened(t *testing.T) {
	opened := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		shown, notice string
		resets        time.Time
	}{
		"a reset with no date is the first such time after the window opened": {
			"⎿  You’ve hit your session limit · resets 3pm (UTC)", "You’ve hit your session limit · resets 3pm (UTC)",
			time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)},
		"a dated reset in its own zone": {
			"⎿  You've hit your weekly limit · resets Oct 17, 9am (America/New_York)\n│ ❯ 1. Stop and wait for limit to reset",
			"You've hit your weekly limit · resets Oct 17, 9am (America/New_York)",
			time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC)},
		"a menu whose notice scrolled away waits the usual hour": {
			"│ ❯ 1. Stop and wait for limit to reset\n│   2. Switch to usage credits", "",
			opened.Add(application.UsageLimitWait)},
	} {
		limit := application.ReadPaneLimit(tc.shown, opened)
		if limit.Notice != tc.notice || !limit.Resets.Equal(tc.resets) {
			t.Errorf("%s: got %q resetting %s, want %q resetting %s", name, limit.Notice, limit.Resets, tc.notice, tc.resets)
		}
	}
}

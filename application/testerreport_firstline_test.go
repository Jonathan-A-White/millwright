package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

// mw tester report writes its first line, the trial's start, before it reads the
// tracker, so a report that takes minutes under load is seen to be at work
// (mw-6ww.175.39). outAtRead is the tracker of factoryreport_firstline_test.go.
func TestATesterReportWritesItsFirstLineBeforeItReadsTheTracker(t *testing.T) {
	out := &bytes.Buffer{}
	tracker := &outAtRead{FakeTracker: apptest.NewFakeTracker(), out: out}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	_, err := application.TesterReport{
		Tracker: tracker, Files: vault.New(t.TempDir()), Seat: "builder",
		Since: now.AddDate(0, 0, -14), Now: func() time.Time { return now }, Out: out,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tracker.reads) == 0 {
		t.Fatal("the report never read the tracker")
	}
	if first := tracker.reads[0]; first != "Tester trial since 2026-10-30 12:00 UTC\n" {
		t.Errorf("at its first tracker read the report had written %q, want its first line", first)
	}
	if got := strings.Count(out.String(), "Tester trial since"); got != 1 {
		t.Errorf("the first line was written %d times:\n%s", got, out.String())
	}
}

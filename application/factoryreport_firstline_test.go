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

// outAtRead is a tracker that notes what the report had written to its Out at
// each read, as a slow tracker holds the report at one (mw-6ww.175.31).
type outAtRead struct {
	*apptest.FakeTracker
	out   *bytes.Buffer
	reads []string
}

func (t *outAtRead) StoriesSince(ctx context.Context, since time.Time) ([]application.StoryDetail, error) {
	t.reads = append(t.reads, t.out.String())
	return t.FakeTracker.StoriesSince(ctx, since)
}

// mw report writes its first line, the window, before it reads the tracker, so
// a report that takes minutes under load is seen to be at work.
func TestAReportWritesItsFirstLineBeforeItReadsTheTracker(t *testing.T) {
	out := &bytes.Buffer{}
	tracker := &outAtRead{FakeTracker: apptest.NewFakeTracker(), out: out}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	_, err := application.FactoryReport{
		Tracker: tracker, Files: vault.New(t.TempDir()), Seat: "builder",
		Benchmarks: &apptest.FakeBenchmarks{}, Out: out,
		Since: now.AddDate(0, 0, -30), Now: func() time.Time { return now },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tracker.reads) == 0 {
		t.Fatal("the report never read the tracker")
	}
	first := tracker.reads[0]
	if !strings.HasPrefix(first, "QUALITY 2026-10-14 to 2026-11-13 (30 days)\n") {
		t.Errorf("at its first tracker read the report had written %q, want its QUALITY window line", first)
	}
	// The line is written once: the whole text is the same as the result's.
	if got := strings.Count(out.String(), "QUALITY "); got != 1 {
		t.Errorf("the window line was written %d times:\n%s", got, out.String())
	}
}

// --json is one document, so nothing is written before it is whole.
func TestAJSONReportWritesNothingBeforeItIsWhole(t *testing.T) {
	out := &bytes.Buffer{}
	tracker := &outAtRead{FakeTracker: apptest.NewFakeTracker(), out: out}
	_, err := application.FactoryReport{
		Tracker: tracker, Files: vault.New(t.TempDir()), Seat: "builder",
		Benchmarks: &apptest.FakeBenchmarks{}, Out: out, JSON: true,
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tracker.reads) == 0 || tracker.reads[0] != "" {
		t.Errorf("a JSON report wrote %q before its first read", tracker.reads)
	}
}

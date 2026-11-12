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

// mw-gq6.1139: the Builder harness (Claude Code on Bun) segfaulted on cloud
// boxes; mw status names a box that crashed it twice in a day and prints each
// box's harness version, so a pinned version can be compared.

// fakePanes is a BoxPanes whose panes are kept per box.
type fakePanes struct {
	panes map[string][]application.BoxPane
	errs  map[string]error
	asked []string
}

func (f *fakePanes) Panes(_ context.Context, box string, _ time.Time) ([]application.BoxPane, error) {
	f.asked = append(f.asked, box)
	return f.panes[box], f.errs[box]
}

const bunCrash = "Bun v1.4.4 (abc123) Linux x64\nPlease file a bug report at https://bun.report/1.4.4/x\npanic(main thread): Segmentation fault at address 0x0\n"

func crashStatus(t *testing.T, now time.Time, boxes []application.CloudBox, panes application.BoxPanes) string {
	t.Helper()
	tracker := apptest.NewFakeTracker()
	book := &apptest.FakeCloudBook{State: application.CloudState{Month: "2026-11", Boxes: boxes}}
	report, err := application.Status{Tracker: tracker, Notes: tracker, Host: "desktop", Seat: "builder",
		Cloud: book, CloudPlan: cloudPlan, BoxPanes: panes, Now: func() time.Time { return now }}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return report.String()
}

func TestStatusNamesABoxWithTwoHarnessCrashesInADay(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	up := now.Add(-5 * time.Hour)
	panes := &fakePanes{panes: map[string][]application.BoxPane{
		"cloud1": {
			{Window: "mw-indx1x6.6", At: now.Add(-90 * time.Minute), Text: bunCrash},
			{Window: "mw-82lb97i.11", At: now.Add(-80 * time.Minute), Text: bunCrash},
			{Window: "mw-ok", At: now.Add(-70 * time.Minute), Text: "all done\n"},
		},
		"cloud2": {{Window: "mw-one", At: now.Add(-60 * time.Minute), Text: bunCrash}},
		"cloud3": {
			{Window: "mw-old1", At: now.Add(-30 * time.Hour), Text: bunCrash},
			{Window: "mw-old2", At: now.Add(-26 * time.Hour), Text: bunCrash},
		},
	}}
	text := crashStatus(t, now, []application.CloudBox{
		{Name: "cloud1", Up: up}, {Name: "cloud2", Up: up}, {Name: "cloud3", Up: up},
	}, panes)
	if !strings.Contains(text, "cloud1: the harness crashed 2 times in the last day") {
		t.Errorf("expected cloud1 named for two crashes in:\n%s", text)
	}
	if !strings.Contains(text, "mw-indx1x6.6") || !strings.Contains(text, "mw-82lb97i.11") {
		t.Errorf("expected the crashed sessions named in:\n%s", text)
	}
	for _, not := range []string{"cloud2: the harness crashed", "cloud3: the harness crashed"} {
		if strings.Contains(text, not) {
			t.Errorf("expected no %q (one crash, or crashes over a day old) in:\n%s", not, text)
		}
	}
}

func TestACrashBannerAloneWithNoBunLineIsNotAHarnessCrash(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	panes := &fakePanes{panes: map[string][]application.BoxPane{"cloud1": {
		{Window: "a", At: now.Add(-time.Hour), Text: "Segmentation fault\n"},
		{Window: "b", At: now.Add(-time.Hour), Text: "we talked about Bun v1.4.4 today\n"},
	}}}
	text := crashStatus(t, now, []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}, panes)
	if strings.Contains(text, "harness crashed") {
		t.Errorf("expected panes that are not a Bun crash banner left alone in:\n%s", text)
	}
}

func TestAPaneThatCannotBeReadCostsOnlyItsBoxesLine(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	panes := &fakePanes{errs: map[string]error{"cloud1": errors.New("ssh: no route")}}
	text := crashStatus(t, now, []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Hour)}}, panes)
	if !strings.Contains(text, "cloud1") || !strings.Contains(text, "ssh: no route") {
		t.Errorf("expected the box and why its panes could not be read in:\n%s", text)
	}
}

func TestABoxBeingMadeIsNotAskedForItsPanes(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	panes := &fakePanes{}
	crashStatus(t, now, []application.CloudBox{{Name: "cloud1", Up: now.Add(-3 * time.Minute), Making: true}}, panes)
	if len(panes.asked) != 0 {
		t.Errorf("expected no ssh to a box still being made, asked %v", panes.asked)
	}
}

func TestStatusPrintsTheHarnessVersionOfEachBox(t *testing.T) {
	now := time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)
	up := now.Add(-5 * time.Hour)
	text := crashStatus(t, now, []application.CloudBox{
		{Name: "cloud1", Up: up, Harness: "2.1.322"},
		{Name: "cloud2", Up: up, Harness: "2.1.300"},
		{Name: "cloud3", Up: up},
	}, nil)
	for _, want := range []string{"cloud1: claude 2.1.322", "cloud2: claude 2.1.300", "cloud3: claude version not known"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected %q in:\n%s", want, text)
		}
	}
}

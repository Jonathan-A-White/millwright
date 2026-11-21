package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A host that declines a rig ([dispatch] skip_rigs) passes over a ready story of
// that rig pathed auto, under the gate skip-rigs, until it has been passed for
// the way-out wait with no other host taking it; a story pathed to the host by
// name is taken at once.
func aSkippingHost(t *testing.T, host string) (application.Dispatch, *skipFixture) {
	t.Helper()
	dispatch, tracker, _, _, _ := aFactory(t)
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	dispatch.Host, dispatch.Boot.Host = host, host
	dispatch.Memory = tracker
	dispatch.Now = func() time.Time { return now }
	dispatch.SkipRigs = application.SkipRigs{Rigs: []string{"millwright"}, Wait: 20 * time.Minute}
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.9", Title: "A story of a declined rig"})
	if err := tracker.SetStoryMetadata(context.Background(), "mw-gq6.9", map[string]string{"host": domain.HostAuto}); err != nil {
		t.Fatalf("pathing the story to auto: %v", err)
	}
	return dispatch, &skipFixture{now: &now, tracker: tracker}
}

type skipFixture struct {
	now     *time.Time
	tracker interface {
		SetStoryMetadata(ctx context.Context, id string, fields map[string]string) error
	}
}

func skipGateOf(report application.DispatchReport, id string) (application.Passed, bool) {
	for _, passed := range report.Passed {
		if passed.StoryID == id && passed.Gate == "skip-rigs" {
			return passed, true
		}
	}
	return application.Passed{}, false
}

func TestAHostThatDeclinesARigPassesItsAutoStoryUnderTheSkipRigsGate(t *testing.T) {
	dispatch, _ := aSkippingHost(t, "vps")

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("the dispatch failed: %v", err)
	}
	passed, ok := skipGateOf(report, "mw-gq6.9")
	if !ok || len(report.Started) != 0 {
		t.Fatalf("expected mw-gq6.9 passed over at skip-rigs and nothing started, got passed %+v, started %+v", report.Passed, report.Started)
	}
	if !strings.Contains(passed.Why, "millwright") || !strings.Contains(passed.Why, "skip_rigs") {
		t.Errorf("expected the reason to name the rig and the key, got %q", passed.Why)
	}
	if !strings.Contains(report.String(), "skip-rigs") {
		t.Errorf("expected the report to print the gate's name, got:\n%s", report.String())
	}
}

func TestAHostThatDeclinesARigTakesAStoryPathedToItByName(t *testing.T) {
	dispatch, fixture := aSkippingHost(t, "vps")
	if err := fixture.tracker.SetStoryMetadata(context.Background(), "mw-gq6.9", map[string]string{"host": "vps"}); err != nil {
		t.Fatal(err)
	}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("the dispatch failed: %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected the story pathed to the host by name started, got %+v, passed %+v", report.Started, report.Passed)
	}
}

func TestAHostThatDeclinesARigTakesTheStoryOnceNoOtherHostHasForTheWayOutWait(t *testing.T) {
	dispatch, fixture := aSkippingHost(t, "vps")
	ctx := context.Background()

	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("the first dispatch failed: %v", err)
	}
	*fixture.now = fixture.now.Add(19 * time.Minute)
	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("the second dispatch failed: %v", err)
	}
	if _, ok := skipGateOf(report, "mw-gq6.9"); !ok || len(report.Started) != 0 {
		t.Fatalf("expected the story still passed at 19 minutes, got passed %+v, started %+v", report.Passed, report.Started)
	}
	*fixture.now = fixture.now.Add(2 * time.Minute)
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("the third dispatch failed: %v", err)
	}
	if len(report.Started) != 1 || report.Started[0].StoryID != "mw-gq6.9" {
		t.Fatalf("expected the story taken after 21 minutes with no other host taking it, got started %+v, passed %+v", report.Started, report.Passed)
	}
}

func TestAHostThatDeclinesOneRigTakesAStoryOfAnother(t *testing.T) {
	dispatch, _ := aSkippingHost(t, "vps")
	dispatch.SkipRigs.Rigs = []string{"lampas"}

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("the dispatch failed: %v", err)
	}
	if _, skipped := skipGateOf(report, "mw-gq6.9"); skipped || len(report.Started) != 1 {
		t.Fatalf("expected the story of a rig not declined started, got started %+v, passed %+v", report.Started, report.Passed)
	}
}

func TestADryRunOfAHostThatDeclinesARigPrintsTheGateBesideTheStory(t *testing.T) {
	dispatch, _ := aSkippingHost(t, "vps")
	dispatch.DryRun = true

	report, err := dispatch.Run(context.Background())
	if err != nil {
		t.Fatalf("the dry run failed: %v", err)
	}
	if _, ok := skipGateOf(report, "mw-gq6.9"); !ok {
		t.Fatalf("expected the dry run to pass the story at skip-rigs, got %+v", report.Passed)
	}
}

// A story the way out let through, passed over again by the cap, is not made to
// wait the whole way out once more when the room comes.
func TestAStoryTheWayOutLetThroughKeepsItsTimeWhileTheCapHoldsIt(t *testing.T) {
	dispatch, fixture := aSkippingHost(t, "vps")
	ctx := context.Background()
	if _, err := dispatch.Run(ctx); err != nil {
		t.Fatalf("the first dispatch failed: %v", err)
	}
	*fixture.now = fixture.now.Add(21 * time.Minute)
	full := dispatch
	full.Cap = 0
	report, err := full.Run(ctx)
	if err != nil {
		t.Fatalf("the full host's dispatch failed: %v", err)
	}
	if len(report.Passed) != 1 || report.Passed[0].Gate != "drained" {
		t.Fatalf("expected the story passed by the cap past the way out, got %+v", report.Passed)
	}
	*fixture.now = fixture.now.Add(time.Minute)
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("the dispatch with room failed: %v", err)
	}
	if len(report.Started) != 1 {
		t.Fatalf("expected the story taken at once when the room came, got started %+v, passed %+v", report.Started, report.Passed)
	}
}

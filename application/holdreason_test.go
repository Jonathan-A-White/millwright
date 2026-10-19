package application_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func opensWithHeld(comments []string) bool {
	return slices.ContainsFunc(comments, func(c string) bool {
		return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(c)), application.HeldCommentPrefix)
	})
}

// A bead mw ice holds is not listed by mw stuck as held with no reason written,
// even when it is not iced by label: the comment opens 'HELD:' and says why.
func TestIceWritesAHeldCommentWithTheReason(t *testing.T) {
	tracker := iceFixture(t)
	if err := ice(t, tracker, "mw-i.1", "waiting on the new rig"); err != nil {
		t.Fatal(err)
	}
	got := tracker.Comments("mw-i.1")
	if len(got) != 1 || !strings.HasPrefix(got[0], application.HeldCommentPrefix) || !strings.Contains(got[0], "waiting on the new rig") {
		t.Fatalf("comments = %q, want one opening %q with the reason", got, application.HeldCommentPrefix)
	}
}

// A held bead with a live bead behind it and a hold comment is left alone by mw stuck.
func TestStuckDoesNotListAHoldTheHoldHelpersWroteAReasonFor(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	tracker.AddEpic("mw-h", domain.Path{})
	for _, id := range []string{"mw-h.1", "mw-h.2"} {
		tracker.AddStory("mw-h", domain.Story{ID: id, Title: id})
	}
	tracker.Needs("mw-h.2", "mw-h.1")
	mustDo(t, tracker.SetStatus("mw-h.1", apptest.StatusDeferred))
	mustDo(t, tracker.CommentOnStory(context.Background(), "mw-h.1", "HELD: Iced: later"))
	report, err := application.StuckSurvey{Graph: tracker, Comments: tracker}.Find(context.Background())
	mustDo(t, err)
	if len(report.Roots) != 0 {
		t.Errorf("roots = %+v, want none", report.Roots)
	}
}

// holdSites lists every place in the production code that sets the status
// deferred, and what it does about the reason. A new site fails the sweep
// until it is listed here with its answer.
//
//	reason: writes a comment opening 'HELD:' (pinned by a test of its own below or elsewhere)
//	refuses: refuses without --reason
//	transient: the hold is undone in the same call
//	filed: a story filed held, awaiting its release; mw stuck rightly lists it when live beads wait behind it with no reason
var holdSites = map[string]string{
	"application/ice.go:HoldStory":                    "refuses",
	"application/retry.go:HoldStory":                  "reason",
	"application/retry.go:HoldClaimHeldBy":            "reason",
	"application/postern.go:HoldStory":                "reason",
	"application/posternapply.go:HoldStory":           "reason",
	"application/eventcontrol.go:HoldStory":           "reason",
	"application/foldsupersede.go:HoldStory":          "transient",
	"infrastructure/beads/beads.go:StatusDeferred":    "filed",
	"infrastructure/beads/dispatch.go:StatusDeferred": "implementation",
}

var holdCall = regexp.MustCompile(`\.(HoldStory|HoldClaimHeldBy)\(`)

func TestEveryCommandThatSetsDeferredWritesAHeldReasonOrRefusesWithoutOne(t *testing.T) {
	root := ".."
	found := map[string]bool{}
	for _, dir := range []string{"application", "cmd/mw", "infrastructure/beads"} {
		files, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		mustDo(t, err)
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			body, err := os.ReadFile(f)
			mustDo(t, err)
			rel := dir + "/" + filepath.Base(f)
			for _, m := range holdCall.FindAllStringSubmatch(string(body), -1) {
				found[rel+":"+m[1]] = true
			}
			if dir == "infrastructure/beads" && strings.Contains(string(body), "--status\", StatusDeferred") {
				found[rel+":StatusDeferred"] = true
			}
		}
	}
	for site := range found {
		if _, ok := holdSites[site]; !ok {
			t.Errorf("%s sets a bead deferred and is not listed in holdSites: write a 'HELD:' reason comment or refuse without --reason, then list it", site)
		}
	}
	for site := range holdSites {
		if !found[site] {
			t.Errorf("holdSites lists %s, which sets nothing deferred any more: remove it", site)
		}
	}
}

func TestRetryHoldWritesAHeldComment(t *testing.T) {
	tracker := apptest.NewFakeTracker()
	aRetryStory(t, tracker, "mw-gq6.1")
	var out strings.Builder
	retry := aRetry(tracker, &fakeRetryLanding{NoWorktree: true}, &apptest.FakeVaultFiles{}, &out)
	retry.Hold = true
	if _, err := retry.Run(context.Background(), "mw-gq6.1"); err != nil {
		t.Fatal(err)
	}
	if !opensWithHeld(tracker.Comments("mw-gq6.1")) {
		t.Errorf("comments = %q, want one opening %q", tracker.Comments("mw-gq6.1"), application.HeldCommentPrefix)
	}
}

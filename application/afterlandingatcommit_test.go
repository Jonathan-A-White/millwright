package application

import (
	"context"
	"strings"
	"testing"
)

// advancingLanding is a Landing whose Advance answers from a list, one answer
// to each call, the last for every call after: it stands for a checkout that
// changes between the landing's own fast-forward and the after-landing run.
type advancingLanding struct {
	Landing
	answers []Advanced
	calls   int
	commits []string
}

func (a *advancingLanding) Advance(_ context.Context, _, _, commit string) (Advanced, error) {
	a.commits = append(a.commits, commit)
	i := a.calls
	if i >= len(a.answers) {
		i = len(a.answers) - 1
	}
	a.calls++
	return a.answers[i], nil
}

// mw-6ww.175.51: a checkout that was current when the landing moved it can
// stop being so before the after-landing command begins (the command waits for
// the rig's after-landing lock, and another fast-forward may be half done); the
// command's own checkout is looked at again once the lock is held, and a build
// is made only of one at the landed commit with clean files.
func TestAfterLandingRunsOnlyOnceTheCheckoutIsAtTheLandedCommit(t *testing.T) {
	cases := []struct {
		name    string
		now     Advanced
		ran     int
		reason  string
		skipped bool
	}{
		{"at the landed commit with clean files", Advanced{Level: true}, 1, "", false},
		{"moved onto it by the second look", Advanced{Moved: true}, 1, "", false},
		{"behind the landed commit", Advanced{Left: "main here has commits of its own that b040268 does not"}, 0, "left as it was", true},
		{"files that differ from HEAD", Advanced{Level: true, Held: "it has 1 uncommitted path(s): package.json"}, 0, "uncommitted path(s): package.json", true},
		{"on another branch", Advanced{Level: true, Held: "it is on wip, not main"}, 0, "it is on wip, not main", true},
		{"git failed", Advanced{Held: "git said: fatal: bad object"}, 0, "git said: fatal: bad object", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deploy := &fakeDeploy{}
			landing := &advancingLanding{answers: []Advanced{tc.now}}
			n := Next{AfterLanding: deploy, Landing: landing, Tracker: &smokeComments{}, Host: "laptop"}
			c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/postern"}
			c.path.Rig = "postern"
			// The landing's own fast-forward said Moved; the checkout is another thing by now.
			report := &NextReport{Rig: Advanced{Moved: true}, Landed: true}
			report.How.Commit = "b040268b"

			n.afterLanding(context.Background(), c, report)

			if deploy.ran != tc.ran {
				t.Fatalf("expected the command to run %d times, ran %d; notes %v", tc.ran, deploy.ran, report.Notes)
			}
			if len(landing.commits) != 1 || landing.commits[0] != "b040268b" {
				t.Errorf("expected the checkout looked at once, against the landed commit, got %v", landing.commits)
			}
			if !tc.skipped {
				if report.AfterLandingSkipped != "" {
					t.Errorf("expected nothing skipped, got %q", report.AfterLandingSkipped)
				}
				return
			}
			joined := strings.Join(report.Notes, "\n")
			if !strings.Contains(joined, tc.reason) || !strings.Contains(report.AfterLandingSkipped, "not run, ") {
				t.Errorf("expected the note to say why, %q, and the report to hold the skip, got %q / %q", tc.reason, joined, report.AfterLandingSkipped)
			}
			if report.Rig.Current() {
				t.Errorf("expected the report's checkout to say it is not current, got %+v", report.Rig)
			}
		})
	}
}

package application

import (
	"context"
	"strings"
	"testing"
)

// fakeDeploy is the rig's after-landing command, counting its runs.
type fakeDeploy struct{ ran int }

func (f *fakeDeploy) Command(string) string { return "make deploy" }

func (f *fakeDeploy) Run(context.Context, string, string) (Ran, error) {
	f.ran++
	return Ran{Command: "make deploy"}, nil
}

// mw-gq6.484: a checkout already holding the landed commit is up to date, so the
// command runs; a level one that is no place to build from, or one git failed
// on, is skipped with its real reason, never "not brought up".
func TestTheAfterLandingCommandRunsForALevelCheckoutAndNamesTheRealReasonWhenSkipped(t *testing.T) {
	cases := []struct {
		name    string
		rig     Advanced
		ran     int
		reason  string
		skipped bool
	}{
		{"moved", Advanced{Moved: true}, 1, "", false},
		{"level", Advanced{Level: true}, 1, "", false},
		{"level on another branch", Advanced{Level: true, Held: "it is on wip, not main"}, 0, "at the landed commit but is no place to build from: it is on wip, not main", true},
		{"level with uncommitted work", Advanced{Level: true, Held: "it has 1 uncommitted path(s): a.md"}, 0, "uncommitted path(s): a.md", true},
		{"left alone", Advanced{Left: "main here has commits of its own that abc does not"}, 0, "left as it was: main here has commits of its own", true},
		{"git failed", Advanced{Held: "git said: fatal: bad object"}, 0, "could not be brought up to main: git said: fatal: bad object", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deploy := &fakeDeploy{}
			n := Next{AfterLanding: deploy, Tracker: &smokeComments{}, Host: "vps"}
			c := &closeOut{id: "mw-1.1", target: "main", rigDir: "/rigs/lampas"}
			c.path.Rig = "lampas"
			report := &NextReport{Rig: tc.rig, Landed: true}

			n.afterLanding(context.Background(), c, report)

			if deploy.ran != tc.ran {
				t.Fatalf("expected the command to run %d times, ran %d; notes %v", tc.ran, deploy.ran, report.Notes)
			}
			joined := strings.Join(report.Notes, "\n")
			if strings.Contains(joined, "was not brought up") {
				t.Errorf("a note must name the real reason, got %q", joined)
			}
			if !tc.skipped {
				if report.AfterLandingSkipped != "" {
					t.Errorf("expected nothing skipped, got %q", report.AfterLandingSkipped)
				}
				return
			}
			if !strings.Contains(joined, tc.reason) || report.AfterLandingSkipped == "" {
				t.Errorf("expected the note to say %q and the report to hold it, got %q / %q", tc.reason, joined, report.AfterLandingSkipped)
			}
			mail := n.mayorMail(c, report, MailLanded, "")
			first, _, _ := strings.Cut(mail.Body, "\n")
			if !strings.HasPrefix(first, AfterLandingNotRunLine+": make deploy: not run, ") || !strings.Contains(first, tc.reason) {
				t.Errorf("expected the Landed mail to open by saying the deploy was not run and why, got first line %q", first)
			}
		})
	}
}

// The grist smoke follows the same rule as the deploy.
func TestTheGristSmokeRunsForALevelCheckout(t *testing.T) {
	stub := &smokeStub{}
	n := Next{Tracker: &smokeComments{}, Host: "laptop", Smoke: GristSmokeAfter{Smoke: stub, Book: GristSmokeBook{Notes: smokeNoteStore{}, Host: "laptop"}}}
	c := &closeOut{id: "mw-1.1", target: "main", smokeApps: []string{"cairn"}}
	c.path.Rig = "cairn"

	n.smoke(context.Background(), c, &NextReport{Rig: Advanced{Level: true}})
	if len(stub.ran) != 1 {
		t.Fatalf("expected a level checkout smoked, got %v", stub.ran)
	}

	report := &NextReport{Rig: Advanced{Level: true, Held: "it is on wip, not main"}}
	n.smoke(context.Background(), c, report)
	if len(stub.ran) != 1 || len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "it is on wip, not main") {
		t.Fatalf("expected a level checkout on wip not smoked, with its reason, got %v / %v", stub.ran, report.Notes)
	}
}

package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.1666: a batch merges through the rig's regenerated_files as well as its
// union_merge_files, and says which generated files it regenerated.

// aRegeneratingLanding merges every branch by MergeRuled only: Merge and
// MergeUnion are failures, as the batch must not use them for a rig that lists
// generated files. A branch in refuses is set aside as the real landing does
// when the generator fails.
type aRegeneratingLanding struct {
	*aBranchConflictLanding
	seen    []domain.MergeRules
	trials  []domain.MergeRules
	refuses map[string]bool
}

func (l *aRegeneratingLanding) Merge(context.Context, string, string) (application.Landed, error) {
	return application.Landed{}, fmt.Errorf("Merge was used for a rig that lists generated files")
}

func (l *aRegeneratingLanding) MergeUnion(context.Context, string, string, []string) (application.Landed, error) {
	return application.Landed{}, fmt.Errorf("MergeUnion was used for a rig that lists generated files")
}

func (l *aRegeneratingLanding) MergeRuled(_ context.Context, dir, branch string, rules domain.MergeRules) (application.Landed, error) {
	if dir == trialDir {
		l.trials = append(l.trials, rules)
	} else {
		l.seen = append(l.seen, rules)
	}
	if l.refuses[branch] {
		return application.Landed{}, &application.MergeConflict{Files: []string{"docs/lxx-align/gen.txt"},
			Said: application.ErrMergeConflict.Error() + ": CONFLICT (regenerated): docs/lxx-align/gen.txt by `npm run gen` failed: exit status 1; its last lines: boom"}
	}
	return application.Landed{Commit: "merge-" + branch, Regenerated: []application.Regeneration{{Path: "docs/lxx-align/gen.txt", Run: "npm run gen"}}}, nil
}

func TestABatchRegeneratesAGeneratedFileOfARigThatListsItAndSaysSo(t *testing.T) {
	a := aBatchOf(t, 2)
	landing := &aRegeneratingLanding{aBranchConflictLanding: a.landing, refuses: map[string]bool{"origin/mw/mw-e.2": true}}
	a.land.Next.Landing = landing
	rules := domain.EpicRequirements{
		UnionMergeFiles:  []string{"docs/*.md"},
		RegeneratedFiles: []domain.RegeneratedFiles{{Paths: []string{"docs/lxx-align/gen.txt"}, Run: "npm run gen"}},
	}

	a.land.LandTheBatchUnder(context.Background(), "/rigs/millwright/.landing-595", rules, a.report)

	if len(landing.seen) != 2 || !strings.Contains(fmt.Sprint(landing.seen[0]), "npm run gen") || len(landing.seen[0].Union) != 1 {
		t.Errorf("expected each branch merged by the rig's rules, union and regenerated both, got %+v", landing.seen)
	}
	// The one that conflicted is tried on main alone by the same rules.
	if len(landing.trials) != 1 || fmt.Sprint(landing.trials[0]) != fmt.Sprint(landing.seen[0]) {
		t.Errorf("expected mw-e.2 tried on main alone by the rig's rules, got %+v", landing.trials)
	}
	r := a.report
	if len(r.Merged) != 1 || r.Merged[0].ID != "mw-e.1" {
		t.Fatalf("expected mw-e.1 merged, got %+v", r.Merged)
	}
	if want := "mw-e.1: regenerated docs/lxx-align/gen.txt by npm run gen"; !strings.Contains(strings.Join(r.Notes, "\n"), want) {
		t.Errorf("expected the note %q, got %q", want, r.Notes)
	}
	if got := r.String(); !strings.Contains(got, "note    mw-e.1: regenerated docs/lxx-align/gen.txt by npm run gen") {
		t.Errorf("expected the batch's report to say it, got:\n%s", got)
	}
	// The one whose generator failed is set aside as any conflict is, saying why.
	if len(r.SetAside) != 1 || r.SetAside[0].ID != "mw-e.2" || !strings.Contains(r.SetAside[0].Why, "its last lines: boom") {
		t.Errorf("expected mw-e.2 set aside with the generator's last lines, got %+v", r.SetAside)
	}
	if a.checks.runs != 1 {
		t.Errorf("expected the gate run once on the merged result")
	}
}

func TestALandingReportSaysWhichFilesItRegenerated(t *testing.T) {
	report := application.NextReport{StoryID: "mw-e.1", Target: "main", Landed: true,
		How: application.Landed{Commit: "0123456789abcdef", Regenerated: []application.Regeneration{{Path: "docs/gen.txt", Run: "make gen"}}}}
	if got := report.String(); !strings.Contains(got, "regenerated docs/gen.txt by make gen") {
		t.Errorf("expected the report to say it, got:\n%s", got)
	}
}

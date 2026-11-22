package application_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// A filed bug's model and effort are its own, by its size and kind, whatever the
// finder ran on (mw-gq6.1640).
func TestAFiledBugPathIsSetBySizeAndKindNotByTheFinder(t *testing.T) {
	finder := domain.Path{Rig: "millwright", Branch: "main", Harness: domain.HarnessClaude, Model: domain.ModelFable, Effort: domain.EffortXHigh,
		Formula: "tdd-feature", Host: "laptop", Size: domain.SizeL, Files: "a.go"}
	cases := []struct {
		name   string
		size   domain.Size
		kind   string
		model  domain.Model
		effort domain.Effort
	}{
		{"XS bug", domain.SizeXS, application.TesterKindBug, domain.ModelSonnet, domain.EffortHigh},
		{"S bug", domain.SizeS, application.TesterKindBug, domain.ModelSonnet, domain.EffortHigh},
		{"M bug", domain.SizeM, application.TesterKindBug, domain.ModelSonnet, domain.EffortHigh},
		{"a bug with no size", "", application.TesterKindBug, domain.ModelSonnet, domain.EffortHigh},
		{"a taste", domain.SizeS, application.TesterKindTaste, domain.ModelSonnet, domain.EffortHigh},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := application.FiledPath(finder, c.size, c.kind)
			if got.Model != c.model || got.Effort != c.effort {
				t.Errorf("expected %s at %s, got %s at %s", c.model, c.effort, got.Model, got.Effort)
			}
			if got.Size != c.size {
				t.Errorf("expected the bug's own size %q, got %q", c.size, got.Size)
			}
			if got.Rig != finder.Rig || got.Branch != finder.Branch || got.Formula != finder.Formula || got.Host != finder.Host || got.Files != finder.Files {
				t.Errorf("expected the finder's rig, branch, formula, host and files kept, got %+v", got)
			}
		})
	}
}

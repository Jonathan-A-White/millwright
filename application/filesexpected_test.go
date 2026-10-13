package application

import (
	"context"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// filesRulesStub says every rig expects a files key.
type filesRulesStub struct{}

func (filesRulesStub) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return domain.EpicRequirements{FilesExpected: true}, nil
}

func filesRules() EpicRules { return filesRulesStub{} }

func openStory(id, formula, files string) StoryDetail {
	return StoryDetail{
		Story:  domain.Story{ID: id, Title: id},
		Status: "open",
		Defaults: domain.Path{
			Rig: "lampas", Branch: "main", Formula: formula, Files: files,
		},
	}
}

func TestATesterStoryWithNoFilesKeyIsNotLackingOne(t *testing.T) {
	finder := &filesExpected{rules: filesRules()}
	lacking, err := finder.lacking(context.Background(),
		openStory("mw-t.1", "tester", ""), openStory("mw-t.2", "feature", ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(lacking) != 1 || lacking[0].ID != "mw-t.2" {
		t.Fatalf("expected only the feature story to lack a files key, got %+v", lacking)
	}
}

func TestATesterStoryWhoseEpicSetsTheTesterFormulaIsNotLackingAFilesKey(t *testing.T) {
	story := openStory("mw-t.1", "", "")
	story.Defaults.Formula = "tester"
	lacking, err := (&filesExpected{rules: filesRules()}).lacking(context.Background(), story)
	if err != nil {
		t.Fatal(err)
	}
	if len(lacking) != 0 {
		t.Fatalf("expected none, got %+v", lacking)
	}
}

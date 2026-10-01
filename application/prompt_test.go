package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func TestPromptSavePutsTheCheckedPromptAndNothingElse(t *testing.T) {
	backend := apptest.NewFakePrompts()
	var out bytes.Buffer
	save := application.PromptSave{Prompts: backend, Out: &out}

	if _, err := save.Run(context.Background(), application.PromptSaveRequest{
		Name: "top5", Summary: "The five next", Options: []string{"count:int=5", "since:string:required"}, Body: "Top <count>.",
	}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	puts := backend.Puts()
	want := domain.Prompt{Name: "top5", Summary: "The five next", Signature: []string{"count:int=5", "since:string:required"}, Body: "Top <count>."}
	if len(puts) != 1 || puts[0].Name != want.Name || puts[0].Summary != want.Summary || puts[0].Body != want.Body || strings.Join(puts[0].Signature, "|") != strings.Join(want.Signature, "|") {
		t.Fatalf("expected one put of %+v, got %+v", want, puts)
	}

	for name, req := range map[string]application.PromptSaveRequest{
		"a name that is not one": {Name: "Top 5", Summary: "x", Body: "b"},
		"no summary":             {Name: "x", Body: "b"},
		"an empty body":          {Name: "x", Summary: "x", Body: " \n"},
		"a bad option":           {Name: "x", Summary: "x", Body: "b", Options: []string{"count:float"}},
		"an option given twice":  {Name: "x", Summary: "x", Body: "b", Options: []string{"a:int=1", "a:int=2"}},
	} {
		if _, err := save.Run(context.Background(), req); err == nil {
			t.Errorf("expected %s refused", name)
		}
	}
	if got := len(backend.Puts()); got != 1 {
		t.Fatalf("expected the refused prompts not saved, got %d puts", got)
	}
}

func TestPromptRunTakesABoolFlagAloneAndTheEqualsForm(t *testing.T) {
	backend := apptest.NewFakePrompts()
	_ = backend.Put(context.Background(), domain.Prompt{Name: "p", Summary: "s", Signature: []string{"all:bool=false", "n:int=1"}, Body: "all=<all> n=<n>"})
	tracker := apptest.NewFakeTracker()
	var out bytes.Buffer
	run := application.PromptRun{Prompts: backend, Tracker: tracker, Notes: tracker, Host: "desktop", Out: &out}

	if err := run.Run(context.Background(), "p", []string{"--all", "--n=3"}); err != nil {
		t.Fatalf("running: %v", err)
	}
	for _, want := range []string{"PROMPT /p --all true --n 3\n", "all=true n=3\n", "FACTS\n", "WAITING FOR THE GOVERNOR\n  none\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("expected %q in:\n%s", want, out.String())
		}
	}
	if err := run.Run(context.Background(), "p", []string{"--n"}); err == nil || !strings.Contains(err.Error(), "needs a value") {
		t.Errorf("expected a flag with no value refused, got %v", err)
	}
	if err := run.Run(context.Background(), "p", []string{"stray"}); err == nil {
		t.Errorf("expected a stray word refused")
	}
}

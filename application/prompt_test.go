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

func TestPromptRunFillsAFreeTextOptionWithTheWordsNoFlagTook(t *testing.T) {
	backend := apptest.NewFakePrompts()
	_ = backend.Put(context.Background(), domain.Prompt{Name: "later", Summary: "park a want", Signature: []string{"x:int=0", "text:text:required"}, Body: "Park: <text> (x=<x>)"})
	tracker := apptest.NewFakeTracker()
	var out bytes.Buffer
	run := application.PromptRun{Prompts: backend, Tracker: tracker, Notes: tracker, Host: "desktop", Out: &out}

	if err := run.Run(context.Background(), "later", []string{"a", "licence", "for", "Luke"}); err != nil {
		t.Fatalf("running: %v", err)
	}
	if !strings.Contains(out.String(), "Park: a licence for Luke (x=0)\n") {
		t.Errorf("expected <text> filled with the words, got:\n%s", out.String())
	}
	out.Reset()
	if err := run.Run(context.Background(), "later", []string{"--x", "1", "two", "words"}); err != nil {
		t.Fatalf("running: %v", err)
	}
	if !strings.Contains(out.String(), "Park: two words (x=1)\n") {
		t.Errorf("expected the flag taken and the text 'two words', got:\n%s", out.String())
	}
	if err := run.Run(context.Background(), "later", []string{"--x", "1"}); err == nil || !strings.Contains(err.Error(), "--text is required") {
		t.Errorf("expected a required text left out refused, got %v", err)
	}
	if err := run.Run(context.Background(), "later", []string{"--text", "given", "and", "more"}); err == nil || !strings.Contains(err.Error(), "--text is given twice") {
		t.Errorf("expected the text given both ways refused, got %v", err)
	}
}

func TestPromptRunWithoutAFreeTextOptionStillRefusesAStrayWord(t *testing.T) {
	backend := apptest.NewFakePrompts()
	_ = backend.Put(context.Background(), domain.Prompt{Name: "p", Summary: "s", Signature: []string{"n:int=1"}, Body: "n=<n>"})
	tracker := apptest.NewFakeTracker()
	run := application.PromptRun{Prompts: backend, Tracker: tracker, Notes: tracker, Host: "desktop", Out: &bytes.Buffer{}}

	err := run.Run(context.Background(), "p", []string{"stray"})
	want := `mw prompt run: /p: "stray" is not --<flag> <value>; its signature is --n:int=1`
	if err == nil || err.Error() != want {
		t.Fatalf("expected %q, got %v", want, err)
	}
}

func TestPromptSaveRefusesTwoFreeTextOptions(t *testing.T) {
	backend := apptest.NewFakePrompts()
	save := application.PromptSave{Prompts: backend}
	if _, err := save.Run(context.Background(), application.PromptSaveRequest{
		Name: "x", Summary: "x", Body: "b", Options: []string{"a:text", "b:text"},
	}); err == nil || !strings.Contains(err.Error(), "free-text") {
		t.Fatalf("expected two free-text options refused, got %v", err)
	}
	if _, err := save.Run(context.Background(), application.PromptSaveRequest{
		Name: "x", Summary: "x", Body: "b", Options: []string{"text:text:required"},
	}); err != nil {
		t.Fatalf("expected one free-text option saved: %v", err)
	}
}

package application_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestReadingResultJSONNamesAreSnakeCase(t *testing.T) {
	r := application.ReadingResult{
		Engine: "local", Accuracy: 80, Seconds: 2.5,
		Words: []application.ReadingWord{{
			Text: "cat", ExpectedPhonemes: []string{"K"}, ProducedPhonemes: []string{"K"},
			Error: application.ErrNone, Accuracy: 90, SelfCorrected: true,
		}},
	}
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"engine"`, `"words"`, `"accuracy"`, `"seconds"`, `"text"`, `"expected_phonemes"`, `"produced_phonemes"`, `"error":"none"`, `"self_corrected":true`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("expected %s in %s", want, out)
		}
	}
}

func TestReadingWordTimesKeepTheirNamesAndAWordNotHeardHasNullTimes(t *testing.T) {
	start, end := 0.25, 0.75
	heard, _ := json.Marshal(application.ReadingWord{Text: "cat", Error: application.ErrNone, Start: &start, End: &end})
	for _, want := range []string{`"start":0.25`, `"end":0.75`} {
		if !strings.Contains(string(heard), want) {
			t.Errorf("expected %s in %s", want, heard)
		}
	}
	silent, _ := json.Marshal(application.ReadingWord{Text: "cat", Error: application.ErrOmission})
	for _, want := range []string{`"start":null`, `"end":null`} {
		if !strings.Contains(string(silent), want) {
			t.Errorf("expected %s in %s", want, silent)
		}
	}
	var back application.ReadingWord
	if err := json.Unmarshal(heard, &back); err != nil || back.Start == nil || *back.Start != 0.25 || *back.End != 0.75 {
		t.Errorf("round trip lost the times: %v %+v", err, back)
	}
}

func TestReadingResultValidateRefusesTimesThatAreNotInOrder(t *testing.T) {
	neg, one, two := -0.1, 1.0, 2.0
	for name, w := range map[string]application.ReadingWord{
		"negative start":   {Text: "cat", Error: application.ErrNone, Start: &neg, End: &one},
		"end before start": {Text: "cat", Error: application.ErrNone, Start: &two, End: &one},
		"start alone":      {Text: "cat", Error: application.ErrNone, Start: &one},
	} {
		if err := (application.ReadingResult{Words: []application.ReadingWord{w}, Seconds: 3}).Validate(); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
	ok := application.ReadingWord{Text: "cat", Error: application.ErrNone, Start: &one, End: &two}
	if err := (application.ReadingResult{Words: []application.ReadingWord{ok}, Seconds: 3}).Validate(); err != nil {
		t.Errorf("a timed word was refused: %v", err)
	}
}

func TestReadingResultValidateRefusesWhatTheContractDoesNot(t *testing.T) {
	ok := application.ReadingWord{Text: "cat", Error: application.ErrNone, Accuracy: 50}
	for name, r := range map[string]application.ReadingResult{
		"unknown error kind":  {Words: []application.ReadingWord{{Text: "cat", Error: "stutter"}}},
		"word accuracy high":  {Words: []application.ReadingWord{{Text: "cat", Error: application.ErrNone, Accuracy: 101}}},
		"result accuracy low": {Words: []application.ReadingWord{ok}, Accuracy: -1},
		"word with no text":   {Words: []application.ReadingWord{{Error: application.ErrNone}}},
		"negative seconds":    {Words: []application.ReadingWord{ok}, Seconds: -1},
		"no words at all":     {},
	} {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
	if err := (application.ReadingResult{Words: []application.ReadingWord{ok}, Accuracy: 50}).Validate(); err != nil {
		t.Errorf("a good result was refused: %v", err)
	}
}

func TestReadingResultValidateAcceptsNotReachedForTheWordsAfterWhereTheReadingStopped(t *testing.T) {
	r := application.ReadingResult{Accuracy: 100, Seconds: 1, Words: []application.ReadingWord{
		{Text: "the", Error: application.ErrNone, Accuracy: 100},
		{Text: "cat", Error: application.ErrNotReached},
	}}
	if err := r.Validate(); err != nil {
		t.Errorf("a result with a word not reached was refused: %v", err)
	}
	out, _ := json.Marshal(r.Words[1])
	if !strings.Contains(string(out), `"error":"not_reached"`) {
		t.Errorf("expected the kind spelled not_reached in %s", out)
	}
}

func TestScorerRegistryFindsAnEngineAndListsTheConfiguredOnesWhenOneIsMissing(t *testing.T) {
	local := &apptest.FakeScorer{}
	reg := application.NewScorerRegistry(map[string]application.Scorer{"local": local, "azure": &apptest.FakeScorer{}})
	got, err := reg.Get("local")
	if err != nil || got != application.Scorer(local) {
		t.Fatalf("expected the local engine, got %v %v", got, err)
	}
	_, err = reg.Get("nosuch")
	if err == nil || !strings.Contains(err.Error(), `"nosuch"`) || !strings.Contains(err.Error(), "azure, local") {
		t.Fatalf("expected the configured engines named, sorted, got %v", err)
	}
	if names := reg.Names(); strings.Join(names, ",") != "azure,local" {
		t.Errorf("expected sorted names, got %v", names)
	}
}

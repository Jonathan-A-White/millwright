package application_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestParseEvalReadsBlocksSkipsCommentsAndSplitsSlugs(t *testing.T) {
	got, err := application.ParseEval("# note\nQ: What is bd?\nexpect: a, b\n\n\n# more\nQ: Where is the tmux socket?\nexpect: c\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []application.EvalQuestion{
		{Question: "What is bd?", Expect: []string{"a", "b"}, Line: 2},
		{Question: "Where is the tmux socket?", Expect: []string{"c"}, Line: 7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestEvalQuestionTermsDropStopWordsAndPunctuation(t *testing.T) {
	q := application.EvalQuestion{Question: "How does a Builder run the tests, in the worktree?"}
	if got, want := q.Terms(), []string{"builder", "run", "tests", "worktree"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseEvalRefusesABlockThatIsNotWhole(t *testing.T) {
	for text, want := range map[string]string{
		"Q: What is bd?\n":               "line 1",
		"expect: a\n":                    "an expect with no Q",
		"Q: one\nQ: two\nexpect: a\n":    "line 2",
		"Q: one\nexpect: a\nexpect: b\n": "line 3",
		"Q: one\nexpect: ,\n":            "names no slug",
		"Q: What is the?\nexpect: a\n":   "no words to look for",
		"Q: one\nexpect: a\nfree text\n": "line 3",
	} {
		if _, err := application.ParseEval(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want an error saying %q", text, err, want)
		}
	}
}

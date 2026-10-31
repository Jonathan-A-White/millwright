package application_test

import (
	"bytes"
	"context"
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

// staleFact is a fact file whose status is set by its extra front matter lines.
func staleFact(status, extra, sentence string) string {
	return "---\nsubject: zoo\nkind: gotcha\nstatus: " + status + "\nsource: mw-1\nsince: 2026-09-01\n" + extra + "---\n\n" + sentence + "\n"
}

// staleMemory is a rig whose facts hold every kind of expect the eval can
// meet: a dead fact behind a chain, a retiree with and without a successor, a
// current fact and a flagged one. Each family has its own animal, so a question
// finds only its own.
func staleMemory(eval string, out *bytes.Buffer) (application.Memory, *editableFiles, *recordingVault) {
	files := &editableFiles{
		files: map[string]string{
			"old-a.md":  staleFact("superseded", "superseded-by: mid-a\n", "A zebra once."),
			"mid-a.md":  staleFact("superseded", "supersedes: old-a\nsuperseded-by: head-a\n", "A zebra later."),
			"head-a.md": staleFact("current", "supersedes: mid-a\n", "A zebra now."),
			"old-b.md":  staleFact("superseded", "superseded-by: head-b\n", "A yak once."),
			"head-b.md": staleFact("retired", "retired: 2026-10-01\nreason: gone\n", "A yak now."),
			"cur-b.md":  staleFact("current", "", "A yak always."),
			"old-c.md":  staleFact("retired", "retired: 2026-10-01\nreason: merged into head-c (mw-2)\n", "A wolf once."),
			"head-c.md": staleFact("current", "", "A wolf now."),
			"old-d.md":  staleFact("superseded", "superseded-by: head-d\n", "A vole once."),
			"head-d.md": staleFact("current", "", "A vole now."),
			"old-e.md":  staleFact("retired", "retired: 2026-10-01\nreason: stale\n", "A urial once."),
			"cur-e.md":  staleFact("current", "", "A urial now."),
			"old-f.md":  staleFact("retired", "retired: 2026-10-01\nreason: duplicate\n", "A tapir once."),
			"cur-g.md":  staleFact("current", "", "A stoat now."),
			"flag-h.md": staleFact("recheck", "reason: doubt\n", "A newt maybe."),
		},
		eval: eval,
	}
	vault := &recordingVault{}
	return anEditableMemory(files, vault, out), files, vault
}

const staleEval = `# what a Builder asks about the zoo

Q: zebra
expect: old-a

Q: yak
expect: old-b, cur-b

Q: wolf
expect: old-c

# the vole is asked twice
Q: vole
expect: old-d, head-d

Q: urial
expect: old-e, cur-e

Q: tapir
expect: old-f

Q: stoat
expect: cur-g

Q: newt
expect: flag-h
`

func TestEvalCallsAnExpectOfASupersededOrRetiredFactStaleAndFails(t *testing.T) {
	var out bytes.Buffer
	m, _, _ := staleMemory(staleEval, &out)
	err := m.Eval(context.Background(), "zoo")
	if err == nil || !strings.Contains(err.Error(), "eval failing: 6 of 8") {
		t.Fatalf("got %v, want eval failing: 6 of 8", err)
	}
	want := `STALE old-a: superseded by head-a (current)  zebra
STALE old-b: superseded by head-b (retired)  yak
PASS 1 cur-b  yak
STALE old-c: retired: merged into head-c (mw-2)  wolf
STALE old-d: superseded by head-d (current)  vole
PASS 1 head-d  vole
STALE old-e: retired: stale  urial
PASS 1 cur-e  urial
STALE old-f: retired: duplicate  tapir
PASS 1 cur-g  stoat
PASS 1 flag-h  newt
`
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestEvalFixRewritesEachStaleExpectAndLeavesTheRestAlone(t *testing.T) {
	var out bytes.Buffer
	m, files, vault := staleMemory(staleEval, &out)
	if err := m.EvalFix(context.Background(), "zoo"); err != nil {
		t.Fatal(err)
	}
	wantOut := `replaced old-a with head-a at line 3 (zebra)
dropped old-b at line 6 (yak): superseded by head-b, which is retired
replaced old-c with head-c at line 9 (wolf)
dropped old-d at line 13 (vole): head-d is already expected
dropped old-e at line 16 (urial): retired: stale
removed the question at line 19 (tapir): no expect left (old-f retired: duplicate)
`
	if out.String() != wantOut {
		t.Errorf("printed:\n%s\nwant:\n%s", out.String(), wantOut)
	}
	wantEval := `# what a Builder asks about the zoo

Q: zebra
expect: head-a

Q: yak
expect: cur-b

Q: wolf
expect: head-c

# the vole is asked twice
Q: vole
expect: head-d

Q: urial
expect: cur-e

Q: stoat
expect: cur-g

Q: newt
expect: flag-h
`
	if files.eval != wantEval {
		t.Errorf("eval.md:\n%s\nwant:\n%s", files.eval, wantEval)
	}
	if len(vault.messages) != 1 || !strings.Contains(vault.messages[0], "memory eval fix zoo") {
		t.Errorf("want one commit of the fix, got %v", vault.messages)
	}
	out.Reset()
	if err := m.Eval(context.Background(), "zoo"); err != nil {
		t.Errorf("eval after the fix: %v", err)
	}
	if strings.Contains(out.String(), "STALE") || strings.Contains(out.String(), "FAIL") {
		t.Errorf("eval after the fix:\n%s", out.String())
	}
}

func TestEvalFixWithNothingStaleWritesAndCommitsNothing(t *testing.T) {
	var out bytes.Buffer
	m, files, vault := staleMemory("Q: stoat\nexpect: cur-g\n", &out)
	if err := m.EvalFix(context.Background(), "zoo"); err != nil {
		t.Fatal(err)
	}
	if out.String() != "nothing stale\n" || len(files.wrote) != 0 || len(vault.messages) != 0 {
		t.Errorf("printed %q, wrote %v, committed %v", out.String(), files.wrote, vault.messages)
	}
}

func TestEvalFixKeepsTheLastBlockOfAFileWhole(t *testing.T) {
	var out bytes.Buffer
	m, files, _ := staleMemory("Q: stoat\nexpect: cur-g\n\nQ: tapir\nexpect: old-f\n", &out)
	if err := m.EvalFix(context.Background(), "zoo"); err != nil {
		t.Fatal(err)
	}
	if want := "Q: stoat\nexpect: cur-g\n"; files.eval != want {
		t.Errorf("got %q, want %q", files.eval, want)
	}
}

func TestEvalFixDropsTheStaleOneOfThreeThatShareAChain(t *testing.T) {
	var out bytes.Buffer
	m, files, _ := staleMemory("Q: zebra\nexpect: old-a, mid-a\n", &out)
	if err := m.EvalFix(context.Background(), "zoo"); err != nil {
		t.Fatal(err)
	}
	if want := "Q: zebra\nexpect: head-a\n"; files.eval != want {
		t.Errorf("got %q, want %q", files.eval, want)
	}
}

// rewordMemory is a rig whose zebra fact was superseded by a head that says
// less than the old one did: the head holds the words zebra and stripes only.
func rewordMemory(eval string, out *bytes.Buffer) (application.Memory, *editableFiles, *recordingVault) {
	files := &editableFiles{
		files: map[string]string{
			"old-a.md":  staleFact("superseded", "superseded-by: head-a\n", "A zebra has stripes and 5,700 entries."),
			"head-a.md": staleFact("current", "supersedes: old-a\n", "A zebra has stripes."),
		},
		eval: eval,
	}
	vault := &recordingVault{}
	return anEditableMemory(files, vault, out), files, vault
}

func TestEvalFixRerunsARewrittenQuestionAndSaysNothingWhenItPasses(t *testing.T) {
	var out bytes.Buffer
	m, files, _ := rewordMemory("Q: zebra stripes\nexpect: old-a\n", &out)
	if err := m.EvalFix(context.Background(), "zoo"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "REWORD") || strings.Contains(out.String(), "FAIL") {
		t.Errorf("printed:\n%s", out.String())
	}
	if want := "Q: zebra stripes\nexpect: head-a\n"; files.eval != want {
		t.Errorf("got %q, want %q", files.eval, want)
	}
}

func TestEvalFixNamesTheWordsOfARewrittenQuestionThatTheNewFactLacks(t *testing.T) {
	var out bytes.Buffer
	m, files, _ := rewordMemory("Q: zebra ATTRIBUTION.md 5,700 entries\nexpect: old-a\n", &out)
	err := m.EvalFix(context.Background(), "zoo")
	if err == nil || !strings.Contains(err.Error(), "eval failing: 1 of 1") {
		t.Fatalf("got %v, want eval failing: 1 of 1", err)
	}
	want := "replaced old-a with head-a at line 1 (zebra ATTRIBUTION.md 5,700 entries)\n" +
		"REWORD head-a  zebra ATTRIBUTION.md 5,700 entries  (missing from the fact: attribution.md, 5,700, entries)\n"
	if out.String() != want {
		t.Errorf("printed:\n%s\nwant:\n%s", out.String(), want)
	}
	if want := "Q: zebra ATTRIBUTION.md 5,700 entries\nexpect: head-a\n"; files.eval != want {
		t.Errorf("eval.md: got %q, want %q", files.eval, want)
	}
}

func TestEvalFixRewordDropsTheMissingWordsWhenTheQuestionThenPasses(t *testing.T) {
	var out bytes.Buffer
	m, files, vault := rewordMemory("Q: zebra ATTRIBUTION.md 5,700 entries stripes\nexpect: old-a\n", &out)
	if err := m.EvalFixReword(context.Background(), "zoo"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "REWORD") || strings.Contains(out.String(), "FAIL") {
		t.Errorf("printed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "reworded") {
		t.Errorf("want a line saying the question was reworded, printed:\n%s", out.String())
	}
	if want := "Q: zebra stripes\nexpect: head-a\n"; files.eval != want {
		t.Errorf("got %q, want %q", files.eval, want)
	}
	if len(vault.messages) != 1 {
		t.Errorf("want one commit holding the rewording with the fix, got %v", vault.messages)
	}
}

func TestEvalFixRewordKeepsTheQuestionWhenDroppingWordsCannotRescueIt(t *testing.T) {
	var out bytes.Buffer
	// Every word of the question is one the head lacks: nothing is left to ask.
	m, files, _ := rewordMemory("Q: attribution entries\nexpect: old-a\n", &out)
	err := m.EvalFixReword(context.Background(), "zoo")
	if err == nil || !strings.Contains(err.Error(), "eval failing: 1 of 1") {
		t.Fatalf("got %v, want eval failing: 1 of 1", err)
	}
	if !strings.Contains(out.String(), "REWORD head-a  attribution entries  (missing from the fact: attribution, entries)") {
		t.Errorf("printed:\n%s", out.String())
	}
	if want := "Q: attribution entries\nexpect: head-a\n"; files.eval != want {
		t.Errorf("got %q, want %q", files.eval, want)
	}
}

func TestEvalFixRewordKeepsTheQuestionWhenTheFewerWordsStillMissTheTop(t *testing.T) {
	var out bytes.Buffer
	// With 'entries' dropped the question finds four other zebra facts ahead of head-a.
	files := &editableFiles{
		files: map[string]string{
			"old-a.md":   staleFact("superseded", "superseded-by: zz-head\n", "A zebra has stripes and entries."),
			"zz-head.md": staleFact("current", "supersedes: old-a\n", "A zebra."),
			"z1.md":      staleFact("current", "", "A zebra one."),
			"z2.md":      staleFact("current", "", "A zebra two."),
			"z3.md":      staleFact("current", "", "A zebra three."),
		},
		eval: "Q: zebra entries\nexpect: old-a\n",
	}
	m := anEditableMemory(files, &recordingVault{}, &out)
	err := m.EvalFixReword(context.Background(), "zoo")
	if err == nil {
		t.Fatalf("want a failing eval, printed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "REWORD zz-head  zebra entries  (missing from the fact: entries)") {
		t.Errorf("printed:\n%s", out.String())
	}
	if want := "Q: zebra entries\nexpect: zz-head\n"; files.eval != want {
		t.Errorf("got %q, want %q", files.eval, want)
	}
}

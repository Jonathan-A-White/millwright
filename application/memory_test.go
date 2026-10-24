package application_test

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// queryFiles is a rig's facts in memory, for the memory tests.
type queryFiles struct{ files map[string]string }

func (q queryFiles) ReadRigFacts(context.Context, string, string) (string, map[string]string, bool, error) {
	return "About.", q.files, true, nil
}
func (q queryFiles) WriteRigFact(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (q queryFiles) ReadRigEval(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (q queryFiles) WriteRigEval(context.Context, string, string, string) (string, error) {
	return "", nil
}

// queryLog is the host's query log in memory.
type queryLog struct {
	records []application.MemoryQueryRecord
}

func (l *queryLog) AppendMemoryQuery(_ context.Context, r application.MemoryQueryRecord) error {
	l.records = append(l.records, r)
	return nil
}
func (l *queryLog) ReadMemoryQueries(context.Context) ([]application.MemoryQueryRecord, error) {
	return l.records, nil
}

func queryFact(subject, sentence string) string {
	return "---\nsubject: " + subject + "\nkind: gotcha\nstatus: current\nsource: mw-1\nsince: 2026-09-01\n---\n\n" + sentence + "\n"
}

func aQueryMemory(log application.MemoryQueryLog, out *bytes.Buffer) application.Memory {
	return application.Memory{
		Files: queryFiles{files: map[string]string{
			"a.md": queryFact("bd", "Use -C with bd."),
			"b.md": queryFact("git", "Rebase before landing."),
			"c.md": queryFact("git", "Never push."),
		}},
		Seat: "builder", Out: out, Queries: log, Story: "mw-9",
		Now: func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) },
	}
}

func TestTwoQueriesAreLoggedAndStatsCountsThem(t *testing.T) {
	log := &queryLog{}
	var out bytes.Buffer
	m := aQueryMemory(log, &out)
	ctx := context.Background()

	if err := m.Query(ctx, application.MemoryQuery{Rig: "kiln", Terms: []string{"git"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Query(ctx, application.MemoryQuery{Rig: "kiln", Terms: []string{"nothing", "here"}}); err == nil {
		t.Fatal("expected the second query to find nothing")
	}
	if len(log.records) != 2 {
		t.Fatalf("expected two logged queries, got %+v", log.records)
	}
	first, second := log.records[0], log.records[1]
	if first.Rig != "kiln" || first.Hits != 2 || first.Story != "mw-9" || strings.Join(first.Terms, " ") != "git" ||
		strings.Join(first.Subjects, ",") != "git" {
		t.Errorf("first record: %+v", first)
	}
	if second.Hits != 0 || len(second.Subjects) != 0 {
		t.Errorf("second record: %+v", second)
	}

	out.Reset()
	if err := m.Stats(ctx, application.MemoryStats{Rig: "kiln", Since: "7d"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 queries", "1 found nothing", "git"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stats lack %q:\n%s", want, out.String())
		}
	}
}

func TestStatsLeavesOutOtherRigsAndQueriesBeforeSince(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	log := &queryLog{records: []application.MemoryQueryRecord{
		{At: now.AddDate(0, 0, -1), Rig: "kiln", Hits: 1, Subjects: []string{"bd"}},
		{At: now.AddDate(0, 0, -20), Rig: "kiln", Hits: 1, Subjects: []string{"git"}},
		{At: now.AddDate(0, 0, -1), Rig: "other", Hits: 1, Subjects: []string{"git"}},
	}}
	var out bytes.Buffer
	if err := aQueryMemory(log, &out).Stats(context.Background(), application.MemoryStats{Rig: "kiln", Since: "7d"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 query") || strings.Contains(out.String(), "git") {
		t.Errorf("stats: %s", out.String())
	}
}

func TestTopSubjectsGoByQueriesThenFacts(t *testing.T) {
	facts := []application.RigFact{
		fact("a", "bd", application.FactGotcha, application.FactCurrent, "s", "x"),
		fact("b", "git", application.FactGotcha, application.FactCurrent, "s", "x"),
		fact("c", "git", application.FactGotcha, application.FactCurrent, "s", "x"),
		fact("d", "tests", application.FactGotcha, application.FactCurrent, "s", "x"),
	}
	got := application.TopSubjects(facts, map[string]int{"bd": 3, "tests": 3, "gone": 9}, 3)
	if strings.Join(got, ",") != "bd,tests,git" {
		t.Errorf("got %v", got)
	}
	if got := application.TopSubjects(facts, nil, 10); strings.Join(got, ",") != "git,bd,tests" {
		t.Errorf("by fact count: %v", got)
	}
}

func TestBootNamesTheMostQueriedSubjectsOfAFactsRigAndNothingOtherwise(t *testing.T) {
	facts := []application.RigFact{
		fact("a", "bd", application.FactGotcha, application.FactCurrent, "Use -C.", "mw-2"),
		fact("b", "git", application.FactGotcha, application.FactCurrent, "Rebase.", "mw-3"),
	}
	seat := application.Seat{
		Name: "builder", Rig: "millwright", Charter: "c", Memory: "About.\n", HasFacts: true,
		Facts: facts, SubjectQueries: map[string]int{"git": 4},
	}
	prompt := application.BootPrompt(seat, aStory(nil))
	if !strings.Contains(prompt, "Most queried subjects: git, bd.") || !strings.Contains(prompt, "mw memory query millwright <term>") {
		t.Errorf("boot lacks the subjects line:\n%s", prompt)
	}

	for name, bare := range map[string]application.Seat{
		"a rig kept as one memory file": {Name: "builder", Rig: "millwright", Charter: "c", Memory: "- a line\n"},
		"a facts rig with no facts":     {Name: "builder", Rig: "millwright", Charter: "c", Memory: "About.\n", HasFacts: true},
	} {
		if prompt := application.BootPrompt(bare, aStory(nil)); strings.Contains(prompt, "queried subjects") {
			t.Errorf("%s: boot names subjects:\n%s", name, prompt)
		}
	}
}

func TestSeatBootCountsTheQueriesOfTheRigsSubjects(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	counts := application.SubjectQueryCounts([]application.MemoryQueryRecord{
		{At: now, Rig: "millwright", Subjects: []string{"git", "bd"}},
		{At: now, Rig: "millwright", Subjects: []string{"git"}},
		{At: now, Rig: "other", Subjects: []string{"git"}},
	}, "millwright", time.Time{})
	if counts["git"] != 2 || counts["bd"] != 1 || len(counts) != 2 {
		t.Errorf("counts: %v", counts)
	}
}

func TestListSaysWhenANearFullRenderLeavesTheNewestFactsSearchOnly(t *testing.T) {
	demoted := strings.Replace(queryFact("git", "Never push."), "since: 2026-09-01", "since: 2026-10-01\nboot: no", 1)
	list := func(budget int) string {
		var out bytes.Buffer
		m := application.Memory{
			Files: queryFiles{files: map[string]string{"a.md": queryFact("bd", "Use -C with bd."), "c.md": demoted}},
			Seat:  "builder", Out: &out, Budget: budget,
		}
		if err := m.List(context.Background(), application.MemoryList{Rig: "kiln"}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := list(50); !strings.Contains(got, "1 of the newest facts are search-only: run `mw memory make-room kiln --bytes N`") {
		t.Errorf("expected the search-only line:\n%s", got)
	}
	if got := list(8000); strings.Contains(got, "search-only") {
		t.Errorf("expected no line with room in the render:\n%s", got)
	}
}

func TestListCurrentOnlyHidesEveryFactThatIsNotCurrentAndKeepsTheRenderLine(t *testing.T) {
	retired := strings.Replace(queryFact("git", "Never push."), "status: current", "status: retired\nretired: 2026-10-01\nreason: old", 1)
	list := func(current bool) string {
		var out bytes.Buffer
		m := application.Memory{
			Files: queryFiles{files: map[string]string{"a.md": queryFact("bd", "Use -C with bd."), "c.md": retired}},
			Seat:  "builder", Out: &out, Budget: 8000,
		}
		if err := m.List(context.Background(), application.MemoryList{Rig: "kiln", Current: current}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := list(false); !strings.Contains(got, "c  retired") || !strings.Contains(got, "a  current") {
		t.Errorf("without the flag every fact is listed:\n%s", got)
	}
	got := list(true)
	if strings.Contains(got, "retired") || !strings.Contains(got, "a  current") {
		t.Errorf("--current lists the current fact and no retired one:\n%s", got)
	}
	if !strings.Contains(got, "render ") {
		t.Errorf("--current still ends with the render size:\n%s", got)
	}
	var out bytes.Buffer
	m := application.Memory{Files: queryFiles{files: map[string]string{"a.md": queryFact("bd", "x")}}, Seat: "builder", Out: &out}
	if err := m.List(context.Background(), application.MemoryList{Rig: "kiln", Current: true, Status: application.FactRetired}); err == nil {
		t.Errorf("--current with --status should be refused")
	}
}

// editableFiles is a rig's facts and eval.md in memory that a verb can write.
type editableFiles struct {
	files map[string]string
	eval  string
	wrote []string
}

func (e *editableFiles) ReadRigFacts(context.Context, string, string) (string, map[string]string, bool, error) {
	return "About.", e.files, true, nil
}
func (e *editableFiles) WriteRigFact(_ context.Context, _, _, slug, text string) (string, error) {
	e.files[slug+".md"] = text
	e.wrote = append(e.wrote, slug+".md")
	return "/vault/" + slug + ".md", nil
}
func (e *editableFiles) ReadRigEval(context.Context, string, string) (string, bool, error) {
	return e.eval, e.eval != "", nil
}
func (e *editableFiles) WriteRigEval(_ context.Context, _, _, text string) (string, error) {
	e.eval = text
	e.wrote = append(e.wrote, "eval.md")
	return "/vault/eval.md", nil
}

// recordingVault notes each commit's message and paths.
type recordingVault struct {
	messages []string
	paths    [][]string
}

func (r *recordingVault) Commit(_ context.Context, message string, paths []string) ([]string, error) {
	r.messages = append(r.messages, message)
	r.paths = append(r.paths, paths)
	return paths, nil
}
func (r *recordingVault) Push(context.Context) (int, error) { return 1, nil }

func anEditableMemory(files *editableFiles, vault *recordingVault, out *bytes.Buffer) application.Memory {
	return application.Memory{
		Files: files, Vault: vault, Seat: "builder", Out: out,
		Now: func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) },
	}
}

func TestAddNamesACurrentFactNearTheNewOneAndStillWritesIt(t *testing.T) {
	files := &editableFiles{files: map[string]string{
		"window-open-noopener-returns-null.md": queryFact("window.open", "window.open with noopener always returns null, so test the popup by its URL."),
		"bd-c.md":                              queryFact("bd", "Use -C with bd."),
	}}
	var out bytes.Buffer
	m := anEditableMemory(files, &recordingVault{}, &out)
	err := m.Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "window.open", Source: "mw-2", Kind: application.FactGotcha,
		Sentence: "A window.open with noopener in its features always returns null.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files.files["a-windowopen-with-noopener-in.md"]; !ok {
		t.Errorf("the fact was not written: %v", files.wrote)
	}
	if !strings.Contains(out.String(), "near: window-open-noopener-returns-null window.open with noopener") {
		t.Errorf("expected a near line:\n%s", out.String())
	}
	if strings.Contains(out.String(), "near: bd-c") {
		t.Errorf("a fact of another subject and other words is not near:\n%s", out.String())
	}
}

func TestAddSaysNothingNearWhenNoCurrentFactIsClose(t *testing.T) {
	retired := strings.Replace(queryFact("git", "Rebase before landing."), "status: current", "status: retired\nretired: 2026-10-01\nreason: old", 1)
	files := &editableFiles{files: map[string]string{"old.md": retired}}
	var out bytes.Buffer
	err := anEditableMemory(files, &recordingVault{}, &out).Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "git", Source: "mw-2", Kind: application.FactGotcha, Sentence: "Rebase before landing.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "near:") {
		t.Errorf("a retired fact is not near:\n%s", out.String())
	}
}

func TestAddSaysItAddedTheFactAfterTheNearLines(t *testing.T) {
	files := &editableFiles{files: map[string]string{
		"window-open-noopener-returns-null.md": queryFact("window.open", "window.open with noopener always returns null, so test the popup by its URL."),
	}}
	var out bytes.Buffer
	err := anEditableMemory(files, &recordingVault{}, &out).Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "window.open", Source: "mw-2", Kind: application.FactGotcha,
		Sentence: "A window.open with noopener in its features always returns null.",
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	last := lines[len(lines)-1]
	if last != "added a-windowopen-with-noopener-in (near: window-open-noopener-returns-null)" {
		t.Errorf("the last line should say the fact was added, naming the near fact:\n%s", out.String())
	}
	if len(lines) < 3 || !strings.HasPrefix(lines[len(lines)-2], "near: ") {
		t.Errorf("the near line comes before the added line:\n%s", out.String())
	}
}

func TestAddSaysAddedAloneWhenNoFactIsNear(t *testing.T) {
	files := &editableFiles{files: map[string]string{}}
	var out bytes.Buffer
	err := anEditableMemory(files, &recordingVault{}, &out).Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "git", Source: "mw-2", Kind: application.FactGotcha, Slug: "rebase-first", Sentence: "Rebase before landing.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "added rebase-first\n") || strings.Contains(out.String(), "(near:") {
		t.Errorf("expected a plain 'added rebase-first':\n%s", out.String())
	}
}

func TestAddSaysNotAddedAndReturnsTheErrorWhenItRefuses(t *testing.T) {
	taken := &editableFiles{files: map[string]string{"taken.md": queryFact("bd", "A fact.")}}
	for name, req := range map[string]application.MemoryAdd{
		"a taken slug": {Rig: "kiln", Subject: "bd", Source: "mw-2", Kind: application.FactGotcha, Slug: "taken", Sentence: "Another one."},
		"a bad kind":   {Rig: "kiln", Subject: "bd", Source: "mw-2", Kind: "rumour", Sentence: "Another one."},
	} {
		var out bytes.Buffer
		err := anEditableMemory(taken, &recordingVault{}, &out).Add(context.Background(), req)
		if err == nil {
			t.Fatalf("%s: add should be refused", name)
		}
		if want := "not added: " + err.Error() + "\n"; out.String() != want {
			t.Errorf("%s: printed %q, want %q", name, out.String(), want)
		}
	}
	var out bytes.Buffer
	m := application.Memory{Files: &noRigFiles{}, Seat: "builder", Out: &out}
	err := m.Add(context.Background(), application.MemoryAdd{Rig: "ghost", Subject: "bd", Source: "mw-1", Kind: application.FactGotcha, Sentence: "Anything at all."})
	if err == nil || !strings.HasPrefix(out.String(), "not added: ") {
		t.Errorf("an unknown rig should print 'not added: ...' and return an error: %v / %q", err, out.String())
	}
}

// noRigFiles is a seat with no folder for any rig.
type noRigFiles struct{ editableFiles }

func (noRigFiles) ReadRigFacts(context.Context, string, string) (string, map[string]string, bool, error) {
	return "", nil, false, nil
}

func TestTheOtherVerbsSayWhatTheyDid(t *testing.T) {
	ctx := context.Background()
	fact := func() *editableFiles {
		f := demotable()
		f.files["old.md"] = queryFact("gate", "The gate is old.")
		return f
	}
	cases := []struct {
		name string
		run  func(application.Memory) error
		want string
	}{
		{"supersede", func(m application.Memory) error {
			return m.Supersede(ctx, application.MemorySupersede{Rig: "kiln", Old: "old", Slug: "new", Source: "mw-3", Sentence: "The gate is new."})
		}, "superseded old with new\n"},
		{"retire", func(m application.Memory) error { return m.Retire(ctx, "kiln", "old", "gone") }, "retired old\n"},
		{"recheck", func(m application.Memory) error { return m.Recheck(ctx, "kiln", "old", "doubt") }, "flagged old to recheck\n"},
		{"demote", func(m application.Memory) error { return m.Demote(ctx, "kiln", "gate", "gate make lint") }, "demoted gate out of the boot render\n"},
	}
	for _, c := range cases {
		var out bytes.Buffer
		if err := c.run(anEditableMemory(fact(), &recordingVault{}, &out)); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.HasSuffix(out.String(), c.want) {
			t.Errorf("%s: output should end %q:\n%s", c.name, c.want, out.String())
		}
	}
	demoted := demotable()
	demoted.files["gate.md"] = strings.Replace(demoted.files["gate.md"], "status: current", "status: current\nboot: no", 1)
	var out bytes.Buffer
	if err := anEditableMemory(demoted, &recordingVault{}, &out).Promote(ctx, "kiln", "gate"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "promoted gate into the boot render\n") {
		t.Errorf("promote should say what it did:\n%s", out.String())
	}
}

func demotable() *editableFiles {
	return &editableFiles{
		files: map[string]string{"gate.md": queryFact("gate", "The gate runs make test then make lint.")},
		eval:  "Q: where is the tmux socket\nexpect: other\n",
	}
}

func TestDemoteWithAQuestionAppendsItToTheEvalAndCommitsBothFilesTogether(t *testing.T) {
	files, vault := demotable(), &recordingVault{}
	var out bytes.Buffer
	err := anEditableMemory(files, vault, &out).Demote(context.Background(), "kiln", "gate", "gate make lint")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Q: where is the tmux socket\nexpect: other\n\nQ: gate make lint\nexpect: gate\n"; files.eval != want {
		t.Errorf("eval.md:\n%q\nwant\n%q", files.eval, want)
	}
	if !strings.Contains(files.files["gate.md"], "boot: no") {
		t.Errorf("the fact is not demoted:\n%s", files.files["gate.md"])
	}
	if len(vault.paths) != 1 || len(vault.paths[0]) != 2 {
		t.Fatalf("expected one commit of two files, got %v", vault.paths)
	}
	if got := strings.Join(vault.paths[0], " "); !strings.Contains(got, "eval.md") || !strings.Contains(got, "gate.md") {
		t.Errorf("the commit lacks a file: %v", vault.paths[0])
	}
}

func TestDemoteWithAQuestionThatDoesNotFindTheFactRefusesAndChangesNothing(t *testing.T) {
	files, vault := demotable(), &recordingVault{}
	before := files.eval
	err := anEditableMemory(files, vault, &bytes.Buffer{}).Demote(context.Background(), "kiln", "gate", "unrelated words")
	if err == nil || !strings.Contains(err.Error(), "no eval question finds gate") {
		t.Fatalf("expected a refusal, got %v", err)
	}
	if files.eval != before || len(files.wrote) != 0 || len(vault.paths) != 0 {
		t.Errorf("something changed: eval %q, wrote %v, commits %v", files.eval, files.wrote, vault.paths)
	}
}

func TestDemoteWithAQuestionThatDoesNotFindTheFactRefusesEvenWhenAnotherQuestionDoes(t *testing.T) {
	files, vault := demotable(), &recordingVault{}
	files.eval += "\nQ: gate make test\nexpect: gate\n"
	before, factBefore := files.eval, files.files["gate.md"]
	err := anEditableMemory(files, vault, &bytes.Buffer{}).Demote(context.Background(), "kiln", "gate", "unrelated words")
	if err == nil || !strings.Contains(err.Error(), "does not put it in the top 3") {
		t.Fatalf("expected a refusal naming the question, got %v", err)
	}
	if files.eval != before || files.files["gate.md"] != factBefore || len(files.wrote) != 0 || len(vault.paths) != 0 {
		t.Errorf("something changed: eval %q, wrote %v, commits %v", files.eval, files.wrote, vault.paths)
	}
}

func TestDemoteWithAQuestionThatFindsTheFactAppendsItWhenAnotherQuestionAlreadyDoes(t *testing.T) {
	files, vault := demotable(), &recordingVault{}
	files.eval += "\nQ: gate make test\nexpect: gate\n"
	err := anEditableMemory(files, vault, &bytes.Buffer{}).Demote(context.Background(), "kiln", "gate", "gate make lint")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(files.eval, "\nQ: gate make lint\nexpect: gate\n") {
		t.Errorf("the question was not appended:\n%q", files.eval)
	}
	if !strings.Contains(files.files["gate.md"], "boot: no") {
		t.Errorf("the fact is not demoted:\n%s", files.files["gate.md"])
	}
}

func TestDemoteWithoutAQuestionAcceptsAnyExistingQuestionThatFindsTheFact(t *testing.T) {
	files, vault := demotable(), &recordingVault{}
	files.eval += "\nQ: gate make test\nexpect: gate\n"
	before := files.eval
	if err := anEditableMemory(files, vault, &bytes.Buffer{}).Demote(context.Background(), "kiln", "gate", ""); err != nil {
		t.Fatal(err)
	}
	if files.eval != before || !strings.Contains(files.files["gate.md"], "boot: no") {
		t.Errorf("eval %q, fact %s", files.eval, files.files["gate.md"])
	}
}

// aDemotedGate is a rig with a demoted fact "gate" (boot: no) that an eval
// question finds, and a booted fact "other".
func aDemotedGate() *editableFiles {
	files := demotable()
	files.files["gate.md"] = strings.Replace(files.files["gate.md"], "status: current", "status: current\nboot: no", 1)
	files.eval += "\nQ: gate make lint\nexpect: gate\n"
	return files
}

func TestSupersedingADemotedFactWritesTheNewFactDemotedToo(t *testing.T) {
	files, vault := aDemotedGate(), &recordingVault{}
	err := anEditableMemory(files, vault, &bytes.Buffer{}).Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "The gate runs make test and make lint in the foreground.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files.files["gate-new.md"], "boot: no") {
		t.Errorf("the new fact is in the boot render:\n%s", files.files["gate-new.md"])
	}
}

func TestSupersedingADemotedFactCarriesItsEvalQuestionToTheNewFact(t *testing.T) {
	files, vault := aDemotedGate(), &recordingVault{}
	var out bytes.Buffer
	err := anEditableMemory(files, vault, &out).Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "The gate runs make test and make lint in the foreground.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files.eval, "Q: gate make lint\nexpect: gate, gate-new\n") {
		t.Errorf("the question does not expect the new fact:\n%s", files.eval)
	}
	if strings.Contains(files.eval, "expect: other, gate-new") {
		t.Errorf("a question of another fact was changed:\n%s", files.eval)
	}
	if len(vault.paths) != 1 || !strings.Contains(strings.Join(vault.paths[0], " "), "eval.md") {
		t.Errorf("eval.md is not in the commit: %v", vault.paths)
	}
	if strings.Contains(out.String(), "WARN") && strings.Contains(out.String(), "no eval question") {
		t.Errorf("warned of no question though one finds the new fact:\n%s", out.String())
	}
}

// aCrowdedDemotedGate is a rig with a demoted fact "gate" that the question
// "gate make lint" expects, and two current facts of an earlier subject that the
// question finds too: while "gate" is current it and they fill the top three
// ahead of a new fact of its subject.
func aCrowdedDemotedGate() *editableFiles {
	files := aDemotedGate()
	files.files["alpha-1.md"] = queryFact("alpha", "The gate make lint step one.")
	files.files["alpha-2.md"] = queryFact("alpha", "The gate make lint step two.")
	return files
}

func TestSupersedingADemotedFactThatTheOldOneCrowdedOutLeavesTheQuestionPassing(t *testing.T) {
	files, vault := aCrowdedDemotedGate(), &recordingVault{}
	files.eval = "Q: gate make lint\nexpect: gate\n"
	var out bytes.Buffer
	memory := anEditableMemory(files, vault, &out)
	err := memory.Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "The gate runs make test and make lint in the foreground.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "WARN: eval question") || strings.Contains(out.String(), "WARN: no eval question") {
		t.Errorf("warned though a question finds the new fact:\n%s", out.String())
	}
	out.Reset()
	if err := memory.Eval(context.Background(), "kiln"); err != nil {
		t.Fatalf("the eval fails after the supersede: %v\n%s\n%s", err, out.String(), files.eval)
	}
	if !strings.Contains(files.eval, "Q: gate make lint\nexpect: gate-new\n") {
		t.Errorf("the superseded slug was not replaced:\n%s", files.eval)
	}
	if !strings.Contains(out.String(), "PASS 3 gate-new") {
		t.Errorf("the new fact does not pass at the rank it now has:\n%s", out.String())
	}
}

func TestSupersedingAFactTheQuestionStillFindsKeepsTheOldSlugAndAddsTheNew(t *testing.T) {
	files, vault := aDemotedGate(), &recordingVault{}
	files.eval = "Q: gate make lint\nexpect: gate\n"
	memory := anEditableMemory(files, vault, &bytes.Buffer{})
	err := memory.Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "The gate runs make test and make lint in the foreground.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files.eval, "Q: gate make lint\nexpect: gate, gate-new\n") {
		t.Errorf("eval.md:\n%s", files.eval)
	}
	if err := memory.Eval(context.Background(), "kiln"); err != nil {
		t.Fatal(err)
	}
}

func TestSupersedingADemotedFactThatAQuestionOfTheOldOneDoesNotFindNamesTheQuestion(t *testing.T) {
	files, vault := aCrowdedDemotedGate(), &recordingVault{}
	files.files["alpha-3.md"] = queryFact("alpha", "The gate make lint step three.")
	files.eval = "Q: gate make lint\nexpect: gate\n"
	var out bytes.Buffer
	memory := anEditableMemory(files, vault, &out)
	err := memory.Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "Nothing alike at all.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "WARN: eval question at line 1 (gate make lint) expects gate, which is superseded now, and does not find gate-new: it will fail until an expect is added") {
		t.Errorf("no warning naming the question:\n%s", out.String())
	}
	if !strings.Contains(files.files["gate-new.md"], "boot: no") || !strings.Contains(files.files["gate.md"], "superseded") {
		t.Errorf("the supersede did not happen:\n%s\n%s", files.files["gate-new.md"], files.files["gate.md"])
	}
	if !strings.Contains(files.eval, "expect: gate\n") || strings.Contains(files.eval, "gate-new") {
		t.Errorf("eval.md was changed:\n%s", files.eval)
	}
}

func TestSupersedingABootFactWarnsOfAQuestionThatDoesNotFindTheNewFactEvenWhenTheOldOneIsStillFound(t *testing.T) {
	files, vault := demotable(), &recordingVault{}
	files.eval = "Q: gate make test\nexpect: gate\n"
	var out bytes.Buffer
	err := anEditableMemory(files, vault, &out).Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "Nothing alike at all.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "WARN: eval question at line 1 (gate make test) expects gate") || !strings.Contains(out.String(), "until an expect is added") {
		t.Errorf("no warning naming the question:\n%s", out.String())
	}
	if strings.Contains(out.String(), "no eval question finds") {
		t.Errorf("a boot fact needs no question to be found:\n%s", out.String())
	}
}

func TestSupersedingADemotedFactNoQuestionFindsWarnsAndKeepsItDemoted(t *testing.T) {
	files, vault := aDemotedGate(), &recordingVault{}
	var out bytes.Buffer
	err := anEditableMemory(files, vault, &out).Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "Nothing alike at all.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files.files["gate-new.md"], "boot: no") {
		t.Errorf("the new fact is in the boot render:\n%s", files.files["gate-new.md"])
	}
	if !strings.Contains(out.String(), "WARN: no eval question finds gate-new") {
		t.Errorf("no warning that no question finds the new fact:\n%s", out.String())
	}
}

func TestSupersedingABootFactKeepsItInTheBootRender(t *testing.T) {
	files, vault := demotable(), &recordingVault{}
	before := files.eval
	err := anEditableMemory(files, vault, &bytes.Buffer{}).Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "The gate runs make test and make lint in the foreground.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(files.files["gate-new.md"], "boot:") || files.eval != before {
		t.Errorf("fact:\n%s\neval: %q", files.files["gate-new.md"], files.eval)
	}
}

func TestSupersedePrintsTheRenderSizeAndWarnsOverTheBudget(t *testing.T) {
	for _, c := range []struct {
		budget int
		want   string
		warn   bool
	}{{100000, "render ", false}, {10, "WARN: the render is over the budget", true}} {
		files := demotable()
		var out bytes.Buffer
		m := anEditableMemory(files, &recordingVault{}, &out)
		m.Budget = c.budget
		err := m.Supersede(context.Background(), application.MemorySupersede{
			Rig: "kiln", Old: "gate", Slug: "gate-new", Source: "mw-4", Sentence: "The gate runs make test and make lint in the foreground.",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "render ") || !strings.Contains(out.String(), "/"+strconv.Itoa(c.budget)+" bytes") {
			t.Errorf("budget %d: no render size line:\n%s", c.budget, out.String())
		}
		if got := strings.Contains(out.String(), "WARN"); got != c.warn {
			t.Errorf("budget %d: warn %v:\n%s", c.budget, got, out.String())
		}
		if !strings.HasSuffix(out.String(), "superseded gate with gate-new\n") {
			t.Errorf("the last line should say what was done:\n%s", out.String())
		}
	}
}

func TestADerivedSlugThatCollidesNamesItAndSaysToPassSlug(t *testing.T) {
	sentence := "The gate runs ten parts each time."
	derived := application.FactSlug(sentence)
	taken := func() *editableFiles {
		f := &editableFiles{files: map[string]string{
			derived + ".md": queryFact("gate", "A fact."),
			"old.md":        queryFact("gate", "The gate is old."),
		}}
		return f
	}
	check := func(verb string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: expected a refusal", verb)
		}
		for _, want := range []string{`"` + derived + `"`, "--slug <new>"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the error lacks %q: %v", verb, want, err)
			}
		}
	}
	check("supersede", anEditableMemory(taken(), &recordingVault{}, &bytes.Buffer{}).Supersede(context.Background(), application.MemorySupersede{
		Rig: "kiln", Old: "old", Source: "mw-4", Sentence: sentence,
	}))
	check("add", anEditableMemory(taken(), &recordingVault{}, &bytes.Buffer{}).Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "gate", Source: "mw-4", Kind: application.FactGotcha, Sentence: sentence,
	}))
}

func TestAnExplicitSlugThatCollidesDoesNotClaimItWasDerived(t *testing.T) {
	files := &editableFiles{files: map[string]string{"taken.md": queryFact("bd", "A fact.")}}
	err := anEditableMemory(files, &recordingVault{}, &bytes.Buffer{}).Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "bd", Source: "mw-2", Kind: application.FactGotcha, Slug: "taken", Sentence: "Another one.",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "first five words") {
		t.Errorf("got %v", err)
	}
}

// roomFact is a fact file with the kind and since date a make-room test needs.
func roomFact(subject, kind, since, sentence string) string {
	return "---\nsubject: " + subject + "\nkind: " + kind + "\nstatus: current\nsource: mw-1\nsince: " + since + "\n---\n\n" + sentence + "\n"
}

// roomLine is what a fact adds to the render, newline included.
func roomLine(subject, sentence string) int {
	return len("- [" + subject + "] " + sentence + " (mw-1)\n")
}

// aFullRig is a rig whose boot render sits exactly at its budget: the oldest
// gotchas are a decision, 'His' and 'Decided by the Governor' sentences (never
// demoted), then three plain ones, then a fact of yesterday and one of two days
// ago, which are too new to demote. The clock says 2026-10-18.
func aFullRig() (*editableFiles, application.Memory, *bytes.Buffer, *recordingVault) {
	files := &editableFiles{files: map[string]string{
		"dec.md":    roomFact("rules", "decision", "2026-07-01", "Landings always go through the merge slot of the host."),
		"his.md":    roomFact("rules", "gotcha", "2026-07-02", "His rule is that a story never touches the target branch."),
		"gov.md":    roomFact("rules", "gotcha", "2026-07-03", "Decided by the Governor: a charter changes only on his word."),
		"alpha.md":  roomFact("alpha", "gotcha", "2026-09-01", "The alpha service restarts only after its socket file is removed by hand."),
		"bravo.md":  roomFact("bravo", "gotcha", "2026-09-05", "The bravo ledger keeps one line a landing and never rewrites an old line."),
		"charly.md": roomFact("charly", "gotcha", "2026-09-09", "The charly fixture builds its vault in a temporary directory every run."),
		"two.md":    roomFact("delta", "gotcha", "2026-10-16", "The delta probe answers only when the window is named by its id."),
		"one.md":    roomFact("delta", "gotcha", "2026-10-17", "The echo gateway drops a message whose body is larger than the cap."),
	}}
	var out bytes.Buffer
	m := anEditableMemory(files, &recordingVault{}, &out)
	vault := &recordingVault{}
	m.Vault = vault
	list, _ := application.LoadRigFacts(files.files)
	m.Budget = len(application.RenderRigMemory("About.", list))
	return files, m, &out, vault
}

func renderSize(files *editableFiles) int {
	list, _ := application.LoadRigFacts(files.files)
	return len(application.RenderRigMemory("About.", list))
}

func demoted(files *editableFiles, slug string) bool {
	return strings.Contains(files.files[slug+".md"], "boot: no")
}

func TestMakeRoomDemotesTheOldestEligibleGotchasAndNeverTheRest(t *testing.T) {
	files, m, out, vault := aFullRig()
	want := roomLine("alpha", "The alpha service restarts only after its socket file is removed by hand.") + 5
	if err := m.MakeRoom(context.Background(), application.MemoryMakeRoom{Rig: "kiln", Bytes: want}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// alpha alone is 5 bytes short of want: bravo goes too, and charly stays.
	for slug, isDemoted := range map[string]bool{
		"alpha": true, "bravo": true, "charly": false,
		"dec": false, "his": false, "gov": false, "two": false, "one": false,
	} {
		if demoted(files, slug) != isDemoted {
			t.Errorf("%s demoted = %v, want %v\n%s", slug, demoted(files, slug), isDemoted, out)
		}
	}
	if free := m.Budget - renderSize(files); free < want {
		t.Errorf("%d bytes free, want at least %d", free, want)
	}
	var evalOut bytes.Buffer
	m.Out = &evalOut
	if err := m.Eval(context.Background(), "kiln"); err != nil {
		t.Fatalf("eval fails after make-room: %v\n%s", err, evalOut.String())
	}
	for _, slug := range []string{"alpha", "bravo"} {
		if !strings.Contains(evalOut.String(), "PASS 1 "+slug) && !strings.Contains(evalOut.String(), "PASS 2 "+slug) &&
			!strings.Contains(evalOut.String(), "PASS 3 "+slug) {
			t.Errorf("no eval question passes for %s:\n%s", slug, evalOut.String())
		}
	}
	if !strings.Contains(files.eval, "Q: The alpha service restarts only after its socket\nexpect: alpha") {
		t.Errorf("the question is not the first eight words of the sentence:\n%s", files.eval)
	}
	if len(vault.messages) != 2 {
		t.Errorf("want one commit a demotion, got %v", vault.messages)
	}
}

func TestMakeRoomWithRoomAlreadySaysSoAndChangesNothing(t *testing.T) {
	files, m, out, _ := aFullRig()
	m.Budget += 100
	if err := m.MakeRoom(context.Background(), application.MemoryMakeRoom{Rig: "kiln", Bytes: 50}); err != nil {
		t.Fatal(err)
	}
	if len(files.wrote) != 0 || !strings.Contains(out.String(), "nothing to demote") {
		t.Errorf("wrote %v, said:\n%s", files.wrote, out)
	}
}

func TestMakeRoomSkipsAFactWhoseOwnSentenceDoesNotFindIt(t *testing.T) {
	files, m, out, _ := aFullRig()
	// Four facts open with the same eight words: the oldest sorts last by
	// subject, so its question puts it fourth and it is passed over.
	same := "Always run the gate before landing any change, said "
	files.files["zed.md"] = roomFact("zed", "gotcha", "2026-06-01", same+"zed.")
	for _, s := range []string{"a", "b", "c"} {
		files.files[s+"-same.md"] = roomFact(s, "gotcha", "2026-06-02", same+s+".")
	}
	list, _ := application.LoadRigFacts(files.files)
	m.Budget = len(application.RenderRigMemory("About.", list))
	if err := m.MakeRoom(context.Background(), application.MemoryMakeRoom{Rig: "kiln", Bytes: 10}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if demoted(files, "zed") {
		t.Errorf("zed was demoted though its question puts it fourth:\n%s", files.eval)
	}
	if !demoted(files, "a-same") || !strings.Contains(out.String(), "skipped zed") {
		t.Errorf("a-same should be the one taken, zed named as skipped:\n%s", out)
	}
}

func TestMakeRoomTakesTheLeastQueriedSubjectAmongFactsOfOneDay(t *testing.T) {
	files, m, out, _ := aFullRig()
	files.files["bravo.md"] = roomFact("bravo", "gotcha", "2026-09-01", "The bravo ledger keeps one line a landing and never rewrites an old line.")
	m.Queries = &queryLog{records: []application.MemoryQueryRecord{
		{At: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC), Rig: "kiln", Hits: 1, Subjects: []string{"alpha"}},
		{At: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC), Rig: "kiln", Hits: 1, Subjects: []string{"alpha"}},
	}}
	if err := m.MakeRoom(context.Background(), application.MemoryMakeRoom{Rig: "kiln", Bytes: 10}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !demoted(files, "bravo") || demoted(files, "alpha") {
		t.Errorf("the less queried bravo should go before alpha:\n%s", out)
	}
}

func TestMakeRoomThatCannotFreeEnoughRefusesAndChangesNothing(t *testing.T) {
	files, m, out, vault := aFullRig()
	err := m.MakeRoom(context.Background(), application.MemoryMakeRoom{Rig: "kiln", Bytes: 5000})
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("want a refusal, got %v\n%s", err, out)
	}
	if len(files.wrote) != 0 || len(vault.messages) != 0 {
		t.Errorf("wrote %v, committed %v", files.wrote, vault.messages)
	}
}

func TestMakeRoomDryRunPrintsThePlanAndWritesNothing(t *testing.T) {
	files, m, out, vault := aFullRig()
	before := map[string]string{}
	for k, v := range files.files {
		before[k] = v
	}
	evalBefore := files.eval
	size := renderSize(files)
	want := roomLine("alpha", "The alpha service restarts only after its socket file is removed by hand.") + 5
	if err := m.MakeRoom(context.Background(), application.MemoryMakeRoom{Rig: "kiln", Bytes: want, DryRun: true}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, line := range []string{
		"would demote alpha", "would demote bravo",
		fmt.Sprintf("render before: %d/%d bytes", size, m.Budget),
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("the plan lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(out.String(), "would demote charly") || !strings.Contains(out.String(), "render after: ") {
		t.Errorf("the plan:\n%s", out)
	}
	if len(files.wrote) != 0 || len(vault.messages) != 0 || files.eval != evalBefore {
		t.Errorf("a dry run wrote %v, committed %v", files.wrote, vault.messages)
	}
	for k, v := range before {
		if files.files[k] != v {
			t.Errorf("%s changed", k)
		}
	}
	if len(before) != len(files.files) {
		t.Errorf("the facts folder grew")
	}
}

func TestAddWithMakeRoomMakesRoomForItsOwnLineThenAddsIt(t *testing.T) {
	files, m, out, _ := aFullRig()
	err := m.Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "clk", Source: "mw-1", Kind: application.FactGotcha, MakeRoom: true,
		Sentence: "The clock fixture starts at midnight UTC of the day.",
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if size := renderSize(files); size > m.Budget {
		t.Errorf("the render is %d/%d after the add", size, m.Budget)
	}
	added := files.files["the-clock-fixture-starts-at.md"]
	if added == "" || demoted(files, "the-clock-fixture-starts-at") {
		t.Errorf("the new fact is missing or demoted:\n%s", added)
	}
	if !demoted(files, "alpha") {
		t.Errorf("the oldest eligible gotcha was not demoted:\n%s", out)
	}
}

func TestAddOverBudgetWithoutMakeRoomPrintsTheCommandAndTheOverage(t *testing.T) {
	files, m, out, _ := aFullRig()
	err := m.Add(context.Background(), application.MemoryAdd{
		Rig: "kiln", Subject: "clk", Source: "mw-1", Kind: application.FactGotcha,
		Sentence: "The clock fixture starts at midnight UTC of the day.",
	})
	if err != nil {
		t.Fatal(err)
	}
	over := renderSize(files) - m.Budget
	for _, want := range []string{
		fmt.Sprintf("over the budget by %d bytes", over), "mw memory make-room kiln --bytes 0",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the add does not say %q:\n%s", want, out)
		}
	}
	if demoted(files, "alpha") {
		t.Errorf("without --make-room nothing is demoted")
	}
}

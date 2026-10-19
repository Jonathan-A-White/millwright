package application_test

import (
	"bytes"
	"context"
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
	if got := list(50); !strings.Contains(got, "1 of the newest facts are search-only: prune") {
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

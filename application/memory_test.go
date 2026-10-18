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

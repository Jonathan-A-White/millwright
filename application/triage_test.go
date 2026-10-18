package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

var triageNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func daysAgo(n int) time.Time { return triageNow.AddDate(0, 0, -n) }

func liveBead(id string) application.TriageBead {
	return application.TriageBead{ID: id, Title: id, Type: "task", Status: "open", Priority: 2,
		Created: daysAgo(10), Updated: daysAgo(1)}
}

func fixtureBeads() []application.TriageBead {
	a := liveBead("t-1")
	a.Labels = []string{"b", "a"}
	a.Rig = "millwright"
	a.Description = strings.Repeat("x", 600)
	blocked := liveBead("t-2")
	blocked.Needs = []string{"t-1"}
	done := liveBead("t-3")
	done.Status = "closed"
	done.ClosedAt = daysAgo(3)
	waited := liveBead("t-4")
	waited.Needs = []string{"t-3"}
	mail := liveBead("t-5")
	mail.Type = "mail"
	step := liveBead("mw-mol-abc")
	root := liveBead("mw-mol-root")
	root.Type = "molecule"
	event := liveBead("t-6")
	event.Type = "event"
	return []application.TriageBead{a, blocked, done, waited, mail, step, root, event}
}

func TestDumpWritesOneLinePerLiveWorkBead(t *testing.T) {
	var out bytes.Buffer
	dump := application.TriageDump{Source: &apptest.FakeTriage{Beads: fixtureBeads()}, Now: func() time.Time { return triageNow }}
	lines, err := dump.Run(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("dumped %d beads, want 3 (t-1, t-2, t-4)", len(lines))
	}
	rows := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(rows) != 3 {
		t.Fatalf("wrote %d lines, want 3:\n%s", len(rows), out.String())
	}
	var first application.TriageLine
	if err := json.Unmarshal([]byte(rows[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.ID != "t-1" || first.Rig != "millwright" || first.Priority != 2 || first.DaysSinceUpdate != 1 {
		t.Fatalf("first line wrong: %+v", first)
	}
	if len([]rune(first.Description)) != 500 {
		t.Fatalf("description is %d runes, want 500", len([]rune(first.Description)))
	}
	if !reflect.DeepEqual(first.Labels, []string{"a", "b"}) {
		t.Fatalf("labels %v, want sorted", first.Labels)
	}
	if !reflect.DeepEqual(first.Dependents, []string{"t-2"}) {
		t.Fatalf("t-1 dependents %v, want t-2", first.Dependents)
	}
}

func TestDumpSaysWhichBeadsAreReadyAndForHowLong(t *testing.T) {
	dump := application.TriageDump{Source: &apptest.FakeTriage{Beads: fixtureBeads()}, Now: func() time.Time { return triageNow }}
	lines, err := dump.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]application.TriageLine{}
	for _, l := range lines {
		by[l.ID] = l
	}
	if !by["t-1"].Ready || by["t-1"].DaysReady != 10 {
		t.Fatalf("t-1 ready %v for %d days, want ready for 10 (since filed)", by["t-1"].Ready, by["t-1"].DaysReady)
	}
	if by["t-2"].Ready || !reflect.DeepEqual(by["t-2"].Deps, []string{"t-1"}) {
		t.Fatalf("t-2 should wait on t-1: %+v", by["t-2"])
	}
	if !by["t-4"].Ready || by["t-4"].DaysReady != 3 || len(by["t-4"].Deps) != 0 {
		t.Fatalf("t-4 became ready when t-3 closed 3 days ago: %+v", by["t-4"])
	}
}

func TestHisPriorityIsTrueWhereTheDescriptionQuotesHimSettingOne(t *testing.T) {
	for name, text := range map[string]string{
		"prioritize":    "The Governor said: prioritize this over the rest.",
		"to P0":         "He moved it to P0 on Tuesday.",
		"postern":       "PRIORITY 1 set by the Governor via postern, txid direct:abc",
		"prioritise":    "Please Prioritise the nightly pass",
		"plain-to-form": "bump to p1 now",
	} {
		if !application.HisPriority(text) {
			t.Errorf("%s: %q should be his_priority", name, text)
		}
	}
	for name, text := range map[string]string{
		"plain priority word": "Set the priority of the cache higher.",
		"priority queue":      "A priority queue orders the work; P0 is the top.",
		"empty":               "",
	} {
		if application.HisPriority(text) {
			t.Errorf("%s: %q should not be his_priority", name, text)
		}
	}
}

func TestABeadLabelledHisPriorityIsHisPriorityWhateverItsDescriptionSays(t *testing.T) {
	labelled := liveBead("t-lab")
	labelled.Labels = []string{"x", "his-priority"}
	labelled.Description = "Reason: the Governor said SpellForge goes up now."
	plain := liveBead("t-plain")
	plain.Labels = []string{"x"}
	plain.Description = "Set the priority of the cache higher."
	bare := liveBead("t-bare")
	bare.Description = "The Governor said SpellForge goes up now."
	by := map[string]application.TriageLine{}
	for _, line := range dumpOf(t, labelled, plain, bare) {
		by[line.ID] = line
	}
	if !by["t-lab"].HisPriority {
		t.Errorf("a bead labelled his-priority should be his_priority: %+v", by["t-lab"])
	}
	if by["t-plain"].HisPriority || by["t-bare"].HisPriority {
		t.Errorf("a bead without the label or a description shape should not be: %+v %+v", by["t-plain"], by["t-bare"])
	}
}

func dumpOf(t *testing.T, beads ...application.TriageBead) []application.TriageLine {
	t.Helper()
	lines, err := application.TriageDump{Source: &apptest.FakeTriage{Beads: beads}, Now: func() time.Time { return triageNow }}.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

func TestDiffListsWhatMovedBetweenTwoDumps(t *testing.T) {
	mk := func(id string, edit func(*application.TriageBead)) application.TriageBead {
		b := liveBead(id)
		if edit != nil {
			edit(&b)
		}
		return b
	}
	old := dumpOf(t,
		mk("keep", nil),
		mk("gone", nil),
		mk("pri", nil),
		mk("stat", nil),
		mk("lab", nil),
		mk("both", nil),
		mk("blocker", nil),
		mk("waiting", func(b *application.TriageBead) { b.Needs = []string{"blocker"} }),
		mk("aging", func(b *application.TriageBead) { b.Updated = daysAgo(10) }),
	)
	fresh := dumpOf(t,
		mk("keep", nil),
		mk("new", nil),
		mk("pri", func(b *application.TriageBead) { b.Priority = 0 }),
		mk("stat", func(b *application.TriageBead) { b.Status = "in_progress" }),
		mk("lab", func(b *application.TriageBead) { b.Labels = []string{"icebox"} }),
		mk("both", func(b *application.TriageBead) { b.Priority = 4; b.Status = "deferred" }),
		mk("waiting", nil),
		mk("aging", func(b *application.TriageBead) { b.Updated = daysAgo(20) }),
	)
	diff := application.DiffTriage(old, fresh)

	if !reflect.DeepEqual(diff.New, []string{"new"}) {
		t.Errorf("new = %v", diff.New)
	}
	if !reflect.DeepEqual(diff.Closed, []string{"blocker", "gone"}) {
		t.Errorf("closed = %v", diff.Closed)
	}
	var moved []string
	for _, m := range diff.Moved {
		moved = append(moved, m.ID)
	}
	if !reflect.DeepEqual(moved, []string{"both", "lab", "pri", "stat"}) {
		t.Errorf("moved = %v", moved)
	}
	if !reflect.DeepEqual(diff.NewlyReady, []string{"waiting"}) {
		t.Errorf("newly ready = %v", diff.NewlyReady)
	}
	if !reflect.DeepEqual(diff.NewlyStale, []string{"aging"}) {
		t.Errorf("newly stale = %v", diff.NewlyStale)
	}

	var buf bytes.Buffer
	if err := diff.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var back struct {
		Moved []json.RawMessage `json:"moved"`
	}
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || len(back.Moved) != 4 {
		t.Fatalf("json moved = %d (%v)", len(back.Moved), err)
	}
	if text := diff.String(); !strings.Contains(text, "pri") || !strings.Contains(text, "P2") {
		t.Errorf("text form should say pri moved from P2:\n%s", text)
	}
}

func TestDiffReadsWhatDumpWrote(t *testing.T) {
	var out bytes.Buffer
	_, err := application.TriageDump{Source: &apptest.FakeTriage{Beads: fixtureBeads()}, Now: func() time.Time { return triageNow }}.Run(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := application.ReadTriageLines(&out)
	if err != nil || len(lines) != 3 {
		t.Fatalf("read %d lines back (%v), want 3", len(lines), err)
	}
}

func orphanFixture() *apptest.FakeTriage {
	story := liveBead("s-1")
	story.Status = "closed"
	story.Molecule = "mw-mol-r1"
	live := liveBead("s-2")
	live.Molecule = "mw-mol-r2"
	root1 := liveBead("mw-mol-r1")
	root1.Type = "molecule"
	step1 := liveBead("mw-mol-a1")
	step1.Parent = "mw-mol-r1"
	root2 := liveBead("mw-mol-r2")
	root2.Type = "molecule"
	step2 := liveBead("mw-mol-a2")
	step2.Parent = "mw-mol-r2"
	return &apptest.FakeTriage{Beads: []application.TriageBead{story, live, root1, step1, root2, step2}}
}

func TestSweepOrphansListsAndChangesNothingUnlessApplied(t *testing.T) {
	fake := orphanFixture()
	var out bytes.Buffer
	sweep := application.TriageOrphans{Source: fake, Closer: fake, Out: &out}
	found, err := sweep.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(found, []string{"mw-mol-a1", "mw-mol-r1"}) {
		t.Fatalf("orphans = %v", found)
	}
	if len(fake.Closed()) != 0 {
		t.Fatalf("a dry run closed %v", fake.Closed())
	}
	if !strings.Contains(out.String(), "mw-mol-a1") {
		t.Fatalf("dry run should list them:\n%s", out.String())
	}

	sweep.Apply = true
	if _, err := sweep.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.Closed(), []string{"mw-mol-a1", "mw-mol-r1"}) {
		t.Fatalf("applied closes %v, want the step then its root", fake.Closed())
	}
}

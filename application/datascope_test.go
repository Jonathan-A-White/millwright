package application

import (
	"context"
	"strings"
	"testing"
)

// mw-gq6.1589: a story that changes only a rig's data files has the one part of its
// data scope in place of the rig's gate.

// dataKeyed is a gate named as parts that also has a data key for a rig.
type dataKeyed struct {
	*partsThatRan
	scope DataScope
}

func (d dataKeyed) DataScope(rig string) (DataScope, bool) {
	return d.scope, d.scope.Command != "" && rig == "lampas"
}

type filesChanged []string

func (f filesChanged) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return f, nil
}
func (filesChanged) AddedFiles(context.Context, string, string, string) ([]AddedFile, error) {
	return nil, nil
}
func (filesChanged) DiffLines(context.Context, string, string, string, string) ([]DiffLine, error) {
	return nil, nil
}

var lxxData = DataScope{Paths: []string{"data/lxx/**", "docs/lxx-*.md", "public/data/audio/msb-david"}, Command: "npx vitest run lxx"}

func dataGateFor(files []string, scope DataScope) *dataGate {
	c := aStoryOf(1)
	return dataGateOf(context.Background(), dataKeyed{elevenParts(), scope}, filesChanged(files), "origin/main", c)
}

func TestAStoryChangingOnlyFilesInsideTheDataPathsIsGatedInItsDataScope(t *testing.T) {
	gate := dataGateFor([]string{"data/lxx/choices/gen-01.tsv", "docs/lxx-notes.md", "public/data/audio/msb-david/psalm-23.json"}, lxxData)
	if gate == nil {
		t.Fatal("expected a gate in the data scope")
	}
	if gate.files != 3 || len(gate.parts()) != 1 || gate.parts()[0].Name != "data scope" || gate.parts()[0].Command != lxxData.Command {
		t.Errorf("expected one part running the data command over 3 files, got %+v", gate)
	}
	if !strings.Contains(gate.words(), "3 changed file(s)") {
		t.Errorf("expected the words to count the files, got %q", gate.words())
	}
}

func TestOneChangedFileOutsideTheDataPathsLeavesTheWholeGate(t *testing.T) {
	for _, files := range [][]string{
		{"data/lxx/choices/gen-01.tsv", "src/review.ts"},
		{"src/review.ts"},
		{"docs/other.md"},
		{},
	} {
		if gate := dataGateFor(files, lxxData); gate != nil {
			t.Errorf("expected no data scope for %v, got %+v", files, gate)
		}
	}
}

func TestARigWithNoDataKeyIsNeverInItsDataScope(t *testing.T) {
	if gate := dataGateFor([]string{"data/lxx/a.tsv"}, DataScope{}); gate != nil {
		t.Errorf("expected none for a rig with no key, got %+v", gate)
	}
	// A gate that cannot say it has a data key, and a check with no way to read the
	// branch's files, read none either.
	c := aStoryOf(1)
	if gate := dataGateOf(context.Background(), elevenParts(), filesChanged{"data/lxx/a.tsv"}, "origin/main", c); gate != nil {
		t.Errorf("expected none from a gate with no data key, got %+v", gate)
	}
	c = aStoryOf(1)
	if gate := dataGateOf(context.Background(), dataKeyed{elevenParts(), lxxData}, nil, "origin/main", c); gate != nil {
		t.Errorf("expected none with no way to read the files, got %+v", gate)
	}
}

func TestADataScopeIsReadOncePerCloseOut(t *testing.T) {
	c := aStoryOf(1)
	first := dataGateOf(context.Background(), dataKeyed{elevenParts(), lxxData}, filesChanged{"data/lxx/a.tsv"}, "origin/main", c)
	second := dataGateOf(context.Background(), dataKeyed{elevenParts(), lxxData}, filesChanged{"src/x.ts"}, "origin/main", c)
	if first == nil || second != first {
		t.Errorf("expected the first read to stand, got %+v then %+v", first, second)
	}
}

// The records of the rig's own parts say nothing of a data-scope story: its one
// part is the data check, and a pass of it at the tip is every part there is.
func TestADataScopePassAtTheTipIsTrustedAndTheLedgerSaysSo(t *testing.T) {
	gate, records := elevenParts(), partRecords{}
	// The rig's part 1 passed at the tip too, which is not the data check.
	records["mw-x.1/"+CheckPartFileName(1, 2)] = passedAt("abc")
	n := Next{GateParts: dataKeyed{gate, lxxData}, GateRecords: records}
	c := aStoryOf(1)
	c.data, c.dataRead = dataGateFor([]string{"data/lxx/a.tsv", "data/lxx/b.tsv"}, lxxData), true

	if p := n.readPartsAtTip(context.Background(), c, "abc"); p != nil {
		t.Fatalf("expected a record of the rig's part 2 to say nothing of the data part, got %+v", p)
	}

	records["mw-x.1/"+CheckPartFileName(1, 1)] = passedAt("abc")
	c.parts = n.readPartsAtTip(context.Background(), c, "abc")
	if !c.parts.all() {
		t.Fatalf("expected the one data part read as passed at the tip, got %+v", c.parts)
	}
	if _, stop := n.trustedPartsRefusal(context.Background(), c, &NextReport{}, "origin/main"); stop {
		t.Fatal("expected no refusal")
	}
	if len(gate.ran) != 0 {
		t.Errorf("expected no part run, got %v", gate.ran)
	}
	want := "tested by mw check, data scope at abc: pass (2 changed file(s)); changed specs run at close-out"
	if got := c.parts.tested(); got != want {
		t.Errorf("expected the ledger to say %q, got %q", want, got)
	}
}

func TestACheckLogOfTheDataScopeIsToldFromAWholeOne(t *testing.T) {
	data := "check of mw-x.1 (attempt 1) began on laptop at 10:00:00Z; its log is written as the check goes\n" +
		"  data scope  3 changed file(s), every one inside the [data_parts.lampas] paths, so the rig's data check runs in place of the rig's parts: `npx vitest run lxx`\n" +
		"  tip     abc\n  passed  1 commit(s) on mw/mw-x.1, none signed, every formula step closed, the rig's tests pass: mw next would land it\n  x\n"
	if files, ok := CheckedDataScope(data); !ok || files != 3 {
		t.Errorf("expected a check of the data scope over 3 files, got %d, %v", files, ok)
	}
	whole := "check of mw-x.1 (attempt 1) began on laptop\n  note    the gate ran as 3 parts and every part passed at the tip\n"
	if _, ok := CheckedDataScope(whole); ok {
		t.Error("expected a whole check's log not to be read as the data scope's")
	}
}

func TestAPartOfADataScopeStoryIsNamedForTheDataScope(t *testing.T) {
	g := &dataGate{scope: lxxData, rig: "lampas", files: 1}
	if got := partLabel(1, 1, g.part()); got != "part 1 of 1 (data scope)" {
		t.Errorf("unexpected label %q", got)
	}
}

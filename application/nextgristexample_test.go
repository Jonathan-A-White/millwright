package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// changedStub says every branch has changed the files it holds, or fails.
type changedStub struct {
	branchChangesStub
	err error
}

func (c changedStub) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return c.branchChangesStub, c.err
}

func gristExampleRefusal(t *testing.T, changes BranchChanges, comment string) (Refusal, bool, *NextReport) {
	t.Helper()
	report := &NextReport{}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", branch: "mw/mw-t.1", target: "main"}
	refusal, refused := Next{Changes: changes}.gristExampleRefusal(context.Background(), c, report, comment)
	return refusal, refused, report
}

const verseExample = "grinds/examples/bible-talk/verse-text-ask.json"

func TestABranchThatAddsAGrindExampleWithNoLiveEvalLineInItsClosingCommentIsRefused(t *testing.T) {
	for name, comment := range map[string]string{
		"no eval line":         "Added the example. Tests pass on the fake mill.",
		"no PASS":              "mw grist eval lampas bible-talk --example verse-text-ask: FAILED 2/2",
		"another example":      "mw grist eval lampas bible-talk --example verse-ask PASS",
		"another kind":         "mw grist eval lampas verse-read --example verse-text-ask PASS",
		"the example as prose": "I ran verse-text-ask live and it PASSED",
	} {
		refusal, refused, _ := gristExampleRefusal(t, changedStub{branchChangesStub: branchChangesStub{verseExample}}, comment)
		if !refused || refusal.Reason != ReasonGristExampleUnproved {
			t.Fatalf("%s: expected a refusal for %s, got %+v, %v", name, ReasonGristExampleUnproved, refusal, refused)
		}
		for _, want := range []string{"bible-talk/verse-text-ask", "mw grist eval <app> bible-talk --example verse-text-ask"} {
			if !strings.Contains(refusal.Why+refusal.Said, want) {
				t.Errorf("%s: expected the refusal to say %q, got %+v", name, want, refusal)
			}
		}
	}
}

func TestEachChangedGrindExampleNeedsItsOwnPassingEvalLine(t *testing.T) {
	changes := changedStub{branchChangesStub: branchChangesStub{verseExample, "grinds/examples/tips/first.json", "grinds/examples/README.md", "grinds/bible-talk.json"}}
	comment := "mw grist eval lampas bible-talk --example verse-text-ask   PASS\n"
	refusal, refused, _ := gristExampleRefusal(t, changes, comment)
	if !refused || !strings.Contains(refusal.Why, "tips/first") || strings.Contains(refusal.Why, "verse-text-ask") {
		t.Fatalf("expected a refusal naming only tips/first, got %+v, %v", refusal, refused)
	}
	if strings.Contains(refusal.Why+refusal.Said, "README") {
		t.Errorf("expected the README not to count as an example, got %+v", refusal)
	}
}

func TestAPassingLiveEvalLineForEveryChangedGrindExampleLetsTheBranchThrough(t *testing.T) {
	changes := changedStub{branchChangesStub: branchChangesStub{verseExample, "grinds/examples/tips/first.json"}}
	for name, comment := range map[string]string{
		"a line each":           "- `mw grist eval lampas bible-talk --example verse-text-ask`: PASS\n- mw grist eval lampas tips --example first -> PASS",
		"two examples one line": "mw grist eval lampas bible-talk --example verse-text-ask --example other PASS\nmw grist eval lampas tips --example first PASS",
	} {
		if refusal, refused, _ := gristExampleRefusal(t, changes, comment); refused {
			t.Errorf("%s: expected no refusal, got %+v", name, refusal)
		}
	}
}

func TestABranchThatChangesNoGrindExampleIsNotAskedForAnEvalLine(t *testing.T) {
	for name, changes := range map[string]BranchChanges{
		"no files":        changedStub{},
		"other files":     changedStub{branchChangesStub: branchChangesStub{"application/next.go", "grinds/bible-talk.json", "grinds/examples/README.md", "grinds/examples/bible-talk/photo.webm"}},
		"no Changes port": nil,
	} {
		if refusal, refused, _ := gristExampleRefusal(t, changes, "Internal."); refused {
			t.Errorf("%s: expected no refusal, got %+v", name, refusal)
		}
	}
}

func TestABranchWhoseChangedFilesCannotBeReadIsNotRefusedButSaysSo(t *testing.T) {
	refusal, refused, report := gristExampleRefusal(t, changedStub{err: errors.New("git is gone")}, "x")
	if refused {
		t.Fatalf("expected no refusal, got %+v", refusal)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "git is gone") {
		t.Errorf("expected one note naming the failure, got %q", report.Notes)
	}
}

func TestAnUnprovedGrindExampleHandsTheBranchOnToTheNextAttempt(t *testing.T) {
	if !ContinuesFrom(ReasonGristExampleUnproved) {
		t.Error("expected the branch to be continued: the Builder mends it by running the eval")
	}
}

// forwardingStub is a BranchChanges that also reads the grind files of the
// branch: the grinds it holds, by kind, as grinds/<kind>.json text.
type forwardingStub struct {
	changedStub
	grinds map[string]string
}

func (f forwardingStub) ReadBranchFile(_ context.Context, _, _, file string) ([]byte, bool, error) {
	if slices.Contains(f.branchChangesStub, file) && gristExampleExpr.MatchString(file) {
		return []byte("{}"), true, nil
	}
	for kind, text := range f.grinds {
		if file == "grinds/"+kind+".json" {
			return []byte(text), true, nil
		}
	}
	return nil, false, nil
}

const makerExample = "grinds/examples/maker-request/tutor-ask.json"

func TestAGrindExampleOfAGrindThatForwardsIsNotRefusedAndTheReportSaysItWasNotEvaluated(t *testing.T) {
	changes := forwardingStub{
		changedStub: changedStub{branchChangesStub: branchChangesStub{makerExample}},
		grinds:      map[string]string{"maker-request": `{"grind":1,"kind":"maker-request","forward":"mayor"}`},
	}
	refusal, refused, report := gristExampleRefusal(t, changes, "Added the example.")
	if refused {
		t.Fatalf("expected no refusal for an example of a grind that forwards, got %+v", refusal)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "not evaluated: maker-request forwards to mayor") {
		t.Errorf("expected one note 'not evaluated: maker-request forwards to mayor', got %q", report.Notes)
	}
}

func TestAGrindExampleOfAGrindThatDoesNotForwardIsStillRefused(t *testing.T) {
	changes := forwardingStub{
		changedStub: changedStub{branchChangesStub: branchChangesStub{verseExample}},
		grinds:      map[string]string{"bible-talk": `{"grind":1,"kind":"bible-talk","model":"sonnet"}`},
	}
	if refusal, refused, _ := gristExampleRefusal(t, changes, "Added the example."); !refused || refusal.Reason != ReasonGristExampleUnproved {
		t.Fatalf("expected %s, got %+v, %v", ReasonGristExampleUnproved, refusal, refused)
	}
}

func TestOnlyTheForwardingKindsExamplesAreSpared(t *testing.T) {
	changes := forwardingStub{
		changedStub: changedStub{branchChangesStub: branchChangesStub{makerExample, verseExample}},
		grinds: map[string]string{
			"maker-request": `{"forward":"mayor"}`,
			"bible-talk":    `{"model":"sonnet"}`,
		},
	}
	refusal, refused, _ := gristExampleRefusal(t, changes, "")
	if !refused || strings.Contains(refusal.Why, "maker-request") || !strings.Contains(refusal.Why, "bible-talk/verse-text-ask") {
		t.Fatalf("expected a refusal naming only bible-talk/verse-text-ask, got %+v, %v", refusal, refused)
	}
}

func TestAGrindFileSaysWhoItForwardsToThroughOneFunction(t *testing.T) {
	if who, ok := (GrindFile{Forward: "mayor"}).Forwards(); !ok || who != "mayor" {
		t.Errorf("expected a forward to mayor, got %q, %v", who, ok)
	}
	if who, ok := (GrindFile{}).Forwards(); ok || who != "" {
		t.Errorf("expected no forward, got %q, %v", who, ok)
	}
}

// deletingStub is a BranchChanges that also reads the files of the branch: it
// holds the files in held and no other, so a path that changed but is not held
// was deleted by the branch.
type deletingStub struct {
	changedStub
	held map[string]string
}

func (d deletingStub) ReadBranchFile(_ context.Context, _, _, file string) ([]byte, bool, error) {
	text, found := d.held[file]
	return []byte(text), found, nil
}

func TestAGrindExampleTheBranchDeletesIsNotRefusedAndTheReportSaysItWasDeleted(t *testing.T) {
	changes := deletingStub{changedStub: changedStub{branchChangesStub: branchChangesStub{verseExample}}}
	refusal, refused, report := gristExampleRefusal(t, changes, "Retired the grind.")
	if refused {
		t.Fatalf("expected no refusal for an example the branch deletes, got %+v", refusal)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "bible-talk/verse-text-ask deleted, not evaluated") {
		t.Errorf("expected one note 'bible-talk/verse-text-ask deleted, not evaluated', got %q", report.Notes)
	}
}

func TestABranchThatDeletesAWholeGrindIsNotRefused(t *testing.T) {
	changes := deletingStub{changedStub: changedStub{branchChangesStub: branchChangesStub{
		"grinds/bible-talk.json", verseExample, "grinds/examples/bible-talk/other.json"}}}
	if refusal, refused, _ := gristExampleRefusal(t, changes, "Retired the grind."); refused {
		t.Fatalf("expected no refusal for a grind the branch deletes with its examples, got %+v", refusal)
	}
}

func TestAnExampleTheBranchChangesIsRefusedWhileAnotherItDeletesIsNot(t *testing.T) {
	changes := deletingStub{
		changedStub: changedStub{branchChangesStub: branchChangesStub{verseExample, "grinds/examples/bible-talk/gone.json"}},
		held:        map[string]string{verseExample: "{}"},
	}
	refusal, refused, report := gristExampleRefusal(t, changes, "")
	if !refused || !strings.Contains(refusal.Why, "bible-talk/verse-text-ask") || strings.Contains(refusal.Why, "gone") {
		t.Fatalf("expected a refusal naming only bible-talk/verse-text-ask, got %+v, %v", refusal, refused)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "bible-talk/gone deleted") {
		t.Errorf("expected a note that bible-talk/gone was deleted, got %q", report.Notes)
	}
}

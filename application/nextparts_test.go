package application

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-6ww.119.11.1.1: a close-out of a batch rig trusts the parts of a split gate
// that passed at the queued tip, and runs the rest only.

type partRecords map[string]string

func (r partRecords) PutRunFile(_ context.Context, storyID, name, contents string) (string, error) {
	r[storyID+"/"+name] = contents
	return name, nil
}

func (r partRecords) ReadRunFile(_ context.Context, storyID, name string) (string, error) {
	if text, ok := r[storyID+"/"+name]; ok {
		return text, nil
	}
	return "", fs.ErrNotExist
}

// partsThatRan is a gate named as parts whose every part passes, and which
// remembers the commands it was asked to run.
type partsThatRan struct {
	parts []GatePart
	ran   []string
}

func (p *partsThatRan) Parts(string) []GatePart { return p.parts }

func (p *partsThatRan) RunLine(_ context.Context, _, _, command string) (Checked, error) {
	p.ran = append(p.ran, command)
	return Checked{Command: command, Passed: true}, nil
}

func elevenParts() *partsThatRan {
	gate := &partsThatRan{}
	for i := 1; i <= 11; i++ {
		gate.parts = append(gate.parts, GatePart{Name: fmt.Sprintf("p%d", i), Command: fmt.Sprintf("run-%d", i)})
	}
	return gate
}

func aStoryOf(attempts int) *closeOut {
	return &closeOut{id: "mw-x.1", path: domain.Path{Rig: "lampas"}, detail: StoryDetail{Attempts: attempts}, worktree: "/wt"}
}

func passedAt(tip string) string { return "tip " + tip + "\npassed\n" }

func TestEveryPartPassedAtTheTipRunsNoPart(t *testing.T) {
	gate, records := elevenParts(), partRecords{}
	for i := 1; i <= 11; i++ {
		records["mw-x.1/"+CheckPartFileName(1, i)] = passedAt("abc")
	}
	n := Next{GateParts: gate, GateRecords: records}
	c := aStoryOf(1)

	c.parts = n.readPartsAtTip(context.Background(), c, "abc")

	if !c.parts.all() {
		t.Fatalf("expected all 11 parts read as passed at the tip, got %+v", c.parts)
	}
	// The close-out of such a story runs the changed specs only: with none named
	// by the host there is nothing to run, and no part is.
	if refusal, stop := n.trustedPartsRefusal(context.Background(), c, &NextReport{}, "origin/main"); stop {
		t.Fatalf("expected no refusal, got %+v", refusal)
	}
	if len(gate.ran) != 0 {
		t.Errorf("expected no part run, got %v", gate.ran)
	}
	want := "tested by mw check, parts at abc: pass; changed specs run at close-out"
	if got := c.parts.tested("abc"); got != want {
		t.Errorf("expected the ledger to say %q, got %q", want, got)
	}
}

func TestAPartAtAnOlderTipIsTheOnlyPartRun(t *testing.T) {
	gate, records := elevenParts(), partRecords{}
	for i := 1; i <= 11; i++ {
		tip := "abc"
		if i == 4 {
			tip = "old"
		}
		records["mw-x.1/"+CheckPartFileName(2, i)] = passedAt(tip)
	}
	n := Next{GateParts: gate, GateRecords: records}
	c := aStoryOf(2)
	c.parts = n.readPartsAtTip(context.Background(), c, "abc")
	if c.parts.all() || c.parts.has(4) || !c.parts.has(3) {
		t.Fatalf("expected ten parts passed at the tip and not part 4, got %+v", c.parts)
	}

	// Under queue precedence the gate keeps its lock and takes no turns.
	checked, err := n.partsTakingTurns(WithQueuePrecedence(context.Background()), c, &NextReport{}, "/wt", gate.parts)
	if err != nil || !checked.Passed {
		t.Fatalf("expected the gate to pass, got %+v, %v", checked, err)
	}
	if len(gate.ran) != 1 || gate.ran[0] != "run-4" {
		t.Errorf("expected part 4 alone to run, got %v", gate.ran)
	}
	want := "tested by mw check, parts 1, 2, 3, 5, 6, 7, 8, 9, 10, 11 of 11 at abc: pass; part 4 (p4) run at close-out: pass"
	if got := c.parts.tested("abc"); got != want {
		t.Errorf("expected the ledger to say %q, got %q", want, got)
	}
}

func TestAPartPassedAtTheTipOfAnotherAttemptOrFailedOrWithNoTipIsRun(t *testing.T) {
	gate, records := elevenParts(), partRecords{}
	records["mw-x.1/"+CheckPartFileName(1, 1)] = passedAt("abc")
	records["mw-x.1/"+CheckPartFileName(1, 2)] = "tip abc\nfailed\n"
	records["mw-x.1/"+CheckPartFileName(1, 3)] = "tip -\npassed\n"
	records["mw-x.1/"+CheckPartFileName(2, 4)] = passedAt("abc")
	n := Next{GateParts: gate, GateRecords: records}
	c := aStoryOf(1)

	c.parts = n.readPartsAtTip(context.Background(), c, "abc")

	if len(c.parts.passed) != 1 || !c.parts.has(1) {
		t.Errorf("expected only part 1 read as passed, got %+v", c.parts)
	}
}

func TestNoPartPassedAtTheTipReadsNothing(t *testing.T) {
	n := Next{GateParts: elevenParts(), GateRecords: partRecords{}}
	c := aStoryOf(1)
	if p := n.readPartsAtTip(context.Background(), c, "abc"); p != nil {
		t.Errorf("expected nothing read, got %+v", p)
	}
	if got := (*partsAtTip)(nil).tested("abc"); got != "" {
		t.Errorf("expected no ledger words for no trusted part, got %q", got)
	}
	if (*partsAtTip)(nil).all() {
		t.Errorf("expected a gate with no read parts not to be all passed")
	}
}

func TestAPassedSpecsRecordAtTheTipIsNotRunAgain(t *testing.T) {
	records := partRecords{}
	records["mw-x.1/"+CheckPartFileName(1, 1)] = passedAt("abc")
	records["mw-x.1/"+CheckSpecsResultFileName(1)] = `{"tip":"abc","result":"passed"}` + "\n"
	gate := &partsThatRan{parts: []GatePart{{Name: "only", Command: "run-1"}}}
	n := Next{GateParts: gate, GateRecords: records}
	c := aStoryOf(1)

	c.parts = n.readPartsAtTip(context.Background(), c, "abc")
	c.parts.skipped = c.parts.passed

	if !c.parts.specsPassed() {
		t.Fatalf("expected the specs read as passed at the tip, got %+v", c.parts)
	}
	if got, want := c.parts.tested("abc"), "changed specs passed at abc"; !strings.Contains(got, want) {
		t.Errorf("expected the ledger to say %q, got %q", want, got)
	}

	records["mw-x.1/"+CheckSpecsResultFileName(1)] = `{"tip":"old","result":"passed"}` + "\n"
	c.parts = n.readPartsAtTip(context.Background(), c, "abc")
	if c.parts.specsPassed() {
		t.Errorf("expected specs that passed at an older tip to be run again")
	}
}

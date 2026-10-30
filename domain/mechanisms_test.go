package domain_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

// mw-gq6.852: an L or XL story whose criteria touch more than one mechanism is
// more than one session finishes; the criteria are named so the Mayor can split.
const manyMechanisms = `1. A new setting next.foo is read from the config, default 10.
2. A new component, the Foo screen, shows the setting.
3. The old bar command is removed, with its tests.
4. The rig's gate is green.
Sweep: every place that prints foo is listed.
Expect: refusals down`

const oneMechanism = `1. A new setting next.foo is read from the config, default 10.
2. A second new setting, next.bar, is read the same way.
3. The rig's gate is green.
Expect: refusals down`

func TestMechanismsNamesEachMechanismAndItsCriteria(t *testing.T) {
	got := domain.AcceptanceMechanisms(manyMechanisms)
	if len(got) != 3 {
		t.Fatalf("expected 3 mechanisms, got %+v", got)
	}
	want := map[string]int{"a new setting": 1, "a new component": 2, "a removal": 3}
	for _, m := range got {
		n, ok := want[m.Name]
		if !ok || len(m.Criteria) != 1 || m.Criteria[0].Number != n {
			t.Errorf("unexpected mechanism %+v", m)
		}
	}
}

func TestOneMechanismTwiceIsOneMechanism(t *testing.T) {
	got := domain.AcceptanceMechanisms(oneMechanism)
	if len(got) != 1 || got[0].Name != "a new setting" || len(got[0].Criteria) != 2 {
		t.Fatalf("expected one mechanism over two criteria, got %+v", got)
	}
}

func TestSweepAndExpectLinesAreNotCriteria(t *testing.T) {
	got := domain.AcceptanceMechanisms("1. The gate is green.\nSweep: the old command is removed here.\nExpect: refusals down")
	if len(got) != 0 {
		t.Fatalf("expected no mechanism from a Sweep or Expect line, got %+v", got)
	}
}

func TestMechanismWarningNamesTheCriteria(t *testing.T) {
	line := domain.MechanismWarning("S1", domain.SizeL, manyMechanisms)
	for _, want := range []string{"S1", "size L", "3 mechanisms", "a new setting (1)", "a new component (2)", "a removal (3)"} {
		if !strings.Contains(line, want) {
			t.Errorf("expected the warning to say %q, got %q", want, line)
		}
	}
	if domain.MechanismWarning("S1", domain.SizeL, oneMechanism) != "" {
		t.Error("expected no warning for a story of one mechanism")
	}
	if domain.MechanismWarning("S1", domain.SizeM, manyMechanisms) != "" {
		t.Error("expected no warning for an M story: only L and XL are judged")
	}
}

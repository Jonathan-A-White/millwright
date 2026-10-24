package application

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// diffStub is a BranchChanges whose diff of any file name is the lines it holds.
type diffStub struct {
	branchChangesStub
	lines []DiffLine
	err   error
	asked string
}

func (d *diffStub) DiffLines(_ context.Context, _, _, _, name string) ([]DiffLine, error) {
	d.asked = name
	return d.lines, d.err
}

func lockfileRefusal(t *testing.T, changes BranchChanges) (Refusal, bool, *NextReport) {
	t.Helper()
	report := &NextReport{}
	c := &closeOut{id: "mw-t.1", rigDir: "/rig", branch: "mw/mw-t.1", target: "main"}
	refusal, refused := Next{Changes: changes}.lockfileSSHRefusal(context.Background(), c, report)
	return refusal, refused, report
}

func TestABranchThatAddsAGitSSHLineToALockfileIsRefusedWithTheFileTheCountAndTheFix(t *testing.T) {
	stub := &diffStub{lines: []DiffLine{
		{Path: "package-lock.json", Text: `      "resolved": "git+ssh://git@github.com/o/a.git#abc",`, Added: true},
		{Path: "package-lock.json", Text: `      "resolved": "git+https://github.com/o/b.git#def",`, Added: true},
		{Path: "web/package-lock.json", Text: `      "resolved": "git+ssh://git@github.com/o/c.git#123",`, Added: true},
		{Path: "web/package-lock.json", Text: `      "resolved": "git+ssh://git@github.com/o/d.git#456",`, Added: true},
	}}
	refusal, refused, _ := lockfileRefusal(t, stub)
	if !refused || refusal.Reason != ReasonLockfileSSH {
		t.Fatalf("expected a refusal for %s, got %+v, %v", ReasonLockfileSSH, refusal, refused)
	}
	if stub.asked != "package-lock.json" {
		t.Errorf("expected the diff of package-lock.json to be read, got %q", stub.asked)
	}
	for _, want := range []string{
		"package-lock.json has 1 added line",
		"web/package-lock.json has 2 added line",
		`"resolved": "git+ssh://git@github.com/o/a.git#abc",`,
		`"resolved": "git+ssh://git@github.com/o/d.git#456",`,
		"sed -i 's#git+ssh://git@github.com/#git+https://github.com/#g' package-lock.json",
		"sed -i 's#git+ssh://git@github.com/#git+https://github.com/#g' web/package-lock.json",
	} {
		if !strings.Contains(refusal.Said, want) {
			t.Errorf("expected the refusal to say %q, got:\n%s", want, refusal.Said)
		}
	}
	if strings.Contains(refusal.Said, "git+https://github.com/o/b.git#def") {
		t.Errorf("expected the refusal to quote only the git+ssh lines, got:\n%s", refusal.Said)
	}
	if !strings.Contains(refusal.Why, "git+ssh://") || !strings.Contains(refusal.Why, "package-lock.json") {
		t.Errorf("expected the why to name git+ssh:// and the file, got %q", refusal.Why)
	}
}

func TestABranchThatAddsNoGitSSHLineIsNotRefused(t *testing.T) {
	ssh := `      "resolved": "git+ssh://git@github.com/o/a.git#abc",`
	for name, lines := range map[string][]DiffLine{
		"nothing changed in a lockfile": nil,
		"https lines added":             {{Path: "package-lock.json", Text: `"resolved": "git+https://github.com/o/a.git#abc",`, Added: true}},
		"ssh lines turned into https": {
			{Path: "package-lock.json", Text: ssh},
			{Path: "package-lock.json", Text: `"resolved": "git+https://github.com/o/a.git#abc",`, Added: true},
		},
		"ssh lines only removed": {{Path: "package-lock.json", Text: ssh}},
		"an ssh line that moved within the file": {
			{Path: "package-lock.json", Text: ssh},
			{Path: "package-lock.json", Text: ssh, Added: true},
		},
	} {
		if refusal, refused, _ := lockfileRefusal(t, &diffStub{lines: lines}); refused {
			t.Errorf("%s: expected no refusal, got %+v", name, refusal)
		}
	}
}

func TestARigWithNoLockfileOrNoChangesPortIsNotRefused(t *testing.T) {
	if _, refused, _ := lockfileRefusal(t, nil); refused {
		t.Error("expected a rig with no Changes not to be refused")
	}
	if _, refused, _ := lockfileRefusal(t, &diffStub{}); refused {
		t.Error("expected a rig with no package-lock.json not to be refused")
	}
}

func TestABranchWhoseDiffCannotBeReadIsNotRefusedButSaysSo(t *testing.T) {
	refusal, refused, report := lockfileRefusal(t, &diffStub{err: errors.New("git is gone")})
	if refused {
		t.Fatalf("expected no refusal, got %+v", refusal)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "git is gone") {
		t.Errorf("expected one note naming the failure, got %q", report.Notes)
	}
}

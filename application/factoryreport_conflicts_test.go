package application

import (
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
)

type rulesOf map[string]domain.EpicRequirements

func (r rulesOf) EpicRequirements(_ context.Context, rig string) (domain.EpicRequirements, error) {
	return r[rig], nil
}

func conflictRefusals(rig string, files ...[]string) []reportEntry {
	var entries []reportEntry
	for i, f := range files {
		entries = append(entries, reportEntry{id: string(rune('a' + i)), rig: rig, refused: true, reason: "merge-conflict", conflictFiles: f})
	}
	return entries
}

func TestTheMergeConflictWinNamesTheTopThreeConflictedFilesWithCounts(t *testing.T) {
	entries := append(conflictRefusals("legend",
		[]string{"CHANGELOG.md", "package.json"}, []string{"CHANGELOG.md", "package.json"}, []string{"CHANGELOG.md", "src/a.go"},
		[]string{"CHANGELOG.md", "docs/module-map.md"}, []string{"src/a.go"}),
		reportEntry{id: "z", rig: "legend", landed: true})
	blocks := FactoryReport{}.blocks(entries, nil, func(e reportEntry) string { return e.rig }, nil)
	got := winDominantReason(blocks)
	want := "merge-conflict is 100% of legend refusals: CHANGELOG.md 4, package.json 2, src/a.go 2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheMergeConflictWinReadsAsBeforeWhenNoRefusalCarriesFiles(t *testing.T) {
	entries := conflictRefusals("legend", nil, nil, nil)
	blocks := FactoryReport{}.blocks(entries, nil, func(e reportEntry) string { return e.rig }, nil)
	if got, want := winDominantReason(blocks), "merge-conflict is 100% of legend refusals: look for what they share"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAConflictInAFileTheRigMergesByRuleIsMarkedAsMachineryThatFailed(t *testing.T) {
	entries := conflictRefusals("legend",
		[]string{"CHANGELOG.md", "package.json", "docs/a.md", "src/x.go"}, []string{"CHANGELOG.md", "package.json", "docs/a.md", "src/x.go"},
		[]string{"CHANGELOG.md", "package.json", "docs/a.md"}, []string{"CHANGELOG.md", "package.json"})
	r := FactoryReport{Rules: rulesOf{"legend": {
		UnionMergeFiles: []string{"docs/*.md"}, VersionFiles: []string{"package.json"}, ChangelogFiles: []string{"CHANGELOG.md"},
	}}}
	blocks := r.blocks(entries, nil, func(e reportEntry) string { return e.rig }, nil)
	r.markConflictFiles(context.Background(), blocks)
	got := winDominantReason(blocks)
	want := "merge-conflict is 100% of legend refusals: CHANGELOG.md (changelog) 4, package.json (version) 4, docs/a.md (union) 3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	var b strings.Builder
	writeCheapWins(&b, CheapWins(WinInputs{Rigs: blocks}))
	for _, line := range strings.Split(b.String(), "\n") {
		if len([]rune(line)) > ReportWidth {
			t.Errorf("a line is %d columns: %q", len([]rune(line)), line)
		}
	}
	if !strings.Contains(b.String(), "(changelog)") {
		t.Errorf("expected the marked file in the printed wins:\n%s", b.String())
	}
}

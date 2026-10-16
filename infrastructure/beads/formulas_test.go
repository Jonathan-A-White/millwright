package beads_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// OpenFormulas is one bd list of what is not closed: it groups the formula beads
// by root, a step whose root is closed already among them, reads the story off
// the first step's title, and names the live story that records a root.
func TestOpenFormulasGroupsTheOpenFormulaBeadsInOneList(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{
		"list": `[{"id": "t-1", "title": "A running story", "status": "in_progress", "issue_type": "task", "metadata": {"molecule": "t-mol-a"}},
		          {"id": "t-3", "title": "A story", "status": "open", "issue_type": "task"},
		          {"id": "t-mol-a", "title": "A running story", "status": "open", "issue_type": "molecule"},
		          {"id": "t-mol-a1", "title": "Implement until green", "status": "open", "issue_type": "task", "parent": "t-mol-a"},
		          {"id": "t-mol-b3", "title": "Commit", "status": "open", "issue_type": "task", "parent": "t-mol-b",
		           "dependencies": [{"issue_id": "t-mol-b3", "depends_on_id": "t-mol-b2", "type": "blocks"}]},
		          {"id": "t-mol-b2", "title": "Understand t-2: A closed story", "status": "open", "issue_type": "task", "parent": "t-mol-b"}]`,
	})

	got, err := gateway.OpenFormulas(context.Background())
	if err != nil {
		t.Fatalf("reading the open formulas: %v", err)
	}
	want := []application.PouredFormula{
		{Root: "t-mol-a", RootOpen: true, Steps: []string{"t-mol-a1"}, LiveStory: "t-1"},
		{Root: "t-mol-b", Steps: []string{"t-mol-b2", "t-mol-b3"}, Story: "t-2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if calls := strings.Split(strings.TrimSpace(string(asked)), "\n"); len(calls) != 1 || !strings.Contains(calls[0], "--exclude-type mail,event") {
		t.Errorf("expected one list of everything but mail and events, got %q", calls)
	}
}

// CloseFormulaBead closes a formula's bead and refuses, without asking bd, an
// id that is not one.
func TestCloseFormulaBeadClosesOnlyAFormulasBead(t *testing.T) {
	gateway, log := leaseStandIn(t, map[string]string{"close": ""})
	ctx := context.Background()

	if err := gateway.CloseFormulaBead(ctx, "t-3", "why"); err == nil {
		t.Fatal("expected a story's id to be refused")
	}
	if err := gateway.CloseFormulaBead(ctx, "t-mol-b2", "Tidied by mw tidy: its story t-2 is closed"); err != nil {
		t.Fatalf("closing a step: %v", err)
	}
	asked, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if calls := strings.TrimSpace(string(asked)); strings.Contains(calls, "t-3") || !strings.Contains(calls, "close t-mol-b2 --reason Tidied by mw tidy") {
		t.Errorf("expected only the step closed, got %q", calls)
	}
}

package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func TestTheImprovementsLedgerReadsItsIdeasAndSaysWhenThereIsNone(t *testing.T) {
	v := vault.New(t.TempDir())
	ctx := context.Background()
	if ideas, exists, err := v.Improvements(ctx); err != nil || exists || len(ideas) != 0 {
		t.Fatalf("no ledger: got %v, %v, %v", ideas, exists, err)
	}
	text := `{"title":"Cache the gate","expect":"Expect: dispatch-to-landing down"}` + "\n" +
		"not json\n" + `{"expect":"no title"}` + "\n" +
		`{"title":"Filed","bead":"mw-x.1"}` + "\n"
	if err := os.MkdirAll(filepath.Dir(v.ImprovementsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v.ImprovementsPath(), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	ideas, exists, err := v.Improvements(ctx)
	if err != nil || !exists || len(ideas) != 2 || ideas[0].Title != "Cache the gate" || ideas[1].Bead != "mw-x.1" {
		t.Errorf("got %+v, %v, %v", ideas, exists, err)
	}
}

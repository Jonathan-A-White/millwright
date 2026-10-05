package cardlog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/cardlog"
)

func TestTheListIsOneJSONLineACardAndReadsBackOldestFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "mw")
	log := cardlog.New(dir)
	ctx := context.Background()
	if records, err := log.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("expected nothing before anything is written, got %+v (%v)", records, err)
	}
	first := domain.CardRecord{Txid: "direct:a", Title: "Top 5", At: "2026-10-05T18:00:00Z",
		Items: []domain.CardRecordItem{{N: 1, Text: "x", Expect: &domain.Expectation{Bead: "mw-a", State: "closed"}}}}
	second := domain.CardRecord{Txid: "direct:b", Re: "direct:a", At: "2026-10-05T18:01:00Z", Items: []domain.CardRecordItem{}, Tick: []int{1}}
	for _, r := range []domain.CardRecord{first, second} {
		if err := log.Append(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cards.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"txid":"direct:a","title":"Top 5","at":"2026-10-05T18:00:00Z","items":[{"n":1,"text":"x","expect":{"bead":"mw-a","state":"closed"}}]}` + "\n" +
		`{"txid":"direct:b","re":"direct:a","at":"2026-10-05T18:01:00Z","items":[],"tick":[1]}` + "\n"
	if string(raw) != want {
		t.Fatalf("expected the file\n%s\ngot\n%s", want, raw)
	}
	records, err := log.List(ctx)
	if err != nil || len(records) != 2 || records[0].Txid != "direct:a" || records[1].Re != "direct:a" {
		t.Fatalf("expected both read back oldest first, got %+v (%v)", records, err)
	}
}

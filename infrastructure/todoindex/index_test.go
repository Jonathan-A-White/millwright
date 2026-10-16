package todoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

func docs() []application.TodoDoc {
	return []application.TodoDoc{
		{ID: "a", Status: "closed", Title: "Passport", Kind: "title", Body: "Passport"},
		{ID: "a", Status: "closed", Title: "Passport", Kind: "comment", Body: "Send to Café Müller on Harbor Lane,\nthen the post office"},
		{ID: "b", Status: "open", Title: "Hose", Kind: "description", Body: "harbor harbor harbor lane lane"},
	}
}

func TestSearchIsCaseAndAccentBlindAndMatchesEveryWord(t *testing.T) {
	ctx := context.Background()
	idx := Index{Path: filepath.Join(t.TempDir(), "sub", "i.db")}
	if err := idx.Rebuild(ctx, docs()); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Search(ctx, []string{"HARBOR", "lane"}, 10)
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits %v, err %v; want 2", hits, err)
	}
	hits, err = idx.Search(ctx, []string{"cafe", "muller"}, 10)
	if err != nil || len(hits) != 1 || hits[0].ID != "a" || hits[0].Kind != "comment" || hits[0].Status != "closed" || hits[0].Title != "Passport" {
		t.Fatalf("hits %+v, err %v", hits, err)
	}
	if hits, err = idx.Search(ctx, []string{"harbor"}, 1); err != nil || len(hits) != 1 {
		t.Fatalf("limit not kept: %v %v", hits, err)
	}
	if hits, err = idx.Search(ctx, []string{"!!"}, 10); err != nil || len(hits) != 0 {
		t.Fatalf("a word of punctuation found %v, err %v", hits, err)
	}
}

func TestRebuildReplacesWhatWasThereAndLeavesNoTempFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	idx := Index{Path: filepath.Join(dir, "i.db")}
	if err := idx.Rebuild(ctx, docs()); err != nil {
		t.Fatal(err)
	}
	if err := idx.Rebuild(ctx, docs()[:1]); err != nil {
		t.Fatal(err)
	}
	if hits, err := idx.Search(ctx, []string{"harbor"}, 10); err != nil || len(hits) != 0 {
		t.Fatalf("an old row survived: %v %v", hits, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("the directory holds %v", entries)
	}
}

func TestSearchOfAMissingFileSaysSo(t *testing.T) {
	if _, err := (Index{Path: filepath.Join(t.TempDir(), "none.db")}).Search(context.Background(), []string{"a"}, 1); err == nil {
		t.Fatal("no error")
	}
}

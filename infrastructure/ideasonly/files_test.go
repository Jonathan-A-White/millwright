package ideasonly_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/ideasonly"
)

func TestNoFilesIsTheZeroStateAndNothingHeld(t *testing.T) {
	files := ideasonly.New(filepath.Join(t.TempDir(), "ideas-only"))

	state, err := files.State(context.Background())
	if err != nil || state != (application.IdeasOnlyState{}) {
		t.Fatalf("expected the zero state, got %+v, %v", state, err)
	}
	held, err := files.Held(context.Background())
	if err != nil || len(held) != 0 {
		t.Fatalf("expected nothing held, got %+v, %v", held, err)
	}
}

func TestTheStateIsWrittenPrivatelyAndReadBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ideas-only")
	files := ideasonly.New(dir)
	want := application.IdeasOnlyState{On: true, Until: 1790003600, Since: 1790000000, ReleasedUntil: 5, Tz: "Europe/Paris"}

	if err := files.SetState(context.Background(), want); err != nil {
		t.Fatalf("writing the state: %v", err)
	}
	got, err := files.State(context.Background())
	if err != nil || got != want {
		t.Fatalf("expected %+v, got %+v, %v", want, got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "state.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected state.json at 0600, got %v, %v", info, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) != 0 {
		t.Fatalf("expected no temp file left, got %v", left)
	}
}

func TestAHeldTxidIsKeptOnce(t *testing.T) {
	files := ideasonly.New(filepath.Join(t.TempDir(), "ideas-only"))
	first := application.IdeasOnlyHeld{Txid: "direct:1", Seq: 1, Class: "message", Channel: "general", Text: "one", When: "2026-10-12T12:00:00Z"}
	second := application.IdeasOnlyHeld{Txid: "direct:2", Seq: 2, Class: "talk", Channel: "talk", Text: "two", Attachment: "/tmp/x.png", When: "2026-10-12T12:01:00Z"}

	for _, idea := range []application.IdeasOnlyHeld{first, second, first} {
		if err := files.Hold(context.Background(), idea); err != nil {
			t.Fatalf("holding %s: %v", idea.Txid, err)
		}
	}
	held, err := files.Held(context.Background())
	if err != nil || !reflect.DeepEqual(held, []application.IdeasOnlyHeld{first, second}) {
		t.Fatalf("expected the two ideas once, in order, got %+v, %v", held, err)
	}
}

func TestAStateFileThatIsNotJSONIsAnErrorNamingIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ideasonly.New(dir).State(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

package reportlog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/reportlog"
)

func TestTheListIsOneJSONLineASendAndReadsBackOldestFirst(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "mw", "report")
	log := reportlog.New(dir)
	ctx := context.Background()
	if sent, err := log.Sent(ctx); err != nil || len(sent) != 0 {
		t.Fatalf("expected nothing before anything is written, got %+v (%v)", sent, err)
	}
	first := application.ReportSent{Day: "2026-10-10", At: time.Date(2026, 10, 10, 12, 0, 3, 0, time.UTC), Txid: "direct:a"}
	second := application.ReportSent{Day: "2026-10-11", At: time.Date(2026, 10, 11, 12, 0, 4, 0, time.UTC), Txid: "direct:b"}
	for _, s := range []application.ReportSent{first, second} {
		if err := log.Record(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "sent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"day":"2026-10-10","at":"2026-10-10T12:00:03Z","txid":"direct:a"}` + "\n" +
		`{"day":"2026-10-11","at":"2026-10-11T12:00:04Z","txid":"direct:b"}` + "\n"
	if string(raw) != want {
		t.Fatalf("expected the file\n%s\ngot\n%s", want, raw)
	}
	sent, err := log.Sent(ctx)
	if err != nil || len(sent) != 2 || sent[0].Txid != "direct:a" || !sent[1].At.Equal(second.At) {
		t.Fatalf("expected both read back oldest first, got %+v (%v)", sent, err)
	}
}

func TestALineThatIsNotASendIsSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sent.jsonl"), []byte("garbage\n{\"day\":\"2026-10-11\",\"at\":\"2026-10-11T12:00:00Z\",\"txid\":\"x\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sent, err := reportlog.New(dir).Sent(context.Background())
	if err != nil || len(sent) != 1 || sent[0].Day != "2026-10-11" {
		t.Fatalf("expected the one good line, got %+v (%v)", sent, err)
	}
}

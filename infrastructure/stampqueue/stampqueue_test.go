package stampqueue_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/stampqueue"
)

func stamp(commit string) domain.Stamp {
	return domain.Stamp{
		Rig: "millwright", Branch: "main", Commit: commit, Story: "mw-x.1", Title: "t", Host: "laptop",
		At: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC),
	}
}

func TestAppendedStampsArePendingOldestFirstInTheFileForThisHost(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stamps")
	q := stampqueue.New(dir)
	ctx := context.Background()
	for _, c := range []string{"aaaa", "bbbb"} {
		if err := q.Append(ctx, stamp(c)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := q.Pending(ctx)
	if err != nil || len(got) != 2 || got[0].Stamp != stamp("aaaa") || got[1].Stamp != stamp("bbbb") {
		t.Fatalf("pending = %+v, %v", got, err)
	}
	held, err := os.ReadFile(filepath.Join(dir, "pending.jsonl"))
	if err != nil || strings.Count(string(held), "\n") != 2 {
		t.Fatalf("pending.jsonl = %q, %v", held, err)
	}
}

func TestPendingOfAQueueThatWasNeverWrittenIsEmpty(t *testing.T) {
	got, err := stampqueue.New(filepath.Join(t.TempDir(), "none")).Pending(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("pending = %+v, %v", got, err)
	}
}

func TestMarkSentMovesAStampToSentWithItsTxidAndCountsTheTries(t *testing.T) {
	dir := t.TempDir()
	q := stampqueue.New(dir)
	ctx := context.Background()
	q.Append(ctx, stamp("aaaa"))
	q.Append(ctx, stamp("bbbb"))
	if err := q.MarkFailed(ctx, stamp("aaaa"), "backend down"); err != nil {
		t.Fatal(err)
	}
	if got, _ := q.Pending(ctx); got[0].Attempts != 1 || got[0].LastError != "backend down" {
		t.Fatalf("after a failure pending = %+v", got)
	}
	if err := q.MarkSent(ctx, stamp("aaaa"), "txid-1", time.Date(2026, 10, 4, 9, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Pending(ctx)
	if len(got) != 1 || got[0].Stamp != stamp("bbbb") {
		t.Fatalf("pending = %+v, want only bbbb", got)
	}
	sent, err := os.ReadFile(filepath.Join(dir, "sent.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"txid":"txid-1"`, `"commit":"aaaa"`, `"attempts":2`} {
		if !strings.Contains(string(sent), want) {
			t.Errorf("sent.jsonl %q lacks %s", sent, want)
		}
	}
}

func TestAStampSentButNotTakenOffTheQueueIsNotPendingAgain(t *testing.T) {
	dir := t.TempDir()
	q := stampqueue.New(dir)
	ctx := context.Background()
	q.Append(ctx, stamp("aaaa"))
	held, _ := os.ReadFile(filepath.Join(dir, "pending.jsonl"))
	q.MarkSent(ctx, stamp("aaaa"), "txid-1", time.Now())
	// the crash between the two files: pending.jsonl as it was before.
	if err := os.WriteFile(filepath.Join(dir, "pending.jsonl"), held, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := q.Pending(ctx); len(got) != 0 {
		t.Fatalf("pending = %+v: a sent stamp would be broadcast twice", got)
	}
}

func TestAnAppendBetweenTwoRewritesIsNotLost(t *testing.T) {
	q := stampqueue.New(t.TempDir())
	ctx := context.Background()
	q.Append(ctx, stamp("aaaa"))
	q.MarkFailed(ctx, stamp("aaaa"), "x")
	q.Append(ctx, stamp("bbbb"))
	q.MarkSent(ctx, stamp("aaaa"), "t", time.Now())
	if got, _ := q.Pending(ctx); len(got) != 1 || got[0].Stamp != stamp("bbbb") {
		t.Fatalf("pending = %+v", got)
	}
}

func TestFindSentReadsSentStampsOfARigByCommitPrefix(t *testing.T) {
	q := stampqueue.New(t.TempDir())
	ctx := context.Background()
	a := domain.Stamp{Rig: "millwright", Commit: "7b4430c1f2", Story: "mw-a.1"}
	b := domain.Stamp{Rig: "millwright", Commit: "9999999999", Story: "mw-b.1"}
	pending := domain.Stamp{Rig: "millwright", Commit: "7b44ffffff", Story: "mw-c.1"}
	for _, s := range []domain.Stamp{a, b, pending} {
		if err := q.Append(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []domain.Stamp{a, b} {
		if err := q.MarkSent(ctx, s, "txid-"+s.Story, time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := q.FindSent(ctx, "millwright", "7B4430C")
	if err != nil || len(got) != 1 || got[0].Stamp != a || got[0].Txid != "txid-mw-a.1" {
		t.Fatalf("FindSent = %+v, %v", got, err)
	}
	if got, _ := q.FindSent(ctx, "millwright", "7b44"); len(got) != 1 {
		t.Fatalf("a pending stamp was found: %+v", got)
	}
	if got, _ := q.FindSent(ctx, "other", "7b44"); len(got) != 0 {
		t.Fatalf("another rig's lookup found %+v", got)
	}
	if got, _ := q.FindSent(ctx, "millwright", ""); len(got) != 0 {
		t.Fatalf("an empty prefix found %+v", got)
	}
}

func TestFindSentOnAQueueThatSentNothingFindsNothing(t *testing.T) {
	got, err := stampqueue.New(t.TempDir()).FindSent(context.Background(), "millwright", "7b44")
	if err != nil || len(got) != 0 {
		t.Fatalf("FindSent = %+v, %v", got, err)
	}
}

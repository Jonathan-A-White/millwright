package vault_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/vault"
)

func put(t *testing.T, dir, rel, text string) string {
	t.Helper()
	at := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(at, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestThePrinciplesAreThePlansFileElseTheNewestTriageOnes(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	ctx := context.Background()
	if _, err := v.Principles(ctx); err == nil {
		t.Fatal("principles from an empty vault")
	}
	put(t, dir, "plans/triage-2026-10-01/principles.md", "old")
	put(t, dir, "plans/triage-2026-10-15/principles.md", "newer")
	if got, err := v.Principles(ctx); err != nil || got != "newer" {
		t.Fatalf("principles = %q (%v), want the newest triage ones", got, err)
	}
	put(t, dir, "plans/principles.md", "own")
	if got, err := v.Principles(ctx); err != nil || got != "own" {
		t.Fatalf("principles = %q (%v), want plans/principles.md", got, err)
	}
}

func TestAPlanFileIsWrittenUnderPlansAndCannotClimbOut(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	rel, err := v.PutPlanFile(context.Background(), "sleep-2026-10-16", "maps.md", "m\n")
	if err != nil || rel != "plans/sleep-2026-10-16/maps.md" {
		t.Fatalf("rel = %q (%v)", rel, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, rel)); string(got) != "m\n" {
		t.Errorf("file = %q", got)
	}
	for _, bad := range [][2]string{{"..", "x"}, {"sleep-1", "../x"}, {"a/b", "x"}} {
		if _, err := v.PutPlanFile(context.Background(), bad[0], bad[1], ""); err == nil {
			t.Errorf("PutPlanFile(%q, %q) was allowed", bad[0], bad[1])
		}
	}
}

func TestTheEarlierDumpIsTheNewestBeforeTheDate(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	ctx := context.Background()
	if _, _, ok, err := v.EarlierDump(ctx, "2026-10-16"); ok || err != nil {
		t.Fatalf("an empty vault has an earlier dump: %v %v", ok, err)
	}
	put(t, dir, "plans/triage-2026-10-15/compact.jsonl", "the Mayor's")
	name, text, ok, err := v.EarlierDump(ctx, "2026-10-16")
	if err != nil || !ok || text != "the Mayor's" || name != "plans/triage-2026-10-15/compact.jsonl" {
		t.Fatalf("got %q %q %v %v", name, text, ok, err)
	}
	put(t, dir, "plans/sleep-2026-10-15/triage.jsonl", "a pass's")
	put(t, dir, "plans/sleep-2026-10-16/triage.jsonl", "today's")
	if _, text, _, _ := v.EarlierDump(ctx, "2026-10-16"); text != "a pass's" {
		t.Errorf("earlier dump = %q, want the pass of the day before", text)
	}
}

func TestSessionsAreTheResultsWrittenSinceAndOldestFirst(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	now := time.Now()
	result := `{"type":"result","subtype":"success","is_error":false,"num_turns":4,"result":"done","usage":{"input_tokens":1,"output_tokens":2}}`
	for story, age := range map[string]time.Duration{"mw-a.1": 2 * time.Hour, "mw-a.2": time.Hour, "mw-old": 72 * time.Hour} {
		at := put(t, dir, "runs/"+story+"/result.json", result)
		if err := os.Chtimes(at, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	put(t, dir, "runs/mw-junk/result.json", "not json")
	got, err := v.Sessions(context.Background(), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var stories []string
	for _, s := range got {
		stories = append(stories, s.Story)
	}
	if strings.Join(stories, ",") != "mw-a.1,mw-a.2" {
		t.Fatalf("sessions = %v, want mw-a.1 then mw-a.2", stories)
	}
	if got[0].Result.Turns != 4 {
		t.Errorf("result = %+v", got[0].Result)
	}
}

func TestRunFilesListsAKindAndNothingForOneNotThere(t *testing.T) {
	dir := t.TempDir()
	v := vault.New(dir)
	put(t, dir, "runs/sleep/2026-10-16.json", "{}")
	put(t, dir, "runs/sleep/2026-10-15.json", "{}")
	got, err := v.RunFiles(context.Background(), "sleep")
	if err != nil || strings.Join(got, ",") != "2026-10-15.json,2026-10-16.json" {
		t.Fatalf("got %v (%v)", got, err)
	}
	if got, err := v.RunFiles(context.Background(), "nothing"); err != nil || len(got) != 0 {
		t.Fatalf("got %v (%v)", got, err)
	}
}

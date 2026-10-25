package application_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// answerWithIdeas is the fixture answer whose recommendations carry the ideas
// of samplePass, as the agent is asked to write them (grinds/sleep.md).
func answerWithIdeas(t *testing.T, recommendations string) json.RawMessage {
	t.Helper()
	var answer map[string]any
	if err := json.Unmarshal([]byte(fixtureAnswer), &answer); err != nil {
		t.Fatal(err)
	}
	answer["recommendations"] = recommendations
	body, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func ledgerOf(t *testing.T, f *sleepFixture) []application.SleepLedgerEntry {
	t.Helper()
	text, _ := f.vault.ReadImprovements(context.Background())
	entries, err := application.ParseSleepLedger(text)
	if err != nil {
		t.Fatalf("the ledger does not read back: %v\n%s", err, text)
	}
	return entries
}

func TestAPassAppendsOneLedgerLinePerIdeaAndLoopRankListsThem(t *testing.T) {
	f := newSleepFixture()
	f.grinder.Result.Answer = answerWithIdeas(t, samplePass)
	f.pass.Ledger = f.vault
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	entries := ledgerOf(t, f)
	if len(entries) != 2 || entries[0].Title != "Cache the gate" || entries[1].Title != "Guard the stale lock" {
		t.Fatalf("ledger = %+v, want the pass's two ideas", entries)
	}
	for _, e := range entries {
		if e.Filed != "2026-10-16" || e.Bead != "" || e.ID == "" {
			t.Errorf("entry %+v: want filed 2026-10-16, no bead, an id", e)
		}
	}
	if entries[0].ID == entries[1].ID {
		t.Errorf("the two ideas share the id %q", entries[0].ID)
	}
	ranked := rankOf(t, f.vault.files["improvements/ledger.jsonl"], 0, false)
	if order2(ranked) != "Guard the stale lock|Cache the gate" || strings.Contains(ranked, "unscored") {
		t.Errorf("mw loop rank on the ledger:\n%s", ranked)
	}
	committed := false
	for _, paths := range f.vault.commits {
		for _, p := range paths {
			committed = committed || p == "plans/improvements/ledger.jsonl"
		}
	}
	if !committed {
		t.Errorf("the ledger was not committed: %v", f.vault.commits)
	}
}

func TestAPassWithNoIdeasAppendsNothing(t *testing.T) {
	f := newSleepFixture()
	f.pass.Ledger = f.vault
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.vault.files["improvements/ledger.jsonl"]; ok {
		t.Errorf("a pass with no ideas wrote the ledger: %q", f.vault.files["improvements/ledger.jsonl"])
	}
	for _, paths := range f.vault.commits {
		for _, p := range paths {
			if strings.Contains(p, "improvements/ledger") {
				t.Errorf("committed %v", paths)
			}
		}
	}
}

func TestTheSamePassWrittenTwiceDoesNotAppendAnIdeaTwice(t *testing.T) {
	f := newSleepFixture()
	f.grinder.Result.Answer = answerWithIdeas(t, samplePass)
	f.pass.Ledger = f.vault
	for range 2 {
		if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if got := ledgerOf(t, f); len(got) != 2 {
		t.Errorf("ledger has %d lines after the same pass twice, want 2", len(got))
	}
	// a line already there, with its bead filed, is never edited
	text := f.vault.files["improvements/ledger.jsonl"]
	filed := strings.Replace(text, `"filed":"2026-10-16"`, `"filed":"2026-10-16","bead":"mw-x.1"`, 1)
	f.vault.files["improvements/ledger.jsonl"] = filed
	if _, err := f.pass.Run(context.Background(), application.SleepOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.vault.files["improvements/ledger.jsonl"]; got != filed {
		t.Errorf("a line was edited or added:\n%s", got)
	}
}

func TestABadIdeasListIsReportedAndTheRestOfThePassStillWrites(t *testing.T) {
	f := newSleepFixture()
	f.grinder.Result.Answer = answerWithIdeas(t, `{"ideas":"no"}`)
	f.pass.Ledger = f.vault
	_, err := f.pass.Run(context.Background(), application.SleepOptions{})
	if err == nil || !strings.Contains(err.Error(), "ideas") {
		t.Errorf("err = %v, want the ideas named", err)
	}
	if _, ok := f.vault.files["sleep-2026-10-16/improvements.md"]; !ok {
		t.Error("improvements.md was not written")
	}
}

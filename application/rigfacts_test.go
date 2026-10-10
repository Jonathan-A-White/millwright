package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const fullFact = `---
subject: bd
kind: gotcha
status: superseded
source: mw-gq6.43
since: 2026-09-01
supersedes: old-bd
superseded-by: new-bd
retired: 2026-10-01
reason: "a: reason, with a colon"
---

Run bd with -C.
`

func TestAFactFileRoundTripsEveryKey(t *testing.T) {
	fact, err := application.ParseRigFact("bd-dash-c", fullFact)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	want := application.RigFact{
		Slug: "bd-dash-c", Subject: "bd", Kind: application.FactGotcha, Status: application.FactSuperseded,
		Source: "mw-gq6.43", Since: "2026-09-01", Supersedes: "old-bd", SupersededBy: "new-bd",
		Retired: "2026-10-01", Reason: "a: reason, with a colon", Sentence: "Run bd with -C.",
	}
	if fact != want {
		t.Fatalf("got %+v, want %+v", fact, want)
	}
	again, err := application.ParseRigFact("bd-dash-c", fact.String())
	if err != nil {
		t.Fatalf("parsing what String wrote: %v\n%s", err, fact.String())
	}
	if again != fact {
		t.Fatalf("round trip changed the fact: %+v vs %+v", again, fact)
	}
}

func TestAFactFileIsRefusedWhenMalformed(t *testing.T) {
	good := func(edit func(string) string) string { return edit(fullFact) }
	cases := map[string]string{
		"no sentence":        good(func(s string) string { return strings.Replace(s, "Run bd with -C.\n", "", 1) }),
		"two sentence lines": good(func(s string) string { return s + "And another line.\n" }),
		"unknown kind":       good(func(s string) string { return strings.Replace(s, "kind: gotcha", "kind: rumour", 1) }),
		"unknown status":     good(func(s string) string { return strings.Replace(s, "status: superseded", "status: stale", 1) }),
		"unknown key":        good(func(s string) string { return strings.Replace(s, "subject: bd", "subject: bd\nmood: grim", 1) }),
		"no subject":         good(func(s string) string { return strings.Replace(s, "subject: bd\n", "", 1) }),
		"bad date":           good(func(s string) string { return strings.Replace(s, "since: 2026-09-01", "since: yesterday", 1) }),
		"no front matter":    "Run bd with -C.\n",
		"unclosed front":     "---\nsubject: bd\n",
	}
	for name, text := range cases {
		if _, err := application.ParseRigFact("x", text); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

func fact(slug, subject string, kind application.FactKind, status application.FactStatus, sentence, source string) application.RigFact {
	return application.RigFact{Slug: slug, Subject: subject, Kind: kind, Status: status, Source: source, Since: "2026-09-01", Sentence: sentence}
}

func TestTheRenderHoldsAboutDecisionsAndCurrentGotchasOnly(t *testing.T) {
	facts := []application.RigFact{
		fact("b", "git", application.FactGotcha, application.FactCurrent, "Rebase before landing.", "mw-1"),
		fact("a", "bd", application.FactGotcha, application.FactCurrent, "Use -C.", "mw-2"),
		fact("c", "layout", application.FactDecision, application.FactCurrent, "Facts live one per file.", "mw-3"),
		fact("d", "bd", application.FactGotcha, application.FactSuperseded, "OLD SUPERSEDED.", "mw-4"),
		fact("e", "bd", application.FactGotcha, application.FactRetired, "OLD RETIRED.", "mw-5"),
		fact("f", "bd", application.FactGotcha, application.FactRecheck, "OLD RECHECK.", "mw-6"),
	}
	got := application.RenderRigMemory("About text.\n", facts)
	want := "About text.\n\n## Decisions\n\n- [layout] Facts live one per file. (mw-3)\n\n## Gotchas\n\n- [bd] Use -C. (mw-2)\n- [git] Rebase before landing. (mw-1)"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTheRenderOfNoCurrentFactsIsTheAboutText(t *testing.T) {
	got := application.RenderRigMemory("About text.\n", []application.RigFact{
		fact("e", "bd", application.FactGotcha, application.FactRetired, "x", "y"),
	})
	if got != "About text." {
		t.Fatalf("got %q", got)
	}
}

func TestLoadingFactFilesSkipsAndNamesTheMalformed(t *testing.T) {
	facts, skipped := application.LoadRigFacts(map[string]string{
		"good.md": fullFact,
		"bad.md":  strings.Replace(fullFact, "status: superseded", "status: stale", 1),
	})
	if len(facts) != 1 || facts[0].Slug != "good" {
		t.Fatalf("facts: %+v", facts)
	}
	if len(skipped) != 1 || !strings.HasPrefix(skipped[0], "bad.md: ") || !strings.Contains(skipped[0], "stale") {
		t.Fatalf("skipped: %q", skipped)
	}
}

func TestBootPromptRendersAFactsRigAndNamesWhatItSkipped(t *testing.T) {
	seat := application.Seat{
		Name: "builder", Rig: "millwright", Charter: "the charter", Memory: "About text.\n", HasFacts: true,
		Facts:        []application.RigFact{fact("a", "bd", application.FactGotcha, application.FactCurrent, "Use -C.", "mw-2")},
		SkippedFacts: []string{"bad.md: unknown status \"stale\""},
	}
	prompt := application.BootPrompt(seat, aStory(nil))
	for _, want := range []string{"About text.", "- [bd] Use -C. (mw-2)", "bad.md: unknown status"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("boot prompt lacks %q:\n%s", want, prompt)
		}
	}
	for _, not := range []string{"subject:", "kind:", "---", "a.md"} {
		if strings.Contains(prompt, not) {
			t.Errorf("boot prompt holds %q", not)
		}
	}
}

func TestStatusNamesTheFixForAFactsRigOverBudget(t *testing.T) {
	report := application.StatusReport{
		RigMemoryBudget: 8000,
		RigMemory: []application.RigMemorySize{
			{Rig: "millwright", Bytes: 8412, Facts: true},
			{Rig: "fellowship", Bytes: 8100},
		},
	}
	printed := report.String()
	if !strings.Contains(printed, "  millwright 8412/8000 bytes: retire or supersede (Mayor)") {
		t.Errorf("missing the facts line:\n%s", printed)
	}
	if !strings.Contains(printed, "  fellowship 8100/8000 bytes: prune (Mayor)") {
		t.Errorf("missing the file line:\n%s", printed)
	}
}

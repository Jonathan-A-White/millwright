package application

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReportModelsCountByTheModelThatRanAndOnceAStory(t *testing.T) {
	entries := []reportEntry{
		// Pathed to opus now, but the ledger row says sonnet ran it.
		{id: "a", rig: "lampas", model: "opus", ran: "sonnet", refused: true, attempts: 2},
		{id: "a", rig: "lampas", model: "opus", ran: "opus", landed: true, attempts: 2, tokens: 3000, costUSD: 3},
		// A second landing row of the same story is not a second landing.
		{id: "a", rig: "lampas", model: "opus", ran: "opus", landed: true, attempts: 2},
		{id: "b", rig: "lampas", model: "opus", ran: "opus", landed: true, attempts: 1, bugStory: true, tokens: 1000, costUSD: 1},
		{id: "c", rig: "orrery", model: "sonnet", ran: "sonnet", landed: true, attempts: 1},
		// The window before, a bug filed and an escape are not MODELS'.
		{id: "d", rig: "lampas", ran: "opus", landed: true, previous: true},
		{id: "e", rig: "lampas", ran: "opus", bug: true},
		{id: "f", rig: "lampas", ran: "opus", escaped: true},
	}
	c := FactoryReport{}.modelCompare(entries, map[string]int{"b": 2})
	if len(c.Models) != 2 || c.Models[0].Model != "opus" || c.Models[1].Model != "sonnet" {
		t.Fatalf("the models shown are %+v, want opus and sonnet alone", c.Models)
	}
	opus, sonnet := c.Models[0], c.Models[1]
	if opus.Landed != 2 || opus.FirstTry != 1 || opus.Retried != 1 || opus.Refused != 0 || opus.FirstTryShare != 50 {
		t.Errorf("opus is %+v", opus)
	}
	if opus.Fuel.TokensPerLanding != 2000 || opus.Fuel.USDPerLanding != 2 || opus.EscapedBugs != 2 || opus.EscapedPerLanding != 1 {
		t.Errorf("opus's fuel and escapes are %+v", opus)
	}
	if sonnet.Landed != 1 || sonnet.Refused != 1 || sonnet.Sessions != 2 {
		t.Errorf("sonnet is %+v", sonnet)
	}
	var kinds []string
	for _, k := range c.Kinds {
		kinds = append(kinds, k.Kind+" "+k.Model)
	}
	if got := strings.Join(kinds, ", "); got != "lampas/bug opus, lampas/feature opus, lampas/feature sonnet, orrery/feature sonnet" {
		t.Errorf("the kinds are %s", got)
	}

	narrowed := FactoryReport{Rig: "orrery"}.modelCompare(entries, nil)
	if narrowed.Models[0].Sessions != 0 || narrowed.Models[1].Landed != 1 {
		t.Errorf("--rig orrery left %+v", narrowed.Models)
	}
}

func TestReportModelsShowHaikuAndFableOnceTheyHadASession(t *testing.T) {
	none := FactoryReport{}.modelCompare(nil, nil)
	if len(none.Models) != 2 || none.hadSessions() {
		t.Errorf("an empty window shows %+v", none.Models)
	}
	var b strings.Builder
	writeModels(&b, none)
	if b.Len() != 0 {
		t.Errorf("an empty window printed MODELS:\n%s", b.String())
	}

	c := FactoryReport{}.modelCompare([]reportEntry{{id: "x", rig: "lampas", ran: "fable", refused: true}}, nil)
	var names []string
	for _, m := range c.Models {
		names = append(names, m.Model)
	}
	if got := strings.Join(names, " "); got != "opus sonnet fable" {
		t.Errorf("the columns are %q", got)
	}
	b.Reset()
	writeModels(&b, c)
	c.Models = append(c.Models, ModelUse{Model: "haiku", Sessions: 1, Landed: 100, FirstTry: 100, FirstTryShare: 100})
	c.Kinds = append(c.Kinds, ModelUse{Model: "haiku", Kind: "a-rig-with-a-very-long-name/feature", Landed: 100, FirstTryShare: 100})
	writeModels(&b, c)
	summaryModels(&b, c)
	for _, line := range strings.Split(b.String(), "\n") {
		if utf8.RuneCountInString(line) > ReportWidth {
			t.Errorf("a MODELS line is %d columns: %q", utf8.RuneCountInString(line), line)
		}
	}
}

func TestReportShortCountFitsACell(t *testing.T) {
	for n, want := range map[float64]string{
		0: "0", 950: "950", 1500: "1.5k", 9_949: "9.9k", 820_000: "820k", 999_499: "999k",
		999_500: "1.0M", 4_140_000: "4.1M", 32_049_706: "32M",
	} {
		if got := shortCount(n); got != want {
			t.Errorf("shortCount(%v) is %q, want %q", n, got, want)
		}
	}
}

func TestReportModelsGapToQualityIsOneLineSayingWhatItIs(t *testing.T) {
	entries := []reportEntry{
		{id: "a", rig: "lampas", ran: "sonnet", landed: true},
		// A second landing row of a story, and a landing on a model with no column.
		{id: "a", rig: "lampas", ran: "sonnet", landed: true},
		{id: "b", rig: "lampas", ran: "unknown", landed: true},
		// A Tester's sessions: tested is a landing, not landed a refusal.
		{id: "t1", rig: "lampas", ran: "haiku", landed: true, tester: true, attempts: 1, tokens: 20000, costUSD: 2},
		{id: "t2", rig: "lampas", ran: "haiku", landed: true, tester: true, attempts: 1},
		{id: "t3", rig: "lampas", ran: "haiku", refused: true, tester: true},
	}
	c := FactoryReport{}.modelCompare(entries, nil)
	var haiku, columns int
	for _, m := range c.Models {
		columns += m.Landed
		if m.Model == "haiku" {
			haiku = m.Landed
			if m.Refused != 1 || m.Fuel.TokensPerLanding != 20000 {
				t.Errorf("haiku is %+v", m)
			}
		}
	}
	if haiku != 2 {
		t.Errorf("haiku landed %d, want its 2 tested rows", haiku)
	}
	// QUALITY would say 3 landed (a, a, b); the columns say 3 (a, t1, t2).
	if columns != 3 {
		t.Errorf("the columns land %d", columns)
	}
	// The long words do not fit in 60 columns with all three, so the short ones.
	want := "landed vs QUALITY's: +2 Tester, -1 repeat, -1 other"
	if got := c.gapLine(); got != want {
		t.Errorf("the gap line is %q, want %q", got, want)
	}
	if got := c.gapLine(); utf8.RuneCountInString(got) > ReportWidth {
		t.Errorf("the gap line is %d columns: %q", utf8.RuneCountInString(got), got)
	}
	var b strings.Builder
	writeModels(&b, c)
	if strings.Count(b.String(), "QUALITY's") != 1 || !strings.Contains(b.String(), "lampas/test") {
		t.Errorf("MODELS prints:\n%s", b.String())
	}
	if (FactoryReport{}).modelCompare([]reportEntry{{id: "a", ran: "opus", landed: true}}, nil).gapLine() != "" {
		t.Error("a gap is said where the totals agree")
	}
}

func TestReportModelsGapLineUsesLongWordsWhenTheyFit(t *testing.T) {
	c := &ModelCompare{Testers: 18, Repeats: 5}
	if got, want := c.gapLine(), "landed vs QUALITY's: +18 Tester sessions, -5 repeat landings"; got != want {
		t.Errorf("the gap line is %q, want %q", got, want)
	}
}

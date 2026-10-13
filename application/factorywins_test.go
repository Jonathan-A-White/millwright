package application

import (
	"strings"
	"testing"
)

func rigBlock(name string, landed, refused, unset int, reasons ...ReasonCount) ReportBlock {
	q := Quality{Landed: landed, Refused: refused, Reasons: reasons, Causes: map[string]int{CauseUnset: unset}}
	if rows := landed + refused; rows > 0 {
		q.RefusalRate = float64(refused) * 100 / float64(rows)
	}
	return ReportBlock{Name: name, Quality: q}
}

func TestADominantReasonNeedsAThirdOfTheRefusalsAndThreeOfThem(t *testing.T) {
	for _, c := range []struct {
		name  string
		block ReportBlock
		want  string
	}{
		{"a share of 30% with 3 refusals", rigBlock("lampas", 5, 10, 0, ReasonCount{"x", 4}, ReasonCount{"tests-fail", 3}, ReasonCount{"merge-conflict", 3}), "x is 40% of lampas refusals: look for what they share"},
		{"under 30%", rigBlock("lampas", 5, 11, 0, ReasonCount{"a", 3}, ReasonCount{"b", 3}, ReasonCount{"c", 3}, ReasonCount{"d", 2}), ""},
		{"only 2 refusals", rigBlock("lampas", 5, 2, 0, ReasonCount{"tests-fail", 2}), ""},
		{"tests-fail is told to gate first", rigBlock("lampas", 5, 3, 0, ReasonCount{"tests-fail", 3}), "tests-fail is 100% of lampas refusals: gate check first"},
	} {
		wins := CheapWins(WinInputs{Rigs: []ReportBlock{c.block}})
		got := ""
		for _, w := range wins {
			if w.Rule == WinDominantReason {
				got = w.Text
			}
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestARefusalRateUpFivePointsIsAWinButFourIsNot(t *testing.T) {
	for rise, want := range map[float64]bool{4.9: false, 5: true, 12: true} {
		block := rigBlock("lampas", 10, 0, 0)
		block.Quality.RefusalRate = 10 + rise
		block.Previous = rigBlock("lampas", 9, 1, 0).Quality
		block.Previous.RefusalRate = 10
		got := winRefusalRate([]ReportBlock{block}) != ""
		if got != want {
			t.Errorf("a rise of %.1f points: held %v, want %v", rise, got, want)
		}
	}
	// A rig with nothing in the window before has no rate to have risen from.
	if got := winRefusalRate([]ReportBlock{rigBlock("lampas", 5, 5, 0)}); got != "" {
		t.Errorf("a rig with no window before made the win %q", got)
	}
}

func TestAGateIsSlowAtOneAndAHalfTimesItsUsual(t *testing.T) {
	for latest, want := range map[float64]bool{299: false, 300: false, 449: false, 450: true, 600: true} {
		held := winGateSlow([]GateTrend{{Rig: "lampas", Host: "laptop", UsualS: 300, LatestS: latest}}) != ""
		if held != want {
			t.Errorf("latest %.0fs against 300s: held %v, want %v", latest, held, want)
		}
	}
	if winGateSlow([]GateTrend{{Rig: "lampas", Host: "laptop", UsualS: 0, LatestS: 90}}) != "" {
		t.Error("a gate with no usual made a win")
	}
}

func TestAParFlagNamesTheKindWhosParIsFurthestOff(t *testing.T) {
	got := winParFlag([]KindCalibration{
		{Kind: "bug", Flag: "try a wider window", MedianErrorPercent: 40},
		{Kind: "feature", Flag: "try rig-only grouping", MedianErrorPercent: 70},
		{Kind: "rig", MedianErrorPercent: 90},
	})
	if want := "par for kind feature is off 70%: try rig-only grouping"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOnlyTheFirstThreeThatHoldAreShownAndEveryRuleKeepsItsId(t *testing.T) {
	in := WinInputs{
		Rigs:  []ReportBlock{rigBlock("lampas", 4, 3, 3, ReasonCount{"tests-fail", 3})},
		Gates: []GateTrend{{Rig: "lampas", Host: "laptop", UsualS: 300, LatestS: 600}},
		Heavy: []HeavyStory{{ID: "mw-a", Attempts: 3}, {ID: "mw-b", Attempts: 3}},
		Kinds: []KindCalibration{{Kind: "rig", Flag: "try a wider window", MedianErrorPercent: 60}},
	}
	in.Rigs[0].Previous = rigBlock("lampas", 10, 0, 0).Quality
	var rules []string
	var shown int
	for _, w := range CheapWins(in) {
		rules = append(rules, w.Rule)
		if w.Shown {
			shown++
		}
	}
	if got := strings.Join(rules, " "); got != "R1 R2 R3 R4 R5 R6" || shown != 3 {
		t.Errorf("rules %s, %d shown", got, shown)
	}
	var b strings.Builder
	writeCheapWins(&b, CheapWins(in))
	if lines := strings.Split(strings.TrimSpace(b.String()), "\n"); len(lines) != 4 || lines[3][:2] != "3." {
		t.Errorf("printed %q", b.String())
	}
}

func TestEveryCheapWinLineFitsTheReportWidth(t *testing.T) {
	long := strings.Repeat("rig", 30)
	in := WinInputs{
		Rigs:  []ReportBlock{rigBlock(long, 4, 3, 3, ReasonCount{"merge-conflict", 3})},
		Gates: []GateTrend{{Rig: long, Host: long, UsualS: 300, LatestS: 600}},
		Heavy: []HeavyStory{{ID: long, Attempts: 3}},
	}
	var b strings.Builder
	writeCheapWins(&b, CheapWins(in))
	for _, line := range strings.Split(b.String(), "\n") {
		if n := len([]rune(line)); n > ReportWidth {
			t.Errorf("a line is %d columns: %q", n, line)
		}
	}
}

func uncausedBlock(ids ...string) ReportBlock {
	block := rigBlock("lampas", 1, len(ids), len(ids))
	block.Quality.Uncaused = ids
	return block
}

func r1(wins []CheapWin) CheapWin {
	for _, w := range wins {
		if w.Rule == WinCausesUnset {
			return w
		}
	}
	return CheapWin{}
}

func TestRuleOneNamesTheUncausedStoriesFiveAndThenPlusMore(t *testing.T) {
	for _, c := range []struct {
		name string
		ids  []string
		want string
	}{
		{"one", []string{"mw-a.1"}, "1 refusal has no cause: mw cause mw-a.1"},
		{"two", []string{"mw-a.1", "mw-b.2"}, "2 refusals have no cause: mw cause mw-a.1 mw-b.2"},
		{"five", []string{"a", "b", "c", "d", "e"}, "5 refusals have no cause: mw cause a b c d e"},
		{"seven", []string{"a", "b", "c", "d", "e", "f", "g"}, "7 refusals have no cause: mw cause a b c d e +2 more"},
	} {
		w := r1(CheapWins(WinInputs{Rigs: []ReportBlock{uncausedBlock(c.ids...)}}))
		if w.Text != c.want {
			t.Errorf("%s: got %q, want %q", c.name, w.Text, c.want)
		}
		if strings.Join(w.IDs, " ") != strings.Join(c.ids, " ") {
			t.Errorf("%s: ids %v, want all of %v", c.name, w.IDs, c.ids)
		}
	}
}

func TestRuleOneJoinsTheRigsAndSortsTheIds(t *testing.T) {
	a, b := uncausedBlock("z-1"), uncausedBlock("a-1")
	b.Name = "other"
	w := r1(CheapWins(WinInputs{Rigs: []ReportBlock{a, b}}))
	if want := "2 refusals have no cause: mw cause a-1 z-1"; w.Text != want {
		t.Errorf("got %q, want %q", w.Text, want)
	}
}

func TestSumQualityNamesOnlyRefusedStoriesWithoutAValidCauseOnce(t *testing.T) {
	q := sumQuality([]reportEntry{
		{id: "b", refused: true, reason: "x"},
		{id: "a", refused: true, reason: "x", cause: "nonsense"},
		{id: "a", refused: true, reason: "x"},
		{id: "c", refused: true, reason: "x", cause: CauseClasses[0]},
		{id: "d", landed: true},
	})
	if got := strings.Join(q.Uncaused, " "); got != "a b" || q.Causes[CauseUnset] != 3 {
		t.Errorf("named %q, %d unset", got, q.Causes[CauseUnset])
	}
}

func TestAWrappedRuleOneFitsTheReportWidthAndKeepsItsPlusMore(t *testing.T) {
	ids := []string{"mw-gq6.401", "mw-gq6.402", "mw-gq6.403", "mw-gq6.404", "mw-gq6.405", "mw-gq6.406", "mw-gq6.407"}
	var b strings.Builder
	writeCheapWins(&b, CheapWins(WinInputs{Rigs: []ReportBlock{uncausedBlock(ids...)}}))
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if n := len([]rune(line)); n > ReportWidth {
			t.Errorf("a line is %d columns: %q", n, line)
		}
	}
	if !strings.Contains(b.String(), "\n   +2 more\n") && !strings.HasSuffix(strings.TrimSpace(b.String()), "+2 more") {
		t.Errorf("the +2 more was lost: %q", b.String())
	}
}

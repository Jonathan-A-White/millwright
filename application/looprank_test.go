package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	millwright "github.com/Jonathan-A-White/millwright"
	"github.com/Jonathan-A-White/millwright/application"
)

// idea is one ledger line of the fixture, every field given but those left out.
func idea(id, quality, hisTime, fuel, speed, compounds, size string) string {
	line := `{"id":"` + id + `","title":"title ` + id + `","expect":"Expect: refusals down"`
	for _, f := range [][2]string{{"quality", quality}, {"his_time", hisTime}, {"fuel", fuel}, {"speed", speed}} {
		if f[1] != "" {
			line += `,"` + f[0] + `":"` + f[1] + `"`
		}
	}
	if compounds != "" {
		line += `,"compounds":` + compounds
	}
	if size != "" {
		line += `,"size":"` + size + `"`
	}
	return line + "}\n"
}

func rankOf(t *testing.T, ledger string, top int, asJSON bool) string {
	t.Helper()
	var out bytes.Buffer
	err := application.LoopRank{Vault: &ledgerVault{ledger: ledger}, Top: top, JSON: asJSON, Out: &out}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// order is the titles of the printed lines, in order.
func order(text string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if i := strings.Index(line, "title "); i >= 0 {
			ids = append(ids, strings.Fields(line[i+len("title "):])[0])
		}
	}
	return ids
}

func TestTheRankOrdersByQualityThenHisTimeFuelSpeedThenCompoundsThenSize(t *testing.T) {
	// written in the reverse of the wanted order, except the last two, which are equal
	ledger := idea("f", "level", "up", "up", "up", "true", "XS") + // quality level
		idea("e", "up", "down", "up", "up", "true", "XS") + // his time down
		idea("d", "up", "up", "level", "up", "true", "XS") + // fuel level
		idea("c", "up", "up", "up", "down", "true", "XS") + // speed down
		idea("b", "up", "up", "up", "up", "false", "XS") + // does not compound
		idea("a2", "up", "up", "up", "up", "true", "L") + // large
		idea("a1", "up", "up", "up", "up", "true", "S") //  small
	// the six: a1 (S) before a2 (L) before b before c before d before e before f
	got := strings.Join(order(rankOf(t, ledger, 0, false)), " ")
	if want := "a1 a2 b c d e f"; got != want {
		t.Errorf("order %q, want %q", got, want)
	}
}

func TestTwoIdeasEqualOnEveryFieldKeepTheLedgersOrder(t *testing.T) {
	ledger := idea("x2", "up", "up", "up", "up", "true", "S") +
		idea("x1", "up", "up", "up", "up", "true", "S") +
		idea("x3", "up", "up", "up", "up", "true", "S")
	if got := strings.Join(order(rankOf(t, ledger, 0, false)), " "); got != "x2 x1 x3" {
		t.Errorf("equal ideas reordered: %q", got)
	}
}

func TestAnIdeaMissingAFieldIsListedLastUnscoredAndNamesTheField(t *testing.T) {
	ledger := idea("gap", "up", "up", "", "up", "true", "XS") +
		idea("nosize", "up", "up", "up", "up", "true", "") +
		idea("nocomp", "up", "up", "up", "up", "", "XS") +
		idea("odd", "up", "up", "up", "sideways", "true", "XS") +
		`{"id":"noexpect","title":"title noexpect","quality":"down","his_time":"down","fuel":"down","speed":"down","compounds":false,"size":"XL"}` + "\n" +
		`{"id":"badexpect","title":"title badexpect","expect":"Expect: wishes up","quality":"down","his_time":"down","fuel":"down","speed":"down","compounds":false,"size":"XL"}` + "\n" +
		idea("whole", "down", "down", "down", "down", "false", "XL")
	text := rankOf(t, ledger, 0, false)
	if got := strings.Join(order(text), " "); got != "whole gap nosize nocomp odd noexpect badexpect" {
		t.Errorf("the scored idea is first, the rest follow in the ledger's order: %q", got)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, field := range []string{"fuel", "size", "compounds", "speed", "expect", "expect"} {
		line := lines[i+1]
		if !strings.Contains(line, "unscored") || !strings.Contains(line, field) {
			t.Errorf("line %d should say unscored and name %q:\n%s", i+2, field, line)
		}
	}
	if strings.Contains(lines[0], "unscored") {
		t.Errorf("a whole idea is scored:\n%s", lines[0])
	}
}

func TestAFiledIdeaIsLeftOutAndTopCutsTheList(t *testing.T) {
	filed := strings.Replace(idea("filed", "up", "up", "up", "up", "true", "XS"), `"title`, `"bead":"mw-z.1","title`, 1)
	ledger := filed
	for _, id := range []string{"p", "q", "r", "s"} {
		ledger += idea(id, "up", "up", "up", "up", "true", "S")
	}
	all := order(rankOf(t, ledger, 0, false))
	if strings.Join(all, " ") != "p q r s" {
		t.Errorf("the filed idea is left out: %v", all)
	}
	if got := order(rankOf(t, ledger, 3, false)); strings.Join(got, " ") != "p q r" {
		t.Errorf("--top 3 prints three: %v", got)
	}
}

func TestTheRankLineHasRankTitleArrowsCompoundsSizeAndExpect(t *testing.T) {
	text := rankOf(t, idea("a", "up", "level", "down", "up", "true", "M"), 0, false)
	for _, want := range []string{"1.", "title a", "quality↑", "his_time→", "fuel↓", "speed↑", " C ", " M ", "Expect: refusals down"} {
		if !strings.Contains(text, want) {
			t.Errorf("line lacks %q:\n%s", want, text)
		}
	}
	if text := rankOf(t, idea("b", "up", "up", "up", "up", "false", "S"), 0, false); strings.Contains(text, " C ") {
		t.Errorf("an idea that does not compound has no C:\n%s", text)
	}
}

func TestTheRankInJSONHoldsTheOrderTheMissingFieldsAndTheRank(t *testing.T) {
	ledger := idea("gap", "up", "up", "", "up", "true", "XS") + idea("ok", "up", "up", "up", "up", "true", "XS")
	var got []struct {
		Rank     int      `json:"rank"`
		ID       string   `json:"id"`
		Unscored bool     `json:"unscored"`
		Missing  []string `json:"missing"`
	}
	if err := json.Unmarshal([]byte(rankOf(t, ledger, 0, true)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "ok" || got[0].Rank != 1 || got[1].ID != "gap" || !got[1].Unscored || len(got[1].Missing) != 1 || got[1].Missing[0] != "fuel" {
		t.Errorf("got %+v", got)
	}
}

func TestAnEmptyOrFullyFiledLedgerSaysThereIsNothingToRank(t *testing.T) {
	for _, ledger := range []string{"", `{"id":"a","title":"a","bead":"mw-1"}` + "\n"} {
		if text := rankOf(t, ledger, 0, false); !strings.Contains(text, "no unfiled idea") {
			t.Errorf("want a line saying so, got %q", text)
		}
	}
}

// samplePass is the recommendations a sleep pass wrote, as the agent was asked to in grinds/sleep.md.
const samplePass = `{"arc":"Postern","calls":[],"stuck":[],"investment_check":[],"ideas":[
 {"title":"Cache the gate","quality":"level","his_time":"level","fuel":"up","speed":"up","compounds":true,"size":"S",
  "expect":"Expect: dispatch-to-landing down",
  "why":{"quality":"same tests","his_time":"no card","fuel":"fewer reruns","speed":"2 min a landing"}},
 {"title":"Guard the stale lock","quality":"up","his_time":"up","fuel":"level","speed":"level","compounds":true,"size":"XS",
  "expect":"Expect: refusals down",
  "why":{"quality":"removes a class","his_time":"no hand step","fuel":"none","speed":"none"}}]}`

func TestASamplePassIsReadIntoTheRecordAndRanksWithoutGaps(t *testing.T) {
	ideas, err := application.ParseSleepIdeas(samplePass)
	if err != nil {
		t.Fatal(err)
	}
	if len(ideas) != 2 {
		t.Fatalf("want 2 ideas, got %d", len(ideas))
	}
	first := ideas[0]
	if first.Title != "Cache the gate" || first.Quality != "level" || first.HisTime != "level" || first.Fuel != "up" ||
		first.Speed != "up" || first.Compounds == nil || !*first.Compounds || first.Size != "S" ||
		first.Expect != "Expect: dispatch-to-landing down" || first.Why == nil || first.Why.Fuel != "fewer reruns" {
		t.Errorf("the first idea is not read whole: %+v", first)
	}
	// written as ledger lines, the ideas rank, none of them unscored
	var ledger string
	for n, one := range ideas {
		one.ID = "S" + string(rune('1'+n))
		body, err := json.Marshal(one)
		if err != nil {
			t.Fatal(err)
		}
		ledger += string(body) + "\n"
	}
	text := rankOf(t, ledger, 0, false)
	if strings.Contains(text, "unscored") {
		t.Errorf("a whole pass has no unscored idea:\n%s", text)
	}
	if got := order2(text); got != "Guard the stale lock|Cache the gate" {
		t.Errorf("order %q", got)
	}
}

// order2 is the titles in a rank, in order, whole.
func order2(text string) string {
	var titles []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		for _, t := range []string{"Guard the stale lock", "Cache the gate"} {
			if strings.Contains(line, t) {
				titles = append(titles, t)
			}
		}
	}
	return strings.Join(titles, "|")
}

func TestARecommendationsWithoutIdeasHasNoIdeaAndABadOneIsAnError(t *testing.T) {
	if ideas, err := application.ParseSleepIdeas(`{"arc":"x","calls":[]}`); err != nil || len(ideas) != 0 {
		t.Errorf("got %v, %v", ideas, err)
	}
	if _, err := application.ParseSleepIdeas(`{"ideas":"no"}`); err == nil {
		t.Error("want an error for ideas that are not a list")
	}
}

func TestTheSleepPassAsksForTheSevenFieldsOnEveryIdea(t *testing.T) {
	for _, field := range []string{"quality", "his_time", "fuel", "speed", "compounds", "size", "expect"} {
		if !strings.Contains(millwright.SleepInstructions, "`"+field+"`") {
			t.Errorf("grinds/sleep.md does not ask for %q", field)
		}
	}
	if !strings.Contains(millwright.SleepInstructions, "every field on every idea") {
		t.Error("grinds/sleep.md does not ask for every field on every idea")
	}
}

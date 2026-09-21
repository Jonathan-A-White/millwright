package application_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
)

// aLedgerLine is the line these tests write, with whatever a test changes.
func aLedgerLine(change func(*application.LedgerLine)) application.LedgerLine {
	line := application.LedgerLine{
		When:    time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC),
		StoryID: "mw-gq6.8",
		Title:   "mw next: close out a finished story",
		Outcome: "landed on main (fast-forward, abc123def456), 6 commits",
		Path:    domain.Path{Model: domain.ModelOpus, Effort: domain.EffortHigh},
		Result: application.SessionResult{
			SessionID: "s-1", Turns: 37, CostUSD: 4.21, Duration: 28 * time.Minute,
			Fuel: application.Fuel{Input: 1200, Output: 18000, CacheRead: 280000, CacheWrite: 12000},
		},
		Notes: []string{"dispatched by mw on vps", "session s-1"},
	}
	if change != nil {
		change(&line)
	}
	return line
}

func TestALedgerLineIsOneRowOfTheLedgersTable(t *testing.T) {
	line := aLedgerLine(nil).String()

	if strings.Contains(line, "\n") {
		t.Fatalf("expected one line, got:\n%s", line)
	}
	cells := strings.Split(strings.Trim(line, "|"), " | ")
	if len(cells) != 6 {
		t.Fatalf("expected the ledger's six columns, got %d in %q", len(cells), line)
	}
	for i, want := range []string{
		"2026-09-18",
		"mw next: close out a finished story (mw-gq6.8)",
		"landed on main",
		"opus/high",
		"311,200 tokens",
		"dispatched by mw on vps; session s-1",
	} {
		if !strings.Contains(cells[i], want) {
			t.Errorf("expected column %d to hold %q, got %q", i+1, want, cells[i])
		}
	}
}

func TestALedgerLineCannotBreakTheTableItLandsIn(t *testing.T) {
	line := aLedgerLine(func(l *application.LedgerLine) {
		l.Title = "a story | with a bar"
		l.Outcome = "not landed: the tests fail\nand here is the output"
	}).String()

	switch {
	case strings.Contains(line, "\n"):
		t.Errorf("expected the newline to be flattened, got:\n%s", line)
	case !strings.Contains(line, `a story \| with a bar`):
		t.Errorf("expected the bar in the title to be escaped, got %q", line)
	case strings.Count(line, " | ") != 5:
		t.Errorf("expected exactly six columns, got %q", line)
	}
}

func TestParseLedgerRowReadsBackWhatALedgerLineWrote(t *testing.T) {
	line := aLedgerLine(nil).String()

	row, ok := application.ParseLedgerRow(line)
	if !ok {
		t.Fatalf("expected %q to parse", line)
	}
	if row.Date != "2026-09-18" {
		t.Errorf("expected the date column, got %q", row.Date)
	}
	if row.Tokens != 311200 {
		t.Errorf("expected the token total off the fuel column, got %d", row.Tokens)
	}
}

func TestParseLedgerRowIsFalseForALineThatIsNotOneOfData(t *testing.T) {
	for _, line := range []string{
		"",
		"# Builder — ledger",
		"| date | story | outcome | model/effort | fuel | notes |",
		"|---|---|---|---|---|---|",
		"not a table row at all",
	} {
		if _, ok := application.ParseLedgerRow(line); ok {
			t.Errorf("expected %q not to parse as a data row", line)
		}
	}
}

func TestALedgerLineSaysWhatItDoesNotKnow(t *testing.T) {
	line := aLedgerLine(func(l *application.LedgerLine) {
		l.Path = domain.Path{}
		l.Title = ""
		l.Notes = nil
	}).String()

	switch {
	case !strings.Contains(line, "unknown"):
		t.Errorf("expected a path with no model to read as unknown, got %q", line)
	case !strings.Contains(line, "mw-gq6.8"):
		t.Errorf("expected a story with no title to be named by its id, got %q", line)
	case !strings.Contains(line, "—"):
		t.Errorf("expected an empty column to be filled rather than left blank, got %q", line)
	}
}

func TestALedgerLineForFuelAlreadyChargedCarriesNoTokenFigure(t *testing.T) {
	line := aLedgerLine(func(l *application.LedgerLine) {
		l.FuelCharged = true
	}).String()

	switch {
	case !strings.Contains(line, "already charged"):
		t.Errorf("expected the fuel column to say the fuel was charged already, got %q", line)
	case strings.Contains(line, "311,200"), strings.Contains(line, "tokens"), strings.Contains(line, "$4.21"):
		t.Errorf("expected no figure that a report would add again, got %q", line)
	case !strings.Contains(line, "session s-1"):
		t.Errorf("expected the line to still name the session it is about, got %q", line)
	}
	if row, ok := application.ParseLedgerRow(line); ok {
		t.Errorf("expected no fuel to read back off the line, got %d tokens", row.Tokens)
	}
}

func TestALedgerChargesASessionWhenALineCarriesItsFuelAndNamesIt(t *testing.T) {
	charged := aLedgerLine(nil).String()

	if !application.LedgerChargesSession(charged, "s-1") {
		t.Errorf("expected %q to charge session s-1", charged)
	}
	if application.LedgerChargesSession(charged, "s-10") || application.LedgerChargesSession(charged, "s") {
		t.Errorf("expected the session to be matched whole, not by prefix, in %q", charged)
	}
	if application.LedgerChargesSession(charged, "") {
		t.Errorf("expected no session at all never to be charged already")
	}
}

func TestALineThatOnlyPointsAtChargedFuelDoesNotChargeItAgain(t *testing.T) {
	pointer := aLedgerLine(func(l *application.LedgerLine) { l.FuelCharged = true }).String()

	if application.LedgerChargesSession(pointer, "s-1") {
		t.Errorf("expected %q not to count as the line that charged the session", pointer)
	}
}

func TestASessionRefusedAndThenLandedIsChargedOnce(t *testing.T) {
	refused := aLedgerLine(func(l *application.LedgerLine) {
		l.Outcome = "not landed: the rig's tests fail"
	}).String()
	ledger := []string{
		"| date | story | outcome | model/effort | fuel | notes |",
		"|---|---|---|---|---|---|",
		refused,
	}

	landed := aLedgerLine(func(l *application.LedgerLine) {
		for _, line := range ledger {
			l.FuelCharged = l.FuelCharged || application.LedgerChargesSession(line, l.Result.SessionID)
		}
	})
	ledger = append(ledger, landed.String())

	if !strings.Contains(ledger[3], "landed on main") {
		t.Fatalf("expected the second line to carry the second outcome, got %q", ledger[3])
	}
	total := 0
	for _, line := range ledger {
		if row, ok := application.ParseLedgerRow(line); ok {
			total += row.Tokens
		}
	}
	if total != 311200 {
		t.Errorf("expected the session's fuel counted once, got %d tokens across the ledger", total)
	}
}

func TestALedgerLineLandsAStoryOnlyWhenItsOutcomeSaysLanded(t *testing.T) {
	landed := aLedgerLine(nil).String()
	refused := aLedgerLine(func(l *application.LedgerLine) {
		l.Outcome = "not landed: the rig's tests fail"
	}).String()

	if !application.LedgerLandsStory(landed, "mw-gq6.8") {
		t.Errorf("a line whose outcome is landed should land mw-gq6.8: %s", landed)
	}
	if application.LedgerLandsStory(refused, "mw-gq6.8") {
		t.Errorf("a line whose outcome is not landed should not land mw-gq6.8: %s", refused)
	}
	if application.LedgerLandsStory(landed, "mw-gq6.80") {
		t.Errorf("a line for mw-gq6.8 should not land mw-gq6.80: %s", landed)
	}
}

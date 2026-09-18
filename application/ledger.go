package application

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// LedgerFileName is the seat's append-only history, one line per story, in the
// seat's own directory in the vault. It is never read at boot and never
// rewritten: mw opens it for append and adds one line (ADR 0003).
const LedgerFileName = "ledger.md"

// LedgerDate is how the date is written in a ledger line.
const LedgerDate = "2006-01-02"

// LedgerLine is one story's line in a seat's ledger: when it was closed out,
// which story it was, how it ended, what worked it, what it burned and whatever
// else the next person needs. The columns are the ones the ledger already has,
// in the order it already has them.
type LedgerLine struct {
	When    time.Time
	StoryID string
	Title   string
	// Outcome is what became of the story in one phrase: landed, or not landed
	// and why.
	Outcome string
	Path    domain.Path
	Result  SessionResult
	Notes   []string
}

// String is the line as it is appended, with no newline of its own. Every cell
// is written so that it cannot break the table it lands in: a newline becomes a
// space, and a bar is escaped.
func (l LedgerLine) String() string {
	cells := []string{
		l.When.Format(LedgerDate),
		l.story(),
		l.Outcome,
		l.worker(),
		l.Result.Spent(),
		strings.Join(l.Notes, "; "),
	}
	for i, cell := range cells {
		cells[i] = ledgerCell(cell)
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

// story is the story column: its title with its id after it, the way the
// ledger's earlier lines name a story.
func (l LedgerLine) story() string {
	title := strings.TrimSpace(l.Title)
	if title == "" {
		return l.StoryID
	}
	return fmt.Sprintf("%s (%s)", title, l.StoryID)
}

// worker is the model and effort column: what the story's path asked for, which
// is what the session was actually run with.
func (l LedgerLine) worker() string {
	model, effort := string(l.Path.Model), string(l.Path.Effort)
	switch {
	case model == "" && effort == "":
		return "unknown"
	case effort == "":
		return model
	case model == "":
		return effort
	default:
		return model + "/" + effort
	}
}

// LedgerRow is as much of one line of a ledger as a report reads back: the day
// it was written and the tokens its fuel column counted. It is the read side
// of LedgerLine, kept as small as `mw status` needs — nothing else in the
// factory reads a ledger back.
type LedgerRow struct {
	Date   string
	Tokens int
}

// ParseLedgerRow reads one markdown table row of a ledger the way LedgerLine
// wrote it: the date in the first column, the token total at the head of the
// fifth (fuel). It reports false for a line that is not a table row of data —
// the ledger's heading, its separator, a blank line, or one a person has
// hand-edited past reading — so that summing a ledger never fails on a line it
// does not need.
func ParseLedgerRow(line string) (LedgerRow, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") || !strings.HasSuffix(trimmed, "|") {
		return LedgerRow{}, false
	}
	cells := strings.Split(strings.Trim(trimmed, "|"), " | ")
	if len(cells) < 5 {
		return LedgerRow{}, false
	}
	tokens, ok := leadingTokens(cells[4])
	if !ok {
		return LedgerRow{}, false
	}
	return LedgerRow{Date: strings.TrimSpace(cells[0]), Tokens: tokens}, true
}

// leadingTokens reads the token total off the head of a ledger's fuel column,
// which Fuel.String writes as "12,345 tokens (...)".
func leadingTokens(cell string) (int, bool) {
	cell = strings.TrimSpace(cell)
	at := strings.Index(cell, " tokens")
	if at < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.ReplaceAll(cell[:at], ",", ""))
	if err != nil {
		return 0, false
	}
	return n, true
}

// ledgerCell makes one value safe to put in a markdown table cell.
func ledgerCell(text string) string {
	text = strings.ReplaceAll(text, "\r\n", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "|", `\|`)
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "—"
	}
	return text
}

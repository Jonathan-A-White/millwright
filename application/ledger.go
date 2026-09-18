package application

import (
	"fmt"
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

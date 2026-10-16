package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// InvestmentsFile is where the investments ledger lives in the vault.
const InvestmentsFile = "plans/investments/ledger.jsonl"

// The kinds of a ledger record: the investment made, and an outcome of it.
const (
	RecordInvestment = "investment"
	RecordOutcome    = "outcome"
)

// The results an outcome may name.
const (
	ResultWin   = "win"
	ResultLoss  = "loss"
	ResultMixed = "mixed"
)

// InvestmentRecord is one line of the investments ledger. A RecordInvestment
// carries the aim, the predicted gain, how it is measured and the days to the
// review; a RecordOutcome carries the result, what actually happened, the
// lesson and, if there is one, the better approach. Lines are only appended.
type InvestmentRecord struct {
	Kind  string    `json:"kind"`
	Bead  string    `json:"bead"`
	At    time.Time `json:"at"`
	Actor string    `json:"actor"`

	Aim        string `json:"aim,omitempty"`
	Predict    string `json:"predict,omitempty"`
	Measure    string `json:"measure,omitempty"`
	ReviewDays int    `json:"review_days,omitempty"`

	Result string `json:"result,omitempty"`
	Actual string `json:"actual,omitempty"`
	Lesson string `json:"lesson,omitempty"`
	Next   string `json:"next,omitempty"`
}

// InvestmentBook is the investments ledger: Investments reads every record
// oldest first, none and no error when there is no ledger yet; Append adds one
// on the end and never rewrites a line. The real adapter is infrastructure/vault.
type InvestmentBook interface {
	Investments(ctx context.Context) ([]InvestmentRecord, error)
	AppendInvestment(ctx context.Context, record InvestmentRecord) error
}

// Investment is an investment as the views show it: the record that made it and
// the outcomes recorded since, in the order they were written.
type Investment struct {
	Made     InvestmentRecord
	Outcomes []InvestmentRecord
}

// Newest is the outcome the views show, the last one written.
func (i Investment) Newest() (InvestmentRecord, bool) {
	if len(i.Outcomes) == 0 {
		return InvestmentRecord{}, false
	}
	return i.Outcomes[len(i.Outcomes)-1], true
}

// ReviewAt is when the investment is to be looked at again.
func (i Investment) ReviewAt() time.Time {
	return i.Made.At.AddDate(0, 0, i.Made.ReviewDays)
}

// Due says the review date has passed and no outcome has been recorded.
func (i Investment) Due(now time.Time) bool {
	return len(i.Outcomes) == 0 && now.After(i.ReviewAt())
}

// GroupInvestments folds the records into investments, in the order they were
// made. The first record that makes an investment of a bead stands, and an
// outcome of a bead with no investment before it is left out.
func GroupInvestments(records []InvestmentRecord) []Investment {
	var all []Investment
	index := map[string]int{}
	for _, rec := range records {
		switch rec.Kind {
		case RecordInvestment:
			if _, seen := index[rec.Bead]; !seen {
				index[rec.Bead] = len(all)
				all = append(all, Investment{Made: rec})
			}
		case RecordOutcome:
			if at, seen := index[rec.Bead]; seen {
				all[at].Outcomes = append(all[at].Outcomes, rec)
			}
		}
	}
	return all
}

// ErrInvestmentExists says a bead already has an investment on the ledger.
var ErrInvestmentExists = errors.New("already has an investment on the ledger")

// Invest is mw invest: it appends investments and their outcomes to the ledger
// and lists them. It never edits a line.
type Invest struct {
	Book  InvestmentBook
	Actor string
	// Vault commits and pushes the ledger after a line is appended to it; nil
	// leaves that to a person. Warn is where a push that did not go is said.
	Vault RecordVault
	Warn  io.Writer
	Now   func() time.Time
	Out   io.Writer
}

func (v Invest) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

// record commits the ledger, which a line was just appended to, and pushes it.
func (v Invest) record(ctx context.Context, message string) error {
	if err := recordInVault(ctx, v.Vault, v.Warn, message, []string{InvestmentsFile}); err != nil {
		return fmt.Errorf("investing: %w", err)
	}
	return nil
}

func (v Invest) read(ctx context.Context) ([]Investment, error) {
	if v.Book == nil {
		return nil, fmt.Errorf("investments: there is no ledger to read")
	}
	records, err := v.Book.Investments(ctx)
	if err != nil {
		return nil, fmt.Errorf("investments: %w", err)
	}
	return GroupInvestments(records), nil
}

func (v Invest) find(ctx context.Context, bead string) (Investment, bool, error) {
	all, err := v.read(ctx)
	if err != nil {
		return Investment{}, false, err
	}
	for _, inv := range all {
		if inv.Made.Bead == bead {
			return inv, true, nil
		}
	}
	return Investment{}, false, nil
}

// Add appends the investment a bead is, once. An empty field or a review of
// less than a day is refused, and so is a second add for the same bead.
func (v Invest) Add(ctx context.Context, bead, aim, predict, measure string, reviewDays int) error {
	bead = strings.TrimSpace(bead)
	for _, field := range []struct{ name, value string }{{"bead", bead}, {"aim", aim}, {"predict", predict}, {"measure", measure}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("investing: the %s is empty: it needs the bead, --aim, --predict and --measure", field.name)
		}
	}
	if reviewDays < 1 {
		return fmt.Errorf("investing: --review is %d days: give the days until it is looked at again, 1 or more", reviewDays)
	}
	if _, found, err := v.find(ctx, bead); err != nil {
		return err
	} else if found {
		return fmt.Errorf("investing: %s %w: record its result with mw invest outcome", bead, ErrInvestmentExists)
	}
	rec := InvestmentRecord{Kind: RecordInvestment, Bead: bead, At: v.now(), Actor: v.Actor,
		Aim: strings.TrimSpace(aim), Predict: strings.TrimSpace(predict), Measure: strings.TrimSpace(measure), ReviewDays: reviewDays}
	if err := v.Book.AppendInvestment(ctx, rec); err != nil {
		return fmt.Errorf("investing: %w", err)
	}
	if err := v.record(ctx, "mw invest add "+bead); err != nil {
		return err
	}
	if v.Out != nil {
		fmt.Fprintf(v.Out, "recorded the investment %s; look again on %s\n", bead, Investment{Made: rec}.ReviewAt().Format(LedgerDate))
	}
	return nil
}

// Outcome appends what happened to an investment: win, loss or mixed, the
// numbers, the lesson and optionally the better approach. A newer outcome of
// the same bead is a new line and is the one the views show.
func (v Invest) Outcome(ctx context.Context, bead, result, actual, lesson, next string) error {
	bead = strings.TrimSpace(bead)
	result = strings.ToLower(strings.TrimSpace(result))
	if result != ResultWin && result != ResultLoss && result != ResultMixed {
		return fmt.Errorf("investing: --result %q is not win, loss or mixed", result)
	}
	if strings.TrimSpace(actual) == "" || strings.TrimSpace(lesson) == "" {
		return fmt.Errorf("investing: an outcome needs --actual (what happened, with numbers) and --lesson")
	}
	if _, found, err := v.find(ctx, bead); err != nil {
		return err
	} else if !found {
		return fmt.Errorf("investing: there is no investment of %s on the ledger: add it first with mw invest add", bead)
	}
	rec := InvestmentRecord{Kind: RecordOutcome, Bead: bead, At: v.now(), Actor: v.Actor,
		Result: result, Actual: strings.TrimSpace(actual), Lesson: strings.TrimSpace(lesson), Next: strings.TrimSpace(next)}
	if err := v.Book.AppendInvestment(ctx, rec); err != nil {
		return fmt.Errorf("investing: %w", err)
	}
	if err := v.record(ctx, "mw invest outcome "+bead); err != nil {
		return err
	}
	if v.Out != nil {
		fmt.Fprintf(v.Out, "recorded a %s for %s\n", result, bead)
	}
	return nil
}

// DueInvestments are the investments whose review date is past with no outcome,
// the longest overdue first.
func DueInvestments(all []Investment, now time.Time) []Investment {
	var due []Investment
	for _, inv := range all {
		if inv.Due(now) {
			due = append(due, inv)
		}
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].ReviewAt().Before(due[j].ReviewAt()) })
	return due
}

// Due prints the investments to look at again.
func (v Invest) Due(ctx context.Context) error {
	all, err := v.read(ctx)
	if err != nil {
		return err
	}
	writeDue(v.Out, DueInvestments(all, v.now()))
	return nil
}

// writeDue prints the investments to look at again, or that none is due.
func writeDue(w io.Writer, due []Investment) {
	if len(due) == 0 {
		fmt.Fprintln(w, "nothing is due: every investment is inside its review date or has an outcome")
		return
	}
	fmt.Fprintf(w, "%d due for review:\n", len(due))
	for _, inv := range due {
		fmt.Fprintf(w, "%s (review was %s)\n", inv.Made.Bead, inv.ReviewAt().Format(LedgerDate))
		writeIndented(w, "  ", "aim: "+inv.Made.Aim)
		writeIndented(w, "  ", "measured by: "+inv.Made.Measure)
	}
}

// List prints every investment with its prediction, how it is measured, its
// outcomes in order and the lesson of each.
func (v Invest) List(ctx context.Context) error {
	all, err := v.read(ctx)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(v.Out, "no investments on the ledger yet")
		return nil
	}
	for i, inv := range all {
		if i > 0 {
			fmt.Fprintln(v.Out)
		}
		fmt.Fprintf(v.Out, "%s (made %s by %s, review %s)\n", inv.Made.Bead, inv.Made.At.Format(LedgerDate), inv.Made.Actor, inv.ReviewAt().Format(LedgerDate))
		writeIndented(v.Out, "  ", "aim: "+inv.Made.Aim)
		writeIndented(v.Out, "  ", "predicted: "+inv.Made.Predict)
		writeIndented(v.Out, "  ", "measured by: "+inv.Made.Measure)
		if len(inv.Outcomes) == 0 {
			state := "no outcome yet"
			if inv.Due(v.now()) {
				state = "no outcome yet, DUE"
			}
			fmt.Fprintf(v.Out, "  %s\n", state)
		}
		for _, out := range inv.Outcomes {
			writeIndented(v.Out, "  ", fmt.Sprintf("%s %s: %s", out.At.Format(LedgerDate), out.Result, out.Actual))
			writeIndented(v.Out, "    ", "lesson: "+out.Lesson)
			if out.Next != "" {
				writeIndented(v.Out, "    ", "next: "+out.Next)
			}
		}
	}
	return nil
}

// writeIndented prints text wrapped to ReportWidth columns, each line after the
// first indented two more spaces.
func writeIndented(w io.Writer, indent, text string) {
	for i, line := range wrapWords(text, ReportWidth-len(indent)-2) {
		pad := indent
		if i > 0 {
			pad += "  "
		}
		fmt.Fprintf(w, "%s%s\n", pad, line)
	}
}

// InvestmentLine is a window's outcome as the report tells it.
type InvestmentLine struct {
	Bead   string `json:"bead"`
	Aim    string `json:"aim"`
	Result string `json:"result"`
	Actual string `json:"actual"`
	Lesson string `json:"lesson"`
	Next   string `json:"next,omitempty"`
}

// InvestmentsSection is the report's INVESTMENTS: the outcomes the window
// recorded, wins first, then losses, then mixed; and how many investments are
// due for review now.
type InvestmentsSection struct {
	Outcomes []InvestmentLine `json:"outcomes"`
	Due      int              `json:"due"`
}

// ReadInvestmentsSection takes, of each investment, its newest outcome if it
// was written inside the window, and counts the investments due at now.
func ReadInvestmentsSection(all []Investment, window ReportWindow, now time.Time) InvestmentsSection {
	rank := map[string]int{ResultWin: 0, ResultLoss: 1, ResultMixed: 2}
	section := InvestmentsSection{Outcomes: []InvestmentLine{}, Due: len(DueInvestments(all, now))}
	for _, inv := range all {
		out, ok := inv.Newest()
		if !ok || out.At.Before(window.Since) || reportDay(out.At).After(reportDay(window.Until)) {
			continue
		}
		section.Outcomes = append(section.Outcomes, InvestmentLine{Bead: inv.Made.Bead, Aim: inv.Made.Aim,
			Result: out.Result, Actual: out.Actual, Lesson: out.Lesson, Next: out.Next})
	}
	sort.SliceStable(section.Outcomes, func(i, j int) bool {
		return rank[section.Outcomes[i].Result] < rank[section.Outcomes[j].Result]
	})
	return section
}

// writeInvestments prints the INVESTMENTS part of the report: a win named with
// its measured gain, a loss with its lesson and the better approach.
func writeInvestments(b *strings.Builder, s *InvestmentsSection) {
	if s == nil {
		return
	}
	b.WriteString("\nINVESTMENTS\n")
	if len(s.Outcomes) == 0 {
		b.WriteString("  none this window\n")
	}
	for _, line := range s.Outcomes {
		fmt.Fprintf(b, "  %s %s\n", strings.ToUpper(line.Result), fitLine(line.Bead, ReportWidth-8))
		writeIndented(b, "    ", "aim: "+line.Aim)
		if line.Result == ResultWin {
			writeIndented(b, "    ", "gain: "+line.Actual)
			writeIndented(b, "    ", "lesson: "+line.Lesson)
		} else {
			writeIndented(b, "    ", "what happened: "+line.Actual)
			writeIndented(b, "    ", "lesson: "+line.Lesson)
		}
		if line.Next != "" {
			writeIndented(b, "    ", "better: "+line.Next)
		}
	}
	fmt.Fprintf(b, "  %d still due for review\n", s.Due)
}

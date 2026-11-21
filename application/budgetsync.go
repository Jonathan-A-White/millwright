package application

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// BudgetSheet is his Budget spreadsheet, read as the text it shows; the
// gdrive Session is it.
type BudgetSheet interface {
	// Read is the cells of a tab (limited to rng when not empty).
	Read(ctx context.Context, id, tab, rng string) ([][]string, error)
}

// YNABCategory is one budget category as YNAB keeps it. Amounts are YNAB's
// milliunits: 239.00 is 239000.
type YNABCategory struct {
	ID    string
	Group string
	Name  string
	// GoalTarget is the category's target in milliunits; HasTarget says a
	// goal is set at all (a goal with a target of 0 is a goal).
	GoalTarget int64
	HasTarget  bool
	Balance    int64
	Hidden     bool
	Deleted    bool
}

// YNABBudget is the part of YNAB that setting targets needs.
type YNABBudget interface {
	// Categories is every category of the budget, deleted ones left out.
	Categories(ctx context.Context, budget string) ([]YNABCategory, error)
	// SetGoalTarget sets one category's target, in milliunits.
	SetGoalTarget(ctx context.Context, budget, category string, milliunits int64) error
}

// Names of the two columns of the Budget sheet that matter.
const (
	budgetItemColumn  = "Item"
	budgetTotalColumn = "Total Per Month"
)

// BudgetSync is mw budget sync: the Budget sheet is the one source of his
// monthly amounts, and YNAB's category targets follow it. A sheet row and a
// YNAB category of the same name (case and spaces ignored) are the same
// thing; a target is written only where it differs.
type BudgetSync struct {
	Sheet BudgetSheet
	YNAB  YNABBudget
	Out   io.Writer
	// SheetID and Tab name the spreadsheet and its tab; BudgetID is the YNAB budget.
	SheetID  string
	Tab      string
	BudgetID string
	// DryRun reads both and writes nothing to YNAB.
	DryRun bool
}

// budgetChange is one target to move.
type budgetChange struct {
	category YNABCategory
	to       int64
}

// sheetAmount is one usable row of the sheet.
type sheetAmount struct {
	item  string
	milli int64
}

// Run reads the sheet and YNAB, says what differs and, unless DryRun, writes
// each changed target. It stops at the first write YNAB refuses, having
// printed the changes already made.
func (b BudgetSync) Run(ctx context.Context) error {
	rows, err := b.Sheet.Read(ctx, b.SheetID, b.Tab, "")
	if err != nil {
		return fmt.Errorf("reading the Budget sheet: %w", err)
	}
	amounts, skipped, err := budgetAmounts(rows)
	if err != nil {
		return fmt.Errorf("the Budget sheet's tab %q: %w", b.Tab, err)
	}
	cats, err := b.YNAB.Categories(ctx, b.BudgetID)
	if err != nil {
		return fmt.Errorf("reading YNAB's categories: %w", err)
	}

	byName := map[string]YNABCategory{}
	for _, c := range cats {
		if c.Deleted || c.Hidden {
			continue
		}
		key := budgetKey(c.Name)
		if _, dup := byName[key]; !dup {
			byName[key] = c
		}
	}
	var changes []budgetChange
	var noCategory []string
	matched := map[string]bool{}
	for _, a := range amounts {
		c, ok := byName[budgetKey(a.item)]
		if !ok {
			noCategory = append(noCategory, a.item)
			continue
		}
		matched[budgetKey(c.Name)] = true
		if c.HasTarget && c.GoalTarget == a.milli {
			continue
		}
		changes = append(changes, budgetChange{category: c, to: a.milli})
	}
	var noRow []string
	for _, c := range byName {
		if c.HasTarget && !matched[budgetKey(c.Name)] && !skippedItem(skipped, c.Name) {
			noRow = append(noRow, fmt.Sprintf("%s (%s)", c.Name, Dollars(c.GoalTarget)))
		}
	}
	sort.Strings(noRow)

	out := b.Out
	verb := "changed"
	if b.DryRun {
		verb = "would change"
	}
	if len(changes) == 0 {
		fmt.Fprintln(out, "nothing to change")
	}
	for _, ch := range changes {
		line := fmt.Sprintf("%s %s: %s -> %s", verb, strings.Join(strings.Fields(ch.category.Name), " "), oldTarget(ch.category), Dollars(ch.to))
		if !b.DryRun {
			if err := b.YNAB.SetGoalTarget(ctx, b.BudgetID, ch.category.ID, ch.to); err != nil {
				return fmt.Errorf("setting the target of %s in YNAB: %w", ch.category.Name, err)
			}
		}
		fmt.Fprintln(out, line)
	}
	b.list("sheet items with no YNAB category:", noCategory)
	b.list("YNAB categories with a target but no sheet row:", noRow)
	skippedLines := make([]string, len(skipped))
	for i, s := range skipped {
		skippedLines[i] = fmt.Sprintf("%s: %s", s.item, s.why)
	}
	b.list("sheet rows skipped, nothing written for them:", skippedLines)
	return nil
}

func (b BudgetSync) list(title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(b.Out, title)
	for _, l := range lines {
		fmt.Fprintf(b.Out, "  %s\n", l)
	}
}

func oldTarget(c YNABCategory) string {
	if !c.HasTarget {
		return "no target"
	}
	return Dollars(c.GoalTarget)
}

// Dollars is milliunits as dollars and cents.
func Dollars(milli int64) string {
	sign := ""
	if milli < 0 {
		sign = "-"
		milli = -milli
	}
	cents := (milli + 5) / 10
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// budgetKey is a name with its case and spaces taken out.
func budgetKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), ""))
}

// skippedRow is a sheet row left alone, with the reason.
type skippedRow struct {
	item string
	why  string
}

func skippedItem(skipped []skippedRow, name string) bool {
	for _, s := range skipped {
		if budgetKey(s.item) == budgetKey(name) {
			return true
		}
	}
	return false
}

// budgetAmounts reads the usable (Item, Total Per Month) rows under the
// header row, and the rows left out with why. A blank Total, an error cell
// such as #REF! and anything that is not an amount is left out, never read as
// 0. An item that is on the sheet twice with different amounts is left out.
func budgetAmounts(rows [][]string) ([]sheetAmount, []skippedRow, error) {
	head, itemCol, totalCol := -1, -1, -1
	for i, row := range rows {
		ic, tc := -1, -1
		for j, cell := range row {
			switch budgetKey(cell) {
			case budgetKey(budgetItemColumn):
				if ic < 0 {
					ic = j
				}
			case budgetKey(budgetTotalColumn):
				if tc < 0 {
					tc = j
				}
			}
		}
		if ic >= 0 && tc >= 0 {
			head, itemCol, totalCol = i, ic, tc
			break
		}
	}
	if head < 0 {
		return nil, nil, fmt.Errorf("no row has both the columns %q and %q", budgetItemColumn, budgetTotalColumn)
	}
	var amounts []sheetAmount
	var skipped []skippedRow
	seen := map[string]int{}
	conflict := map[string]bool{}
	for _, row := range rows[head+1:] {
		item := cell(row, itemCol)
		if item == "" {
			continue
		}
		raw := cell(row, totalCol)
		milli, why := parseMilli(raw)
		if why != "" {
			skipped = append(skipped, skippedRow{item: item, why: why})
			continue
		}
		key := budgetKey(item)
		if at, dup := seen[key]; dup {
			if amounts[at].milli != milli && !conflict[key] {
				conflict[key] = true
				skipped = append(skipped, skippedRow{item: item, why: "on the sheet twice with different amounts"})
			}
			continue
		}
		seen[key] = len(amounts)
		amounts = append(amounts, sheetAmount{item: item, milli: milli})
	}
	kept := amounts[:0]
	for _, a := range amounts {
		if !conflict[budgetKey(a.item)] {
			kept = append(kept, a)
		}
	}
	return kept, skipped, nil
}

func cell(row []string, i int) string {
	if i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// parseMilli reads a cell as dollars, in milliunits. When it is not an
// amount it says why in the second value.
func parseMilli(raw string) (int64, string) {
	if raw == "" {
		return 0, "the Total Per Month is blank"
	}
	if strings.HasPrefix(raw, "#") {
		return 0, "the Total Per Month is " + raw
	}
	s := strings.NewReplacer("$", "", ",", "", " ", "").Replace(raw)
	negative := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		negative = true
		s = s[1 : len(s)-1]
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Sprintf("the Total Per Month %q is not an amount", raw)
	}
	if negative {
		f = -f
	}
	return int64(math.Round(f*100)) * 10, ""
}

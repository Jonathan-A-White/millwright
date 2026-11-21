package application

import (
	"fmt"
	"sort"
	"strings"
)

// targetMonths is how many months one YNAB target covers: a monthly goal (or
// a goal with no cadence, as the needed-by-a-date kinds have) is 1, a yearly
// one 12. When it is not a cadence this can convert (weekly, and any
// other), ok is false and why names it.
func targetMonths(c YNABCategory) (months int64, label string, ok bool, why string) {
	freq := int64(c.GoalCadenceFrequency)
	if freq < 1 {
		freq = 1
	}
	switch c.GoalCadence {
	case 0, 1:
		if freq > 1 {
			return freq, fmt.Sprintf("every %d months", freq), true, ""
		}
		return 1, "", true, ""
	case 13:
		if freq > 1 {
			return 12 * freq, fmt.Sprintf("every %d years", freq), true, ""
		}
		return 12, "yearly", true, ""
	case 2:
		return 0, "", false, "the YNAB target is weekly, which this does not convert"
	}
	return 0, "", false, fmt.Sprintf("the YNAB target has cadence %d, which this does not convert", c.GoalCadence)
}

// sameTarget says the YNAB target already is the sheet's monthly amount: for a
// target covering several months, when it is within two cents a month of it
// (his Life Insurance, 4478.20 a year, is 373.20 a month on the sheet, 1.7
// cents a month from 373.18). A monthly target one cent off is the same too:
// YNAB keeps a monthly target a cent from the sheet's (Giving 2158.34 against
// 2158.33), which is rounding, not a change; two cents off is a change.
func sameTarget(c YNABCategory, months, sheetMilli int64) bool {
	if !c.HasTarget {
		return false
	}
	want := sheetMilli * months
	diff := c.GoalTarget - want
	if diff < 0 {
		diff = -diff
	}
	if months == 1 {
		return diff <= oneCent
	}
	return diff <= 20*months
}

// oneCent is a cent in YNAB's milliunits.
const oneCent = 10

// TargetText is a category's target as a line shows it, its cadence named when
// it is not monthly.
func TargetText(c YNABCategory) string {
	if !c.HasTarget {
		return "no target"
	}
	if _, label, ok, _ := targetMonths(c); ok && label != "" {
		return Dollars(c.GoalTarget) + " " + label
	}
	return Dollars(c.GoalTarget)
}

// budgetStem is the name of a sheet row written '<X> Budget', as its key; empty
// when the row is not so written.
func budgetStem(item string) string {
	key := budgetKey(item)
	stem := strings.TrimSuffix(key, "budget")
	if stem == key {
		return ""
	}
	return stem
}

// matchCategory finds the YNAB category of a sheet row: the one of the same
// name, else, for a row written '<X> Budget', the one named exactly '<X>'.
func matchCategory(byName map[string]YNABCategory, item string) (YNABCategory, bool) {
	if c, ok := byName[budgetKey(item)]; ok {
		return c, true
	}
	if stem := budgetStem(item); stem != "" {
		c, ok := byName[stem]
		return c, ok
	}
	return YNABCategory{}, false
}

// noCategoryLine is a sheet item with no YNAB category; a '<X> Budget' row
// that more than one category starts with says which, so he can pick.
func noCategoryLine(byName map[string]YNABCategory, item string) string {
	stem := budgetStem(item)
	if stem == "" {
		return item
	}
	var names []string
	for k, c := range byName {
		if strings.HasPrefix(k, stem) {
			names = append(names, strings.Join(strings.Fields(c.Name), " "))
		}
	}
	if len(names) < 2 {
		return item
	}
	sort.Strings(names)
	return fmt.Sprintf("%s (YNAB has more than one: %s)", item, strings.Join(names, ", "))
}

// changeLine is one change as printed: the old and new target, and for a target
// that is not monthly its cadence and the monthly amount it is.
func changeLine(verb string, ch budgetChange) string {
	name := strings.Join(strings.Fields(ch.category.Name), " ")
	line := fmt.Sprintf("%s %s: %s -> %s", verb, name, oldTarget(ch.category), Dollars(ch.to))
	if ch.label != "" {
		line += fmt.Sprintf(" (%s, %s a month)", ch.label, Dollars(ch.to/ch.months))
	}
	return line
}

// exactNamesFirst puts the sheet rows named as a category is before the rows
// written '<X> Budget', so a row named exactly as a category keeps it; the
// order within each is the sheet's.
func exactNamesFirst(amounts []sheetAmount, byName map[string]YNABCategory) []sheetAmount {
	var exact, rest []sheetAmount
	for _, a := range amounts {
		if _, ok := byName[budgetKey(a.item)]; ok {
			exact = append(exact, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(exact, rest...)
}

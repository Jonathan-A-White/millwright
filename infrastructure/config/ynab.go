package config

// What mw ynab and mw budget sync read, each from an environment variable, else
// the root-table key of ~/.config/mw/config.toml, else the default.
const (
	// YNABBudgetEnv replaces ynab_budget_id.
	YNABBudgetEnv = "MW_YNAB_BUDGET_ID"
	// BudgetSheetEnv replaces budget_sheet_id.
	BudgetSheetEnv = "MW_BUDGET_SHEET_ID"
	// BudgetSheetTabEnv replaces budget_sheet_tab.
	BudgetSheetTabEnv = "MW_BUDGET_SHEET_TAB"
)

// DefaultYNABBudget is his YNAB budget 'Simplified Budget'.
const DefaultYNABBudget = "483d32cb-d741-439c-8d65-cac9fedb85e0"

// DefaultBudgetSheet is his main Budget spreadsheet, and DefaultBudgetSheetTab
// the tab whose Item and Total Per Month columns mw budget sync reads.
const (
	DefaultBudgetSheet    = "1JLk_cznrn9iIqi4wsr2XQrx30lN2ny7O7Zu4LssIKBI"
	DefaultBudgetSheetTab = "2025"
)

// YNABBudget is the YNAB budget id mw ynab and mw budget sync work on.
func YNABBudget() (string, error) {
	return optionalSetting("ynab_budget_id", YNABBudgetEnv, DefaultYNABBudget)
}

// BudgetSheet is the id of the Budget spreadsheet.
func BudgetSheet() (string, error) {
	return optionalSetting("budget_sheet_id", BudgetSheetEnv, DefaultBudgetSheet)
}

// BudgetSheetTab is the tab of the Budget spreadsheet mw budget sync reads.
func BudgetSheetTab() (string, error) {
	return optionalSetting("budget_sheet_tab", BudgetSheetTabEnv, DefaultBudgetSheetTab)
}

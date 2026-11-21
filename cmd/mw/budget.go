package main

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"

	"github.com/spf13/cobra"
)

// budgetSheet is his Budget spreadsheet's reader: the signed-in gdrive
// session; a test puts a stand-in in its place.
var budgetSheet = func(context.Context) (application.BudgetSheet, error) {
	s, err := gdriveSession()
	if err != nil {
		return nil, err
	}
	return s, nil
}

// newBudgetCmd builds `mw budget`: his YNAB targets kept level with his
// Budget sheet.
func newBudgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Keep his YNAB monthly targets level with his Budget sheet (sync)",
		Long: "budget works on his budget. The Budget sheet is the one source of his monthly amounts; YNAB's\n" +
			"category targets follow it by a command, never by hand.",
	}
	cmd.AddCommand(newBudgetSyncCmd())
	return cmd
}

func newBudgetSyncCmd() *cobra.Command {
	var dryRun bool
	var budget, sheetID, tab string
	cmd := &cobra.Command{
		Use:   "sync [--dry-run]",
		Short: "Set each YNAB category's monthly target from the Budget sheet's Total Per Month",
		Long: "sync reads the Budget sheet's Item and Total Per Month columns (through the mw gdrive sign-in)\n" +
			"and, for each YNAB category of the same name (case and spaces ignored), sets its goal target to\n" +
			"that amount, writing only the targets that differ and printing each change. A second run\n" +
			"prints 'nothing to change'.\n\n" +
			"A row whose Total is blank, #REF! or not an amount is skipped and named, never written as 0.\n" +
			"It also lists sheet items with no YNAB category and YNAB categories with a target but no sheet\n" +
			"row; it changes neither. With --dry-run it reads both and writes nothing.\n\n" +
			"The sheet is budget_sheet_id and its tab budget_sheet_tab in ~/.config/mw/config.toml (or\n" +
			"$" + config.BudgetSheetEnv + " and $" + config.BudgetSheetTabEnv + "); the YNAB budget is --budget,\n" +
			"else ynab_budget_id; the token is the one mw ynab uses.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var err error
			if sheetID == "" {
				if sheetID, err = config.BudgetSheet(); err != nil {
					return err
				}
			}
			if tab == "" {
				if tab, err = config.BudgetSheetTab(); err != nil {
					return err
				}
			}
			c, b, err := ynabClient(cmd.Context(), budget)
			if err != nil {
				return err
			}
			sheet, err := budgetSheet(cmd.Context())
			if err != nil {
				return err
			}
			return application.BudgetSync{
				Sheet: sheet, YNAB: c, Out: cmd.OutOrStdout(),
				SheetID: sheetID, Tab: tab, BudgetID: b, DryRun: dryRun,
			}.Run(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "read the sheet and YNAB, say what would change, write nothing")
	cmd.Flags().StringVar(&budget, "budget", "", "the YNAB budget id (default: the config's, else his Simplified Budget)")
	cmd.Flags().StringVar(&sheetID, "sheet-id", "", "the Budget spreadsheet's id (default: the config's)")
	cmd.Flags().StringVar(&tab, "tab", "", "the tab to read (default: the config's)")
	return cmd
}

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/config"
	"github.com/Jonathan-A-White/millwright/infrastructure/ynab"

	"github.com/spf13/cobra"
)

// ynabURL is YNAB's API; a test puts a stand-in in its place.
var ynabURL = ynab.DefaultURL

// ynabToken is his YNAB token, read from the vault's secrets; a test puts a
// stand-in in its place. The value is never printed or logged.
var ynabToken = func(ctx context.Context) (string, error) {
	store, _, _, err := secretsStore()
	if err != nil {
		return "", fmt.Errorf("opening the vault's secrets: %w", err)
	}
	raw, err := store.Get(ctx, ynab.TokenName)
	if errors.Is(err, application.ErrNoSecret) {
		return "", fmt.Errorf("no YNAB token is kept: put it with mw secrets put %s", ynab.TokenName)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// ynabClient is the client signed in with the kept token, and the budget
// asked for (--budget, else the config's, else the default).
func ynabClient(ctx context.Context, budget string) (ynab.Client, string, error) {
	if budget == "" {
		var err error
		if budget, err = config.YNABBudget(); err != nil {
			return ynab.Client{}, "", err
		}
	}
	token, err := ynabToken(ctx)
	if err != nil {
		return ynab.Client{}, "", err
	}
	return ynab.Client{URL: ynabURL, Token: token}, budget, nil
}

// newYnabCmd builds `mw ynab`: his YNAB budget read, as rows.
func newYnabCmd() *cobra.Command {
	var budget string
	cmd := &cobra.Command{
		Use:   "ynab",
		Short: "Read his YNAB budget: accounts, categories, transactions",
		Long: "ynab reads his YNAB budget through YNAB's API with the token kept by\n" +
			"mw secrets put " + ynab.TokenName + ". The budget is --budget, else ynab_budget_id in\n" +
			"~/.config/mw/config.toml (or $" + config.YNABBudgetEnv + "), else his 'Simplified Budget'.\n" +
			"The token is never printed or logged. Amounts are dollars. To set the monthly targets from\n" +
			"the Budget sheet, see mw budget sync.",
	}
	cmd.PersistentFlags().StringVar(&budget, "budget", "", "the YNAB budget id (default: the config's, else his Simplified Budget)")
	cmd.AddCommand(newYnabAccountsCmd(&budget), newYnabCategoriesCmd(&budget), newYnabTransactionsCmd(&budget))
	return cmd
}

func newYnabAccountsCmd(budget *string) *cobra.Command {
	return &cobra.Command{
		Use:   "accounts",
		Short: "List the budget's accounts: id, name, type, balance, closed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, b, err := ynabClient(cmd.Context(), *budget)
			if err != nil {
				return err
			}
			accounts, err := c.Accounts(cmd.Context(), b)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, a := range accounts {
				closed := ""
				if a.Closed {
					closed = "closed"
				}
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", a.ID, oneLine(a.Name), a.Type, application.Dollars(a.Balance), closed)
			}
			return nil
		},
	}
}

func newYnabCategoriesCmd(budget *string) *cobra.Command {
	return &cobra.Command{
		Use:   "categories",
		Short: "List the budget's categories: id, group, name, target, balance",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, b, err := ynabClient(cmd.Context(), *budget)
			if err != nil {
				return err
			}
			cats, err := c.Categories(cmd.Context(), b)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, k := range cats {
				target := "-"
				if k.HasTarget {
					target = application.Dollars(k.GoalTarget)
				}
				fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", k.ID, oneLine(k.Group), oneLine(k.Name), target, application.Dollars(k.Balance))
			}
			return nil
		},
	}
}

var ynabDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func newYnabTransactionsCmd(budget *string) *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "transactions [--since YYYY-MM-DD]",
		Short: "List the budget's transactions: date, amount, payee, category, account, memo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if since != "" && !ynabDate.MatchString(since) {
				return fmt.Errorf("--since %q is not a date: write it as YYYY-MM-DD", since)
			}
			c, b, err := ynabClient(cmd.Context(), *budget)
			if err != nil {
				return err
			}
			txs, err := c.Transactions(cmd.Context(), b, since)
			if err != nil {
				return err
			}
			printTransactions(cmd.OutOrStdout(), txs)
			return nil
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "only transactions on or after this date, YYYY-MM-DD")
	return cmd
}

func printTransactions(out io.Writer, txs []ynab.Transaction) {
	for _, t := range txs {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\n", t.Date, application.Dollars(t.Amount), oneLine(t.PayeeName), oneLine(t.CategoryName), oneLine(t.AccountName), oneLine(t.Memo))
	}
}

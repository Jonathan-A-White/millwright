// Package ynab is the YNAB API, narrowed to what mw needs: his accounts,
// categories and transactions read, and one category's target written. The
// token goes in the Authorization header only; no error quotes it.
package ynab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultURL is YNAB's API address.
const DefaultURL = "https://api.ynab.com/v1"

// TokenName is the secret the token is kept under (mw secrets put).
const TokenName = "ynab_token"

// DefaultBudgetID is his 'Simplified Budget'.
const DefaultBudgetID = "483d32cb-d741-439c-8d65-cac9fedb85e0"

const callTimeout = 60 * time.Second

// Client is YNAB over HTTP.
type Client struct {
	// URL is the API address, no trailing slash; DefaultURL when empty.
	URL string
	// Token is the personal access token.
	Token string
	// HTTP makes the calls; a client bounded by callTimeout when nil.
	HTTP *http.Client
}

var _ application.YNABBudget = Client{}

// Account is one YNAB account; Balance is in milliunits.
type Account struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Balance int64  `json:"balance"`
	Closed  bool   `json:"closed"`
	Deleted bool   `json:"deleted"`
}

// Transaction is one YNAB transaction; Amount is in milliunits.
type Transaction struct {
	ID           string `json:"id"`
	Date         string `json:"date"`
	Amount       int64  `json:"amount"`
	Memo         string `json:"memo"`
	PayeeName    string `json:"payee_name"`
	CategoryName string `json:"category_name"`
	AccountName  string `json:"account_name"`
	Cleared      string `json:"cleared"`
	Deleted      bool   `json:"deleted"`
}

// Error is YNAB refusing a call.
type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string {
	return fmt.Sprintf("YNAB answered HTTP %d: %s", e.Status, e.Detail)
}

func (c Client) base() string {
	if c.URL == "" {
		return DefaultURL
	}
	return strings.TrimRight(c.URL, "/")
}

func (c Client) call(ctx context.Context, method, path string, body, into any) error {
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("the YNAB token kept is empty: put it again with mw secrets put %s", TokenName)
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: callTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // the request's address, not the token, but no reason to carry it
		}
		return fmt.Errorf("calling YNAB: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("reading YNAB's answer: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		var refused struct {
			Error struct {
				Detail string `json:"detail"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &refused)
		detail := strings.Join(strings.Fields(refused.Error.Detail), " ")
		if len(detail) > 200 {
			detail = detail[:200] + "…"
		}
		if resp.StatusCode == http.StatusUnauthorized {
			detail += " (is the token current? mw secrets put " + TokenName + ")"
		}
		return &Error{Status: resp.StatusCode, Detail: detail}
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("YNAB's answer was not the JSON expected: %w", err)
	}
	return nil
}

func budgetPath(budget, suffix string) string {
	return "/budgets/" + url.PathEscape(budget) + suffix
}

// Accounts are the budget's accounts, deleted ones left out.
func (c Client) Accounts(ctx context.Context, budget string) ([]Account, error) {
	var out struct {
		Data struct {
			Accounts []Account `json:"accounts"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, budgetPath(budget, "/accounts"), nil, &out); err != nil {
		return nil, err
	}
	var accounts []Account
	for _, a := range out.Data.Accounts {
		if !a.Deleted {
			accounts = append(accounts, a)
		}
	}
	return accounts, nil
}

// Categories implements application.YNABBudget: the budget's categories with
// their group, deleted ones and deleted groups left out.
func (c Client) Categories(ctx context.Context, budget string) ([]application.YNABCategory, error) {
	var out struct {
		Data struct {
			Groups []struct {
				Name       string `json:"name"`
				Hidden     bool   `json:"hidden"`
				Deleted    bool   `json:"deleted"`
				Categories []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					Hidden     bool   `json:"hidden"`
					Deleted    bool   `json:"deleted"`
					GoalType   string `json:"goal_type"`
					GoalTarget int64  `json:"goal_target"`
					Balance    int64  `json:"balance"`
				} `json:"categories"`
			} `json:"category_groups"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, budgetPath(budget, "/categories"), nil, &out); err != nil {
		return nil, err
	}
	var cats []application.YNABCategory
	for _, g := range out.Data.Groups {
		if g.Deleted {
			continue
		}
		for _, k := range g.Categories {
			if k.Deleted {
				continue
			}
			cats = append(cats, application.YNABCategory{
				ID: k.ID, Group: g.Name, Name: k.Name,
				GoalTarget: k.GoalTarget, HasTarget: k.GoalType != "",
				Balance: k.Balance, Hidden: k.Hidden || g.Hidden,
			})
		}
	}
	return cats, nil
}

// Transactions are the budget's transactions on or after since (a YYYY-MM-DD
// date; all of them when empty), deleted ones left out.
func (c Client) Transactions(ctx context.Context, budget, since string) ([]Transaction, error) {
	suffix := "/transactions"
	if since != "" {
		suffix += "?since_date=" + url.QueryEscape(since)
	}
	var out struct {
		Data struct {
			Transactions []Transaction `json:"transactions"`
		} `json:"data"`
	}
	if err := c.call(ctx, http.MethodGet, budgetPath(budget, suffix), nil, &out); err != nil {
		return nil, err
	}
	var txs []Transaction
	for _, t := range out.Data.Transactions {
		if !t.Deleted {
			txs = append(txs, t)
		}
	}
	return txs, nil
}

// SetGoalTarget implements application.YNABBudget: it sets one category's
// goal target, in milliunits.
func (c Client) SetGoalTarget(ctx context.Context, budget, category string, milliunits int64) error {
	body := map[string]any{"category": map[string]any{"goal_target": milliunits}}
	return c.call(ctx, http.MethodPatch, budgetPath(budget, "/categories/"+url.PathEscape(category)), body, nil)
}

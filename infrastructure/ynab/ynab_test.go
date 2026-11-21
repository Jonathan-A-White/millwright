package ynab_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/ynab"
)

const token = "ynab-token-SECRET"

type seen struct {
	method, uri, auth, body string
}

// standIn is YNAB for the tests: it records each call and answers as the API does.
func standIn(t *testing.T) (ynab.Client, *[]seen) {
	t.Helper()
	var mu sync.Mutex
	var calls []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, seen{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), string(body)})
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"id":"401","name":"unauthorized","detail":"Unauthorized"}}`))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/budgets/b1/accounts":
			_, _ = w.Write([]byte(`{"data":{"accounts":[{"id":"a1","name":"Checking","type":"checking","balance":1234560,"closed":false},{"id":"a2","name":"Gone","deleted":true}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/budgets/b1/categories":
			_, _ = w.Write([]byte(`{"data":{"category_groups":[
				{"name":"Bills","hidden":false,"deleted":false,"categories":[
					{"id":"c1","name":"Utilities","goal_type":"NEED","goal_target":1009000,"balance":5000},
					{"id":"c2","name":"Groceries","goal_type":null,"goal_target":0},
					{"id":"c3","name":"Removed","deleted":true}]},
				{"name":"Old","hidden":true,"categories":[{"id":"c4","name":"Stale","goal_type":"MF","goal_target":1000}]},
				{"name":"Deleted group","deleted":true,"categories":[{"id":"c5","name":"Never"}]}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/budgets/b1/transactions":
			_, _ = w.Write([]byte(`{"data":{"transactions":[{"id":"t1","date":"2026-11-02","amount":-45670,"payee_name":"Cafe","category_name":"Eating Out","account_name":"Checking","memo":"lunch"},{"id":"t2","deleted":true}]}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/budgets/b1/categories/c1":
			_, _ = w.Write([]byte(`{"data":{"category":{"id":"c1"}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"id":"404.2","name":"resource_not_found","detail":"Resource not found"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return ynab.Client{URL: srv.URL + "/", Token: token}, &calls
}

func TestAccountsAreReadWithTheTokenInTheHeaderAndDeletedOnesLeftOut(t *testing.T) {
	c, calls := standIn(t)
	accounts, err := c.Accounts(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].Name != "Checking" || accounts[0].Balance != 1234560 {
		t.Fatalf("accounts = %+v", accounts)
	}
	if (*calls)[0].auth != "Bearer "+token || strings.Contains((*calls)[0].uri, token) {
		t.Fatalf("call = %+v", (*calls)[0])
	}
}

func TestCategoriesAreFlattenedWithTheirGroupAndTarget(t *testing.T) {
	c, _ := standIn(t)
	cats, err := c.Categories(context.Background(), "b1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 3 {
		t.Fatalf("cats = %+v", cats)
	}
	u, g, s := cats[0], cats[1], cats[2]
	if u.Name != "Utilities" || u.Group != "Bills" || !u.HasTarget || u.GoalTarget != 1009000 || u.Balance != 5000 {
		t.Errorf("utilities = %+v", u)
	}
	if g.HasTarget {
		t.Errorf("a category with no goal has a target: %+v", g)
	}
	if !s.Hidden {
		t.Errorf("a category of a hidden group is not hidden: %+v", s)
	}
}

func TestTransactionsAskForTheDateAndLeaveDeletedOnesOut(t *testing.T) {
	c, calls := standIn(t)
	txs, err := c.Transactions(context.Background(), "b1", "2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 1 || txs[0].Amount != -45670 || txs[0].PayeeName != "Cafe" {
		t.Fatalf("txs = %+v", txs)
	}
	if got := (*calls)[0].uri; got != "/budgets/b1/transactions?since_date=2026-11-01" {
		t.Errorf("asked %q", got)
	}
	if _, err := c.Transactions(context.Background(), "b1", ""); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[1].uri; got != "/budgets/b1/transactions" {
		t.Errorf("with no date asked %q", got)
	}
}

func TestSetGoalTargetPatchesTheMilliunits(t *testing.T) {
	c, calls := standIn(t)
	if err := c.SetGoalTarget(context.Background(), "b1", "c1", 239000); err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if got.method != http.MethodPatch || got.uri != "/budgets/b1/categories/c1" {
		t.Fatalf("call = %+v", got)
	}
	var body struct {
		Category map[string]int64 `json:"category"`
	}
	if err := json.Unmarshal([]byte(got.body), &body); err != nil || len(body.Category) != 1 || body.Category["goal_target"] != 239000 {
		t.Errorf("body = %s (%v)", got.body, err)
	}
}

func TestARefusalIsSaidWithYNABsWordsAndNeverTheToken(t *testing.T) {
	c, _ := standIn(t)
	_, err := c.Accounts(context.Background(), "nobudget")
	var refused *ynab.Error
	if !errors.As(err, &refused) || refused.Status != 404 || !strings.Contains(err.Error(), "Resource not found") {
		t.Fatalf("err = %v", err)
	}

	c.Token = "wrong-token-SECRET"
	err = c.SetGoalTarget(context.Background(), "b1", "c1", 1)
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "wrong-token-SECRET") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnEmptyTokenIsRefusedBeforeAnyCall(t *testing.T) {
	c, calls := standIn(t)
	c.Token = "  \n"
	if _, err := c.Accounts(context.Background(), "b1"); err == nil || !strings.Contains(err.Error(), "mw secrets put ynab_token") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("called YNAB: %+v", *calls)
	}
}

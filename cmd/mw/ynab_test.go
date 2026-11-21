package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

const ynabTestToken = "ynab-cmd-token-SECRET"

// ynabCalls is what the stand-in YNAB was asked.
type ynabCalls struct {
	mu   sync.Mutex
	list []string
}

func (c *ynabCalls) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, s)
}

func (c *ynabCalls) patches() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, s := range c.list {
		if strings.HasPrefix(s, "PATCH ") {
			out = append(out, s)
		}
	}
	return out
}

// standInYnab is YNAB with the default budget; the token comes from a stand-in
// store of secrets. Categories change as patched.
func standInYnab(t *testing.T) *ynabCalls {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MW_YNAB_BUDGET_ID", "")
	t.Setenv("MW_BUDGET_SHEET_ID", "")
	t.Setenv("MW_BUDGET_SHEET_TAB", "")
	calls := &ynabCalls{}
	var mu sync.Mutex
	targets := map[string]string{"pm": "102000", "ut": "1009000", "rent": "2000000"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.add(r.Method + " " + r.URL.RequestURI() + " " + string(body))
		if r.Header.Get("Authorization") != "Bearer "+ynabTestToken {
			http.Error(w, `{"error":{"detail":"Unauthorized"}}`, http.StatusUnauthorized)
			return
		}
		const budget = "/budgets/483d32cb-d741-439c-8d65-cac9fedb85e0"
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == budget+"/accounts":
			_, _ = w.Write([]byte(`{"data":{"accounts":[{"id":"a1","name":"Checking","type":"checking","balance":1234560}]}}`))
		case r.URL.Path == budget+"/categories" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"data":{"category_groups":[{"name":"Bills","categories":[
				{"id":"pm","name":"Personal Maintenance","goal_type":"NEED","goal_target":` + targets["pm"] + `},
				{"id":"ut","name":"Utilities","goal_type":"NEED","goal_target":` + targets["ut"] + `},
				{"id":"rent","name":"Rent","goal_type":"NEED","goal_target":` + targets["rent"] + `}]}]}}`))
		case strings.HasPrefix(r.URL.Path, budget+"/categories/") && r.Method == http.MethodPatch:
			id := strings.TrimPrefix(r.URL.Path, budget+"/categories/")
			if strings.Contains(string(body), `"goal_target":239000`) {
				targets[id] = "239000"
			} else if strings.Contains(string(body), `"goal_target":1215000`) {
				targets[id] = "1215000"
			}
			_, _ = w.Write([]byte(`{"data":{"category":{"id":"` + id + `"}}}`))
		case r.URL.Path == budget+"/transactions":
			_, _ = w.Write([]byte(`{"data":{"transactions":[{"date":"2026-11-02","amount":-45670,"payee_name":"Cafe","category_name":"Eating Out","account_name":"Checking","memo":"lunch"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	oldURL, oldToken := ynabURL, ynabToken
	ynabURL = srv.URL
	ynabToken = func(context.Context) (string, error) { return ynabTestToken + "\n", nil }
	t.Cleanup(func() { ynabURL, ynabToken = oldURL, oldToken })
	return calls
}

func runYnab(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

type sheetStandIn struct{ rows [][]string }

func (s sheetStandIn) Read(_ context.Context, id, tab, _ string) ([][]string, error) {
	if id != "1JLk_cznrn9iIqi4wsr2XQrx30lN2ny7O7Zu4LssIKBI" || tab != "2025" {
		return nil, errors.New("the stand-in sheet was asked for " + id + " / " + tab)
	}
	return s.rows, nil
}

func standInBudgetSheet(t *testing.T, rows [][]string) {
	t.Helper()
	old := budgetSheet
	budgetSheet = func(context.Context) (application.BudgetSheet, error) { return sheetStandIn{rows}, nil }
	t.Cleanup(func() { budgetSheet = old })
}

func TestYnabAccountsCategoriesAndTransactionsPrintTheRowsAndNeverTheToken(t *testing.T) {
	calls := standInYnab(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"ynab", "accounts"}, "a1\tChecking\tchecking\t1234.56"},
		{[]string{"ynab", "categories"}, "ut\tBills\tUtilities\t1009.00"},
		{[]string{"ynab", "transactions", "--since", "2026-11-01"}, "2026-11-02\t-45.67\tCafe\tEating Out\tChecking\tlunch"},
	} {
		out, err := runYnab(t, tc.args...)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Errorf("mw %v: %v\n%s", tc.args, err, out)
		}
		if strings.Contains(out, ynabTestToken) {
			t.Errorf("mw %v printed the token", tc.args)
		}
	}
	found := false
	for _, c := range calls.list {
		found = found || strings.Contains(c, "transactions?since_date=2026-11-01")
	}
	if !found {
		t.Errorf("--since was not sent: %v", calls.list)
	}
}

func TestYnabTransactionsRefuseASinceThatIsNotADate(t *testing.T) {
	standInYnab(t)
	if _, err := runYnab(t, "ynab", "transactions", "--since", "last week"); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("err = %v", err)
	}
}

func TestYnabWithNoTokenSaysHowToPutOne(t *testing.T) {
	standInYnab(t)
	ynabToken = func(context.Context) (string, error) {
		return "", errors.New("no YNAB token is kept: put it with mw secrets put ynab_token")
	}
	if _, err := runYnab(t, "ynab", "accounts"); err == nil || !strings.Contains(err.Error(), "mw secrets put ynab_token") {
		t.Fatalf("err = %v", err)
	}
}

func budgetRows() [][]string {
	return [][]string{
		{"Item", "Total Per Month"},
		{"Personal Maintenance", "$239.00"},
		{"Utilities", "#REF!"},
		{"Rent", "$2,000.00"},
	}
}

func TestBudgetSyncDryRunSaysTheChangesAndPatchesNothing(t *testing.T) {
	calls := standInYnab(t)
	standInBudgetSheet(t, budgetRows())
	out, err := runYnab(t, "budget", "sync", "--dry-run")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Personal Maintenance: 102.00 -> 239.00") || !strings.Contains(out, "Utilities: the Total Per Month is #REF!") {
		t.Errorf("output:\n%s", out)
	}
	if p := calls.patches(); len(p) != 0 {
		t.Errorf("a dry run patched %v", p)
	}
}

func TestBudgetSyncPatchesOnlyTheChangedTargetsAndASecondRunChangesNothing(t *testing.T) {
	calls := standInYnab(t)
	standInBudgetSheet(t, budgetRows())
	out, err := runYnab(t, "budget", "sync")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	patches := calls.patches()
	if len(patches) != 1 || !strings.Contains(patches[0], "/categories/pm ") || !strings.Contains(patches[0], `{"category":{"goal_target":239000}}`) {
		t.Fatalf("patches = %v", patches)
	}
	if !strings.Contains(out, "changed Personal Maintenance: 102.00 -> 239.00") {
		t.Errorf("output:\n%s", out)
	}

	again, err := runYnab(t, "budget", "sync")
	if err != nil || !strings.HasPrefix(again, "nothing to change") {
		t.Fatalf("second run: %v\n%s", err, again)
	}
	if n := len(calls.patches()); n != 1 {
		t.Errorf("second run patched: %v", calls.patches())
	}
}

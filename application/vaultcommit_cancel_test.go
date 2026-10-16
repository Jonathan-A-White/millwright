package application_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// A vault file mw has written and not committed is a tracked change every
// host's dispatch refuses on, so the commit that follows a write runs to its
// end even when the caller's context has ended (mw-gq6.502, after mw-gq6.462).
// The fake vault fails a commit asked for on an ended context, as git run
// under that context would.

func endedContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestSecretsPutCommitsEvenWhenTheContextHasEnded(t *testing.T) {
	vault := &apptest.FakeVaultFiles{}
	err := application.SecretsPut{Store: &apptest.FakeSecretStore{}, In: strings.NewReader("tok-123\n"), Out: &bytes.Buffer{}, Vault: vault}.
		Run(endedContext(), "github_token")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if commits := vault.Commits(); len(commits) != 1 || commits[0].Paths[0] != application.SecretsFile {
		t.Fatalf("want one commit of %s, got %+v", application.SecretsFile, commits)
	}
}

// memoryConfig is a ConfigFile held in memory.
type memoryConfig struct {
	path string
	data []byte
	has  bool
}

func (m *memoryConfig) Path() string                  { return m.path }
func (m *memoryConfig) Read() ([]byte, bool, error)   { return m.data, m.has, nil }
func (m *memoryConfig) Backup(string) (string, error) { return m.path + ".bak", nil }
func (m *memoryConfig) Write(data []byte) error       { m.data, m.has = data, true; return nil }

func TestConfigExportBoostCommitsEvenWhenTheContextHasEnded(t *testing.T) {
	vault := &apptest.FakeVaultFiles{}
	var out bytes.Buffer
	err := application.ConfigExportBoost{
		Config: &memoryConfig{path: "/home/.config/mw/config.toml", has: true, data: []byte("[tests]\nmillwright = \"make test\"\n")},
		Target: &memoryConfig{path: "/vault/" + application.BoostConfigFile},
		Vault:  vault,
		Home:   &apptest.FakeHomeFile{Text: "laptop 2026-10-15T17:03:00Z mw@laptop"},
		Host:   "laptop",
		Out:    &out,
	}.Run(endedContext(), false)
	if err != nil {
		t.Fatalf("export-boost: %v\n%s", err, &out)
	}
	if commits := vault.Commits(); len(commits) != 1 || commits[0].Paths[0] != application.BoostConfigFile {
		t.Fatalf("want one commit of %s, got %+v", application.BoostConfigFile, commits)
	}
}

func TestMoveCommitsTheHomeFileEvenWhenTheContextEndsAfterItIsWritten(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, out := newMoveWorld(), &bytes.Buffer{}
	w.afterWrite = cancel

	_ = move(w, out).Run(ctx)

	committed := false
	for _, call := range w.calls {
		committed = committed || call == "git commit home"
	}
	if !committed {
		t.Errorf("the home file was written and not committed; calls: %v\n%s", w.calls, out)
	}
}

// ledgerBook is an InvestmentBook that writes nothing down.
type ledgerBook struct {
	records []application.InvestmentRecord
}

func (b *ledgerBook) Investments(context.Context) ([]application.InvestmentRecord, error) {
	return b.records, nil
}

func (b *ledgerBook) AppendInvestment(_ context.Context, r application.InvestmentRecord) error {
	b.records = append(b.records, r)
	return nil
}

func TestInvestAddAndOutcomeCommitTheLedgerEvenWhenTheContextHasEnded(t *testing.T) {
	vault := &apptest.FakeVaultFiles{}
	invest := application.Invest{Book: &ledgerBook{}, Vault: vault, Actor: "mw@laptop", Out: &bytes.Buffer{},
		Now: func() time.Time { return time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC) }}

	if err := invest.Add(endedContext(), "mw-a.1", "cut merge conflicts", "tokens -10%", "tokens per story", 14); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := invest.Outcome(endedContext(), "mw-a.1", "win", "tokens -12%", "keep going", ""); err != nil {
		t.Fatalf("outcome: %v", err)
	}
	commits := vault.Commits()
	if len(commits) != 2 {
		t.Fatalf("want one commit for each of add and outcome, got %+v", commits)
	}
	for _, c := range commits {
		if len(c.Paths) != 1 || c.Paths[0] != application.InvestmentsFile {
			t.Errorf("a commit holds %v, want only %s", c.Paths, application.InvestmentsFile)
		}
	}
}

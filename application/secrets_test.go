package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func TestSecretsPutKeepsTheValueAndCommitsTheSecretsFileAlone(t *testing.T) {
	store := &apptest.FakeSecretStore{}
	vault := &apptest.FakeVaultFiles{}
	var out bytes.Buffer

	err := application.SecretsPut{Store: store, In: strings.NewReader("tok-123\r\n"), Out: &out, Vault: vault}.
		Run(context.Background(), "github_token")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := string(store.Values["github_token"]); got != "tok-123" {
		t.Fatalf("expected the value without its line ending, got %q", got)
	}
	commits := vault.Commits()
	if len(commits) != 1 || len(commits[0].Paths) != 1 || commits[0].Paths[0] != application.SecretsFile {
		t.Fatalf("expected one commit of %s alone, got %+v", application.SecretsFile, commits)
	}
	if strings.Contains(out.String()+commits[0].Message, "tok-123") {
		t.Fatalf("the value was printed or committed in a message: %q / %q", out.String(), commits[0].Message)
	}
	if !strings.Contains(out.String(), "github_token") {
		t.Fatalf("expected the put to name the secret, got %q", out.String())
	}
}

func TestSecretsPutRefusesWithoutTouchingTheStore(t *testing.T) {
	for _, tc := range []struct {
		name, in, said string
	}{
		{"github_token", "", "empty"},
		{"github_token", "\n", "empty"},
		{"github_token", "\xff\xfe\n", "not text"},
		{"github_token", strings.Repeat("x", application.MaxSecretBytes+1), "over"},
		{"../escape", "v", "name"},
		{"9lives", "v", "name"},
		{"two words", "v", "name"},
		{"sops", "v", "metadata"},
		{"SOPS", "v", "metadata"},
		{"token_unencrypted", "v", "in the clear"},
		{"", "v", "name"},
	} {
		store := &apptest.FakeSecretStore{}
		err := application.SecretsPut{Store: store, In: strings.NewReader(tc.in)}.Run(context.Background(), tc.name)
		if err == nil || !strings.Contains(err.Error(), tc.said) {
			t.Errorf("put %q of %d bytes: expected a refusal saying %q, got %v", tc.name, len(tc.in), tc.said, err)
		}
		if store.Calls != 0 {
			t.Errorf("put %q: the store was called on a refused put", tc.name)
		}
	}
}

func TestSecretsPutTakesAValueOfExactlyTheLimit(t *testing.T) {
	store := &apptest.FakeSecretStore{}
	value := strings.Repeat("x", application.MaxSecretBytes)
	if err := (application.SecretsPut{Store: store, In: strings.NewReader(value)}).Run(context.Background(), "big"); err != nil {
		t.Fatalf("put of %d bytes: %v", len(value), err)
	}
}

func TestSecretsGetRefusesATerminalBeforeTheStoreIsAsked(t *testing.T) {
	store := &apptest.FakeSecretStore{Values: map[string][]byte{"github_token": []byte("tok-123")}}
	var out bytes.Buffer

	err := application.SecretsGet{Store: store, Out: &out, OutIsTerminal: true}.Run(context.Background(), "github_token")
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected a refusal naming the terminal, got %v", err)
	}
	if store.Calls != 0 || out.Len() != 0 {
		t.Fatalf("expected nothing read or written, got %d calls and %q", store.Calls, out.String())
	}
}

func TestSecretsGetWritesTheValueExactly(t *testing.T) {
	store := &apptest.FakeSecretStore{Values: map[string][]byte{"github_token": []byte("tok-123")}}
	var out bytes.Buffer

	if err := (application.SecretsGet{Store: store, Out: &out}).Run(context.Background(), "github_token"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.String() != "tok-123" {
		t.Fatalf("expected exactly the value, no newline, got %q", out.String())
	}
}

func TestSecretsGetOfANameNeverPutSaysSo(t *testing.T) {
	store := &apptest.FakeSecretStore{}
	err := application.SecretsGet{Store: store, Out: &bytes.Buffer{}}.Run(context.Background(), "github_token")
	if err == nil || !strings.Contains(err.Error(), "no secret named github_token") {
		t.Fatalf("expected the get to say no secret has that name, got %v", err)
	}
}

func TestSecretsListPrintsNamesOnly(t *testing.T) {
	store := &apptest.FakeSecretStore{Values: map[string][]byte{"b_token": []byte("vb"), "a_token": []byte("va")}}
	var out bytes.Buffer

	if err := (application.SecretsList{Store: store, Out: &out}).Run(context.Background()); err != nil {
		t.Fatalf("list: %v", err)
	}
	if out.String() != "a_token\nb_token\n" {
		t.Fatalf("expected the names sorted, one a line, got %q", out.String())
	}
}

func TestSecretsListPassesOnTheStoresError(t *testing.T) {
	store := &apptest.FakeSecretStore{Err: errors.New("no age key")}
	if err := (application.SecretsList{Store: store, Out: &bytes.Buffer{}}).Run(context.Background()); err == nil {
		t.Fatal("expected the store's error")
	}
}

package application_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const (
	fakeRefresh = "1//fake-refresh-SECRET-0001"
	fakeAuthURL = "https://accounts.example/auth?client_id=c&code_challenge=x"
)

func atHome(context.Context) (bool, error) { return true, nil }

// mustNotCarry fails when any of what was said carries the refresh token.
func mustNotCarry(t *testing.T, said ...string) {
	t.Helper()
	for _, text := range said {
		if strings.Contains(text, fakeRefresh) || strings.Contains(text, "SECRET") {
			t.Fatalf("the token was shown: %q", text)
		}
	}
}

func TestGoogleLinkAsksForContactsAloneAndKeepsTheGrant(t *testing.T) {
	account := &apptest.FakeGoogleAccount{
		AuthURL: fakeAuthURL,
		Grant:   application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope},
	}
	tokens := &apptest.FakeGoogleTokenFile{}
	var opened []string
	var out bytes.Buffer

	err := application.GoogleLink{
		Account: account, Tokens: tokens, Out: &out, Home: atHome,
		Open: func(url string) error { opened = append(opened, url); return nil },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(account.Scopes) != 1 || account.Scopes[0] != "https://www.googleapis.com/auth/contacts.readonly" {
		t.Fatalf("expected one sign-in for contacts.readonly alone, got %q", account.Scopes)
	}
	if len(opened) != 1 || opened[0] != fakeAuthURL {
		t.Fatalf("expected the browser opened at the sign-in address, got %q", opened)
	}
	if !strings.Contains(out.String(), fakeAuthURL) {
		t.Fatalf("expected the sign-in address printed for a headless home, got %q", out.String())
	}
	if tokens.Kept == nil || tokens.Kept.Refresh != fakeRefresh {
		t.Fatalf("expected the grant kept, got %+v", tokens.Kept)
	}
	if !strings.Contains(out.String(), "linked (contacts.readonly)") {
		t.Fatalf("expected the link said with its scope, got %q", out.String())
	}
	mustNotCarry(t, out.String())
}

func TestGoogleLinkSaysTheAddressWhenTheBrowserDoesNotOpen(t *testing.T) {
	account := &apptest.FakeGoogleAccount{
		AuthURL: fakeAuthURL,
		Grant:   application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope},
	}
	tokens := &apptest.FakeGoogleTokenFile{}
	var out bytes.Buffer

	err := application.GoogleLink{
		Account: account, Tokens: tokens, Out: &out, Home: atHome,
		Open: func(string) error { return errors.New("no xdg-open") },
	}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), fakeAuthURL) || !strings.Contains(out.String(), "no xdg-open") {
		t.Fatalf("expected the address and why the browser did not open, got %q", out.String())
	}
	if tokens.Kept == nil {
		t.Fatal("expected the grant kept all the same")
	}
}

func TestGoogleLinkSaysASecondLinkReplacesTheFirst(t *testing.T) {
	account := &apptest.FakeGoogleAccount{
		AuthURL: fakeAuthURL,
		Grant:   application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope},
	}
	tokens := &apptest.FakeGoogleTokenFile{Kept: &application.GoogleGrant{Refresh: "1//older-SECRET", Scope: application.GoogleContactsScope}}
	var out bytes.Buffer

	err := application.GoogleLink{Account: account, Tokens: tokens, Out: &out, Home: atHome}.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tokens.Kept.Refresh != fakeRefresh {
		t.Fatalf("expected the new grant in place of the old, got %+v", tokens.Kept)
	}
	if !strings.Contains(out.String(), "replac") {
		t.Fatalf("expected the replacement said, got %q", out.String())
	}
	mustNotCarry(t, out.String())
}

func TestGoogleLinkRefusesOffTheHomeBeforeAnySignIn(t *testing.T) {
	account := &apptest.FakeGoogleAccount{AuthURL: fakeAuthURL}
	tokens := &apptest.FakeGoogleTokenFile{}
	var out bytes.Buffer

	err := application.GoogleLink{
		Account: account, Tokens: tokens, Out: &out,
		Home: func(context.Context) (bool, error) { return false, nil },
	}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "home") {
		t.Fatalf("expected a refusal off the home, got %v", err)
	}
	if len(account.Scopes) != 0 || tokens.Saves != 0 {
		t.Fatalf("expected no sign-in and nothing kept, got %q and %d saves", account.Scopes, tokens.Saves)
	}
}

func TestGoogleLinkRevokesAGrantWiderThanContactsAndKeepsNothing(t *testing.T) {
	account := &apptest.FakeGoogleAccount{
		AuthURL: fakeAuthURL,
		Grant: application.GoogleGrant{Refresh: fakeRefresh,
			Scope: application.GoogleContactsScope + " https://www.googleapis.com/auth/gmail.send"},
	}
	tokens := &apptest.FakeGoogleTokenFile{}
	var out bytes.Buffer

	err := application.GoogleLink{Account: account, Tokens: tokens, Out: &out, Home: atHome}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gmail.send") {
		t.Fatalf("expected the wider grant refused by name, got %v", err)
	}
	if tokens.Saves != 0 {
		t.Fatal("expected nothing kept")
	}
	if len(account.Revoked) != 1 || account.Revoked[0] != fakeRefresh {
		t.Fatalf("expected the unwanted grant revoked, got %q", account.Revoked)
	}
	mustNotCarry(t, out.String(), err.Error())
}

func TestGoogleLinkRefusesAGrantWithoutContacts(t *testing.T) {
	account := &apptest.FakeGoogleAccount{
		AuthURL: fakeAuthURL,
		Grant:   application.GoogleGrant{Refresh: fakeRefresh, Scope: ""},
	}
	tokens := &apptest.FakeGoogleTokenFile{}
	var out bytes.Buffer

	err := application.GoogleLink{Account: account, Tokens: tokens, Out: &out, Home: atHome}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "contacts.readonly") {
		t.Fatalf("expected a grant without contacts refused, got %v", err)
	}
	if tokens.Saves != 0 {
		t.Fatal("expected nothing kept")
	}
	mustNotCarry(t, out.String(), err.Error())
}

func TestGoogleStatusSaysLinkedWithTheScopeOrNotLinked(t *testing.T) {
	var out bytes.Buffer
	tokens := &apptest.FakeGoogleTokenFile{}
	if err := (application.GoogleStatus{Tokens: tokens, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "not linked" {
		t.Fatalf("expected 'not linked', got %q", out.String())
	}

	out.Reset()
	tokens.Kept = &application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope}
	if err := (application.GoogleStatus{Tokens: tokens, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "linked (contacts.readonly)" {
		t.Fatalf("expected 'linked (contacts.readonly)', got %q", out.String())
	}
	mustNotCarry(t, out.String())
}

func TestGoogleUnlinkRevokesThenDeletes(t *testing.T) {
	account := &apptest.FakeGoogleAccount{}
	tokens := &apptest.FakeGoogleTokenFile{Kept: &application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope}}
	var out bytes.Buffer

	if err := (application.GoogleUnlink{Account: account, Tokens: tokens, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(account.Revoked) != 1 || account.Revoked[0] != fakeRefresh {
		t.Fatalf("expected the refresh token revoked, got %q", account.Revoked)
	}
	if tokens.Kept != nil {
		t.Fatal("expected the token file deleted")
	}
	if !strings.Contains(out.String(), "unlinked") {
		t.Fatalf("expected 'unlinked' said, got %q", out.String())
	}
	mustNotCarry(t, out.String())
}

func TestGoogleUnlinkWhenNotLinkedSaysSoAndSucceeds(t *testing.T) {
	account := &apptest.FakeGoogleAccount{}
	var out bytes.Buffer

	if err := (application.GoogleUnlink{Account: account, Tokens: &apptest.FakeGoogleTokenFile{}, Out: &out}).Run(context.Background()); err != nil {
		t.Fatalf("expected unlink with nothing linked to succeed, got %v", err)
	}
	if !strings.Contains(out.String(), "not linked") || len(account.Revoked) != 0 {
		t.Fatalf("expected 'not linked' and no revoke, got %q and %q", out.String(), account.Revoked)
	}
}

func TestGoogleUnlinkDeletesATokenGoogleAlreadyHoldsDead(t *testing.T) {
	account := &apptest.FakeGoogleAccount{RevokeErr: fmt.Errorf("invalid_token: %w", application.ErrGoogleTokenDead)}
	tokens := &apptest.FakeGoogleTokenFile{Kept: &application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope}}
	var out bytes.Buffer

	if err := (application.GoogleUnlink{Account: account, Tokens: tokens, Out: &out}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tokens.Kept != nil {
		t.Fatal("expected a dead token's file deleted")
	}
	mustNotCarry(t, out.String())
}

func TestGoogleUnlinkKeepsTheFileWhenGoogleCannotBeReachedAndNeverEchoesTheToken(t *testing.T) {
	// An adapter that slipped the token into its error is scrubbed all the same.
	account := &apptest.FakeGoogleAccount{RevokeErr: errors.New("post https://oauth2.example/revoke token=" + fakeRefresh + ": connection refused")}
	tokens := &apptest.FakeGoogleTokenFile{Kept: &application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope}}
	var out bytes.Buffer

	err := application.GoogleUnlink{Account: account, Tokens: tokens, Out: &out}.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected the revoke's failure said, got %v", err)
	}
	if tokens.Kept == nil {
		t.Fatal("expected the token kept while Google still holds it live")
	}
	mustNotCarry(t, out.String(), err.Error())
}

func TestAGoogleGrantNeverPrintsItsToken(t *testing.T) {
	grant := application.GoogleGrant{Refresh: fakeRefresh, Scope: application.GoogleContactsScope}
	mustNotCarry(t, fmt.Sprint(grant), fmt.Sprintf("%v %+v %#v %s", grant, grant, grant, grant))
}

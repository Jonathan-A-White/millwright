package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// GoogleContactsScope is the one scope mw google link asks for: read his
// contacts, nothing else (the People API's contacts.readonly).
const GoogleContactsScope = "https://www.googleapis.com/auth/contacts.readonly"

// googleScopePrefix is what every Google API scope starts with; a scope is
// said without it.
const googleScopePrefix = "https://www.googleapis.com/auth/"

// GoogleGrant is what one sign-in leaves: the refresh token and the scopes
// Google granted, space separated as Google says them. It is printed only as
// its scopes: String and GoString never show the token, so a grant passed to
// fmt by mistake cannot leak it.
type GoogleGrant struct {
	Refresh string
	Scope   string
}

// String says the grant's scopes and never its token.
func (g GoogleGrant) String() string { return "Google grant (" + GoogleScopeNames(g.Scope) + ")" }

// GoString is String, for %#v.
func (g GoogleGrant) GoString() string { return g.String() }

// GoogleScopeNames is scopes, space separated as Google says them, each
// without the https://www.googleapis.com/auth/ prefix, joined by ", ":
// "contacts.readonly".
func GoogleScopeNames(scopes string) string {
	var names []string
	for _, scope := range strings.Fields(scopes) {
		names = append(names, strings.TrimPrefix(scope, googleScopePrefix))
	}
	return strings.Join(names, ", ")
}

// ErrGoogleNotLinked is what GoogleTokenFile.Load wraps when no grant is kept.
var ErrGoogleNotLinked = errors.New("not linked")

// ErrGoogleTokenDead is what GoogleAccount.Revoke wraps when Google says the
// token is already revoked or expired: there is nothing left to revoke.
var ErrGoogleTokenDead = errors.New("Google holds the token revoked or expired already")

// GoogleAccount is Google's side of the account link: a Desktop OAuth client
// signing in through the browser, and the revoke endpoint. Neither method may
// put a token or an authorization code in an error.
type GoogleAccount interface {
	// SignIn runs one browser sign-in asking for scope alone (PKCE, a loopback
	// redirect on a free port, offline access): it calls show with the address
	// to open once it is listening, waits for the browser to come back, and
	// exchanges the code for the grant.
	SignIn(ctx context.Context, scope string, show func(authURL string)) (GoogleGrant, error)
	// Revoke asks Google to revoke token and the grant it belongs to; an error
	// wrapping ErrGoogleTokenDead when Google says it is dead already.
	Revoke(ctx context.Context, token string) error
}

// GoogleTokenFile is where the grant is kept on this host: one file only its
// owner can read (mode 0600), under ~/.config/mw/, never the vault.
type GoogleTokenFile interface {
	// Load is the grant kept: an error wrapping ErrGoogleNotLinked when none
	// is. An error never quotes the file.
	Load() (GoogleGrant, error)
	// Save keeps grant in place of any kept before, mode 0600.
	Save(grant GoogleGrant) error
	// Remove deletes the file; no error when there is none.
	Remove() error
	// Path is where the file is, for saying so.
	Path() string
}

// GoogleLink is mw google link: on the home only, it signs his Google account
// in once for contacts.readonly alone and keeps the refresh token in the token
// file. A grant of anything more, or of less, is revoked at once and nothing
// is kept. Out is told the sign-in address and the outcome, never a token.
type GoogleLink struct {
	Account GoogleAccount
	Tokens  GoogleTokenFile
	Out     io.Writer
	// Home reports whether this host is the home: the token is never kept on
	// the VPS or any other host.
	Home func(ctx context.Context) (bool, error)
	// Open opens the sign-in address in a browser; nil, or an error, leaves it
	// to him to open the address printed.
	Open func(url string) error
}

// Run signs in and keeps the grant.
func (l GoogleLink) Run(ctx context.Context) error {
	if l.Home == nil {
		return errors.New("mw google link cannot tell whether this host is the home")
	}
	home, err := l.Home(ctx)
	if err != nil {
		return fmt.Errorf("mw google link runs on the home only, and the home cannot be told: %w", err)
	}
	if !home {
		return errors.New("mw google link runs on the home only: the Google token is kept on the home and nowhere else")
	}
	_, earlier := l.Tokens.Load()
	replacing := earlier == nil

	grant, err := l.Account.SignIn(ctx, GoogleContactsScope, l.show)
	if err != nil {
		return fmt.Errorf("the Google sign-in did not finish, nothing is kept: %w", err)
	}
	if grant.Refresh == "" {
		return errors.New("Google gave no refresh token, nothing is kept: remove mw at https://myaccount.google.com/permissions and run mw google link again")
	}
	if scopes := strings.Fields(grant.Scope); len(scopes) != 1 || scopes[0] != GoogleContactsScope {
		return l.refuse(ctx, grant)
	}
	if err := l.Tokens.Save(grant); err != nil {
		return fmt.Errorf("keeping the Google token in %s: %w", l.Tokens.Path(), err)
	}
	if replacing {
		fmt.Fprintf(l.Out, "replaced the earlier Google link: linked (%s); the token is in %s, readable by you alone\n", GoogleScopeNames(grant.Scope), l.Tokens.Path())
		return nil
	}
	fmt.Fprintf(l.Out, "linked (%s); the token is in %s, readable by you alone\n", GoogleScopeNames(grant.Scope), l.Tokens.Path())
	return nil
}

// show prints the sign-in address, then tries the browser.
func (l GoogleLink) show(authURL string) {
	fmt.Fprintf(l.Out, "Sign in to Google in a browser on this machine (mw asks to see your contacts, nothing else):\n\n  %s\n\n", authURL)
	if l.Open == nil {
		return
	}
	if err := l.Open(authURL); err != nil {
		fmt.Fprintf(l.Out, "(the browser did not open: %v; open the address above by hand)\n", err)
	}
}

// refuse revokes a grant whose scopes are not contacts.readonly alone and
// says why nothing is kept.
func (l GoogleLink) refuse(ctx context.Context, grant GoogleGrant) error {
	said := GoogleScopeNames(grant.Scope)
	if said == "" {
		said = "no scope"
	}
	why := fmt.Sprintf("Google granted %s, not %s alone", said, GoogleScopeNames(GoogleContactsScope))
	if err := l.Account.Revoke(ctx, grant.Refresh); err != nil && !errors.Is(err, ErrGoogleTokenDead) {
		return fmt.Errorf("%s; nothing is kept, and revoking that grant failed (%v): remove mw at https://myaccount.google.com/permissions", why, scrubbed(err, grant.Refresh))
	}
	return fmt.Errorf("%s; that grant is revoked and nothing is kept: run mw google link again and allow contacts", why)
}

// GoogleStatus is mw google status: "linked (<scopes>)" or "not linked",
// never the token.
type GoogleStatus struct {
	Tokens GoogleTokenFile
	Out    io.Writer
}

// Run says whether a grant is kept.
func (s GoogleStatus) Run(_ context.Context) error {
	grant, err := s.Tokens.Load()
	if errors.Is(err, ErrGoogleNotLinked) {
		fmt.Fprintln(s.Out, "not linked")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Out, "linked (%s)\n", GoogleScopeNames(grant.Scope))
	return nil
}

// GoogleUnlink is mw google unlink: it asks Google to revoke the kept token,
// then deletes the token file. While Google cannot be reached the file is
// kept, so the token is never lost here while still live there. Nothing kept
// is no error.
type GoogleUnlink struct {
	Account GoogleAccount
	Tokens  GoogleTokenFile
	Out     io.Writer
}

// Run revokes and deletes.
func (u GoogleUnlink) Run(ctx context.Context) error {
	grant, err := u.Tokens.Load()
	if errors.Is(err, ErrGoogleNotLinked) {
		fmt.Fprintln(u.Out, "not linked: nothing to revoke")
		return nil
	}
	if err != nil {
		return err
	}
	revoked := u.Account.Revoke(ctx, grant.Refresh)
	if revoked != nil && !errors.Is(revoked, ErrGoogleTokenDead) {
		return fmt.Errorf("Google did not revoke the token, so %s is kept: %v; run mw google unlink again, or remove mw at https://myaccount.google.com/permissions and delete the file",
			u.Tokens.Path(), scrubbed(revoked, grant.Refresh))
	}
	if err := u.Tokens.Remove(); err != nil {
		return fmt.Errorf("Google revoked the token, but deleting %s failed: %w", u.Tokens.Path(), err)
	}
	if revoked != nil {
		fmt.Fprintf(u.Out, "unlinked: Google held the token revoked or expired already; %s is deleted\n", u.Tokens.Path())
		return nil
	}
	fmt.Fprintf(u.Out, "unlinked: Google revoked the token and %s is deleted\n", u.Tokens.Path())
	return nil
}

// scrubbed is err's text with every secret in it replaced: the last guard
// before an adapter's error is shown.
func scrubbed(err error, secrets ...string) string {
	text := err.Error()
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[token]")
		}
	}
	return text
}

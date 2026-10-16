package apptest

import (
	"context"

	"github.com/Jonathan-A-White/millwright/application"
)

var (
	_ application.GoogleAccount   = (*FakeGoogleAccount)(nil)
	_ application.GoogleTokenFile = (*FakeGoogleTokenFile)(nil)
)

// FakeGoogleAccount is Google's side of the link in memory: SignIn shows
// AuthURL and hands back Grant (or SignInErr); Revoke records each token it is
// given and answers RevokeErr.
type FakeGoogleAccount struct {
	AuthURL   string
	Grant     application.GoogleGrant
	SignInErr error
	RevokeErr error

	// Scopes is every scope SignIn was asked for, in order.
	Scopes []string
	// Revoked is every token Revoke was given, in order.
	Revoked []string
}

// SignIn implements application.GoogleAccount.
func (f *FakeGoogleAccount) SignIn(_ context.Context, scope string, show func(authURL string)) (application.GoogleGrant, error) {
	f.Scopes = append(f.Scopes, scope)
	show(f.AuthURL)
	if f.SignInErr != nil {
		return application.GoogleGrant{}, f.SignInErr
	}
	return f.Grant, nil
}

// Revoke implements application.GoogleAccount.
func (f *FakeGoogleAccount) Revoke(_ context.Context, token string) error {
	f.Revoked = append(f.Revoked, token)
	return f.RevokeErr
}

// FakeGoogleTokenFile keeps the grant in memory. Kept is nil when nothing is
// linked; Saves counts each Save.
type FakeGoogleTokenFile struct {
	Kept  *application.GoogleGrant
	Saves int
	Where string
}

// Load implements application.GoogleTokenFile.
func (f *FakeGoogleTokenFile) Load() (application.GoogleGrant, error) {
	if f.Kept == nil {
		return application.GoogleGrant{}, application.ErrGoogleNotLinked
	}
	return *f.Kept, nil
}

// Save implements application.GoogleTokenFile.
func (f *FakeGoogleTokenFile) Save(grant application.GoogleGrant) error {
	f.Saves++
	f.Kept = &grant
	return nil
}

// Remove implements application.GoogleTokenFile.
func (f *FakeGoogleTokenFile) Remove() error {
	f.Kept = nil
	return nil
}

// Path implements application.GoogleTokenFile.
func (f *FakeGoogleTokenFile) Path() string {
	if f.Where == "" {
		return "/home/him/.config/mw/google-token.json"
	}
	return f.Where
}

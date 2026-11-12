// Package gdrive is the home's read-write look into his Google Drive and
// Sheets, signed in as him: a Desktop OAuth client, a consent URL he opens in
// any browser and pastes the localhost address back from, Drive v3 file search
// and Sheets v4 cell reads and writes. Nothing is mirrored; a file is fetched
// only when a command names it. No token, secret or authorization code is ever
// put in an error or anything printed.
package gdrive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/atomicfile"
)

// Client is a Desktop OAuth client. Google says a Desktop client's secret is
// not secret; it is still kept at mode 0600 and never printed.
type Client struct {
	ID     string
	Secret string
}

// Token is what the token endpoint handed out, kept between runs.
type Token struct {
	Access  string    `json:"access_token,omitempty"`
	Refresh string    `json:"refresh_token"`
	Expiry  time.Time `json:"expiry"`
	Scope   string    `json:"scope,omitempty"`
	Type    string    `json:"token_type,omitempty"`
}

// ErrNotSignedIn is wrapped by every error that says mw gdrive auth has not
// been run (or its token is dead).
var ErrNotSignedIn = errors.New("not signed in to Google Drive")

// Files is where the sign-in is kept: Dir holds client.json and token.json,
// the directory made 0700 and each file 0600.
type Files struct {
	Dir string
}

// ClientPath is the client copy gdrive keeps beside its token.
func (f Files) ClientPath() string { return filepath.Join(f.Dir, "client.json") }

// TokenPath is the token file.
func (f Files) TokenPath() string { return filepath.Join(f.Dir, "token.json") }

func (f Files) ensureDir() error {
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(f.Dir, 0o700)
}

// SaveClient writes the client in Google's Desktop JSON.
func (f Files) SaveClient(c Client) error {
	if err := f.ensureDir(); err != nil {
		return err
	}
	return writeClient(f.ClientPath(), c)
}

// LoadClient reads the client copy.
func (f Files) LoadClient() (Client, error) { return ReadClientFile(f.ClientPath()) }

// SaveToken writes the token, replacing the old one whole.
func (f Files) SaveToken(t Token) error {
	if err := f.ensureDir(); err != nil {
		return err
	}
	body, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return atomicfile.Write(f.TokenPath(), append(body, '\n'), 0o600)
}

// LoadToken reads the token; ErrNotSignedIn when there is none.
func (f Files) LoadToken() (Token, error) {
	body, err := os.ReadFile(f.TokenPath())
	if errors.Is(err, os.ErrNotExist) {
		return Token{}, fmt.Errorf("%w: run mw gdrive auth", ErrNotSignedIn)
	}
	if err != nil {
		return Token{}, fmt.Errorf("reading the Drive token file: %w", err)
	}
	var t Token
	if err := json.Unmarshal(body, &t); err != nil || t.Refresh == "" {
		return Token{}, fmt.Errorf("%w: the token file %s is not one mw gdrive auth wrote; run mw gdrive revoke, then mw gdrive auth", ErrNotSignedIn, f.TokenPath())
	}
	return t, nil
}

// Remove deletes the client copy and the token and, when it is then empty, the
// directory. It returns the paths it deleted; nothing there is no error.
func (f Files) Remove() ([]string, error) {
	var gone []string
	for _, path := range []string{f.TokenPath(), f.ClientPath()} {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return gone, err
		}
		gone = append(gone, path)
	}
	// A directory with something else in it stays; that is not a failure.
	_ = os.Remove(f.Dir)
	return gone, nil
}

// desktopJSON is Google's Desktop client file.
type desktopJSON struct {
	Installed *desktopClient `json:"installed,omitempty"`
	Web       *desktopClient `json:"web,omitempty"`
}

type desktopClient struct {
	ID           string   `json:"client_id"`
	Secret       string   `json:"client_secret,omitempty"`
	AuthURI      string   `json:"auth_uri,omitempty"`
	TokenURI     string   `json:"token_uri,omitempty"`
	RedirectURIs []string `json:"redirect_uris,omitempty"`
}

// WriteClientFile writes c at path in Google's Desktop JSON, mode 0600, making
// the directory 0700 when it is missing. An existing file is replaced.
func WriteClientFile(path string, c Client) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeClient(path, c)
}

func writeClient(path string, c Client) error {
	body, err := json.MarshalIndent(desktopJSON{Installed: &desktopClient{
		ID: c.ID, Secret: c.Secret,
		AuthURI:      "https://accounts.google.com/o/oauth2/auth",
		TokenURI:     "https://oauth2.googleapis.com/token",
		RedirectURIs: []string{"http://localhost"},
	}}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(body, '\n'), 0o600)
}

// ReadClientFile reads a Desktop client's JSON as the Google Cloud console
// downloads it. A missing file is an error wrapping os.ErrNotExist.
func ReadClientFile(path string) (Client, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Client{}, fmt.Errorf("reading the Google OAuth client %s: %w", path, err)
	}
	var file desktopJSON
	if err := json.Unmarshal(body, &file); err != nil {
		return Client{}, fmt.Errorf("the Google OAuth client %s is not the JSON the Google Cloud console downloads", path)
	}
	if file.Installed == nil && file.Web != nil {
		return Client{}, fmt.Errorf("the Google OAuth client %s is a Web application client: make a Desktop app client instead", path)
	}
	if file.Installed == nil || file.Installed.ID == "" {
		return Client{}, fmt.Errorf("the Google OAuth client %s holds no Desktop app client_id", path)
	}
	return Client{ID: file.Installed.ID, Secret: file.Installed.Secret}, nil
}

// Package google is the home's link to his Google account: a Desktop OAuth
// client signing in once through the browser (PKCE, a loopback redirect on a
// free port), Google's revoke endpoint, and the file the refresh token is kept
// in, readable by its owner alone. No token or authorization code is ever put
// in an error, a log line or anything printed.
package google

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Endpoints are where Google's OAuth lives.
type Endpoints struct {
	Auth   string
	Token  string
	Revoke string
}

// GoogleEndpoints are Google's own, the ones every real sign-in uses.
var GoogleEndpoints = Endpoints{
	Auth:   "https://accounts.google.com/o/oauth2/v2/auth",
	Token:  "https://oauth2.googleapis.com/token",
	Revoke: "https://oauth2.googleapis.com/revoke",
}

// DefaultWait is how long SignIn waits for the browser to come back.
const DefaultWait = 5 * time.Minute

// callTimeout bounds each call to the token and revoke endpoints.
const callTimeout = 30 * time.Second

var _ application.GoogleAccount = OAuth{}

// OAuth is application.GoogleAccount against Google: the Desktop client whose
// JSON (as the Google Cloud console downloads it) is at ClientFile.
type OAuth struct {
	ClientFile string
	Endpoints  Endpoints
	// Wait is how long SignIn waits for the browser; DefaultWait when zero.
	Wait time.Duration
	// HTTP calls the token and revoke endpoints; a client bounded by
	// callTimeout when nil.
	HTTP *http.Client
}

// desktopClient is the "installed" half of a Desktop client's JSON. Google
// says a Desktop client's secret is not secret: PKCE is what guards the code.
type desktopClient struct {
	ID     string `json:"client_id"`
	Secret string `json:"client_secret"`
}

func (o OAuth) client() (desktopClient, error) {
	body, err := os.ReadFile(o.ClientFile)
	if errors.Is(err, os.ErrNotExist) {
		return desktopClient{}, fmt.Errorf("there is no Google OAuth client at %s (google_client_file): make a Desktop app client in the Google Cloud console and save its JSON there", o.ClientFile)
	}
	if err != nil {
		return desktopClient{}, fmt.Errorf("reading the Google OAuth client %s (google_client_file): %w", o.ClientFile, err)
	}
	var file struct {
		Installed *desktopClient `json:"installed"`
		Web       *desktopClient `json:"web"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return desktopClient{}, fmt.Errorf("the Google OAuth client %s (google_client_file) is not the JSON the Google Cloud console downloads", o.ClientFile)
	}
	if file.Installed == nil && file.Web != nil {
		return desktopClient{}, fmt.Errorf("the Google OAuth client %s (google_client_file) is a Web application client: make a Desktop app client instead", o.ClientFile)
	}
	if file.Installed == nil || file.Installed.ID == "" {
		return desktopClient{}, fmt.Errorf("the Google OAuth client %s (google_client_file) holds no Desktop app client_id", o.ClientFile)
	}
	return *file.Installed, nil
}

func (o OAuth) http() *http.Client {
	if o.HTTP != nil {
		return o.HTTP
	}
	return &http.Client{Timeout: callTimeout}
}

// randomWord is n random bytes, base64url without padding.
func randomWord(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("no randomness for the sign-in: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// callback is what the browser brought back to the loopback redirect.
type callback struct {
	code string
	err  string
}

// SignIn implements application.GoogleAccount.
func (o OAuth) SignIn(ctx context.Context, scope string, show func(authURL string)) (application.GoogleGrant, error) {
	client, err := o.client()
	if err != nil {
		return application.GoogleGrant{}, err
	}
	verifier, err := randomWord(32) // 43 characters, the least RFC 7636 allows
	if err != nil {
		return application.GoogleGrant{}, err
	}
	state, err := randomWord(24)
	if err != nil {
		return application.GoogleGrant{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return application.GoogleGrant{}, fmt.Errorf("no free loopback port for the sign-in to come back to: %w", err)
	}
	redirect := fmt.Sprintf("http://127.0.0.1:%d/", listener.Addr().(*net.TCPAddr).Port)

	came := make(chan callback, 1)
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if r.URL.Path != "/" || q.Get("state") != state {
				http.Error(w, "This is not the sign-in mw is waiting for.", http.StatusBadRequest)
				return
			}
			back := callback{code: q.Get("code"), err: q.Get("error")}
			if back.code == "" && back.err == "" {
				back.err = "no code"
			}
			if back.err != "" {
				_, _ = io.WriteString(w, "mw was not linked: "+oneWord(back.err)+". You can close this tab.\n")
			} else {
				_, _ = io.WriteString(w, "Signed in. mw is finishing the link in the terminal; you can close this tab.\n")
			}
			select {
			case came <- back:
			default:
			}
		}),
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	auth := url.Values{
		"client_id":             {client.ID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {scope},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	show(o.Endpoints.Auth + "?" + auth.Encode())

	wait := o.Wait
	if wait <= 0 {
		wait = DefaultWait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	var back callback
	select {
	case back = <-came:
	case <-timer.C:
		return application.GoogleGrant{}, fmt.Errorf("no sign-in came back within %s", wait)
	case <-ctx.Done():
		return application.GoogleGrant{}, ctx.Err()
	}
	if back.err != "" {
		return application.GoogleGrant{}, fmt.Errorf("Google said %s", oneWord(back.err))
	}
	return o.exchange(ctx, client, back.code, verifier, redirect)
}

// tokenAnswer is the token endpoint's answer, success or refusal.
type tokenAnswer struct {
	Refresh     string `json:"refresh_token"`
	Access      string `json:"access_token"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// exchange trades the code, with the PKCE verifier, for the grant. The access
// token in the answer is dropped: the grant keeps the refresh token alone.
func (o OAuth) exchange(ctx context.Context, client desktopClient, code, verifier, redirect string) (application.GoogleGrant, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirect},
		"client_id":     {client.ID},
	}
	if client.Secret != "" {
		form.Set("client_secret", client.Secret)
	}
	var answer tokenAnswer
	status, err := o.post(ctx, o.Endpoints.Token, form, &answer)
	if err != nil {
		return application.GoogleGrant{}, fmt.Errorf("calling Google's token endpoint: %w", err)
	}
	if status != http.StatusOK || answer.Error != "" {
		return application.GoogleGrant{}, fmt.Errorf("Google's token endpoint refused the code (HTTP %d): %s", status, refusal(answer.Error, answer.Description))
	}
	return application.GoogleGrant{Refresh: answer.Refresh, Scope: answer.Scope}, nil
}

// Revoke implements application.GoogleAccount: the token goes in the POST
// body, never the address, so no proxy or log line sees it.
func (o OAuth) Revoke(ctx context.Context, token string) error {
	var answer tokenAnswer
	status, err := o.post(ctx, o.Endpoints.Revoke, url.Values{"token": {token}}, &answer)
	if err != nil {
		return fmt.Errorf("calling Google's revoke endpoint: %w", err)
	}
	if status == http.StatusOK {
		return nil
	}
	if status == http.StatusBadRequest && answer.Error == "invalid_token" {
		return fmt.Errorf("%w (invalid_token)", application.ErrGoogleTokenDead)
	}
	return fmt.Errorf("Google's revoke endpoint answered HTTP %d: %s", status, oneWord(answer.Error))
}

// post sends form and reads a JSON answer into into, whatever the status; a
// body that is not JSON leaves into empty. The error is the transport's alone,
// and names no part of form.
func (o OAuth) post(ctx context.Context, endpoint string, form url.Values, into *tokenAnswer) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := o.http().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return resp.StatusCode, err
	}
	_ = json.Unmarshal(body, into)
	return resp.StatusCode, nil
}

// refusal is an OAuth error and its description, said briefly: the error is
// one of OAuth's own words, and the description is cut short.
func refusal(code, description string) string {
	said := oneWord(code)
	description = strings.Join(strings.Fields(description), " ")
	if len(description) > 120 {
		description = description[:120] + "…"
	}
	if description != "" {
		said += " (" + description + ")"
	}
	return said
}

// oneWord is an OAuth error code as it may be shown: letters, digits and
// underscores only, "an unknown error" when none is left.
func oneWord(code string) string {
	var b strings.Builder
	for _, r := range code {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() >= 40 {
			break
		}
	}
	if b.Len() == 0 {
		return "an unknown error"
	}
	return b.String()
}

// OpenBrowser opens address in this machine's browser without waiting for it;
// an error when no opener is found here.
func OpenBrowser(address string) error {
	var openers [][]string
	switch runtime.GOOS {
	case "darwin":
		openers = [][]string{{"open"}}
	case "windows":
		openers = [][]string{{"rundll32", "url.dll,FileProtocolHandler"}}
	default:
		openers = [][]string{{"xdg-open"}, {"wslview"}}
	}
	for _, opener := range openers {
		path, err := exec.LookPath(opener[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, append(opener[1:], address)...)
		if err := cmd.Start(); err != nil {
			continue
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return errors.New("no browser opener (xdg-open, wslview) on this machine")
}

var _ application.GoogleTokenFile = TokenFile{}

// TokenFile is application.GoogleTokenFile: a JSON file at File, mode 0600,
// in a directory made 0700 when it is missing.
type TokenFile struct {
	File string
}

// keptGrant is the file's JSON.
type keptGrant struct {
	Refresh string `json:"refresh_token"`
	Scope   string `json:"scope"`
}

// Load implements application.GoogleTokenFile.
func (f TokenFile) Load() (application.GoogleGrant, error) {
	body, err := os.ReadFile(f.File)
	if errors.Is(err, os.ErrNotExist) {
		return application.GoogleGrant{}, fmt.Errorf("%w: there is no %s", application.ErrGoogleNotLinked, f.File)
	}
	if err != nil {
		return application.GoogleGrant{}, fmt.Errorf("reading the Google token file: %w", err)
	}
	var kept keptGrant
	if err := json.Unmarshal(body, &kept); err != nil || kept.Refresh == "" {
		return application.GoogleGrant{}, fmt.Errorf("the Google token file %s is not one mw google link wrote: revoke mw at https://myaccount.google.com/permissions and delete the file", f.File)
	}
	return application.GoogleGrant{Refresh: kept.Refresh, Scope: kept.Scope}, nil
}

// Save implements application.GoogleTokenFile: it writes a temporary file made
// 0600 beside the token and renames it over, so a reader sees the old grant or
// the new, and a loose file left by hand is replaced by one only he can read.
func (f TokenFile) Save(grant application.GoogleGrant) error {
	dir := filepath.Dir(f.File)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(keptGrant{Refresh: grant.Refresh, Scope: grant.Scope})
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".google-token-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(body, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), f.File)
}

// Remove implements application.GoogleTokenFile.
func (f TokenFile) Remove() error {
	if err := os.Remove(f.File); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Path implements application.GoogleTokenFile.
func (f TokenFile) Path() string { return f.File }

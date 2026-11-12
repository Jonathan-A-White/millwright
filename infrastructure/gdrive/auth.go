package gdrive

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The two scopes mw gdrive asks for: his Drive files and his Sheets cells.
const (
	DriveScope  = "https://www.googleapis.com/auth/drive"
	SheetsScope = "https://www.googleapis.com/auth/spreadsheets"
	Scope       = DriveScope + " " + SheetsScope
)

// Redirect is the address Google sends the browser to after consent. Nothing
// listens there: he copies the address bar back, which is the loopback flow
// without a server, and works from a browser on another machine.
const Redirect = "http://localhost"

// callTimeout bounds each call to Google.
const callTimeout = 30 * time.Second

// Endpoints are where Google lives; a test points them at a fake.
type Endpoints struct {
	Auth   string
	Token  string
	Drive  string
	Sheets string
}

// GoogleEndpoints are Google's own.
var GoogleEndpoints = Endpoints{
	Auth:   "https://accounts.google.com/o/oauth2/v2/auth",
	Token:  "https://oauth2.googleapis.com/token",
	Drive:  "https://www.googleapis.com",
	Sheets: "https://sheets.googleapis.com",
}

// Account is Google's sign-in side: the consent URL and the code exchange.
type Account struct {
	Endpoints Endpoints
	// HTTP calls the token endpoint; a client bounded by callTimeout when nil.
	HTTP *http.Client
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

func (a Account) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a Account) http() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: callTimeout}
}

// Flow is one sign-in in progress: the consent URL to show, and the state and
// PKCE verifier its answer must match.
type Flow struct {
	URL      string
	State    string
	Verifier string
}

func randomWord(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("no randomness for the sign-in: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Begin makes the consent URL for client: offline access and a forced consent
// screen (so a refresh token is handed out every time), PKCE and a state.
func (a Account) Begin(c Client) (Flow, error) {
	verifier, err := randomWord(32)
	if err != nil {
		return Flow{}, err
	}
	state, err := randomWord(24)
	if err != nil {
		return Flow{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {c.ID},
		"redirect_uri":          {Redirect},
		"response_type":         {"code"},
		"scope":                 {Scope},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	return Flow{URL: a.Endpoints.Auth + "?" + q.Encode(), State: state, Verifier: verifier}, nil
}

// Finish takes the address he pasted back (the browser's failed trip to
// http://localhost, with the code in it), checks its state and trades the code
// for a token. The code is never put in an error.
func (a Account) Finish(ctx context.Context, c Client, flow Flow, pasted string) (Token, error) {
	pasted = strings.TrimSpace(pasted)
	u, err := url.Parse(pasted)
	if err != nil || u.Host == "" || u.RawQuery == "" {
		return Token{}, fmt.Errorf("that is not the address the browser ended on: paste the whole http://localhost/?... line from the address bar")
	}
	q := u.Query()
	if q.Get("state") != flow.State {
		return Token{}, fmt.Errorf("the pasted address is not from this sign-in (its state does not match): run mw gdrive auth again")
	}
	if e := q.Get("error"); e != "" {
		return Token{}, fmt.Errorf("Google said %s", oneWord(e))
	}
	code := q.Get("code")
	if code == "" {
		return Token{}, fmt.Errorf("the pasted address has no code in it")
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {flow.Verifier},
		"redirect_uri":  {Redirect},
		"client_id":     {c.ID},
	}
	if c.Secret != "" {
		form.Set("client_secret", c.Secret)
	}
	answer, status, err := a.postToken(ctx, form)
	if err != nil {
		return Token{}, fmt.Errorf("calling Google's token endpoint: %w", err)
	}
	if status != http.StatusOK || answer.Error != "" || answer.Refresh == "" {
		why := refusal(answer.Error, answer.Description)
		if status == http.StatusOK && answer.Error == "" {
			why = "no refresh token came back"
		}
		return Token{}, fmt.Errorf("Google's token endpoint refused the code (HTTP %d): %s", status, why)
	}
	return answer.token(a.now()), nil
}

// tokenAnswer is the token endpoint's answer, success or refusal.
type tokenAnswer struct {
	Access      string `json:"access_token"`
	Refresh     string `json:"refresh_token"`
	Scope       string `json:"scope"`
	Type        string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (t tokenAnswer) token(now time.Time) Token {
	return Token{Access: t.Access, Refresh: t.Refresh, Scope: t.Scope, Type: t.Type, Expiry: now.Add(time.Duration(t.ExpiresIn) * time.Second)}
}

// postToken sends form to the token endpoint and reads the JSON answer
// whatever the status. The error is the transport's alone and names no part of
// form.
func (a Account) postToken(ctx context.Context, form url.Values) (tokenAnswer, int, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Endpoints.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenAnswer{}, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http().Do(req)
	if err != nil {
		return tokenAnswer{}, 0, scrubTransport(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return tokenAnswer{}, resp.StatusCode, err
	}
	var answer tokenAnswer
	_ = json.Unmarshal(body, &answer)
	return answer, resp.StatusCode, nil
}

// scrubTransport keeps a transport error's reason and drops its URL, which
// could carry a query.
func scrubTransport(err error) error {
	if ue, ok := err.(*url.Error); ok {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// refusal is an OAuth error and its description, said briefly.
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

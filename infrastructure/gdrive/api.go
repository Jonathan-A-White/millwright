package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Session is a signed-in look into his Drive: the files holding the token, the
// client that refreshes it, and where Google is.
type Session struct {
	Endpoints Endpoints
	Files     Files
	Client    Client
	// HTTP calls Google; a client bounded by callTimeout when nil.
	HTTP *http.Client
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

func (s Session) account() Account {
	return Account{Endpoints: s.Endpoints, HTTP: s.HTTP, Now: s.Now}
}

// bearer is a good access token: the saved one while it has a minute left,
// else one refreshed with the refresh token, saved at once.
func (s Session) bearer(ctx context.Context) (string, error) {
	tok, err := s.Files.LoadToken()
	if err != nil {
		return "", err
	}
	a := s.account()
	if tok.Access != "" && tok.Expiry.After(a.now().Add(time.Minute)) {
		return tok.Access, nil
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.Refresh},
		"client_id":     {s.Client.ID},
	}
	if s.Client.Secret != "" {
		form.Set("client_secret", s.Client.Secret)
	}
	answer, status, err := a.postToken(ctx, form)
	if err != nil {
		return "", fmt.Errorf("calling Google's token endpoint: %w", err)
	}
	if answer.Error == "invalid_grant" {
		return "", fmt.Errorf("%w: Google no longer honours the refresh token; run mw gdrive auth again", ErrNotSignedIn)
	}
	if status != http.StatusOK || answer.Error != "" || answer.Access == "" {
		return "", fmt.Errorf("Google's token endpoint refused the refresh (HTTP %d): %s", status, refusal(answer.Error, answer.Description))
	}
	fresh := answer.token(a.now())
	if fresh.Refresh == "" {
		fresh.Refresh = tok.Refresh
	}
	if fresh.Scope == "" {
		fresh.Scope = tok.Scope
	}
	if err := s.Files.SaveToken(fresh); err != nil {
		return "", fmt.Errorf("saving the refreshed Drive token: %w", err)
	}
	return fresh.Access, nil
}

// HTTPError is Google refusing a call: the status and its own message, cut
// short. It holds nothing of the request.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Google answered HTTP %d: %s", e.Status, e.Message)
}

// GetJSON is a GET of address with the bearer token (refreshed and saved when
// it has run out), its JSON answer decoded into into. It is how mw gmail
// reaches Google through this one sign-in; an answer of 401 wraps
// ErrNotSignedIn, any other refusal is an *HTTPError.
func (s Session) GetJSON(ctx context.Context, address string, into any) error {
	return s.call(ctx, http.MethodGet, address, nil, into)
}

// call sends one request with the bearer token and decodes a JSON answer into
// into (nil to ignore it). A refusal is said as HTTP status and Google's own
// message, cut short.
func (s Session) call(ctx context.Context, method, address string, body any, into any) error {
	access, err := s.bearer(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: callTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling Google: %w", scrubTransport(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("reading Google's answer: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		var refused struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &refused)
		msg := strings.Join(strings.Fields(refused.Error.Message), " ")
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: Google refused the token (HTTP 401); run mw gdrive auth again", ErrNotSignedIn)
		}
		return &HTTPError{Status: resp.StatusCode, Message: msg}
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("Google's answer was not the JSON expected: %w", err)
	}
	return nil
}

// File is one Drive file as find lists it.
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Modified string `json:"modifiedTime"`
	MimeType string `json:"mimeType"`
}

// maxFound is the most files find lists.
const maxFound = "100"

// Find lists the Drive files whose name contains name, newest modified first.
// It lists metadata only; nothing is downloaded.
func (s Session) Find(ctx context.Context, name string) ([]File, error) {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(name)
	q := url.Values{
		"q":                         {"name contains '" + escaped + "' and trashed = false"},
		"orderBy":                   {"modifiedTime desc"},
		"fields":                    {"files(id,name,modifiedTime,mimeType)"},
		"pageSize":                  {maxFound},
		"supportsAllDrives":         {"true"},
		"includeItemsFromAllDrives": {"true"},
	}
	var out struct {
		Files []File `json:"files"`
	}
	if err := s.call(ctx, http.MethodGet, s.Endpoints.Drive+"/drive/v3/files?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// Tab is one tab (sheet) of a spreadsheet.
type Tab struct {
	Title   string
	ID      int
	Rows    int
	Columns int
}

func (s Session) sheetURL(id, suffix string) string {
	return s.Endpoints.Sheets + "/v4/spreadsheets/" + url.PathEscape(id) + suffix
}

// Tabs returns the spreadsheet's title and its tabs.
func (s Session) Tabs(ctx context.Context, id string) (string, []Tab, error) {
	var out struct {
		Properties struct {
			Title string `json:"title"`
		} `json:"properties"`
		Sheets []struct {
			Properties struct {
				Title string `json:"title"`
				ID    int    `json:"sheetId"`
				Grid  struct {
					Rows    int `json:"rowCount"`
					Columns int `json:"columnCount"`
				} `json:"gridProperties"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	fields := "properties.title,sheets.properties(title,sheetId,gridProperties(rowCount,columnCount))"
	if err := s.call(ctx, http.MethodGet, s.sheetURL(id, "?fields="+url.QueryEscape(fields)), nil, &out); err != nil {
		return "", nil, err
	}
	tabs := make([]Tab, 0, len(out.Sheets))
	for _, sh := range out.Sheets {
		p := sh.Properties
		tabs = append(tabs, Tab{Title: p.Title, ID: p.ID, Rows: p.Grid.Rows, Columns: p.Grid.Columns})
	}
	return out.Properties.Title, tabs, nil
}

// title is the spreadsheet's name alone.
func (s Session) title(ctx context.Context, id string) (string, error) {
	var out struct {
		Properties struct {
			Title string `json:"title"`
		} `json:"properties"`
	}
	if err := s.call(ctx, http.MethodGet, s.sheetURL(id, "?fields=properties.title"), nil, &out); err != nil {
		return "", err
	}
	return out.Properties.Title, nil
}

// A1 joins a tab and a range into Sheets' A1 notation, quoting the tab when its
// name needs it. With no tab it is the range alone; with no range, the tab.
func A1(tab, rng string) string {
	if tab == "" {
		return rng
	}
	quoted := tab
	for _, r := range tab {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			quoted = "'" + strings.ReplaceAll(tab, "'", "''") + "'"
			break
		}
	}
	if rng == "" {
		return quoted
	}
	return quoted + "!" + rng
}

// Read returns the cells of tab (and rng, when given) as the text Sheets shows
// them. Trailing empty cells and rows are left off by Sheets itself.
func (s Session) Read(ctx context.Context, id, tab, rng string) ([][]string, error) {
	return s.values(ctx, id, A1(tab, rng))
}

func (s Session) values(ctx context.Context, id, a1 string) ([][]string, error) {
	var out struct {
		Values [][]any `json:"values"`
	}
	if err := s.call(ctx, http.MethodGet, s.sheetURL(id, "/values/"+url.PathEscape(a1)), nil, &out); err != nil {
		return nil, err
	}
	rows := make([][]string, len(out.Values))
	for i, row := range out.Values {
		rows[i] = make([]string, len(row))
		for j, cell := range row {
			rows[i][j] = fmt.Sprint(cell)
		}
	}
	return rows, nil
}

// GuardedWord is the word in a file's name that makes a write need --yes.
const GuardedWord = "budget"

// Written is what a write changed.
type Written struct {
	Title  string
	Range  string
	Cells  int
	Before [][]string
	After  [][]string
}

// Write puts values in one row starting at a1 (a range like Sheet1!A1:C1),
// USER_ENTERED, as if typed. A file whose name contains "Budget" (any case) is
// refused unless yes, before anything is changed.
func (s Session) Write(ctx context.Context, id, a1 string, values []string, yes bool) (Written, error) {
	title, err := s.title(ctx, id)
	if err != nil {
		return Written{}, err
	}
	if !yes && strings.Contains(strings.ToLower(title), GuardedWord) {
		return Written{}, fmt.Errorf("%q is a budget: nothing was written; run it again with --yes if you mean to change it", title)
	}
	before, err := s.values(ctx, id, a1)
	if err != nil {
		return Written{}, err
	}
	row := make([]any, len(values))
	for i, v := range values {
		row[i] = v
	}
	var out struct {
		UpdatedRange string `json:"updatedRange"`
		UpdatedCells int    `json:"updatedCells"`
	}
	body := map[string]any{"range": a1, "majorDimension": "ROWS", "values": [][]any{row}}
	address := s.sheetURL(id, "/values/"+url.PathEscape(a1)+"?valueInputOption=USER_ENTERED")
	if err := s.call(ctx, http.MethodPut, address, body, &out); err != nil {
		return Written{}, err
	}
	return Written{Title: title, Range: out.UpdatedRange, Cells: out.UpdatedCells, Before: before, After: [][]string{values}}, nil
}

package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/gdrive"

	"github.com/cucumber/godog"
)

// gdriveSelftestContext is a stand-in Drive and Sheets, the session signed in to
// it, and what mw gdrive selftest did there.
type gdriveSelftestContext struct {
	mu         sync.Mutex
	server     *httptest.Server
	dir        string
	now        time.Time
	refuseCell bool
	cell       string
	trashed    []string
	got        gdrive.Selftested
	err        error
}

const (
	selftestAccess  = "ya29.feature-access"
	selftestMadeID  = "id-made"
	selftestOtherID = "id-same-name"
)

// InitializeGdriveSelftestScenario registers the steps of features/gdrive_selftest.feature.
func InitializeGdriveSelftestScenario(ctx *godog.ScenarioContext) {
	c := &gdriveSelftestContext{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*c = gdriveSelftestContext{now: time.Date(2026, 11, 16, 13, 9, 41, 0, time.UTC)}
		return ctx, nil
	})
	ctx.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if c.server != nil {
			c.server.Close()
		}
		if c.dir != "" {
			_ = os.RemoveAll(c.dir)
		}
		return ctx, nil
	})

	ctx.Given(`^a signed-in Drive holding a file also named "([^"]*)"$`, c.aSignedInDrive)
	ctx.Given(`^the Drive refuses writes to cells$`, func() error { c.refuseCell = true; return nil })
	ctx.When(`^the Drive selftest runs$`, c.theSelftestRuns)
	ctx.Then(`^the Drive selftest succeeds$`, c.theSelftestSucceeds)
	ctx.Then(`^the Drive selftest fails saying: (.+)$`, c.theSelftestFailsSaying)
	ctx.Then(`^the Drive selftest wrote and read back the same time$`, c.wroteAndReadBack)
	ctx.Then(`^the Drive selftest trashed only the sheet it created$`, c.trashedOnlyItsOwn)
}

func (c *gdriveSelftestContext) aSignedInDrive(name string) error {
	dir, err := scenarioTempDir("", "mw-gdrive-selftest-")
	if err != nil {
		return err
	}
	c.dir = dir
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/spreadsheets", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"spreadsheetId":"` + selftestMadeID + `"}`))
	})
	mux.HandleFunc("/v4/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch {
		case !strings.Contains(r.URL.Path, "/values/"):
			_, _ = w.Write([]byte(`{"properties":{"title":"` + name + `"}}`))
		case r.Method == http.MethodPut && c.refuseCell:
			http.Error(w, `{"error":{"message":"write refused"}}`, http.StatusForbidden)
		case r.Method == http.MethodPut:
			var in struct {
				Values [][]string `json:"values"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			c.cell = in.Values[0][0]
			_, _ = w.Write([]byte(`{"updatedRange":"Sheet1!A1","updatedCells":1}`))
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"values": [][]string{{c.cell}}})
		}
	})
	mux.HandleFunc("/drive/v3/files/", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")
		c.trashed = append(c.trashed, id)
		_, _ = w.Write([]byte(`{"id":"` + id + `","trashed":true}`))
	})
	c.server = httptest.NewServer(mux)
	return nil
}

func (c *gdriveSelftestContext) theSelftestRuns() error {
	files := gdrive.Files{Dir: filepath.Join(c.dir, "gdrive")}
	token := gdrive.Token{Access: selftestAccess, Refresh: "refresh", Expiry: c.now.Add(time.Hour), Scope: "s"}
	if err := files.SaveToken(token); err != nil {
		return err
	}
	s := gdrive.Session{
		Endpoints: gdrive.Endpoints{Token: c.server.URL + "/token", Drive: c.server.URL, Sheets: c.server.URL},
		Files:     files,
		Client:    gdrive.Client{ID: "cid"},
		Now:       func() time.Time { return c.now },
	}
	c.got, c.err = s.Selftest(context.Background())
	return nil
}

func (c *gdriveSelftestContext) theSelftestSucceeds() error {
	if c.err != nil {
		return fmt.Errorf("the selftest failed: %v", c.err)
	}
	return nil
}

func (c *gdriveSelftestContext) theSelftestFailsSaying(want string) error {
	if c.err == nil || !strings.Contains(c.err.Error(), want) {
		return fmt.Errorf("expected a failure saying %q, got %v", want, c.err)
	}
	return nil
}

func (c *gdriveSelftestContext) wroteAndReadBack() error {
	want := c.now.UTC().Format(time.RFC3339)
	if c.got.Wrote != want || c.got.ReadBack != want {
		return fmt.Errorf("expected %q written and read back, got %+v", want, c.got)
	}
	return nil
}

func (c *gdriveSelftestContext) trashedOnlyItsOwn() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.trashed) != 1 || c.trashed[0] != selftestMadeID {
		return fmt.Errorf("expected only %s trashed, got %v (another file of the name is %s)", selftestMadeID, c.trashed, selftestOtherID)
	}
	return nil
}

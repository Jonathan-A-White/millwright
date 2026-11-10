package steps

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/vault"

	"github.com/cucumber/godog"
)

// The steps of the live check a landing makes once the rig's after-landing
// command has passed (application/deploychecked.go). They hang off nextContext:
// the site is a fake one on this machine, named in the rig's file in the
// vault, and the comment is read off the fake tracker.

// liveSiteFixture is the rig's public site in a scenario.
type liveSiteFixture struct {
	server   *httptest.Server
	pageGets atomic.Int32
	version  string
	silent   bool
}

// registerNextDeployedCheckedSteps registers the live check's steps of
// features/next.feature and features/batch_land.feature.
func registerNextDeployedCheckedSteps(ctx *godog.ScenarioContext, c *nextContext) {
	ctx.Given(`^the rig's live site serves a page that holds the version "([^"]*)"$`, c.theLiveSiteServesVersion)
	ctx.Given(`^the rig's live site answers its healthz$`, c.theLiveSiteAnswersHealthz)
	ctx.Given(`^the rig's live site does not answer$`, c.theLiveSiteIsSilent)
	ctx.Given(`^the live check is bounded at (\d+) milliseconds$`, c.theLiveCheckIsBoundedAt)

	ctx.Then(`^"([^"]*)" carries one comment that begins "([^"]*)"$`, c.theStoryCarriesOneCommentBeginning)
	ctx.Then(`^"([^"]*)" carries no DEPLOYED-CHECKED comment$`, c.theStoryCarriesNoDeployedChecked)
	ctx.Then(`^that DEPLOYED-CHECKED comment says: (.+)$`, c.thatCommentSays)
	ctx.Then(`^that DEPLOYED-CHECKED comment does not say: (.+)$`, c.thatCommentDoesNotSay)
	ctx.Then(`^the live site was asked for its page (\d+) times?$`, c.theLiveSiteWasAsked)
}

// siteWeb is the application.VerifyWeb of the scenarios: a plain GET.
type siteWeb struct{}

func (siteWeb) Get(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

// liveSite starts the scenario's site the first time it is asked for and names
// it in the rig's file in the vault.
func (c *nextContext) liveSite() (*liveSiteFixture, error) {
	if c.live != nil {
		return c.live, nil
	}
	site := &liveSiteFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		site.pageGets.Add(1)
		if site.silent {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		fmt.Fprintf(w, "<html><head><meta name=\"app-version\" content=\"%s\"></head><body>hello</body></html>", site.version)
	})
	site.server = httptest.NewServer(mux)
	c.live = site
	return site, c.appendToRigFile(fmt.Sprintf("site_url = %q", site.server.URL+"/"))
}

func (c *nextContext) appendToRigFile(line string) error {
	dir := filepath.Join(c.vault, application.RigsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, c.rigKey()+vault.RigFileExt), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}

func (c *nextContext) theLiveSiteServesVersion(version string) error {
	site, err := c.liveSite()
	if err != nil {
		return err
	}
	site.version = version
	return nil
}

func (c *nextContext) theLiveSiteAnswersHealthz() error {
	site, err := c.liveSite()
	if err != nil {
		return err
	}
	return c.appendToRigFile(fmt.Sprintf("healthz_url = %q", site.server.URL+"/healthz"))
}

func (c *nextContext) theLiveSiteIsSilent() error {
	site, err := c.liveSite()
	if err != nil {
		return err
	}
	site.silent = true
	return nil
}

func (c *nextContext) theLiveCheckIsBoundedAt(milliseconds int) error {
	c.liveBound = time.Duration(milliseconds) * time.Millisecond
	return nil
}

// liveWeb is the site's reader mw next is given.
func (c *nextContext) liveWeb() application.VerifyWeb { return siteWeb{} }

func (c *nextContext) stopTheLiveSite() {
	if c.live != nil && c.live.server != nil {
		c.live.server.Close()
	}
}

// deployedCheckedComments are the comments on id that open DEPLOYED-CHECKED.
func (c *nextContext) deployedCheckedComments(id string) []string {
	var found []string
	for _, comment := range c.tracker.Comments(id) {
		if strings.HasPrefix(comment, "DEPLOYED-CHECKED") {
			found = append(found, comment)
		}
	}
	return found
}

func (c *nextContext) theStoryCarriesOneCommentBeginning(id, begins string) error {
	var found []string
	for _, comment := range c.tracker.Comments(id) {
		if strings.HasPrefix(comment, begins) {
			found = append(found, comment)
		}
	}
	if len(found) != 1 {
		return fmt.Errorf("expected one comment on %s beginning %q, got %d, among %q", id, begins, len(found), c.tracker.Comments(id))
	}
	c.deployedComment = found[0]
	return nil
}

func (c *nextContext) theStoryCarriesNoDeployedChecked(id string) error {
	if found := c.deployedCheckedComments(id); len(found) != 0 {
		return fmt.Errorf("expected no DEPLOYED-CHECKED comment on %s, got %q", id, found)
	}
	return nil
}

// siteWords puts the fake site's addresses in where a step names them.
func (c *nextContext) siteWords(text string) string {
	if c.live != nil && c.live.server != nil {
		text = strings.ReplaceAll(text, "<the site>", c.live.server.URL+"/")
		text = strings.ReplaceAll(text, "<the healthz>", c.live.server.URL+"/healthz")
	}
	return text
}

func (c *nextContext) thatCommentSays(line string) error {
	if want := c.siteWords(line); !strings.Contains(c.deployedComment, want) {
		return fmt.Errorf("expected the comment to say %q, got:\n%s", want, c.deployedComment)
	}
	return nil
}

func (c *nextContext) thatCommentDoesNotSay(line string) error {
	if strings.Contains(strings.ToLower(c.deployedComment), strings.ToLower(line)) {
		return fmt.Errorf("expected the comment not to say %q, got:\n%s", line, c.deployedComment)
	}
	return nil
}

func (c *nextContext) theLiveSiteWasAsked(times int) error {
	got := 0
	if c.live != nil {
		got = int(c.live.pageGets.Load())
	}
	if got != times {
		return fmt.Errorf("expected the live site to be asked for its page %d times, it was asked %d times", times, got)
	}
	return nil
}

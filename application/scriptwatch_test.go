package application_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

const blog = "https://allmymind.org"

// The hosts the blog's list allows when nothing in the config file says
// otherwise: its own domain and subdomains, and jsDelivr for Ghost's own paths.
var allowed = []string{"allmymind.org", "cdn.jsdelivr.net/ghost/"}

// blogFetcher serves the recorded pages of the blog (application/testdata/scriptwatch)
// by the address asked for. An address it has no page for is a failed read.
type blogFetcher struct {
	t     *testing.T
	pages map[string]string
	asked []string
	fail  map[string]error
}

func theBlogAsItWas(t *testing.T) *blogFetcher {
	return &blogFetcher{t: t, pages: map[string]string{
		blog + "/":                                 "home.html",
		blog + "/sitemap-posts.xml":                "sitemap-posts.xml",
		blog + "/sitemap-pages.xml":                "sitemap-pages.xml",
		blog + "/john-5-25/":                       "post-planted.html",
		blog + "/a-clean-post/":                    "post-clean.html",
		blog + "/a-post-with-a-script-of-its-own/": "post-inline.html",
		blog + "/about/":                           "page-about.html",
	}}
}

func (f *blogFetcher) Fetch(_ context.Context, url string) ([]byte, error) {
	f.asked = append(f.asked, url)
	if err := f.fail[url]; err != nil {
		return nil, err
	}
	name, ok := f.pages[url]
	if !ok {
		return nil, errors.New(url + " answered 404 Not Found")
	}
	data, err := os.ReadFile(filepath.Join("testdata", "scriptwatch", name))
	if err != nil {
		f.t.Fatal(err)
	}
	return data, nil
}

func aScriptWatch(t *testing.T) (application.ScriptWatch, *blogFetcher, *apptest.FakePosternSender, notesMap, *bytes.Buffer) {
	t.Helper()
	fetch := theBlogAsItWas(t)
	sender := &apptest.FakePosternSender{}
	notes := notesMap{}
	said := &bytes.Buffer{}
	return application.ScriptWatch{
		Fetch: fetch, Notes: notes, Sender: sender, Blog: blog, Err: said,
		Allowed: func() ([]string, error) { return allowed, nil },
	}, fetch, sender, notes, said
}

func (f *blogFetcher) with(url, page string) *blogFetcher {
	f.pages[url] = page
	return f
}

// The planted script card of John 5:25, `<script src="https://baiduia.my/jquery-1.js">`
// in the first kg-card html block, alarms him: one post, naming the post's address
// and the foreign host. The clean post, the home page and what the list allows
// (the blog's own domain and subdomains, jsDelivr's /ghost/ paths) stay quiet.
func TestAForeignScriptInAPostAlarmsHimNamingThePostAndTheHost(t *testing.T) {
	watch, _, sender, _, _ := aScriptWatch(t)

	alarmed, err := watch.Run(context.Background())
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	var planted []application.PosternSendRequest
	for _, req := range sender.Sent() {
		if req.Class != "alarm" {
			t.Errorf("an alarm post is class alarm, got %q", req.Class)
		}
		if strings.Contains(req.Text, "https://allmymind.org/john-5-25/") {
			planted = append(planted, req)
		}
		if strings.Contains(req.Text, "a-clean-post") || strings.Contains(req.Text, "static.allmymind.org") {
			t.Errorf("alarmed of a clean page:\n%s", req.Text)
		}
	}
	if len(planted) != 1 {
		t.Fatalf("expected one alarm for john-5-25 (three loads of baiduia.my are one post+host), got %d of %d: %+v", len(planted), alarmed, sender.Sent())
	}
	for _, want := range []string{"baiduia.my", "https://baiduia.my/jquery-1.js"} {
		if !strings.Contains(planted[0].Text, want) {
			t.Errorf("the alarm does not name %q:\n%s", want, planted[0].Text)
		}
	}
}

// A page that loads only from the list is quiet, and says nothing.
func TestACleanBlogStaysQuiet(t *testing.T) {
	watch, fetch, sender, notes, said := aScriptWatch(t)
	fetch.with(blog+"/john-5-25/", "post-clean.html").
		with(blog+"/a-post-with-a-script-of-its-own/", "post-clean.html").
		with(blog+"/about/", "post-clean.html")

	alarmed, err := watch.Run(context.Background())
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if alarmed != 0 || len(sender.Sent()) != 0 || len(notes) != 0 || said.Len() != 0 {
		t.Errorf("a clean blog alarmed %d (%d sent, notes %v, said %q)", alarmed, len(sender.Sent()), notes, said)
	}
}

// The scan reads the home page and every address of both sitemaps.
func TestTheScanReadsTheHomePageAndEveryAddressOfBothSitemaps(t *testing.T) {
	watch, fetch, _, _, _ := aScriptWatch(t)
	if _, err := watch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		blog + "/", blog + "/sitemap-posts.xml", blog + "/sitemap-pages.xml",
		blog + "/john-5-25/", blog + "/a-clean-post/", blog + "/a-post-with-a-script-of-its-own/", blog + "/about/",
	} {
		found := false
		for _, got := range fetch.asked {
			found = found || got == want
		}
		if !found {
			t.Errorf("the scan never read %s (read %v)", want, fetch.asked)
		}
	}
}

// jsDelivr is allowed for Ghost's own /ghost/ paths only: another package on the
// same host is foreign.
func TestJsDelivrIsAllowedForGhostsOwnPathsOnly(t *testing.T) {
	watch, _, sender, _, _ := aScriptWatch(t)
	if _, err := watch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var about *application.PosternSendRequest
	for _, req := range sender.Sent() {
		if strings.Contains(req.Text, "https://allmymind.org/about/") {
			r := req
			about = &r
		}
		if strings.Contains(req.Text, "/ghost/sodo-search") || strings.Contains(req.Text, "/ghost/portal") {
			t.Errorf("alarmed of Ghost's own jsDelivr path:\n%s", req.Text)
		}
	}
	if about == nil || !strings.Contains(about.Text, "cdn.jsdelivr.net") {
		t.Fatalf("no alarm of the other jsDelivr package on /about/: %+v", sender.Sent())
	}
}

// An inline script in the body of a post is flagged, once for the post; the
// theme's own inline scripts, in the head and outside the post's body, are not.
func TestAnInlineScriptInAPostBodyIsFlaggedButTheThemesIsNot(t *testing.T) {
	watch, _, sender, _, _ := aScriptWatch(t)
	if _, err := watch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var inline, other int
	for _, req := range sender.Sent() {
		switch {
		case strings.Contains(req.Text, "https://allmymind.org/a-post-with-a-script-of-its-own/"):
			inline++
			if !strings.Contains(req.Text, "inline script") || !strings.Contains(req.Text, "evil.example") {
				t.Errorf("the alarm does not say it is an inline script and show it:\n%s", req.Text)
			}
		case strings.Contains(req.Text, "a-clean-post") || strings.Contains(req.Text, "allmymind.org/ ") || strings.Contains(req.Text, "casper"):
			other++
		}
	}
	if inline != 1 || other != 0 {
		t.Errorf("expected one alarm of the inline script and none of the theme's, got %d and %d: %+v", inline, other, sender.Sent())
	}
}

// A post+host is alarmed once: the daily passes after it stay quiet, and so does
// the pass after a restart, for the note is kept.
func TestAPostAndHostAreAlarmedOnceNotOnEveryPass(t *testing.T) {
	watch, _, sender, notes, _ := aScriptWatch(t)
	for i := 0; i < 3; i++ {
		if _, err := watch.Run(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	first := len(sender.Sent())
	if first != 3 {
		t.Errorf("expected 3 alarms (john-5-25, the inline script, about) over 3 passes, got %d", first)
	}
	again, _, againSender, _, _ := aScriptWatch(t)
	again.Notes = notes
	if alarmed, err := again.Run(context.Background()); err != nil || alarmed != 0 || len(againSender.Sent()) != 0 {
		t.Errorf("a new follower with the notes kept alarmed %d again (err %v)", alarmed, err)
	}
}

// A post that already alarmed him for one host alarms again for a second host.
func TestASecondHostOnTheSamePostIsAlarmedOfToo(t *testing.T) {
	watch, fetch, sender, notes, _ := aScriptWatch(t)
	if _, err := watch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(sender.Sent())
	fetch.with(blog+"/a-clean-post/", "post-planted.html")
	watch.Fetch = &replacing{blogFetcher: fetch, from: "baiduia.my", to: "other-bad.example", only: blog + "/a-clean-post/"}
	if _, err := watch.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(sender.Sent()) - before; got != 1 {
		t.Errorf("expected one new alarm for the new post+host, got %d (notes %v)", got, notes)
	}
}

type replacing struct {
	*blogFetcher
	from, to, only string
}

func (r *replacing) Fetch(ctx context.Context, url string) ([]byte, error) {
	data, err := r.blogFetcher.Fetch(ctx, url)
	if err != nil || url != r.only {
		return data, err
	}
	return bytes.ReplaceAll(data, []byte(r.from), []byte(r.to)), nil
}

// A push that fails takes its mark off, so the next pass tries it again; the
// pass says what failed.
func TestAScriptAlarmThatFailsToSendIsTriedOnTheNextPass(t *testing.T) {
	watch, _, sender, notes, _ := aScriptWatch(t)
	sender.Err = errors.New("no postern key")

	alarmed, err := watch.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "baiduia.my") {
		t.Fatalf("expected an error naming the host, got %v", err)
	}
	if alarmed != 0 {
		t.Errorf("alarmed %d, expected 0", alarmed)
	}
	for key, value := range notes {
		if strings.TrimSpace(value) != "" {
			t.Errorf("a failed alarm left the mark %s = %q", key, value)
		}
	}
	sender.Err = nil
	if alarmed, err = watch.Run(context.Background()); err != nil || alarmed != 3 {
		t.Errorf("the next pass alarmed %d (err %v), expected 3", alarmed, err)
	}
}

// A page that cannot be read is said and the rest still scanned; the pass ends in
// an error, so the Mayor is told, and never in a false all-clear.
func TestAPageThatCannotBeReadIsSaidAndTheRestIsScanned(t *testing.T) {
	watch, fetch, sender, _, _ := aScriptWatch(t)
	fetch.fail = map[string]error{blog + "/a-clean-post/": errors.New("timed out")}

	_, err := watch.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "a-clean-post") || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected an error naming the page, got %v", err)
	}
	if len(sender.Sent()) != 3 {
		t.Errorf("the other pages were not scanned: %d alarms", len(sender.Sent()))
	}

	watch, fetch, sender, _, _ = aScriptWatch(t)
	fetch.fail = map[string]error{blog + "/sitemap-posts.xml": errors.New("no route to host")}
	if _, err := watch.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "sitemap-posts.xml") {
		t.Errorf("expected the failed sitemap named, got %v", err)
	}
	if len(sender.Sent()) != 1 {
		t.Errorf("the pages of the sitemap that was read were not scanned: %d alarms", len(sender.Sent()))
	}
}

// A list that cannot be read is an error and no alarm: a scan with no list would
// alarm of everything.
func TestAListThatCannotBeReadSendsNothing(t *testing.T) {
	watch, fetch, sender, _, _ := aScriptWatch(t)
	watch.Allowed = func() ([]string, error) { return nil, errors.New("bad config") }
	if _, err := watch.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Errorf("expected the list's error, got %v", err)
	}
	if len(sender.Sent()) != 0 || len(fetch.asked) != 0 {
		t.Errorf("read %v and sent %d", fetch.asked, len(sender.Sent()))
	}
}

// Only the home runs the watch.
func TestAFollowerThatIsNotTheHomeDoesNotScan(t *testing.T) {
	watch, fetch, sender, _, _ := aScriptWatch(t)
	watch.Home = func(context.Context) (bool, error) { return false, nil }
	if alarmed, err := watch.Run(context.Background()); err != nil || alarmed != 0 {
		t.Errorf("a host that is not home alarmed %d (err %v)", alarmed, err)
	}
	if len(fetch.asked) != 0 || len(sender.Sent()) != 0 {
		t.Errorf("a host that is not home read %v and sent %d", fetch.asked, len(sender.Sent()))
	}
}

// What the list allows, entry by entry: a host and its subdomains, never a host
// that only ends the same way; a host with a path, that path only.
func TestScriptHostsAllowed(t *testing.T) {
	list := []string{"allmymind.org", "cdn.jsdelivr.net/ghost/"}
	for _, c := range []struct {
		src     string
		allowed bool
	}{
		{"https://allmymind.org/a.js", true},
		{"https://static.allmymind.org/a.js", true},
		{"https://ALLMYMIND.org:443/a.js", true},
		{"/assets/a.js", true},
		{"a.js", true},
		{"https://notallmymind.org/a.js", false},
		{"https://allmymind.org.evil.example/a.js", false},
		{"https://baiduia.my/jquery-1.js", false},
		{"//baiduia.my/jquery-1.js", false},
		{"https://cdn.jsdelivr.net/ghost/portal@~2.37/p.js", true},
		{"https://cdn.jsdelivr.net/npm/x@1/x.js", false},
		{"https://cdn.jsdelivr.net/ghostly/x.js", false},
		{"https://sub.cdn.jsdelivr.net/ghost/x.js", false},
		{"data:text/javascript,alert(1)", false},
		{"javascript:alert(1)", false},
	} {
		host, ok := application.ScriptSrcAllowed(blog+"/john-5-25/", c.src, list)
		if ok != c.allowed {
			t.Errorf("%s: allowed %v (host %q), expected %v", c.src, ok, host, c.allowed)
		}
		if !ok && host == "" {
			t.Errorf("%s: a refused source names no host", c.src)
		}
	}
}

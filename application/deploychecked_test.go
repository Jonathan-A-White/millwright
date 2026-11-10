package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// siteWeb answers each address from a table, counting what it was asked for.
type siteWeb struct {
	pages map[string]string
	fail  map[string]error
	asked map[string]int
}

func (s *siteWeb) Get(_ context.Context, url string) (int, []byte, error) {
	if s.asked == nil {
		s.asked = map[string]int{}
	}
	s.asked[url]++
	if err := s.fail[url]; err != nil {
		return 0, nil, err
	}
	body, ok := s.pages[url]
	if !ok {
		return 404, nil, nil
	}
	return 200, []byte(body), nil
}

type siteRules struct{ rules domain.EpicRequirements }

func (r siteRules) EpicRequirements(context.Context, string) (domain.EpicRequirements, error) {
	return r.rules, nil
}

// commentedStories is a tracker that keeps the comments written on it.
type commentedStories struct {
	WorkTracker
	said map[string][]string
}

func (c *commentedStories) CommentOnStory(_ context.Context, id, text string) error {
	if c.said == nil {
		c.said = map[string][]string{}
	}
	c.said[id] = append(c.said[id], text)
	return nil
}

func (c *commentedStories) StoryComments(context.Context, string) ([]Comment, error) { return nil, nil }

func checkedNext(web VerifyWeb, rules domain.EpicRequirements, tracker WorkTracker) Next {
	return Next{
		LiveSite: web, Rules: siteRules{rules}, Tracker: tracker, Host: "cloud1",
		Now: func() time.Time { return time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC) },
	}
}

func passedReport() *NextReport { return &NextReport{AfterLandingPassed: true} }

const lampasSite = "https://lampas.example/"

func TestDeployedCheckedWritesOneCommentPerStoryFromOneFetchOfThePage(t *testing.T) {
	web := &siteWeb{pages: map[string]string{
		lampasSite:                         `<html><meta name="v" content="2.4.7"><script src="/assets/index-abc.js"></script></html>`,
		lampasSite + "assets/index-abc.js": `console.log("Save it now")`,
		"https://lampas.example/healthz":   "ok",
	}}
	tracker := &commentedStories{}
	n := checkedNext(web, domain.EpicRequirements{SiteURL: lampasSite, HealthzURL: "https://lampas.example/healthz"}, tracker)
	report := passedReport()

	n.deployedChecked(context.Background(), "lampas", report, "0123456789abcdef", "2.4.7", []liveStory{
		{id: "mw-1", texts: []string{"Verify strings:\nSave it now\nNot in the bundle"}},
		{id: "mw-2"},
	})

	if web.asked[lampasSite] != 1 {
		t.Errorf("expected the page fetched once for two stories, got %d", web.asked[lampasSite])
	}
	for _, id := range []string{"mw-1", "mw-2"} {
		if len(tracker.said[id]) != 1 {
			t.Fatalf("expected one comment on %s, got %q", id, tracker.said[id])
		}
		comment := tracker.said[id][0]
		if !strings.HasPrefix(comment, "DEPLOYED-CHECKED by mw on cloud1") {
			t.Errorf("expected the comment to open DEPLOYED-CHECKED by mw on cloud1, got %q", comment)
		}
		if strings.Contains(strings.ToLower(comment), "verified") {
			t.Errorf("expected no VERIFIED in the comment, got %q", comment)
		}
		for _, want := range []string{"commit 0123456789ab", "version 2.4.7", "version 2.4.7 was found on the page", "answered 200", "healthz https://lampas.example/healthz answered 200: ok", "2026-11-10T15:00:00Z"} {
			if !strings.Contains(comment, want) {
				t.Errorf("expected the comment on %s to say %q, got %q", id, want, comment)
			}
		}
	}
	if !strings.Contains(tracker.said["mw-1"][0], "1 of 2 string(s) named on the story were found; not found: Not in the bundle") {
		t.Errorf("expected the comment on mw-1 to say which of its strings were found, got %q", tracker.said["mw-1"][0])
	}
	if strings.Contains(tracker.said["mw-2"][0], "Strings:") {
		t.Errorf("expected no strings line on a story that names none, got %q", tracker.said["mw-2"][0])
	}
	if !strings.Contains(report.Notes[0], "version 2.4.7 found") {
		t.Errorf("expected the report to say the version was found, got %q", report.Notes)
	}
}

func TestDeployedCheckedFindsTheVersionInABundle(t *testing.T) {
	web := &siteWeb{pages: map[string]string{
		lampasSite:                         `<script src="/assets/index-abc.js"></script>`,
		lampasSite + "assets/index-abc.js": `var v="2.4.7"`,
	}}
	tracker := &commentedStories{}
	n := checkedNext(web, domain.EpicRequirements{SiteURL: lampasSite}, tracker)

	n.deployedChecked(context.Background(), "lampas", passedReport(), "abc1234", "2.4.7", []liveStory{{id: "mw-1"}})

	if got := tracker.said["mw-1"]; len(got) != 1 || !strings.Contains(got[0], "version 2.4.7 was found on the page's bundles") {
		t.Errorf("expected the version found in the bundles, got %q", got)
	}
}

func TestDeployedCheckedSaysAnOlderVersionIsNotTheLandedOne(t *testing.T) {
	web := &siteWeb{pages: map[string]string{lampasSite: `<meta content="2.4.6">`}}
	tracker := &commentedStories{}
	n := checkedNext(web, domain.EpicRequirements{SiteURL: lampasSite}, tracker)
	report := passedReport()

	n.deployedChecked(context.Background(), "lampas", report, "abc1234", "2.4.7", []liveStory{{id: "mw-1"}})

	if got := tracker.said["mw-1"]; len(got) != 1 || !strings.Contains(got[0], "version 2.4.7 was not found on the page") {
		t.Errorf("expected the landed version not found, got %q", got)
	}
	if !strings.Contains(report.Notes[0], "version 2.4.7 not found") {
		t.Errorf("expected the report to say the same, got %q", report.Notes)
	}
}

func TestDeployedCheckedSaysAPageThatCouldNotBeRead(t *testing.T) {
	cases := map[string]*siteWeb{
		"no answer":   {fail: map[string]error{lampasSite: errors.New("context deadline exceeded")}},
		"a 404 reply": {},
	}
	for name, web := range cases {
		tracker := &commentedStories{}
		n := checkedNext(web, domain.EpicRequirements{SiteURL: lampasSite}, tracker)
		report := passedReport()

		n.deployedChecked(context.Background(), "lampas", report, "abc1234", "2.4.7", []liveStory{{id: "mw-1"}})

		got := tracker.said["mw-1"]
		if len(got) != 1 || !strings.Contains(got[0], "could not be looked for, for the page was not read") || strings.Contains(got[0], "was found") {
			t.Errorf("%s: expected a comment that the version could not be looked for, got %q", name, got)
		}
		if name == "no answer" && (!strings.Contains(got[0], "the page "+lampasSite+" could not be read") || !strings.Contains(report.Notes[0], "the page could not be read")) {
			t.Errorf("%s: expected the page said unread in the comment and the report, got %q and %q", name, got, report.Notes)
		}
	}
}

func TestDeployedCheckedWritesNothingWhenNothingLiveWasChecked(t *testing.T) {
	web := &siteWeb{pages: map[string]string{lampasSite: "2.4.7"}}
	cases := map[string]struct {
		next   Next
		report *NextReport
	}{
		"the command did not pass": {checkedNext(web, domain.EpicRequirements{SiteURL: lampasSite}, &commentedStories{}), &NextReport{}},
		"no site and no healthz":   {checkedNext(web, domain.EpicRequirements{}, &commentedStories{}), passedReport()},
		"no reader of the site":    {checkedNext(nil, domain.EpicRequirements{SiteURL: lampasSite}, &commentedStories{}), passedReport()},
	}
	for name, c := range cases {
		c.next.deployedChecked(context.Background(), "lampas", c.report, "abc1234", "2.4.7", []liveStory{{id: "mw-1"}})
		if said := c.next.Tracker.(*commentedStories).said; len(said) != 0 {
			t.Errorf("%s: expected no comment, got %q", name, said)
		}
		if len(web.asked) != 0 {
			t.Errorf("%s: expected the site not asked, got %v", name, web.asked)
		}
	}
}

func TestDeployedCheckedOfARigWithOnlyAHealthzNamesNoVersion(t *testing.T) {
	web := &siteWeb{pages: map[string]string{"https://x.example/healthz": "ok"}}
	tracker := &commentedStories{}
	n := checkedNext(web, domain.EpicRequirements{HealthzURL: "https://x.example/healthz"}, tracker)

	n.deployedChecked(context.Background(), "x", passedReport(), "abc1234", "", []liveStory{{id: "mw-1"}})

	got := tracker.said["mw-1"]
	if len(got) != 1 || !strings.Contains(got[0], "the rig names no site_url, so the version was not looked for") || !strings.Contains(got[0], "healthz https://x.example/healthz answered 200") {
		t.Errorf("expected the healthz alone read, got %q", got)
	}
}

func TestVersionOnPageIsAWholeVersion(t *testing.T) {
	cases := []struct {
		text, version string
		want          bool
	}{
		{`content="1.4.3"`, "1.4.3", true},
		{`v1.4.3`, "1.4.3", true},
		{`1.4.3`, "1.4.3", true},
		{`version 1.4.3.`, "1.4.3", true},
		{`11.4.30`, "1.4.3", false},
		{`1.4.3.1`, "1.4.3", false},
		{`0.1.4.3`, "1.4.3", false},
		{`1.4.30`, "1.4.3", false},
		{`1.4.2`, "1.4.3", false},
		{`anything`, "", false},
	}
	for _, c := range cases {
		if got := versionOnPage(c.text, c.version); got != c.want {
			t.Errorf("versionOnPage(%q, %q) = %v, want %v", c.text, c.version, got, c.want)
		}
	}
}

// A story with only a DEPLOYED-CHECKED comment has not been verified: the comment
// opens with no VERIFIED, so the story is still a candidate for it.
func TestADeployedCheckedCommentIsNotAVerifiedOne(t *testing.T) {
	comment := checkedLiveRead().Comment("cloud1", "abc1234", "2.4.7", nil)
	if beginsWithVerifiedWord(comment) {
		t.Errorf("expected beginsWithVerifiedWord false for %q", comment)
	}
	comments := []Comment{{Text: "HOW TO CHECK IT, for the Governor:\n1. Open the app."}, {Text: comment}}
	if verifiedIn(comments) {
		t.Errorf("expected a story with only a DEPLOYED-CHECKED comment not to be verifiedIn")
	}
	beads := candidateBeads{
		closed:   []ClosedStory{{ID: "mw-1", Title: "Story", Type: "task", CloseReason: "landed on main", ClosedAt: time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC)}},
		comments: map[string][]Comment{"mw-1": comments},
	}
	got, err := VerifyCandidates(context.Background(), beads, time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC), 7*24*time.Hour)
	if err != nil || len(got) != 1 || got[0].Bead != "mw-1" {
		t.Errorf("expected mw-1 still listed by VerifyCandidates, got %v (err %v)", got, err)
	}
}

func checkedLiveRead() liveRead {
	return liveRead{Site: lampasSite, At: time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC), Page: livePage{Status: 200, Page: "2.4.7"}}
}

type candidateBeads struct {
	closed   []ClosedStory
	comments map[string][]Comment
}

func (c candidateBeads) WorkInHand(context.Context) (WorkInHand, error) { return WorkInHand{}, nil }
func (c candidateBeads) ClosedStories(context.Context, time.Time) ([]ClosedStory, error) {
	return c.closed, nil
}
func (c candidateBeads) StoriesComments(context.Context, []string) (map[string][]Comment, error) {
	return c.comments, nil
}
func (c candidateBeads) CommentOnStory(context.Context, string, string) error { return nil }

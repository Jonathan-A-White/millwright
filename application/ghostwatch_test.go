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

// fixtureFetcher serves the recorded replies of the blog's home page and of
// GitHub's API (application/testdata/ghostwatch), by the address asked for.
type fixtureFetcher struct {
	t     *testing.T
	home  string
	asked []string
	fail  map[string]error
}

func (f *fixtureFetcher) Fetch(_ context.Context, url string) ([]byte, error) {
	f.asked = append(f.asked, url)
	if err := f.fail[url]; err != nil {
		return nil, err
	}
	var name string
	switch {
	case url == application.GhostWatchBlogURL:
		name = f.home
	case url == application.GhostWatchAdvisoriesURL:
		name = "advisories.json"
	case url == application.GhostWatchLatestURL:
		name = "release-latest.json"
	default:
		f.t.Fatalf("asked for an address the watch has no business with: %s", url)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "ghostwatch", name))
	if err != nil {
		f.t.Fatal(err)
	}
	return data, nil
}

type notesMap map[string]string

func (n notesMap) Note(_ context.Context, key string) (string, error) { return n[key], nil }
func (n notesMap) SetNote(_ context.Context, key, value string) error {
	n[key] = value
	return nil
}

func aGhostWatch(t *testing.T, home string) (application.GhostWatch, *fixtureFetcher, *apptest.FakePosternSender, notesMap, *bytes.Buffer) {
	t.Helper()
	fetch := &fixtureFetcher{t: t, home: home}
	sender := &apptest.FakePosternSender{}
	notes := notesMap{}
	said := &bytes.Buffer{}
	return application.GhostWatch{Fetch: fetch, Notes: notes, Sender: sender, Err: said}, fetch, sender, notes, said
}

// The blog's Ghost 5.105 is inside the range GHSA-w52v-v783-gw97 (<= 6.19.0)
// and GHSA-8jqq-qq46-c657 (>= 1.0.0, < 6.76.0) name: one alarm for each, naming
// the advisory and the version that fixes it. The advisory of another package
// and the one of Ghost 4 are not his.
func TestTheBlogAtGhost5_105IsAlarmedOfEachAdvisoryItIsInside(t *testing.T) {
	watch, _, sender, _, _ := aGhostWatch(t, "blog-home.html")

	alarmed, err := watch.Run(context.Background())
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if alarmed != 2 || len(sender.Sent()) != 2 {
		t.Fatalf("expected 2 alarms, got %d (%d sent)", alarmed, len(sender.Sent()))
	}
	var critical *application.PosternSendRequest
	for _, req := range sender.Sent() {
		if req.Class != "alarm" {
			t.Errorf("an alarm post is class alarm, got %q", req.Class)
		}
		if strings.Contains(req.Text, "GHSA-w52v-v783-gw97") {
			r := req
			critical = &r
		}
		for _, notHis := range []string{"GHSA-aaaa-bbbb-cccc", "GHSA-dddd-eeee-ffff"} {
			if strings.Contains(req.Text, notHis) {
				t.Errorf("alarmed of %s, an advisory the blog is not inside:\n%s", notHis, req.Text)
			}
		}
	}
	if critical == nil {
		t.Fatalf("no alarm names GHSA-w52v-v783-gw97: %+v", sender.Sent())
	}
	for _, want := range []string{"5.105", "CVE-2026-26980", "SQL injection in Content API", "6.19.1", "6.77.0"} {
		if !strings.Contains(critical.Text, want) {
			t.Errorf("the alarm does not name %q:\n%s", want, critical.Text)
		}
	}
}

// An advisory is alarmed once: the daily passes after it stay quiet, and so
// does the pass after a restart, for the note is kept.
func TestAnAdvisoryIsAlarmedOnceNotOnEveryPass(t *testing.T) {
	watch, _, sender, notes, _ := aGhostWatch(t, "blog-home.html")
	for i := 0; i < 3; i++ {
		if _, err := watch.Run(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	if got := len(sender.Sent()); got != 2 {
		t.Errorf("expected 2 alarms over 3 passes, got %d", got)
	}
	again, _, againSender, _, _ := aGhostWatch(t, "blog-home.html")
	again.Notes = notes
	if alarmed, err := again.Run(context.Background()); err != nil || alarmed != 0 || len(againSender.Sent()) != 0 {
		t.Errorf("a new follower with the notes kept alarmed %d again (err %v)", alarmed, err)
	}
}

// A blog past every fix stays quiet.
func TestABlogPastTheFixStaysQuiet(t *testing.T) {
	for _, version := range []string{"6.76.0", "6.77.0", "7.0"} {
		watch, fetch, sender, notes, said := aGhostWatch(t, "blog-home.html")
		fetch.home = "blog-home.html"
		watch.Fetch = versionedFetcher{fixtureFetcher: fetch, version: version}

		alarmed, err := watch.Run(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		if alarmed != 0 || len(sender.Sent()) != 0 || len(notes) != 0 {
			t.Errorf("%s is past the fixes yet %d alarms were sent (notes %v)", version, alarmed, notes)
		}
		if said.Len() != 0 {
			t.Errorf("%s: a quiet pass said: %s", version, said)
		}
	}
}

// versionedFetcher serves the recorded home page with its generator version
// changed.
type versionedFetcher struct {
	*fixtureFetcher
	version string
}

func (v versionedFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	data, err := v.fixtureFetcher.Fetch(ctx, url)
	if err != nil || url != application.GhostWatchBlogURL {
		return data, err
	}
	return bytes.ReplaceAll(data, []byte("Ghost 5.105"), []byte("Ghost "+v.version)), nil
}

// A push that fails takes its mark off, so the next pass tries it again; and the
// pass says what failed.
func TestAnAlarmThatFailsToSendIsTriedOnTheNextPass(t *testing.T) {
	watch, _, sender, notes, _ := aGhostWatch(t, "blog-home.html")
	sender.Err = errors.New("no postern key")

	alarmed, err := watch.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "GHSA-w52v-v783-gw97") {
		t.Fatalf("expected an error naming the advisory, got %v", err)
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
	if alarmed, err = watch.Run(context.Background()); err != nil || alarmed != 2 {
		t.Errorf("the next pass alarmed %d (err %v), expected 2", alarmed, err)
	}
}

// A page with no generator, or a failed read of GitHub, is an error and no
// alarm: the pass cannot tell, and says so.
func TestAPassThatCannotReadAVersionSendsNothing(t *testing.T) {
	watch, fetch, sender, _, _ := aGhostWatch(t, "blog-home.html")
	fetch.fail = map[string]error{application.GhostWatchAdvisoriesURL: errors.New("rate limited")}
	if _, err := watch.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected the failed read, got %v", err)
	}

	watch, fetch, sender, _, _ = aGhostWatch(t, "blog-home.html")
	fetch.fail = map[string]error{application.GhostWatchBlogURL: errors.New("no route to host")}
	if _, err := watch.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "no route") {
		t.Errorf("expected the failed read, got %v", err)
	}

	watch, _, sender, _, _ = aGhostWatch(t, "blog-home.html")
	watch.Fetch = pageWithout{}
	if _, err := watch.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "generator") {
		t.Errorf("expected an error about the generator, got %v", err)
	}
	if len(sender.Sent()) != 0 {
		t.Errorf("a pass that could not tell sent %d alarms", len(sender.Sent()))
	}
}

type pageWithout struct{}

func (pageWithout) Fetch(context.Context, string) ([]byte, error) {
	return []byte("<html><head><title>x</title></head></html>"), nil
}

// Only the home runs the watch: another host's follower does nothing and reads
// nothing.
func TestAFollowerThatIsNotTheHomeDoesNotWatch(t *testing.T) {
	watch, fetch, sender, _, _ := aGhostWatch(t, "blog-home.html")
	watch.Home = func(context.Context) (bool, error) { return false, nil }
	if alarmed, err := watch.Run(context.Background()); err != nil || alarmed != 0 {
		t.Errorf("a host that is not home alarmed %d (err %v)", alarmed, err)
	}
	if len(fetch.asked) != 0 || len(sender.Sent()) != 0 {
		t.Errorf("a host that is not home read %v and sent %d", fetch.asked, len(sender.Sent()))
	}
}

// The version ranges GitHub writes: a comparison, or two joined by a comma.
func TestGhostVersionRanges(t *testing.T) {
	for _, c := range []struct {
		version, rng string
		inside       bool
	}{
		{"5.105", "<= 6.19.0", true},
		{"6.19.0", "<= 6.19.0", true},
		{"6.19.1", "<= 6.19.0", false},
		{"5.105", ">= 1.0.0, < 6.76.0", true},
		{"6.76.0", ">= 1.0.0, < 6.76.0", false},
		{"0.9", ">= 1.0.0, < 6.76.0", false},
		{"5.105.2", "= 5.105.2", true},
		{"5.105", "> 5.105.0", false},
		{"5.105.0", "< 5.105.1", true},
	} {
		got, err := application.GhostVersionInRange(c.version, c.rng)
		if err != nil || got != c.inside {
			t.Errorf("%s in %q: got %v (err %v), expected %v", c.version, c.rng, got, err, c.inside)
		}
	}
	for _, bad := range []string{"", "newer than 6", "<= six"} {
		if _, err := application.GhostVersionInRange("5.105", bad); err == nil {
			t.Errorf("expected %q to be refused", bad)
		}
	}
}

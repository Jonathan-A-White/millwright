package application_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

func ghostFixture(t *testing.T) []byte {
	t.Helper()
	text, err := os.ReadFile("../testdata/ghost/john-5-25.md")
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func ghostRun(t *testing.T, store application.SecretStore, blog *apptest.FakeGhostBlog, text []byte) (string, error) {
	t.Helper()
	return ghostRunWith(t, store, blog, text, application.GhostImage{})
}

func ghostRunWith(t *testing.T, store application.SecretStore, blog *apptest.FakeGhostBlog, text []byte, image application.GhostImage) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := application.GhostDraft{
		Secrets: store,
		Open: func(key string) (application.GhostBlog, error) {
			return blog, nil
		},
		Image: image,
		Out:   &out,
	}.Run(context.Background(), text)
	return out.String(), err
}

func ghostKeyKept() *apptest.FakeSecretStore {
	return &apptest.FakeSecretStore{Values: map[string][]byte{application.GhostKeyName: []byte("id1:abcd")}}
}

func TestParseGhostPostReadsTheFrontmatterAndTheBody(t *testing.T) {
	post, err := application.ParseGhostPost(ghostFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if post.Title != "Who Are the Dead Who Hear? (John 5:25)" {
		t.Errorf("title = %q", post.Title)
	}
	if post.Slug != "who-are-the-dead-who-hear-john-5-25" {
		t.Errorf("slug = %q", post.Slug)
	}
	if !strings.HasPrefix(post.Excerpt, "Read in its context, John 5:25") {
		t.Errorf("excerpt = %q: the excerpt key should serve when custom_excerpt is null", post.Excerpt)
	}
	if !strings.HasPrefix(post.Markdown, "### The Verse and the Claim") {
		t.Errorf("the body should start after the frontmatter: %.60q", post.Markdown)
	}
	if !strings.Contains(post.Markdown, "[^1]: [John 5:1-15](https://lampas.allmymind.org/#/?ref=John.5.1-15).") {
		t.Errorf("the footnote text is not whole in the body")
	}
	if strings.Contains(post.Markdown, "reading_time") {
		t.Error("the frontmatter leaked into the body")
	}
}

func TestParseGhostPostPrefersCustomExcerptAndReadsQuotes(t *testing.T) {
	text := "---\ntitle: 'It''s a \"test\"'\nslug: a-post\ncustom_excerpt: \"Said \\\"so\\\".\"\nexcerpt: the other\n---\nBody.\n"
	post, err := application.ParseGhostPost([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if post.Title != `It's a "test"` || post.Excerpt != `Said "so".` || post.Markdown != "Body." {
		t.Errorf("got %+v", post)
	}
}

func TestParseGhostPostRefusesWhatCannotBeADraft(t *testing.T) {
	for name, text := range map[string]string{
		"no frontmatter": "just a body\n",
		"unclosed":       "---\ntitle: x\nslug: y\nbody\n",
		"no title":       "---\nslug: y\n---\nbody\n",
		"no slug":        "---\ntitle: x\n---\nbody\n",
		"a bad slug":     "---\ntitle: x\nslug: ../../users\n---\nbody\n",
		"no body":        "---\ntitle: x\nslug: y\n---\n\n",
	} {
		if _, err := application.ParseGhostPost([]byte(text)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestGhostDraftCreatesADraftAndSaysItsEditorAddress(t *testing.T) {
	blog := &apptest.FakeGhostBlog{}
	out, err := ghostRun(t, ghostKeyKept(), blog, ghostFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(blog.Created) != 1 || len(blog.Updated) != 0 {
		t.Fatalf("created %d, updated %d", len(blog.Created), len(blog.Updated))
	}
	if !strings.Contains(out, "https://blog.test/ghost/#/editor/post/new1") {
		t.Errorf("out = %q: it should print the editor address", out)
	}
}

func TestGhostDraftUpdatesTheDraftOfTheSameSlug(t *testing.T) {
	blog := &apptest.FakeGhostBlog{Posts: map[string]application.GhostPost{
		"who-are-the-dead-who-hear-john-5-25": {ID: "p9", Slug: "who-are-the-dead-who-hear-john-5-25", Status: "draft", EditorURL: "https://blog.test/ghost/#/editor/post/p9"},
	}}
	out, err := ghostRun(t, ghostKeyKept(), blog, ghostFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(blog.Created) != 0 || len(blog.Updated) != 1 {
		t.Fatalf("created %d, updated %d", len(blog.Created), len(blog.Updated))
	}
	if !strings.Contains(out, "editor/post/p9") {
		t.Errorf("out = %q", out)
	}
}

func TestGhostDraftRefusesAPostThatIsNotADraftAndChangesNothing(t *testing.T) {
	for _, status := range []string{"published", "scheduled", "sent"} {
		blog := &apptest.FakeGhostBlog{Posts: map[string]application.GhostPost{
			"who-are-the-dead-who-hear-john-5-25": {ID: "p9", Slug: "who-are-the-dead-who-hear-john-5-25", Title: "Who Are", Status: status},
		}}
		_, err := ghostRun(t, ghostKeyKept(), blog, ghostFixture(t))
		if err == nil {
			t.Fatalf("%s: no refusal", status)
		}
		for _, want := range []string{"who-are-the-dead-who-hear-john-5-25", "p9", status} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the refusal %q should name %q", status, err, want)
			}
		}
		if len(blog.Created)+len(blog.Updated) != 0 {
			t.Errorf("%s: the blog was changed", status)
		}
	}
}

func TestGhostDraftWithNoKeyNamesTheCommandThatPutsIt(t *testing.T) {
	blog := &apptest.FakeGhostBlog{}
	_, err := ghostRun(t, &apptest.FakeSecretStore{}, blog, ghostFixture(t))
	if err == nil || !strings.Contains(err.Error(), "mw secrets put ghost_admin_key") {
		t.Fatalf("err = %v, want one naming mw secrets put ghost_admin_key", err)
	}
	if len(blog.Created) != 0 {
		t.Error("a post was made without a key")
	}
}

func TestGhostDraftCutsTheKeyOutOfAnErrorTheBlogGives(t *testing.T) {
	const key = "sentinelid:5e17f00dcafe"
	store := &apptest.FakeSecretStore{Values: map[string][]byte{application.GhostKeyName: []byte(key)}}
	blog := &apptest.FakeGhostBlog{Err: errors.New("Ghost said no to " + key + " and 5e17f00dcafe")}
	_, err := ghostRun(t, store, blog, ghostFixture(t))
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(err.Error(), "5e17f00dcafe") || strings.Contains(err.Error(), "sentinelid") {
		t.Errorf("the key is in the error: %v", err)
	}
}

var ghostPic = application.GhostImage{Name: "lighthouse.png", Data: []byte("\x89PNGfake")}

func TestGhostDraftUploadsTheFeatureImageAndSetsItOnTheDraft(t *testing.T) {
	blog := &apptest.FakeGhostBlog{}
	if _, err := ghostRunWith(t, ghostKeyKept(), blog, ghostFixture(t), ghostPic); err != nil {
		t.Fatal(err)
	}
	if len(blog.Uploaded) != 1 || blog.Uploaded[0].Name != "lighthouse.png" {
		t.Fatalf("uploaded = %v", blog.Uploaded)
	}
	if len(blog.Created) != 1 || blog.Created[0].FeatureImage != blog.UploadedURL("lighthouse.png") || blog.Created[0].FeatureImage == "" {
		t.Errorf("created = %+v, want the uploaded address as the feature image", blog.Created)
	}
}

func TestGhostDraftWithNoImageUploadsNothingAndSetsNoFeatureImage(t *testing.T) {
	blog := &apptest.FakeGhostBlog{}
	if _, err := ghostRun(t, ghostKeyKept(), blog, ghostFixture(t)); err != nil {
		t.Fatal(err)
	}
	if len(blog.Uploaded) != 0 || blog.Created[0].FeatureImage != "" {
		t.Errorf("uploaded %v, created %+v", blog.Uploaded, blog.Created)
	}
}

func TestGhostDraftUploadsNothingForAPostThatIsNotADraft(t *testing.T) {
	for _, status := range []string{"published", "scheduled", "sent"} {
		blog := &apptest.FakeGhostBlog{Posts: map[string]application.GhostPost{
			"who-are-the-dead-who-hear-john-5-25": {ID: "p9", Slug: "who-are-the-dead-who-hear-john-5-25", Title: "Who Are", Status: status},
		}}
		if _, err := ghostRunWith(t, ghostKeyKept(), blog, ghostFixture(t), ghostPic); err == nil {
			t.Fatalf("%s: no refusal", status)
		}
		if len(blog.Uploaded)+len(blog.Created)+len(blog.Updated) != 0 {
			t.Errorf("%s: the blog was changed", status)
		}
	}
}

func TestGhostDraftRefusesAFeatureImageThatIsNotAPictureBeforeAskingTheBlogAnything(t *testing.T) {
	big := make([]byte, application.GhostImageMaxBytes+1)
	for name, image := range map[string]application.GhostImage{
		"text file": {Name: "notes.txt", Data: []byte("hi")},
		"empty":     {Name: "a.png", Data: nil},
		"too big":   {Name: "a.png", Data: big},
		"no name":   {Name: "", Data: []byte("x")},
	} {
		blog := &apptest.FakeGhostBlog{}
		_, err := ghostRunWith(t, ghostKeyKept(), blog, ghostFixture(t), image)
		if err == nil {
			t.Errorf("%s: no refusal", name)
		}
		if len(blog.Uploaded)+len(blog.Created) != 0 {
			t.Errorf("%s: the blog was changed", name)
		}
	}
}

package ghost_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/ghost"
)

const (
	sentinelID     = "5e17c0de0ddba11"
	sentinelSecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	sentinelKey    = sentinelID + ":" + sentinelSecret
	slug           = "who-are-the-dead-who-hear-john-5-25"
)

// blog is a Ghost Admin API in memory. It records every request, checks the
// token on each, and fails the test on any request the command has no business
// making.
type blog struct {
	t        *testing.T
	mu       sync.Mutex
	version  string // the Content-Version it answers with, "" for none
	posts    map[string]map[string]any
	seen     []string
	bodies   []map[string]any
	failWith int // when set, every request is answered with this status

	uploads     []upload
	uploadFails bool // when set, an image upload is refused
}

func newBlog(t *testing.T) (*blog, *httptest.Server) {
	b := &blog{t: t, version: "v5.0", posts: map[string]map[string]any{}}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return b, srv
}

func (b *blog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seen = append(b.seen, r.Method+" "+r.URL.Path)
	if !b.tokenGood(r.Header.Get("Authorization")) && b.failWith != http.StatusUnauthorized {
		b.t.Errorf("%s %s: the token is bad: %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
	}
	if b.version != "" {
		w.Header().Set("Content-Version", b.version)
	}
	// An unkind server: it quotes what it was sent back in its errors.
	if b.failWith != 0 {
		w.WriteHeader(b.failWith)
		io.WriteString(w, `{"errors":[{"message":"nope, got `+r.Header.Get("Authorization")+` for `+sentinelKey+`"}]}`)
		return
	}
	if r.URL.Path == "/ghost/api/admin/images/upload/" && r.Method == http.MethodPost {
		b.upload(w, r)
		return
	}
	const prefix = "/ghost/api/admin/posts/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		b.t.Errorf("a request outside the posts: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	var body map[string]any
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				b.t.Errorf("a body that is not JSON: %v", err)
			}
		}
	}
	b.bodies = append(b.bodies, body)
	reply := func(code int, post map[string]any) {
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]any{"posts": []any{post}})
	}
	switch {
	case r.Method == http.MethodGet && strings.HasPrefix(rest, "slug/"):
		post, ok := b.posts[strings.Trim(strings.TrimPrefix(rest, "slug/"), "/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"errors":[{"message":"Post not found."}]}`)
			return
		}
		reply(http.StatusOK, post)
	case r.Method == http.MethodPost && rest == "":
		post := b.sent(body)
		post["id"] = "newid1"
		post["updated_at"] = "2026-11-04T15:00:00.000Z"
		b.posts[post["slug"].(string)] = post
		reply(http.StatusCreated, post)
	case r.Method == http.MethodPut && rest != "":
		post := b.sent(body)
		id := strings.Trim(rest, "/")
		for s, old := range b.posts {
			if old["id"] == id {
				post["id"] = id
				b.posts[s] = post
				reply(http.StatusOK, post)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		b.t.Errorf("a request the command may not make: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// upload answers the one image upload mw ghost draft may make: a multipart
// body with a file and purpose=image. It keeps what it was sent.
func (b *blog) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		b.t.Errorf("an upload that is not multipart: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		b.t.Errorf("an upload with no file part: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, _ := io.ReadAll(file)
	b.uploads = append(b.uploads, upload{name: header.Filename, kind: header.Header.Get("Content-Type"), purpose: r.FormValue("purpose"), data: data})
	if b.uploadFails {
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"errors":[{"message":"unsupported file, token `+r.Header.Get("Authorization")+`"}]}`)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"images": []any{map[string]any{"url": "https://blog.test/content/images/2026/11/" + header.Filename, "ref": nil}}})
}

// upload is what the blog was sent as an image.
type upload struct {
	name, kind, purpose string
	data                []byte
}

// sent is the one post of a request's body.
func (b *blog) sent(body map[string]any) map[string]any {
	posts, _ := body["posts"].([]any)
	if len(posts) != 1 {
		b.t.Fatalf("a body of %d posts", len(posts))
	}
	return posts[0].(map[string]any)
}

// tokenGood checks a Ghost admin token: HS256 over the hex-decoded secret,
// the id in kid, the audience /admin/, and five minutes at most to live.
func (b *blog) tokenGood(header string) bool {
	token, ok := strings.CutPrefix(header, "Ghost ")
	if !ok {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	secret, _ := hex.DecodeString(sentinelSecret)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[2] {
		return false
	}
	var head struct{ Alg, Kid, Typ string }
	var claims struct {
		Iat, Exp int64
		Aud      string
	}
	for i, into := range []any{&head, &claims} {
		raw, err := base64.RawURLEncoding.DecodeString(parts[i])
		if err != nil || json.Unmarshal(raw, into) != nil {
			return false
		}
	}
	return head.Alg == "HS256" && head.Kid == sentinelID && head.Typ == "JWT" &&
		claims.Aud == "/admin/" && claims.Exp-claims.Iat <= 300 && claims.Exp > time.Now().Unix()
}

func fixture(t *testing.T) []byte {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ghost", "john-5-25.md"))
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// run is mw ghost draft against the fake, with the sentinel key kept, and all
// that it says, on stdout or in its error, joined.
func run(t *testing.T, srv *httptest.Server, text []byte) (string, error) {
	t.Helper()
	return runWith(t, srv, text, application.GhostImage{})
}

// runWith is run with a feature image.
func runWith(t *testing.T, srv *httptest.Server, text []byte, image application.GhostImage) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := application.GhostDraft{
		Secrets: &apptest.FakeSecretStore{Values: map[string][]byte{application.GhostKeyName: []byte(sentinelKey)}},
		Open: func(key string) (application.GhostBlog, error) {
			return ghost.New(srv.URL, key)
		},
		Image: image,
		Out:   &out,
	}.Run(context.Background(), text)
	said := out.String()
	if err != nil {
		said += "\n" + err.Error()
	}
	return said, err
}

func TestDraftCreatesADraftWithTheFootnotesWhole(t *testing.T) {
	b, srv := newBlog(t)
	out, err := run(t, srv, fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GET /ghost/api/admin/posts/slug/" + slug + "/", "POST /ghost/api/admin/posts/"}; strings.Join(b.seen, "|") != strings.Join(want, "|") {
		t.Fatalf("requests = %v, want %v", b.seen, want)
	}
	post := b.sent(b.bodies[1])
	if post["status"] != "draft" {
		t.Errorf("status = %v, want draft though the frontmatter says otherwise", post["status"])
	}
	if post["title"] != "Who Are the Dead Who Hear? (John 5:25)" || post["slug"] != slug {
		t.Errorf("title %v, slug %v", post["title"], post["slug"])
	}
	if !strings.HasPrefix(post["custom_excerpt"].(string), "Read in its context") {
		t.Errorf("custom_excerpt = %v", post["custom_excerpt"])
	}
	// Ghost 5: lexical, the body one markdown card.
	var doc struct {
		Root struct {
			Children []struct{ Type, Markdown string }
		}
	}
	if err := json.Unmarshal([]byte(post["lexical"].(string)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Root.Children) != 1 || doc.Root.Children[0].Type != "markdown" {
		t.Fatalf("lexical children = %+v, want one markdown card", doc.Root.Children)
	}
	md := doc.Root.Children[0].Markdown
	for _, want := range []string{
		"[^1]: [John 5:1-15](https://lampas.allmymind.org/#/?ref=John.5.1-15).",
		"[^2]: [Malachi 3:2](https://lampas.allmymind.org/#/?ref=Mal.3.2); [Revelation 6:17](https://lampas.allmymind.org/#/?ref=Rev.6.17).",
		"Lord appears.[^2] But",
		"[report of the service](https://nak-sued.de/db/14239)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the markdown card lost %q", want)
		}
	}
	if _, has := post["mobiledoc"]; has {
		t.Error("a Ghost 5 post carries mobiledoc")
	}
	if !strings.Contains(out, srv.URL+"/ghost/#/editor/post/newid1") {
		t.Errorf("out = %q: want the editor address", out)
	}
}

func TestDraftUsesMobiledocForGhostFour(t *testing.T) {
	b, srv := newBlog(t)
	b.version = "v4.48"
	if _, err := run(t, srv, fixture(t)); err != nil {
		t.Fatal(err)
	}
	post := b.sent(b.bodies[1])
	if _, has := post["lexical"]; has {
		t.Error("a Ghost 4 post carries lexical")
	}
	var doc struct {
		Cards [][]any
	}
	if err := json.Unmarshal([]byte(post["mobiledoc"].(string)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cards) != 1 || doc.Cards[0][0] != "markdown" {
		t.Fatalf("cards = %v", doc.Cards)
	}
	card := doc.Cards[0][1].(map[string]any)
	if !strings.Contains(card["markdown"].(string), "[^3]: [Acts 3:19]") {
		t.Error("the mobiledoc markdown card lost a footnote")
	}
}

func TestASecondRunUpdatesTheDraftWithTheTimeItWasLastChanged(t *testing.T) {
	b, srv := newBlog(t)
	if _, err := run(t, srv, fixture(t)); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, srv, fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /ghost/api/admin/posts/slug/" + slug + "/", "POST /ghost/api/admin/posts/",
		"GET /ghost/api/admin/posts/slug/" + slug + "/", "PUT /ghost/api/admin/posts/newid1/",
	}
	if strings.Join(b.seen, "|") != strings.Join(want, "|") {
		t.Fatalf("requests = %v, want %v", b.seen, want)
	}
	put := b.sent(b.bodies[3])
	if put["status"] != "draft" || put["updated_at"] != "2026-11-04T15:00:00.000Z" {
		t.Errorf("the update sent status %v, updated_at %v", put["status"], put["updated_at"])
	}
	if !strings.Contains(out, "editor/post/newid1") {
		t.Errorf("out = %q", out)
	}
}

func TestAPostThatIsNotADraftIsRefusedAndNothingIsSent(t *testing.T) {
	for _, status := range []string{"published", "scheduled", "sent"} {
		b, srv := newBlog(t)
		b.posts[slug] = map[string]any{"id": "live42", "slug": slug, "title": "Who Are", "status": status, "updated_at": "2026-11-01T00:00:00.000Z"}
		out, err := run(t, srv, fixture(t))
		if err == nil {
			t.Fatalf("%s: no refusal", status)
		}
		if !strings.Contains(out, "live42") || !strings.Contains(out, slug) {
			t.Errorf("%s: the refusal does not name the post: %s", status, out)
		}
		if len(b.seen) != 1 || !strings.HasPrefix(b.seen[0], "GET ") {
			t.Errorf("%s: requests = %v, want the one lookup", status, b.seen)
		}
	}
}

func TestNoRequestButGetPostAndPutOnPostsIsMadeAndNoSourceHasADeletePath(t *testing.T) {
	b, srv := newBlog(t)
	run(t, srv, fixture(t))
	run(t, srv, fixture(t))
	for _, line := range b.seen {
		method, path, _ := strings.Cut(line, " ")
		if method != http.MethodGet && method != http.MethodPost && method != http.MethodPut {
			t.Errorf("a %s request was made: %s", method, line)
		}
		if !strings.HasPrefix(path, "/ghost/api/admin/posts/") && line != "POST /ghost/api/admin/images/upload/" {
			t.Errorf("a request off the posts and the one image upload: %s", line)
		}
	}
	for _, path := range []string{"ghost.go", "../../application/ghost.go", "../../cmd/mw/ghost.go"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"MethodDelete", `"DELETE"`, "MethodPatch", `"PATCH"`} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s names %s: mw ghost draft has no delete path", path, banned)
			}
		}
	}
}

func TestTheKeyIsNowhereInWhatIsSaidOnASuccessAUnauthorisedOrAServerError(t *testing.T) {
	for name, status := range map[string]int{"success": 0, "401": http.StatusUnauthorized, "500": http.StatusInternalServerError} {
		b, srv := newBlog(t)
		b.failWith = status
		out, err := run(t, srv, fixture(t))
		if status != 0 && err == nil {
			t.Errorf("%s: no error", name)
		}
		if status == 0 && err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, secret := range []string{sentinelKey, sentinelSecret, sentinelID} {
			if strings.Contains(out, secret) {
				t.Errorf("%s: the key's %q is in what was said: %s", name, secret, out)
			}
		}
		if strings.Contains(out, "Ghost eyJ") || strings.Contains(out, "eyJ") {
			t.Errorf("%s: a token is in what was said: %s", name, out)
		}
		if status != 0 && !strings.Contains(out, "HTTP "+strconv.Itoa(status)) {
			t.Errorf("%s: the error should say the status: %s", name, out)
		}
	}
}

func TestAMalformedKeyIsRefusedWithoutSayingIt(t *testing.T) {
	for _, key := range []string{"nocolon", "kid9:not-hex", ":abcd", "kid9:"} {
		_, err := ghost.New("https://blog.test", key)
		if err == nil {
			t.Errorf("%q: no error", key)
			continue
		}
		if strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "mw secrets put ghost_admin_key") {
			t.Errorf("%q: the error %q should not quote the key and should name the put", key, err)
		}
	}
}

var pic = application.GhostImage{Name: "lighthouse.png", Data: []byte("\x89PNG\r\n\x1a\nfake picture")}

func TestAFeatureImageIsUploadedAsAnImageThenSetOnTheNewDraft(t *testing.T) {
	b, srv := newBlog(t)
	out, err := runWith(t, srv, fixture(t), pic)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.uploads) != 1 {
		t.Fatalf("%d uploads, want one", len(b.uploads))
	}
	up := b.uploads[0]
	if up.name != "lighthouse.png" || up.purpose != "image" || up.kind != "image/png" || !bytes.Equal(up.data, pic.Data) {
		t.Errorf("upload = %+v", up)
	}
	post := b.sent(b.bodies[len(b.bodies)-1])
	if post["feature_image"] != "https://blog.test/content/images/2026/11/lighthouse.png" || post["status"] != "draft" {
		t.Errorf("the draft = %v, want feature_image set and status draft", post)
	}
	if want := []string{"GET /ghost/api/admin/posts/slug/" + slug + "/", "POST /ghost/api/admin/images/upload/", "POST /ghost/api/admin/posts/"}; strings.Join(b.seen, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %v, want %v: the lookup first, so nothing is uploaded for a post that is refused", b.seen, want)
	}
	if !strings.Contains(out, "newid1") {
		t.Errorf("out = %s", out)
	}
}

func TestAFeatureImageIsSetOnTheDraftOfASecondRunToo(t *testing.T) {
	b, srv := newBlog(t)
	b.posts[slug] = map[string]any{"id": "d1", "slug": slug, "title": "T", "status": "draft", "updated_at": "2026-11-01T00:00:00.000Z"}
	if _, err := runWith(t, srv, fixture(t), pic); err != nil {
		t.Fatal(err)
	}
	if post := b.sent(b.bodies[len(b.bodies)-1]); post["feature_image"] == nil || post["status"] != "draft" {
		t.Errorf("the update = %v", post)
	}
}

func TestADraftWithNoFeatureImageSendsNoFeatureImageKeyAndNoUpload(t *testing.T) {
	b, srv := newBlog(t)
	if _, err := run(t, srv, fixture(t)); err != nil {
		t.Fatal(err)
	}
	if len(b.uploads) != 0 {
		t.Error("an upload with no image")
	}
	if _, has := b.sent(b.bodies[len(b.bodies)-1])["feature_image"]; has {
		t.Error("a feature_image key with no image: an update would clear the one the draft has")
	}
}

func TestAPostThatIsNotADraftIsRefusedWithAFeatureImageAndNothingIsUploaded(t *testing.T) {
	for _, status := range []string{"published", "scheduled", "sent"} {
		b, srv := newBlog(t)
		b.posts[slug] = map[string]any{"id": "live42", "slug": slug, "title": "Who Are", "status": status, "updated_at": "2026-11-01T00:00:00.000Z"}
		if _, err := runWith(t, srv, fixture(t), pic); err == nil {
			t.Fatalf("%s: no refusal", status)
		}
		if len(b.uploads) != 0 || len(b.seen) != 1 {
			t.Errorf("%s: uploads %d, requests %v, want the one lookup", status, len(b.uploads), b.seen)
		}
	}
}

func TestTheKeyAndTheTokenAreNowhereInWhatIsSaidWhenTheUploadFails(t *testing.T) {
	b, srv := newBlog(t)
	b.uploadFails = true
	out, err := runWith(t, srv, fixture(t), pic)
	if err == nil || !strings.Contains(out, "HTTP 422") {
		t.Fatalf("err = %v, out = %s, want a refusal saying the status", err, out)
	}
	for _, secret := range []string{sentinelKey, sentinelSecret, sentinelID, "eyJ"} {
		if strings.Contains(out, secret) {
			t.Errorf("%q is in what was said: %s", secret, out)
		}
	}
	for _, line := range b.seen {
		if strings.HasPrefix(line, "POST /ghost/api/admin/posts/") {
			t.Error("a draft was made after the upload failed")
		}
	}
}

// Package ghost is the blog's Ghost Admin API, narrowed to drafts: it finds a
// post by slug, creates a draft and updates a draft, and has no other call.
// Nothing here sends a status but "draft", and no request is made with any
// method but GET, POST or PUT, on /ghost/api/admin/posts/ alone.
package ghost

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultURL is the blog's address when the config does not say another.
const DefaultURL = "https://allmymind.org"

// tokenLife is how long an Admin API token lives; Ghost takes five minutes at
// most.
const tokenLife = 5 * time.Minute

// maxReply is the most of a reply read: a post is small.
const maxReply = 4 << 20

// Blog implements application.GhostBlog over Ghost's Admin API.
type Blog struct {
	// URL is the blog's address, without a path.
	URL string
	// Client makes the requests; nil is a client with a 30 second timeout.
	Client *http.Client
	// Now is the clock the token is dated by; nil is time.Now.
	Now func() time.Time

	id     string
	secret []byte
	key    string
	// lexical is whether the blog takes a post's body as lexical (Ghost 5 and
	// after) rather than mobiledoc (Ghost 4). It is learnt from the
	// Content-Version header of the reply to Find, which always comes first.
	lexical bool
}

var _ application.GhostBlog = (*Blog)(nil)

// New is the blog at url, to be signed into with key, Ghost's "id:secret"
// form. The error for a key that is not that form never quotes it.
func New(url, key string) (*Blog, error) {
	id, secret, found := strings.Cut(strings.TrimSpace(key), ":")
	raw, err := hex.DecodeString(secret)
	if !found || id == "" || secret == "" || err != nil {
		return nil, errors.New("the Ghost key kept is not Ghost's id:secret form (the secret in hex): put the Admin API key again with mw secrets put ghost_admin_key")
	}
	return &Blog{URL: strings.TrimRight(url, "/"), id: id, secret: raw, key: strings.TrimSpace(key), lexical: true}, nil
}

// token is an Admin API token: HS256 over the hex-decoded secret, the key's id
// as kid, the audience /admin/.
func (b *Blog) token() string {
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	iat := now().Unix()
	head, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT", "kid": b.id})
	claims, _ := json.Marshal(map[string]any{"iat": iat, "exp": iat + int64(tokenLife.Seconds()), "aud": "/admin/"})
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(head) + "." + enc.EncodeToString(claims)
	mac := hmac.New(sha256.New, b.secret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil))
}

// apiPost is a post as Ghost says it.
type apiPost struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updated_at"`
}

// call is the one place a request is made. It allows GET, POST and PUT on the
// posts, and nothing else, and it keeps the key and the token out of anything
// it returns. A 404 is returned as (nil, 404, nil).
func (b *Blog) call(ctx context.Context, method, path string, body any) (*apiPost, int, error) {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut:
	default:
		return nil, 0, fmt.Errorf("the %s method is not one mw ghost draft makes", method)
	}
	if !strings.HasPrefix(path, "posts/") {
		return nil, 0, errors.New("mw ghost draft touches the posts and nothing else")
	}
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.URL+"/ghost/api/admin/"+path, payload)
	if err != nil {
		return nil, 0, b.clean(err)
	}
	token := b.token()
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, b.clean(err, token)
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return nil, 0, b.clean(fmt.Errorf("reading the blog's reply: %w", err), token)
	}
	if version := resp.Header.Get("Content-Version"); method == http.MethodGet && version != "" {
		b.lexical = lexicalFrom(version)
	}
	if resp.StatusCode == http.StatusNotFound && method == http.MethodGet {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode/100 != 2 {
		return nil, resp.StatusCode, b.clean(fmt.Errorf("the blog refused %s %s (HTTP %d): %s", method, "/"+path, resp.StatusCode, apiMessage(reply)), token)
	}
	var answer struct {
		Posts []apiPost `json:"posts"`
	}
	if err := json.Unmarshal(reply, &answer); err != nil || len(answer.Posts) != 1 {
		return nil, resp.StatusCode, fmt.Errorf("the blog's reply to %s /%s was not one post (HTTP %d)", method, path, resp.StatusCode)
	}
	return &answer.Posts[0], resp.StatusCode, nil
}

// lexicalFrom reads a Content-Version like v5.0 or v4.48: lexical from major
// version 5, mobiledoc before it; lexical when it cannot be read.
func lexicalFrom(version string) bool {
	major, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	n, err := strconv.Atoi(major)
	return err != nil || n >= 5
}

// apiMessage is the first message of a Ghost error reply, short; "no message"
// when it has none.
func apiMessage(reply []byte) string {
	var answer struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(reply, &answer) != nil || len(answer.Errors) == 0 || answer.Errors[0].Message == "" {
		return "no message"
	}
	message := answer.Errors[0].Message
	if len(message) > 200 {
		message = message[:200] + "..."
	}
	return message
}

// clean is err with the key, its parts and each token cut out of its text.
func (b *Blog) clean(err error, tokens ...string) error {
	text := err.Error()
	for _, secret := range append([]string{b.key, hex.EncodeToString(b.secret), b.id}, tokens...) {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "<key>")
		}
	}
	// A Ghost reply may quote the header it was sent whole.
	if i := strings.Index(text, "Ghost eyJ"); i >= 0 {
		text = text[:i] + "<key>"
	}
	return errors.New(text)
}

// Find implements application.GhostBlog.
func (b *Blog) Find(ctx context.Context, slug string) (application.GhostPost, bool, error) {
	post, status, err := b.call(ctx, http.MethodGet, "posts/slug/"+url.PathEscape(slug)+"/", nil)
	if err != nil {
		return application.GhostPost{}, false, err
	}
	if status == http.StatusNotFound {
		return application.GhostPost{}, false, nil
	}
	return b.known(*post), true, nil
}

// Create implements application.GhostBlog.
func (b *Blog) Create(ctx context.Context, draft application.GhostDraftPost) (application.GhostPost, error) {
	post, _, err := b.call(ctx, http.MethodPost, "posts/", b.body(draft, ""))
	if err != nil {
		return application.GhostPost{}, err
	}
	return b.known(*post), nil
}

// Update implements application.GhostBlog. The body carries the post's
// updated_at, so the blog refuses it if the post has changed since Find.
func (b *Blog) Update(ctx context.Context, post application.GhostPost, draft application.GhostDraftPost) (application.GhostPost, error) {
	if post.ID == "" || strings.ContainsAny(post.ID, "/?#") {
		return application.GhostPost{}, errors.New("the post to update has no usable id")
	}
	updated, _, err := b.call(ctx, http.MethodPut, "posts/"+url.PathEscape(post.ID)+"/", b.body(draft, post.UpdatedAt))
	if err != nil {
		return application.GhostPost{}, err
	}
	return b.known(*updated), nil
}

func (b *Blog) known(p apiPost) application.GhostPost {
	return application.GhostPost{
		ID: p.ID, Slug: p.Slug, Title: p.Title, Status: p.Status, UpdatedAt: p.UpdatedAt,
		EditorURL: b.URL + "/ghost/#/editor/post/" + p.ID,
	}
}

// body is the request body for a draft: the status is the draft's, always,
// and the markdown is one markdown card, which is Ghost's own renderer, so
// links and footnotes come out as Ghost makes them. The card is lexical for
// Ghost 5 and after, mobiledoc before.
func (b *Blog) body(draft application.GhostDraftPost, updatedAt string) map[string]any {
	post := map[string]any{
		"title":          draft.Title,
		"slug":           draft.Slug,
		"status":         application.GhostStatusDraft,
		"custom_excerpt": nil,
	}
	if draft.Excerpt != "" {
		post["custom_excerpt"] = draft.Excerpt
	}
	if updatedAt != "" {
		post["updated_at"] = updatedAt
	}
	if b.lexical {
		doc, _ := json.Marshal(map[string]any{"root": map[string]any{
			"type": "root", "version": 1, "direction": nil, "format": "", "indent": 0,
			"children": []any{map[string]any{"type": "markdown", "version": 1, "markdown": draft.Markdown}},
		}})
		post["lexical"] = string(doc)
	} else {
		doc, _ := json.Marshal(map[string]any{
			"version": "0.3.1", "atoms": []any{}, "markups": []any{}, "sections": []any{[]int{10, 0}},
			"cards": []any{[]any{"markdown", map[string]string{"markdown": draft.Markdown}}},
		})
		post["mobiledoc"] = string(doc)
	}
	return map[string]any{"posts": []any{post}}
}

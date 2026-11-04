package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// GhostKeyName is the SecretStore name the blog's Admin API key is kept under:
// Ghost's "id:secret" form, from the custom integration named millwright.
const GhostKeyName = "ghost_admin_key"

// GhostStatusDraft is the one status mw ghost draft ever gives a post.
const GhostStatusDraft = "draft"

// ghostKeyHint is what an error about the key points a person at.
const ghostKeyHint = "mw secrets put " + GhostKeyName

// GhostPost is a post the blog holds, as far as mw ghost draft needs to know.
type GhostPost struct {
	ID    string
	Slug  string
	Title string
	// Status is the blog's word for it: draft, or anything else it holds.
	Status string
	// UpdatedAt is when the post last changed, as the blog says it; an update
	// carries it, and the blog refuses the update if the post has changed
	// since.
	UpdatedAt string
	// EditorURL is where a person opens the post in the blog's editor.
	EditorURL string
}

// GhostDraftPost is what mw ghost draft sends: no status, because every post
// it sends is a draft.
type GhostDraftPost struct {
	Title    string
	Slug     string
	Excerpt  string
	Markdown string
}

// GhostBlog is the blog's Admin API, narrowed to a post's draft. It has no
// way to publish, schedule, send or delete: whatever it creates or updates is
// a draft. No method may put the key in an error.
type GhostBlog interface {
	// Find is the post with this slug, whatever its status; false when the
	// blog holds none.
	Find(ctx context.Context, slug string) (GhostPost, bool, error)
	// Create makes a new draft.
	Create(ctx context.Context, draft GhostDraftPost) (GhostPost, error)
	// Update replaces what the draft post holds, and leaves it a draft.
	Update(ctx context.Context, post GhostPost, draft GhostDraftPost) (GhostPost, error)
}

// ghostSlug is what a slug may be: it ends up in a request path.
var ghostSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,190}$`)

// ParseGhostPost reads a post's markdown file: the frontmatter between the
// first two --- lines gives the title, the slug and the excerpt (custom_excerpt,
// else excerpt), and what follows is the body. Every other key, status
// included, is ignored.
func ParseGhostPost(text []byte) (GhostDraftPost, error) {
	lines := strings.Split(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return GhostDraftPost{}, errors.New("the post has no frontmatter: it must open with a --- line, then title and slug, then --- again")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return GhostDraftPost{}, errors.New("the post's frontmatter is not closed by a second --- line")
	}
	keys := map[string]string{}
	for _, line := range lines[1:end] {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
			continue // a comment, or inside a list: no key we read is
		}
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		keys[strings.TrimSpace(name)] = frontmatterValue(value)
	}
	draft := GhostDraftPost{
		Title:    keys["title"],
		Slug:     keys["slug"],
		Excerpt:  keys["custom_excerpt"],
		Markdown: strings.TrimSpace(strings.Join(lines[end+1:], "\n")),
	}
	if draft.Excerpt == "" {
		draft.Excerpt = keys["excerpt"]
	}
	switch {
	case draft.Title == "":
		return GhostDraftPost{}, errors.New("the post's frontmatter has no title")
	case draft.Slug == "":
		return GhostDraftPost{}, errors.New("the post's frontmatter has no slug: it is how a second run finds the draft")
	case !ghostSlug.MatchString(draft.Slug):
		return GhostDraftPost{}, fmt.Errorf("the post's slug %q is not lower-case letters, digits and hyphens", draft.Slug)
	case draft.Markdown == "":
		return GhostDraftPost{}, errors.New("the post has no body after its frontmatter")
	}
	return draft, nil
}

// frontmatterValue reads the value of a `key: value` line: quotes, either
// kind, are taken off, and null, ~ and nothing are no value.
func frontmatterValue(raw string) string {
	value := strings.TrimSpace(raw)
	switch {
	case value == "null" || value == "~":
		return ""
	case len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"':
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(value[1 : len(value)-1])
	case len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'':
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value
}

// GhostDraft is mw ghost draft: it pushes a post's markdown to the blog as a
// draft, and never as anything else. A second run with the same slug updates
// that draft; a post under the slug that is not a draft is refused and left
// alone. It prints the post's editor address.
type GhostDraft struct {
	Secrets SecretStore
	// Open is the blog for a key; the error it gives never quotes the key.
	Open func(key string) (GhostBlog, error)
	Out  io.Writer
}

// Run implements mw ghost draft, over the text of the post's file.
func (g GhostDraft) Run(ctx context.Context, text []byte) error {
	draft, err := ParseGhostPost(text)
	if err != nil {
		return err
	}
	raw, err := g.Secrets.Get(ctx, GhostKeyName)
	if errors.Is(err, ErrNoSecret) {
		return fmt.Errorf("no Ghost key is kept: put the Admin API key of the millwright custom integration with %s", ghostKeyHint)
	}
	if err != nil {
		return fmt.Errorf("reading %s from the vault (%s puts it): %w", GhostKeyName, ghostKeyHint, err)
	}
	key := strings.TrimSpace(string(raw))
	blog, err := g.Open(key)
	if err != nil {
		return cutOut(err, key)
	}
	post, err := g.push(ctx, blog, draft)
	if err != nil {
		return cutOut(err, key)
	}
	if g.Out != nil {
		fmt.Fprintf(g.Out, "%s the draft %q\n%s\n", post.verb, draft.Title, post.EditorURL)
	}
	return nil
}

// pushed is the post a push left, and what was done to it.
type pushed struct {
	GhostPost
	verb string
}

// push finds the draft by slug and updates it, or makes a new one; a post of
// the slug that is anything but a draft is refused before anything is sent.
func (g GhostDraft) push(ctx context.Context, blog GhostBlog, draft GhostDraftPost) (pushed, error) {
	existing, found, err := blog.Find(ctx, draft.Slug)
	if err != nil {
		return pushed{}, err
	}
	if !found {
		post, err := blog.Create(ctx, draft)
		return pushed{post, "created"}, err
	}
	if existing.Status != GhostStatusDraft {
		return pushed{}, fmt.Errorf("the post %q (%s, id %s) is %s, not a draft: mw ghost draft changes drafts only, so a published, scheduled or sent post is left as it is", draft.Slug, existing.Title, existing.ID, existing.Status)
	}
	post, err := blog.Update(ctx, existing, draft)
	return pushed{post, "updated"}, err
}

// cutOut is err with the key, and each half of it, cut out of its text: a
// last net under the adapter's own, so that no error says the key.
func cutOut(err error, key string) error {
	text := err.Error()
	if id, secret, found := strings.Cut(key, ":"); found {
		text = strings.ReplaceAll(text, key, "<key>")
		for _, part := range []string{secret, id} {
			if part != "" {
				text = strings.ReplaceAll(text, part, "<key>")
			}
		}
	}
	if text == err.Error() {
		return err
	}
	return errors.New(text)
}

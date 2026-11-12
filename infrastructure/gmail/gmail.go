// Package gmail is the home's read-only look into his Gmail: a search that
// lists messages and a read that shows one message's headers and plain text.
// It goes through the one Google sign-in of mw gdrive (infrastructure/gdrive:
// the same client, the same token file, the same refresh) and asks for the
// gmail.readonly scope alone. There is no send, modify, delete or label call
// here, and an attachment is named, never downloaded. No token or secret is
// ever put in an error or anything printed.
package gmail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Jonathan-A-White/millwright/infrastructure/gdrive"
)

// ErrNoGmailScope is wrapped by the error that says the saved sign-in was made
// before Gmail was added to it.
var ErrNoGmailScope = errors.New("this Google sign-in does not include Gmail")

// noGmail is that error, with what to do.
func noGmail() error {
	return fmt.Errorf("%w: run mw gdrive auth again to add Gmail", ErrNoGmailScope)
}

// Limits on a search's size.
const (
	DefaultLimit = 10
	MaxLimit     = 100
)

// Client is a signed-in look into his Gmail: the gdrive session, which holds
// the token files, the client and where Google is.
type Client struct {
	Session gdrive.Session
}

// Summary is one message as search lists it.
type Summary struct {
	ID      string
	Date    string // UTC, "2006-01-02 15:04"
	When    time.Time
	From    string
	Subject string
}

// Header is one message header.
type Header struct {
	Name  string
	Value string
}

// Attachment is a file on a message, named and not fetched.
type Attachment struct {
	Name     string
	MimeType string
	Size     int
}

// Message is one message as read shows it.
type Message struct {
	ID          string
	Headers     []Header
	Body        string
	Attachments []Attachment
}

func (c Client) base() string {
	return c.Session.Endpoints.Gmail + "/gmail/v1/users/me/messages"
}

// ready checks the saved token includes the Gmail scope before any call leaves.
func (c Client) ready() error {
	tok, err := c.Session.Files.LoadToken()
	if err != nil {
		return err
	}
	if !tok.Has(gdrive.GmailScope) {
		return noGmail()
	}
	return nil
}

// get is one GET, with Google's 403 for a missing scope said as the sign-in
// being short of Gmail.
func (c Client) get(ctx context.Context, address string, into any) error {
	err := c.Session.GetJSON(ctx, address, into)
	var refused *gdrive.HTTPError
	if errors.As(err, &refused) && refused.Status == http.StatusForbidden && strings.Contains(strings.ToLower(refused.Message), "scope") {
		return noGmail()
	}
	return err
}

type apiHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type apiPart struct {
	MimeType string      `json:"mimeType"`
	Filename string      `json:"filename"`
	Headers  []apiHeader `json:"headers"`
	Body     struct {
		Size int    `json:"size"`
		Data string `json:"data"`
	} `json:"body"`
	Parts []apiPart `json:"parts"`
}

type apiMessage struct {
	ID           string  `json:"id"`
	InternalDate string  `json:"internalDate"`
	Payload      apiPart `json:"payload"`
}

func (m apiMessage) header(name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// Search lists the messages matching query (Gmail's own search syntax), newest
// first, at most limit of them.
func (c Client) Search(ctx context.Context, query string, limit int) ([]Summary, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	if err := c.ready(); err != nil {
		return nil, err
	}
	q := url.Values{"q": {query}, "maxResults": {strconv.Itoa(limit)}}
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := c.get(ctx, c.base()+"?"+q.Encode(), &list); err != nil {
		return nil, err
	}
	meta := url.Values{"format": {"metadata"}, "metadataHeaders": {"From", "Subject", "Date"}}.Encode()
	var out []Summary
	for _, ref := range list.Messages {
		if len(out) >= limit {
			break
		}
		var m apiMessage
		if err := c.get(ctx, c.base()+"/"+url.PathEscape(ref.ID)+"?"+meta, &m); err != nil {
			return nil, err
		}
		s := Summary{ID: ref.ID, From: oneLine(m.header("From")), Subject: oneLine(m.header("Subject"))}
		if ms, err := strconv.ParseInt(m.InternalDate, 10, 64); err == nil {
			s.When = time.UnixMilli(ms).UTC()
		} else if t, err := time.Parse(time.RFC1123Z, m.header("Date")); err == nil {
			s.When = t.UTC()
		}
		if !s.When.IsZero() {
			s.Date = s.When.Format("2006-01-02 15:04")
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].When.After(out[j].When) })
	return out, nil
}

// shown are the headers read prints, in this order.
var shown = []string{"From", "To", "Cc", "Date", "Subject"}

// Read fetches one message and returns its headers, its plain-text body (the
// text part, else the HTML part stripped to text) and its attachments' names.
func (c Client) Read(ctx context.Context, id string) (Message, error) {
	if err := c.ready(); err != nil {
		return Message{}, err
	}
	var m apiMessage
	if err := c.get(ctx, c.base()+"/"+url.PathEscape(id)+"?format=full", &m); err != nil {
		return Message{}, err
	}
	out := Message{ID: id}
	for _, name := range shown {
		if v := oneLine(m.header(name)); v != "" {
			out.Headers = append(out.Headers, Header{Name: name, Value: v})
		}
	}
	var plain, markup string
	var walk func(p apiPart)
	walk = func(p apiPart) {
		switch {
		case p.Filename != "":
			out.Attachments = append(out.Attachments, Attachment{Name: oneLine(p.Filename), MimeType: oneLine(p.MimeType), Size: p.Body.Size})
		case strings.EqualFold(p.MimeType, "text/plain") && plain == "":
			plain = decode(p.Body.Data)
		case strings.EqualFold(p.MimeType, "text/html") && markup == "":
			markup = decode(p.Body.Data)
		}
		for _, sub := range p.Parts {
			walk(sub)
		}
	}
	walk(m.Payload)
	switch {
	case strings.TrimSpace(plain) != "":
		out.Body = clean(plain)
	case markup != "":
		out.Body = clean(StripHTML(markup))
	}
	return out, nil
}

// decode reads Gmail's base64url body data; what it cannot read is empty.
func decode(data string) string {
	raw, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(data)
	}
	if err != nil {
		return ""
	}
	return string(raw)
}

var (
	hiddenBlocks = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)\s*>`)
	comments     = regexp.MustCompile(`(?s)<!--.*?-->`)
	breaks       = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|tr|li|h[1-6]|table|ul|ol|blockquote)\s*>`)
	tags         = regexp.MustCompile(`(?s)<[^>]*>`)
)

// StripHTML is markup as plain text: script, style and head dropped, tags
// removed, line ends kept at breaks and block ends, entities read.
func StripHTML(markup string) string {
	s := hiddenBlocks.ReplaceAllString(markup, "")
	s = comments.ReplaceAllString(s, "")
	s = breaks.ReplaceAllString(s, "\n")
	s = tags.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

// clean is text with control characters dropped (a message must not move his
// terminal), each line's spaces tidied and blank lines taken out.
func clean(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(strings.Map(visible, line)), " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// oneLine is a header value on one line, control characters dropped.
func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.Map(visible, text)), " ")
}

// visible keeps a rune unless it is a control character other than a tab or a
// line end, which become spaces.
func visible(r rune) rune {
	switch {
	case r == '\t' || r == '\n' || r == ' ':
		return ' '
	case unicode.IsControl(r):
		return -1
	}
	return r
}

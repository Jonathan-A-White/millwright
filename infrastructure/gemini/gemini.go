// Package gemini is the Gemini API, narrowed to one thing: one request that
// makes one picture. It has no retry, no batch and no call that touches
// billing. The key goes in the x-goog-api-key header, never in the URL, and no
// error it gives quotes it.
package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// DefaultURL is the Gemini API's address.
const DefaultURL = "https://generativelanguage.googleapis.com"

// maxReply is the most of a reply read: a picture comes back as base64.
const maxReply = 64 << 20

// modelName is what a model id may be: it ends up in a request path.
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,100}$`)

// Maker implements application.ImageMaker over generateContent.
type Maker struct {
	// URL is the API's address, without a path.
	URL string
	// Client makes the request; nil is a client with a 3 minute timeout that
	// follows no redirect.
	Client *http.Client

	model string
	key   string
}

var _ application.ImageMaker = (*Maker)(nil)

// New is the maker at url (DefaultURL for the real one) for a model and a key.
// The error for either that cannot be used never quotes the key.
func New(url, model, key string) (*Maker, error) {
	key = strings.TrimSpace(key)
	if !modelName.MatchString(model) {
		return nil, fmt.Errorf("the Gemini model %q is not a model id (letters, digits, dots, hyphens): set gemini_model in the config file", model)
	}
	if key == "" {
		return nil, errors.New("the Gemini key kept is empty: put the API key again with mw secrets put gemini_api_key")
	}
	return &Maker{URL: strings.TrimRight(url, "/"), model: model, key: key}, nil
}

// apiReply is what generateContent answers, as far as a picture needs.
type apiReply struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string `json:"text"`
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// Make implements application.ImageMaker: one POST, whatever comes of it.
func (m *Maker) Make(ctx context.Context, prompt string) (application.MadeImage, error) {
	body, err := json.Marshal(map[string]any{
		"contents":         []any{map[string]any{"parts": []any{map[string]string{"text": prompt}}}},
		"generationConfig": map[string]any{"responseModalities": []string{"IMAGE"}},
	})
	if err != nil {
		return application.MadeImage{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL+"/v1beta/models/"+m.model+":generateContent", bytes.NewReader(body))
	if err != nil {
		return application.MadeImage{}, m.clean(err)
	}
	req.Header.Set("x-goog-api-key", m.key)
	req.Header.Set("Content-Type", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{
			Timeout: 3 * time.Minute,
			// A redirect would carry the key's header to another place.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return application.MadeImage{}, m.clean(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return application.MadeImage{}, m.clean(fmt.Errorf("reading Gemini's reply: %w", err))
	}
	var reply apiReply
	parsed := json.Unmarshal(raw, &reply) == nil
	if resp.StatusCode/100 != 2 {
		return application.MadeImage{}, m.refused(resp.StatusCode, reply, parsed)
	}
	if !parsed {
		return application.MadeImage{}, errors.New("Gemini sent no picture: its reply was not JSON")
	}
	return m.picture(reply)
}

// refused is the error for a reply that was not a success: one wrapping
// ErrImageQuota when it names quota, billing or a spent resource.
func (m *Maker) refused(status int, reply apiReply, parsed bool) error {
	message := "no message"
	if parsed && reply.Error.Message != "" {
		message = reply.Error.Message
		if len(message) > 300 {
			message = message[:300] + "..."
		}
	}
	message = strings.ReplaceAll(message, m.key, "<key>")
	text := fmt.Sprintf("Gemini refused the request (HTTP %d): %s", status, message)
	lower := strings.ToLower(message)
	if status == http.StatusTooManyRequests || reply.Error.Status == "RESOURCE_EXHAUSTED" ||
		reply.Error.Status == "FAILED_PRECONDITION" || strings.Contains(lower, "quota") ||
		strings.Contains(lower, "billing") || strings.Contains(lower, "free tier") {
		return fmt.Errorf("%s: %w", text, application.ErrImageQuota)
	}
	return m.clean(errors.New(text))
}

// picture is the first picture a reply holds, or the reason it holds none.
func (m *Maker) picture(reply apiReply) (application.MadeImage, error) {
	var said []string
	for _, candidate := range reply.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				data, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return application.MadeImage{}, errors.New("Gemini sent no picture: its image data was not base64")
				}
				return application.MadeImage{Data: data, MimeType: part.InlineData.MimeType}, nil
			}
			if part.Text != "" {
				said = append(said, part.Text)
			}
		}
		if candidate.FinishReason != "" && candidate.FinishReason != "STOP" {
			said = append(said, "finish reason "+candidate.FinishReason)
		}
	}
	if reply.PromptFeedback.BlockReason != "" {
		said = append(said, "the prompt was blocked: "+reply.PromptFeedback.BlockReason)
	}
	why := strings.Join(said, "; ")
	if why == "" {
		why = "its reply held none"
	}
	if len(why) > 300 {
		why = why[:300] + "..."
	}
	return application.MadeImage{}, m.clean(fmt.Errorf("Gemini sent no picture: %s", why))
}

// clean is err with the key cut out of its text.
func (m *Maker) clean(err error) error {
	text := err.Error()
	if !strings.Contains(text, m.key) {
		return err
	}
	return errors.New(strings.ReplaceAll(text, m.key, "<key>"))
}

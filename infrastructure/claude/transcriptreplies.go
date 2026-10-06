package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// Transcripts satisfies the replies port too.
var _ application.TranscriptReplies = (*Transcripts)(nil)

// apiErrorPrefix begins the text the harness records when a request failed.
const apiErrorPrefix = "API Error"

// maxReplyText is the longest a reply's text is kept.
const maxReplyText = 120

// syntheticModel is the model name the harness gives a message it wrote
// itself, an interruption notice say, rather than one the API answered.
const syntheticModel = "<synthetic>"

// replyEntry is the part of a transcript line Replies reads.
type replyEntry struct {
	IsSidechain       bool   `json:"isSidechain"`
	IsAPIErrorMessage bool   `json:"isApiErrorMessage"`
	Type              string `json:"type"`
	Timestamp         string `json:"timestamp"`
	Message           struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// Replies implements application.TranscriptReplies: the assistant entries of
// the newest session run in dir, at or after since. An entry the harness
// wrote about a failed request — flagged isApiErrorMessage, or whose text
// begins "API Error" — is an API error; one the harness wrote itself and that
// is no error (an interruption notice) is no reply at all.
//
// Lines that are not JSON are skipped, as the last of a session still being
// written may be cut short, and so are a subagent's replies and entries with
// no readable time.
func (t *Transcripts) Replies(_ context.Context, dir string, since time.Time) ([]application.HarnessReply, error) {
	newest := newestTranscript(ProjectDir(t.root, dir))
	if newest == "" {
		return nil, nil
	}
	file, err := os.Open(newest)
	if err != nil {
		return nil, fmt.Errorf("reading the transcript %s: %w", newest, err)
	}
	defer file.Close()

	var replies []application.HarnessReply
	reader := bufio.NewReaderSize(file, 1<<20)
	for {
		raw, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(raw)) > 0 {
			var seen replyEntry
			if json.Unmarshal(raw, &seen) == nil && !seen.IsSidechain && seen.Type == "assistant" {
				if reply, ok := replyOf(seen); ok && !reply.At.Before(since) {
					replies = append(replies, reply)
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	return replies, nil
}

// replyOf is the reply an assistant entry records, and whether it is one.
func replyOf(e replyEntry) (application.HarnessReply, bool) {
	at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
	if err != nil {
		return application.HarnessReply{}, false
	}
	text := firstText(e.Message.Content)
	apiError := e.IsAPIErrorMessage || strings.HasPrefix(text, apiErrorPrefix)
	if e.Message.Model == syntheticModel && !apiError {
		return application.HarnessReply{}, false
	}
	return application.HarnessReply{At: at, APIError: apiError, Text: text}, true
}

// firstText is the first line of the first text in a message's content, cut
// to maxReplyText; "" when it has none.
func firstText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) != nil {
		var blocks []block
		if json.Unmarshal(content, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				text = b.Text
				break
			}
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if runes := []rune(line); len(runes) > maxReplyText {
		line = string(runes[:maxReplyText]) + "…"
	}
	return line
}

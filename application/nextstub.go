package application

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// StubCommentBytes is the size under which a comment after a story's newest
// commit is a stub, not a closing comment (mw-gq6.1045).
const StubCommentBytes = 40

// StubCommentCure is what a stub closing comment is told: bd 1.3.0 reads no
// stdin dash, so 'bd comments add <id> -' posts the text '-'.
const StubCommentCure = "bd comments add takes -f <file>, not -"

// StubHeldSaid is what the HELD: comment of a story held for a stub closing
// comment adds to why it is held.
const StubHeldSaid = "Its work is done and committed; the Mayor writes the closing comment and releases it."

// quoteStub is a stub comment between single quotes, as the refusal quotes it.
func quoteStub(text string) string { return "'" + strings.TrimSpace(text) + "'" }

// byteCount is n with its unit: '1 byte', '3 bytes'.
func byteCount(n int) string {
	if n == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", n)
}

// stubCommentOnStory is the newest comment written after the newest commit of
// the story's branch when it is under StubCommentBytes, and whether there is
// one. A read that failed is the error, not a no.
func (n Next) stubCommentOnStory(ctx context.Context, c *closeOut, report *NextReport, base string) (string, bool, error) {
	committed, comments, err := n.newestCommitAndComments(ctx, c, report, base)
	if err != nil {
		return "", false, err
	}
	var newest time.Time
	var text string
	found := false
	for _, comment := range comments {
		if comment.Created.After(committed) && !comment.Created.Before(newest) {
			newest, text, found = comment.Created, strings.TrimSpace(comment.Text), true
		}
	}
	if !found || len(text) >= StubCommentBytes {
		return "", false, nil
	}
	return text, true, nil
}

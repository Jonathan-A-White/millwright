package application_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// TestAIAttributionFindsTheLineThatSignsTheWork pins the rule mw next refuses a
// branch by. It is a pure reading of a commit message: no git, no tracker,
// nothing on disk.
func TestAIAttributionFindsTheLineThatSignsTheWork(t *testing.T) {
	signed := map[string]string{
		"a trailer as Claude Code writes it": "Do the thing\n\nCo-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>",
		"the same trailer lower-cased":       "Do the thing\n\nco-authored-by: Claude <noreply@anthropic.com>",
		"the same trailer shouted":           "Do the thing\n\nCO-AUTHORED-BY: Claude <noreply@anthropic.com>",
		"a trailer with space before it":     "Do the thing\n\n  Co-Authored-By: Claude <noreply@anthropic.com>",
		"a human co-author":                  "Do the thing\n\nCo-Authored-By: Jonathan <jonathan@example.com>",
		"the pull request line":              "Do the thing\n\n\U0001F916 Generated with [Claude Code](https://claude.com/claude-code)",
		"that line lower-cased":              "Do the thing\n\ngenerated with claude code",
		"that line in the subject":           "Generated with a model, landed by nobody",
	}
	for name, message := range signed {
		if line := application.AIAttribution(message); line == "" {
			t.Errorf("%s: expected the message to be refused, it was not:\n%s", name, message)
		}
	}

	honest := map[string]string{
		"a plain message":                "Do the thing\n\nBecause it needed doing.",
		"an empty message":               "",
		"a message naming the seat":      "Do the thing\n\nWorked by the builder seat on vps.",
		"a message about attribution":  "Refuse a branch whose commits carry a Co-Authored-By trailer",
		"a trailer that is not a line": "Do the thing (the Co-Authored-By trailer is what we refuse)",
	}
	for name, message := range honest {
		if line := application.AIAttribution(message); line != "" {
			t.Errorf("%s: expected the message to pass, it was refused for %q", name, line)
		}
	}
}

// TestAIAttributionSaysWhichLine is the part a person reads: the refusal has to
// quote the offending line back, not just say there was one.
func TestAIAttributionSaysWhichLine(t *testing.T) {
	message := "Do the thing\n\nBecause it needed doing.\n\nCo-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>\n"
	want := "Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>"
	if got := application.AIAttribution(message); got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

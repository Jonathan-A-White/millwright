package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// PublishedTags is what became of the release tags a landing carries to the
// remote (mw-gq6.673). Each list holds tag names.
type PublishedTags struct {
	// Pushed are the tags that point at a commit of the landed range and were
	// missing from the remote: now there, at the commit they were made on.
	Pushed []string
	// Elsewhere are the tags of the range that the remote already has at
	// another commit. They are neither moved nor forced.
	Elsewhere []string
	// Missing are the version tags the story's acceptance criteria name that
	// are not on the remote after the landing.
	Missing []string
	// Failed are the lines saying a tag could not be pushed, and why.
	Failed []string
}

// Any says there is something to tell.
func (p PublishedTags) Any() bool {
	return len(p.Pushed)+len(p.Elsewhere)+len(p.Missing)+len(p.Failed) > 0
}

// Lines are the report's lines for the tags, each opening with two spaces.
func (p PublishedTags) Lines() []string {
	var lines []string
	for _, tag := range p.Pushed {
		lines = append(lines, fmt.Sprintf("  tag     %s pushed to origin", tag))
	}
	for _, tag := range p.Elsewhere {
		lines = append(lines, fmt.Sprintf("  tag     %s on origin points elsewhere: not moved, not forced", tag))
	}
	for _, said := range p.Failed {
		lines = append(lines, "  tag     "+said)
	}
	for _, tag := range p.Missing {
		lines = append(lines, fmt.Sprintf("  %s", TagMissingLine(tag)))
	}
	return lines
}

// TagMissingLine is the warning a Landed mail carries for a version tag the
// story's acceptance criteria name that is not on the remote.
func TagMissingLine(tag string) string {
	return fmt.Sprintf("WARN: tag %s named in the AC is not on origin", tag)
}

// ForStory is p for one story of a batch: the tags are the batch's, and only
// the version tags that story's own criteria name can be missing for it.
func (p PublishedTags) ForStory(acceptance string) PublishedTags {
	named := ACVersionTags(acceptance)
	missing := p.Missing
	p.Missing = nil
	for _, tag := range missing {
		if containsString(named, tag) {
			p.Missing = append(p.Missing, tag)
		}
	}
	return p
}

// TagPublisher is the part of a Landing that carries release tags to the
// remote. It is a port of its own so that a landing that has no use for it
// need not have it.
type TagPublisher interface {
	// PublishTags pushes, from the landing worktree, every tag that points at
	// a commit of base..HEAD and is not on the remote, and never moves or
	// forces a tag the remote has at another commit. named are version tags
	// the story's criteria name: those not on the remote afterwards are
	// Missing. A tag that cannot be pushed is a line of Failed, not an error.
	PublishTags(ctx context.Context, landingDir, remote, base string, named []string) (PublishedTags, error)
}

var versionTag = regexp.MustCompile(`\bv\d+\.\d+\.\d+\b`)

// ACVersionTags are the version tags (v<major>.<minor>.<patch>) the acceptance
// criteria name, each once, in the order they are named.
func ACVersionTags(acceptance string) []string {
	var tags []string
	for _, tag := range versionTag.FindAllString(acceptance, -1) {
		if !containsString(tags, tag) {
			tags = append(tags, tag)
		}
	}
	return tags
}

func containsString(list []string, s string) bool {
	for _, have := range list {
		if have == s {
			return true
		}
	}
	return false
}

// publishTags carries the release tags of a landing's range to the remote,
// after its branch has been pushed, and keeps the answer on the report. A
// failure to do so is a note: the landing stands.
func (n Next) publishTags(ctx context.Context, report *NextReport, dir, base string, named []string) PublishedTags {
	publisher, ok := n.Landing.(TagPublisher)
	if !ok {
		return PublishedTags{}
	}
	got, err := publisher.PublishTags(ctx, dir, n.remote(), base, named)
	if err != nil {
		report.Notes = append(report.Notes, fmt.Sprintf("the tags of the landing could not be published: %s", firstLine(err.Error())))
		return PublishedTags{}
	}
	if len(got.Pushed) > 0 {
		report.Notes = append(report.Notes, "pushed tag(s) "+strings.Join(got.Pushed, ", "))
	}
	return got
}

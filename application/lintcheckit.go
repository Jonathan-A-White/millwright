package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// ErrNoSource is what SourceText.Contains answers for a directory that is not
// there: a rig with no src/ cannot say a label is on its screen.
var ErrNoSource = errors.New("no source directory")

// SourceText searches a rig's source. The real adapter is cmd/mw's
// sourceFiles, the fake a map in the tests.
type SourceText interface {
	// Contains is whether text, as a fixed string, is in any file under dir;
	// ErrNoSource when dir is not a directory.
	Contains(dir, text string) (bool, error)
}

// LintCheckItBeads is what mw lint-checkit reads of the tracker.
type LintCheckItBeads = VerifyDeployBeads

// LintCheckIt is mw lint-checkit <bead> (mw-gq6.764): HOW TO CHECK IT steps
// quote the labels on the Governor's screen, and a label written from a guess
// is a step he cannot follow. It reads the newest HOW TO CHECK IT (a line that begins with the words) of the bead's
// comments, takes each label quoted in it ("..." or backticks) and looks for
// the label as a fixed string under the rig's src/. It writes nothing.
type LintCheckIt struct {
	Beads LintCheckItBeads
	Src   SourceText
	// Rigs are the rigs checked out on this host, by name.
	Rigs map[string]string
	// Dir, when set, is the checkout to search instead of the rig's: a
	// Builder's worktree holds the labels its story adds.
	Dir string
	Out io.Writer
}

// LintCheckItResult is the labels a HOW TO CHECK IT quotes and the ones the
// source lacks.
type LintCheckItResult struct {
	Quoted, Missing []string
}

// Failed is whether a quoted label is not in the source.
func (r LintCheckItResult) Failed() bool { return len(r.Missing) > 0 }

// Run lints one bead. The error is for a run that could not be made: no such
// bead, no rig checkout, no src/ to search; labels not found are in the result.
func (l LintCheckIt) Run(ctx context.Context, id string) (LintCheckItResult, error) {
	comments, err := l.Beads.StoriesComments(ctx, []string{id})
	if err != nil {
		return LintCheckItResult{}, fmt.Errorf("mw lint-checkit: reading the comments of %s: %w", id, err)
	}
	section, read := howToCheckItFrom(comments[id])
	if section == "" {
		fmt.Fprintf(l.Out, "lint-checkit %s: no HOW TO CHECK IT on the bead, nothing to check\n", id)
		return LintCheckItResult{}, nil
	}
	result := LintCheckItResult{Quoted: quotedLabels(section)}
	fmt.Fprintf(l.Out, "lint-checkit %s: read the HOW TO CHECK IT of the comment by %s at %s\n", id, commentAuthor(read), commentTime(read))
	if len(result.Quoted) == 0 {
		fmt.Fprintf(l.Out, "lint-checkit %s: HOW TO CHECK IT quotes no label, nothing to check\n", id)
		return result, nil
	}
	src, err := l.sourceDir(ctx, id)
	if err != nil {
		return result, err
	}
	for _, label := range result.Quoted {
		found, err := l.Src.Contains(src, label)
		if err != nil {
			return result, fmt.Errorf("mw lint-checkit: searching %s for %q: %w", src, label, err)
		}
		if !found {
			result.Missing = append(result.Missing, label)
		}
	}
	found := len(result.Quoted) - len(result.Missing)
	if result.Failed() {
		fmt.Fprintf(l.Out, "lint-checkit %s: %d quoted labels, %d found in src/, not found in src/: %s\n",
			id, len(result.Quoted), found, strings.Join(result.Missing, ", "))
		return result, nil
	}
	fmt.Fprintf(l.Out, "lint-checkit %s: %d quoted labels, %d found in src/\n", id, len(result.Quoted), found)
	return result, nil
}

// commentAuthor and commentTime say which comment a section was read from;
// "unknown" when the tracker gave none.
func commentAuthor(c Comment) string {
	if strings.TrimSpace(c.Author) == "" {
		return "unknown"
	}
	return c.Author
}

func commentTime(c Comment) string {
	if c.Created.IsZero() {
		return "unknown time"
	}
	return c.Created.UTC().Format("2006-01-02T15:04:05Z")
}

// sourceDir is the src/ of the checkout to search: Dir, else the bead's rig.
func (l LintCheckIt) sourceDir(ctx context.Context, id string) (string, error) {
	if l.Dir != "" {
		return filepath.Join(l.Dir, "src"), nil
	}
	found, err := l.Beads.ShowBeads(ctx, []string{id})
	if err != nil {
		return "", fmt.Errorf("mw lint-checkit: reading %s: %w", id, err)
	}
	if len(found) != 1 {
		return "", fmt.Errorf("mw lint-checkit: the tracker has no bead %s", id)
	}
	rig := strings.TrimSpace(found[0].Merged().Rig)
	if rig == "" {
		return "", fmt.Errorf("mw lint-checkit: %s names no rig in its path", id)
	}
	dir := l.Rigs[rig]
	if dir == "" {
		return "", fmt.Errorf("mw lint-checkit: rig %s has no checkout on this host", rig)
	}
	return filepath.Join(dir, "src"), nil
}

// quotedLabels are the labels the text quotes, in order, each once: a span
// between straight or curly double quotes, or between backticks, on one line.
// A single quote is no mark, since an apostrophe is one.
func quotedLabels(text string) []string {
	closers := map[rune]rune{'"': '"', '“': '”', '`': '`'}
	var labels []string
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			closer, opens := closers[runes[i]]
			if !opens {
				continue
			}
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == closer {
					end = j
					break
				}
			}
			if end < 0 {
				continue
			}
			label := strings.TrimSpace(string(runes[i+1 : end]))
			i = end
			if label == "" || containsString(labels, label) {
				continue
			}
			labels = append(labels, label)
		}
	}
	return labels
}

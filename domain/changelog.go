package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// The kinds of a changelog entry, as public/changelog.json spells them.
const (
	ChangelogNew   = "new"
	ChangelogFixed = "fixed"
)

// ChangelogTitle heads the markdown changelog.
const ChangelogTitle = "# What's new"

// WhatsNewMarker starts the line of a closing comment that says what a story
// changed for the people who use the app.
const WhatsNewMarker = "What's new:"

// ChangelogEntry is one landing's note in the app's changelog.
type ChangelogEntry struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Story   string `json:"story"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
}

// WhatsNew is what a closing comment's What's new: line says: None for a change
// nobody using the app can see, else the sentence and, when the line gave one,
// its kind (ChangelogNew or ChangelogFixed; empty when it did not).
type WhatsNew struct {
	Kind string
	Text string
	None bool
}

// ParseWhatsNew reads the What's new: line of one comment, the last such line
// when it has several. A comment with no such line, or an empty one, has none.
func ParseWhatsNew(comment string) (WhatsNew, bool) {
	var found WhatsNew
	ok := false
	for _, line := range strings.Split(comment, "\n") {
		rest, has := afterMarker(line)
		if !has {
			continue
		}
		rest = strings.Trim(rest, " \t*_`")
		if rest == "" {
			continue
		}
		if strings.EqualFold(strings.Trim(rest, " .*_`"), "none") {
			found, ok = WhatsNew{None: true}, true
			continue
		}
		note := WhatsNew{Text: strings.Join(strings.Fields(rest), " ")}
		for kind, word := range map[string]string{ChangelogNew: "new:", ChangelogFixed: "fixed:"} {
			if len(rest) > len(word) && strings.EqualFold(rest[:len(word)], word) {
				note.Kind = kind
				note.Text = strings.Join(strings.Fields(rest[len(word):]), " ")
			}
		}
		if note.Text == "" {
			continue
		}
		found, ok = note, true
	}
	return found, ok
}

// afterMarker is what follows the What's new: marker on a line, in any case and
// with either apostrophe.
func afterMarker(line string) (string, bool) {
	line = strings.ReplaceAll(line, "’", "'")
	at := strings.Index(strings.ToLower(line), strings.ToLower(WhatsNewMarker))
	if at < 0 {
		return "", false
	}
	return line[at+len(WhatsNewMarker):], true
}

// NewestWhatsNew reads the newest of the comments (given oldest first) that
// holds a What's new: line.
func NewestWhatsNew(comments []string) (WhatsNew, bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		if note, found := ParseWhatsNew(comments[i]); found {
			return note, true
		}
	}
	return WhatsNew{}, false
}

// NoteFromTitle is a story's title as the changelog's sentence when its closing
// comment gave none: without the tags it opens with ([bug], [<rig>]) or a
// leading "<rig>:", on one line.
func NoteFromTitle(title, rig string) string {
	title = strings.Join(strings.Fields(title), " ")
	for {
		switch {
		case strings.HasPrefix(title, "["):
			end := strings.Index(title, "]")
			if end < 0 {
				return title
			}
			title = strings.TrimSpace(title[end+1:])
		case rig != "" && strings.HasPrefix(strings.ToLower(title), strings.ToLower(rig)+":"):
			title = strings.TrimSpace(title[len(rig)+1:])
		default:
			return title
		}
	}
}

// AddToChangelogJSON puts the entry at the top of the JSON array that existing
// holds, newest first, and returns the whole file. An absent or blank file is
// created; one that is not an array of entries is an error.
func AddToChangelogJSON(existing []byte, entry ChangelogEntry) ([]byte, error) {
	entries := []ChangelogEntry{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &entries); err != nil {
			return nil, fmt.Errorf("it is not a JSON array of entries: %w", err)
		}
	}
	entries = append([]ChangelogEntry{entry}, entries...)
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// AddToChangelogMarkdown puts the entry at the top of the markdown changelog,
// just under its title, and returns the whole file. An absent or blank file is
// created, and one without the title keeps what it holds below the entry.
func AddToChangelogMarkdown(existing []byte, entry ChangelogEntry) []byte {
	kind := "New"
	if entry.Kind == ChangelogFixed {
		kind = "Fixed"
	}
	block := fmt.Sprintf("## %s\n_%s_\n- %s: %s\n", entry.Version, entry.Date, kind, entry.Text)

	rest := string(existing)
	if first, after, _ := strings.Cut(rest, "\n"); strings.TrimSpace(first) == ChangelogTitle {
		rest = after
	}
	rest = strings.TrimLeft(rest, "\n")
	out := ChangelogTitle + "\n\n" + block
	if strings.TrimSpace(rest) != "" {
		out += "\n" + rest
	}
	return []byte(out)
}

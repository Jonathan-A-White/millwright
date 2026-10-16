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
// with either apostrophe. The marker counts only at the start of the line,
// after leading spaces, bullet and emphasis marks; mid-sentence it is prose
// (mw's own refusal comments quote it), not a note.
func afterMarker(line string) (string, bool) {
	line = strings.ReplaceAll(line, "’", "'")
	line = strings.TrimLeft(line, " \t-*+•_`>")
	if len(line) < len(WhatsNewMarker) || !strings.EqualFold(line[:len(WhatsNewMarker)], WhatsNewMarker) {
		return "", false
	}
	return line[len(WhatsNewMarker):], true
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
// created; one that is not an array of entries is an error. When the newest
// entry already carries the entry's story or its text (a Builder seeded the
// story's note), it is replaced by the entry, not written a second time; so is
// any entry for the entry's own version and story (mw-gq6.514).
func AddToChangelogJSON(existing []byte, entry ChangelogEntry) ([]byte, error) {
	entries, err := changelogEntries(existing)
	if err != nil {
		return nil, err
	}
	if len(entries) > 0 && (entries[0].Story == entry.Story && entry.Story != "" || entries[0].Text == entry.Text) {
		entries = entries[1:]
	}
	kept := []ChangelogEntry{entry}
	for _, e := range entries {
		if entry.Story == "" || e.Version != entry.Version || e.Story != entry.Story {
			kept = append(kept, e)
		}
	}
	return encodeChangelog(kept)
}

// TidyChangelogJSON takes out of the JSON changelog every entry for a version
// and story that an entry above it already has, and names each it took out as
// "<version> <story>". A changelog with none is returned as it was.
func TidyChangelogJSON(existing []byte) ([]byte, []string, error) {
	entries, err := changelogEntries(existing)
	if err != nil {
		return nil, nil, err
	}
	seen := map[[2]string]bool{}
	var kept []ChangelogEntry
	var removed []string
	for _, e := range entries {
		key := [2]string{e.Version, e.Story}
		if e.Story != "" && seen[key] {
			removed = append(removed, e.Version+" "+e.Story)
			continue
		}
		seen[key] = true
		kept = append(kept, e)
	}
	if len(removed) == 0 {
		return existing, nil, nil
	}
	tidied, err := encodeChangelog(kept)
	return tidied, removed, err
}

// changelogEntries reads a JSON changelog; an absent or blank one holds none.
func changelogEntries(existing []byte) ([]ChangelogEntry, error) {
	entries := []ChangelogEntry{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &entries); err != nil {
			return nil, fmt.Errorf("it is not a JSON array of entries: %w", err)
		}
	}
	return entries, nil
}

func encodeChangelog(entries []ChangelogEntry) ([]byte, error) {
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
// just above its first "## " heading, and returns the whole file. Whatever
// stands before that heading (a rig's own title and intro) is kept as it is. An
// absent or blank file is created with ChangelogTitle; a file that opens with a
// "# " title but has no "## " heading yet gets the entry after what it holds;
// one with neither keeps what it holds below the entry, under ChangelogTitle.
// A version has one block, under one heading and one date: when the newest
// block is the entry's version already, the entry's line goes at the top of its
// lines, or in place of the block's line saying its text (mw-gq6.514), and the
// block takes the entry's date, the landing's own. When the newest block,
// of an older version, holds a line with the entry's text (a Builder seeded the
// story's note), that line is taken out of it, the block with it when it has no
// other, and the entry written above.
func AddToChangelogMarkdown(existing []byte, entry ChangelogEntry) []byte {
	kind := "New"
	if entry.Kind == ChangelogFixed {
		kind = "Fixed"
	}
	line := fmt.Sprintf("- %s: %s", kind, entry.Text)
	block := fmt.Sprintf("## %s\n_%s_\n%s\n", entry.Version, entry.Date, line)

	text := string(existing)
	head, rest := text, ""
	if at := firstVersionHeading(text); at >= 0 {
		head, rest = text[:at], text[at:]
		blocks := versionBlocks(rest)
		newest := blocks[0]
		switch {
		case blockHeading(newest) == "## "+entry.Version:
			blocks[0] = withLine(newest, line, entry.Date, entry.Text)
			return []byte(head + strings.Join(blocks, ""))
		case blockSays(newest, entry.Text):
			if blocks[0] = withoutLine(newest, entry.Text); blocks[0] == "" {
				blocks = blocks[1:]
			}
			rest = strings.Join(blocks, "")
			text = head + rest
		}
	}
	if strings.TrimSpace(head) != "" && (rest != "" || strings.HasPrefix(strings.TrimSpace(head), "# ")) {
		out := strings.TrimRight(head, "\n") + "\n\n" + block
		if rest != "" {
			out += "\n" + rest
		}
		return []byte(out)
	}

	// No title of the rig's own: ours heads the file. A leading ChangelogTitle
	// line already in it is not written twice.
	if first, after, _ := strings.Cut(text, "\n"); strings.TrimSpace(first) == ChangelogTitle {
		text = after
	}
	text = strings.TrimLeft(text, "\n")
	out := ChangelogTitle + "\n\n" + block
	if strings.TrimSpace(text) != "" {
		out += "\n" + text
	}
	return []byte(out)
}

// TidyChangelogMarkdown folds the blocks of one version that stand one after
// another into the first of them: its heading and its date line, then every
// line of the others it does not hold already. It names each version it folded;
// a changelog with none is returned as it was.
func TidyChangelogMarkdown(existing []byte) ([]byte, []string) {
	text := string(existing)
	at := firstVersionHeading(text)
	if at < 0 {
		return existing, nil
	}
	var kept []string
	var folded []string
	for _, block := range versionBlocks(text[at:]) {
		last := len(kept) - 1
		if last < 0 || blockHeading(kept[last]) != blockHeading(block) {
			kept = append(kept, block)
			continue
		}
		if version := strings.TrimPrefix(blockHeading(block), "## "); len(folded) == 0 || folded[len(folded)-1] != version {
			folded = append(folded, version)
		}
		kept[last] = foldBlock(kept[last], block)
	}
	if len(folded) == 0 {
		return existing, nil
	}
	return []byte(text[:at] + strings.Join(kept, "")), folded
}

// versionBlocks cuts the markdown changelog from its first "## " heading on into
// its version blocks, each from its heading to the next, blank lines and all.
func versionBlocks(rest string) []string {
	var blocks []string
	for {
		next := firstVersionHeading(rest[len("## "):])
		if next < 0 {
			return append(blocks, rest)
		}
		blocks = append(blocks, rest[:len("## ")+next])
		rest = rest[len("## ")+next:]
	}
}

// blockHeading is a version block's heading line.
func blockHeading(block string) string {
	heading, _, _ := strings.Cut(block, "\n")
	return strings.TrimSpace(heading)
}

// blockParts cuts a version block into its heading and date lines (the date
// empty when the block has none), its other lines without the blank ones at
// the end, and the blank lines that end it.
func blockParts(block string) (top []string, lines []string, tail string) {
	body := strings.TrimRight(block, "\n")
	tail = block[len(body):]
	all := strings.Split(body, "\n")
	cut := 1
	if len(all) > 1 && isDateLine(all[1]) {
		cut = 2
	}
	// Copies, so that what is added to one never writes over the other.
	top = append([]string(nil), all[:cut]...)
	lines = append([]string(nil), all[cut:]...)
	return top, lines, tail
}

func isDateLine(line string) bool {
	line = strings.TrimSpace(line)
	return len(line) > 2 && strings.HasPrefix(line, "_") && strings.HasSuffix(line, "_")
}

// withLine puts line in a version block: in place of the "- New:" or
// "- Fixed:" line saying text, else at the top of its lines, below its date,
// which is made date.
func withLine(block, line, date, text string) string {
	top, lines, tail := blockParts(block)
	top = append(top[:1], "_"+date+"_")
	said := false
	for i, had := range lines {
		if blockSays(had, text) {
			lines[i], said = line, true
		}
	}
	if !said {
		lines = append([]string{line}, lines...)
	}
	return strings.Join(append(top, lines...), "\n") + tail
}

// withoutLine takes the "- New:" or "- Fixed:" line saying text out of a
// version block; it is empty when no line of the block's own is left.
func withoutLine(block, text string) string {
	top, lines, tail := blockParts(block)
	var kept []string
	left := false
	for _, line := range lines {
		if blockSays(line, text) {
			continue
		}
		kept = append(kept, line)
		left = left || strings.TrimSpace(line) != ""
	}
	if !left {
		return ""
	}
	return strings.Join(append(top, kept...), "\n") + tail
}

// foldBlock puts the lines of a later block of the same version below the
// lines of the first, but for its heading, its date and any line the first
// has already.
func foldBlock(first, later string) string {
	top, lines, _ := blockParts(first)
	_, more, tail := blockParts(later)
	held := map[string]bool{}
	for _, line := range lines {
		held[strings.TrimSpace(line)] = true
	}
	for _, line := range more {
		if said := strings.TrimSpace(line); said != "" && !held[said] {
			lines = append(lines, line)
			held[said] = true
		}
	}
	return strings.Join(append(top, lines...), "\n") + tail
}

// firstVersionHeading is the offset of the first line that starts "## ", or -1.
func firstVersionHeading(text string) int {
	if strings.HasPrefix(text, "## ") {
		return 0
	}
	if at := strings.Index(text, "\n## "); at >= 0 {
		return at + 1
	}
	return -1
}

// blockSays reports whether a version block of the markdown changelog has a
// "- New: <text>" or "- Fixed: <text>" line saying text.
func blockSays(block, text string) bool {
	for _, line := range strings.Split(block, "\n") {
		for _, kind := range []string{"New", "Fixed"} {
			if strings.TrimSpace(line) == fmt.Sprintf("- %s: %s", kind, text) {
				return true
			}
		}
	}
	return false
}

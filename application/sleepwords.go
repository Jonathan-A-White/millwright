package application

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A message of his that came with a picture and words is words. The agent reads
// them in evidence.txt, but it may still call the message wordless, or say he
// sent no reply; the pass checks every such claim in questions.md against the
// inbox store, where his words are kept whole, before it writes the file
// (mw-gq6.1345).

// wordlessClaim finds the phrases that say a message of his had no words: the
// claim the pass checks. A claim that names no message cannot be checked, and
// is said so.
var wordlessClaim = regexp.MustCompile(`(?i)\bwordless\b|\bno reply\b|\bno words\b|\bwithout (?:any )?words\b`)

// namedMessage finds a txid, or the name of a saved file, in a line: direct:<hash>
// or direct-<hash>, with a part number after it, or a bare hash of 16 or more
// hex digits.
var namedMessage = regexp.MustCompile(`(?i)\bdirect[:-]([0-9a-f]+)(?:-\d+)?|\b([0-9a-f]{16,})(?:-\d+)?\b`)

// wordsOfMessage is the words he sent in the message whose txid begins with
// token (in full, or in 8 digits or more) in one of dirs: the text the inbox kept
// whole, or the .txt it saved. ok is false when none holds words.
func wordsOfMessage(dirs []string, token string) (txid, words string, ok bool) {
	token = strings.ToLower(token)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			stem, found := strings.CutSuffix(name, ".message.json")
			if !found {
				stem, found = strings.CutSuffix(name, ".txt")
			}
			if !found || e.IsDir() {
				continue
			}
			hash := strings.TrimPrefix(stem, "direct-")
			if hash != token && (len(token) < 8 || !strings.HasPrefix(hash, token)) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			text := string(raw)
			if strings.HasSuffix(name, ".message.json") {
				var kept keptMessage
				if json.Unmarshal(raw, &kept) != nil {
					continue
				}
				text = kept.Text
			}
			if text = strings.TrimSpace(text); text != "" {
				return txidOfStem(stem), text, true
			}
		}
	}
	return "", "", false
}

// checkWordlessClaims is questions with each line that calls a message of his
// wordless, or says it had no reply, checked against the inbox store in dirs: a
// line naming a message whose words are kept is replaced by those words, and a
// line naming none is kept with a note that mw could not check it. It says each
// line it changed.
func checkWordlessClaims(questions string, dirs []string) (checked string, said []string) {
	lines := strings.Split(questions, "\n")
	for n, line := range lines {
		if !wordlessClaim.MatchString(line) {
			continue
		}
		named := namedMessage.FindAllStringSubmatch(line, -1)
		for _, m := range named {
			token := m[1]
			if token == "" {
				token = m[2]
			}
			if txid, words, ok := wordsOfMessage(dirs, token); ok {
				lines[n] = fmt.Sprintf("- mw corrected a line here: the inbox holds his words for %s: %q. (The line it replaced was the agent's, and it said these words had not come.)", txid, cutBytes(strings.Join(strings.Fields(words), " "), 400))
				said = append(said, "questions.md: a line said "+txid+" came with no words; the inbox holds them")
				break
			}
		}
		if len(named) == 0 {
			lines[n] = line + " (mw could not check this against the inbox: the line names no message)"
			said = append(said, "questions.md: a claim of no words names no message, kept unchecked")
		}
	}
	return strings.Join(lines, "\n"), said
}

// sleepInboxDirs are where his messages are read from for the claims check:
// the pass's own inbox dir and the dir of each file of the evidence it gave.
func (p SleepPass) sleepInboxDirs(bundle SleepBundle) []string {
	var dirs []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		for _, d := range dirs {
			if d == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	add(p.InboxDir)
	for _, part := range bundle {
		if part.Verify == nil {
			continue
		}
		for _, e := range part.Verify.Evidence {
			for _, path := range e.Files {
				add(filepath.Dir(path))
			}
		}
	}
	return dirs
}

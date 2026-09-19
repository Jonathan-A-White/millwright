package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Transcripts reads the transcripts Claude Code keeps of its sessions. It is
// the adapter behind application.Transcripts, and the one place the factory
// reaches into ~/.claude: it reads and never writes, and it costs no fuel.
//
// Claude Code keeps one directory per working directory under its projects
// root, and one `<session id>.jsonl` file in it per session.
type Transcripts struct {
	root string
}

// Transcripts satisfies the port.
var _ application.Transcripts = (*Transcripts)(nil)

// NewTranscripts returns the adapter reading the projects directory at root.
// The root is given rather than found so that a test can put fixtures in a
// temporary directory and never read the machine's own transcripts.
func NewTranscripts(root string) *Transcripts { return &Transcripts{root: root} }

// DefaultProjectsRoot is where Claude Code keeps its projects on this host.
func DefaultProjectsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("there is no home directory to find Claude Code's transcripts in: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// ProjectDir is the directory under root that holds the transcripts of
// sessions run in dir. Claude Code names it after the directory with every
// character that is not a letter or a digit turned into a hyphen, so that
// /root/.mw-worktrees/mw-1.2 is -root--mw-worktrees-mw-1-2.
func ProjectDir(root, dir string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '-'
	}, dir)
	return filepath.Join(root, name)
}

// usage is the part of a transcript line this adapter reads.
type usage struct {
	IsSidechain bool `json:"isSidechain"`
	Message     struct {
		Usage *struct {
			Input         int `json:"input_tokens"`
			CacheRead     int `json:"cache_read_input_tokens"`
			CacheCreation int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// LiveContext implements application.Transcripts: the size of the context the
// newest session's last assistant turn was given.
//
// Lines that are not JSON are skipped — the last line of a session still being
// written may be cut short — and so are a subagent's turns, which have a
// context of their own.
func (t *Transcripts) LiveContext(ctx context.Context, dir string) (application.TranscriptContext, error) {
	project := ProjectDir(t.root, dir)

	newest := newestTranscript(project)
	if newest == "" {
		return application.TranscriptContext{}, fmt.Errorf("no transcript under %s: Claude Code has recorded no session run in %s", project, dir)
	}

	file, err := os.Open(newest)
	if err != nil {
		return application.TranscriptContext{}, fmt.Errorf("reading the transcript in %s: %w", project, err)
	}
	defer file.Close()

	var last *usage
	lines := bufio.NewReaderSize(file, 1<<20)
	for {
		line, readErr := lines.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var seen usage
			if json.Unmarshal(line, &seen) == nil && seen.Message.Usage != nil && !seen.IsSidechain {
				last = &seen
			}
		}
		if readErr != nil {
			break
		}
	}
	if last == nil {
		return application.TranscriptContext{}, fmt.Errorf("no assistant turn recorded yet in %s: looked in %s", newest, project)
	}

	u := last.Message.Usage
	return application.TranscriptContext{
		SessionID: strings.TrimSuffix(filepath.Base(newest), ".jsonl"),
		Tokens:    u.Input + u.CacheRead + u.CacheCreation,
	}, nil
}

// newestTranscript is the most recently modified transcript in a project
// directory, or "" when it holds none or cannot be read.
func newestTranscript(project string) string {
	found, _ := filepath.Glob(filepath.Join(project, "*.jsonl"))
	var newest string
	var newestAt int64
	for _, path := range found {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		// A tie is settled by name, so that the answer does not depend on the
		// order the disk lists the files in.
		if at := info.ModTime().UnixNano(); newest == "" || at > newestAt || (at == newestAt && path > newest) {
			newest, newestAt = path, at
		}
	}
	return newest
}

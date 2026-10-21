package grist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// TalksDir is the directory of the state directory holding the talks'
// records, one file each; TalkCwdDir holds each app's talk directory, the one
// its talk sessions run in.
const (
	TalksDir   = "talks"
	TalkCwdDir = "cwd"
)

// Talks keeps the talks' records under Dir/talks/<hash>.json, and each app's
// talk directory at Dir/cwd/<app> (application's GristTalkStore). Projects is
// where Claude Code keeps its sessions' transcripts, ~/.claude/projects.
type Talks struct {
	Dir      string
	Projects string
}

// Talks satisfies the port.
var _ application.GristTalkStore = (*Talks)(nil)

// NewTalks is the talks kept under the state directory dir, their transcripts
// under the user's own ~/.claude/projects.
func NewTalks(dir string) *Talks {
	t := &Talks{Dir: dir}
	if home, err := os.UserHomeDir(); err == nil {
		t.Projects = filepath.Join(home, ".claude", "projects")
	}
	return t
}

// Cwd implements application.GristTalkStore.
func (t *Talks) Cwd(app string) (string, error) {
	if app == "" || strings.ContainsAny(app, `/\`) || app == "." || app == ".." {
		return "", fmt.Errorf("%q is not an app's name", app)
	}
	dir, err := filepath.Abs(filepath.Join(t.Dir, TalkCwdDir, app))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("making %s: %w", dir, err)
	}
	return dir, nil
}

// path is the file of key's record: named by a hash, since a talk's name is
// the app's to choose.
func (t *Talks) path(key application.GristTalkKey) string {
	sum := sha256.Sum256([]byte(key.App + "\x00" + key.Sender + "\x00" + key.Talk))
	return filepath.Join(t.Dir, TalksDir, hex.EncodeToString(sum[:16])+".json")
}

// Load implements application.GristTalkStore.
func (t *Talks) Load(_ context.Context, key application.GristTalkKey) (application.GristTalk, bool, error) {
	raw, err := os.ReadFile(t.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return application.GristTalk{}, false, nil
	}
	if err != nil {
		return application.GristTalk{}, false, err
	}
	var talk application.GristTalk
	if err := json.Unmarshal(raw, &talk); err != nil {
		return application.GristTalk{}, false, fmt.Errorf("reading %s: %w", t.path(key), err)
	}
	return talk, true, nil
}

// Save implements application.GristTalkStore: the record is written whole,
// through a temporary file renamed into place.
func (t *Talks) Save(_ context.Context, talk application.GristTalk) error {
	data, err := json.MarshalIndent(talk, "", "  ")
	if err != nil {
		return err
	}
	return (&State{Dir: t.Dir}).writeWhole(t.path(talk.GristTalkKey), append(data, '\n'))
}

// Remove implements application.GristTalkStore.
func (t *Talks) Remove(_ context.Context, key application.GristTalkKey) error {
	err := os.Remove(t.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// List implements application.GristTalkStore, in the order of the files'
// names. A record that cannot be read is left out.
func (t *Talks) List(context.Context) ([]application.GristTalk, error) {
	entries, err := os.ReadDir(filepath.Join(t.Dir, TalksDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	talks := make([]application.GristTalk, 0, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(t.Dir, TalksDir, name))
		if err != nil {
			continue
		}
		var talk application.GristTalk
		if json.Unmarshal(raw, &talk) == nil {
			talks = append(talks, talk)
		}
	}
	return talks, nil
}

// Transcript implements application.GristTalkStore: Claude Code keeps a
// session run from cwd at Projects/<cwd's slug>/<session>.jsonl.
func (t *Talks) Transcript(cwd, session string) (bool, error) {
	if t.Projects == "" {
		return false, fmt.Errorf("there is no home directory to find Claude Code's transcripts in")
	}
	_, err := os.Stat(TranscriptPath(t.Projects, cwd, session))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// RemoveTranscript implements application.GristTalkStore.
func (t *Talks) RemoveTranscript(cwd, session string) error {
	if t.Projects == "" {
		return fmt.Errorf("there is no home directory to find Claude Code's transcripts in")
	}
	if session == "" || session != filepath.Base(session) || session == "." || session == ".." || !filepath.IsAbs(cwd) {
		return fmt.Errorf("%q in %q is not a session to delete the transcript of", session, cwd)
	}
	err := os.Remove(TranscriptPath(t.Projects, cwd, session))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// TranscriptPath is where Claude Code keeps the transcript of session, run
// from cwd, under projects: the directory is cwd with every character but a
// letter or digit made "-".
func TranscriptPath(projects, cwd, session string) string {
	slug := []byte(cwd)
	for i, c := range slug {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9') {
			slug[i] = '-'
		}
	}
	return filepath.Join(projects, string(slug), session+".jsonl")
}

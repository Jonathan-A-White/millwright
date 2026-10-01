package postern

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.PosternThreadIndex = (*ThreadFile)(nil)

// ThreadFile is the application.PosternThreadIndex kept in a file on this
// host: one JSON line per post, appended, the last line for a txid the one
// that counts. It is host-local state beside the inbox's attachments, never
// the vault.
type ThreadFile struct{ path string }

// NewThreadFile is the index at path.
func NewThreadFile(path string) *ThreadFile { return &ThreadFile{path: path} }

type threadLine struct {
	Txid  string `json:"txid"`
	Bead  string `json:"bead,omitempty"`
	Topic string `json:"topic,omitempty"`
}

// threadKey is txid as the index keys it: "direct:<sha256>" and the bare
// hash are one post.
func threadKey(txid string) string { return strings.TrimPrefix(strings.TrimSpace(txid), "direct:") }

// Remember appends that the post txid is in thread, creating the file and
// its directory (0700, the file 0600) when they are not there.
func (f *ThreadFile) Remember(txid string, thread application.PosternThread) error {
	line, err := json.Marshal(threadLine{Txid: threadKey(txid), Bead: thread.Bead, Topic: thread.Topic})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("making the directory of %s: %w", f.path, err)
	}
	file, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", f.path, err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return fmt.Errorf("writing %s: %w", f.path, err)
	}
	return file.Close()
}

// Lookup reports the channel of the post txid, false when the file does not
// name it. A line that is not JSON is skipped, not an error.
func (f *ThreadFile) Lookup(txid string) (application.PosternThread, bool, error) {
	file, err := os.Open(f.path)
	if os.IsNotExist(err) {
		return application.PosternThread{}, false, nil
	}
	if err != nil {
		return application.PosternThread{}, false, fmt.Errorf("opening %s: %w", f.path, err)
	}
	defer file.Close()
	want := threadKey(txid)
	var found threadLine
	ok := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line threadLine
		if json.Unmarshal(scanner.Bytes(), &line) == nil && line.Txid == want {
			found, ok = line, true
		}
	}
	if err := scanner.Err(); err != nil {
		return application.PosternThread{}, false, fmt.Errorf("reading %s: %w", f.path, err)
	}
	return application.PosternThread{Bead: found.Bead, Topic: found.Topic}, ok, nil
}

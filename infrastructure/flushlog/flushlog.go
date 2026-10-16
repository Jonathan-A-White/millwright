// Package flushlog keeps the home's last beads flush in a one-line file on
// this host: "ok <time>" or "failed <time> <why>", times in RFC 3339 UTC. It is
// the application.BeadsFlushLog adapter.
package flushlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.BeadsFlushLog = File{}

// File is the log at Path.
type File struct{ Path string }

// New is the log at path.
func New(path string) File { return File{Path: path} }

// Last implements application.BeadsFlushLog: the zero record when the file is
// not there, an error when it cannot be read as a record.
func (f File) Last(context.Context) (application.BeadsFlushRecord, error) {
	text, err := os.ReadFile(f.Path)
	if os.IsNotExist(err) {
		return application.BeadsFlushRecord{}, nil
	}
	if err != nil {
		return application.BeadsFlushRecord{}, fmt.Errorf("reading the beads flush log: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(text)))
	if len(fields) < 2 || (fields[0] != "ok" && fields[0] != "failed") {
		return application.BeadsFlushRecord{}, fmt.Errorf("%s holds %q, not a flush record", f.Path, strings.TrimSpace(string(text)))
	}
	at, err := time.Parse(time.RFC3339, fields[1])
	if err != nil {
		return application.BeadsFlushRecord{}, fmt.Errorf("%s: %w", f.Path, err)
	}
	record := application.BeadsFlushRecord{At: at}
	if fields[0] == "failed" {
		record.Failed = strings.Join(fields[2:], " ")
		if record.Failed == "" {
			record.Failed = "no reason given"
		}
	}
	return record, nil
}

// Record implements application.BeadsFlushLog. The file is written beside
// itself and moved into place, so a reader never sees half a line.
func (f File) Record(_ context.Context, record application.BeadsFlushRecord) error {
	line := "ok " + record.At.UTC().Format(time.RFC3339)
	if record.Failed != "" {
		line = "failed " + record.At.UTC().Format(time.RFC3339) + " " + strings.Join(strings.Fields(record.Failed), " ")
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return fmt.Errorf("making the beads flush log's directory: %w", err)
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(line+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing the beads flush log: %w", err)
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		return fmt.Errorf("writing the beads flush log: %w", err)
	}
	return nil
}

package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Vault is the adapter behind application.ReapLog as well: the reaper's
// account of what it did goes beside the acting file it watches. So does `mw
// talk model`'s, behind application.TalkLog.
var (
	_ application.ReapLog = (*Vault)(nil)
	_ application.TalkLog = (*Vault)(nil)
	// And of application.ActingFile: `mw seat handover` writes the acting
	// file the reaper watches.
	_ application.ActingFile = (*Vault)(nil)
)

// WriteActing implements application.ActingFile: it replaces `.<seat>-acting`
// in the vault with text, whole or not at all.
func (v *Vault) WriteActing(_ context.Context, seat, text string) error {
	if err := safeName("seat", seat); err != nil {
		return err
	}
	path := filepath.Join(v.dir, application.ActingFileName(seat))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return fmt.Errorf("writing the %s seat's acting file: %w", seat, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing the %s seat's acting file: %w", seat, err)
	}
	return nil
}

// Note implements application.ReapLog: one line appended to
// `.<seat>-reaper.log` in the vault. The log is made when there is none, and
// what is in it is never rewritten.
func (v *Vault) Note(_ context.Context, seat, line string) error {
	if err := safeName("seat", seat); err != nil {
		return err
	}
	return v.appendLine(application.ReapLogFileName(seat), "the "+seat+" seat's reaper log", line)
}

// NoteTalk implements application.TalkLog: one line appended to
// `.<seat>-talk.log` in the vault, made when there is none and never
// rewritten.
func (v *Vault) NoteTalk(_ context.Context, seat, line string) error {
	if err := safeName("seat", seat); err != nil {
		return err
	}
	return v.appendLine(application.TalkLogFileName(seat), "the "+seat+" seat's talk log", line)
}

// appendLine appends one line to a log file of the vault's top directory.
func (v *Vault) appendLine(name, what, line string) error {
	line = strings.TrimRight(line, "\r\n")
	if strings.ContainsAny(line, "\r\n") {
		return fmt.Errorf("a line of %s is one line, got %q", what, line)
	}

	file, err := os.OpenFile(filepath.Join(v.dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", what, err)
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing %s: %w", what, err)
	}
	return file.Close()
}

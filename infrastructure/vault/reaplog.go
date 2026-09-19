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
// account of what it did goes beside the acting file it watches.
var _ application.ReapLog = (*Vault)(nil)

// Note implements application.ReapLog: one line appended to
// `.<seat>-reaper.log` in the vault. The log is made when there is none, and
// what is in it is never rewritten.
func (v *Vault) Note(_ context.Context, seat, line string) error {
	if err := safeName("seat", seat); err != nil {
		return err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.ContainsAny(line, "\r\n") {
		return fmt.Errorf("a line of the %s seat's reaper log is one line, got %q", seat, line)
	}

	file, err := os.OpenFile(filepath.Join(v.dir, application.ReapLogFileName(seat)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening the %s seat's reaper log: %w", seat, err)
	}
	if _, err := file.WriteString(line + "\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing the %s seat's reaper log: %w", seat, err)
	}
	return file.Close()
}

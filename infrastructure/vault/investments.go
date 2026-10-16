package vault

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.InvestmentBook = (*Vault)(nil)

// InvestmentsPath is where the investments ledger lives in the vault.
func (v *Vault) InvestmentsPath() string {
	return filepath.Join(v.dir, filepath.FromSlash(application.InvestmentsFile))
}

// AppendInvestment implements application.InvestmentBook: one JSON line on the end of the
// ledger, made when there is none. Nothing here rewrites a line.
func (v *Vault) AppendInvestment(_ context.Context, record application.InvestmentRecord) error {
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	path := v.InvestmentsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening the investments ledger to append to it: %w", err)
	}
	if ends, err := endsInNewline(path); err == nil && !ends {
		line = append([]byte("\n"), line...)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("appending to the investments ledger: %w", err)
	}
	return file.Close()
}

// Investments implements application.InvestmentBook: the records oldest first as
// the file holds them; none and no error when there is no file. A line that is
// not a record is skipped rather than hiding the rest.
func (v *Vault) Investments(context.Context) ([]application.InvestmentRecord, error) {
	file, err := os.Open(v.InvestmentsPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the investments ledger: %w", err)
	}
	defer file.Close()
	var kept []application.InvestmentRecord
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 64*1024), snapshotsLines)
	for lines.Scan() {
		var one application.InvestmentRecord
		if json.Unmarshal(lines.Bytes(), &one) == nil && one.Bead != "" && one.Kind != "" {
			kept = append(kept, one)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading the investments ledger: %w", err)
	}
	return kept, nil
}

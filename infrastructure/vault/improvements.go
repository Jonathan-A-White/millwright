package vault

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.ImprovementBook = (*Vault)(nil)

// ImprovementsPath is where the improvements ledger lives in the vault.
func (v *Vault) ImprovementsPath() string {
	return filepath.Join(v.dir, filepath.FromSlash(application.ImprovementsFile))
}

// Improvements implements application.ImprovementBook: the ideas in the order the
// file holds them, and whether the file exists. A line that is not an idea with a
// title is skipped rather than hiding the rest.
func (v *Vault) Improvements(context.Context) ([]application.ImprovementIdea, bool, error) {
	file, err := os.Open(v.ImprovementsPath())
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading the improvements ledger: %w", err)
	}
	defer file.Close()
	var kept []application.ImprovementIdea
	lines := bufio.NewScanner(file)
	lines.Buffer(make([]byte, 0, 64*1024), snapshotsLines)
	for lines.Scan() {
		var one application.ImprovementIdea
		if json.Unmarshal(lines.Bytes(), &one) == nil && strings.TrimSpace(one.Title) != "" {
			kept = append(kept, one)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, false, fmt.Errorf("reading the improvements ledger: %w", err)
	}
	return kept, true, nil
}

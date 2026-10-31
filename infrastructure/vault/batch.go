package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.BatchRecords = (*Vault)(nil)

// NewBatch implements application.BatchRecords: the directory
// runs/batch/<rig>/<n>, n one more than the highest number already there (1
// for the rig's first batch), made empty. Mkdir rather than MkdirAll for the
// last step, so that two landings that pick the same number do not share it:
// the second takes the next.
func (v *Vault) NewBatch(_ context.Context, rig string) (int, error) {
	if err := safeName("rig", rig); err != nil {
		return 0, err
	}
	dir := filepath.Join(v.dir, filepath.FromSlash(path.Join(application.BatchDir, rig)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("making the batch directory of %s: %w", rig, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("listing %s: %w", dir, err)
	}
	highest := 0
	for _, entry := range entries {
		if n, err := strconv.Atoi(entry.Name()); err == nil && entry.IsDir() && n > highest {
			highest = n
		}
	}
	for n := highest + 1; ; n++ {
		err := os.Mkdir(filepath.Join(dir, strconv.Itoa(n)), 0o755)
		if err == nil {
			return n, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return 0, fmt.Errorf("making batch %d of %s: %w", n, rig, err)
		}
	}
}

// PutBatchFile implements application.BatchRecords: runs/batch/<rig>/<n>/<name>,
// reported as its path from the vault's root.
func (v *Vault) PutBatchFile(_ context.Context, rig string, number int, name, contents string) (string, error) {
	if err := safeName("rig", rig); err != nil {
		return "", err
	}
	if err := safeName("batch file", name); err != nil {
		return "", err
	}
	if number < 1 {
		return "", fmt.Errorf("writing %s of a batch of %s: which batch?", name, rig)
	}
	rel := path.Join(application.BatchDir, rig, strconv.Itoa(number), name)
	at := filepath.Join(v.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return "", fmt.Errorf("making the directory of batch %d of %s: %w", number, rig, err)
	}
	if err := os.WriteFile(at, []byte(contents), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", at, err)
	}
	return rel, nil
}

// BatchFilePath implements application.BatchRecords: the file's place on disk,
// empty for a name or a batch that has none.
func (v *Vault) BatchFilePath(rig string, number int, name string) string {
	if safeName("rig", rig) != nil || safeName("batch file", name) != nil || number < 1 {
		return ""
	}
	return filepath.Join(v.dir, filepath.FromSlash(path.Join(application.BatchDir, rig, strconv.Itoa(number), name)))
}

var _ application.GristBatches = (*Vault)(nil)

// BatchOf implements application.GristBatches: the highest batch of the rig
// whose outcome.txt says it landed the story ("landed <commit> on <target>, <n>
// stories: <ids>"). A batch that was held, or that set the story aside, did not
// carry it.
func (v *Vault) BatchOf(_ context.Context, rig, story string) (int, bool, error) {
	if err := safeName("rig", rig); err != nil {
		return 0, false, err
	}
	dir := filepath.Join(v.dir, filepath.FromSlash(path.Join(application.BatchDir, rig)))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("listing %s: %w", dir, err)
	}
	found, highest := false, 0
	for _, entry := range entries {
		number, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() || number <= highest {
			continue
		}
		outcome, err := os.ReadFile(filepath.Join(dir, entry.Name(), application.BatchOutcomeFile))
		if err != nil {
			continue
		}
		if landedStory(string(outcome), story) {
			found, highest = true, number
		}
	}
	return highest, found, nil
}

// landedStory reports whether the first line of a batch's outcome.txt is a
// landing that names story among its stories.
func landedStory(outcome, story string) bool {
	first, _, _ := strings.Cut(outcome, "\n")
	if !strings.HasPrefix(first, "landed ") {
		return false
	}
	_, ids, ok := strings.Cut(first, " stories: ")
	if !ok {
		return false
	}
	for _, id := range strings.Split(ids, ",") {
		if strings.TrimSpace(id) == story {
			return true
		}
	}
	return false
}

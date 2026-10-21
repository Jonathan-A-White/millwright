package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// PutTimingFile implements application.TimingRecords: runs/timing/<rig>/<name>,
// made if it is not there, and reports the path from the vault's root.
func (v *Vault) PutTimingFile(_ context.Context, rig, name, contents string) (string, error) {
	if err := safeName("rig", rig); err != nil {
		return "", err
	}
	if err := safeName("timing file", name); err != nil {
		return "", err
	}
	rel := path.Join(application.TimingDir, rig, name)
	at := filepath.Join(v.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return "", fmt.Errorf("making the timing directory of %s: %w", rig, err)
	}
	if err := os.WriteFile(at, []byte(contents), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", at, err)
	}
	return rel, nil
}

var _ application.TimingRecords = (*Vault)(nil)

// LatestTiming implements application.TimingHistory: the newest of the
// runs/timing/<rig>/<date>.json files, by the date in its name.
func (v *Vault) LatestTiming(_ context.Context, rig string) (application.TimingRecord, bool, error) {
	return v.newestTiming(rig, false)
}

// LatestPassingTiming implements application.TimingHistory: the newest of them
// whose run passed.
func (v *Vault) LatestPassingTiming(_ context.Context, rig string) (application.TimingRecord, bool, error) {
	return v.newestTiming(rig, true)
}

var _ application.TimingHistory = (*Vault)(nil)

func (v *Vault) newestTiming(rig string, passing bool) (application.TimingRecord, bool, error) {
	if err := safeName("rig", rig); err != nil {
		return application.TimingRecord{}, false, err
	}
	dir := filepath.Join(v.dir, filepath.FromSlash(path.Join(application.TimingDir, rig)))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return application.TimingRecord{}, false, nil
	}
	if err != nil {
		return application.TimingRecord{}, false, fmt.Errorf("listing %s: %w", dir, err)
	}
	var days []string
	for _, entry := range entries {
		name := entry.Name()
		if day := strings.TrimSuffix(name, ".json"); day != name && !entry.IsDir() {
			days = append(days, day)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	for _, day := range days {
		contents, err := os.ReadFile(filepath.Join(dir, day+".json"))
		if err != nil {
			return application.TimingRecord{}, false, fmt.Errorf("reading the timing result of %s: %w", day, err)
		}
		record, err := application.ParseTimingRecord(day, string(contents))
		if err != nil {
			return application.TimingRecord{}, false, err
		}
		if !passing || record.Result.Passed {
			return record, true, nil
		}
	}
	return application.TimingRecord{}, false, nil
}

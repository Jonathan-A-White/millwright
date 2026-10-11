package grist

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// RequestsDir is the directory of the state directory holding the records of
// the requests the mill linked to beads, one <bead>.json for each.
const RequestsDir = "requests"

// requestBead is a bead id as a file name: no path in it.
var requestBead = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Requests keeps the records of linked requests under Dir/requests/
// (application's GristRequestStore). They name the key of a person, so the
// files are private: 0700 directories, 0600 files.
type Requests struct {
	Dir string
}

var _ application.GristRequestStore = (*Requests)(nil)

// NewRequests is the record of linked requests kept under the state directory dir.
func NewRequests(dir string) *Requests { return &Requests{Dir: dir} }

func (r *Requests) path(bead string) (string, error) {
	if !requestBead.MatchString(bead) {
		return "", fmt.Errorf("%q is not a bead id", bead)
	}
	return filepath.Join(r.Dir, RequestsDir, bead+".json"), nil
}

// Keep implements application.GristRequestStore.
func (r *Requests) Keep(_ context.Context, record application.GristRequestRecord) error {
	path, err := r.path(record.Bead)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("making %s: %w", filepath.Dir(path), err)
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	// Written whole beside the file and renamed over it, so that a pass cut
	// short never leaves half a record.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Get implements application.GristRequestStore.
func (r *Requests) Get(_ context.Context, bead string) (application.GristRequestRecord, bool, error) {
	path, err := r.path(bead)
	if err != nil {
		return application.GristRequestRecord{}, false, err
	}
	return readRequest(path)
}

func readRequest(path string) (application.GristRequestRecord, bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return application.GristRequestRecord{}, false, nil
	}
	if err != nil {
		return application.GristRequestRecord{}, false, err
	}
	var record application.GristRequestRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return application.GristRequestRecord{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	return record, true, nil
}

// List implements application.GristRequestStore.
func (r *Requests) List(_ context.Context) ([]application.GristRequestRecord, error) {
	entries, err := os.ReadDir(filepath.Join(r.Dir, RequestsDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []application.GristRequestRecord
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		record, ok, err := readRequest(filepath.Join(r.Dir, RequestsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		if ok {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Bead < records[j].Bead })
	return records, nil
}

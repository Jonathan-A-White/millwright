package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// runRecordPath matches the run record of one attempt of a story, by path from
// the vault's root: runs/<story>/result.json, or result-<n>.json for a later
// attempt (RunRecordForAttempt). The story id is the first group.
var runRecordPath = regexp.MustCompile(`^` + RunsDir + `/([^/]+)/result(?:-[0-9]+)?\.json$`)

// commitRunRecords commits, as it was found, the run record of every story in
// progress on this host that shows among dirty, and returns what the vault
// still holds uncommitted. A retried story's session writes its result over the
// one the failed attempt's close-out committed, and a retry that follows a
// refunded attempt reuses the same name (mw-gq6.87's numbering follows the
// attempt count, which a refund lowers); until the story's own close-out commits
// the new one, every sync on the host would refuse on it (mw-vwveeq.1). The run
// record is the one thing a session writes beside mw's own in the vault, so it is
// the one file committed here; nothing else is, and a story that is not in
// progress here keeps its file in the way.
//
// It is best effort: a tracker that cannot say what is running, or a commit
// that fails, leaves dirty as it was, and why is noted on the report.
func (s Sync) commitRunRecords(ctx context.Context, dirty []string, report *SyncReport) []string {
	var records []string
	owners := map[string]string{}
	for _, file := range dirty {
		if m := runRecordPath.FindStringSubmatch(file); m != nil {
			records = append(records, file)
			owners[file] = m[1]
		}
	}
	// The tracker's read of the stories in progress on a host is the one thing a
	// sync asks of it beside the beads cycle; the gateway and the fake both have it.
	reader, ok := s.Tracker.(interface {
		RunningStories(ctx context.Context, host string) ([]StoryDetail, error)
	})
	if len(records) == 0 || !ok {
		return dirty
	}
	running, err := reader.RunningStories(ctx, s.Host)
	if err != nil {
		report.RunRecordNote = fmt.Sprintf("could not read which stories are in progress here: %s", firstLine(err.Error()))
		return dirty
	}
	inProgress := map[string]bool{}
	for _, detail := range running {
		inProgress[detail.Story.ID] = true
	}
	var paths, ids []string
	for _, file := range records {
		if inProgress[owners[file]] {
			paths = append(paths, file)
			ids = append(ids, owners[file])
		}
	}
	if len(paths) == 0 {
		return dirty
	}
	message := fmt.Sprintf("mw sync: commit as found the run record of %s, in progress on %s", strings.Join(ids, ", "), s.Host)
	if _, err := commitWritten(ctx, s.Vault, message, paths); err != nil {
		report.RunRecordNote = fmt.Sprintf("the run record of an in-progress story was not committed: %s", firstLine(err.Error()))
		return dirty
	}
	still, err := s.Vault.Uncommitted(ctx)
	if err != nil {
		report.RunRecordNote = fmt.Sprintf("the run record was committed but the vault could not be read again: %s", firstLine(err.Error()))
		return dirty
	}
	return still
}

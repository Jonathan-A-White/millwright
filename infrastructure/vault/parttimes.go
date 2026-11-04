package vault

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// PartTimes is the times of past parts of split gates, read out of the records
// mw check --part leaves in the stories' runs in the vault (mw-gq6.944). The
// hosts' syncs carry them to each other, as they carry the benchmarks.
type PartTimes struct{ Vault *Vault }

var _ application.PartTimeBook = PartTimes{}

// RecentParts implements application.PartTimeBook: every part time under runs/,
// oldest first. A file that cannot be read or is not a part time is not one.
func (p PartTimes) RecentParts(context.Context) ([]application.PartTime, error) {
	runs := filepath.Join(p.Vault.dir, RunsDir)
	stories, err := os.ReadDir(runs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []application.PartTime
	for _, story := range stories {
		if !story.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(runs, story.Name()))
		if err != nil {
			continue
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), application.PartTimeFileSuffix) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(runs, story.Name(), file.Name()))
			if err != nil {
				continue
			}
			if taken, ok := application.ReadPartTime(story.Name(), string(raw)); ok {
				found = append(found, taken)
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].At.Before(found[j].At) })
	return found, nil
}

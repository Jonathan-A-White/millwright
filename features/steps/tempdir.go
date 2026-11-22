package steps

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/cucumber/godog"
)

// The directories a scenario's steps have made and not yet removed. A feature's
// scenarios run one after another in their process, so one list serves; the lock
// is for a step that makes a directory from a goroutine of its own.
var (
	tempDirsMu sync.Mutex
	tempDirs   []string
)

// scenarioTempDir makes a temporary directory, as os.MkdirTemp does, and removes
// it when the scenario ends (InitializeTempDirScenario). Every step that needs a
// directory of its own takes it from here: a bare os.MkdirTemp left each gate
// run's directories in /tmp for ever, 370,000 of them on the home (mw-gq6.1635),
// and TestStepsMakeTheirTempDirsWithTheHelper fails on one.
func scenarioTempDir(dir, pattern string) (string, error) {
	made, err := os.MkdirTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	tempDirsMu.Lock()
	tempDirs = append(tempDirs, made)
	tempDirsMu.Unlock()
	return made, nil
}

// InitializeTempDirScenario removes what scenarioTempDir made when each scenario
// ends. Register it last, after every other step file's hooks: godog runs the
// After hooks in the order they were registered, and a hook that stops a process
// or reads a file of a scenario's directory must run before the directory goes.
func InitializeTempDirScenario(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		removeScenarioTempDirs()
		return ctx, nil
	})
}

// removeScenarioTempDirs removes the directories made so far, and what is in
// them even when a step made it read-only. A directory that cannot be removed is
// left, not an error: the scenario's verdict is not the clean-up's to change.
func removeScenarioTempDirs() {
	tempDirsMu.Lock()
	dirs := tempDirs
	tempDirs = nil
	tempDirsMu.Unlock()
	for _, dir := range dirs {
		if os.RemoveAll(dir) == nil {
			continue
		}
		filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if entry != nil && entry.IsDir() {
				os.Chmod(path, 0o700)
			}
			return nil
		})
		os.RemoveAll(dir)
	}
}

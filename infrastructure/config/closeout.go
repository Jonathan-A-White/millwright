package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CloseoutTable is the config table of the close-out's own knobs, and
// OpenStepsGuardKey its one key (mw-gq6.1676).
const (
	CloseoutTable      = "closeout"
	OpenStepsGuardKey  = "open_steps_guard"
	openStepsGuardName = CloseoutTable + "." + OpenStepsGuardKey
)

// OpenStepsGuard says whether a Builder's session is launched with the Stop hook
// that keeps it from ending with a formula step open: the `[closeout]` table's
// `open_steps_guard`, true when the file, the table or the key is missing. A
// value that is not true or false is an error naming the key; a Builder is not
// launched on a guess at it. Claude sessions only: Codex has no such hook.
func OpenStepsGuard() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)
	table, err := tableIn(path, CloseoutTable)
	if err != nil {
		return false, err
	}
	said, ok := table[OpenStepsGuardKey]
	if !ok || strings.TrimSpace(said) == "" {
		return true, nil
	}
	on, err := strconv.ParseBool(strings.TrimSpace(said))
	if err != nil {
		return false, fmt.Errorf("%s = %q in %s: it must be true or false", openStepsGuardName, said, path)
	}
	return on, nil
}

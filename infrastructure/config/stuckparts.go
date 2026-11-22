package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// StuckTable is the table of the config file that says how mw tells the Mayor
// of work that is stuck: `[stuck]`.
const StuckTable = "stuck"

// StuckThawable reports whether dispatch mails the Mayor one line of counts
// when its host has had room for 15 minutes with nothing to start (mw-gq6.1645):
// the `thawable` key of the `[stuck]` table of ~/.config/mw/config.toml, true
// when the table or the key is missing, and false for `thawable = false`, which
// is the rollback. A value that is not true or false is an error naming the key.
func StuckThawable() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	return stuckThawableIn(filepath.Join(home, File))
}

// stuckThawableIn reads the key from the config file at path.
func stuckThawableIn(path string) (bool, error) {
	table, err := tableIn(path, StuckTable)
	if err != nil {
		return false, err
	}
	said := strings.TrimSpace(table["thawable"])
	if said == "" {
		return true, nil
	}
	on, err := strconv.ParseBool(said)
	if err != nil {
		return false, fmt.Errorf("the [%s] table of %s says thawable = %q: it must be true or false", StuckTable, path, said)
	}
	return on, nil
}

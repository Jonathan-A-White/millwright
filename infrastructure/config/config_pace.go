package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PaceTable is the table of the config file that holds the pace of a paced
// release.
const PaceTable = "pace"

// DefaultPaceUsageLine is the percent of the week's usage over which a paced
// release lets nothing go when the [pace] table says nothing;
// application.ReleasePacedUsageLine is the same.
const DefaultPaceUsageLine = 60.0

// PaceUsageLine reports the line mw release-paced and the model-story hold of mw
// path set keep to, the `usage_line` key of the [pace] table of
// ~/.config/mw/config.toml as a percent above 0 and at most 100;
// DefaultPaceUsageLine when unset (mw-6ww.119.10.8).
func PaceUsageLine() (float64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)
	table, err := tableIn(path, PaceTable)
	if err != nil {
		return 0, err
	}
	said := strings.Trim(strings.TrimSpace(table["usage_line"]), `"'`)
	if said == "" {
		return DefaultPaceUsageLine, nil
	}
	line, err := strconv.ParseFloat(said, 64)
	if err != nil || line <= 0 || line > 100 {
		return 0, fmt.Errorf("the [%s] table of %s says usage_line = %q: it must be a percent above 0 and at most 100, such as 60", PaceTable, path, said)
	}
	return line, nil
}

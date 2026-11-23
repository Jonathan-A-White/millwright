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

// PaceFile is the file of the vault, read by every host, that holds the
// factory-wide [pace] keys (usage_line, max_at_once): a pace the Governor sets
// once is the pace of a landing on the home, the desktop and a cloud host alike
// (mw-gq6.1698). A key it says answers ahead of the same key of the host's own
// [pace] table; a key it leaves out is read from the host's own table, then the
// default.
const PaceFile = "pace.toml"

// paceKeyIn reads one key of the [pace] table: from the vault's PaceFile when
// vaultDir is given and says it, otherwise from the host's own config file. It
// returns the word said ("" when neither says it) and the path it was read from.
func paceKeyIn(vaultDir, key string) (said, path string, err error) {
	if vaultDir != "" {
		path = filepath.Join(vaultDir, PaceFile)
		table, err := tableIn(path, PaceTable)
		if err != nil {
			return "", path, err
		}
		if said = strings.Trim(strings.TrimSpace(table[key]), `"'`); said != "" {
			return said, path, nil
		}
	}
	return hostPaceKey(key)
}

// hostPaceKey reads one key of the host's own [pace] table.
func hostPaceKey(key string) (said, path string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path = filepath.Join(home, File)
	table, err := tableIn(path, PaceTable)
	if err != nil {
		return "", path, err
	}
	return strings.Trim(strings.TrimSpace(table[key]), `"'`), path, nil
}

// vaultOrNone is the vault's directory, or "" when this host names none.
func vaultOrNone() string {
	dir, err := Vault()
	if err != nil {
		return ""
	}
	return dir
}

func parsePaceUsageLine(said, path string) (float64, error) {
	line, err := strconv.ParseFloat(said, 64)
	if err != nil || line <= 0 || line > 100 {
		return 0, fmt.Errorf("the [%s] table of %s says usage_line = %q: it must be a percent above 0 and at most 100, such as 60", PaceTable, path, said)
	}
	return line, nil
}

func parsePaceMaxAtOnce(said, path string) (int, error) {
	n, err := strconv.Atoi(said)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("the [%s] table of %s says max_at_once = %q: it must be a whole number of 1 or more, such as 3", PaceTable, path, said)
	}
	return n, nil
}

// PaceUsageLine reports the line mw release-paced and the model-story hold of mw
// path set keep to: PaceUsageLineIn the vault this host names.
func PaceUsageLine() (float64, error) { return PaceUsageLineIn(vaultOrNone()) }

// PaceUsageLineIn reports the `usage_line` key of the [pace] table, as a percent
// above 0 and at most 100: the vault's PaceFile first, then the host's own
// ~/.config/mw/config.toml; DefaultPaceUsageLine when neither says
// (mw-6ww.119.10.8, mw-gq6.1698).
func PaceUsageLineIn(vaultDir string) (float64, error) {
	said, path, err := paceKeyIn(vaultDir, "usage_line")
	if err != nil {
		return 0, err
	}
	if said == "" {
		return DefaultPaceUsageLine, nil
	}
	return parsePaceUsageLine(said, path)
}

// DefaultPaceMaxAtOnce is how many stories of a list a paced release lets run or
// be open at once when the [pace] table says nothing;
// application.ReleasePacedMax is the same.
const DefaultPaceMaxAtOnce = 3

// PaceMaxAtOnce reports the count mw release-paced and the model-story hold of mw
// path set keep to: PaceMaxAtOnceIn the vault this host names.
func PaceMaxAtOnce() (int, error) { return PaceMaxAtOnceIn(vaultOrNone()) }

// PaceMaxAtOnceIn reports the `max_at_once` key of the [pace] table, as a whole
// number of 1 or more: the vault's PaceFile first, then the host's own
// ~/.config/mw/config.toml; DefaultPaceMaxAtOnce when neither says (mw-gq6.1695,
// mw-gq6.1698).
func PaceMaxAtOnceIn(vaultDir string) (int, error) {
	said, path, err := paceKeyIn(vaultDir, "max_at_once")
	if err != nil {
		return 0, err
	}
	if said == "" {
		return DefaultPaceMaxAtOnce, nil
	}
	return parsePaceMaxAtOnce(said, path)
}

// PaceDifferences names each [pace] key of this host's own config that the
// vault's PaceFile says otherwise: the host's key is ignored, so the host is not
// at the pace it says (mw-gq6.1698). A key the vault leaves out is no
// difference, nor is one the host leaves out or says in other words for the same
// number (60 and 60.0).
func PaceDifferences(vaultDir string) ([]string, error) {
	if vaultDir == "" {
		return nil, nil
	}
	var found []string
	for _, key := range []string{"usage_line", "max_at_once"} {
		table, err := tableIn(filepath.Join(vaultDir, PaceFile), PaceTable)
		if err != nil {
			return nil, err
		}
		vaultSaid := strings.Trim(strings.TrimSpace(table[key]), `"'`)
		if vaultSaid == "" {
			continue
		}
		hostSaid, path, err := hostPaceKey(key)
		if err != nil {
			return nil, err
		}
		if hostSaid == "" {
			continue
		}
		if sameNumber(vaultSaid, hostSaid) {
			continue
		}
		found = append(found, fmt.Sprintf("%s says %s = %s but the vault's %s says %s", path, key, hostSaid, PaceFile, vaultSaid))
	}
	return found, nil
}

func sameNumber(a, b string) bool {
	x, errA := strconv.ParseFloat(a, 64)
	y, errB := strconv.ParseFloat(b, 64)
	return errA == nil && errB == nil && x == y
}

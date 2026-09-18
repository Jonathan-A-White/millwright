// Package config finds the factory's settings on the machine it is running
// on: where the vault is, which host this machine is, how many sessions may
// run here at once, and where each rig is checked out. Each is read from the
// environment or from ~/.config/mw/config.toml, which looks like this:
//
//	vault = "/root/millwright-vault"
//	host  = "vps"
//	cap   = 1
//
//	[rigs]
//	millwright = "/root/millwright"
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The environment variables that answer for each setting, ahead of the file.
const (
	VaultEnv = "MW_VAULT"
	HostEnv  = "MW_HOST"
	CapEnv   = "MW_CAP"
)

// RigsTable is the table of the config file that says where each rig is checked
// out on this host: rig name on the left, directory on the right.
const RigsTable = "rigs"

// DefaultCap is how many sessions may run at once on a host that does not say.
// One, because the smaller of the factory's two hosts has a single core and
// under a gigabyte of memory, and because two sessions racing is the expensive
// mistake to make by default.
const DefaultCap = 1

// File is the config file's path under the home directory.
var File = filepath.Join(".config", "mw", "config.toml")

// Vault reports the directory holding the vault, and so the one beads
// database: $MW_VAULT if it is set, otherwise the root-table `vault` key of
// ~/.config/mw/config.toml.
func Vault() (string, error) {
	return setting("vault", VaultEnv)
}

// Host reports which of the factory's hosts this machine is — the name the
// Mayor writes into a story's Path, `vps` or `laptop`, not the machine's
// hostname: $MW_HOST if it is set, otherwise the root-table `host` key of
// ~/.config/mw/config.toml. It is what a session's seat is signed with and
// what the dispatcher asks beads for ready stories by.
func Host() (string, error) {
	return setting("host", HostEnv)
}

// Cap reports how many sessions may run on this machine at once: $MW_CAP if it
// is set, otherwise the root-table `cap` key of ~/.config/mw/config.toml, and
// DefaultCap when neither says. A host that says nothing runs one session at a
// time, which is the safe way round on a small machine.
func Cap() (int, error) {
	said := strings.TrimSpace(os.Getenv(CapEnv))
	if said == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", CapEnv, File, err)
		}
		if said, err = valueIn(filepath.Join(home, File), "cap"); err != nil {
			return 0, err
		}
	}
	if said == "" {
		return DefaultCap, nil
	}

	atOnce, err := strconv.Atoi(said)
	if err != nil {
		return 0, fmt.Errorf("the cap on sessions running at once is %q, which is not a whole number: set %s=<n>, or `cap = <n>` in %s", said, CapEnv, File)
	}
	if atOnce < 1 {
		return 0, fmt.Errorf("the cap on sessions running at once is %d, so nothing could ever be started: set it to 1 or more", atOnce)
	}
	return atOnce, nil
}

// Rigs reports where each rig the factory works is checked out on this machine,
// by rig name, read from the `[rigs]` table of ~/.config/mw/config.toml. A
// machine with no such table works no rigs, which is not an error here: it is
// the dispatcher that says so, naming the rig the story wanted.
func Rigs() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("there is no home directory to read %s in: %w", File, err)
	}
	path := filepath.Join(home, File)

	rigs, err := tableIn(path, RigsTable)
	if err != nil {
		return nil, err
	}
	for name, dir := range rigs {
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("the rig %s is at %q in %s: a rig's checkout is named by its full path", name, dir, path)
		}
	}
	return rigs, nil
}

// RigNames is the rigs this machine has, in a settled order, for a message a
// person reads.
func RigNames(rigs map[string]string) []string {
	names := make([]string, 0, len(rigs))
	for name := range rigs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// setting reads one root-table key: the environment first, the config file
// after it, and the way to set it if neither answers.
func setting(key, env string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(env)); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", env, File, err)
	}
	path := filepath.Join(home, File)

	value, err := valueIn(path, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("the %s is not set: export %s=<value>, or put `%s = \"<value>\"` in %s", key, env, key, path)
	}
	return value, nil
}

// valueIn reads one root-table key out of a config file, or "" if the file has
// no such key. A missing file is not an error: the environment may still be how
// this machine is told what it needs to know.
//
// Only the handful of TOML this needs is understood: `key = value` in the root
// table, with the value optionally quoted, and `#` comments. Keys after a
// `[table]` header belong to that table and are skipped.
func valueIn(path, key string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	defer file.Close()

	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") {
			break // the root table has ended; every later key belongs to a table
		}
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		return unquote(value), nil
	}
	if err := lines.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return "", nil
}

// tableIn reads every `key = value` of one `[table]` in a config file. A
// missing file, or a file without that table, is an empty table rather than an
// error: a machine may simply have no rigs checked out yet.
func tableIn(path, table string) (map[string]string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	inside := false
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") {
			inside = strings.TrimSpace(strings.Trim(line, "[]")) == table
			continue
		}
		if !inside || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if name = strings.TrimSpace(name); name != "" {
			values[strings.Trim(name, `"'`)] = unquote(value)
		}
	}
	if err := lines.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return values, nil
}

// unquote trims a TOML value down to the string it holds: a quoted value keeps
// everything between the quotes, a bare one loses any trailing comment.
func unquote(value string) string {
	value = strings.TrimSpace(value)
	for _, quote := range []string{`"`, `'`} {
		if strings.HasPrefix(value, quote) {
			if end := strings.Index(value[1:], quote); end >= 0 {
				return value[1 : end+1]
			}
		}
	}
	if comment := strings.Index(value, "#"); comment >= 0 {
		value = value[:comment]
	}
	return strings.TrimSpace(value)
}

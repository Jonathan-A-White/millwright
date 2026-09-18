// Package config finds the factory's settings on the machine it is running
// on. There are two so far — where the vault is, and which host this machine
// is — and each is read from the environment or from ~/.config/mw/config.toml.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The environment variables that answer for each setting, ahead of the file.
const (
	VaultEnv = "MW_VAULT"
	HostEnv  = "MW_HOST"
)

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

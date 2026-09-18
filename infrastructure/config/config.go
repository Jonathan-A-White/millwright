// Package config finds the factory's settings on the machine it is running
// on. There is one setting so far — where the vault is — and it is read from
// the environment or from ~/.config/mw/config.toml.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VaultEnv is the environment variable that names the vault directory.
const VaultEnv = "MW_VAULT"

// File is the config file's path under the home directory.
var File = filepath.Join(".config", "mw", "config.toml")

// Vault reports the directory holding the vault, and so the one beads
// database: $MW_VAULT if it is set, otherwise the root-table `vault` key of
// ~/.config/mw/config.toml.
func Vault() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(VaultEnv)); dir != "" {
		return dir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no %s is set and there is no home directory to read %s in: %w", VaultEnv, File, err)
	}
	path := filepath.Join(home, File)

	dir, err := vaultIn(path)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("the vault is not set: export %s=<dir>, or put `vault = \"<dir>\"` in %s", VaultEnv, path)
	}
	return dir, nil
}

// vaultIn reads the root-table `vault` key out of a config file, or "" if the
// file has no such key. A missing file is not an error: the environment may
// still be how this machine is told where the vault is.
//
// Only the handful of TOML this needs is understood: `key = value` in the root
// table, with the value optionally quoted, and `#` comments. Keys after a
// `[table]` header belong to that table and are skipped.
func vaultIn(path string) (string, error) {
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
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "vault" {
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

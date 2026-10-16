package cloud

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// The files box_cap is kept in, in the vault.
const (
	// CloudTablePath is the vault's copy of the home's [cloud] table.
	CloudTablePath = "hosts/vultr/cloud-table.toml"
	// BoostTfvarsPath is the settings a box is made with: its `cap` is what
	// contrib/boost-bootstrap.sh writes into the box's config.
	BoostTfvarsPath = "hosts/vultr/vultr-boost.tfvars"
)

// capCommitWait bounds the commit of the vault's two files once it is begun.
const capCommitWait = 2 * time.Minute

// VaultCommitter commits the given paths of the vault, and says which it did.
type VaultCommitter interface {
	Dir() string
	Commit(ctx context.Context, message string, paths []string) ([]string, error)
}

// CapFiles is application.CloudCapFiles over the home's config file and the
// vault.
type CapFiles struct {
	// Config is the home's ~/.config/mw/config.toml.
	Config string
	Vault  VaultCommitter
}

var _ application.CloudCapFiles = CapFiles{}

// BoxCap implements application.CloudCapFiles.
func (f CapFiles) BoxCap(context.Context) (int, error) {
	raw, err := os.ReadFile(f.Config)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", f.Config, err)
	}
	_, old, _ := setKey(string(raw), "cloud", "box_cap", 0, false)
	return old, nil
}

// SetBoxCap implements application.CloudCapFiles. The vault's files are
// written and committed first, the home's config last: it is the one the check
// reads, so a failure before it leaves the old cap in force.
func (f CapFiles) SetBoxCap(ctx context.Context, n int) error {
	if err := f.edit(filepath.Join(f.Vault.Dir(), CloudTablePath), "cloud", "box_cap", n, false); err != nil {
		return err
	}
	if err := f.edit(filepath.Join(f.Vault.Dir(), BoostTfvarsPath), "", "cap", n, true); err != nil {
		return fmt.Errorf("%w (%s was written, not committed, and the home's config is as it was)", err, CloudTablePath)
	}
	commit, cancel := context.WithTimeout(context.WithoutCancel(ctx), capCommitWait)
	defer cancel()
	if _, err := f.Vault.Commit(commit, fmt.Sprintf("mw cloud cap: box_cap %d", n), []string{CloudTablePath, BoostTfvarsPath}); err != nil {
		return fmt.Errorf("committing the box_cap in the vault: %w (the home's config is as it was)", err)
	}
	if err := f.edit(f.Config, "cloud", "box_cap", n, false); err != nil {
		return fmt.Errorf("%w (the vault's two files were written and committed)", err)
	}
	return nil
}

// edit rewrites the file with key set to n in table ("" is the root table,
// before any), keeping every other line; a key the table lacks is added as the
// table's first line.
func (f CapFiles) edit(path, table, key string, n int, quoted bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	text, _, found := setKey(string(raw), table, key, n, quoted)
	if !found {
		if text, found = addKey(text, table, keyLine(key, n, quoted)); !found {
			return fmt.Errorf("%s has no [%s] table to put %s in", path, table, key)
		}
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func keyLine(key string, n int, quoted bool) string {
	if quoted {
		return fmt.Sprintf("%s = %q", key, strconv.Itoa(n))
	}
	return fmt.Sprintf("%s = %d", key, n)
}

var tableHeader = regexp.MustCompile(`^\s*\[+\s*([^\]]*?)\s*\]+`)

// setKey is text with the line `key = <number>` of table set to n, the line's
// own comment kept, the number the line held, and whether there was a line. The
// root table is "".
func setKey(text, table, key string, n int, quoted bool) (out string, old int, found bool) {
	line := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)(["']?)(\d+)(["']?)(.*)$`)
	lines := strings.Split(text, "\n")
	inside := table == ""
	for i, l := range lines {
		if m := tableHeader.FindStringSubmatch(l); m != nil {
			inside = m[1] == table
			continue
		}
		if !inside || found {
			continue
		}
		if m := line.FindStringSubmatch(l); m != nil {
			old, _ = strconv.Atoi(m[3])
			found = true
			lines[i] = m[1] + strings.Replace(keyLine(key, n, quoted), key+" = ", "", 1) + m[5]
		}
	}
	return strings.Join(lines, "\n"), old, found
}

// addKey is text with line as the first line of table, whose header must be
// there; the root table "" takes it as the first line of the file.
func addKey(text, table, line string) (string, bool) {
	if table == "" {
		return line + "\n" + text, true
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if m := tableHeader.FindStringSubmatch(l); m != nil && m[1] == table {
			return strings.Join(append(lines[:i+1], append([]string{line}, lines[i+1:]...)...), "\n"), true
		}
	}
	return text, false
}

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
	"github.com/Jonathan-A-White/millwright/infrastructure/atomicfile"
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
	// DefaultMaxBoxes is the max_boxes in force when the config says none.
	DefaultMaxBoxes int
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
	return atomicfile.Write(path, []byte(text), 0o644)
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

// DrainedKey is the [cloud] key a drain keeps the old max_boxes in, in the
// home's config and in the vault's copy: undrain reads it and takes it away.
const DrainedKey = "drained_max_boxes"

// MaxBoxesKept implements application.CloudDrainFiles: the max_boxes the home's
// [cloud] table says (DefaultMaxBoxes when it says none), the value a drain
// kept, and whether one did.
func (f CapFiles) MaxBoxesKept(context.Context) (n, kept int, drained bool, err error) {
	raw, err := os.ReadFile(f.Config)
	if err != nil {
		return 0, 0, false, fmt.Errorf("reading %s: %w", f.Config, err)
	}
	_, n, found := setKey(string(raw), "cloud", "max_boxes", 0, false)
	if !found {
		n = f.DefaultMaxBoxes
	}
	_, kept, drained = setKey(string(raw), "cloud", DrainedKey, 0, false)
	return n, kept, drained, nil
}

// SetDrained implements application.CloudDrainFiles: max_boxes 0 and the old
// value kept, written as SetBoxCap writes: the vault's copy first and committed
// by path, the home's config last.
func (f CapFiles) SetDrained(ctx context.Context, old int) error {
	return f.drain(ctx, 0, old, true, fmt.Sprintf("mw cloud drain: max_boxes 0 (was %d)", old))
}

// SetUndrained implements application.CloudDrainFiles: max_boxes back at the
// kept value and the kept value taken away, in the same order.
func (f CapFiles) SetUndrained(ctx context.Context, back int) error {
	return f.drain(ctx, back, 0, false, fmt.Sprintf("mw cloud undrain: max_boxes %d", back))
}

// drain sets max_boxes to n and, with keep, the kept value to old; without it
// the kept value is taken away.
func (f CapFiles) drain(ctx context.Context, n, old int, keep bool, message string) error {
	table := filepath.Join(f.Vault.Dir(), CloudTablePath)
	if err := f.editDrained(table, n, old, keep); err != nil {
		return err
	}
	commit, cancel := context.WithTimeout(context.WithoutCancel(ctx), capCommitWait)
	defer cancel()
	if _, err := f.Vault.Commit(commit, message, []string{CloudTablePath}); err != nil {
		return fmt.Errorf("committing max_boxes in the vault: %w (the home's config is as it was)", err)
	}
	if err := f.editDrained(f.Config, n, old, keep); err != nil {
		return fmt.Errorf("%w (the vault's %s was written and committed)", err, CloudTablePath)
	}
	return nil
}

func (f CapFiles) editDrained(path string, n, old int, keep bool) error {
	if err := f.edit(path, "cloud", "max_boxes", n, false); err != nil {
		return err
	}
	if !keep {
		return f.remove(path, "cloud", DrainedKey)
	}
	return f.edit(path, "cloud", DrainedKey, old, false)
}

// remove rewrites the file without the line `key = <number>` of table; a file
// without one is left as it is.
func (f CapFiles) remove(path, table, key string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	line := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=\s*["']?\d+["']?.*$`)
	lines := strings.Split(string(raw), "\n")
	inside := table == ""
	for i, l := range lines {
		if m := tableHeader.FindStringSubmatch(l); m != nil {
			inside = m[1] == table
			continue
		}
		if inside && line.MatchString(l) {
			return atomicfile.Write(path, []byte(strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")), 0o644)
		}
	}
	return nil
}

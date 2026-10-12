package application

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// ConfigFile is ~/.config/mw/config.toml as a file the config commands change.
type ConfigFile interface {
	// Path is where the file is (or would be).
	Path() string
	// Read is the file's bytes, and whether the file exists; a missing file is
	// not an error.
	Read() (data []byte, exists bool, err error)
	// Backup copies the file to its path plus ".bak-" and suffix, beside it,
	// and returns that path. It never overwrites an earlier backup: if the name
	// is taken it adds -2, -3 and so on.
	Backup(suffix string) (path string, err error)
	// Write replaces the file's bytes, keeping its mode; a missing file is made.
	Write(data []byte) error
}

// BackupStamp is the UTC time a config backup is named by.
const BackupStamp = "2006-01-02T150405Z"

// ConfigSet changes one key of the config file: `mw config set`.
type ConfigSet struct {
	File ConfigFile
	// Now is the clock the backup is dated by. The zero value reads the real one.
	Now func() time.Time
	Out io.Writer
}

// Run sets table.key to value, or with dryRun says what it would do and writes
// nothing. It refuses, writing nothing and making no backup, a key with no
// table, a value on more than one line, a file that does not parse before or
// after the change, and a value the key already holds.
func (c ConfigSet) Run(ctx context.Context, dotted, value string, dryRun bool) error {
	table, key, err := domain.SplitConfigKey(dotted)
	if err != nil {
		return err
	}
	literal, err := domain.TOMLLiteral(value)
	if err != nil {
		return fmt.Errorf("%s: %w", dotted, err)
	}

	path := c.File.Path()
	data, exists, err := c.File.Read()
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	before, err := domain.ParseConfigTOML(string(data))
	if err != nil {
		return fmt.Errorf("%s does not parse as TOML, so nothing was changed: %w", path, err)
	}
	old, had := before.Lookup(table, key)
	if had && domain.SameTOMLValue(old, literal) {
		return fmt.Errorf("%s is unchanged: it already holds %s", dotted, old)
	}

	changed := before.Set(table, key, literal)
	after, err := domain.ParseConfigTOML(changed)
	if err != nil {
		return fmt.Errorf("setting %s to %s would not parse as TOML, so nothing was changed: %w", dotted, literal, err)
	}
	if got, ok := after.Lookup(table, key); !ok || got != literal {
		return fmt.Errorf("setting %s to %s would not read back as that value, so nothing was changed", dotted, literal)
	}

	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	suffix := now().UTC().Format(BackupStamp)
	backup, wayBack := path+".bak-"+suffix, "rm "+shellWord(path)
	if exists {
		wayBack = "cp " + shellWord(backup) + " " + shellWord(path)
	} else {
		backup = ""
	}

	if dryRun {
		fmt.Fprintln(c.Out, "dry run: nothing is written")
	} else {
		if exists {
			if backup, err = c.File.Backup(suffix); err != nil {
				return fmt.Errorf("backing up %s, so nothing was changed: %w", path, err)
			}
			wayBack = "cp " + shellWord(backup) + " " + shellWord(path)
		}
		if err := c.File.Write([]byte(changed)); err != nil {
			return fmt.Errorf("writing %s: %w (the backup is %s)", path, err, backup)
		}
	}

	fmt.Fprintf(c.Out, "config: %s\n", path)
	if had {
		fmt.Fprintf(c.Out, "old: %s\n", old)
	} else {
		fmt.Fprintln(c.Out, "old: unset")
	}
	fmt.Fprintf(c.Out, "new: %s\n", literal)
	if backup == "" {
		fmt.Fprintln(c.Out, "backup: none (there was no config file)")
	} else {
		fmt.Fprintf(c.Out, "backup: %s\n", backup)
	}
	fmt.Fprintf(c.Out, "way back: %s\n", wayBack)
	return nil
}

// ConfigGet reads one key of the config file: `mw config get`.
type ConfigGet struct {
	File ConfigFile
	Out  io.Writer
}

// Run prints table.key's value, a string without its quotes, or fails saying
// the key is unset.
func (c ConfigGet) Run(ctx context.Context, dotted string) error {
	table, key, err := domain.SplitConfigKey(dotted)
	if err != nil {
		return err
	}
	data, _, err := c.File.Read()
	if err != nil {
		return fmt.Errorf("reading %s: %w", c.File.Path(), err)
	}
	file, err := domain.ParseConfigTOML(string(data))
	if err != nil {
		return fmt.Errorf("%s does not parse as TOML: %w", c.File.Path(), err)
	}
	literal, ok := file.Lookup(table, key)
	if !ok {
		return fmt.Errorf("%s is unset in %s", dotted, c.File.Path())
	}
	value, _ := domain.TOMLString(literal)
	fmt.Fprintln(c.Out, value)
	return nil
}

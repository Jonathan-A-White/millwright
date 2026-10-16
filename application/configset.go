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

// Run sets table.key (or a known root key, with no table) to value, or with
// dryRun says what it would do and writes nothing. It refuses, writing nothing
// and making no backup, a key with no table that is not a known root key, a
// value on more than one line, a file that does not parse before or after the
// change, and a value the key already holds.
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

// BoostConfigFile is the vault file that holds the tables a Boost copies from
// the home's config file, from the vault's root (mw-gq6.453).
const BoostConfigFile = "hosts/boost-config.toml"

// BoostConfigVault commits paths in the vault; VaultFiles' Commit, narrowed.
type BoostConfigVault interface {
	Commit(ctx context.Context, message string, paths []string) ([]string, error)
}

// ConfigExportBoost rewrites the vault's hosts/boost-config.toml from the
// home's config file: `mw config export-boost` (mw-gq6.456). It carries the
// [tests] and [changed_specs] tables (domain.BoostTableNames) and nothing
// else of the config, and commits that one file.
type ConfigExportBoost struct {
	// Config is the home's ~/.config/mw/config.toml.
	Config ConfigFile
	// Target is the vault's hosts/boost-config.toml.
	Target ConfigFile
	// Vault commits Target, as BoostConfigFile, after it is written.
	Vault BoostConfigVault
	// Home and Host are the vault's home file and this host's name: only the
	// home exports, since a Boost's own tables are a part copied from it.
	Home HomeFile
	Host string
	Out  io.Writer
}

// Run writes Target from Config's tables and commits it, printing each line
// it brings level; with dryRun it prints the same and writes nothing. A file
// already level and spelled as mw writes it is left alone. It refuses, writing
// nothing, on a host that is not the home or whose home cannot be told, and a
// config that is missing or whose tables do not read.
func (e ConfigExportBoost) Run(ctx context.Context, dryRun bool) error {
	home, err := IsHome(ctx, e.Home, e.Host)
	if err != nil {
		return fmt.Errorf("cannot tell whether %s is the home, so nothing was exported: %w", e.Host, err)
	}
	if !home {
		return fmt.Errorf("%s is not the home: a Boost's tables are a copy of the home's, so export on the home", e.Host)
	}
	data, exists, err := e.Config.Read()
	if err != nil {
		return fmt.Errorf("reading %s: %w", e.Config.Path(), err)
	}
	if !exists {
		return fmt.Errorf("there is no %s, so nothing was exported", e.Config.Path())
	}
	file, err := domain.ParseConfigTOML(string(data))
	if err != nil {
		return fmt.Errorf("%s does not parse as TOML, so nothing was exported: %w", e.Config.Path(), err)
	}
	tables, err := domain.BoostTablesOf(file)
	if err != nil {
		return fmt.Errorf("%s: %w, so nothing was exported", e.Config.Path(), err)
	}
	text := tables.Text()

	old, exists, err := e.Target.Read()
	if err != nil {
		return fmt.Errorf("reading %s: %w", e.Target.Path(), err)
	}
	if exists && string(old) == text {
		fmt.Fprintf(e.Out, "%s is already level with %s: nothing to write\n", e.Target.Path(), e.Config.Path())
		return nil
	}
	drift := domain.BoostTablesDrift(tables, nil)
	if exists {
		if was, err := domain.ParseConfigTOML(string(old)); err != nil {
			drift = []string{fmt.Sprintf("the old file does not parse as TOML: %v", err)}
		} else if wasTables, err := domain.BoostTablesOf(was); err != nil {
			drift = []string{fmt.Sprintf("the old file: %v", err)}
		} else {
			drift = domain.BoostTablesDrift(tables, wasTables)
		}
	}

	if dryRun {
		fmt.Fprintln(e.Out, "dry run: nothing is written")
	}
	fmt.Fprintf(e.Out, "from: %s\n", e.Config.Path())
	fmt.Fprintf(e.Out, "to: %s\n", e.Target.Path())
	if len(drift) == 0 {
		fmt.Fprintln(e.Out, "the tables are level; the file is rewritten as mw writes it")
	}
	for _, line := range drift {
		fmt.Fprintln(e.Out, line)
	}
	if dryRun {
		return nil
	}
	if err := e.Target.Write([]byte(text)); err != nil {
		return fmt.Errorf("writing %s: %w", e.Target.Path(), err)
	}
	message := fmt.Sprintf("%s: [tests] and [changed_specs] from %s's config.toml (mw config export-boost)", BoostConfigFile, e.Host)
	committed, err := commitWritten(ctx, e.Vault, message, []string{BoostConfigFile})
	if err != nil {
		return fmt.Errorf("%s is written but not committed: %w", e.Target.Path(), err)
	}
	if len(committed) > 0 {
		fmt.Fprintf(e.Out, "committed %s in the vault; the next mw sync carries it\n", BoostConfigFile)
	}
	return nil
}

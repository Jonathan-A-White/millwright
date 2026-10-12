// Package configfile is ~/.config/mw/config.toml as a file the config commands
// read, copy and replace.
package configfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File is one config file on disk.
type File struct {
	path string
}

// New is the config file at path.
func New(path string) File { return File{path: path} }

// Path is where the file is.
func (f File) Path() string { return f.path }

// Read is the file's bytes, and whether it exists.
func (f File) Read() ([]byte, bool, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Backup copies the file to its path plus ".bak-" and suffix, with the file's
// mode, beside it. A backup that is already there is left alone and the copy
// is named -2, -3 and so on.
func (f File) Backup(suffix string) (string, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(f.path)
	if err != nil {
		return "", err
	}
	name := f.path + ".bak-" + suffix
	for n := 2; ; n++ {
		out, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if errors.Is(err, fs.ErrExist) {
			name = fmt.Sprintf("%s.bak-%s-%d", f.path, suffix, n)
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := out.Write(data)
		if cerr := out.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(name)
			return "", werr
		}
		return name, nil
	}
}

// Write replaces the file's bytes by writing a new file beside it and renaming
// it over the old, so a reader never sees half a file. A symlink is followed:
// the file it points at is the one replaced. The mode is kept; a new file is
// private to its owner.
func (f File) Write(data []byte) error {
	target := f.path
	if resolved, err := filepath.EvalSymlinks(f.path); err == nil {
		target = resolved
	}
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

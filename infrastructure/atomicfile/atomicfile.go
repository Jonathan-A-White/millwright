// Package atomicfile replaces a file whole: the new content is written to a
// file of its own beside the target and renamed over it, so a reader never sees
// half of it and a writer killed half way leaves the old file.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write puts data in path with mode perm, replacing what was there. The temp
// file is made by os.CreateTemp in path's directory, so it is named for this
// call alone and two writers at once never share it; the later rename wins,
// whole. The data is synced before the rename, and the temp file is removed on
// any failure. The directory must exist.
func Write(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.new")
	if err != nil {
		return fmt.Errorf("making a file beside %s: %w", path, err)
	}
	next := f.Name()
	fail := func(format string, err error) error {
		_ = f.Close()
		_ = os.Remove(next)
		return fmt.Errorf(format, next, err)
	}
	if _, err := f.Write(data); err != nil {
		return fail("writing %s: %w", err)
	}
	if err := f.Chmod(perm); err != nil {
		return fail("setting the mode of %s: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fail("syncing %s: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("closing %s: %w", next, err)
	}
	if err := os.Rename(next, path); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

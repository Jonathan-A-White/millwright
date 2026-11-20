// Package gocache reads and tidies Go's build cache on this host's disk.
package gocache

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.GoCacheDisk = Disk{}

// Dir is where this host's Go keeps its build cache: GOCACHE when set, else
// <user cache dir>/go-build.
func Dir() (string, error) {
	if dir := os.Getenv("GOCACHE"); dir != "" && dir != "off" {
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "go-build"), nil
}

// entryName matches the files Go writes into the cache: a hex id and "-a"
// (an action) or "-d" (data), in a two-hex-digit directory. Its own README and
// trim.txt, and anything else a person put there, never match.
var entryName = regexp.MustCompile(`^[0-9a-f]+-[ad]$`)
var entryDir = regexp.MustCompile(`^[0-9a-f]{2}$`)

// Disk is the real disk.
type Disk struct{}

// Entries implements application.GoCacheDisk.
func (Disk) Entries(ctx context.Context, dir string) ([]application.GoCacheEntry, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	var out []application.GoCacheEntry
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A file Go removed under the walk is not an error.
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() || !entryName.MatchString(d.Name()) || !entryDir.MatchString(filepath.Base(filepath.Dir(path))) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		out = append(out, application.GoCacheEntry{Path: path, Size: info.Size(), Used: info.ModTime()})
		return nil
	})
	return out, err
}

// Remove implements application.GoCacheDisk; a file already gone is removed.
func (Disk) Remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

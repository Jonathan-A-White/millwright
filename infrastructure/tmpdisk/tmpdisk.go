// Package tmpdisk reads and tidies what tools leave in the system temp.
package tmpdisk

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

var _ application.TmpDisk = Disk{}

// The names of what the tidy may delete, directly under the temp directory.
const (
	nodeCache       = "node-compile-cache"
	playwrightCache = "playwright-transform-cache-"
	testDirPrefix   = "Test"
)

// Disk is the real disk. Uid is the user whose files it reads and deletes; zero
// reads the user running it (root's own, 0, is therefore never a default).
type Disk struct {
	Uid int
}

func (d Disk) uid() int {
	if d.Uid != 0 {
		return d.Uid
	}
	return os.Getuid()
}

func owned(info fs.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

// Entries implements application.TmpDisk. Only the temp directory's own top
// level is listed, then the one cache or test directory under it that matches;
// a symbolic link is never followed.
func (d Disk) Entries(ctx context.Context, dir string) ([]application.TmpEntry, error) {
	top, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	uid := d.uid()
	var out []application.TmpEntry
	for _, e := range top {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		name := e.Name()
		cache := name == nodeCache || strings.HasPrefix(name, playwrightCache)
		test := strings.HasPrefix(name, testDirPrefix)
		if !e.IsDir() || !(cache || test) {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || !owned(info, uid) {
			continue
		}
		if test {
			size, newest, err := tree(ctx, path, uid)
			if err != nil {
				return out, err
			}
			out = append(out, application.TmpEntry{Path: path, Size: size, Used: newest})
			continue
		}
		err = filepath.WalkDir(path, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return nil // a file gone, or one that is not ours to open
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !de.Type().IsRegular() {
				return nil
			}
			fi, err := de.Info()
			if err != nil || !owned(fi, uid) {
				return nil
			}
			out = append(out, application.TmpEntry{Path: p, Size: fi.Size(), Used: fi.ModTime()})
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// tree totals the user's files under path and reports the newest modification
// time of anything in it (the directories included): a test dir with one
// thing touched an hour ago is not abandoned.
func tree(ctx context.Context, path string, uid int) (int64, time.Time, error) {
	var size int64
	var newest time.Time
	err := filepath.WalkDir(path, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fi, err := de.Info()
		if err != nil {
			return nil
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
		if fi.Mode().IsRegular() && owned(fi, uid) {
			size += fi.Size()
		}
		return nil
	})
	return size, newest, err
}

// Used implements application.TmpDisk: the bytes of regular files under dir
// that the user owns, links not followed, what cannot be read skipped.
func (d Disk) Used(ctx context.Context, dir string) (int64, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0, nil
	}
	uid := d.uid()
	var total int64
	err := filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !de.Type().IsRegular() {
			return nil
		}
		if fi, err := de.Info(); err == nil && owned(fi, uid) {
			total += fi.Size()
		}
		return nil
	})
	return total, err
}

// Remove implements application.TmpDisk; what is already gone is removed. A
// directory goes with what is in it, but only one named Test*: the tidy never
// lists another, and this is the belt for the braces.
func (Disk) Remove(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		if !strings.HasPrefix(filepath.Base(path), testDirPrefix) {
			return fmt.Errorf("%s is a directory that is not a Go test's", path)
		}
		return os.RemoveAll(path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

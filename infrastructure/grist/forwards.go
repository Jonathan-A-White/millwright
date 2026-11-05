package grist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Jonathan-A-White/millwright/application"
)

// ForwardsDir is the directory of the state directory holding the pictures of
// every forwarded grist, one directory for each, named by its grist's txid.
const ForwardsDir = "forwarded"

// Forwards keeps the pictures of a forwarded grist under Dir/forwarded/<txid>/
// (application's GristForwardStore). mw grist tidy deletes a grist's directory
// whole once GristKeepAge has passed since the forward. They are private: 0700 directories, 0600 files.
type Forwards struct {
	Dir string
}

// Forwards satisfies the ports.
var (
	_ application.GristForwardStore = (*Forwards)(nil)
	_ application.GristForwardShelf = (*Forwards)(nil)
)

// NewForwards is the pictures kept under the state directory dir.
func NewForwards(dir string) *Forwards { return &Forwards{Dir: dir} }

// Keep implements application.GristForwardStore: picture-N.<ext>, N from 1,
// written again if an earlier try left them there. It reports each full path.
func (f *Forwards) Keep(_ context.Context, txid string, pictures []application.GristRunAttachment) ([]string, error) {
	dir := filepath.Join(f.Dir, ForwardsDir, application.GristRunName(txid))
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("making %s: %w", dir, err)
	}
	paths := make([]string, 0, len(pictures))
	for i, p := range pictures {
		path := filepath.Join(dir, fmt.Sprintf("picture-%d%s", i+1, p.Ext))
		if err := os.WriteFile(path, p.Data, 0o600); err != nil {
			return nil, fmt.Errorf("writing %s: %w", path, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// Shelved implements application.GristForwardShelf: every directory of
// forwarded/, oldest first. A directory was forwarded when the last picture
// was kept in it, so it is the newest of its own time and its files'.
func (f *Forwards) Shelved(_ context.Context) ([]application.GristShelved, error) {
	root := filepath.Join(f.Dir, ForwardsDir)
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the forwarded pictures: %w", err)
	}
	var shelved []application.GristShelved
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Join(root, e.Name()), err)
		}
		held := application.GristShelved{Name: e.Name(), Forwarded: info.ModTime()}
		files, err := os.ReadDir(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Join(root, e.Name()), err)
		}
		for _, file := range files {
			fi, err := file.Info()
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", filepath.Join(root, e.Name(), file.Name()), err)
			}
			held.Files++
			held.Bytes += fi.Size()
			if fi.ModTime().After(held.Forwarded) {
				held.Forwarded = fi.ModTime()
			}
		}
		shelved = append(shelved, held)
	}
	sort.SliceStable(shelved, func(i, j int) bool { return shelved[i].Forwarded.Before(shelved[j].Forwarded) })
	return shelved, nil
}

// DropShelved implements application.GristForwardShelf: it removes the
// directory named, with its pictures. A name that is not one directory of
// forwarded/ is refused.
func (f *Forwards) DropShelved(_ context.Context, name string) error {
	if !singleSegment(name) {
		return fmt.Errorf("%q is not the name of a forwarded grist", name)
	}
	return os.RemoveAll(filepath.Join(f.Dir, ForwardsDir, name))
}

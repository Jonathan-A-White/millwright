package rig

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
)

// Files reads the checkouts of the rigs on this host from disk.
type Files struct{}

var _ application.RigFiles = Files{}

// Exists implements application.RigFiles: whether the pattern, a file, a
// directory or a glob of a story's files key, is in the checkout at rigDir. A
// glob with ** is found when the directory it starts from is there, since path
// has no way to say any depth.
func (Files) Exists(rigDir, pattern string) bool {
	if !strings.ContainsAny(pattern, "*?[") {
		_, err := os.Stat(filepath.Join(rigDir, filepath.FromSlash(pattern)))
		return err == nil
	}
	if strings.Contains(pattern, "**") {
		start := strings.SplitN(pattern, "**", 2)[0]
		if i := strings.LastIndex(start, "/"); i >= 0 {
			start = start[:i]
		} else {
			start = ""
		}
		info, err := os.Stat(filepath.Join(rigDir, filepath.FromSlash(start)))
		return err == nil && info.IsDir()
	}
	found, err := filepath.Glob(filepath.Join(rigDir, filepath.FromSlash(pattern)))
	return err == nil && len(found) > 0
}

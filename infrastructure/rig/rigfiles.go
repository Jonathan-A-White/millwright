package rig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
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

// FilesIn implements application.RigFiles: the files under dir in the checkout
// at rigDir, counted no further than one past most. What git and a package
// manager keep there (.git and the other dot directories, node_modules) is not
// counted: a story does not change it.
func (Files) FilesIn(rigDir, dir string, most int) (int, bool) {
	root := filepath.Join(rigDir, filepath.FromSlash(dir))
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return 0, false
	}
	var n int
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if n++; n > most {
			return filepath.SkipAll
		}
		return nil
	})
	return n, true
}

// Matching implements application.RigFiles: the files in the checkout at rigDir
// that the glob covers, counted no further than one past most. The walk starts
// at the directory the glob names before its first wildcard, and skips what
// FilesIn does not count.
func (Files) Matching(rigDir, pattern string, most int) int {
	start := ""
	for _, part := range strings.Split(pattern, "/") {
		if strings.ContainsAny(part, "*?[") {
			break
		}
		start = path.Join(start, part)
	}
	root := filepath.Join(rigDir, filepath.FromSlash(start))
	var n int
	_ = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if file != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(rigDir, file)
		if err != nil || !domain.Covers(pattern, filepath.ToSlash(rel)) {
			return nil
		}
		if n++; n > most {
			return filepath.SkipAll
		}
		return nil
	})
	return n
}

// callerSearchLimit is how long the search for test callers may take.
const callerSearchLimit = 20 * time.Second

// TestCallers implements application.RigFiles: the tracked test files of the
// checkout at rigDir (those under features/ or tests/, and any *_test.go) that
// name any of the stems, in one git grep (mw-gq6.661). The config's [tests] is a
// command, not a list of globs, so those three are the test files of every rig.
func (Files) TestCallers(rigDir string, stems []string) (int, error) {
	args := []string{"-C", rigDir, "grep", "-l", "-F", "--no-color"}
	for _, stem := range stems {
		args = append(args, "-e", stem)
	}
	args = append(args, "--", "features", "tests", ":(glob)**/*_test.go")
	ctx, cancel := context.WithTimeout(context.Background(), callerSearchLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err == nil && len(out) == 0 {
		return 0, nil
	}
	if err != nil {
		// git grep says no match with status 1 and nothing printed.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(out) == 0 {
			return 0, nil
		}
		return 0, fmt.Errorf("counting the test callers in %s: %w", rigDir, err)
	}
	return len(strings.Split(strings.TrimSpace(string(out)), "\n")), nil
}

// Docs reads one file of a rig's checkout on this host, for a seat's boot
// manifest entry that names a rig: the adapter behind application.RigDocs.
// Rigs is where each rig is checked out, by name, as the config's [rigs] says.
type Docs struct {
	Rigs map[string]string
}

var _ application.RigDocs = Docs{}

// RigFile implements application.RigDocs.
func (d Docs) RigFile(_ context.Context, rig, rel string) (string, error) {
	dir, here := d.Rigs[rig]
	if !here || dir == "" {
		return "", fmt.Errorf("the rig %s is not checked out on this host (the config's [rigs] does not name it)", rig)
	}
	switch {
	case rel == "":
		return "", fmt.Errorf("a path in the rig %s is needed", rig)
	case strings.HasPrefix(rel, "/"), strings.Contains(rel, `\`), strings.Contains(rel, ".."):
		return "", fmt.Errorf("%q would reach outside the rig %s", rel, rig)
	}
	text, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("reading %s of the rig %s: %w", rel, rig, err)
	}
	return string(text), nil
}

package application

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// The numbers behind SHAPE (scorecard measures 7 and 8 of
// docs/research/2026-11-scaling-for-agents.md).
const (
	// ShapeHotDays is how far back a file's commits are counted for the hot list.
	ShapeHotDays = 90
	// ShapeHotCommits is how many commits in those days make a file hot enough to
	// have its test callers weighed against the allowance.
	ShapeHotCommits = 10
	// ShapeTop is how many of the hottest files SHAPE names.
	ShapeTop = 5
)

// ShapeChange is one production-or-test file one commit changed: when the
// commit was made, the file's path (a renamed file under its new name), and the
// lines it added and removed there. A file a commit deleted has the lines it
// held as removed.
type ShapeChange struct {
	At      time.Time
	Path    string
	Added   int
	Removed int
}

// ShapeSource reads a rig's checkout for SHAPE. The real adapter is
// infrastructure/rig (Shape); the report reads, it writes nothing.
type ShapeSource interface {
	// Changes are the files of every commit since the time, merges left out.
	Changes(ctx context.Context, rigDir string, since time.Time) ([]ShapeChange, error)
	// Deleted are the files commits since the time deleted, each at its commit's
	// time; the lines are not read.
	Deleted(ctx context.Context, rigDir string, since time.Time) ([]ShapeChange, error)
	// Lines is how many lines of the file at path in the checkout are not
	// blank, and an error when the file is not there.
	Lines(rigDir, path string) (int, error)
	// TestCallersOf is RigFiles.TestCallers for each file alone: how many of
	// the rig's test files call what that file declares, by file.
	TestCallersOf(rigDir string, files []string) (map[string]int, error)
}

// HotFile is a production file the checkout changed often, with how big it is
// and how many tests lean on it.
type HotFile struct {
	Path        string `json:"path"`
	Commits     int    `json:"commits"`
	Lines       int    `json:"lines"`
	TestCallers int    `json:"test_callers"`
}

// Score is the file's commits times its lines, what the hot list is ranked by.
func (h HotFile) Score() int { return h.Commits * h.Lines }

// ShapeSection is SHAPE, the shape of one rig's code: the few files where work
// gathers, how many of the often-changed ones have more test callers than a
// size M story allows, and how much code the window added and took away.
type ShapeSection struct {
	Rig string `json:"rig"`
	// Unread, when set, is why the checkout could not be read; the rest is empty.
	Unread string `json:"unread,omitempty"`
	// Hot are the ShapeTop production files with the highest commits times
	// lines over HotDays, hottest first.
	Hot []HotFile `json:"hot_files"`
	// HotFiles are the production files changed in ShapeHotCommits commits or
	// more over HotDays that are still there; OverAllowance are those with more
	// test callers than Allowance, a size M story's.
	HotFiles      int `json:"hot_file_count"`
	OverAllowance int `json:"over_allowance"`
	Allowance     int `json:"allowance"`
	// Added and Removed are the production code lines the window added and took
	// away, and CodeFilesDeleted the production code files it deleted.
	Added            int `json:"lines_added"`
	Removed          int `json:"lines_removed"`
	CodeFilesDeleted int `json:"code_files_deleted"`
	// Commands, for the factory rig alone, are the commands of mw this host has
	// not run lately; nil for any other rig.
	Commands *CommandUse `json:"commands,omitempty"`
}

// shapeCodeExtensions are the files SHAPE counts as code.
var shapeCodeExtensions = map[string]bool{".go": true, ".ts": true, ".tsx": true, ".js": true, ".mjs": true, ".py": true, ".sh": true}

// shapeNotCode are the directories whose files are built, bundled or data and
// not written by hand.
var shapeNotCode = map[string]bool{"public": true, "data": true, "dist": true, "node_modules": true}

// shapeTestDirs are the directories whose files are tests or the fakes they use.
var shapeTestDirs = map[string]bool{"tests": true, "features": true, "apptest": true}

// IsProductionCode says a path in a rig is a code file that is not a test: its
// extension is one of .go, .ts, .tsx, .js, .mjs, .py and .sh, it is not a
// _test.go file, and no directory of it is public, data, dist, node_modules,
// tests, features or apptest.
func IsProductionCode(file string) bool {
	if !shapeCodeExtensions[path.Ext(file)] || strings.HasSuffix(file, "_test.go") {
		return false
	}
	dirs := strings.Split(path.Dir(file), "/")
	for _, dir := range dirs {
		if shapeNotCode[dir] || shapeTestDirs[dir] {
			return false
		}
	}
	return true
}

// ReadShape reads the SHAPE of the rig whose checkout is at rigDir. Hot files
// are counted over ShapeHotDays before now, and lines over the report's window.
// A checkout that cannot be read is an Unread section, not an error: the rest
// of the report stands.
func ReadShape(ctx context.Context, source ShapeSource, rigName, rigDir string, window ReportWindow, now time.Time) ShapeSection {
	section := ShapeSection{Rig: rigName, Allowance: domain.SizeM.CallersAllowed()}
	unread := func(err error) ShapeSection {
		section.Unread = strings.TrimSpace(err.Error())
		return section
	}
	if strings.TrimSpace(rigDir) == "" {
		return unread(fmt.Errorf("%s has no checkout on this host", rigName))
	}
	hotFrom := reportDay(now).AddDate(0, 0, -ShapeHotDays)
	from := window.Since
	if hotFrom.Before(from) {
		from = hotFrom
	}
	changes, err := source.Changes(ctx, rigDir, from)
	if err != nil {
		return unread(err)
	}
	deleted, err := source.Deleted(ctx, rigDir, window.Since)
	if err != nil {
		return unread(err)
	}

	commits := map[string]int{}
	for _, c := range changes {
		if !IsProductionCode(c.Path) {
			continue
		}
		if !c.At.Before(hotFrom) {
			commits[c.Path]++
		}
		if !c.At.Before(window.Since) && c.At.Before(window.Until) {
			section.Added += c.Added
			section.Removed += c.Removed
		}
	}
	for _, d := range deleted {
		if IsProductionCode(d.Path) && !d.At.Before(window.Since) && d.At.Before(window.Until) {
			section.CodeFilesDeleted++
		}
	}

	// The files to weigh: those the allowance counts, and every file that could
	// be among the hottest, which are the same few once they are sorted.
	var heated []HotFile
	for file, n := range commits {
		lines, err := source.Lines(rigDir, file)
		if err != nil || lines == 0 {
			continue // renamed away or deleted: it is not in the code today
		}
		heated = append(heated, HotFile{Path: file, Commits: n, Lines: lines})
	}
	sort.Slice(heated, func(i, j int) bool {
		if a, b := heated[i].Score(), heated[j].Score(); a != b {
			return a > b
		}
		return heated[i].Path < heated[j].Path
	})
	var weigh []string
	for i, h := range heated {
		if h.Commits >= ShapeHotCommits || i < ShapeTop {
			weigh = append(weigh, h.Path)
		}
	}
	callers, err := source.TestCallersOf(rigDir, weigh)
	if err != nil {
		return unread(err)
	}
	for i := range heated {
		h := &heated[i]
		h.TestCallers = callers[h.Path]
		if h.Commits >= ShapeHotCommits {
			section.HotFiles++
			if h.TestCallers > section.Allowance {
				section.OverAllowance++
			}
		}
	}
	if len(heated) > ShapeTop {
		heated = heated[:ShapeTop]
	}
	section.Hot = append([]HotFile{}, heated...)
	return section
}

// write prints SHAPE; nothing when the report has none.
func (s *ShapeSection) write(b *strings.Builder) {
	if s == nil {
		return
	}
	fmt.Fprintf(b, "\nSHAPE %s\n", s.Rig)
	if s.Unread != "" {
		wrapInto(b, "  ", "not read: "+s.Unread)
		s.Commands.write(b)
		return
	}
	fmt.Fprintf(b, "  hottest files, commits times lines, %d days\n", ShapeHotDays)
	for _, h := range s.Hot {
		fmt.Fprintf(b, "  %s\n", fitLine(h.Path, ReportWidth-2))
		fmt.Fprintf(b, "    %d commits, %d lines, %d test callers\n", h.Commits, h.Lines, h.TestCallers)
	}
	if len(s.Hot) == 0 {
		b.WriteString("  no code file was changed in them\n")
	}
	fmt.Fprintf(b, "  hot files over the allowance %d of %d\n", s.OverAllowance, s.HotFiles)
	fmt.Fprintf(b, "  lines added %d, removed %d, code files deleted %d\n", s.Added, s.Removed, s.CodeFilesDeleted)
	s.Commands.write(b)
}

// write prints the commands not run, and their names; nothing when there is no
// such part.
func (u *CommandUse) write(b *strings.Builder) {
	if u == nil {
		return
	}
	if u.Unread != "" {
		wrapInto(b, "  ", "command log not read: "+u.Unread)
		return
	}
	fmt.Fprintf(b, "  not run in %d days: %d\n", u.Days, len(u.NotRun))
	if len(u.NotRun) > 0 {
		wrapInto(b, "    ", strings.Join(u.NotRun, ", "))
	}
}

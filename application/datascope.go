package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// A rig whose stories sometimes change nothing but data files (a book's mapping
// file, a voice's JSON) need not run its whole gate on them. A host can name, in
// the `[data_parts.<rig>]` table of its config file, the paths of the rig's data
// files and the command that checks them (the data's own tests, a typecheck). A
// story whose branch changes only files inside those paths has, in place of the
// rig's gate and its parts, one part of its own, named "data scope": mw check runs
// the command as that part, and mw next trusts a pass of it at the tip as it trusts
// the parts of a split gate (nextparts.go). One file outside the paths and the story
// is gated whole, as for a rig with no key. The batch gate (mw land) and the
// nightly suite never read the key: they run everything on the merged result.

// DataScope is the `[data_parts.<rig>]` key of a rig: Paths are globs of its data
// files, a file, a directory or a glob as domain.Covers reads them, and Command is
// the line that checks the data.
type DataScope struct {
	Paths   []string
	Command string
}

// DataScopes is the optional part of a GateParts that says a rig's data scope:
// nothing, for a rig with no `[data_parts.<rig>]` key.
type DataScopes interface {
	DataScope(rig string) (DataScope, bool)
}

// DataScopeName is the name of the one part of a story gated in its data scope, in
// what a check prints and in the ledger line.
const DataScopeName = "data scope"

// dataGate is the gate of one story whose every changed file is inside its rig's
// data paths: the command that is its one part, and how many files it changed.
type dataGate struct {
	scope DataScope
	rig   string
	files int
}

// part is the one part of the story's gate.
func (d *dataGate) part() GatePart {
	return GatePart{Name: DataScopeName, Command: d.scope.Command}
}

// parts is the story's gate as a split gate of one part.
func (d *dataGate) parts() []GatePart { return []GatePart{d.part()} }

// words are what a check says of such a story, after dataScopeLineStart, and what
// its log keeps.
func (d *dataGate) words() string {
	return fmt.Sprintf("%d changed file(s), every one inside the [data_parts.%s] paths, so the rig's data check runs in place of the rig's parts: `%s`",
		d.files, d.rig, d.scope.Command)
}

// dataScopeLineStart begins the line of a check's output that says the story was
// gated in its data scope (CheckedDataScope).
const dataScopeLineStart = "  " + DataScopeName + "  "

// CheckedDataScope says the log a check kept is of a check in the data scope, and
// of how many files: the line words writes, which a check of the whole gate never
// prints. It is how mw next tells a pass of the data's own checks from a pass of
// the whole gate.
func CheckedDataScope(log string) (files int, ok bool) {
	for _, line := range strings.Split(log, "\n") {
		if rest, found := strings.CutPrefix(line, dataScopeLineStart); found {
			if _, err := fmt.Sscanf(rest, "%d changed file(s)", &files); err == nil {
				return files, true
			}
		}
	}
	return 0, false
}

// dataGateOf reads whether the story's whole change is inside its rig's data
// paths: nil when the host names none for the rig, when the branch changes no file
// or one file outside them, or when its files cannot be read; the story is then
// gated whole, as it was before the key. base is the commit the branch's files are
// counted from.
func dataGateOf(ctx context.Context, parts GateParts, changes BranchChanges, base string, c *closeOut) *dataGate {
	if c.dataRead {
		return c.data
	}
	c.dataRead = true
	scopes, ok := parts.(DataScopes)
	if !ok || changes == nil {
		return nil
	}
	scope, ok := scopes.DataScope(c.path.Rig)
	if !ok || scope.Command == "" || len(scope.Paths) == 0 {
		return nil
	}
	files, err := changes.ChangedFiles(ctx, c.rigDir, base, c.branch)
	if err != nil || len(files) == 0 {
		return nil
	}
	for _, file := range files {
		if !insideDataPaths(scope.Paths, file) {
			return nil
		}
	}
	c.data = &dataGate{scope: scope, rig: c.path.Rig, files: len(files)}
	return c.data
}

func insideDataPaths(paths []string, file string) bool {
	for _, written := range paths {
		for _, glob := range domain.FileList(written) {
			if domain.Covers(glob, file) {
				return true
			}
		}
	}
	return false
}

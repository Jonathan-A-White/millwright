package application

import (
	"context"
	"fmt"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FileShare is a file a ready story names that a running story on its rig
// names too, or has changed on its branch already: the ready story waits for
// that one, for each landing moves the target branch under the other and they
// come back as merge conflicts (mw-gq6.349).
type FileShare struct {
	File string
	With string
	// Host is where the story waited for is claimed, when that is not the host
	// asking (mw-gq6.389); empty for one of its own.
	Host string
	// Rig is the rig the two stories are on, said when a story names no files
	// on a rig that expects them (mw-gq6.390); Unkeyed says which of the two
	// that is, and File is empty then.
	Rig     string
	Unkeyed Unkeyed
}

// Unkeyed is which story of a share names no files key on a rig that expects
// one: such a story may change any file, so it shares every file with every
// story of its rig (mw-gq6.390).
type Unkeyed int

const (
	// KeyedBoth is a share of a file two stories name.
	KeyedBoth Unkeyed = iota
	// ReadyUnkeyed is a ready story that names no files, waiting for a running one.
	ReadyUnkeyed
	// RunningUnkeyed is a running story that names no files, which a ready one waits for.
	RunningUnkeyed
)

// String is the share as mw dispatch and mw status say it.
func (s FileShare) String() string {
	var line string
	switch s.Unkeyed {
	case ReadyUnkeyed:
		line = fmt.Sprintf("waiting: no files key (%s expects one), so it shares every file with %s", s.Rig, s.With)
	case RunningUnkeyed:
		line = fmt.Sprintf("waiting: %s has no files key (%s expects one), so it shares every file with it", s.With, s.Rig)
	default:
		line = fmt.Sprintf("waiting: shares %s with %s", s.File, s.With)
	}
	if s.Host != "" {
		line += fmt.Sprintf(" (running on %s)", s.Host)
	}
	return line
}

// fileShares finds the running story a ready one shares a file with. A ready
// story that names no files shares none, and is dispatched as it always was,
// unless its rig expects a files key (mw-gq6.390): then it shares every file
// with every story of the rig, as a running one that names none does.
// What a running story's branch has changed is read once a pass, and only for
// a rig a ready story naming files is on.
type fileShares struct {
	changes BranchChanges
	// rules say which rigs expect a files key; nil expects none.
	rules   EpicRules
	expects map[string]bool
	rigs    map[string]string
	remote  string
	running []StoryDetail
	// away is the host each story of running that another host claimed was
	// claimed on: only its files key is read, for its branch is on that host
	// (mw-gq6.389).
	away    map[string]string
	changed map[string][]string
	// notes are what could not be read, one line each: a branch whose changes
	// cannot be read holds nothing back.
	notes []string
}

// add counts a story as running, as one this pass started is from then on.
func (f *fileShares) add(detail StoryDetail) {
	f.running = append(f.running, detail)
}

// addAway counts a story another host has claimed as running there: it keeps
// its files from the ready stories here, and counts for nothing else.
func (f *fileShares) addAway(detail StoryDetail, host string) {
	if f.away == nil {
		f.away = map[string]string{}
	}
	f.away[detail.Story.ID] = host
	f.running = append(f.running, detail)
}

// shared is the first running story on the ready story's rig it shares a file
// with, and the file, in the order the running stories were added.
func (f *fileShares) shared(ctx context.Context, ready StoryDetail) (FileShare, bool) {
	path := ready.Merged()
	ours := domain.FileList(path.Files)
	expects := f.filesExpected(ctx, path.Rig)
	if len(ours) == 0 {
		if !expects {
			return FileShare{}, false
		}
		// A branch an earlier attempt left, with files changed on it, says what
		// the story touches as well as a files key does.
		if ours = f.branchChanged(ctx, ready); len(ours) == 0 {
			for _, running := range f.running {
				if running.Story.ID != ready.Story.ID && running.Merged().Rig == path.Rig {
					return FileShare{With: running.Story.ID, Host: f.away[running.Story.ID], Rig: path.Rig, Unkeyed: ReadyUnkeyed}, true
				}
			}
			return FileShare{}, false
		}
	}
	for _, running := range f.running {
		theirs := running.Merged()
		if running.Story.ID == ready.Story.ID || theirs.Rig != path.Rig {
			continue
		}
		if expects && len(domain.FileList(theirs.Files)) == 0 {
			return FileShare{With: running.Story.ID, Host: f.away[running.Story.ID], Rig: path.Rig, Unkeyed: RunningUnkeyed}, true
		}
		if file, ok := domain.SharedFile(ours, domain.FileList(theirs.Files)); ok {
			return FileShare{File: file, With: running.Story.ID, Host: f.away[running.Story.ID]}, true
		}
		if _, elsewhere := f.away[running.Story.ID]; elsewhere {
			continue
		}
		if file, ok := domain.SharedFile(ours, f.branchChanged(ctx, running)); ok {
			return FileShare{File: file, With: running.Story.ID}, true
		}
	}
	return FileShare{}, false
}

// branchChanged is what the running story's branch has changed from its target
// branch as the rig's remote has it, nothing when that cannot be known here.
func (f *fileShares) branchChanged(ctx context.Context, running StoryDetail) []string {
	id := running.Story.ID
	if files, read := f.changed[id]; read {
		return files
	}
	if f.changed == nil {
		f.changed = map[string][]string{}
	}
	path := running.Merged()
	rigDir, checkedOut := f.rigs[path.Rig]
	if f.changes == nil || !checkedOut || path.Branch == "" {
		f.changed[id] = nil
		return nil
	}
	remote := f.remote
	if remote == "" {
		remote = DefaultRemote
	}
	files, err := f.changes.ChangedFiles(ctx, rigDir, StartPoint(remote, path.Branch), StoryBranch(id))
	if err != nil {
		f.notes = append(f.notes, fmt.Sprintf("what the branch of %s has changed could not be read, so it holds nothing back: %v", id, err))
	}
	f.changed[id] = files
	return files
}

// filesExpected is whether the rig's file in the vault says files_expected =
// true, read once a pass. A rig whose file cannot be read expects nothing, and
// the pass says so.
func (f *fileShares) filesExpected(ctx context.Context, rig string) bool {
	if f.rules == nil || rig == "" {
		return false
	}
	if expects, read := f.expects[rig]; read {
		return expects
	}
	if f.expects == nil {
		f.expects = map[string]bool{}
	}
	required, err := f.rules.EpicRequirements(ctx, rig)
	if err != nil {
		f.notes = append(f.notes, fmt.Sprintf("whether the rig %s expects a files key could not be read, so a story with none holds nothing back: %v", rig, err))
	}
	f.expects[rig] = required.FilesExpected
	return required.FilesExpected
}

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
}

// String is the share as mw dispatch and mw status say it.
func (s FileShare) String() string {
	if s.Host != "" {
		return fmt.Sprintf("waiting: shares %s with %s (running on %s)", s.File, s.With, s.Host)
	}
	return fmt.Sprintf("waiting: shares %s with %s", s.File, s.With)
}

// fileShares finds the running story a ready one shares a file with. A ready
// story that names no files shares none, and is dispatched as it always was.
// What a running story's branch has changed is read once a pass, and only for
// a rig a ready story naming files is on.
type fileShares struct {
	changes BranchChanges
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
	if len(ours) == 0 {
		return FileShare{}, false
	}
	for _, running := range f.running {
		theirs := running.Merged()
		if running.Story.ID == ready.Story.ID || theirs.Rig != path.Rig {
			continue
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

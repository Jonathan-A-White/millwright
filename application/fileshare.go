package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/Jonathan-A-White/millwright/domain"
)

// FileShare is a file a ready story names that a running story on its rig
// names too, or has changed on its branch already (or recorded as changed,
// which any host reads: files_changed, mw-gq6.406): the ready story waits for
// that one, for each landing moves the target branch under the other and they
// come back as merge conflicts (mw-gq6.349).
type FileShare struct {
	File string
	With string
	// Host is where the story waited for is claimed, when that is not the host
	// asking (mw-gq6.389); empty for one of its own.
	Host string
	// Unkeyed says why the ready story waits when it is not for a file a
	// running story shares: NeedsFilesKey, and File and Host are empty then.
	Unkeyed Unkeyed
	// Refused says the story waited for is a claim whose landing was refused and
	// waits on the Mayor (mw-gq6.941): it keeps its files until a retry or a
	// hold, so the wait does not end by itself. Only mw status knows it.
	Refused bool
	// Kept says the story waited for is held, not running, and its kept branch
	// carries work ahead of its target: a hold leaves that branch for the next
	// attempt to be cut from, and a story landing on the same file meanwhile
	// turns it into a merge conflict (mw-gq6.881).
	Kept bool
}

// Unkeyed is what a share is for. A ready story that names no files key on a
// rig that expects one is held until it has one, and holds nothing else back
// (mw-gq6.599); a running story that names none overlaps only by the files its
// branch has changed, as one that names files does.
type Unkeyed int

const (
	// KeyedBoth is a share of a file two stories name or have changed.
	KeyedBoth Unkeyed = iota
	// NeedsFilesKey is a ready story that names no files, on a rig that expects them.
	NeedsFilesKey
)

// The titles mw next and mw land file the bug that carries a red main under
// (mw-gq6.645, mw-gq6.1395); carriedBy files them and IsRedMainCarrier knows them.
const (
	MainRedCarrierPrefix = "[bug] main is red: "
	HostGapCarrierPrefix = "[bug] host gap: "
)

// IsRedMainCarrier says a story is the bug mw next or mw land filed for a red
// main, or for a host's gap that fails main's tests: one predicate for mw
// dispatch, mw status and the cloud's demand (mw-gq6.1705).
func IsRedMainCarrier(detail StoryDetail) bool {
	title := strings.TrimSpace(detail.Story.Title)
	return strings.HasPrefix(title, MainRedCarrierPrefix) || strings.HasPrefix(title, HostGapCarrierPrefix)
}

// waitsOnTheCarrier says a story's landing is itself held on the bug that carries
// main's red, so that bug must not wait for it: it is queued for its batch on
// the same target as the carrier, which the batch holds on main's red, or it is
// held on main's red by its own close-out (MainRedState). Each lands only after
// the carrier does, so a carrier held for its files would be a cycle with no end
// (lampas mw-y1k16cd and mw-fnvjof.17.263, mw-gq6.1705). A story a Builder is
// running, or one held or refused for any other reason, is no such story.
func waitsOnTheCarrier(carrier, other StoryDetail) bool {
	if other.Merged().Branch != carrier.Merged().Branch {
		return false
	}
	if QueuedForBatch(other) {
		return true
	}
	for _, label := range other.Labels {
		if wait, held := strings.CutPrefix(strings.TrimSpace(label), MainRedState+":"); held && wait != MainRedFixed {
			return true
		}
	}
	return false
}

// String is the share as mw dispatch and mw status say it.
func (s FileShare) String() string {
	if s.Unkeyed == NeedsFilesKey {
		return fmt.Sprintf("needs files key: mw path set %s files=<csv>", s.With)
	}
	line := fmt.Sprintf("waiting: shares %s with %s", s.File, s.With)
	if s.Host != "" {
		line += fmt.Sprintf(" (running on %s)", s.Host)
	}
	if s.Refused {
		line += fmt.Sprintf("; %s is refused, waiting on the Mayor", s.With)
	}
	if s.Kept {
		line += fmt.Sprintf("; %s is held, its kept branch carries work", s.With)
	}
	return line
}

// fileShares finds the running story a ready one shares a file with. A ready
// story that names no files shares none, and is dispatched as it always was,
// unless its rig expects a files key: then it needs one (mw-gq6.599) and is
// held for it, holding nothing else back. A running story that names none is
// compared by what its branch has changed, as one that names files is. A story
// of the tester formula that names none claims no files, and neither waits nor
// is waited for (mw-gq6.413).
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
	away map[string]string
	// kept are the held stories on the rigs: each keeps its files from a ready
	// story while its kept branch is ahead of its target (mw-gq6.881).
	kept    []StoryDetail
	changed map[string][]string
	// notes are what could not be read, one line each: a branch whose changes
	// cannot be read holds nothing back.
	notes []string
}

// add counts a story as running, as one this pass started is from then on.
func (f *fileShares) add(detail StoryDetail) {
	f.running = append(f.running, detail)
}

// addKept counts a held story as holding its files for as long as its kept
// branch is ahead of its target: a hold gives up the session and leaves the
// branch, from which the next attempt is cut (mw-gq6.881). It is no running
// story and counts for nothing else.
func (f *fileShares) addKept(detail StoryDetail) {
	f.kept = append(f.kept, detail)
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
	if claimsNoFiles(path) {
		return FileShare{}, false
	}
	ours := domain.FileList(path.Files)
	carrier := IsRedMainCarrier(ready)
	expects := f.filesExpected(ctx, path.Rig)
	if len(ours) == 0 {
		if !expects {
			return FileShare{}, false
		}
		// A branch an earlier attempt left, with files changed on it, says what
		// the story touches as well as a files key does.
		if ours = f.branchChanged(ctx, ready); len(ours) == 0 {
			return FileShare{With: ready.Story.ID, Unkeyed: NeedsFilesKey}, true
		}
	}
	for _, running := range f.running {
		theirs := running.Merged()
		if running.Story.ID == ready.Story.ID || theirs.Rig != path.Rig || claimsNoFiles(theirs) {
			continue
		}
		// A red-main carrier lands first and a story whose landing waits on it
		// meets it at the batch (mw-gq6.1705).
		if carrier && waitsOnTheCarrier(ready, running) {
			continue
		}
		// What the story has really changed, as mw next recorded it where any host
		// reads it, counts with its files key (mw-gq6.406).
		if file, ok := domain.SharedFile(ours, append(domain.FileList(theirs.Files), running.FilesChanged...)); ok {
			return FileShare{File: file, With: running.Story.ID, Host: f.away[running.Story.ID]}, true
		}
		if _, elsewhere := f.away[running.Story.ID]; elsewhere {
			continue
		}
		if file, ok := domain.SharedFile(ours, f.branchChanged(ctx, running)); ok {
			return FileShare{File: file, With: running.Story.ID}, true
		}
	}
	return f.sharedWithKept(ctx, ours, ready)
}

// sharedWithKept is the first held story on the rig whose kept branch is ahead
// of its target and that names or has changed a file of ours (mw-gq6.881). A held
// story whose branch is not there, has been merged or has no commits beyond its
// target has nothing to lose, and holds nothing back. A story held on another
// host has its branch there, which this host cannot read: it holds nothing back.
func (f *fileShares) sharedWithKept(ctx context.Context, ours []string, ready StoryDetail) (FileShare, bool) {
	rig, readyID, carrier := ready.Merged().Rig, ready.Story.ID, IsRedMainCarrier(ready)
	for _, held := range f.kept {
		theirs := held.Merged()
		if held.Story.ID == readyID || theirs.Rig != rig || claimsNoFiles(theirs) {
			continue
		}
		if carrier && waitsOnTheCarrier(ready, held) {
			continue
		}
		branch := f.branchChanged(ctx, held)
		if len(branch) == 0 {
			continue
		}
		if file, ok := domain.SharedFile(ours, append(append(domain.FileList(theirs.Files), held.FilesChanged...), branch...)); ok {
			return FileShare{File: file, With: held.Story.ID, Kept: true}, true
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

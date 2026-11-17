package application

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/domain"
)

// PathUnset is what `mw path show` prints for a path key the story has no
// value for.
const PathUnset = "(unset)"

// PathSet sets a story's path keys in one command: the seven keys of a sibling
// story's path (From) — never its files, which are its own — the keys named in
// Pairs laid over them, or both. The Mayor files each story under an existing
// epic with its own path, which was seven `bd update --set-metadata` calls one
// at a time, because the tracker takes a single writer; here the whole path
// goes in as one write, so there is never a second writer to wait for and never
// a half-set path.
//
// A path is adjusted only before a story starts, so a story that is claimed or
// finished is refused. Every refusal comes before the write: a refused set
// changes nothing.
type PathSet struct {
	Tracker WorkTracker
	// Now is the clock; the zero value is the real one.
	Now func() time.Time

	// Rigs are the rigs checked out on this host, by name, and RigFiles counts
	// the files of a directory in one: a files key that names a directory of
	// more than MostFilesInADirectory files is refused (mw-gq6.442), and so is a
	// glob matching more. A rig not checked out here cannot be counted in, so a
	// whole directory named for it ("src/") is refused there too, and a glob
	// ending in a wildcard ("tests/e2e/*.ts").
	Rigs     map[string]string
	RigFiles RigFiles

	// Rules is where the story's rig's file is read from, to keep the epic's
	// last story last when the story is filed into it (KeepDemoLast). A nil
	// Rules keeps nothing.
	Rules EpicRules

	// Readings are the usage readings mw usage read keeps. A story labelled
	// model-story is released on path set only at the pace of mw release-paced
	// (ReleasePacedUsageLine, ReleasePacedMax), which needs them: with none
	// (a nil Notes) such a story stays held.
	Readings UsageReadings

	// Out is where the path that was set is printed. A nil Out prints nothing.
	Out io.Writer

	// After is --after: the stories the story waits on, each added as a blocker
	// before the path is written. Nil is no --after; an empty, non-nil list is
	// --after none, a story that waits on nothing. On a rig whose file says
	// release_on_path = true a given --after also releases the held story last,
	// unless it is held on purpose (mw-t5shinh.16).
	After []string
	// Hold is --hold: why the story stays held. It is written as a HELD: comment
	// and the story is not released.
	Hold string

	// OverClaim is --over-claim: why the path of a story a Builder has claimed is
	// adjusted all the same (mw-gq6.1129). It is written as an OVER-CLAIM: comment
	// after the path. A closed story is refused whatever it says.
	OverClaim string

	// HeavyNet names the rigs that install dependencies ([heavy_net]): a path
	// that pins one to a home host draws a warning on Err naming host=auto
	// (mw-gq6.702). A nil HeavyNet warns of none.
	HeavyNet map[string]bool

	// Err is where the warning of a story with more test callers than its size
	// allows goes (mw-gq6.661). A nil Err prints none.
	Err io.Writer

	// Retired are the hosts the Governor retired (retired_hosts), and CloudBook
	// and CloudPlan say which cloud boxes exist: a path naming a retired host
	// or a box the book does not hold, written or copied from a sibling, is
	// written as host=auto instead, and what was done is printed (mw-gq6.1165;
	// before it the path was refused, mw-6ww.163.1). A nil CloudBook calls no
	// box gone.
	Retired   []string
	CloudBook CloudBook
	CloudPlan CloudPlan
}

// Run sets the path of story id. from names the sibling to copy the seven
// keys from (empty for none), and pairs are key=value settings that override
// what from gave. It reports the keys that were set.
func (s PathSet) Run(ctx context.Context, id, from string, pairs []string) (domain.Path, error) {
	if s.Tracker == nil {
		return domain.Path{}, fmt.Errorf("setting a path: there is no work tracker to set it in")
	}
	id, from = strings.TrimSpace(id), strings.TrimSpace(from)
	if id == "" {
		return domain.Path{}, fmt.Errorf("setting a path: which story?")
	}
	if from == "" && len(pairs) == 0 {
		return domain.Path{}, fmt.Errorf("setting a path: name a story to copy with --from, or keys to set as key=value (keys: %s)", settableKeys)
	}

	var path domain.Path
	if s.Hold != "" && strings.TrimSpace(s.Hold) == "" {
		return domain.Path{}, fmt.Errorf("setting a path: --hold needs a reason, as --hold \"waits for the demo\"")
	}
	named, err := parsePathPairs(pairs)
	if err != nil {
		return domain.Path{}, err
	}

	story, err := s.Tracker.ShowStory(ctx, id)
	if err != nil {
		return domain.Path{}, fmt.Errorf("reading the story %s: %w", id, err)
	}
	status := strings.ToLower(strings.TrimSpace(story.Status))
	if status == StatusClosed {
		return domain.Path{}, fmt.Errorf("the story %s is %s, and a path is adjusted only before a story starts: nothing was written", id, status)
	}
	overClaim := strings.Join(strings.Fields(s.OverClaim), " ")
	claimedBy := ""
	if status == StatusInProgress {
		claimedBy = fmt.Sprintf("%s (%s)", orNoAssignee(story.Assignee), status)
		if overClaim == "" {
			return domain.Path{}, fmt.Errorf("the story %s is %s, and a path is adjusted only before a story starts: nothing was written; to adjust it anyway, add %s \"<why>\"", id, status, BdOverClaimFlag)
		}
	}
	// A story set back to open or held under its session still has the session,
	// and its claim lease is what says so (mw-gq6.880).
	if claimedBy == "" && LiveClaim(s.now(), story) {
		claimedBy = fmt.Sprintf("%s (%s)", story.Assignee, status)
		if overClaim == "" {
			return domain.Path{}, fmt.Errorf("the story %s is %s but holds a live claim (%s until %s), so a session works it, and a path is adjusted only before a story starts: nothing was written; to adjust it anyway, add %s \"<why>\"",
				id, status, story.Assignee, story.LeaseExpires.UTC().Format(time.RFC3339), BdOverClaimFlag)
		}
	}

	if from != "" {
		sibling, err := s.Tracker.ShowStory(ctx, from)
		if err != nil {
			return domain.Path{}, fmt.Errorf("reading the story %s to copy the path from: %w", from, err)
		}
		path = sibling.Merged()
		// The files a story expects to change are its own: a sibling's are
		// never copied (mw-gq6.349).
		path.Files = ""
		// And so is its size (mw-1ljpo9.3).
		path.Size = ""
		// And its bump: one story raises the minor, not the ones copied from it.
		path.Bump = ""
		// And its callers_ok: the reason is for the sibling's files (mw-gq6.672).
		path.CallersOK = ""
		if path.Rig == "" {
			return domain.Path{}, fmt.Errorf("the story %s has no rig, so there is no path to copy from it: nothing was written", from)
		}
	}
	path = path.Overlay(named)
	// A host that cannot take a story is never written: host=auto is, and the
	// output says so (mw-gq6.1165).
	hostNote := ""
	if why := HostRefusedWhy(ctx, path.Host, s.Retired, s.CloudBook, s.CloudPlan); why != "" {
		hostNote = fmt.Sprintf("host=%s was not written: %s; host=auto was written instead, so a host that passes its gates takes the story", path.Host, why)
		path.Host, path.HostWhy = domain.HostAuto, ""
	}
	// A story whose path says why its host must stay keeps it: no other host,
	// host=auto among them, is written over it unless the same command writes
	// a new host_why (mw-gq6.1288).
	if err := hostWhyRefusal(id, story.Merged(), path, named); err != nil {
		return domain.Path{}, err
	}
	// A formula the vault has not installed would park the story: dispatch
	// leaves it unclaimed with one comment nobody reads (mw-gq6.497).
	if path.Formula != "" {
		installed, err := s.Tracker.Formulas(ctx)
		if err != nil {
			return domain.Path{}, fmt.Errorf("checking which formulas are installed: %w", err)
		}
		if err := path.ValidateFormula(installed); err != nil {
			return domain.Path{}, fmt.Errorf("%w: nothing was written", err)
		}
	}
	// Only a files key being written is judged: one the story already holds is
	// not this command's to refuse a model= or rig= over.
	if named.Files != "" {
		if err := crowdedFiles(s.Rigs, s.RigFiles, story.Merged().Overlay(path)); err != nil {
			return domain.Path{}, fmt.Errorf("%w: nothing was written", err)
		}
	}

	// A branch the rig's origin does not have would fail every dispatch cutting
	// its worktree, so it is refused here (mw-gq6.1416); one that cannot be
	// judged is a warning, said for a branch the command names.
	judged := story.Merged().Overlay(path)
	refused, branchWarn := branchOnOrigin(s.Rigs, s.RigFiles, judged)
	if refused != nil {
		return domain.Path{}, fmt.Errorf("%w: nothing was written", refused)
	}

	// --after and --hold are judged, like the rest, before anything is written.
	after, err := s.afterIDs(ctx, id)
	if err != nil {
		return domain.Path{}, err
	}
	holding := strings.TrimSpace(s.Hold) != ""
	if holding && !story.Held() {
		return domain.Path{}, fmt.Errorf("the story %s is %s, and --hold keeps a held story held: nothing was written", id, strings.ToLower(strings.TrimSpace(story.Status)))
	}
	releasing, err := s.releasesOnPath(ctx, story.Merged().Overlay(path).Rig)
	if err != nil {
		return domain.Path{}, fmt.Errorf("%w: nothing was written", err)
	}
	// The dependencies go in before the path, and the release is last, so a story
	// is never open and not yet waiting on what it waits on (mw-b00z.9).
	for _, blocker := range after {
		if err := s.Tracker.AddBlocker(ctx, id, blocker); err != nil {
			return domain.Path{}, fmt.Errorf("making %s wait on %s: %w", id, blocker, err)
		}
	}

	fields := path.Metadata()
	if err := s.Tracker.SetStoryMetadata(ctx, id, fields); err != nil {
		return domain.Path{}, fmt.Errorf("setting the path of %s: %w", id, err)
	}
	if claimedBy != "" {
		if err := s.Tracker.CommentOnStory(ctx, id, fmt.Sprintf("%s path set over the claim of %s: %s", OverClaimPrefix, claimedBy, overClaim)); err != nil {
			return path, fmt.Errorf("the path of %s was set, but the reason could not be written on it: %w", id, err)
		}
	}
	if s.Out != nil {
		// The echo is the path as PathShow reads it after the write: the keys
		// the story already held, and its epic's defaults, under the ones just
		// set (mw-gq6.371).
		now := story.Merged().Overlay(path)
		fmt.Fprintf(s.Out, "Set the path of %s:\n%s", id, pathLines(now))
		if hostNote != "" {
			fmt.Fprintln(s.Out, hostNote)
		}
		if governorsDemo(story.Labels, story.Story.Overrides.Overlay(path).Files) && !hasHitlLabel(story.Labels) {
			fmt.Fprintln(s.Out, demoNoHitlLine(id))
		}
	}
	if branchWarn != "" && named.Branch != "" && s.Err != nil {
		fmt.Fprintf(s.Err, "WARN: %s: %s\n", id, branchWarn)
	}
	// What dispatch would refuse the story for, said now and not by a Blocked
	// mail after it is un-held (mw-oi2rkcb): the story as the write leaves it.
	if s.Out != nil {
		written := story
		written.Story.Overrides = story.Story.Overrides.Overlay(path)
		warnBeforeDispatch(ctx, s.Out, s.Rules, written)
	}
	// The test files that call the story's files are changed with them, so the
	// size is judged by them too (mw-gq6.661). Only a files, size or callers_ok key
	// being written is judged; a warning, never a refusal.
	if named.Files != "" || named.Size != "" || named.CallersOK != "" {
		if callers, ok := testCallersOf(s.Rigs, s.RigFiles, id, story.Merged().Overlay(path)); ok {
			callerNotes(s.Out, s.Err, callers)
		}
		// Likewise the files the key names: a story wide in itself runs out of
		// turns whatever its tests are (mw-gq6.839).
		if named, ok := filesNamedOf(s.Rigs, s.RigFiles, id, story.Merged().Overlay(path)); ok {
			filesNamedNote(s.Err, named)
		}
	}
	// An L or XL story whose criteria ask for more than one kind of change is more
	// than one session finishes (mw-gq6.852); judged when the size is written.
	if named.Size != "" && s.Err != nil {
		if line := domain.MechanismWarning(id, story.Merged().Overlay(path).Size, story.Acceptance); line != "" {
			fmt.Fprintln(s.Err, line)
		}
	}
	// A criterion only the Mayor's after-landing check can run is said now and not
	// by a Builder handing the story back (mw-gq6.1271).
	if s.Err != nil {
		now := story.Merged().Overlay(path)
		if line := s.notRunnableWarning(ctx, id, now.Rig, story.Acceptance); line != "" {
			fmt.Fprintln(s.Err, line)
		}
	}
	// Only a path written with a host or a rig (--from writes both) is judged: a
	// model= set on a story already pinned is not this warning's to repeat.
	if s.Err != nil && (from != "" || named.Host != "" || named.Rig != "") {
		if now := story.Merged().Overlay(path); s.HeavyNet[now.Rig] && slices.Contains(domain.HomeHosts, now.Host) {
			fmt.Fprintf(s.Err, "WARN: host=%s on %s, a rig that installs dependencies: while that host's network is metered "+
				"mw dispatch starts none of its stories, and no other host may take it. Set host=auto: mw path set %s host=auto\n",
				now.Host, now.Rig, id)
		}
	}
	// --from never copies files, so on a rig that expects them the seat that
	// forgot files= is told now and not by a later mw path show (mw-gq6.1130).
	if from != "" && named.Files == "" && s.Err != nil {
		now := story.Merged().Overlay(path)
		if strings.TrimSpace(now.Files) == "" && !claimsNoFiles(now) && strings.TrimSpace(now.Rig) != "" && s.Rules != nil {
			// A rig file that cannot be read says nothing here: mw path show warns later.
			if required, err := s.Rules.EpicRequirements(ctx, now.Rig); err == nil && required.FilesExpected {
				fmt.Fprintln(s.Err, FilesNotCopiedLine(from))
			}
		}
	}
	// A story filed by hand into an epic is made a need of the epic's last
	// story, as the Tester's is (mw-gq6.498).
	line, notes := KeepDemoLast(ctx, s.Tracker, s.Rules, story.Merged().Overlay(path).Rig, story.EpicID, id)
	if s.Out != nil {
		if line != "" {
			fmt.Fprintln(s.Out, line)
		}
		for _, note := range notes {
			fmt.Fprintln(s.Out, note)
		}
	}
	// A held story on a rig that releases on path set, given neither --after nor
	// --hold, stays held by design; say so, so the seat does not think it released it.
	if !releasing && !holding && s.After == nil && story.Held() && s.Err != nil {
		if onPath, err := s.rigReleasesOnPath(ctx, story.Merged().Overlay(path).Rig); err == nil && onPath {
			fmt.Fprintln(s.Err, StaysHeldNoAfterLine)
		}
	}
	if holding {
		if err := s.holdOnPathSet(ctx, id); err != nil {
			return path, err
		}
	} else if releasing {
		if err := s.releaseOnPathSet(ctx, id, story, path); err != nil {
			return path, err
		}
	}
	return path, nil
}

// hostWhyRefusal is the refusal of a path whose host is another than the one a
// story must keep: the story's host_why, its own or its epic's (now), says why
// it stays, and the path would write a different host over it (mw-gq6.1288).
// The Mayor pathed the Septuagint model stories host=auto three times, and
// mw took each, though only the Laptop has the Morpheus cache. A host=auto story
// is pinned to nothing, so it is never refused; a command that writes a new
// host_why too moves the story on purpose.
func hostWhyRefusal(id string, now, path, named domain.Path) error {
	why := strings.TrimSpace(now.HostWhy)
	if why == "" || now.Host == "" || now.Host == domain.HostAuto {
		return nil
	}
	if path.Host == "" || path.Host == now.Host || strings.TrimSpace(named.HostWhy) != "" {
		return nil
	}
	return fmt.Errorf("the story %s must stay on host=%s: its host_why says %q, and host=%s would move it: nothing was written; "+
		"to move it on purpose, write the new host with a new reason in one command: mw path set %s host=%s host_why=\"<why>\"; "+
		"to clear the reason, remove it where it is kept (the story's, or its epic's): bd update <id> --unset-metadata host_why",
		id, now.Host, why, path.Host, id, path.Host)
}

// FilesNotCopiedLine is the line on stderr of a path set --from on a rig that
// expects files, given no files=: the sibling's files are never copied.
func FilesNotCopiedLine(from string) string {
	return fmt.Sprintf("files not copied from %s: pass files=", from)
}

// StaysHeldNoAfterLine is the line on stderr of a path set on a release_on_path
// rig that gave neither --after nor --hold: the held story stays held.
const StaysHeldNoAfterLine = `held: no --after given; pass --after none to release or --hold "<why>" to keep it held`

// MostFilesInADirectory is the most files a directory a files key names, or a
// glob in it, may hold (mw-gq6.442, mw-gq6.633). Dispatch holds a story behind
// every running story on its rig that touches anything under a directory it
// names, so a key naming src/ kept a lampas bug unstarted for three days, and
// tests/e2e/*.ts, over more files than that, held a P1 for twenty-five hours;
// twenty is a module's worth, a few stories' files, and well short of a whole
// source tree.
const MostFilesInADirectory = 20

// mostGlobCount is where a glob's files stop being counted for a refusal to
// say how many there are.
const mostGlobCount = 1000

// crowdedFiles refuses a path whose files key names a directory of more than
// MostFilesInADirectory files in its rig's checkout here, or one written as a
// whole directory ("src/", "src/**") when the rig is not checked out here to
// count it in; and likewise a glob that matches more than that many files, or,
// where the rig is not checked out, one whose last segment is a wildcard
// ("tests/e2e/*.ts"). A directory the checkout does not have yet is a new one,
// and is taken: nothing running touches it. A nil RigFiles counts nothing, so a
// whole directory is refused and a plain name is taken, as for a rig not here.
func crowdedFiles(rigs map[string]string, files RigFiles, path domain.Path) error {
	rig := strings.TrimSpace(path.Rig)
	rigDir, here := rigs[rig]
	here = here && rigDir != "" && files != nil
	for _, named := range domain.Directories(path.Files) {
		if !here {
			if named.Whole {
				return crowded(named, fmt.Sprintf("a whole directory, and %s is not checked out here to count what it holds", orRig(rig)))
			}
			continue
		}
		if n, isDir := files.FilesIn(rigDir, named.Dir, MostFilesInADirectory); isDir && n > MostFilesInADirectory {
			return crowded(named, fmt.Sprintf("a directory of more than %d files in %s", MostFilesInADirectory, rig))
		}
	}
	for _, glob := range domain.Globs(path.Files) {
		if !here {
			if glob.BareWildcard {
				return crowdedGlob(glob, fmt.Sprintf("a glob whose last part is a wildcard, and %s is not checked out here to count what it matches", orRig(rig)))
			}
			continue
		}
		if n := files.Matching(rigDir, glob.Pattern, mostGlobCount); n > MostFilesInADirectory {
			count := fmt.Sprintf("%d", n)
			if n > mostGlobCount {
				count = fmt.Sprintf("more than %d", mostGlobCount)
			}
			return crowdedGlob(glob, fmt.Sprintf("a glob that matches %s files in %s, more than the %d a files key may", count, rig, MostFilesInADirectory))
		}
	}
	return nil
}

// crowded is the refusal of a files key naming a directory, and how to name
// files instead.
func crowded(named domain.NamedDirectory, what string) error {
	return fmt.Errorf("the files key names %s, %s, and dispatch holds a story behind every running story "+
		"that touches anything in it: name the files the story will change, comma-separated, "+
		"as files=src/Talk.tsx,src/chat/Chat.tsx, or a directory of at most %d files", named.Written, what, MostFilesInADirectory)
}

// crowdedGlob is the refusal of a files key naming a glob over too many files.
func crowdedGlob(glob domain.NamedGlob, what string) error {
	return fmt.Errorf("the files key names %s, %s, and dispatch holds a story behind every running story "+
		"that touches any file it matches: name the files instead, the ones the story will change, comma-separated, "+
		"as files=src/Talk.tsx,src/chat/Chat.tsx, or a glob of at most %d files", glob.Written, what, MostFilesInADirectory)
}

// branchOnOrigin judges the branch key of a path against the origin of its rig's
// checkout on this host: a refusal when the branch is not one of origin's, naming
// the branch its HEAD is (mw-gq6.1416), and otherwise, when the checkout cannot be
// read (the rig is not checked out here, or git cannot ask), the words of a
// warning, "" for none. A path with no branch or no rig is not judged.
func branchOnOrigin(rigs map[string]string, files RigFiles, path domain.Path) (error, string) {
	branch, rig := strings.TrimSpace(path.Branch), strings.TrimSpace(path.Rig)
	if branch == "" || rig == "" || files == nil {
		return nil, ""
	}
	rigDir, here := rigs[rig]
	if !here || rigDir == "" {
		return nil, fmt.Sprintf("branch=%s was not checked against the origin of %s: %s is not checked out on this host", branch, rig, rig)
	}
	found, head, err := files.BranchOnOrigin(rigDir, branch)
	if err != nil {
		return nil, fmt.Sprintf("branch=%s was not checked against the origin of %s: %v", branch, rig, err)
	}
	if found {
		return nil, ""
	}
	if head == "" {
		return fmt.Errorf("branch=%s is not a branch of the origin of %s, and dispatch could not cut a worktree from it: name a branch origin has", branch, rig), ""
	}
	return fmt.Errorf("branch=%s is not a branch of the origin of %s, and dispatch could not cut a worktree from it: its HEAD branch is %s, so set branch=%s",
		branch, rig, head, head), ""
}

// orRig is the rig as a refusal names it, "its rig" when the path has none.
func orRig(rig string) string {
	if rig == "" {
		return "its rig"
	}
	return rig
}

// settableKeys are the keys mw path set takes, as a refusal names them: the
// eleven path keys and callers_ok (mw-gq6.672).
var settableKeys = strings.Join(append(append([]string{}, domain.Fields...), domain.CallersOKField), ", ")

// parsePathPairs reads key=value settings into a path, refusing a key that is
// not one of the eleven or callers_ok and a value left empty.
func parsePathPairs(pairs []string) (domain.Path, error) {
	var path domain.Path
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || value == "" {
			return domain.Path{}, fmt.Errorf("%q is not key=value with a value (keys: %s)", pair, settableKeys)
		}
		if err := path.Set(key, value); err != nil {
			return domain.Path{}, fmt.Errorf("%q is not a path key: the keys are %s", key, settableKeys)
		}
	}
	if path.Size != "" && !domain.KnownSize(path.Size) {
		return domain.Path{}, fmt.Errorf("%q is not a size: the sizes are XS, S, M, L, XL: nothing was written", path.Size)
	}
	if path.Bump != "" && path.Bump != domain.BumpMinor {
		return domain.Path{}, fmt.Errorf("%q is not a bump: the only one is %s: nothing was written", path.Bump, domain.BumpMinor)
	}
	return path, nil
}

// pathLines is a path as a key=value line for each of the eleven keys, a key it
// does not set shown as PathUnset.
func pathLines(path domain.Path) string {
	var b strings.Builder
	for _, key := range domain.Fields {
		value, _ := path.Field(key)
		if value == "" {
			value = PathUnset
		}
		fmt.Fprintf(&b, "%s=%s\n", key, value)
	}
	// callers_ok is no path key, so it is shown only for a story that carries one.
	if path.CallersOK != "" {
		fmt.Fprintf(&b, "%s=%s\n", domain.CallersOKField, path.CallersOK)
	}
	return b.String()
}

// PathShow prints the eleven path keys of a story, as the story is worked by
// them: its own settings over its epic's defaults.
type PathShow struct {
	Tracker WorkTracker

	// Rules is where the story's rig's file is read from, to warn of a story
	// that has no files key on a rig that expects one. A nil Rules warns of none.
	Rules EpicRules

	// Out is where the keys are printed. A nil Out prints nothing.
	Out io.Writer
}

// Run prints the path of story id and reports it.
func (s PathShow) Run(ctx context.Context, id string) (domain.Path, error) {
	if s.Tracker == nil {
		return domain.Path{}, fmt.Errorf("showing a path: there is no work tracker to read it from")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.Path{}, fmt.Errorf("showing a path: which story?")
	}
	story, err := s.Tracker.ShowStory(ctx, id)
	if err != nil {
		return domain.Path{}, fmt.Errorf("reading the story %s: %w", id, err)
	}
	path := story.Merged()
	if s.Out != nil {
		fmt.Fprint(s.Out, pathLines(path))
		warnBeforeDispatch(ctx, s.Out, s.Rules, story)
	}
	return path, nil
}

// now is the clock this path set reads.
func (s PathSet) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}

// orNoAssignee names a claim's holder, or says there is none named.
func orNoAssignee(assignee string) string {
	if assignee == "" {
		return "a session with no assignee named"
	}
	return assignee
}

var (
	// liveServedURL is an address on a served rig's domain; a rig's own site_url
	// is read from its file and added to it.
	liveServedURL = regexp.MustCompile(`(?i)https?://[^\s"'<>)]*\.allmymind\.org\b`)
	// afterLandingWords say a criterion is judged only once the change is live.
	afterLandingWords = regexp.MustCompile(`(?i)\b(after the fix lands|after landing|once it is live)\b`)
	// aTestOrFixture marks a criterion that names a URL or those words as part of
	// a test or a fixture, which a Builder can run before landing.
	aTestOrFixture = regexp.MustCompile(`(?i)\b(tests?|fixtures?|testdata)\b|_test\.go|features/`)
)

// notRunnableWarning is the one warning, empty for none, of a story whose
// acceptance has a criterion that names a live URL of a served rig (an
// allmymind.org address or the rig's site_url) or says it is judged after the fix
// lands, after landing or once it is live: a Builder cannot run that before
// landing, and it is the Mayor's after-landing check (Verify strings, mw verify).
// A criterion that names a test or a fixture is not warned of. A warning only.
func (s PathSet) notRunnableWarning(ctx context.Context, id, rig, acceptance string) string {
	site := ""
	if s.Rules != nil && strings.TrimSpace(rig) != "" {
		if requirements, err := s.Rules.EpicRequirements(ctx, rig); err == nil {
			site = strings.ToLower(strings.TrimRight(strings.TrimSpace(requirements.SiteURL), "/"))
		}
	}
	var numbers []string
	for _, criterion := range domain.AcceptanceCriteria(acceptance) {
		if aTestOrFixture.MatchString(criterion.Text) {
			continue
		}
		if liveServedURL.MatchString(criterion.Text) || afterLandingWords.MatchString(criterion.Text) ||
			(site != "" && strings.Contains(strings.ToLower(criterion.Text), site)) {
			numbers = append(numbers, fmt.Sprint(criterion.Number))
		}
	}
	if len(numbers) == 0 {
		return ""
	}
	return fmt.Sprintf("WARN: %s: criterion %s of its acceptance names a live URL of a served rig or says it is judged after the fix lands, "+
		"which no Builder can run before landing; move it out of the acceptance and into the Mayor's after-landing check "+
		"(a Verify strings: line, or mw verify)", id, strings.Join(numbers, ", "))
}

package application

import (
	"context"
	"fmt"
	"io"
	"strings"

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

	// Rigs are the rigs checked out on this host, by name, and RigFiles counts
	// the files of a directory in one: a files key that names a directory of
	// more than MostFilesInADirectory files is refused (mw-gq6.442). A rig not
	// checked out here cannot be counted in, so a whole directory named for it
	// ("src/") is refused there too.
	Rigs     map[string]string
	RigFiles RigFiles

	// Rules is where the story's rig's file is read from, to keep the epic's
	// last story last when the story is filed into it (KeepDemoLast). A nil
	// Rules keeps nothing.
	Rules EpicRules

	// Out is where the path that was set is printed. A nil Out prints nothing.
	Out io.Writer
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
		return domain.Path{}, fmt.Errorf("setting a path: name a story to copy with --from, or keys to set as key=value (keys: %s)", strings.Join(domain.Fields, ", "))
	}

	var path domain.Path
	named, err := parsePathPairs(pairs)
	if err != nil {
		return domain.Path{}, err
	}

	story, err := s.Tracker.ShowStory(ctx, id)
	if err != nil {
		return domain.Path{}, fmt.Errorf("reading the story %s: %w", id, err)
	}
	if status := strings.ToLower(strings.TrimSpace(story.Status)); status == StatusInProgress || status == StatusClosed {
		return domain.Path{}, fmt.Errorf("the story %s is %s, and a path is adjusted only before a story starts: nothing was written", id, status)
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
		if path.Rig == "" {
			return domain.Path{}, fmt.Errorf("the story %s has no rig, so there is no path to copy from it: nothing was written", from)
		}
	}
	path = path.Overlay(named)
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

	fields := path.Metadata()
	if err := s.Tracker.SetStoryMetadata(ctx, id, fields); err != nil {
		return domain.Path{}, fmt.Errorf("setting the path of %s: %w", id, err)
	}
	if s.Out != nil {
		// The echo is the path as PathShow reads it after the write: the keys
		// the story already held, and its epic's defaults, under the ones just
		// set (mw-gq6.371).
		now := story.Merged().Overlay(path)
		fmt.Fprintf(s.Out, "Set the path of %s:\n%s", id, pathLines(now))
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
	return path, nil
}

// MostFilesInADirectory is the most files a directory a files key names may
// hold (mw-gq6.442). Dispatch holds a story behind every running story on its
// rig that touches anything under a directory it names, so a key naming src/
// kept a lampas bug unstarted for three days; twenty is a module's worth, a few
// stories' files, and well short of a whole source tree.
const MostFilesInADirectory = 20

// crowdedFiles refuses a path whose files key names a directory of more than
// MostFilesInADirectory files in its rig's checkout here, or one written as a
// whole directory ("src/", "src/**") when the rig is not checked out here to
// count it in. A directory the checkout does not have yet is a new one, and is
// taken: nothing running touches it. A nil RigFiles counts nothing, so a whole
// directory is refused and a plain name is taken, as for a rig not here.
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
	return nil
}

// crowded is the refusal of a files key naming a directory, and how to name
// files instead.
func crowded(named domain.NamedDirectory, what string) error {
	return fmt.Errorf("the files key names %s, %s, and dispatch holds a story behind every running story "+
		"that touches anything in it: name the files the story will change, comma-separated, "+
		"as files=src/Talk.tsx,src/chat/*.tsx, or a directory of at most %d files", named.Written, what, MostFilesInADirectory)
}

// orRig is the rig as a refusal names it, "its rig" when the path has none.
func orRig(rig string) string {
	if rig == "" {
		return "its rig"
	}
	return rig
}

// parsePathPairs reads key=value settings into a path, refusing a key that is
// not one of the ten and a value left empty.
func parsePathPairs(pairs []string) (domain.Path, error) {
	var path domain.Path
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || value == "" {
			return domain.Path{}, fmt.Errorf("%q is not key=value with a value (keys: %s)", pair, strings.Join(domain.Fields, ", "))
		}
		if err := path.Set(key, value); err != nil {
			return domain.Path{}, fmt.Errorf("%q is not a path key: the keys are %s", key, strings.Join(domain.Fields, ", "))
		}
	}
	if path.Size != "" && !domain.KnownSize(path.Size) {
		return domain.Path{}, fmt.Errorf("%q is not a size: the sizes are XS, S, M, L, XL: nothing was written", path.Size)
	}
	return path, nil
}

// pathLines is a path as a key=value line for each of the ten keys, a key it
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
	return b.String()
}

// PathShow prints the ten path keys of a story, as the story is worked by
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
		warnNoFilesKey(ctx, s.Out, s.Rules, story)
	}
	return path, nil
}

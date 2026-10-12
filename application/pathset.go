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
		if path.Rig == "" {
			return domain.Path{}, fmt.Errorf("the story %s has no rig, so there is no path to copy from it: nothing was written", from)
		}
	}
	path = path.Overlay(named)

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
	return path, nil
}

// parsePathPairs reads key=value settings into a path, refusing a key that is
// not one of the eight and a value left empty.
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
	return path, nil
}

// pathLines is a path as a key=value line for each of the eight keys, a key it
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

// PathShow prints the eight path keys of a story, as the story is worked by
// them: its own settings over its epic's defaults.
type PathShow struct {
	Tracker WorkTracker

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
	}
	return path, nil
}

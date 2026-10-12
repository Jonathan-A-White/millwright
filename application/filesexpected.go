package application

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
)

// NoFilesKey is an open or held story of a rig that expects a files key (the
// rig's file in the vault says files_expected = true) whose path has none, so
// that the overlap guard of mw dispatch (mw-gq6.349) does not look at it.
type NoFilesKey struct {
	ID  string
	Rig string
}

// Line is the warning as mw status, mw release and mw path show say it.
func (n NoFilesKey) Line() string {
	return fmt.Sprintf("no files key: %s (%s expects one; mw path set %s files=...)", n.ID, n.Rig, n.ID)
}

// filesExpected finds the stories that lack a files key on a rig that expects
// one, reading each rig's file once. A nil rules expects nothing of any rig.
type filesExpected struct {
	rules   EpicRules
	expects map[string]bool
}

// lacking is the stories among details that are open or held, worked by a
// session, on a rig that expects files, and name none; each once, in order.
func (f *filesExpected) lacking(ctx context.Context, details ...StoryDetail) ([]NoFilesKey, error) {
	if f.rules == nil {
		return nil, nil
	}
	var found []NoFilesKey
	seen := map[string]bool{}
	for _, d := range details {
		id := d.Story.ID
		if seen[id] || d.IsEpic || d.Hitl() || !slices.Contains([]string{StateOpen, StateHeld}, StateOf(d.Status)) {
			continue
		}
		seen[id] = true
		path := d.Merged()
		rig := strings.TrimSpace(path.Rig)
		if rig == "" || strings.TrimSpace(path.Files) != "" {
			continue
		}
		expects, read := f.expects[rig]
		if !read {
			required, err := f.rules.EpicRequirements(ctx, rig)
			if err != nil {
				return nil, fmt.Errorf("reading whether the rig %s expects a files key: %w", rig, err)
			}
			if f.expects == nil {
				f.expects = map[string]bool{}
			}
			expects = required.FilesExpected
			f.expects[rig] = expects
		}
		if expects {
			found = append(found, NoFilesKey{ID: id, Rig: rig})
		}
	}
	return found, nil
}

// warnNoFilesKey prints the warning for each story of details that lacks a
// files key where the rig expects one. A warning that cannot be worked out is
// said as one line, and costs the command nothing else.
func warnNoFilesKey(ctx context.Context, out io.Writer, rules EpicRules, details ...StoryDetail) {
	if out == nil || rules == nil {
		return
	}
	lacking, err := (&filesExpected{rules: rules}).lacking(ctx, details...)
	if err != nil {
		fmt.Fprintf(out, "mw: could not tell whether a files key is missing: %v\n", err)
		return
	}
	for _, n := range lacking {
		fmt.Fprintln(out, n.Line())
	}
}

// noFilesKey reads the stories of the live epics of rigs that expect files,
// held ones among them (the tracker lists a held story only under its epic),
// and with known, the stories the report already holds, finds the ones that
// have none. Nothing is written.
func (s Status) noFilesKey(ctx context.Context, known []StoryDetail) ([]NoFilesKey, error) {
	if s.Rules == nil {
		return nil, nil
	}
	finder := &filesExpected{rules: s.Rules}
	ids, err := s.Tracker.LiveEpics(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing the live epics: %w", err)
	}
	var shown []StoryDetail
	if len(ids) > 0 {
		epics, err := s.Tracker.ShowBeads(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("reading the live epics: %w", err)
		}
		var wanted []string
		for _, epic := range epics {
			rig := strings.TrimSpace(epic.Merged().Rig)
			if rig == "" || slices.Contains(epic.Labels, "wayfinder:map") {
				continue
			}
			required, err := s.Rules.EpicRequirements(ctx, rig)
			if err != nil {
				return nil, fmt.Errorf("reading whether the rig %s expects a files key: %w", rig, err)
			}
			if required.FilesExpected {
				wanted = append(wanted, epic.Story.ID)
			}
		}
		if len(wanted) > 0 {
			details, err := s.Tracker.ShowEpics(ctx, wanted)
			if err != nil {
				return nil, fmt.Errorf("reading the stories of %s: %w", strings.Join(wanted, ", "), err)
			}
			for _, epic := range details {
				shown = append(shown, epic.Stories...)
			}
		}
	}
	return finder.lacking(ctx, append(known, shown...)...)
}

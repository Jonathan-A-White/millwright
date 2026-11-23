package domain

import "path"

// RegeneratedFiles is one entry of a rig's regenerated_files: files that a
// program writes whole, so that two stories which each regenerate them conflict
// however their changes are merged. Paths are the files, as paths relative to
// the rig's root or path.Match globs; Run is the shell command, run in the
// landing worktree, that writes them again from what the merge holds.
type RegeneratedFiles struct {
	Paths []string
	Run   string
}

// Matches reports whether file is one of the entry's paths or globs.
func (r RegeneratedFiles) Matches(file string) bool {
	for _, pattern := range r.Paths {
		if pattern == file {
			return true
		}
		if ok, err := path.Match(pattern, file); err == nil && ok {
			return true
		}
	}
	return false
}

// MergeRules are what a landing's merge is told of the rig's files: the ones
// whose conflicts are settled by the trim-safe merge (union_merge_files) and
// the generated ones whose conflicts are settled by running their generator
// (regenerated_files). A file both list is regenerated.
type MergeRules struct {
	Union       []string
	Regenerated []RegeneratedFiles
}

// None reports whether the rig lists no file to settle a conflict in.
func (m MergeRules) None() bool { return len(m.Union) == 0 && len(m.Regenerated) == 0 }

// MergeRules are the rig's file's words for a landing's merge.
func (r EpicRequirements) MergeRules() MergeRules {
	return MergeRules{Union: r.UnionMergeFiles, Regenerated: r.RegeneratedFiles}
}

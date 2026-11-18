package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// What mw host sweep keeps, and for how long. The two ages are config keys of
// the root table (host_sweep_runs_days, host_sweep_aside_days) and PROVISIONAL,
// for the Governor to confirm (mw-gq6.1427); the page that says each is
// docs/host-sweep.md, and changing a bound is a change to that page too.
const (
	// HostSweepDay is a day, the unit of the sweep's two ages.
	HostSweepDay = 24 * time.Hour
	// HostSweepRunsAge is how old a run's directory under the vault's runs/ is
	// let grow, by the newest file in it.
	HostSweepRunsAge = 14 * 24 * time.Hour
	// HostSweepAsideAge is how old a beads-*-aside-* directory is let grow.
	HostSweepAsideAge = 7 * 24 * time.Hour
	// HostSweepClosedGrace is how long a closed story's worktree is left after
	// the tracker says it closed: the close-out takes it away itself.
	HostSweepClosedGrace = time.Hour
	// HostSweepLogPrefix starts the words of a tick log line mw host sweep
	// writes, one per act. It starts with TidyLogPrefix, so that the tick's
	// counts and resume rules read past it as they do mw tidy's lines.
	HostSweepLogPrefix = TidyLogPrefix + "host: "
)

// HostSweepDir is a directory the sweep may take away.
type HostSweepDir struct {
	Path string
	// Modified is when the directory, or anything in it, was last written.
	Modified time.Time
	Bytes    int64
	// Live, when it is not empty, says what keeps the directory: a process
	// running in it, a change not committed, a commit not on main.
	Live string
	// Tracked is true when the vault's git tracks a file in the directory (the
	// vault's runs/ is tracked): a sweep on one host would show as deletions in
	// the git the vault's sync commits and the other hosts pull, so the sweep
	// leaves it to the sync.
	Tracked bool
}

// HostSweepTree is a story's worktree and branch in a rig, as the host holds
// them. Dir is empty for a branch whose worktree is already gone, and Branch
// for a worktree whose branch is.
type HostSweepTree struct {
	Story  string
	Rig    string
	RigDir string
	Dir    string
	Branch string
	Bytes  int64
	// Uncommitted is true when the worktree has a change git would lose, or
	// git could not read it. Busy says which process is running in it. Unsaved
	// says why the branch holds a commit that is neither on main nor pushed.
	Uncommitted bool
	Busy        string
	Unsaved     string
	// Caches are the node_modules and Playwright browser directories in the
	// worktree: what a build makes again.
	Caches []HostSweepDir
}

// HostSweepFiles is the host's disk as the sweep reads and clears it: git's
// worktrees and branches, the landing directories, the vault's runs, and the
// directories a home move set aside. The real adapter is
// infrastructure/hostsweep.
type HostSweepFiles interface {
	// Trees lists every story worktree and story branch of every rig on this
	// host, by story, with what keeps each.
	Trees(ctx context.Context) ([]HostSweepTree, error)
	// Landings lists the .mw-landing-* directories, whatever their age.
	Landings(ctx context.Context) ([]HostSweepDir, error)
	// Runs lists each directory of the vault's runs/.
	Runs(ctx context.Context) ([]HostSweepDir, error)
	// Asides lists the beads-*-aside-* directories of the home directory.
	Asides(ctx context.Context) ([]HostSweepDir, error)
	// RemoveTree takes the worktree and the branch away, as git would, and
	// refuses what it would lose: nothing uncommitted is thrown away.
	RemoveTree(ctx context.Context, tree HostSweepTree) error
	// RemoveDir deletes the directory, whole.
	RemoveDir(ctx context.Context, path string) error
}

// HostSweepStories is the part of the tracker the sweep reads a story's state
// through: the work tracker's ShowBeads. A story it does not find is not listed.
type HostSweepStories interface {
	ShowBeads(ctx context.Context, ids []string) ([]StoryDetail, error)
}

// HostSweepAct is one thing mw host sweep did, or in a dry run would do, or
// kept (Kept is the reason it was not done).
type HostSweepAct struct {
	// Kind is worktree, branch, cache, landing, run or aside.
	Kind  string
	Path  string
	Bytes int64
	// Why is the reason it goes, or, for a kept one, the reason it stays.
	Why  string
	Kept bool
}

// String is the act as a line of the log and the report.
func (a HostSweepAct) String() string {
	if a.Kept {
		return fmt.Sprintf("kept: %s (%s %s)", a.Why, a.Kind, a.Path)
	}
	return fmt.Sprintf("%s %s: %s, %s", a.Kind, a.Path, humanBytes(a.Bytes), a.Why)
}

// HostSweepReport is what one pass did.
type HostSweepReport struct {
	DryRun bool
	// Acts are what was removed, or would be; Kept what was found and left,
	// with why; Notes what could not be read or done.
	Acts  []HostSweepAct
	Kept  []HostSweepAct
	Notes []string
}

// Bytes is the size of everything the pass removed, or would.
func (r HostSweepReport) Bytes() int64 {
	var n int64
	for _, a := range r.Acts {
		n += a.Bytes
	}
	return n
}

// Summary is the report in a phrase.
func (r HostSweepReport) Summary() string {
	verb := "host sweep removed "
	if r.DryRun {
		verb = "host sweep would remove "
	}
	return verb + counted(len(r.Acts), "item", "items") + " (" + humanBytes(r.Bytes()) + ")"
}

// String is the report as a person reads it.
func (r HostSweepReport) String() string {
	if len(r.Acts) == 0 && len(r.Kept) == 0 && len(r.Notes) == 0 {
		return "host sweep: nothing on this host is dead\n"
	}
	var b strings.Builder
	verb := "removed"
	if r.DryRun {
		verb = "would remove"
	}
	fmt.Fprintf(&b, "host sweep: %s %s, %s\n", verb, counted(len(r.Acts), "item", "items"), humanBytes(r.Bytes()))
	for _, a := range r.Acts {
		fmt.Fprintf(&b, "  %s %s\n", verb, a)
	}
	for _, k := range r.Kept {
		fmt.Fprintf(&b, "  %s\n", k)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "  ! %s\n", n)
	}
	return b.String()
}

// HostSweep frees what no live story can need, on this host: the worktree and
// branch of a story that is closed, the caches of one whose commits are all
// that is left, landing directories a close-out could not take away, old runs,
// and old directories a home move set aside. It never touches a worktree with
// an uncommitted change, a branch with a commit that is neither on main nor
// pushed, a story that is not closed, or what a process is running in. A look or
// an act that fails is a note and the pass goes on; Run returns an error when any
// did. docs/host-sweep.md is the table.
type HostSweep struct {
	Files   HostSweepFiles
	Stories HostSweepStories

	// RunsAge and AsideAge are the ages; zero reads HostSweepRunsAge and
	// HostSweepAsideAge.
	RunsAge  time.Duration
	AsideAge time.Duration

	// Log is the tick log each act adds a line to; nil writes none.
	Log TickLog

	DryRun bool
	// Now is the clock ages are measured against; nil is time.Now.
	Now func() time.Time
	// Out is where the report is printed; nil prints nothing.
	Out io.Writer
}

// Run makes one pass.
func (s HostSweep) Run(ctx context.Context) (HostSweepReport, error) {
	report := HostSweepReport{DryRun: s.DryRun}
	if s.Files == nil || s.Stories == nil {
		return report, fmt.Errorf("host sweep: there is nothing wired to sweep")
	}
	now := s.now()
	var faults []error
	fault := func(err error) {
		faults = append(faults, err)
		report.Notes = append(report.Notes, oneLine(err.Error()))
	}
	// remove does one act, or in a dry run only says it.
	remove := func(act HostSweepAct, do func() error) {
		if !s.DryRun {
			if err := do(); err != nil {
				fault(fmt.Errorf("removing %s: %w", act.Path, err))
				return
			}
		}
		report.Acts = append(report.Acts, act)
		s.log(ctx, now, act, &report)
	}
	keep := func(kind, path, why string) {
		report.Kept = append(report.Kept, HostSweepAct{Kind: kind, Path: path, Why: why, Kept: true})
	}

	trees, err := s.Files.Trees(ctx)
	if err != nil {
		fault(fmt.Errorf("reading the worktrees: %w", err))
	}
	details := s.storyStates(ctx, trees, fault)
	liveStories := map[string]bool{}
	for _, tree := range trees {
		detail, found := details[tree.Story]
		where := tree.Dir
		kind := "worktree"
		if where == "" {
			where, kind = tree.Branch, "branch"
		}
		switch {
		case !found:
			keep(kind, where, "the tracker has no story "+tree.Story)
			liveStories[tree.Story] = true
			continue
		case detail.Status != StateClosed:
			// Live: claimed, being retried or waiting. Said by nobody.
			liveStories[tree.Story] = true
			continue
		case !detail.ClosedAt.IsZero() && now.Sub(detail.ClosedAt) < HostSweepClosedGrace:
			keep(kind, where, "closed less than an hour ago")
			continue
		case tree.Busy != "":
			keep(kind, where, tree.Busy)
			continue
		case tree.Uncommitted:
			keep(kind, where, "uncommitted changes")
			continue
		case tree.Unsaved != "":
			keep(kind, where, tree.Unsaved)
			// What is left of the tree is its commits: what a build makes again goes.
			for _, cache := range tree.Caches {
				s.removeCache(ctx, cache, tree.Story, remove)
			}
			continue
		}
		act := HostSweepAct{Kind: kind, Path: where, Bytes: tree.Bytes, Why: "story " + tree.Story + " is closed"}
		tree := tree
		remove(act, func() error { return s.Files.RemoveTree(ctx, tree) })
	}

	landings, err := s.Files.Landings(ctx)
	if err != nil {
		fault(fmt.Errorf("reading the landing directories: %w", err))
	}
	for _, dir := range landings {
		if now.Sub(dir.Modified) <= StaleLandingsAge {
			continue
		}
		if dir.Live != "" {
			keep("landing", dir.Path, dir.Live)
			continue
		}
		act := HostSweepAct{Kind: "landing", Path: dir.Path, Bytes: dir.Bytes, Why: "a landing directory older than a day with no live landing in it"}
		remove(act, func() error { return s.Files.RemoveDir(ctx, dir.Path) })
	}

	runs, err := s.Files.Runs(ctx)
	if err != nil {
		fault(fmt.Errorf("reading the runs: %w", err))
	}
	runsAge := orAge(s.RunsAge, HostSweepRunsAge)
	for _, dir := range runs {
		age := now.Sub(dir.Modified)
		if age < runsAge {
			continue
		}
		if dir.Tracked {
			keep("run", dir.Path, "the vault's git tracks it: the vault's sync owns it")
			continue
		}
		if liveStories[lastSegment(dir.Path)] {
			keep("run", dir.Path, "its story is not closed")
			continue
		}
		act := HostSweepAct{Kind: "run", Path: dir.Path, Bytes: dir.Bytes, Why: fmt.Sprintf("%s old, over the %s kept", days(age), days(runsAge))}
		remove(act, func() error { return s.Files.RemoveDir(ctx, dir.Path) })
	}

	asides, err := s.Files.Asides(ctx)
	if err != nil {
		fault(fmt.Errorf("reading the set-aside directories: %w", err))
	}
	asideAge := orAge(s.AsideAge, HostSweepAsideAge)
	for _, dir := range asides {
		age := now.Sub(dir.Modified)
		if age < asideAge {
			continue
		}
		act := HostSweepAct{Kind: "aside", Path: dir.Path, Bytes: dir.Bytes, Why: fmt.Sprintf("%s old, over the %s kept", days(age), days(asideAge))}
		remove(act, func() error { return s.Files.RemoveDir(ctx, dir.Path) })
	}

	if s.Out != nil {
		fmt.Fprint(s.Out, report.String())
	}
	return report, errors.Join(faults...)
}

// removeCache removes one cache directory of a kept tree.
func (s HostSweep) removeCache(ctx context.Context, cache HostSweepDir, story string, remove func(HostSweepAct, func() error)) {
	act := HostSweepAct{Kind: "cache", Path: cache.Path, Bytes: cache.Bytes, Why: "story " + story + " is closed and a build makes it again"}
	remove(act, func() error { return s.Files.RemoveDir(ctx, cache.Path) })
}

// storyStates reads each worktree's story from the tracker, once, by id.
func (s HostSweep) storyStates(ctx context.Context, trees []HostSweepTree, fault func(error)) map[string]StoryDetail {
	seen := map[string]bool{}
	var ids []string
	for _, tree := range trees {
		if !seen[tree.Story] {
			seen[tree.Story] = true
			ids = append(ids, tree.Story)
		}
	}
	sort.Strings(ids)
	details := map[string]StoryDetail{}
	if len(ids) == 0 {
		return details
	}
	found, err := s.Stories.ShowBeads(ctx, ids)
	if err != nil {
		// Without the tracker no story is known closed, so no tree is taken.
		fault(fmt.Errorf("reading the stories of the worktrees: %w", err))
		for _, id := range ids {
			details[id] = StoryDetail{Status: "unknown"}
		}
		return details
	}
	for _, detail := range found {
		details[detail.Story.ID] = detail
	}
	return details
}

// log adds the act to the tick log when it was done. A log that cannot be
// written is a note: the act is done.
func (s HostSweep) log(ctx context.Context, now time.Time, act HostSweepAct, report *HostSweepReport) {
	if s.Log == nil || s.DryRun {
		return
	}
	line := now.UTC().Format(time.RFC3339) + " " + HostSweepLogPrefix + "removed " + act.String()
	if err := s.Log.Append(ctx, line); err != nil {
		report.Notes = append(report.Notes, "the tick log could not be written: "+oneLine(err.Error()))
	}
}

func (s HostSweep) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now() //mw:realclock the default of the injected Now, used where none is set
}

func orAge(set, fallback time.Duration) time.Duration {
	if set > 0 {
		return set
	}
	return fallback
}

func days(d time.Duration) string { return fmt.Sprintf("%d days", int(d/(24*time.Hour))) }

func lastSegment(path string) string {
	path = strings.TrimRight(path, "/")
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

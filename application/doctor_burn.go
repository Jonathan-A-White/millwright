package application

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BuilderBurnName is what the check is called: in the log, and on the command
// line as `mw doctor builder-burn`.
const BuilderBurnName = "builder-burn"

// MailBuilderBurn starts the subject of the Mayor's mail about a tree of a
// Builder's processes that keeps the host over its cores.
const MailBuilderBurn = "Builder processes over the host's cores"

const (
	// BuilderBurnSustain is how long a tree must keep more processes running than
	// the host has cores before it is named: a gate runs flat out for minutes, and
	// that is its work.
	BuilderBurnSustain = 10 * time.Minute
	// BuilderBurnGap is the longest time between two looks that still count as
	// one stretch: a run after a longer gap says nothing of the time between.
	BuilderBurnGap = 30 * time.Minute
	// builderBurnState is where the check keeps what it saw last look.
	builderBurnState = "builder-burn-seen"
)

// RunnableProcess is one process of the host's table as BuilderBurnCheck reads
// it: where it is, what it runs and whether it is on a core or waiting for one
// (state R, the processes the load average counts).
type RunnableProcess struct {
	Pid, Ppid int
	Cwd       string
	Argv      []string
	Running   bool
}

// RunnableSnapshot is the host's whole table at one time.
type RunnableSnapshot struct {
	At        time.Time
	Processes []RunnableProcess
}

// RunnableTable reads the host's process table.
type RunnableTable interface {
	Snapshot(ctx context.Context) (RunnableSnapshot, error)
}

// BuilderBurnCheck is the mw doctor check for a Builder (or a batch's fixer)
// that makes load by hand: a tree of processes under one story's worktree, or
// one landing, with more of them running than the host has cores, look after
// look for over ten minutes (BuilderBurnSustain). A batch's fixer once ran forty
// busy loops of its own for 40 minutes on the 20-core home, load 55 to 65, and
// the Millhand found it (mw-gq6.1529; sixteen of them, load 44 of 20, on
// 2026-10-16: mw-gq6.483). The one bounded way is contrib/mw-under-load, which
// never starts more loops than the free cores.
//
// A tree is a process whose working directory is in a story's worktree (under
// WorktreeDirs) or in a landing (LandingPrefix, under LandingDirs) and that has
// no parent in the same one, with everything that descends from it, wherever
// each works: the burners of a Builder's script do not work in the worktree. Its
// load is the count of those processes running. The check keeps, under its own
// name, the trees it found over the cores and since when. The cure only mails
// the Mayor, so that he hears of it without the Millhand; it stops nothing, for
// the Mayor and the Builder's seat know which of the tree's processes are work.
type BuilderBurnCheck struct {
	// Host is this host, named in the mail.
	Host string
	// WorktreeDirs are the directories story worktrees are made in, one per rig;
	// LandingDirs the directories landings are made in (the home directory and
	// the one each rig lives in).
	WorktreeDirs []string
	LandingDirs  []string
	// Table is the host's process table, Cores how many cores it has.
	Table RunnableTable
	Cores func(ctx context.Context) (int, error)
	// State keeps what the last look saw.
	State DoctorState
	// Mail sends the Mayor the message.
	Mail func(ctx context.Context, message NewMessage) error

	found []burnTree
	cores int
}

var _ DoctorCheck = (*BuilderBurnCheck)(nil)
var _ DoctorCoolingQuiet = (*BuilderBurnCheck)(nil)
var _ DoctorStandsAfterCure = (*BuilderBurnCheck)(nil)

// burnTree is one owner's tree over the cores.
type burnTree struct {
	owner   string
	root    int
	procs   int
	running int
	since   time.Time
}

// Name implements DoctorCheck.
func (c *BuilderBurnCheck) Name() string { return BuilderBurnName }

// ownerOf says whose a working directory is: the story whose worktree it is in,
// or the landing it is in. ok is false for a directory that is neither.
func (c *BuilderBurnCheck) ownerOf(cwd string) (string, bool) {
	if cwd == "" {
		return "", false
	}
	cwd = filepath.Clean(cwd)
	for _, root := range c.WorktreeDirs {
		if first, ok := firstBelow(root, cwd); ok {
			return first, true
		}
	}
	for _, root := range c.LandingDirs {
		if first, ok := firstBelow(root, cwd); ok && strings.HasPrefix(first, LandingPrefix) {
			return first, true
		}
	}
	return "", false
}

// firstBelow is the first element of dir beneath root; ok is false when dir is
// not beneath it.
func firstBelow(root, dir string) (string, bool) {
	rel, err := filepath.Rel(filepath.Clean(root), dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return strings.SplitN(rel, string(filepath.Separator), 2)[0], true
}

// trees reads each owner's tree out of the table, counting its processes and
// those of them running.
func (c *BuilderBurnCheck) trees(snap RunnableSnapshot) map[string]*burnTree {
	byPid := map[int]RunnableProcess{}
	children := map[int][]int{}
	for _, p := range snap.Processes {
		byPid[p.Pid] = p
		children[p.Ppid] = append(children[p.Ppid], p.Pid)
	}
	owner := map[int]string{}
	for _, p := range snap.Processes {
		if o, ok := c.ownerOf(p.Cwd); ok {
			owner[p.Pid] = o
		}
	}
	out := map[string]*burnTree{}
	taken := map[int]bool{}
	pids := make([]int, 0, len(owner))
	for pid := range owner {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		o := owner[pid]
		if parent, ok := byPid[byPid[pid].Ppid]; ok && owner[parent.Pid] == o {
			continue // not a root: its parent is in the same tree
		}
		tree := out[o]
		if tree == nil {
			tree = &burnTree{owner: o, root: pid}
			out[o] = tree
		}
		stack := []int{pid}
		for len(stack) > 0 {
			at := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if taken[at] {
				continue
			}
			taken[at] = true
			tree.procs++
			if byPid[at].Running {
				tree.running++
			}
			stack = append(stack, children[at]...)
		}
	}
	return out
}

// Probe implements DoctorCheck: faulty, naming each owner whose tree has had
// more processes running than the host has cores at every look for over
// BuilderBurnSustain; cannot-tell when the table, the cores or the check's own
// memory cannot be read; ok otherwise. The reason holds no count or age that
// moves, so that the same tree said again reads the same and is said once.
func (c *BuilderBurnCheck) Probe(ctx context.Context) (Verdict, string) {
	c.found = nil
	cores, err := c.Cores(ctx)
	if err != nil {
		return DoctorCannotTell, fmt.Sprintf("the host's core count could not be read: %v", err)
	}
	if cores < 1 {
		return DoctorCannotTell, "the host's core count could not be read: it says none"
	}
	snap, err := c.Table.Snapshot(ctx)
	if err != nil {
		return DoctorCannotTell, fmt.Sprintf("the host's processes could not be read: %v", err)
	}
	prev, err := c.remembered(ctx, snap.At)
	if err != nil {
		return DoctorCannotTell, err.Error()
	}
	var over []*burnTree
	for _, tree := range c.trees(snap) {
		if tree.running <= cores {
			continue
		}
		tree.since = snap.At
		if at, ok := prev[tree.owner]; ok {
			tree.since = at
		}
		over = append(over, tree)
	}
	sort.Slice(over, func(i, j int) bool { return over[i].owner < over[j].owner })
	if err := c.remember(ctx, snap.At, over); err != nil {
		return DoctorCannotTell, err.Error()
	}
	var named []string
	for _, tree := range over {
		if snap.At.Sub(tree.since) > BuilderBurnSustain {
			c.found = append(c.found, *tree)
			named = append(named, tree.owner)
		}
	}
	c.cores = cores
	if len(named) == 0 {
		return DoctorOK, ""
	}
	return DoctorFaulty, fmt.Sprintf("a Builder's processes have kept more than the host's %d cores running for over %d minutes: %s",
		cores, int(BuilderBurnSustain/time.Minute), strings.Join(named, ", "))
}

// remembered is each owner the last look found over the cores and since when,
// and nothing when that look was too long ago to be one stretch with this.
func (c *BuilderBurnCheck) remembered(ctx context.Context, now time.Time) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	if c.State == nil {
		return out, nil
	}
	episode, err := c.State.Load(ctx, builderBurnState)
	if err != nil {
		return nil, fmt.Errorf("reading this check's own state: %w", err)
	}
	if len(episode.SeenPaths) == 0 {
		return out, nil
	}
	last, err := time.Parse(time.RFC3339Nano, episode.SeenPaths[0])
	if err != nil || now.Before(last) || now.Sub(last) > BuilderBurnGap {
		return out, nil
	}
	for _, line := range episode.SeenPaths[1:] {
		owner, since, ok := strings.Cut(line, " ")
		if at, err := time.Parse(time.RFC3339Nano, since); ok && err == nil {
			out[owner] = at
		}
	}
	return out, nil
}

// remember keeps the owners found over the cores now and since when; none found
// forgets the state altogether.
func (c *BuilderBurnCheck) remember(ctx context.Context, now time.Time, over []*burnTree) error {
	if c.State == nil {
		return nil
	}
	if len(over) == 0 {
		if err := c.State.Reset(ctx, builderBurnState); err != nil {
			return fmt.Errorf("forgetting this check's own state: %w", err)
		}
		return nil
	}
	seen := []string{now.UTC().Format(time.RFC3339Nano)}
	for _, tree := range over {
		seen = append(seen, tree.owner+" "+tree.since.UTC().Format(time.RFC3339Nano))
	}
	if err := c.State.Save(ctx, builderBurnState, DoctorEpisode{SeenPaths: seen}); err != nil {
		return fmt.Errorf("saving this check's own state: %w", err)
	}
	return nil
}

// Cure implements DoctorCheck: it mails the Mayor each tree found, with its
// root, how many of its processes were running and since when.
func (c *BuilderBurnCheck) Cure(ctx context.Context) error {
	if len(c.found) == 0 {
		return fmt.Errorf("no Builder tree over the cores was found to tell the Mayor of")
	}
	body := make([]string, 0, len(c.found)+3)
	for _, t := range c.found {
		body = append(body, fmt.Sprintf("%s: %d of its %d processes running (the host has %d cores), since %s, root pid %d",
			t.owner, t.running, t.procs, c.cores, t.since.UTC().Format(time.RFC3339), t.root))
	}
	body = append(body, "",
		"The tree is a Builder's session, or a batch's fixer, and what it started. Load is made under load only through contrib/mw-under-load <command>, "+
			"which starts at most 4 loops and never more than the host's cores less its load, each ended within 30 minutes (mw-gq6.1529). "+
			"Look with `ps -eo pid,ppid,stat,etime,args --forest`; mw doctor stops nothing.")
	return c.Mail(ctx, NewMessage{
		From:    SeatIdentity(MwSeat, c.Host),
		To:      MayorMailbox,
		Subject: fmt.Sprintf("%s: %s on %s", MailBuilderBurn, c.found[0].owner+plusMore(len(c.found)), c.Host),
		Body:    strings.Join(body, "\n"),
	})
}

// plusMore says " and N more" for the trees past the first.
func plusMore(n int) string {
	if n <= 1 {
		return ""
	}
	return " and " + strconv.Itoa(n-1) + " more"
}

// Damper implements DoctorCheck: one mail an hour while it lasts, no cap on the
// hours.
func (c *BuilderBurnCheck) Damper() (time.Duration, int) { return time.Hour, 0 }

// QuietWhileCooling implements DoctorCoolingQuiet: the hour between two mails is
// expected, and wakes nobody.
func (c *BuilderBurnCheck) QuietWhileCooling() bool { return true }

// CureLeavesFault implements DoctorStandsAfterCure: the mail changes nothing on
// the host, so the verdict other hosts read still says faulty.
func (c *BuilderBurnCheck) CureLeavesFault() bool { return true }

// WayBack implements DoctorCheck: nothing is changed but the mail.
func (c *BuilderBurnCheck) WayBack() string {
	return "none: the cure only mails the Mayor; end the burners (or the session) and the check clears at the next look"
}

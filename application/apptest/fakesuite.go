package apptest

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeSuiteTrees is an application.SuiteTrees that makes no worktree: it
// remembers which directories exist, so that a scenario can say the suite's was
// made, was not the rig's own checkout, and was taken away again.
type FakeSuiteTrees struct {
	mu sync.Mutex
	// Present names directories that are there before the suite starts, as a
	// killed run leaves one.
	Present []string
	// AddErr stops AddDetached.
	AddErr error

	added   []SuiteTree
	removed []string
	live    map[string]bool
}

// SuiteTree is one worktree the fake was asked to make: the rig, where, and at
// which commit.
type SuiteTree struct {
	RigDir, Dir, Commit string
}

var _ application.SuiteTrees = (*FakeSuiteTrees)(nil)

// AddDetached implements application.SuiteTrees.
func (f *FakeSuiteTrees) AddDetached(_ context.Context, rigDir, dir, commit string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AddErr != nil {
		return f.AddErr
	}
	if f.live == nil {
		f.live = map[string]bool{}
	}
	if f.live[dir] {
		return fmt.Errorf("%s is already there", dir)
	}
	f.live[dir] = true
	f.added = append(f.added, SuiteTree{RigDir: rigDir, Dir: dir, Commit: commit})
	return nil
}

// Exists implements application.SuiteTrees.
func (f *FakeSuiteTrees) Exists(_ context.Context, _, dir, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live[dir] || holds(f.Present, dir), nil
}

// Remove implements application.SuiteTrees.
func (f *FakeSuiteTrees) Remove(_ context.Context, _, dir, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.live, dir)
	f.Present = without(f.Present, dir)
	f.removed = append(f.removed, dir)
	return nil
}

// Added is every worktree the fake made, in order.
func (f *FakeSuiteTrees) Added() []SuiteTree {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SuiteTree(nil), f.added...)
}

// Removed is every directory the fake was asked to take away, in order.
func (f *FakeSuiteTrees) Removed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// Live is how many worktrees are still there.
func (f *FakeSuiteTrees) Live() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.live)
}

func without(list []string, drop string) []string {
	var kept []string
	for _, item := range list {
		if item != drop {
			kept = append(kept, item)
		}
	}
	return kept
}

// SuiteRan is one command line the fake runner was asked to run.
type SuiteRan struct {
	Rig, Dir, Command string
}

// FakeSuiteRunner is an application.SuiteRunner that runs nothing: Respond says
// what each command line printed, and Ran remembers what was asked.
type FakeSuiteRunner struct {
	mu sync.Mutex
	// Respond answers a command line run in dir. Nil is a pass that printed
	// nothing.
	Respond func(dir, command string) (application.Checked, error)

	ran []SuiteRan
}

var _ application.SuiteRunner = (*FakeSuiteRunner)(nil)

// RunLine implements application.SuiteRunner.
func (f *FakeSuiteRunner) RunLine(_ context.Context, rig, dir, command string) (application.Checked, error) {
	f.mu.Lock()
	f.ran = append(f.ran, SuiteRan{Rig: rig, Dir: dir, Command: command})
	respond := f.Respond
	f.mu.Unlock()
	if respond == nil {
		return application.Checked{Command: command, Passed: true}, nil
	}
	checked, err := respond(dir, command)
	checked.Command = command
	return checked, err
}

// Ran is every command line run, in order.
func (f *FakeSuiteRunner) Ran() []SuiteRan {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SuiteRan(nil), f.ran...)
}

// FakeSuiteRecords is an application.SuiteRecords that keeps the files in
// memory, by their path from the vault's root.
type FakeSuiteRecords struct {
	mu    sync.Mutex
	files map[string]string
	order []string
}

var _ application.SuiteRecords = (*FakeSuiteRecords)(nil)

// PutSuiteFile implements application.SuiteRecords.
func (f *FakeSuiteRecords) PutSuiteFile(_ context.Context, rig, name, contents string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.files == nil {
		f.files = map[string]string{}
	}
	at := path.Join(application.SuiteDir, rig, name)
	if _, there := f.files[at]; !there {
		f.order = append(f.order, at)
	}
	f.files[at] = contents
	return at, nil
}

// File is what was written at a path from the vault's root, and whether
// anything was.
func (f *FakeSuiteRecords) File(at string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	contents, there := f.files[at]
	return contents, there
}

// Paths is every path written, in the order first written.
func (f *FakeSuiteRecords) Paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
}

var _ application.SuiteHistory = (*FakeSuiteRecords)(nil)

// LatestSuite implements application.SuiteHistory: the newest of the
// <date>.json files put for rig.
func (f *FakeSuiteRecords) LatestSuite(_ context.Context, rig string) (application.SuiteRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var day string
	for at := range f.files {
		dir, name := path.Split(at)
		if dir != application.SuiteDir+"/"+rig+"/" || !strings.HasSuffix(name, ".json") {
			continue
		}
		if d := strings.TrimSuffix(name, ".json"); d > day {
			day = d
		}
	}
	if day == "" {
		return application.SuiteRecord{}, false, nil
	}
	record, err := application.ParseSuiteRecord(day, f.files[path.Join(application.SuiteDir, rig, day+".json")])
	return record, err == nil, err
}

// FakeSuiteProcesses is an application.SuiteProcesses whose suites run apart
// from whoever began them, as the real ones do: a suite begun stays running,
// whatever becomes of the context it was begun under, until a scenario ends it.
type FakeSuiteProcesses struct {
	mu      sync.Mutex
	procs   map[string]*fakeSuiteProc
	begun   []string
	waiting map[string]int
}

type fakeSuiteProc struct {
	done chan struct{}
	err  error
	told bool // a Wait has returned its outcome
}

// owed says whether p is still running, or ended with nobody told how.
func (p *fakeSuiteProc) owed() bool {
	select {
	case <-p.done:
		return !p.told
	default:
		return true
	}
}

var _ application.SuiteProcesses = (*FakeSuiteProcesses)(nil)

// Begin implements application.SuiteProcesses: a suite of rig already running,
// or ended with nobody told how, is left as it is.
func (f *FakeSuiteProcesses) Begin(_ context.Context, rig string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.procs[rig]; p != nil && p.owed() {
		return nil
	}
	if f.procs == nil {
		f.procs = map[string]*fakeSuiteProc{}
	}
	f.procs[rig] = &fakeSuiteProc{done: make(chan struct{})}
	f.begun = append(f.begun, rig)
	return nil
}

// Wait implements application.SuiteProcesses.
func (f *FakeSuiteProcesses) Wait(ctx context.Context, rig string) error {
	f.mu.Lock()
	p := f.procs[rig]
	if f.waiting == nil {
		f.waiting = map[string]int{}
	}
	f.waiting[rig]++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.waiting[rig]--
		f.mu.Unlock()
	}()
	if p == nil {
		return fmt.Errorf("no suite of %s was begun", rig)
	}
	select {
	case <-p.done:
		f.mu.Lock()
		p.told = true
		f.mu.Unlock()
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Pending names the rigs whose suite is running or ended with nobody told how.
func (f *FakeSuiteProcesses) Pending(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for rig, p := range f.procs {
		if p.owed() {
			names = append(names, rig)
		}
	}
	sort.Strings(names)
	return names, nil
}

// FakeLandProcesses is an application.LandProcesses whose landings run apart
// from whoever began them, as FakeSuiteProcesses' suites do; End, Waiting and
// Begun are theirs.
type FakeLandProcesses struct {
	FakeSuiteProcesses
}

var _ application.LandProcesses = (*FakeLandProcesses)(nil)

// Begin implements application.LandProcesses.
func (f *FakeLandProcesses) Begin(ctx context.Context, rig string, _ bool) error {
	return f.FakeSuiteProcesses.Begin(ctx, rig)
}

// Waiting says whether something is waiting on the suite of rig.
func (f *FakeSuiteProcesses) Waiting(rig string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waiting[rig] > 0
}

// End ends the suite of rig: with err, it failed.
func (f *FakeSuiteProcesses) End(rig string, err error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.procs[rig]
	if p == nil {
		return fmt.Errorf("no suite of %s is running", rig)
	}
	p.err = err
	close(p.done)
	return nil
}

// Begun is how many times a suite of rig was begun.
func (f *FakeSuiteProcesses) Begun(rig string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.begun {
		if r == rig {
			n++
		}
	}
	return n
}

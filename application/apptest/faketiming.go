package apptest

import (
	"context"
	"path"
	"strings"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeTimingRig is an application.TimingRig over what it is told: each rig
// directory's tagged tests, and what changed between two commits.
type FakeTimingRig struct {
	mu sync.Mutex
	// Tests are the tagged files of a rig directory.
	Tests map[string][]application.TaggedTest
	// Changes are the files that differ between "<from>..<to>".
	Changes map[string][]string
	// ChangedErr is what ChangedBetween says when it is set.
	ChangedErr error

	asked []string
}

var _ application.TimingRig = (*FakeTimingRig)(nil)

// Tagged implements application.TimingRig.
func (f *FakeTimingRig) Tagged(_ context.Context, rigDir, _ string, _ []string) ([]application.TaggedTest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Tests[rigDir], nil
}

// ChangedBetween implements application.TimingRig.
func (f *FakeTimingRig) ChangedBetween(_ context.Context, _, from, to string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, from+".."+to)
	if f.ChangedErr != nil {
		return nil, f.ChangedErr
	}
	return f.Changes[from+".."+to], nil
}

// Asked is every range ChangedBetween was asked about, in order.
func (f *FakeTimingRig) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// FakeTimingRecords is an application.TimingRecords and TimingHistory that
// keeps the files in memory, by their path from the vault's root.
type FakeTimingRecords struct {
	mu    sync.Mutex
	files map[string]string
	order []string
}

var _ application.TimingRecords = (*FakeTimingRecords)(nil)
var _ application.TimingHistory = (*FakeTimingRecords)(nil)

// PutTimingFile implements application.TimingRecords.
func (f *FakeTimingRecords) PutTimingFile(_ context.Context, rig, name, contents string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.files == nil {
		f.files = map[string]string{}
	}
	at := path.Join(application.TimingDir, rig, name)
	if _, there := f.files[at]; !there {
		f.order = append(f.order, at)
	}
	f.files[at] = contents
	return at, nil
}

// File is what was written at a path from the vault's root.
func (f *FakeTimingRecords) File(at string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	contents, there := f.files[at]
	return contents, there
}

// Paths is every path written, in the order first written.
func (f *FakeTimingRecords) Paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
}

// records are the kept results of rig, newest day first.
func (f *FakeTimingRecords) records(rig string) ([]application.TimingRecord, error) {
	var days []string
	for at := range f.files {
		dir, name := path.Split(at)
		if dir == application.TimingDir+"/"+rig+"/" && strings.HasSuffix(name, ".json") {
			days = append(days, strings.TrimSuffix(name, ".json"))
		}
	}
	for i := range days {
		for j := i + 1; j < len(days); j++ {
			if days[j] > days[i] {
				days[i], days[j] = days[j], days[i]
			}
		}
	}
	var found []application.TimingRecord
	for _, day := range days {
		record, err := application.ParseTimingRecord(day, f.files[path.Join(application.TimingDir, rig, day+".json")])
		if err != nil {
			return nil, err
		}
		found = append(found, record)
	}
	return found, nil
}

// LatestTiming implements application.TimingHistory.
func (f *FakeTimingRecords) LatestTiming(_ context.Context, rig string) (application.TimingRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	found, err := f.records(rig)
	if err != nil || len(found) == 0 {
		return application.TimingRecord{}, false, err
	}
	return found[0], true, nil
}

// LatestPassingTiming implements application.TimingHistory.
func (f *FakeTimingRecords) LatestPassingTiming(_ context.Context, rig string) (application.TimingRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	found, err := f.records(rig)
	for _, record := range found {
		if record.Result.Passed {
			return record, true, nil
		}
	}
	return application.TimingRecord{}, false, err
}

// TimingRan is one command line the fake timing runner was asked to run, with
// the environment it was handed.
type TimingRan struct {
	Rig, Dir, Command string
	Env               map[string]string
}

// FakeTimingRunner is an application.TimingRunner that runs nothing: Respond
// says what each command printed, and Ran remembers what was asked.
type FakeTimingRunner struct {
	mu sync.Mutex
	// Respond answers a command line run in dir. Nil is a pass that printed
	// nothing.
	Respond func(dir, command string, env map[string]string) (application.Checked, error)

	ran []TimingRan
}

var _ application.TimingRunner = (*FakeTimingRunner)(nil)

// RunTiming implements application.TimingRunner.
func (f *FakeTimingRunner) RunTiming(_ context.Context, rig, dir, command string, env map[string]string) (application.Checked, error) {
	f.mu.Lock()
	f.ran = append(f.ran, TimingRan{Rig: rig, Dir: dir, Command: command, Env: env})
	respond := f.Respond
	f.mu.Unlock()
	if respond == nil {
		return application.Checked{Command: command, Passed: true}, nil
	}
	checked, err := respond(dir, command, env)
	checked.Command = command
	return checked, err
}

// Ran is every command line run, in order.
func (f *FakeTimingRunner) Ran() []TimingRan {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TimingRan(nil), f.ran...)
}

// FakeUptime is an application.Uptimes that says each of Lines in turn, and
// the last again after; "up 1 day, load average: 0.10" when it has none.
type FakeUptime struct {
	mu    sync.Mutex
	Lines []string
	reads int
}

var _ application.Uptimes = (*FakeUptime)(nil)

// Uptime implements application.Uptimes.
func (f *FakeUptime) Uptime(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if len(f.Lines) == 0 {
		return "up 1 day, load average: 0.10", nil
	}
	return f.Lines[min(f.reads, len(f.Lines))-1], nil
}

// Reads is how many times the uptime was asked for.
func (f *FakeUptime) Reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

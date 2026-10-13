package apptest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

// BdFault is one way a write to the beads server goes wrong for a FakeBd.
// Said is what bd prints. Landed is a write that went through all the same, as
// one on a connection that dropped after the server had it can: bd then says
// the result is indeterminate and the story is changed.
type BdFault struct {
	Said   string
	Landed bool
}

// FakeBd is a bd program with a few beads behind it: it closes beads, comments
// on them and reads both back, and goes wrong the way a beads server on a
// passing fault does. It stands in for application.BdProgram.
type FakeBd struct {
	mu sync.Mutex

	status     map[string]string
	comments   map[string][]string
	faults     []BdFault
	readFaults []BdFault
	down       string
	calls      [][]string
	now        func() time.Time
}

// NewFakeBd is a bd whose beads are the given ids, all open.
func NewFakeBd(ids ...string) *FakeBd {
	f := &FakeBd{status: map[string]string{}, comments: map[string][]string{}, now: time.Now}
	for _, id := range ids {
		f.status[id] = "open"
	}
	return f
}

// FailWrites makes the next writes, one fault each in order, go wrong before
// the ones after them work again.
func (f *FakeBd) FailWrites(faults ...BdFault) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults = append(f.faults, faults...)
}

// FailReads makes the next reads, one fault each in order, fail before the ones
// after them work again.
func (f *FakeBd) FailReads(faults ...BdFault) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readFaults = append(f.readFaults, faults...)
}

// GoDown makes every command from now on fail, reads included, saying said.
func (f *FakeBd) GoDown(said string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = said
}

// Status is the status of a bead.
func (f *FakeBd) Status(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status[id]
}

// Comments are the comments left on a bead, oldest first.
func (f *FakeBd) Comments(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.comments[id]...)
}

// Calls are the argument lists bd was run with, in order.
func (f *FakeBd) Calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.calls...)
}

// Run implements application.BdProgram.
func (f *FakeBd) Run(_ context.Context, args []string) (application.BdRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), args...))

	if f.down != "" {
		return application.BdRun{Stderr: "Error: " + f.down + "\n", Exit: 1}, nil
	}
	rest := args
	for len(rest) > 1 && (rest[0] == "-C" || rest[0] == "--actor") {
		rest = rest[2:]
	}
	if len(rest) == 0 {
		return application.BdRun{}, nil
	}

	if (rest[0] == "show" || rest[0] == "comments" && !(len(rest) > 1 && rest[1] == "add")) && len(f.readFaults) > 0 {
		fault := f.readFaults[0]
		f.readFaults = f.readFaults[1:]
		return application.BdRun{Stderr: "Error: " + fault.Said + "\n", Exit: 1}, nil
	}

	switch rest[0] {
	case "close":
		ids := []string{}
		for i := 1; i < len(rest); i++ {
			switch {
			case rest[i] == "--reason" || rest[i] == "-r":
				i++
			case !strings.HasPrefix(rest[i], "-"):
				ids = append(ids, rest[i])
			}
		}
		return f.write(func() {
			for _, id := range ids {
				f.status[id] = "closed"
			}
		}), nil
	case "comment":
		return f.write(func() { f.comments[rest[1]] = append(f.comments[rest[1]], strings.Join(rest[2:], " ")) }), nil
	case "comments":
		if len(rest) > 2 && rest[1] == "add" {
			return f.write(func() { f.comments[rest[2]] = append(f.comments[rest[2]], strings.Join(rest[3:], " ")) }), nil
		}
		type said struct {
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
		}
		var listed []said
		for _, text := range f.comments[rest[1]] {
			listed = append(listed, said{Text: text, CreatedAt: f.now().UTC().Format(time.RFC3339)})
		}
		out, _ := json.Marshal(listed)
		return application.BdRun{Stdout: string(out) + "\n"}, nil
	case "create":
		return f.write(func() { f.status[strings.Join(rest[1:], " ")] = "open" }), nil
	case "show":
		out, _ := json.Marshal([]map[string]string{{"id": rest[1], "status": f.status[rest[1]]}})
		return application.BdRun{Stdout: string(out) + "\n"}, nil
	}
	return application.BdRun{Stdout: "ok\n"}, nil
}

// write runs apply unless a fault is next in line for a write.
func (f *FakeBd) write(apply func()) application.BdRun {
	if len(f.faults) == 0 {
		apply()
		return application.BdRun{Stdout: "done\n"}
	}
	fault := f.faults[0]
	f.faults = f.faults[1:]
	if fault.Landed {
		apply()
	}
	return application.BdRun{Stderr: fmt.Sprintf("Error: %s\n", fault.Said), Exit: 1}
}

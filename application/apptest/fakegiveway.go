package apptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Jonathan-A-White/millwright/application"
)

// FakeGiveWayHost is a host's give-way world in memory: its process table, its
// tmux panes, the marker file and the log, behind the four ports
// application.GiveWay works through. It holds every signal sent, in order, in
// Calls, and a shared Order a test can pass to the other fakes it wires in (a
// retry, a tracker) to learn what happened before the last stop.
type FakeGiveWayHost struct {
	mu sync.Mutex

	// Procs is the process table. Stop sets a matching process to state T and
	// Continue to S, as the kernel would.
	Procs []application.GiveWayProcess
	// PaneList is the tmux server's panes.
	PaneList []application.GiveWayPane
	// Forks are children a process forks in the instant it is stopped: they
	// appear in the table after the Stop of their parent, as a child that was
	// mid-fork does.
	Forks map[int][]application.GiveWayProcess

	// Marker is giveway.json, nil when none stands; Lines is giveway.log.
	Marker *application.GiveWayMarker
	Lines  []string

	// Calls is every Stop and Continue sent, as "stop <pid>" and "cont <pid>",
	// and every refused one as "skip stop <pid>" and "skip cont <pid>".
	Calls []string
	// Order is every event of the host and of the fakes that share it, in order.
	Order []string
}

var (
	_ application.GiveWayProcesses = (*FakeGiveWayHost)(nil)
	_ application.GiveWayPanes     = (*FakeGiveWayHost)(nil)
	_ application.GiveWayMarkers   = (*FakeGiveWayHost)(nil)
	_ application.GiveWayLog       = (*FakeGiveWayHost)(nil)
)

// Note puts an event in Order, for the fakes that share it.
func (h *FakeGiveWayHost) Note(event string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Order = append(h.Order, event)
}

// Reuse gives pid to a new process, as the kernel does once the old one has
// ended: the start time differs.
func (h *FakeGiveWayHost) Reuse(pid int, start string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.Procs {
		if h.Procs[i].Pid == pid {
			h.Procs[i].Start = start
			h.Procs[i].State = "S"
		}
	}
}

// State is the state letter of pid, "" when no such process.
func (h *FakeGiveWayHost) State(pid int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.Procs {
		if p.Pid == pid {
			return p.State
		}
	}
	return ""
}

// Stopped is the pids in state T, in table order.
func (h *FakeGiveWayHost) Stopped() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var pids []int
	for _, p := range h.Procs {
		if p.State == "T" {
			pids = append(pids, p.Pid)
		}
	}
	return pids
}

// List implements application.GiveWayProcesses.
func (h *FakeGiveWayHost) List(context.Context) ([]application.GiveWayProcess, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Order = append(h.Order, "list")
	return append([]application.GiveWayProcess(nil), h.Procs...), nil
}

// Stop implements application.GiveWayProcesses.
func (h *FakeGiveWayHost) Stop(_ context.Context, pid int, start string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.Procs {
		if h.Procs[i].Pid != pid {
			continue
		}
		if h.Procs[i].Start != start {
			h.Calls = append(h.Calls, fmt.Sprintf("skip stop %d", pid))
			return false, nil
		}
		h.Procs[i].State = "T"
		h.Calls = append(h.Calls, fmt.Sprintf("stop %d", pid))
		h.Order = append(h.Order, fmt.Sprintf("stop %d", pid))
		h.Procs = append(h.Procs, h.Forks[pid]...)
		delete(h.Forks, pid)
		return true, nil
	}
	h.Calls = append(h.Calls, fmt.Sprintf("skip stop %d", pid))
	return false, nil
}

// Continue implements application.GiveWayProcesses.
func (h *FakeGiveWayHost) Continue(_ context.Context, pid int, start string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.Procs {
		if h.Procs[i].Pid != pid {
			continue
		}
		if h.Procs[i].Start != start {
			h.Calls = append(h.Calls, fmt.Sprintf("skip cont %d", pid))
			return false, nil
		}
		h.Procs[i].State = "S"
		h.Calls = append(h.Calls, fmt.Sprintf("cont %d", pid))
		h.Order = append(h.Order, fmt.Sprintf("cont %d", pid))
		return true, nil
	}
	h.Calls = append(h.Calls, fmt.Sprintf("skip cont %d", pid))
	return false, nil
}

// Panes implements application.GiveWayPanes.
func (h *FakeGiveWayHost) Panes(context.Context) ([]application.GiveWayPane, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]application.GiveWayPane(nil), h.PaneList...), nil
}

// Read implements application.GiveWayMarkers.
func (h *FakeGiveWayHost) Read(context.Context) (application.GiveWayMarker, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.Marker == nil {
		return application.GiveWayMarker{}, false, nil
	}
	return *h.Marker, true, nil
}

// Write implements application.GiveWayMarkers.
func (h *FakeGiveWayHost) Write(_ context.Context, marker application.GiveWayMarker) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Order = append(h.Order, "marker written")
	h.Marker = &marker
	return nil
}

// Remove implements application.GiveWayMarkers.
func (h *FakeGiveWayHost) Remove(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Marker = nil
	return nil
}

// Append implements application.GiveWayLog.
func (h *FakeGiveWayHost) Append(_ context.Context, line string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Order = append(h.Order, "log")
	h.Lines = append(h.Lines, line)
	return nil
}

// Tail implements application.GiveWayLog.
func (h *FakeGiveWayHost) Tail(_ context.Context, n int) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines := h.Lines
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return append([]string(nil), lines...), nil
}

// FakeGiveWayRetry is the mw retry a give-way off runs for a dead session. It
// is a call to the work tracker, so it notes itself in the host's Order; Err
// makes it fail, as a tracker that cannot be reached does.
type FakeGiveWayRetry struct {
	Host    *FakeGiveWayHost
	Err     error
	Retried []string
}

var _ application.PauseRetry = (*FakeGiveWayRetry)(nil)

// Retry implements application.PauseRetry.
func (r *FakeGiveWayRetry) Retry(_ context.Context, story string, _ bool) (string, error) {
	if r.Host != nil {
		r.Host.Note("retry " + story)
	}
	if r.Err != nil {
		return "", r.Err
	}
	r.Retried = append(r.Retried, story)
	return story + " is open again", nil
}

package application_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// fakePreviewServers is the preview servers a host runs, and the pids Stop was
// asked to stop. Stopping removes the server, as the real adapter's does, and
// a pid already gone is no error.
type fakePreviewServers struct {
	running []application.PreviewServer
	stopped []int
}

func (f *fakePreviewServers) List(context.Context) ([]application.PreviewServer, error) {
	return append([]application.PreviewServer(nil), f.running...), nil
}

func (f *fakePreviewServers) Stop(_ context.Context, server application.PreviewServer) error {
	f.stopped = append(f.stopped, server.Pid)
	for i, running := range f.running {
		if running.Pid == server.Pid {
			f.running = append(f.running[:i], f.running[i+1:]...)
			break
		}
	}
	return nil
}

// previewStoryStates answers StoryClosed from a table of story status.
func previewStoryStates(states map[string]string) func(context.Context, string) (bool, error) {
	return func(_ context.Context, id string) (bool, error) {
		status, ok := states[id]
		if !ok {
			return false, fmt.Errorf("no story %s", id)
		}
		return status == "closed", nil
	}
}

const (
	previewWorktrees = "/home/jonathan/.mw-worktrees"
	previewPostern   = "/home/jonathan/postern"
)

func aPreviewIn(pid, port int, cwd string, deleted bool) application.PreviewServer {
	return application.PreviewServer{Pid: pid, Port: port, Cwd: cwd, CwdDeleted: deleted,
		Cmd: fmt.Sprintf("vite preview --port %d --strictPort", port)}
}

func strayPreviewCheck(servers *fakePreviewServers, states map[string]string) *application.StrayPreviewsCheck {
	return &application.StrayPreviewsCheck{
		WorktreeDirs: []string{previewWorktrees, previewWorktrees},
		Servers:      servers,
		StoryClosed:  previewStoryStates(states),
	}
}

func TestMwDoctorNamesAPreviewLeftByADeletedWorktreeOrAClosedStoryWithPidPortAndStory(t *testing.T) {
	servers := &fakePreviewServers{running: []application.PreviewServer{
		aPreviewIn(760426, 5205, filepath.Join(previewWorktrees, "mw-5r3p30.943"), true),
		aPreviewIn(848366, 5367, filepath.Join(previewWorktrees, "mw-5r3p30.882", "node_modules"), false),
	}}
	check := strayPreviewCheck(servers, map[string]string{"mw-5r3p30.943": "closed", "mw-5r3p30.882": "closed"})

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty {
		t.Fatalf("expected faulty, which the cure puts right, got %v: %s", verdict, reason)
	}
	for _, want := range []string{"760426", "5205", "mw-5r3p30.943", "848366", "5367", "mw-5r3p30.882"} {
		if !strings.Contains(reason, want) {
			t.Errorf("expected the finding to name %q, got %q", want, reason)
		}
	}
	if len(servers.stopped) != 0 {
		t.Errorf("a probe changes nothing, but it stopped %v", servers.stopped)
	}
}

func TestMwDoctorNamesAPreviewOfADeletedWorktreeWhateverItsStoryReads(t *testing.T) {
	// A deleted worktree is gone for any story, open or one the tracker cannot
	// read: nothing is left for its preview to serve.
	servers := &fakePreviewServers{running: []application.PreviewServer{
		aPreviewIn(11, 5001, filepath.Join(previewWorktrees, "mw-1.1"), true),
		aPreviewIn(12, 5002, filepath.Join(previewWorktrees, "mw-2.2"), true),
	}}
	check := strayPreviewCheck(servers, map[string]string{"mw-1.1": "in_progress"})

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "mw-1.1") || !strings.Contains(reason, "mw-2.2") {
		t.Errorf("expected both deleted worktrees' previews named, got %v %q", verdict, reason)
	}
}

func TestMwDoctorNeverNamesAPreviewOfAnOpenStoryOrOutsideTheWorktreeDirs(t *testing.T) {
	servers := &fakePreviewServers{running: []application.PreviewServer{
		aPreviewIn(21, 5101, filepath.Join(previewWorktrees, "mw-open.1"), false),
		aPreviewIn(22, 5102, filepath.Join(previewWorktrees, "mw-working.2", "src"), false),
		aPreviewIn(23, 4173, previewPostern, false),
		aPreviewIn(24, 4174, "/home/jonathan/postern-old", true),
		aPreviewIn(25, 4175, previewWorktrees+"-not", true),
		aPreviewIn(26, 4176, previewWorktrees, false),
	}}
	check := strayPreviewCheck(servers, map[string]string{"mw-open.1": "open", "mw-working.2": "in_progress"})

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Errorf("expected a plain ok, got %v %q", verdict, reason)
	}
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("cure: %v", err)
	}
	if len(servers.stopped) != 0 {
		t.Errorf("expected nothing stopped, got pids %v", servers.stopped)
	}
}

func TestMwDoctorStopsOnlyTheStrayPreviewsAndSaysOkAfterwards(t *testing.T) {
	servers := &fakePreviewServers{running: []application.PreviewServer{
		aPreviewIn(31, 5201, filepath.Join(previewWorktrees, "mw-closed.1"), false),
		aPreviewIn(32, 5202, filepath.Join(previewWorktrees, "mw-open.2"), false),
		aPreviewIn(33, 4173, previewPostern, false),
		aPreviewIn(34, 5203, filepath.Join(previewWorktrees, "mw-gone.3"), true),
	}}
	check := strayPreviewCheck(servers, map[string]string{"mw-closed.1": "closed", "mw-open.2": "in_progress", "mw-gone.3": "closed"})

	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("cure: %v", err)
	}
	if got := fmt.Sprint(servers.stopped); got != "[31 34]" {
		t.Errorf("expected only pids 31 and 34 stopped, got %s", got)
	}
	for _, want := range []string{"31", "34", "5201", "5203"} {
		if !strings.Contains(check.WayBack(), want) {
			t.Errorf("expected the way back to name %q, got %q", want, check.WayBack())
		}
	}
	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK || reason != "" {
		t.Errorf("expected ok after the cure, got %v %q", verdict, reason)
	}
	// Run again with nothing left to stop: no error, nothing else stopped.
	if err := check.Cure(context.Background()); err != nil {
		t.Fatalf("a second cure: %v", err)
	}
	if len(servers.stopped) != 2 {
		t.Errorf("expected the second cure to stop nothing, got %v", servers.stopped)
	}
	if got := check.WayBack(); got != "nothing was changed" {
		t.Errorf("expected the way back of a cure that stopped nothing to say so, got %q", got)
	}
}

func TestMwDoctorCannotTellWhenAClosedStoryCannotBeReadAndNamesNothingForIt(t *testing.T) {
	servers := &fakePreviewServers{running: []application.PreviewServer{
		aPreviewIn(41, 5301, filepath.Join(previewWorktrees, "mw-unreadable.1"), false),
	}}
	check := strayPreviewCheck(servers, map[string]string{})

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "mw-unreadable.1") {
		t.Errorf("expected cannot-tell naming the story, got %v %q", verdict, reason)
	}
	if err := check.Cure(context.Background()); err != nil || len(servers.stopped) != 0 {
		t.Errorf("a preview whose story could not be read is left alone: err %v, stopped %v", err, servers.stopped)
	}
}

type failingPreviewServers struct{ fakePreviewServers }

func (f *failingPreviewServers) List(context.Context) ([]application.PreviewServer, error) {
	return nil, errors.New("proc unreadable")
}

func TestMwDoctorCannotTellWhenTheHostsPreviewServersCannotBeListed(t *testing.T) {
	check := &application.StrayPreviewsCheck{WorktreeDirs: []string{previewWorktrees}, Servers: &failingPreviewServers{}}
	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorCannotTell || !strings.Contains(reason, "proc unreadable") {
		t.Errorf("expected cannot-tell with the cause, got %v %q", verdict, reason)
	}
}

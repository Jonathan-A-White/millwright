package giveway

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
)

func TestStoreKeepsTheMarkerWholeAndTakesItAway(t *testing.T) {
	ctx := context.Background()
	s := Store{Dir: filepath.Join(t.TempDir(), "state")}
	if _, there, err := s.Read(ctx); err != nil || there {
		t.Fatalf("an empty store: there=%v err=%v", there, err)
	}
	marker := application.GiveWayMarker{
		Since: time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC), By: "mw@desktop", Seen: time.Date(2026, 11, 13, 16, 45, 0, 0, time.UTC),
		Sessions: []application.GiveWaySession{{Name: "mw-a_1", Story: "mw-a.1"}},
		Stopped:  []application.GiveWayStopped{{Pid: 12, Start: "345", Cmd: "claude"}},
	}
	if err := s.Write(ctx, marker); err != nil {
		t.Fatal(err)
	}
	got, there, err := s.Read(ctx)
	if err != nil || !there || !reflect.DeepEqual(got, marker) {
		t.Fatalf("read %+v there=%v err=%v, want %+v", got, there, err, marker)
	}
	if err := s.Remove(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx); err != nil {
		t.Fatalf("removing none: %v", err)
	}
	if _, there, _ := s.Read(ctx); there {
		t.Fatal("the marker is still there")
	}
}

func TestStoreRefusesAFileThatIsNotAMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte("half a ma"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (Store{Dir: dir}).Read(context.Background()); err == nil || !strings.Contains(err.Error(), "remove it") {
		t.Fatalf("err = %v, want a refusal saying to remove it", err)
	}
}

func TestStoreAppendsLinesAndTailsTheLastOnes(t *testing.T) {
	ctx := context.Background()
	s := Store{Dir: t.TempDir()}
	if lines, err := s.Tail(ctx, 5); err != nil || len(lines) != 0 {
		t.Fatalf("an empty log: %q %v", lines, err)
	}
	for _, l := range []string{"a", "b", "c", "d"} {
		if err := s.Append(ctx, l+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	if lines, _ := s.Tail(ctx, 2); !reflect.DeepEqual(lines, []string{"c", "d"}) {
		t.Fatalf("tail = %q", lines)
	}
}

func TestParsePanesReadsSessionPidAndDeadness(t *testing.T) {
	got := ParsePanes("mw-a_1|200|0\nodd|name|301|0\nmw-b_2|0|1\nbroken\n")
	want := []application.GiveWayPane{{Session: "mw-a_1", Pid: 200}, {Session: "odd|name", Pid: 301}, {Session: "mw-b_2", Dead: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("panes = %+v, want %+v", got, want)
	}
}

// aTmuxThatServes writes a stand-in tmux that answers `list-panes` for each server
// by its -L socket name (none is the default server), and, for a socket it does not
// know, as tmux does when no server runs.
func aTmuxThatServes(t *testing.T, byServer map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nsocket=default\nif [ \"$1\" = -L ]; then socket=$2; shift 2; fi\ncase \"$socket\" in\n"
	for socket, printed := range byServer {
		script += socket + ") printf '%s' '" + printed + "' ;;\n"
	}
	script += "*) echo 'no server running on /tmp/tmux-1000/'\"$socket\" >&2; exit 1 ;;\nesac\n"
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// mw-2891blf.7: panes are listed from the default server and from every socket
// the host names, so a factory whose sessions run on a -L server is seen.
func TestServersListThePanesOfTheDefaultServerAndOfEachSocket(t *testing.T) {
	tmux := aTmuxThatServes(t, map[string]string{
		"default": "0|600|0\n",
		"mw-seat": "mw-a_1|200|0\nmw-b_2|0|1\n",
	})
	servers := ServersFor([]string{"mw-seat", "idle-socket", "mw-seat", ""})
	for i := range servers {
		servers[i].Program = tmux
	}
	if len(servers) != 3 {
		t.Fatalf("servers = %+v, want the default, mw-seat and idle-socket (each once)", servers)
	}
	got, err := servers.Panes(context.Background())
	if err != nil {
		t.Fatalf("a socket with no server running is no error: %v", err)
	}
	want := []application.GiveWayPane{{Session: "0", Pid: 600}, {Session: "mw-a_1", Pid: 200}, {Session: "mw-b_2", Dead: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("panes = %+v, want %+v", got, want)
	}
}

func TestServersWithNoSocketAreTheDefaultServerAlone(t *testing.T) {
	servers := ServersFor(nil)
	if len(servers) != 1 || servers[0].Socket != "" {
		t.Fatalf("servers = %+v, want the default server only", servers)
	}
}

// With a real tmux: a pane on a -L socket of its own is listed.
func TestPanesOfARealTmuxOnASocketOfItsOwn(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	socket := fmt.Sprintf("mw-test-giveway-%d-%d", os.Getpid(), time.Now().UnixNano())
	if out, err := exec.Command("tmux", "-L", socket, "new-session", "-d", "-s", "mw-x_1", "sleep 60").CombinedOutput(); err != nil {
		t.Skipf("tmux cannot start a server here: %v %s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	got, err := ServersFor([]string{socket}).Panes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, pane := range got {
		if pane.Session == "mw-x_1" && pane.Pid > 0 && !pane.Dead {
			found = true
		}
	}
	if !found {
		t.Fatalf("panes = %+v, want the session mw-x_1 of the -L server", got)
	}
}

// noProcesses is a host with nothing running, so an On has only the panes to go by.
type noProcesses struct{}

func (noProcesses) List(context.Context) ([]application.GiveWayProcess, error) { return nil, nil }
func (noProcesses) Stop(context.Context, int, string) (bool, error)            { return false, nil }
func (noProcesses) Continue(context.Context, int, string) (bool, error)        { return false, nil }
func (noProcesses) Lower(context.Context, int, string) (bool, error)           { return false, nil }

// mw-2891blf.27: a host whose PATH lacks tmux (wsl.exe --exec runs mw with no
// login environment) has no panes to freeze; giveway.log says so, so that a
// give-way that froze nothing is not a silent one.
func TestPanesSaysWhenTmuxIsNotFound(t *testing.T) {
	ctx := context.Background()
	store := Store{Dir: t.TempDir()}
	missing := filepath.Join(t.TempDir(), "no-such-tmux")
	servers := ServersFor([]string{"mw-seat"}).WithLog(store)
	for i := range servers {
		servers[i].Program = missing
	}
	way := application.GiveWay{
		Processes: noProcesses{}, Panes: servers, Marker: store, Log: store, Host: "desktop",
		Now: func() time.Time { return time.Date(2026, 11, 13, 17, 0, 0, 0, time.UTC) },
	}
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatalf("a host with no tmux is no panes, not an error: %v", err)
	}
	lines, err := store.Tail(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	noted := 0
	for _, line := range lines {
		if strings.Contains(line, "tmux") && strings.Contains(line, "not found") && strings.Contains(line, "PATH") {
			noted++
		}
	}
	if noted != 1 {
		t.Fatalf("giveway.log = %q, want one note that tmux was not found on the PATH", lines)
	}
}

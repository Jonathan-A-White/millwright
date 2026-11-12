package application_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/domain"
)

func proc(pid, ppid int, state, start string, argv ...string) application.GiveWayProcess {
	return application.GiveWayProcess{Pid: pid, Ppid: ppid, Sid: ppid, State: state, Start: start, Argv: argv}
}

// aGiveWayWorld is a host with a tmux server (100), two Builder sessions, each
// with a pane shell, a harness, a heartbeat and a tool process (200s, 300s), a
// factory scope (400s), mw giveway itself (900) and a browser (500) that is
// nobody's to stop.
func aGiveWayWorld() (application.GiveWay, *apptest.FakeGiveWayHost, *bytes.Buffer, *time.Time) {
	host := &apptest.FakeGiveWayHost{
		Procs: []application.GiveWayProcess{
			proc(100, 1, "S", "t100", "tmux: server"),
			proc(200, 100, "S", "t200", "/bin/sh", "-c", "run"),
			proc(201, 200, "S", "t201", "claude", "--print"),
			proc(202, 200, "S", "t202", "/bin/mw", "next", "--heartbeat", "mw-a.1"),
			proc(203, 201, "R", "t203", "go", "test", "./..."),
			proc(300, 100, "S", "t300", "/bin/sh", "-c", "run"),
			proc(301, 300, "S", "t301", "claude", "--print"),
			proc(302, 300, "S", "t302", "/bin/mw", "next", "--heartbeat", "mw-b.2"),
			proc(500, 1, "S", "t500", "firefox"),
			proc(900, 1, "R", "t900", "/bin/mw", "giveway", "on"),
		},
		PaneList: []application.GiveWayPane{
			{Session: "mw-a_1", Pid: 200}, {Session: "mw-b_2", Pid: 300}, {Session: "0", Pid: 600},
		},
	}
	scope1 := proc(400, 1, "S", "t400", "sh", "-c", "land")
	scope1.Scope = true
	scope2 := proc(401, 400, "R", "t401", "go", "build")
	scope2.Scope = true
	host.Procs = append(host.Procs, scope1, scope2)
	now := time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC)
	var out bytes.Buffer
	return application.GiveWay{
		Processes: host, Panes: host, Marker: host, Log: host, Host: "desktop", Self: 900,
		Now: func() time.Time { return now }, Out: &out,
	}, host, &out, &now
}

func logEntry(t *testing.T, line string) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("a log line %q is not JSON: %v", line, err)
	}
	return entry
}

func TestGiveWayOnStopsEverySessionAndScopeProcessButNotTheHeartbeat(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	if err := way.On(context.Background(), application.GiveWayRequest{Notes: map[string]string{"windows_free_mb": "1234"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := host.Stopped(), []int{201, 203, 301, 400, 401}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stopped %v, want %v (not the heartbeats 202 and 302, the panes' own 200 and 300 (tmux resumes a stopped pane process and its whole group), the tmux server 100, mw giveway 900, the browser 500)", got, want)
	}
	if host.Marker == nil || host.Marker.By != "mw@desktop" || len(host.Marker.Stopped) != 5 || len(host.Marker.Sessions) != 2 {
		t.Fatalf("marker = %+v, want two sessions and five processes by mw@desktop", host.Marker)
	}
	if len(host.Lines) != 1 {
		t.Fatalf("log = %q, want one line", host.Lines)
	}
	entry := logEntry(t, host.Lines[0])
	if entry["event"] != "on" || entry["stopped"] != float64(5) || entry["sessions"] != float64(2) {
		t.Fatalf("log entry = %v", entry)
	}
	if _, ok := entry["took_ms"]; !ok {
		t.Fatalf("log entry has no took_ms: %v", entry)
	}
	if notes, _ := entry["notes"].(map[string]any); notes["windows_free_mb"] != "1234" {
		t.Fatalf("notes = %v, want windows_free_mb=1234", entry["notes"])
	}
	if i := slices.Index(host.Order, "marker written"); i != 0 {
		t.Fatalf("the marker was not written first: %v", host.Order)
	}
}

// A heartbeat that renews the lease through a child process (bd) must not wait
// for that child for good: what the heartbeat starts is not stopped either.
func TestGiveWayOnLeavesWhatTheHeartbeatStartsGoing(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	host.Procs = append(host.Procs, proc(210, 202, "S", "t210", "bd", "update", "mw-a.1"))
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if host.State(210) == "T" || host.State(202) == "T" {
		t.Fatalf("stopped %v: the heartbeat's child was stopped", host.Stopped())
	}
}

func TestGiveWayOnPassesOverTheTableUntilNothingIsNew(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	// 201 forks 204 in the instant it is stopped, and 204 forks 205.
	host.Forks = map[int][]application.GiveWayProcess{
		201: {proc(204, 201, "R", "t204", "sh", "-c", "late")},
		204: {proc(205, 204, "R", "t205", "sleep", "9")},
	}
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(host.Stopped(), 204) || !slices.Contains(host.Stopped(), 205) {
		t.Fatalf("stopped %v: a child that was mid-fork was missed", host.Stopped())
	}
}

func TestASecondGiveWayOnRenewsAndStopsOnlyWhatIsNew(t *testing.T) {
	way, host, _, now := aGiveWayWorld()
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	since := host.Marker.Since
	host.Procs = append(host.Procs, proc(206, 201, "R", "t206", "make"))
	host.Calls = nil
	*now = now.Add(5 * time.Minute)
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(host.Calls, []string{"stop 206"}) {
		t.Fatalf("calls = %v, want only stop 206", host.Calls)
	}
	if !host.Marker.Since.Equal(since) || !host.Marker.Seen.Equal(*now) || len(host.Marker.Stopped) != 6 {
		t.Fatalf("marker = %+v, want since kept, seen renewed, six processes held", host.Marker)
	}
	if entry := logEntry(t, host.Lines[1]); entry["event"] != "renew" || entry["stopped"] != float64(1) || entry["held"] != float64(6) {
		t.Fatalf("second log entry = %v, want a renew of one new process", entry)
	}
}

// mw-2891blf.1: nothing before the last stop is a call to the tracker, a mail,
// ssh or the network, and on goes through with a tracker that errors.
func TestGiveWayOnMakesNoCallToTheTrackerAndSucceedsWithOneThatFails(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	retry := &apptest.FakeGiveWayRetry{Host: host, Err: errors.New("beads: connection refused")}
	way.Retry = retry
	home := &apptest.FakeHomeFile{Text: "laptop 2026-11-11T00:00:00Z mw@laptop\n"}
	way.Home = home
	if err := way.On(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatalf("on with a failing tracker: %v", err)
	}
	for _, event := range host.Order {
		if strings.HasPrefix(event, "retry") {
			t.Fatalf("a tracker call was made during on: %v", host.Order)
		}
	}
}

func TestGiveWayOnTheHomeWithoutASessionIsRefusedAndWithOneIsNot(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	way.Host = "laptop"
	way.Home = &apptest.FakeHomeFile{Text: "laptop 2026-11-11T00:00:00Z mw@laptop\n"}
	err := way.On(context.Background(), application.GiveWayRequest{})
	if err == nil || !strings.Contains(err.Error(), "never gives way") || !strings.Contains(err.Error(), "--session") {
		t.Fatalf("on the home: %v, want a refusal saying the home never gives way", err)
	}
	if len(host.Stopped()) != 0 || host.Marker != nil || len(host.Lines) != 0 {
		t.Fatalf("a refused on stopped %v, wrote %+v and logged %q", host.Stopped(), host.Marker, host.Lines)
	}
	if err := way.On(context.Background(), application.GiveWayRequest{Sessions: []string{"mw-a_1"}}); err != nil {
		t.Fatalf("on the home with --session: %v", err)
	}
	if got, want := host.Stopped(), []int{201, 203}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stopped %v, want only the named session's %v", got, want)
	}
	if host.Marker != nil {
		t.Fatalf("a --session on wrote the marker %+v", host.Marker)
	}
	if entry := logEntry(t, host.Lines[0]); entry["event"] != "on" || entry["only_sessions"] == nil {
		t.Fatalf("entry = %v", entry)
	}
	// Off for the same session lets it go on and leaves the rest alone.
	if err := way.Off(context.Background(), application.GiveWayRequest{Sessions: []string{"mw-a_1"}}); err != nil {
		t.Fatal(err)
	}
	if len(host.Stopped()) != 0 {
		t.Fatalf("still stopped after off: %v", host.Stopped())
	}
}

func TestAHostWhoseHomeCannotBeToldDoesNotGiveWayWithoutASession(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	way.Home = &apptest.FakeHomeFile{Missing: true}
	if err := way.On(context.Background(), application.GiveWayRequest{}); err == nil {
		t.Fatal("on went ahead with no home file")
	}
	if len(host.Stopped()) != 0 {
		t.Fatalf("stopped %v", host.Stopped())
	}
}

func TestGiveWayOffContinuesWhatWasStoppedRemovesTheMarkerAndTimesItself(t *testing.T) {
	way, host, out, now := aGiveWayWorld()
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(7 * time.Minute)
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(host.Stopped()) != 0 || host.Marker != nil {
		t.Fatalf("after off: stopped %v, marker %+v", host.Stopped(), host.Marker)
	}
	entry := logEntry(t, host.Lines[1])
	if entry["event"] != "off" || entry["continued"] != float64(5) || entry["gave_way_ms"] != float64((7*time.Minute).Milliseconds()) {
		t.Fatalf("off entry = %v, want 7 continued after 7 minutes", entry)
	}
	if !strings.Contains(out.String(), "goes on") {
		t.Fatalf("said %q", out.String())
	}
}

func TestGiveWayOffRetriesTheStoryOfASessionWhosePaneDied(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	retry := &apptest.FakeGiveWayRetry{Host: host}
	way.Retry = retry
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	// Session mw-b_2 is killed while the host gives way: its pane is dead.
	host.PaneList = []application.GiveWayPane{{Session: "mw-a_1", Pid: 200}, {Session: "mw-b_2", Dead: true}}
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retry.Retried, []string{"mw-b.2"}) {
		t.Fatalf("retried %v, want only the dead session's story mw-b.2", retry.Retried)
	}
	var dead map[string]any
	for _, line := range host.Lines {
		if e := logEntry(t, line); e["event"] == "dead_pane" {
			dead = e
		}
	}
	if dead == nil || dead["session"] != "mw-b_2" || dead["story"] != "mw-b.2" {
		t.Fatalf("log = %q, want a dead_pane line for mw-b_2", host.Lines)
	}
	// The retry came after the continue: it is not timed.
	if slices.Index(host.Order, "retry mw-b.2") < slices.Index(host.Order, "cont 301") {
		t.Fatalf("the retry came before the continue: %v", host.Order)
	}
}

func TestGiveWayOffWithNoMarkerStillContinuesAStoppedBuilderProcess(t *testing.T) {
	way, host, out, _ := aGiveWayWorld()
	for i := range host.Procs {
		if host.Procs[i].Pid == 203 {
			host.Procs[i].State = "T"
		}
	}
	if err := way.Off(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(host.Stopped()) != 0 {
		t.Fatalf("still stopped: %v", host.Stopped())
	}
	if entry := logEntry(t, host.Lines[0]); entry["event"] != "off" || entry["no_marker"] != true || entry["continued"] != float64(1) {
		t.Fatalf("entry = %v", entry)
	}
	if !strings.Contains(out.String(), "no marker") {
		t.Fatalf("said %q, want it to say there was no marker", out.String())
	}
}

// mw-2891blf.1: a process that ended and whose pid another process now has is
// sent nothing: pid and start time both match, or no signal.
func TestGiveWayOffSendsNothingToAReusedPid(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	// 203 ended; 203 is now somebody's other process, running, in no session.
	host.Reuse(203, "other-start")
	host.Calls = nil
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range host.Calls {
		if strings.HasSuffix(call, " 203") {
			t.Fatalf("a signal went to the reused pid: %v", host.Calls)
		}
	}
}

func TestAStaleGiveWayMarkerIsLiftedByTheGateAndLoggedAsExpired(t *testing.T) {
	way, host, _, now := aGiveWayWorld()
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * time.Minute)
	if _, standing, err := way.Standing(ctx, true); err != nil || !standing {
		t.Fatalf("a marker renewed 10 minutes ago: standing=%v err=%v", standing, err)
	}
	*now = now.Add(6 * time.Minute)
	if _, standing, err := way.Standing(ctx, false); err != nil || standing || host.Marker == nil {
		t.Fatalf("a dry look at a stale marker: standing=%v err=%v marker=%v", standing, err, host.Marker)
	}
	if _, standing, err := way.Standing(ctx, true); err != nil || standing {
		t.Fatalf("a stale marker: standing=%v err=%v", standing, err)
	}
	if host.Marker != nil || len(host.Stopped()) != 0 {
		t.Fatalf("not lifted: marker %+v stopped %v", host.Marker, host.Stopped())
	}
	if entry := logEntry(t, host.Lines[len(host.Lines)-1]); entry["event"] != "expired" {
		t.Fatalf("last entry = %v, want event expired", entry)
	}
}

func TestGiveWayStatusAndMark(t *testing.T) {
	way, host, out, _ := aGiveWayWorld()
	ctx := context.Background()
	if err := way.Status(ctx); err != nil || !strings.Contains(out.String(), "not giving way") {
		t.Fatalf("status of an idle host: %v %q", err, out.String())
	}
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := way.Mark(ctx, map[string]string{"windows_free_mb": "1234"}); err != nil {
		t.Fatal(err)
	}
	if entry := logEntry(t, host.Lines[1]); entry["event"] != "mark" || entry["notes"].(map[string]any)["windows_free_mb"] != "1234" {
		t.Fatalf("mark entry = %v", entry)
	}
	out.Reset()
	if err := way.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "gives way since 2026-11-13T16:40:00Z") || !strings.Contains(out.String(), "2 sessions, 5 processes stopped") {
		t.Fatalf("status = %q", out.String())
	}
}

// The give-way use case holds no work tracker, mailbox or network port at all
// but the retry of off and the note for the home (Notes, mw-2891blf.3), which on
// writes only after its last stop (TestGiveWayNoteIsWrittenOnlyAfterTheLastStop):
// the second that matters (on) has nothing to call before then.
func TestGiveWayHoldsNoNetworkPortButTheRetryOfOff(t *testing.T) {
	allowed := map[string]bool{
		"Processes": true, "Panes": true, "Marker": true, "Log": true, "Load": true, "Home": true,
		"Host": true, "Retry": true, "Stale": true, "Self": true, "By": true, "Now": true, "Out": true,
		"Notes": true, "NoteTimeout": true,
	}
	typ := reflect.TypeOf(application.GiveWay{})
	for i := 0; i < typ.NumField(); i++ {
		if !allowed[typ.Field(i).Name] {
			t.Errorf("GiveWay has a field %s: a port that on might reach the network through must be added here on purpose", typ.Field(i).Name)
		}
	}
}

func TestDispatchOnAHostThatGivesWayClaimsNothingAndSaysSince(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, _, runner, _ := aFactory(t)
	way, host, _, now := aGiveWayWorld()
	var out bytes.Buffer
	ticks := &apptest.FakeTickLog{}
	dispatch.GiveWay, dispatch.Out, dispatch.Log = way, &out, ticks
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v", err)
	}
	if report.GivesWay == nil || len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("report = %+v: a host that gives way started something", report)
	}
	if d, _ := tracker.ShowStory(ctx, "mw-gq6.1"); d.Assignee != "" {
		t.Fatalf("claimed the story for %q", d.Assignee)
	}
	if !strings.Contains(out.String(), "gives way since 2026-11-13T16:40:00Z") {
		t.Fatalf("said %q", out.String())
	}
	if lines := ticks.Lines(); len(lines) != 1 || !strings.Contains(lines[0], "ok: gives way since") {
		t.Fatalf("dispatch log = %q", lines)
	}
	if host.Marker == nil {
		t.Fatal("the gate removed a fresh marker")
	}

	// Nobody renewed it: the next tick lifts it first, logs expired, and goes on.
	*now = now.Add(20 * time.Minute)
	report, err = dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching past the stale marker: %v", err)
	}
	if report.GivesWay != nil || len(report.Started) != 1 || host.Marker != nil {
		t.Fatalf("after expiry: gives way %v, started %d, marker %v", report.GivesWay, len(report.Started), host.Marker)
	}
	if entry := logEntry(t, host.Lines[len(host.Lines)-1]); entry["event"] != "expired" {
		t.Fatalf("last giveway.log line = %v", entry)
	}
}

// mw-2891blf.6: a mw dispatch or a mw millhand tick that is running when the host
// gives way (a systemd service cgroup: no pane, no run scope) is not stopped, for
// a stop in the middle of a sync could wedge the sync lock; it and its children
// are lowered (nice 19, ionice idle) at once, and status names them until they end.
func TestGiveWayOnLowersARunningDispatchAndATickAndTheirBuildsAndStopsNeitherOfThem(t *testing.T) {
	way, host, out, now := aGiveWayWorld()
	host.Procs = append(host.Procs,
		proc(800, 1, "S", "t800", "mw", "dispatch"),
		proc(801, 800, "S", "t801", "git", "pull"),
		proc(810, 1, "S", "t810", "/home/g/.local/bin/mw", "millhand", "tick"),
		proc(811, 810, "S", "t811", "go", "build", "./cmd/mw"),
		proc(812, 811, "R", "t812", "compile"),
	)
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{800, 801, 810, 811, 812} {
		if slices.Contains(host.Stopped(), pid) {
			t.Errorf("process %d was stopped: a running dispatch or tick and its children are lowered, never stopped", pid)
		}
	}
	got := slices.Clone(host.LoweredPids)
	slices.Sort(got)
	if want := []int{800, 801, 810, 811, 812}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lowered %v, want %v", got, want)
	}
	if host.Marker == nil || len(host.Marker.Lowered) != 5 {
		t.Fatalf("marker = %+v, want five lowered processes named", host.Marker)
	}
	if entry := logEntry(t, host.Lines[0]); entry["lowered"] != float64(5) {
		t.Fatalf("log entry = %v, want lowered 5", entry)
	}

	out.Reset()
	if err := way.Status(ctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"800", "mw dispatch", "810", "millhand tick", "811", "go build"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status = %q, want it to name %q as lowered", out.String(), want)
		}
	}

	// The tick ends and its build with it: status stops naming them.
	var left []application.GiveWayProcess
	for _, p := range host.Procs {
		if p.Pid < 810 || p.Pid > 812 {
			left = append(left, p)
		}
	}
	host.Procs = left
	out.Reset()
	if err := way.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "millhand tick") || !strings.Contains(out.String(), "mw dispatch") {
		t.Fatalf("status = %q, want only the dispatch still named", out.String())
	}

	// A child the dispatch starts later is lowered by the next on, once.
	host.Procs = append(host.Procs, proc(820, 800, "S", "t820", "make"))
	host.Calls = nil
	*now = now.Add(time.Minute)
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(host.Calls, []string{"lower 820"}) {
		t.Fatalf("calls = %v, want only lower 820", host.Calls)
	}
	if len(host.Marker.Lowered) != 3 {
		t.Fatalf("marker lowered = %+v, want the dispatch, its first child and the new one", host.Marker.Lowered)
	}
}

func TestGiveWayOffLeavesALoweredProcessAloneAndTheMarkerGoes(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	host.Procs = append(host.Procs, proc(800, 1, "S", "t800", "mw", "dispatch"))
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	host.Calls = nil
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range host.Calls {
		if strings.HasSuffix(call, " 800") {
			t.Fatalf("off signalled the dispatch: %v", host.Calls)
		}
	}
	if host.Marker != nil {
		t.Fatalf("marker stands after off: %+v", host.Marker)
	}
}

// A named session is the way to try give way on a busy host: it lowers and stops
// nothing outside the sessions.
func TestGiveWayOnForANamedSessionLowersNothingAndStopsNoOrphan(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	way.Home = &apptest.FakeHomeFile{Text: "laptop 2026-11-11T00:00:00Z mw@laptop\n"}
	way.Host = "laptop"
	orphan := proc(700, 1, "S", "t700", "sleep", "900")
	orphan.Sid, orphan.Cwd = 700, "/home/g/.mw-worktrees/mw-a.1/web"
	host.Procs = append(host.Procs, orphan, proc(800, 1, "S", "t800", "mw", "dispatch"))
	if err := way.On(context.Background(), application.GiveWayRequest{Sessions: []string{"mw-a_1"}}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(host.Stopped(), 700) || len(host.LoweredPids) != 0 {
		t.Fatalf("a named session touched the rest: stopped %v lowered %v", host.Stopped(), host.LoweredPids)
	}
}

// mw-2891blf.8: what a Builder's tool started and that was reparented to init in
// a session of its own (setsid), in no scope, is in no pane's tree: it is found by
// the worktree in its cwd, or by the story marker in its environment, and stopped.
func TestGiveWayOnStopsAnOrphanedBuilderProcessInASessionOfItsOwnAndOffContinuesIt(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	byCwd := proc(700, 1, "S", "t700", "node", "dev-server")
	byCwd.Sid, byCwd.Cwd = 700, "/home/g/.mw-worktrees/mw-a.1/web"
	byEnv := proc(710, 1, "S", "t710", "sleep", "900")
	byEnv.Sid, byEnv.Story = 710, "mw-b.2"
	childOfOrphan := proc(701, 700, "S", "t701", "esbuild")
	childOfOrphan.Sid = 700
	stranger := proc(720, 1, "S", "t720", "sleep", "900")
	stranger.Sid, stranger.Cwd = 720, "/home/g/projects/other"
	// A Mayor's pane (session "0", pane shell 600) is no Builder's: what sits under
	// it is left alone even with a worktree for its cwd.
	mayorShell := proc(600, 100, "S", "t600", "bash")
	underMayor := proc(601, 600, "S", "t601", "git", "status")
	underMayor.Cwd = "/home/g/.mw-worktrees/mw-a.1"
	// A person's terminal that sits in a worktree is not an orphan: its parent is a
	// terminal emulator, and it is not reparented. Nor is the user's systemd's child
	// the less an orphan: systemd adopts orphans.
	terminal := proc(730, 731, "S", "t730", "bash")
	terminal.Sid, terminal.Cwd = 730, "/home/g/.mw-worktrees/mw-a.1"
	emulator := proc(731, 1, "S", "t731", "/usr/bin/gnome-terminal")
	userManager := proc(740, 1, "S", "t740", "/lib/systemd/systemd", "--user")
	adopted := proc(741, 740, "S", "t741", "sleep", "900")
	adopted.Sid, adopted.Cwd = 741, "/home/g/.mw-worktrees/mw-a.1"
	host.Procs = append(host.Procs, byCwd, byEnv, childOfOrphan, stranger, mayorShell, underMayor, terminal, emulator, userManager, adopted)
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	stopped := host.Stopped()
	for _, pid := range []int{700, 701, 710, 741} {
		if !slices.Contains(stopped, pid) {
			t.Errorf("orphan %d was not stopped: %v", pid, stopped)
		}
	}
	for _, pid := range []int{720, 600, 601, 730, 731, 740} {
		if slices.Contains(stopped, pid) {
			t.Errorf("process %d was stopped though it is no Builder's orphan: %v", pid, stopped)
		}
	}
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(host.Stopped()) != 0 {
		t.Fatalf("still stopped after off: %v", host.Stopped())
	}
}

// With no marker (a crashed on, a hand-made stop) off still finds the orphan.
func TestGiveWayOffWithNoMarkerContinuesAStoppedOrphan(t *testing.T) {
	way, host, _, _ := aGiveWayWorld()
	orphan := proc(700, 1, "T", "t700", "node", "dev-server")
	orphan.Sid, orphan.Cwd = 700, "/home/g/.mw-worktrees/mw-a.1/web"
	host.Procs = append(host.Procs, orphan)
	if err := way.Off(context.Background(), application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(host.Stopped()) != 0 {
		t.Fatalf("still stopped: %v", host.Stopped())
	}
}

// lateGate is a give-way gate that finds the host free when the dispatch begins
// and giving way every time it is asked after: give way turned on while the
// dispatch was between its gate and its first session (mw-2891blf.16).
type lateGate struct {
	calls int
	since time.Time
}

func (g *lateGate) Standing(context.Context, bool) (application.GiveWayStanding, bool, error) {
	g.calls++
	if g.calls == 1 {
		return application.GiveWayStanding{}, false, nil
	}
	return application.GiveWayStanding{Since: g.since, By: "mw@laptop"}, true, nil
}

// mw-2891blf.16: a dispatch that passed its gate before give way turned on reads
// the marker again right before it starts a Builder session, and starts none: it
// leaves the story as it found it (ready, nobody's), and the run log says why.
func TestADispatchPastItsGateStartsNoSessionOnceTheHostGivesWay(t *testing.T) {
	ctx := context.Background()
	dispatch, tracker, worktrees, runner, _ := aFactory(t)
	var out bytes.Buffer
	ticks := &apptest.FakeTickLog{}
	gate := &lateGate{since: time.Date(2026, 11, 13, 16, 40, 0, 0, time.UTC)}
	dispatch.GiveWay, dispatch.Out, dispatch.Log = gate, &out, ticks
	tracker.AddStory("mw-gq6", domain.Story{ID: "mw-gq6.1", Title: "A story"})

	report, err := dispatch.Run(ctx)
	if err != nil {
		t.Fatalf("dispatching: %v\n%s", err, out.String())
	}
	if len(report.Started) != 0 || len(runner.Names()) != 0 {
		t.Fatalf("started %+v, sessions %v: a host that gives way started a Builder", report.Started, runner.Names())
	}
	if len(report.Failed) != 0 {
		t.Fatalf("giving way is not a failed start: %+v", report.Failed)
	}
	if report.GivesWay == nil {
		t.Fatalf("the report does not say the host gives way: %+v", report)
	}
	detail, err := tracker.ShowStory(ctx, "mw-gq6.1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Assignee != "" || detail.Held() || !detail.LeaseExpires.IsZero() {
		t.Fatalf("story left claimed or held: status %q assignee %q lease %v", detail.Status, detail.Assignee, detail.LeaseExpires)
	}
	for _, label := range detail.Labels {
		if label == application.RunState+":"+application.RunRunning {
			t.Fatalf("story reads as running, labels %v", detail.Labels)
		}
	}
	if len(worktrees.removed) == 0 {
		t.Fatalf("the worktree cut for the story was left behind")
	}
	if lines := ticks.Lines(); len(lines) != 1 || !strings.Contains(lines[0], "ok: gives way since 2026-11-13T16:40:00Z") {
		t.Fatalf("dispatch log = %q, want it to say the host gives way", lines)
	}
}

// mw-2891blf.16: give way lowers the scorer service and a running oneshot timer
// service of mw's (usage read, doctor, report send, postern mirror, mw-health,
// mw-mail-notify) as it lowers a running dispatch, stops none of them, and status
// names each; off signals none of them.
func TestGiveWayOnLowersTheScorerAndTheTimerServicesAndStopsNoneOfThem(t *testing.T) {
	way, host, out, _ := aGiveWayWorld()
	host.Procs = append(host.Procs,
		proc(700, 1, "S", "t700", "/home/g/.local/share/mw-scorer/venv/bin/python", "-m", "mw_scorer", "--host", "127.0.0.1", "--port", "8765"),
		proc(710, 1, "S", "t710", "/home/g/.local/bin/mw", "usage", "read"),
		proc(720, 1, "S", "t720", "/home/g/.local/bin/mw", "doctor"),
		proc(721, 720, "S", "t721", "bd", "list"),
		proc(730, 1, "S", "t730", "/home/g/.local/bin/mw", "report", "send"),
		proc(740, 1, "S", "t740", "mw", "postern", "mirror"),
		proc(750, 1, "S", "t750", "/bin/sh", "/home/g/.local/bin/mw-health"),
		proc(760, 1, "S", "t760", "/bin/bash", "/home/g/.local/bin/mw-mail-notify"),
		proc(761, 760, "S", "t761", "sleep", "60"),
		proc(770, 1, "S", "t770", "/usr/bin/python3", "-m", "http.server"),
	)
	ctx := context.Background()
	if err := way.On(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	lowered := slices.Clone(host.LoweredPids)
	slices.Sort(lowered)
	if want := []int{700, 710, 720, 721, 730, 740, 750, 760, 761}; !reflect.DeepEqual(lowered, want) {
		t.Fatalf("lowered %v, want %v (and not the unrelated python 770)", lowered, want)
	}
	for _, pid := range lowered {
		if slices.Contains(host.Stopped(), pid) {
			t.Errorf("process %d was stopped: a service is lowered, never stopped", pid)
		}
	}
	out.Reset()
	if err := way.Status(ctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mw_scorer", "usage read", "mw doctor", "report send", "postern mirror", "mw-health", "mw-mail-notify"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status = %q, want it to name %q as lowered", out.String(), want)
		}
	}
	host.Calls = nil
	if err := way.Off(ctx, application.GiveWayRequest{}); err != nil {
		t.Fatal(err)
	}
	for _, call := range host.Calls {
		for _, pid := range lowered {
			if strings.HasSuffix(call, fmt.Sprintf(" %d", pid)) {
				t.Fatalf("off signalled a lowered process: %v", host.Calls)
			}
		}
	}
}

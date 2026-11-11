package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// DefaultGiveWayStale is how long a give-way marker may go without being renewed
// before the next dispatch tick on the host lifts it (config giveway_stale).
const DefaultGiveWayStale = 15 * time.Minute

// giveWayPasses is the most times On looks at the process table again for what a
// stopped parent could not fork and a mid-fork child escaped: a bound, for a
// table that never stops changing.
const giveWayPasses = 50

// GiveWayProcess is one process of the host's table, as give way sees it. Start
// is the kernel's start time of the process (an opaque string): with Pid it
// names the process, so that a number a later process reuses is not mistaken
// for it.
type GiveWayProcess struct {
	Pid, Ppid, Sid int
	Start          string
	// State is the process's state letter: R, S, D, T, Z and the like.
	State string
	Argv  []string
	// Scope says the process sits in a systemd-run scope, which is where the
	// factory puts its landings, suites and mw-heavy commands, outside any pane.
	Scope bool
}

// stopped is whether the process is stopped already, or ended.
func (p GiveWayProcess) stopped() bool { return p.State == "T" || p.State == "t" }

func (p GiveWayProcess) gone() bool { return p.State == "Z" || p.State == "X" || p.State == "x" }

// GiveWayProcesses is the host's process table, and the two signals give way
// sends. Stop and Continue send only to the process whose pid AND start time
// match: the bool is whether a signal went.
type GiveWayProcesses interface {
	List(ctx context.Context) ([]GiveWayProcess, error)
	// Stop sends SIGSTOP.
	Stop(ctx context.Context, pid int, start string) (bool, error)
	// Continue sends SIGCONT.
	Continue(ctx context.Context, pid int, start string) (bool, error)
}

// GiveWayPane is one tmux pane: its session, the pid of the process it runs
// (0 once it is dead) and whether it is dead (remain-on-exit keeps it).
type GiveWayPane struct {
	Session string
	Pid     int
	Dead    bool
}

// GiveWayPanes lists every pane of the host's tmux server; no server is no panes.
type GiveWayPanes interface {
	Panes(ctx context.Context) ([]GiveWayPane, error)
}

// GiveWayStopped is one process a give-way stopped.
type GiveWayStopped struct {
	Pid   int    `json:"pid"`
	Start string `json:"start"`
	// Cmd is the first words of its command line, for a person reading the file.
	Cmd string `json:"cmd,omitempty"`
}

// GiveWaySession is one Builder session a give-way stopped: its name and, when a
// heartbeat showed it, the story it works.
type GiveWaySession struct {
	Name  string `json:"name"`
	Story string `json:"story,omitempty"`
}

// GiveWayMarker is giveway.json: the host gives way while it stands.
type GiveWayMarker struct {
	Since    time.Time        `json:"since"`
	By       string           `json:"by"`
	Seen     time.Time        `json:"seen"`
	Sessions []GiveWaySession `json:"sessions,omitempty"`
	Stopped  []GiveWayStopped `json:"stopped,omitempty"`
}

// GiveWayMarkers keeps the marker in the host's own state directory. Nothing
// here is a network call.
type GiveWayMarkers interface {
	// Read returns the marker, false when none stands.
	Read(ctx context.Context) (GiveWayMarker, bool, error)
	Write(ctx context.Context, marker GiveWayMarker) error
	// Remove takes the marker away; none standing is no error.
	Remove(ctx context.Context) error
}

// GiveWayLog is giveway.log, one JSON line to an event.
type GiveWayLog interface {
	Append(ctx context.Context, line string) error
	// Tail is the last n lines, oldest first.
	Tail(ctx context.Context, n int) ([]string, error)
}

// GiveWayEntry is one line of giveway.log.
type GiveWayEntry struct {
	Event string    `json:"event"`
	At    time.Time `json:"at"`
	By    string    `json:"by,omitempty"`
	// TookMs is, for on, from the start to the last stop; for off, from the start
	// to the last continue. GaveWayMs is how long the host gave way.
	TookMs    *int64 `json:"took_ms,omitempty"`
	GaveWayMs *int64 `json:"gave_way_ms,omitempty"`
	Sessions  *int   `json:"sessions,omitempty"`
	Stopped   *int   `json:"stopped,omitempty"`
	Held      *int   `json:"held,omitempty"`
	Continued *int   `json:"continued,omitempty"`
	// MemAvailableMB and Load1 are read from /proc after the last stop.
	MemAvailableMB *int              `json:"mem_available_mb,omitempty"`
	Load1          *float64          `json:"load1,omitempty"`
	Scoped         []string          `json:"only_sessions,omitempty"`
	Session        string            `json:"session,omitempty"`
	Story          string            `json:"story,omitempty"`
	Result         string            `json:"result,omitempty"`
	NoMarker       bool              `json:"no_marker,omitempty"`
	Notes          map[string]string `json:"notes,omitempty"`
}

// GiveWay is mw giveway: a host gives way to the person at its screen. On
// stops every process of every Builder session and of the factory's apart
// scopes (never a session's heartbeat, which keeps the story's lease), writes a
// marker the dispatch gate reads, and times itself in a log. Off lets them go
// on. Nothing before the last stop of On is a call to the beads, a mail, ssh or
// any network: the host's own files and /proc only.
type GiveWay struct {
	Processes GiveWayProcesses
	Panes     GiveWayPanes
	Marker    GiveWayMarkers
	Log       GiveWayLog
	// Load reads MemAvailable and load1 for the log; nil leaves them out.
	Load HostLoad
	// Home and Host refuse On on the home, which never gives way, except for the
	// named sessions. A nil Home makes no such check.
	Home HomeFile
	Host string
	// Retry is mw retry, for a session found dead when the host is let go on.
	Retry PauseRetry
	// Stale is how long a marker may go unrenewed (zero is DefaultGiveWayStale).
	Stale time.Duration
	// Self is the process give way runs as, which it never stops, nor any
	// process it descends from.
	Self int
	By   string
	Now  func() time.Time
	Out  io.Writer
}

// GiveWayRequest is what mw giveway on or off was asked.
type GiveWayRequest struct {
	// Sessions limits the work to those tmux sessions and leaves the marker and
	// the gate alone.
	Sessions []string
	// Notes are the --note k=v given, put in the log line.
	Notes map[string]string
}

func (g GiveWay) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g GiveWay) stale() time.Duration {
	if g.Stale > 0 {
		return g.Stale
	}
	return DefaultGiveWayStale
}

func (g GiveWay) say(format string, args ...any) {
	if g.Out != nil {
		fmt.Fprintf(g.Out, format+"\n", args...)
	}
}

func (g GiveWay) by() string {
	if g.By != "" {
		return g.By
	}
	return "mw@" + g.Host
}

func ms(d time.Duration) *int64 {
	v := d.Milliseconds()
	return &v
}

func intp(n int) *int { return &n }

// ErrGiveWayHome is what On says on the home without --session.
var ErrGiveWayHome = errors.New("the home never gives way")

// procKey names a process: its pid and its start time.
type procKey struct {
	pid   int
	start string
}

func keyOf(p GiveWayProcess) procKey { return procKey{p.Pid, p.Start} }

// heartbeatStory is the story a `<program> next --heartbeat <story>` command
// line names, and whether it is one.
func heartbeatStory(argv []string) (string, bool) {
	n := len(argv)
	if n >= 4 && argv[n-3] == "next" && argv[n-2] == "--heartbeat" && argv[n-1] != "" {
		return argv[n-1], true
	}
	return "", false
}

func isTmux(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	name := argv[0]
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.HasPrefix(name, "tmux")
}

// giveWayPlan is who is to give way, read from one look at the table.
type giveWayPlan struct {
	// targets are the processes to stop, parents before children.
	targets  []GiveWayProcess
	sessions []GiveWaySession
}

// plan picks the processes that give way from one listing: everything below the
// panes of the Builder sessions (or of the named sessions) and, when no sessions
// are named, everything in an apart scope; never a heartbeat, a tmux process
// or this process and the ones it descends from.
func (g GiveWay) plan(table []GiveWayProcess, panes []GiveWayPane, only []string) giveWayPlan {
	byPid := map[int]GiveWayProcess{}
	children := map[int][]int{}
	bySid := map[int][]int{}
	for _, p := range table {
		byPid[p.Pid] = p
		children[p.Ppid] = append(children[p.Ppid], p.Pid)
		bySid[p.Sid] = append(bySid[p.Sid], p.Pid)
	}
	spared := map[int]bool{}
	for pid := g.Self; pid > 1 && !spared[pid]; pid = byPid[pid].Ppid {
		spared[pid] = true
	}
	named := map[string]bool{}
	for _, name := range only {
		named[name] = true
	}
	panePids := map[string][]int{}
	var order []string
	for _, pane := range panes {
		if pane.Dead || pane.Pid <= 0 {
			continue
		}
		if len(named) > 0 && !named[pane.Session] {
			continue
		}
		if _, seen := panePids[pane.Session]; !seen {
			order = append(order, pane.Session)
		}
		panePids[pane.Session] = append(panePids[pane.Session], pane.Pid)
	}

	chosen := map[int]bool{}
	// A pane's own process is never stopped: tmux sees its child stop and sends
	// SIGCONT to the pane's whole process group, so the stop would undo itself and
	// everything below it with it. Its children are stopped, and it waits for them.
	roots := map[int]bool{}
	var plan giveWayPlan
	var walk func(pid int, into map[int]bool)
	walk = func(pid int, into map[int]bool) {
		if into[pid] {
			return
		}
		into[pid] = true
		for _, child := range children[pid] {
			walk(child, into)
		}
	}
	for _, name := range order {
		below := map[int]bool{}
		for _, root := range panePids[name] {
			roots[root] = true
			walk(root, below)
			// What a pane's command put out of its parentage (a child whose parent
			// ended) is still in the pane's own session.
			for _, pid := range bySid[root] {
				walk(pid, below)
			}
		}
		story, beating := "", false
		for pid := range below {
			if s, ok := heartbeatStory(byPid[pid].Argv); ok && byPid[pid].Pid != 0 {
				story, beating = s, true
			}
		}
		if len(named) == 0 && !beating {
			continue
		}
		plan.sessions = append(plan.sessions, GiveWaySession{Name: name, Story: story})
		for pid := range below {
			chosen[pid] = true
		}
	}
	if len(named) == 0 {
		for _, p := range table {
			if p.Scope {
				chosen[p.Pid] = true
			}
		}
	}
	// The heartbeat and what it starts keep going: a heartbeat that renews the
	// lease through a child (bd) would otherwise wait for that child for good.
	beating := map[int]bool{}
	for _, p := range table {
		if _, ok := heartbeatStory(p.Argv); ok {
			walk(p.Pid, beating)
		}
	}
	for _, p := range table {
		if !chosen[p.Pid] || spared[p.Pid] || roots[p.Pid] || beating[p.Pid] || isTmux(p.Argv) {
			continue
		}
		if _, ok := byPid[p.Pid]; !ok {
			continue
		}
		plan.targets = append(plan.targets, p)
	}
	// Parents before children, so that a stopped parent forks nothing more.
	depth := func(p GiveWayProcess) int {
		d := 0
		for pid, seen := p.Ppid, map[int]bool{}; pid > 1 && !seen[pid]; pid = byPid[pid].Ppid {
			seen[pid] = true
			d++
		}
		return d
	}
	sort.SliceStable(plan.targets, func(i, j int) bool { return depth(plan.targets[i]) < depth(plan.targets[j]) })
	return plan
}

func (g GiveWay) panes(ctx context.Context) ([]GiveWayPane, error) {
	if g.Panes == nil {
		return nil, nil
	}
	return g.Panes.Panes(ctx)
}

// On makes the host give way. It refuses on the home unless sessions are named.
func (g GiveWay) On(ctx context.Context, req GiveWayRequest) error {
	start := g.now()
	if len(req.Sessions) == 0 && g.Home != nil {
		home, err := IsHome(ctx, g.Home, g.Host)
		switch {
		case err != nil:
			return fmt.Errorf("giving way on %s refused: %v; the home never gives way, and which host is home cannot be told (--session <name> limits it to one tmux session)", g.Host, err)
		case home:
			return fmt.Errorf("giving way on %s refused: %s is the home, which serves the Mayor and the beads and never gives way (--session <name> limits it to one tmux session)", g.Host, g.Host)
		}
	}
	scoped := len(req.Sessions) > 0

	var marker GiveWayMarker
	renewing := false
	if !scoped {
		old, there, err := g.Marker.Read(ctx)
		if err != nil {
			return fmt.Errorf("reading the give-way marker: %w", err)
		}
		marker = GiveWayMarker{Since: start, By: g.by(), Seen: start}
		if there {
			marker, renewing = old, true
			marker.Seen = start
		}
		if err := g.Marker.Write(ctx, marker); err != nil {
			return fmt.Errorf("writing the give-way marker: %w", err)
		}
	}

	panes, err := g.panes(ctx)
	if err != nil {
		return fmt.Errorf("listing the tmux panes: %w", err)
	}
	signalled := map[procKey]GiveWayStopped{}
	sessions := map[string]GiveWaySession{}
	lastStop := start
	for pass := 0; pass < giveWayPasses; pass++ {
		table, err := g.Processes.List(ctx)
		if err != nil {
			return fmt.Errorf("reading the process table: %w", err)
		}
		plan := g.plan(table, panes, req.Sessions)
		for _, s := range plan.sessions {
			sessions[s.Name] = s
		}
		fresh := 0
		for _, p := range plan.targets {
			if _, done := signalled[keyOf(p)]; done || p.stopped() || p.gone() {
				continue
			}
			sent, err := g.Processes.Stop(ctx, p.Pid, p.Start)
			if err != nil {
				return fmt.Errorf("stopping process %d: %w", p.Pid, err)
			}
			if sent {
				signalled[keyOf(p)] = GiveWayStopped{Pid: p.Pid, Start: p.Start, Cmd: commandHead(p.Argv)}
				lastStop = g.now()
				fresh++
			}
		}
		if fresh == 0 {
			break
		}
	}
	took := lastStop.Sub(start)

	if !scoped {
		table, _ := g.Processes.List(ctx)
		alive := map[procKey]bool{}
		for _, p := range table {
			if p.stopped() {
				alive[keyOf(p)] = true
			}
		}
		var kept []GiveWayStopped
		for _, s := range marker.Stopped {
			if alive[procKey{s.Pid, s.Start}] {
				if _, again := signalled[procKey{s.Pid, s.Start}]; !again {
					kept = append(kept, s)
				}
			}
		}
		keys := make([]procKey, 0, len(signalled))
		for k := range signalled {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].pid < keys[j].pid })
		for _, k := range keys {
			kept = append(kept, signalled[k])
		}
		marker.Stopped = kept
		names := make([]string, 0, len(sessions))
		for n := range sessions {
			names = append(names, n)
		}
		sort.Strings(names)
		known := map[string]bool{}
		for _, s := range marker.Sessions {
			known[s.Name] = true
		}
		for _, n := range names {
			if !known[n] {
				marker.Sessions = append(marker.Sessions, sessions[n])
			}
		}
		if err := g.Marker.Write(ctx, marker); err != nil {
			return fmt.Errorf("writing the give-way marker: %w", err)
		}
	}

	event := "on"
	if renewing {
		event = "renew"
	}
	entry := GiveWayEntry{
		Event: event, At: start.UTC(), By: g.by(), TookMs: ms(took),
		Sessions: intp(len(sessions)), Stopped: intp(len(signalled)), Notes: req.Notes, Scoped: req.Sessions,
	}
	if !scoped {
		entry.Held = intp(len(marker.Stopped))
		entry.Sessions = intp(len(marker.Sessions))
	}
	g.reading(ctx, &entry)
	if err := g.append(ctx, entry); err != nil {
		return err
	}
	if scoped {
		g.say("%s gives way for %s: %d processes stopped in %d ms", g.Host, strings.Join(req.Sessions, ", "), len(signalled), took.Milliseconds())
	} else {
		g.say("%s gives way since %s: %d processes stopped now (%d held), %d sessions, in %d ms", g.Host,
			marker.Since.UTC().Format(time.RFC3339), len(signalled), len(marker.Stopped), len(marker.Sessions), took.Milliseconds())
	}
	return nil
}

func commandHead(argv []string) string {
	line := strings.Join(argv, " ")
	if len(line) > 80 {
		line = line[:80]
	}
	return line
}

func (g GiveWay) reading(ctx context.Context, entry *GiveWayEntry) {
	if g.Load == nil {
		return
	}
	reading, err := g.Load.Load(ctx)
	if err != nil {
		return
	}
	if reading.MemKnown {
		entry.MemAvailableMB = intp(int(reading.MemAvailableMB))
	}
	load := reading.Load
	entry.Load1 = &load
}

func (g GiveWay) append(ctx context.Context, entry GiveWayEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := g.Log.Append(ctx, string(line)); err != nil {
		return fmt.Errorf("appending to giveway.log: %w", err)
	}
	return nil
}

// Off lets the host go on: every process the marker names (by pid and start
// time) and any process of a Builder session still stopped gets SIGCONT, the
// marker goes, and the log says how long it took and how long the host gave way.
// Then, untimed, a session whose pane died is retried.
func (g GiveWay) Off(ctx context.Context, req GiveWayRequest) error {
	return g.off(ctx, req, "off")
}

func (g GiveWay) off(ctx context.Context, req GiveWayRequest, event string) error {
	start := g.now()
	scoped := len(req.Sessions) > 0
	var marker GiveWayMarker
	there := false
	if !scoped {
		var err error
		if marker, there, err = g.Marker.Read(ctx); err != nil {
			return fmt.Errorf("reading the give-way marker: %w", err)
		}
	}
	panes, err := g.panes(ctx)
	if err != nil {
		return fmt.Errorf("listing the tmux panes: %w", err)
	}
	table, err := g.Processes.List(ctx)
	if err != nil {
		return fmt.Errorf("reading the process table: %w", err)
	}
	plan := g.plan(table, panes, req.Sessions)
	want := map[procKey]GiveWayProcess{}
	for _, p := range plan.targets {
		if p.stopped() {
			want[keyOf(p)] = p
		}
	}
	byKey := map[procKey]GiveWayProcess{}
	for _, p := range table {
		byKey[keyOf(p)] = p
	}
	for _, s := range marker.Stopped {
		if p, ok := byKey[procKey{s.Pid, s.Start}]; ok && p.stopped() {
			want[keyOf(p)] = p
		}
	}
	keys := make([]procKey, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	// Children first, the reverse of the stop.
	sort.Slice(keys, func(i, j int) bool { return keys[i].pid > keys[j].pid })
	continued := 0
	lastCont := start
	for _, k := range keys {
		sent, err := g.Processes.Continue(ctx, k.pid, k.start)
		if err != nil {
			return fmt.Errorf("continuing process %d: %w", k.pid, err)
		}
		if sent {
			continued++
			lastCont = g.now()
		}
	}
	if there {
		if err := g.Marker.Remove(ctx); err != nil {
			return fmt.Errorf("removing the give-way marker: %w", err)
		}
	}
	took := lastCont.Sub(start)
	entry := GiveWayEntry{
		Event: event, At: start.UTC(), By: g.by(), TookMs: ms(took), Continued: intp(continued),
		Notes: req.Notes, Scoped: req.Sessions, NoMarker: !scoped && !there,
	}
	if there {
		entry.GaveWayMs = ms(start.Sub(marker.Since))
	}
	if err := g.append(ctx, entry); err != nil {
		return err
	}
	switch {
	case scoped:
		g.say("%s goes on for %s: %d processes continued in %d ms", g.Host, strings.Join(req.Sessions, ", "), continued, took.Milliseconds())
	case !there:
		g.say("%s was not giving way (no marker); %d stopped Builder processes continued", g.Host, continued)
	default:
		g.say("%s goes on: %d processes continued in %d ms after giving way for %s", g.Host, continued, took.Milliseconds(),
			start.Sub(marker.Since).Round(time.Second))
	}
	g.retryDead(ctx, marker)
	return nil
}

// retryDead says of each session the marker stopped whose pane is dead now that
// it is, and does for it what mw resume does for a session that is gone: mw
// retry. It is not timed, and a failure of it is the log's word and no error.
func (g GiveWay) retryDead(ctx context.Context, marker GiveWayMarker) {
	if len(marker.Sessions) == 0 {
		return
	}
	panes, err := g.panes(ctx)
	if err != nil {
		return
	}
	living := map[string]bool{}
	for _, pane := range panes {
		if !pane.Dead {
			living[pane.Session] = true
		}
	}
	for _, s := range marker.Sessions {
		if living[s.Name] {
			continue
		}
		entry := GiveWayEntry{Event: "dead_pane", At: g.now().UTC(), By: g.by(), Session: s.Name, Story: s.Story}
		switch {
		case s.Story == "" || g.Retry == nil:
			entry.Result = "no story known for this session: nothing retried"
		default:
			said, err := g.Retry.Retry(ctx, s.Story, false)
			if err != nil {
				entry.Result = "mw retry failed: " + oneLine(err.Error())
			} else {
				entry.Result = "mw retry: " + oneLine(said)
			}
		}
		_ = g.append(ctx, entry)
		g.say("session %s has a dead pane: %s", s.Name, entry.Result)
	}
}

// GiveWayStanding is what the dispatch gate finds.
type GiveWayStanding struct {
	Since time.Time
	By    string
}

// GiveWayGate is what Dispatch asks before it does anything.
type GiveWayGate interface {
	// Standing says whether the host gives way. A marker nobody renewed is lifted
	// first (event expired) unless lift is false, as for a dry run.
	Standing(ctx context.Context, lift bool) (GiveWayStanding, bool, error)
}

var _ GiveWayGate = GiveWay{}

// Standing implements GiveWayGate.
func (g GiveWay) Standing(ctx context.Context, lift bool) (GiveWayStanding, bool, error) {
	marker, there, err := g.Marker.Read(ctx)
	if err != nil {
		return GiveWayStanding{}, false, err
	}
	if !there {
		return GiveWayStanding{}, false, nil
	}
	if g.now().Sub(marker.Seen) > g.stale() {
		if !lift {
			return GiveWayStanding{}, false, nil
		}
		if err := g.off(ctx, GiveWayRequest{Notes: map[string]string{"unrenewed_for": g.now().Sub(marker.Seen).Round(time.Second).String()}}, "expired"); err != nil {
			return GiveWayStanding{}, false, fmt.Errorf("lifting the stale give-way marker: %w", err)
		}
		return GiveWayStanding{}, false, nil
	}
	return GiveWayStanding{Since: marker.Since, By: marker.By}, true, nil
}

// Mark appends a line of event mark with the notes given: the Windows side puts
// its own measurements in the same log.
func (g GiveWay) Mark(ctx context.Context, notes map[string]string) error {
	return g.append(ctx, GiveWayEntry{Event: "mark", At: g.now().UTC(), By: g.by(), Notes: notes})
}

// Status says whether the host gives way, and the last five lines of the log.
func (g GiveWay) Status(ctx context.Context) error {
	marker, there, err := g.Marker.Read(ctx)
	if err != nil {
		return fmt.Errorf("reading the give-way marker: %w", err)
	}
	now := g.now()
	if !there {
		g.say("not giving way")
	} else {
		g.say("gives way since %s (by %s, last seen %s ago): %d sessions, %d processes stopped",
			marker.Since.UTC().Format(time.RFC3339), marker.By, now.Sub(marker.Seen).Round(time.Second), len(marker.Sessions), len(marker.Stopped))
		if now.Sub(marker.Seen) > g.stale() {
			g.say("the marker is stale (not renewed for over %s): the next dispatch tick lifts it", g.stale())
		}
	}
	lines, err := g.Log.Tail(ctx, 5)
	if err != nil {
		return fmt.Errorf("reading giveway.log: %w", err)
	}
	if len(lines) == 0 {
		g.say("giveway.log: no lines")
		return nil
	}
	g.say("last %d lines of giveway.log:", len(lines))
	for _, line := range lines {
		g.say("%s", line)
	}
	return nil
}

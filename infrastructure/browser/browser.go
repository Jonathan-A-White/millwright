// Package browser is mw browser: a Chromium on a VNC display of the Laptop that
// he signs into himself over his ssh tunnel, that a script then drives through
// the DevTools protocol, and that forgets the session when closed.
//
// Everything Chromium keeps (cookies, local storage, its logs, the logs of the
// VNC display) lives in one new temp directory of mode 700, the profile, and
// mw browser close deletes it. The only other file mw writes is the state
// file, which names the two process ids, the ports and the profile's path, never
// a page, a cookie or a password. Password saving is turned off in the profile.
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/atomicfile"
)

const (
	// VNCName and ChromiumName name the two programs mw browser starts.
	VNCName      = "Xtigervnc"
	ChromiumName = "chromium"
	// StateFile is the file in the state directory naming what is open.
	StateFile = "browser.json"
	// ProfilePrefix begins the name of every profile directory mw browser makes.
	ProfilePrefix = "mw-browser-profile-"
	// DefaultDisplay, DefaultDevToolsPort and DefaultGeometry are the display,
	// the DevTools port and the screen size when nothing else is asked.
	DefaultDisplay      = 5
	DefaultDevToolsPort = 9222
	DefaultGeometry     = "1280x800"
)

// Program is one program to start: its output goes to Log.
type Program struct {
	Name string
	Path string
	Args []string
	Env  []string
	Log  string
}

// Launcher starts and stops the programs. ExecLauncher is the real one; Fake
// stands in for it in tests.
type Launcher interface {
	// Start starts p in a session and process group of its own and returns
	// its pid without waiting for it.
	Start(p Program) (int, error)
	// Running reports whether pid is alive and its command line carries
	// marker, so a pid reused by another program after a reboot is not ours.
	Running(pid int, marker string) bool
	// Stop ends pid's process group: politely, then by force.
	Stop(pid int) error
}

// Browser opens, reads and closes the one browser of this host.
type Browser struct {
	Launcher Launcher
	// StateDir holds StateFile (~/.local/state/mw/browser).
	StateDir string
	// TempRoot is where the profile is made; "" is the system's temp dir.
	TempRoot string
	// Display is the X display number; VNCPort is 0 for 5900+Display.
	Display      int
	VNCPort      int
	DevToolsPort int
	// Xtigervnc and Chromium are the programs' paths.
	Xtigervnc string
	Chromium  string
	Geometry  string
	// Ready is how long each program is given to answer; 0 is a minute.
	Ready  time.Duration
	Client *http.Client
}

// State is what the state file keeps: no page, no cookie, no password.
type State struct {
	Display      int       `json:"display"`
	VNCPort      int       `json:"vnc_port"`
	DevToolsPort int       `json:"devtools_port"`
	Profile      string    `json:"profile"`
	VNCPid       int       `json:"vnc_pid"`
	ChromiumPid  int       `json:"chromium_pid"`
	Started      time.Time `json:"started"`
}

// Endpoint is the address chromium.connectOverCDP is given.
func Endpoint(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// VNCAddress is the address his VNC app connects to through his tunnel.
func (s State) VNCAddress() string { return fmt.Sprintf("127.0.0.1:%d", s.VNCPort) }

// Opened is what mw browser open started.
type Opened struct {
	State State
	URL   string
}

// Say is what mw browser open prints: the one tunnel line, the VNC address,
// the endpoint and how to forget it all. sshHost is the name his phone's ssh
// knows this host by.
func (o Opened) Say(sshHost string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "mw browser: open on display :%d at %s\n", o.State.Display, o.URL)
	fmt.Fprintf(&b, "1. On your phone: ssh -L %d:127.0.0.1:%d %s\n", o.State.VNCPort, o.State.VNCPort, sshHost)
	fmt.Fprintf(&b, "2. In the VNC app: %s (no VNC password: only your ssh reaches it)\n", o.State.VNCAddress())
	b.WriteString("Sign in yourself; the browser saves no password, and its profile is deleted on close.\n")
	fmt.Fprintf(&b, "A script connects to %s (chromium.connectOverCDP)\n", Endpoint(o.State.DevToolsPort))
	b.WriteString("mw browser close stops it and deletes the profile, cookies and all.")
	return b.String()
}

// Tab is one page Chromium shows.
type Tab struct {
	Title string
	URL   string
}

// Status is what mw browser status reads.
type Status struct {
	Open       bool
	State      State
	VNCUp      bool
	ChromiumUp bool
	Tabs       []Tab
	Endpoint   string
	WebSocket  string
	// TabsErr is why the tabs could not be read, when Chromium runs.
	TabsErr error
}

// Up is whether both programs run.
func (s Status) Up() bool { return s.Open && s.VNCUp && s.ChromiumUp }

func upDown(up bool) string {
	if up {
		return "up"
	}
	return "down"
}

func (s Status) String() string {
	if !s.Open {
		return "mw browser: not open"
	}
	var b strings.Builder
	if s.Up() {
		fmt.Fprintf(&b, "mw browser: up on display :%d, VNC at %s, Chromium pid %d, profile %s\n", s.State.Display, s.State.VNCAddress(), s.State.ChromiumPid, s.State.Profile)
	} else {
		fmt.Fprintf(&b, "mw browser: down (VNC display %s, Chromium %s); mw browser close deletes the profile %s\n", upDown(s.VNCUp), upDown(s.ChromiumUp), s.State.Profile)
	}
	if s.TabsErr != nil {
		fmt.Fprintf(&b, "tabs: cannot tell: %v\n", s.TabsErr)
	}
	for _, t := range s.Tabs {
		if t.Title != "" {
			fmt.Fprintf(&b, "tab: %s  (%s)\n", t.URL, t.Title)
		} else {
			fmt.Fprintf(&b, "tab: %s\n", t.URL)
		}
	}
	fmt.Fprintf(&b, "CDP endpoint: %s (chromium.connectOverCDP)", s.Endpoint)
	if s.WebSocket != "" {
		fmt.Fprintf(&b, "\nbrowser websocket: %s", s.WebSocket)
	}
	return b.String()
}

// Closed is what mw browser close stopped and deleted.
type Closed struct {
	WasOpen bool
	Stopped []string
	Removed []string
}

func (c Closed) String() string {
	var b strings.Builder
	if c.WasOpen {
		b.WriteString("mw browser: closed")
	} else {
		b.WriteString("mw browser: was not open")
	}
	if len(c.Stopped) > 0 {
		fmt.Fprintf(&b, "; stopped %s", strings.Join(c.Stopped, " and "))
	}
	for _, r := range c.Removed {
		fmt.Fprintf(&b, "\ndeleted the profile %s", r)
	}
	return b.String()
}

func (b Browser) vncPort() int {
	if b.VNCPort != 0 {
		return b.VNCPort
	}
	return 5900 + b.Display
}

func (b Browser) ready() time.Duration {
	if b.Ready > 0 {
		return b.Ready
	}
	return time.Minute
}

func (b Browser) tempRoot() string {
	if b.TempRoot != "" {
		return b.TempRoot
	}
	return os.TempDir()
}

func (b Browser) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (b Browser) statePath() string { return filepath.Join(b.StateDir, StateFile) }

func (b Browser) readState() (State, bool, error) {
	raw, err := os.ReadFile(b.statePath())
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, false, fmt.Errorf("reading %s: %w", b.statePath(), err)
	}
	return s, true, nil
}

func (b Browser) writeState(s State) error {
	if err := os.MkdirAll(b.StateDir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(b.statePath(), append(raw, '\n'), 0o600)
}

func (b Browser) vncRunning(s State) bool {
	return s.VNCPid > 0 && b.Launcher.Running(s.VNCPid, VNCName)
}

func (b Browser) chromiumRunning(s State) bool {
	return s.ChromiumPid > 0 && s.Profile != "" && b.Launcher.Running(s.ChromiumPid, s.Profile)
}

// portFree reports an error when something already listens on 127.0.0.1:port.
func portFree(port int, what string) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("127.0.0.1:%d (%s) is already in use: %w", port, what, err)
	}
	return ln.Close()
}

// noPasswords is the profile's starting Preferences: Chromium offers to save
// no password, address or card.
const noPasswords = `{"credentials_enable_service":false,"profile":{"password_manager_enabled":false},"autofill":{"profile_enabled":false,"credit_card_enabled":false}}` + "\n"

// Open starts the VNC display and a Chromium on it at url, on a new profile.
// It refuses when a browser is already open; a state left by a browser whose
// programs both died is forgotten first. When a step fails, whatever it
// started is stopped and the profile deleted.
func (b Browser) Open(ctx context.Context, url string) (Opened, error) {
	old, ok, err := b.readState()
	if err != nil {
		return Opened{}, err
	}
	if ok {
		if b.vncRunning(old) || b.chromiumRunning(old) {
			return Opened{}, fmt.Errorf("mw browser is already open on display :%d (Chromium pid %d): mw browser status reads it, mw browser close ends it", old.Display, old.ChromiumPid)
		}
		if _, err := b.forget(old); err != nil {
			return Opened{}, err
		}
	}
	s := State{Display: b.Display, VNCPort: b.vncPort(), DevToolsPort: b.DevToolsPort, Started: time.Now().UTC()}
	if err := portFree(s.VNCPort, fmt.Sprintf("display :%d's VNC port; a VNC server started by hand? pkill -x Xtigervnc", s.Display)); err != nil {
		return Opened{}, err
	}
	if err := portFree(s.DevToolsPort, "the DevTools port; another browser with remote debugging?"); err != nil {
		return Opened{}, err
	}
	profile, err := os.MkdirTemp(b.tempRoot(), ProfilePrefix)
	if err != nil {
		return Opened{}, err
	}
	s.Profile = profile
	fail := func(err error) (Opened, error) {
		_, _ = b.forget(s)
		return Opened{}, err
	}
	if err := os.Chmod(profile, 0o700); err != nil {
		return fail(err)
	}
	if err := os.Mkdir(filepath.Join(profile, "Default"), 0o700); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "Default", "Preferences"), []byte(noPasswords), 0o600); err != nil {
		return fail(err)
	}
	if err := b.writeState(s); err != nil {
		return fail(err)
	}

	vnc := b.vncProgram(s)
	s.VNCPid, err = b.Launcher.Start(vnc)
	if err != nil {
		return fail(fmt.Errorf("starting %s: %w", b.Xtigervnc, err))
	}
	if err := b.writeState(s); err != nil {
		return fail(err)
	}
	if err := b.await(ctx, func() bool { return dials(s.VNCAddress()) }); err != nil {
		return fail(fmt.Errorf("the VNC display never listened on %s: %w%s", s.VNCAddress(), err, logTail(vnc.Log)))
	}

	chromium := b.chromiumProgram(s, url)
	s.ChromiumPid, err = b.Launcher.Start(chromium)
	if err != nil {
		return fail(fmt.Errorf("starting %s: %w", b.Chromium, err))
	}
	if err := b.writeState(s); err != nil {
		return fail(err)
	}
	if err := b.await(ctx, func() bool { _, err := b.version(ctx, s); return err == nil }); err != nil {
		return fail(fmt.Errorf("Chromium's DevTools never answered at %s: %w%s", Endpoint(s.DevToolsPort), err, logTail(chromium.Log)))
	}
	return Opened{State: s, URL: url}, nil
}

func (b Browser) vncProgram(s State) Program {
	geometry := b.Geometry
	if geometry == "" {
		geometry = DefaultGeometry
	}
	return Program{
		Name: VNCName,
		Path: b.Xtigervnc,
		Args: []string{
			fmt.Sprintf(":%d", s.Display),
			"-localhost",
			"-interface", "127.0.0.1",
			"-rfbport", fmt.Sprint(s.VNCPort),
			"-SecurityTypes", "None",
			"-nolisten", "tcp",
			"-geometry", geometry,
			"-depth", "24",
		},
		Log: filepath.Join(s.Profile, "xtigervnc.log"),
	}
}

func (b Browser) chromiumProgram(s State, url string) Program {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "DISPLAY=") || strings.HasPrefix(kv, "WAYLAND_DISPLAY=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, fmt.Sprintf("DISPLAY=:%d", s.Display))
	w, h, _ := strings.Cut(b.Geometry, "x")
	if b.Geometry == "" {
		w, h, _ = strings.Cut(DefaultGeometry, "x")
	}
	return Program{
		Name: ChromiumName,
		Path: b.Chromium,
		Args: []string{
			"--user-data-dir=" + s.Profile,
			"--remote-debugging-address=127.0.0.1",
			fmt.Sprintf("--remote-debugging-port=%d", s.DevToolsPort),
			"--ozone-platform=x11",
			"--no-first-run",
			"--no-default-browser-check",
			"--disable-sync",
			"--window-position=0,0",
			fmt.Sprintf("--window-size=%s,%s", w, h),
			url,
		},
		Env: env,
		Log: filepath.Join(s.Profile, "chromium.log"),
	}
}

func dials(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// await asks ok every tenth of a second until it is true or Ready runs out.
func (b Browser) await(ctx context.Context, ok func() bool) error {
	deadline := time.Now().Add(b.ready())
	for {
		if ok() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no answer in %s", b.ready())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// logTail is the last lines of a program's log, for an error, read before the
// profile holding it is deleted.
func logTail(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return "; its log ends:\n" + strings.Join(lines, "\n")
}

func (b Browser) getJSON(ctx context.Context, s State, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Endpoint(s.DevToolsPort)+path, nil)
	if err != nil {
		return err
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

func (b Browser) version(ctx context.Context, s State) (string, error) {
	var v struct {
		WebSocket string `json:"webSocketDebuggerUrl"`
	}
	err := b.getJSON(ctx, s, "/json/version", &v)
	return v.WebSocket, err
}

func (b Browser) tabs(ctx context.Context, s State) ([]Tab, error) {
	var list []struct {
		Type  string `json:"type"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if err := b.getJSON(ctx, s, "/json/list", &list); err != nil {
		return nil, err
	}
	var tabs []Tab
	for _, t := range list {
		if t.Type == "page" {
			tabs = append(tabs, Tab{Title: t.Title, URL: t.URL})
		}
	}
	return tabs, nil
}

// Status reads whether the browser is open and up, its tabs and its endpoint.
func (b Browser) Status(ctx context.Context) (Status, error) {
	s, ok, err := b.readState()
	if err != nil || !ok {
		return Status{}, err
	}
	st := Status{Open: true, State: s, VNCUp: b.vncRunning(s), ChromiumUp: b.chromiumRunning(s), Endpoint: Endpoint(s.DevToolsPort)}
	if st.ChromiumUp {
		st.Tabs, st.TabsErr = b.tabs(ctx, s)
		st.WebSocket, _ = b.version(ctx, s)
	}
	return st, nil
}

// Close stops Chromium and the VNC display and deletes the profile and the
// state file. With nothing open it still deletes any profile left behind.
func (b Browser) Close(ctx context.Context) (Closed, error) {
	s, ok, err := b.readState()
	if err != nil {
		return Closed{}, err
	}
	if !ok {
		removed, err := b.sweep()
		return Closed{Removed: removed}, err
	}
	c, err := b.forget(s)
	c.WasOpen = true
	return c, err
}

// forget stops what s started, deletes its profile and any other left behind,
// and removes the state file.
func (b Browser) forget(s State) (Closed, error) {
	var c Closed
	var errs []error
	if b.chromiumRunning(s) {
		if err := b.Launcher.Stop(s.ChromiumPid); err != nil {
			errs = append(errs, fmt.Errorf("stopping Chromium pid %d: %w", s.ChromiumPid, err))
		} else {
			c.Stopped = append(c.Stopped, fmt.Sprintf("Chromium (pid %d)", s.ChromiumPid))
		}
	}
	if b.vncRunning(s) {
		if err := b.Launcher.Stop(s.VNCPid); err != nil {
			errs = append(errs, fmt.Errorf("stopping the VNC display pid %d: %w", s.VNCPid, err))
		} else {
			c.Stopped = append(c.Stopped, fmt.Sprintf("the VNC display :%d (pid %d)", s.Display, s.VNCPid))
		}
	}
	if s.Profile != "" && strings.HasPrefix(filepath.Base(s.Profile), ProfilePrefix) {
		if _, err := os.Lstat(s.Profile); err == nil {
			if err := os.RemoveAll(s.Profile); err != nil {
				errs = append(errs, err)
			} else {
				c.Removed = append(c.Removed, s.Profile)
			}
		}
	}
	more, err := b.sweep()
	c.Removed = append(c.Removed, more...)
	if err != nil {
		errs = append(errs, err)
	}
	// The state file goes only when all is gone, so a close that failed can
	// be run again and still finds what to stop.
	if len(errs) == 0 {
		if err := os.Remove(b.statePath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return c, errors.Join(errs...)
}

// sweep deletes every profile directory mw browser made in the temp root.
func (b Browser) sweep() ([]string, error) {
	entries, err := os.ReadDir(b.tempRoot())
	if err != nil {
		return nil, err
	}
	var removed []string
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), ProfilePrefix) {
			continue
		}
		p := filepath.Join(b.tempRoot(), e.Name())
		if err := os.RemoveAll(p); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, p)
	}
	return removed, errors.Join(errs...)
}

// sessionNames are the files and directories a browser keeps a signed-in
// session in: Chromium's cookies, saved logins and web storage.
var sessionNames = []string{"Cookies", "Login Data", "Web Data", "Local Storage", "Session Storage", "IndexedDB", "Network Persistent State"}

// SessionFilesUnder lists every file or directory under root that keeps a
// browser's session: a profile mw browser made, or a file named as Chromium
// names its cookies, logins and storage, or a JSON file whose name speaks of
// storage or cookies.
func SessionFilesUnder(root string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		name := d.Name()
		lower := strings.ToLower(name)
		switch {
		case d.IsDir() && strings.HasPrefix(name, ProfilePrefix):
			found = append(found, p)
			return filepath.SkipDir
		case slices.Contains(sessionNames, name):
			found = append(found, p)
		case strings.HasSuffix(lower, ".json") && (strings.Contains(lower, "storage") || strings.Contains(lower, "cookie")):
			found = append(found, p)
		}
		return nil
	})
	sort.Strings(found)
	return found, err
}

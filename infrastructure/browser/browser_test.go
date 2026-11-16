package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func freeTestPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// newTestBrowser is a Browser over a fake launcher, its temp root and state
// directory inside root.
func newTestBrowser(t *testing.T) (Browser, *Fake, string) {
	t.Helper()
	root := t.TempDir()
	tmp := filepath.Join(root, "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	f := NewFake()
	t.Cleanup(f.StopAll)
	return Browser{
		Launcher:     f,
		StateDir:     filepath.Join(root, "state"),
		TempRoot:     tmp,
		Display:      5,
		VNCPort:      freeTestPort(t),
		DevToolsPort: freeTestPort(t),
		Xtigervnc:    "Xtigervnc",
		Chromium:     "chrome",
		Ready:        3 * time.Second,
	}, f, root
}

func TestOpenThenCloseLeavesNoSessionFileAnywhereMWWrites(t *testing.T) {
	b, f, root := newTestBrowser(t)
	o, err := b.Open(context.Background(), "https://bank.example/login")
	if err != nil {
		t.Fatal(err)
	}
	profile := o.State.Profile
	for _, name := range []string{"Default/Cookies", "Default/Login Data", "Default/Local Storage/leveldb/000003.log", "Default/Session Storage/000003.log", "Default/IndexedDB/x/y"} {
		p := filepath.Join(profile, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if left, _ := SessionFilesUnder(root); len(left) == 0 {
		t.Fatal("SessionFilesUnder found none of the files Chromium wrote: the check below would prove nothing")
	}
	c, err := b.Close(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !c.WasOpen {
		t.Errorf("close said nothing was open: %s", c)
	}
	if _, err := os.Stat(profile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the profile %s survives the close (%v)", profile, err)
	}
	if f.Up(ChromiumName) || f.Up(VNCName) {
		t.Errorf("a program still runs after close")
	}
	left, err := SessionFilesUnder(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("session files survive the close: %v", left)
	}
	var all []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			all = append(all, p)
		}
		return nil
	})
	if len(all) > 0 {
		t.Errorf("files mw wrote survive the close: %v", all)
	}
}

func TestOpenStartsVNCOnLoopbackWithNoPasswordAndChromiumOnAPrivateProfile(t *testing.T) {
	b, f, _ := newTestBrowser(t)
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("DISPLAY", ":0")
	o, err := b.Open(context.Background(), "https://bank.example/")
	if err != nil {
		t.Fatal(err)
	}
	started := f.Started()
	if len(started) != 2 || started[0].Name != VNCName || started[1].Name != ChromiumName {
		t.Fatalf("started %v, want the VNC display then Chromium", started)
	}
	vnc, chromium := started[0], started[1]
	for _, a := range [][]string{{":5"}, {"-localhost"}, {"-interface", "127.0.0.1"}, {"-SecurityTypes", "None"}, {"-nolisten", "tcp"}} {
		if !containsRun(vnc.Args, a) {
			t.Errorf("Xtigervnc %v lacks %v", vnc.Args, a)
		}
	}
	for _, a := range []string{"--user-data-dir=" + o.State.Profile, "--remote-debugging-address=127.0.0.1", "--ozone-platform=x11", "https://bank.example/"} {
		if !slices.Contains(chromium.Args, a) {
			t.Errorf("Chromium %v lacks %s", chromium.Args, a)
		}
	}
	if !slices.Contains(chromium.Env, "DISPLAY=:5") || slices.Contains(chromium.Env, "DISPLAY=:0") || slices.Contains(chromium.Env, "WAYLAND_DISPLAY=wayland-0") {
		t.Errorf("Chromium's environment does not put it on :5 alone: %v", chromium.Env)
	}
	for _, p := range started {
		if !strings.HasPrefix(p.Log, o.State.Profile+string(filepath.Separator)) {
			t.Errorf("%s logs to %s, outside the profile", p.Name, p.Log)
		}
	}
	fi, err := os.Stat(o.State.Profile)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("the profile's mode: %v %v", fi, err)
	}
}

func containsRun(args, run []string) bool {
	for i := range args {
		if i+len(run) <= len(args) && slices.Equal(args[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

func TestTheProfileTurnsPasswordSavingOff(t *testing.T) {
	b, _, _ := newTestBrowser(t)
	o, err := b.Open(context.Background(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(o.State.Profile, "Default", "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	var prefs struct {
		Credentials *bool `json:"credentials_enable_service"`
		Profile     struct {
			PasswordManager *bool `json:"password_manager_enabled"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(raw, &prefs); err != nil {
		t.Fatal(err)
	}
	if prefs.Credentials == nil || *prefs.Credentials || prefs.Profile.PasswordManager == nil || *prefs.Profile.PasswordManager {
		t.Errorf("the profile's Preferences leave password saving on: %s", raw)
	}
}

func TestTheStateFileHoldsNoURLAndIsPrivate(t *testing.T) {
	b, _, _ := newTestBrowser(t)
	if _, err := b.Open(context.Background(), "https://bank.example/login?token=abc"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(b.StateDir, StateFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "bank.example") || strings.Contains(string(raw), "token") {
		t.Errorf("the state file names the page: %s", raw)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("the state file's mode is %v", fi.Mode().Perm())
	}
}

func TestStatusNamesTabsAndTheEndpointAndSaysWhenChromiumWasClosedByHand(t *testing.T) {
	b, f, _ := newTestBrowser(t)
	if _, err := b.Open(context.Background(), "https://bank.example/login"); err != nil {
		t.Fatal(err)
	}
	if err := f.AddTab("https://bank.example/checks"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Up() || len(s.Tabs) != 2 || s.Tabs[1].URL != "https://bank.example/checks" {
		t.Errorf("status: %+v", s)
	}
	if !strings.HasPrefix(s.WebSocket, "ws://127.0.0.1:") {
		t.Errorf("status names no browser websocket: %+v", s)
	}
	f.Kill(ChromiumName)
	s, err = b.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Up() || !s.Open || !strings.Contains(s.String(), "mw browser close") {
		t.Errorf("a closed-by-hand Chromium is not said: %s", s)
	}
}

func TestOpenRefusesAPortAlreadyInUse(t *testing.T) {
	b, f, root := newTestBrowser(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	b.VNCPort = ln.Addr().(*net.TCPAddr).Port
	_, err = b.Open(context.Background(), "about:blank")
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("open on a used port: %v", err)
	}
	if len(f.Started()) != 0 {
		t.Errorf("started %v on a used port", f.Started())
	}
	if left, _ := SessionFilesUnder(root); len(left) > 0 {
		t.Errorf("left %v", left)
	}
}

func TestAnOpenThatFailsHalfwayStopsWhatItStartedAndDeletesTheProfile(t *testing.T) {
	b, f, root := newTestBrowser(t)
	b.Ready = 300 * time.Millisecond
	f.Silent(ChromiumName)
	_, err := b.Open(context.Background(), "about:blank")
	if err == nil || !strings.Contains(err.Error(), "DevTools") {
		t.Fatalf("open with a silent Chromium: %v", err)
	}
	if !strings.Contains(err.Error(), "fake chrome started") {
		t.Errorf("the error does not carry Chromium's log: %v", err)
	}
	if f.Up(VNCName) || f.Up(ChromiumName) {
		t.Errorf("a program still runs after a failed open")
	}
	if left, _ := SessionFilesUnder(root); len(left) > 0 {
		t.Errorf("left %v", left)
	}
	if _, err := os.Stat(filepath.Join(b.StateDir, StateFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the state file survives a failed open: %v", err)
	}
}

func TestCloseWithNothingOpenStillDeletesALeftoverProfile(t *testing.T) {
	b, _, root := newTestBrowser(t)
	left, err := os.MkdirTemp(b.TempRoot, ProfilePrefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(left, "Cookies"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(b.TempRoot, "someone-else")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	c, err := b.Close(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.WasOpen || len(c.Removed) != 1 {
		t.Errorf("close: %+v", c)
	}
	if found, _ := SessionFilesUnder(root); len(found) > 0 {
		t.Errorf("left %v", found)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("close deleted a directory not its own: %v", err)
	}
}

func TestASecondOpenIsRefusedButAStaleOneIsForgotten(t *testing.T) {
	b, f, _ := newTestBrowser(t)
	first, err := b.Open(context.Background(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Open(context.Background(), "about:blank"); err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("a second open: %v", err)
	}
	f.Kill(ChromiumName)
	f.Kill(VNCName)
	second, err := b.Open(context.Background(), "about:blank")
	if err != nil {
		t.Fatalf("an open after both died: %v", err)
	}
	if _, err := os.Stat(first.State.Profile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the stale profile %s was kept", first.State.Profile)
	}
	if second.State.Profile == first.State.Profile {
		t.Errorf("the profile was reused")
	}
}

func TestSayNamesTheTunnelTheVNCAddressAndTheEndpoint(t *testing.T) {
	o := Opened{State: State{Display: 5, VNCPort: 5905, DevToolsPort: 9222}, URL: "https://x.example/"}
	said := o.Say("lap")
	for _, want := range []string{"ssh -L 5905:127.0.0.1:5905 lap", "127.0.0.1:5905", "http://127.0.0.1:9222", "connectOverCDP", "mw browser close"} {
		if !strings.Contains(said, want) {
			t.Errorf("open did not say %q:\n%s", want, said)
		}
	}
}

func TestFindChromiumPrefersPathThenTheNewestPlaywrightBuild(t *testing.T) {
	home := t.TempDir()
	for _, d := range []string{"chromium-1100/chrome-linux", "chromium-1243/chrome-linux64", "chromium_headless_shell-1300/chrome-linux"} {
		if err := os.MkdirAll(filepath.Join(home, ".cache", "ms-playwright", d), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".cache", "ms-playwright", d, "chrome"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	none := func(string) (string, error) { return "", exec.ErrNotFound }
	got, err := FindChromium(home, none)
	if err != nil || got != filepath.Join(home, ".cache", "ms-playwright", "chromium-1243", "chrome-linux64", "chrome") {
		t.Errorf("FindChromium = %q, %v", got, err)
	}
	onPath := func(name string) (string, error) {
		if name == "chromium" {
			return "/usr/bin/chromium", nil
		}
		return "", exec.ErrNotFound
	}
	if got, _ := FindChromium(home, onPath); got != "/usr/bin/chromium" {
		t.Errorf("FindChromium with chromium on PATH = %q", got)
	}
	if _, err := FindChromium(t.TempDir(), none); err == nil {
		t.Error("FindChromium found a Chromium where there is none")
	}
}

func TestTheExecLauncherStopsTheWholeProcessGroup(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	l := ExecLauncher{}
	pid, err := l.Start(Program{Name: "sh", Path: "/bin/sh", Args: []string{"-c", "sleep 60 & echo started; wait", marker}, Log: filepath.Join(dir, "log")})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !l.Running(pid, marker) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !l.Running(pid, marker) {
		t.Fatal("the started program is not running")
	}
	if l.Running(pid, "no-such-marker") {
		t.Error("Running matched a marker the program does not carry")
	}
	if err := l.Stop(pid); err != nil {
		t.Fatal(err)
	}
	if l.Running(pid, marker) {
		t.Error("the program runs after Stop")
	}
	if err := exec.Command("pgrep", "-g", strconv.Itoa(pid)).Run(); err == nil {
		t.Error("a process of the group survives Stop")
	}
	if fi, err := os.Stat(filepath.Join(dir, "log")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the log: %v %v", fi, err)
	}
}

// TestNoMWSourceKeepsASignedInSession greps every file mw is built or run
// from for a way of keeping a browser's session outside its throwaway profile:
// Playwright's save-storage flag and storage-state files.
func TestNoMWSourceKeepsASignedInSession(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("not the repository root: %s", root)
	}
	banned := []string{"save" + "-storage", "storage" + "State", "storage" + "_state", "storage" + "-state"}
	seen := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "node_modules" || n == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".go", ".sh", ".js", ".ts", ".py", ".ps1", ".service", ".toml":
		default:
			return nil
		}
		if strings.HasSuffix(p, "_test.go") || strings.Contains(p, string(filepath.Separator)+"features"+string(filepath.Separator)) {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		seen++
		for _, word := range banned {
			if strings.Contains(string(raw), word) {
				t.Errorf("%s names %q: mw keeps no signed-in session outside the browser's throwaway profile", p, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 100 {
		t.Fatalf("read only %d source files: the walk missed the repository", seen)
	}
}

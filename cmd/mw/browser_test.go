package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mwbrowser "github.com/Jonathan-A-White/millwright/infrastructure/browser"
)

// standInBrowser points mw browser at a fake launcher, a home and a temp dir
// of the test's own, and returns the fake and the directory holding both.
func standInBrowser(t *testing.T) (*mwbrowser.Fake, string) {
	t.Helper()
	root := t.TempDir()
	home, tmp := filepath.Join(root, "home"), filepath.Join(root, "tmp")
	for _, d := range []string{home, tmp} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", tmp)
	f := mwbrowser.NewFake()
	t.Cleanup(f.StopAll)
	oldL, oldP := browserLauncher, browserPrograms
	browserLauncher = f
	browserPrograms = func(string) (string, string, error) { return "/usr/bin/Xtigervnc", "/opt/chrome/chrome", nil }
	t.Cleanup(func() { browserLauncher, browserPrograms = oldL, oldP })
	return f, root
}

func runBrowser(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"browser"}, args...))
	err := root.Execute()
	return out.String(), err
}

func testPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)
}

func TestMwBrowserOpenStatusCloseForgetsTheSession(t *testing.T) {
	f, root := standInBrowser(t)
	vnc, devtools := testPort(t), testPort(t)
	said, err := runBrowser(t, "open", "https://bank.example/login", "--vnc-port", vnc, "--devtools-port", devtools, "--ssh-host", "lap")
	if err != nil {
		t.Fatalf("open: %v\n%s", err, said)
	}
	for _, want := range []string{"ssh -L " + vnc + ":127.0.0.1:" + vnc + " lap", "127.0.0.1:" + vnc, "http://127.0.0.1:" + devtools} {
		if !strings.Contains(said, want) {
			t.Errorf("open did not say %q:\n%s", want, said)
		}
	}
	started := f.Started()
	if len(started) != 2 || started[0].Path != "/usr/bin/Xtigervnc" || started[1].Path != "/opt/chrome/chrome" {
		t.Fatalf("started %+v", started)
	}
	profile := strings.TrimPrefix(started[1].Args[0], "--user-data-dir=")
	if filepath.Dir(profile) != filepath.Join(root, "tmp") {
		t.Errorf("the profile %s is not in TMPDIR", profile)
	}
	if err := os.WriteFile(filepath.Join(profile, "Default", "Cookies"), []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}

	said, err = runBrowser(t, "status")
	if err != nil || !strings.Contains(said, "mw browser: up") || !strings.Contains(said, "tab: https://bank.example/login") || !strings.Contains(said, "CDP endpoint: http://127.0.0.1:"+devtools) {
		t.Errorf("status: %v\n%s", err, said)
	}

	said, err = runBrowser(t, "close")
	if err != nil || !strings.Contains(said, "mw browser: closed") || !strings.Contains(said, profile) {
		t.Errorf("close: %v\n%s", err, said)
	}
	if _, err := os.Stat(profile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the profile survives close: %v", err)
	}
	if left, _ := mwbrowser.SessionFilesUnder(root); len(left) > 0 {
		t.Errorf("session files survive close: %v", left)
	}
	if _, err := os.Stat(filepath.Join(root, "home", browserStateDir, mwbrowser.StateFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the state file survives close: %v", err)
	}
	said, err = runBrowser(t, "status")
	if err != nil || !strings.Contains(said, "mw browser: not open") {
		t.Errorf("status after close: %v\n%s", err, said)
	}
}

func TestMwBrowserOpenWantsAURL(t *testing.T) {
	standInBrowser(t)
	if _, err := runBrowser(t, "open"); err == nil {
		t.Error("open with no url was not refused")
	}
}

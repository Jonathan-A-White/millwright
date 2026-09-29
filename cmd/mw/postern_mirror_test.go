package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mirrorWorld is a host that runs mw postern mirror against a stand-in rsync and
// ssh first on PATH: each logs its arguments to calls, and the ssh answers the
// question of the boost's index with remoteLast.
type mirrorWorld struct {
	calls      string
	data       string
	remoteLast string
	vault      string
}

// mirrorHost sets up this host as host, with the vault's home file saying home,
// the boost reached by `ssh desktop`, and the watchdog target when watchdog is
// not empty. remoteLast is what the boost's last index line is (empty: it has no
// index).
func mirrorHost(t *testing.T, host, home, watchdog, remoteLast string) *mirrorWorld {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins for rsync and ssh are shell scripts")
	}
	w := &mirrorWorld{data: filepath.Join(t.TempDir(), "postern-data")}
	dir := t.TempDir()
	w.calls = filepath.Join(dir, "calls")
	w.remoteLast = filepath.Join(dir, "remote-last")
	if err := os.WriteFile(w.remoteLast, []byte(remoteLast), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(w.data, 0o755); err != nil {
		t.Fatal(err)
	}
	local := `{"seq":2,"txid":"b","vout":0,"scriptHex":"00","height":0,"firstSeen":"2026-09-29T10:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(w.data, "postern-index.jsonl"), []byte(`{"seq":1,"firstSeen":"2026-09-29T09:00:00Z"}`+"\n"+local), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	rsync := "#!/bin/sh\nprintf 'rsync' >> " + w.calls + "\nfor a in \"$@\"; do printf ' [%s]' \"$a\" >> " + w.calls + "; done\necho >> " + w.calls + "\n"
	ssh := "#!/bin/sh\nprintf 'ssh' >> " + w.calls + "\nfor a in \"$@\"; do printf ' [%s]' \"$a\" >> " + w.calls + "; done\necho >> " + w.calls + "\ncat " + w.remoteLast + "\n"
	for name, script := range map[string]string{"rsync": rsync, "ssh": ssh} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	vault := t.TempDir()
	w.vault = vault
	if err := os.WriteFile(filepath.Join(vault, "home"), []byte(home+" 2026-09-29T00:10:00Z mw@"+home+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("postern_data = %q\n", w.data)
	if watchdog != "" {
		config += fmt.Sprintf("postern_watchdog_target = %q\n", watchdog)
	}
	config += "[hands_hosts]\ndesktop = \"ssh desktop\"\nlaptop = \"ssh laptop\"\n"
	mwConfig(t, config)
	t.Setenv("MW_VAULT", vault)
	t.Setenv("MW_HOST", host)
	t.Setenv("MW_POSTERN_DATA", "")
	return w
}

func (w *mirrorWorld) called(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(w.calls)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

// runMirror runs mw postern mirror and returns what it printed and its status.
func runMirror(t *testing.T) (string, int) {
	t.Helper()
	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"postern", "mirror"})
	err := root.Execute()
	return out.String(), exitCode(err)
}

func TestPosternMirrorOnTheHomeCopiesTheDataToTheBoostAndTheTwoFilesToTheWatchdog(t *testing.T) {
	w := mirrorHost(t, "laptop", "laptop", "root@vps.example:/var/lib/postern-watchdog/", `{"seq":1,"firstSeen":"2026-09-29T09:00:00Z"}`+"\n")

	out, status := runMirror(t)
	if status != 0 {
		t.Fatalf("expected status 0, got %d:\n%s", status, out)
	}
	calls := w.called(t)
	var rsyncs []string
	for _, c := range calls {
		if strings.HasPrefix(c, "rsync") {
			rsyncs = append(rsyncs, c)
		}
	}
	if len(rsyncs) != 2 {
		t.Fatalf("expected two rsyncs (the boost and the watchdog), got calls:\n%s", strings.Join(calls, "\n"))
	}
	boost, watchdog := rsyncs[0], rsyncs[1]
	for _, want := range []string{"[-rpR]", "[ssh]", "[postern-index.jsonl]", "[postern-vapid.json]", "[postern-push-subscriptions.json]", "[blobs]", "[desktop:" + w.data + "/]"} {
		if !strings.Contains(boost, want) {
			t.Errorf("the boost's rsync lacks %s:\n%s", want, boost)
		}
	}
	for _, want := range []string{"[-rpR]", "[postern-vapid.json]", "[postern-push-subscriptions.json]", "[root@vps.example:/var/lib/postern-watchdog/]"} {
		if !strings.Contains(watchdog, want) {
			t.Errorf("the watchdog's rsync lacks %s:\n%s", want, watchdog)
		}
	}
	if strings.Contains(watchdog, "postern-index.jsonl") || strings.Contains(watchdog, "blobs") {
		t.Errorf("the watchdog's rsync must carry only the two files:\n%s", watchdog)
	}
	if !strings.Contains(out, "desktop") || !strings.Contains(out, "root@vps.example") {
		t.Errorf("expected the run to say where it copied, got:\n%s", out)
	}
}

func TestPosternMirrorWithNoWatchdogTargetCopiesToTheBoostOnly(t *testing.T) {
	w := mirrorHost(t, "laptop", "laptop", "", "")

	out, status := runMirror(t)
	if status != 0 {
		t.Fatalf("expected status 0, got %d:\n%s", status, out)
	}
	rsyncs := 0
	for _, c := range w.called(t) {
		if strings.HasPrefix(c, "rsync") {
			rsyncs++
		}
	}
	if rsyncs != 1 {
		t.Errorf("expected one rsync, got calls:\n%s", strings.Join(w.called(t), "\n"))
	}
}

func TestPosternMirrorOnABoostRunsNothingAndSaysSo(t *testing.T) {
	w := mirrorHost(t, "desktop", "laptop", "root@vps.example:/var/lib/postern-watchdog/", "")

	out, status := runMirror(t)
	if status != 0 {
		t.Fatalf("expected status 0, got %d:\n%s", status, out)
	}
	if calls := w.called(t); len(calls) != 0 {
		t.Errorf("expected no rsync or ssh on a boost, got:\n%s", strings.Join(calls, "\n"))
	}
	if !strings.Contains(out, "not home") || !strings.Contains(out, "laptop is home") {
		t.Errorf("expected the run to say this host is not home, got:\n%s", out)
	}
}

func TestPosternMirrorLeavesABoostWhoseIndexIsNewerAloneAndSaysWhy(t *testing.T) {
	w := mirrorHost(t, "laptop", "laptop", "root@vps.example:/var/lib/postern-watchdog/", `{"seq":9,"firstSeen":"2026-09-29T10:00:01Z"}`+"\n")

	out, status := runMirror(t)
	if status != 1 {
		t.Errorf("expected status 1 (the boost was not copied to), got %d:\n%s", status, out)
	}
	if !strings.Contains(out, "newer") || !strings.Contains(out, "desktop") || !strings.Contains(out, "2026-09-29T10:00:01Z") {
		t.Errorf("expected the run to say the boost's index is newer, and when, got:\n%s", out)
	}
	var rsyncs []string
	for _, c := range w.called(t) {
		if strings.HasPrefix(c, "rsync") {
			rsyncs = append(rsyncs, c)
		}
	}
	if len(rsyncs) != 1 || !strings.Contains(rsyncs[0], "root@vps.example") {
		t.Errorf("expected the watchdog's copy alone to run, got:\n%s", strings.Join(rsyncs, "\n"))
	}
}

func TestPosternMirrorCopiesOverAnIndexWithTheSameLastLineTime(t *testing.T) {
	w := mirrorHost(t, "laptop", "laptop", "", `{"seq":2,"firstSeen":"2026-09-29T10:00:00Z"}`+"\n")

	if out, status := runMirror(t); status != 0 {
		t.Fatalf("expected status 0, got %d:\n%s", status, out)
	}
	if calls := w.called(t); len(calls) != 2 || !strings.HasPrefix(calls[1], "rsync") {
		t.Errorf("expected the ssh question, then the rsync, got:\n%s", strings.Join(calls, "\n"))
	}
}

func TestPosternMirrorWithNoWayToReachTheBoostFails(t *testing.T) {
	w := mirrorHost(t, "laptop", "laptop", "", "")
	mwConfig(t, fmt.Sprintf("postern_data = %q\n", w.data))
	t.Setenv("MW_VAULT", w.vault)
	t.Setenv("MW_HOST", "laptop")

	out, status := runMirror(t)
	if status != 1 || !strings.Contains(out, "desktop") || !strings.Contains(out, "hands_hosts") {
		t.Errorf("expected status 1 naming the missing host and its table, got %d:\n%s", status, out)
	}
}

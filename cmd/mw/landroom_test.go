package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/apartproc"
)

// an ssh that answers for the hosts of a book: cloud14 has room, cloud2's gate is
// held, boost1 does not answer.
func aBookOfHostsOverSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$@" >> ` + log + `
case "$*" in
*cloud14*--room*) printf 'rig: yes\ngate: free\nload: 0.5 8 0\n' ;;
*cloud2*--room*) printf 'rig: yes\ngate: held by mw-x.1 check\nload: 1 8 0\n' ;;
*boost1*--room*) echo "ssh: connect to host boost1: Connection timed out" >&2; exit 255 ;;
*cloud14*) exit 0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func testBatchHosts(t *testing.T) sshBatchHosts {
	h := newSSHBatchHosts("desktop", &apartproc.Landings{Dir: t.TempDir(), Program: "unused", Scope: noScopeRun(t), Poll: 20 * time.Millisecond})
	h.reach = func(context.Context) (map[string]string, map[string]string, error) {
		return map[string]string{"desktop": "ssh desktop", "cloud2": "ssh mw@cloud2", "cloud14": "ssh mw@cloud14", "boost1": "ssh mw@boost1"}, nil, nil
	}
	return h
}

func TestTheHostBookAsksEveryOtherHostForItsRoomAndNamesWhoCouldNotBeAsked(t *testing.T) {
	aBookOfHostsOverSSH(t)
	rooms, err := testBatchHosts(t).Rooms(context.Background(), "lampas")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	free := ""
	for _, r := range rooms {
		names = append(names, r.Host)
		if r.Free() {
			free = r.Host
		}
	}
	if got := strings.Join(names, ","); got != "boost1,cloud14,cloud2" {
		t.Fatalf("expected every host but the home asked, in name order, got %s", got)
	}
	if free != "cloud14" {
		t.Fatalf("expected only cloud14 to have room, got %q from %+v", free, rooms)
	}
	for _, r := range rooms {
		if r.Host == "boost1" && !strings.Contains(r.Trouble, "Connection timed out") {
			t.Errorf("expected boost1's trouble said, got %+v", r)
		}
		if r.Host == "cloud2" && r.GateHeldBy != "mw-x.1 check" {
			t.Errorf("expected cloud2's gate holder said, got %+v", r)
		}
	}
}

func TestTheHostBookLandsABatchOnAHostOverSSHAndWaitsForIt(t *testing.T) {
	log := aBookOfHostsOverSSH(t)
	h := testBatchHosts(t)
	if err := h.Land(context.Background(), "cloud14", "lampas"); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "mw@cloud14 bash -lc 'mw land '\\''lampas'\\'''") {
		t.Fatalf("expected mw land lampas run on cloud14 over ssh, got %q", calls)
	}
	if err := h.Land(context.Background(), "cloud99", "lampas"); err == nil || !strings.Contains(err.Error(), "no way to reach cloud99") {
		t.Fatalf("expected a host that is not in the book refused, got %v", err)
	}
}

// noScopeRun stands for systemd-run: it runs what follows its own options.
func noScopeRun(t *testing.T) string {
	t.Helper()
	prog := filepath.Join(t.TempDir(), "systemd-run")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\nwhile [ \"$1\" != sh ]; do shift; done\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog
}

func TestMwLandRoomSaysWhetherThisHostHasTheRigAndLandsNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := filepath.Join(home, ".config", "mw")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "config.toml"), []byte("[rigs]\nlampas = \""+home+"/lampas\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rig, want := range map[string]string{"lampas": "rig: yes\n", "millwright": "rig: no\n"} {
		var out strings.Builder
		cmd := newLandCmd()
		cmd.SetOut(&out)
		cmd.SetArgs([]string{rig, "--room"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), want) || !strings.Contains(out.String(), "\ngate: ") || !strings.Contains(out.String(), "\nload: ") {
			t.Fatalf("expected the room of %s to open with %q and say its gate and load, got %q", rig, want, out.String())
		}
	}
}

func TestMwLandRoomGoesWithNoOtherFlag(t *testing.T) {
	cmd := newLandCmd()
	cmd.SetArgs([]string{"lampas", "--room", "--one"})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--room") {
		t.Fatalf("expected --room with --one refused, got %v", err)
	}
}

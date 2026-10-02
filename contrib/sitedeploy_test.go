package contrib_test

// This test drives contrib/site-deploy against a pair of temporary directories:
// host `local` is a directory on this machine, and a fake ssh on the front of
// PATH stands in for another host by dropping its host argument and running the
// command here. A stand-in rsync that is killed midway makes the cut-off copy.
// The real rsync does the rest, so the tests skip where there is none.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// site is a throwaway deploy: a build to ship, the tree being served, and a
// bin of stand-ins for the front of PATH.
type site struct {
	t               *testing.T
	dist, live, bin string
}

func newSite(t *testing.T) *site {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("no rsync on this host")
	}
	root := t.TempDir()
	s := &site{t: t, dist: filepath.Join(root, "dist"), live: filepath.Join(root, "www", "postern"), bin: filepath.Join(root, "bin")}
	for _, d := range []string{s.dist, filepath.Dir(s.live), s.bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// write puts files, path to contents, under dir.
func (s *site) write(dir string, files map[string]string) {
	s.t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			s.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			s.t.Fatal(err)
		}
	}
}

func (s *site) standIn(name, body string) {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(s.bin, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		s.t.Fatal(err)
	}
}

func (s *site) deploy(host string) (string, int) {
	s.t.Helper()
	script, err := filepath.Abs("site-deploy")
	if err != nil {
		s.t.Fatal(err)
	}
	cmd := exec.Command(script, s.dist, host, s.live)
	cmd.Env = []string{"PATH=" + s.bin + ":" + os.Getenv("PATH"), "HOME=" + s.t.TempDir()}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	exited, ok := err.(*exec.ExitError)
	if !ok {
		s.t.Fatalf("site-deploy could not be run: %v", err)
	}
	return string(out), exited.ExitCode()
}

// tree is every file under dir, path to contents.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = string(body)
		return err
	})
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	return files
}

func sameTree(t *testing.T, what, dir string, want map[string]string) {
	t.Helper()
	got := tree(t, dir)
	if len(got) != len(want) {
		t.Errorf("%s: expected %v, got %v", what, want, got)
		return
	}
	for name, body := range want {
		if got[name] != body {
			t.Errorf("%s: expected %s to hold %q, got %q (tree %v)", what, name, body, got[name], got)
		}
	}
}

const (
	oldPage = `<html><script src="/assets/index-OLD.js"></script><link href="/assets/app-OLD.css" rel="stylesheet"></html>`
	newPage = `<html><script type="module" src="/assets/index-NEW.js"></script><link rel="stylesheet" href="/assets/app-OLD.css"></html>`
)

func (s *site) oldLive() map[string]string {
	old := map[string]string{"index.html": oldPage, "assets/index-OLD.js": "old js", "assets/app-OLD.css": "css"}
	s.write(s.live, old)
	return old
}

func (s *site) newBuild() map[string]string {
	build := map[string]string{"index.html": newPage, "assets/index-NEW.js": "new js", "assets/app-OLD.css": "css"}
	s.write(s.dist, build)
	return build
}

func TestAGoodCopySwapsAndKeepsThePreviousCompleteTree(t *testing.T) {
	s := newSite(t)
	old := s.oldLive()
	build := s.newBuild()

	if out, status := s.deploy("local"); status != 0 {
		t.Fatalf("expected the deploy to succeed, got status %d:\n%s", status, out)
	}

	sameTree(t, "the live tree", s.live, build)
	sameTree(t, "the previous tree", s.live+".prev", old)
	if _, err := os.Stat(s.live + ".next"); err == nil {
		t.Error("expected no .next folder to be left behind")
	}
}

func TestOnlyWhatChangedTravelsAFileThatDidNotIsLinkedToTheLiveOne(t *testing.T) {
	s := newSite(t)
	s.oldLive()
	s.newBuild()
	before, err := os.Stat(filepath.Join(s.live, "assets", "app-OLD.css"))
	if err != nil {
		t.Fatal(err)
	}

	if out, status := s.deploy("local"); status != 0 {
		t.Fatalf("deploy failed, status %d:\n%s", status, out)
	}

	after, err := os.Stat(filepath.Join(s.live, "assets", "app-OLD.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("expected the unchanged asset to be a hard link to the one that was live, not a new copy")
	}
}

func TestACopyKilledMidwayLeavesTheLiveTreeAsItWas(t *testing.T) {
	s := newSite(t)
	old := s.oldLive()
	s.write(s.live+".prev", map[string]string{"index.html": "the one before", "assets/x.js": "x"})
	s.newBuild()
	// The page goes up and then rsync is killed, with the script not sent: the
	// half-deployed state of 2026-10-02.
	s.standIn("rsync", `for last; do :; done
mkdir -p "$last"
cp `+s.dist+`/index.html "$last"
kill -9 $$
`)

	out, status := s.deploy("local")

	if status == 0 {
		t.Fatalf("expected a killed copy to fail the deploy, got:\n%s", out)
	}
	if !strings.Contains(out, "untouched") {
		t.Errorf("expected the failure to say the live tree is untouched, got %q", out)
	}
	sameTree(t, "the live tree", s.live, old)
	sameTree(t, "the previous tree", s.live+".prev", map[string]string{"index.html": "the one before", "assets/x.js": "x"})
	if _, err := os.Stat(s.live + ".next"); err == nil {
		t.Error("expected the half copy to be removed")
	}
}

func TestAPageNamingAMissingAssetRefusesTheSwap(t *testing.T) {
	s := newSite(t)
	old := s.oldLive()
	s.write(s.dist, map[string]string{"index.html": newPage, "assets/app-OLD.css": "css"}) // index-NEW.js never built

	out, status := s.deploy("local")

	if status == 0 {
		t.Fatalf("expected the swap to be refused, got:\n%s", out)
	}
	if !strings.Contains(out, "/assets/index-NEW.js") {
		t.Errorf("expected the missing asset to be named, got %q", out)
	}
	sameTree(t, "the live tree", s.live, old)
	if _, err := os.Stat(s.live + ".prev"); err == nil {
		t.Error("expected no .prev folder from a refused swap")
	}
	if _, err := os.Stat(s.live + ".next"); err == nil {
		t.Error("expected the refused copy to be removed")
	}
}

func TestAFirstDeployWithNothingLiveYetHasNoPrevious(t *testing.T) {
	s := newSite(t)
	build := s.newBuild()

	if out, status := s.deploy("local"); status != 0 {
		t.Fatalf("expected the first deploy to succeed, got status %d:\n%s", status, out)
	}

	sameTree(t, "the live tree", s.live, build)
	if _, err := os.Stat(s.live + ".prev"); err == nil {
		t.Error("expected no previous tree when nothing was live")
	}
}

func TestAStaleSideFolderFromAnEarlierRunIsReplaced(t *testing.T) {
	s := newSite(t)
	s.oldLive()
	build := s.newBuild()
	s.write(s.live+".next", map[string]string{"junk.txt": "left by a killed run"})

	if out, status := s.deploy("local"); status != 0 {
		t.Fatalf("deploy failed, status %d:\n%s", status, out)
	}

	sameTree(t, "the live tree", s.live, build)
}

func TestAHostOtherThanLocalIsReachedThroughSsh(t *testing.T) {
	s := newSite(t)
	old := s.oldLive()
	build := s.newBuild()
	log := filepath.Join(t.TempDir(), "ssh.log")
	// The stand-in drops the host and runs the rest here, as rsync's own
	// `ssh host rsync --server ...` and the script's `ssh host sh -s` both need.
	s.standIn("ssh", `echo "$1" >> `+log+`
shift
exec sh -c "$*"
`)

	if out, status := s.deploy("vps"); status != 0 {
		t.Fatalf("expected the deploy over ssh to succeed, got status %d:\n%s", status, out)
	}

	sameTree(t, "the live tree", s.live, build)
	sameTree(t, "the previous tree", s.live+".prev", old)
	if logged, _ := os.ReadFile(log); !strings.Contains(string(logged), "vps") {
		t.Errorf("expected ssh to be asked for vps, got %q", logged)
	}
}

func TestSiteDeployRefusesWhatItCannotQuoteOrCannotUse(t *testing.T) {
	s := newSite(t)
	s.newBuild()
	script, err := filepath.Abs("site-deploy")
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"too few arguments": {s.dist, "local"},
		"relative dir":      {s.dist, "local", "www/postern"},
		"root as dir":       {s.dist, "local", "/"},
		"a dir with space":  {s.dist, "local", "/tmp/a b"},
		"an option as host": {s.dist, "-oProxyCommand=x", s.live},
	} {
		out, err := exec.Command(script, args...).CombinedOutput()
		exited, ok := err.(*exec.ExitError)
		if !ok || exited.ExitCode() != 2 {
			t.Errorf("%s: expected exit status 2, got %v:\n%s", name, err, out)
		}
	}
	if err := os.Remove(filepath.Join(s.dist, "index.html")); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(script, s.dist, "local", s.live).CombinedOutput()
	if exited, ok := err.(*exec.ExitError); !ok || exited.ExitCode() != 1 || !strings.Contains(string(out), "index.html") {
		t.Errorf("expected a dist with no index.html to fail with status 1 naming it, got %v:\n%s", err, out)
	}
}

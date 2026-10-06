package vpsnginx_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/vpsnginx"
)

func TestTheSiteFileIsReadWithOneBatchModeCat(t *testing.T) {
	var asked []string
	vps := vpsnginx.New("root@vps.example", "/etc/nginx/site.conf", "/root/mw", "")
	vps.Run = func(_ context.Context, argv []string) ([]byte, error) {
		asked = argv
		return []byte("upstream postern_api {}\n"), nil
	}
	got, err := vps.NginxSite(context.Background())
	if err != nil || got != "upstream postern_api {}\n" {
		t.Fatalf("got %q, %v", got, err)
	}
	if line := strings.Join(asked, " "); !strings.Contains(line, "BatchMode=yes") || !strings.HasSuffix(line, "root@vps.example cat /etc/nginx/site.conf") {
		t.Errorf("asked %q", line)
	}

	vps.Run = func(context.Context, []string) ([]byte, error) { return nil, errors.New("timed out") }
	if _, err := vps.NginxSite(context.Background()); err == nil {
		t.Error("an unreachable VPS must be an error")
	}
}

func TestTheRevisionIsReadOutOfMwVersion(t *testing.T) {
	for text, want := range map[string]string{
		"mw 0.1.0-dev (8248967)\nclaude: not installed\n": "8248967",
		"mw 0.1.0-dev (8248967, modified)\n":              "8248967",
		"mw 0.1.0-dev\n":                                  "",
		"":                                                "",
	} {
		if got := vpsnginx.RevisionIn(text); got != want {
			t.Errorf("RevisionIn(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestTheBinaryLacksACommitItsRevisionDoesNotHold(t *testing.T) {
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	dir := t.TempDir()
	git("-C", dir, "init", "-q")
	commit := func(msg string) string {
		git("-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", msg)
		return git("-C", dir, "rev-parse", "HEAD")
	}
	old, fix := commit("old"), commit("fix")

	vps := vpsnginx.New("root@vps.example", "/c", "/root/mw", dir)
	says := func(revision string) {
		vps.Run = func(_ context.Context, argv []string) ([]byte, error) {
			if argv[0] == "git" {
				return exec.Command(argv[0], argv[1:]...).CombinedOutput()
			}
			return []byte("mw 0.1.0-dev (" + revision + ")\n"), nil
		}
	}
	says(old[:7])
	if lacks, err := vps.BinaryLacks(context.Background(), fix); err != nil || !lacks {
		t.Errorf("a binary built at the old commit lacks the fix: %v %v", lacks, err)
	}
	says(fix[:7])
	if lacks, err := vps.BinaryLacks(context.Background(), fix); err != nil || lacks {
		t.Errorf("a binary built at the fix has it: %v %v", lacks, err)
	}
	says("0000000")
	if _, err := vps.BinaryLacks(context.Background(), fix); err == nil {
		t.Error("a revision this checkout does not know cannot be told")
	}
	vps.Run = func(context.Context, []string) ([]byte, error) { return []byte("mw 0.1.0-dev\n"), nil }
	if _, err := vps.BinaryLacks(context.Background(), fix); err == nil {
		t.Error("a binary with no revision stamped cannot be told")
	}
}

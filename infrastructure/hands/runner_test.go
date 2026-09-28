package hands_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/hands"
)

// standIn writes an executable shell script and returns its path.
func standIn(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-ins are shell scripts")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func userJob(run string) application.HandsJob {
	return application.HandsJob{Request: domain.HandsRequest{Bead: "mw-1", ID: "s", Host: "desktop", As: "user", Run: run}}
}

// A user step here is sh -c: its output and errors together, and its exit
// status.
func TestRunnerRunsAUserStepHere(t *testing.T) {
	runner := hands.NewRunner("/usr/local/sbin/mw-hands-root")

	outcome, err := runner.Run(context.Background(), userJob("echo out; echo err >&2; exit 4"))
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if outcome.Exit != 4 || !strings.Contains(outcome.Output, "out") || !strings.Contains(outcome.Output, "err") {
		t.Fatalf("expected exit 4 with both streams, got %+v", outcome)
	}
}

// A step past its limit is stopped, everything it started with it.
func TestRunnerStopsAStepAtItsLimit(t *testing.T) {
	runner := hands.NewRunner("/usr/local/sbin/mw-hands-root")
	runner.UserLimit = 300 * time.Millisecond

	started := time.Now()
	outcome, err := runner.Run(context.Background(), userJob("echo begun; sleep 30"))
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if outcome.Exit != 124 || !strings.Contains(outcome.Output, "begun") || !strings.Contains(outcome.Output, "gave up") {
		t.Fatalf("expected the step stopped, exit 124, got %+v", outcome)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("expected it stopped at its limit, took %s", time.Since(started))
	}
}

// A root step here goes to mw-hands-root through sudo -n, the whole request
// on its standard input, for the helper to check again.
func TestRunnerHandsARootStepToTheHelperThroughSudo(t *testing.T) {
	dir := t.TempDir()
	sudo := standIn(t, "sudo", `echo "$*" > `+dir+`/args
cat > `+dir+`/stdin
echo "root ran"
exit 5
`)
	runner := hands.NewRunner("/usr/local/sbin/mw-hands-root")
	runner.Sudo = sudo
	req := domain.HandsRequest{Bead: "mw-1", ID: "linger", Host: "desktop", As: "root", Run: "loginctl enable-linger jwhite",
		SHA256: "ab", ApprovedAt: 1790000000, Sig: "3044"}

	outcome, err := runner.Run(context.Background(), application.HandsJob{Request: req})
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if outcome.Exit != 5 || strings.TrimSpace(outcome.Output) != "root ran" {
		t.Fatalf("expected the helper's own status and output, got %+v", outcome)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if strings.TrimSpace(string(args)) != "-n /usr/local/sbin/mw-hands-root" {
		t.Fatalf("expected sudo -n <helper> and nothing else, got %q", args)
	}
	var sent domain.HandsRequest
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	if err := json.Unmarshal(stdin, &sent); err != nil || sent != req {
		t.Fatalf("expected the request on the helper's standard input, got %s: %v", stdin, err)
	}
}

// A step for another host goes over its ssh prefix, the commands quoted so
// the far shell runs exactly the approved text; a root step's request rides
// ssh's standard input to the far side's sudo -n helper.
func TestRunnerReachesAnotherHostOverItsSSHPrefix(t *testing.T) {
	dir := t.TempDir()
	// The stand-in for ssh does what ssh does with its last argument: hand it
	// to a shell on the far side, which here is this one.
	ssh := standIn(t, "ssh", `for last in "$@"; do :; done
printf '%s\n' "$@" > `+dir+`/args
exec sh -c "$last"
`)
	runner := hands.NewRunner("/usr/local/sbin/mw-hands-root")
	run := `printf '%s|%s\n' "it's" "$((1+1)) \"quoted\""; echo 'single'`

	here, err := runner.Run(context.Background(), userJob(run))
	if err != nil {
		t.Fatal(err)
	}
	job := userJob(run)
	job.Request.Host = "laptop"
	job.Remote = []string{ssh, "laptop"}
	there, err := runner.Run(context.Background(), job)
	if err != nil {
		t.Fatalf("running over ssh: %v", err)
	}
	if there.Output != here.Output || there.Exit != 0 {
		t.Fatalf("expected the far side to run exactly the text run here\nhere:  %q\nthere: %q", here.Output, there.Output)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if !strings.HasPrefix(string(args), "laptop\nsh -c '") {
		t.Fatalf("expected ssh laptop 'sh -c <quoted>', got %q", args)
	}

	root := application.HandsJob{
		Request: domain.HandsRequest{Bead: "mw-1", ID: "r", Host: "laptop", As: "root", Run: "true"},
		Remote: []string{standIn(t, "ssh-root", `for last in "$@"; do :; done
printf '%s' "$last" > `+dir+`/remote
cat > `+dir+`/rootstdin
`), "root@laptop"},
	}
	if _, err := runner.Run(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	remote, _ := os.ReadFile(filepath.Join(dir, "remote"))
	if string(remote) != "sudo -n '/usr/local/sbin/mw-hands-root'" {
		t.Fatalf("expected the far side's sudo -n helper, got %q", remote)
	}
	if stdin, _ := os.ReadFile(filepath.Join(dir, "rootstdin")); !strings.Contains(string(stdin), `"as":"root"`) {
		t.Fatalf("expected the request on ssh's standard input, got %q", stdin)
	}
}

func TestVerifierIsTheOneCheckMwHandsRootMakes(t *testing.T) {
	const (
		sha = "2d74974c1cc77dd9faa271dc7c6b18dd690aa369c2cba8c5067b3c56bf42fe14"
		key = "03f01d6b9018ab421dd410404cb869072065522bf85734008f105cf385a023a80f"
		sig = "3044022044fb7a49fdaeda2ff47ed5d2d70d9371399740dd4dca94c8bcd2efe3ca2a8d4602200d029784332f7d32334d7bf3c8cdcabda8ed381fcbf0799826ec460262c06bb3"
	)
	if err := (hands.Verifier{}).VerifyApproval(key, sha, 1790000000, sig); err != nil {
		t.Fatalf("expected postern's vector to verify, got %v", err)
	}
	if err := (hands.Verifier{}).VerifyApproval(key, sha, 1790000001, sig); err == nil {
		t.Fatal("expected another time refused")
	}
}

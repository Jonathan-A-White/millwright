package cloud_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/cloud"
)

// standIn writes a wrapper that appends its arguments to calls and then runs
// body, and returns its path and the calls file. The tests here are not
// parallel: a fork while another test still holds its new script open for
// writing fails with "text file busy".
func standIn(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	path := filepath.Join(dir, "vultr-boost")
	script := "#!/bin/sh\necho \"$*\" >>" + calls + "\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, calls
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestUpAndDownRunTheWrapperForTheBoxFromTheSnapshot(t *testing.T) {
	command, calls := standIn(t, "echo made")
	var out strings.Builder
	v := cloud.Vultr{Command: command, Snapshot: "snap-1", Out: &out}
	if err := v.Up(context.Background(), "cloud2"); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := v.Down(context.Background(), "cloud2"); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got, want := read(t, calls), "up --yes --name cloud2 --snapshot snap-1\ndown --yes --name cloud2\n"; got != want {
		t.Errorf("expected the wrapper run as %q, got %q", want, got)
	}
	if !strings.Contains(out.String(), "made") {
		t.Errorf("expected the wrapper's output copied, got %q", out.String())
	}
}

func TestAFailedUpNamesTheWrappersLastLine(t *testing.T) {
	command, _ := standIn(t, "echo 'step one'; echo 'vultr-boost: terraform apply failed (status 1)' >&2; exit 1")
	err := cloud.Vultr{Command: command}.Up(context.Background(), "cloud1")
	if err == nil || !strings.Contains(err.Error(), "terraform apply failed") {
		t.Errorf("expected the failure to name the wrapper's last line, got %v", err)
	}
}

func TestBilledReadsTheSpendOrUnknown(t *testing.T) {
	command, calls := standIn(t, "echo 12.34")
	usd, known, err := cloud.Vultr{Command: command}.Billed(context.Background(), []string{"cloud1", "cloud2"})
	if err != nil || !known || usd != 12.34 {
		t.Errorf("expected $12.34 known, got %v %v %v", usd, known, err)
	}
	if got := read(t, calls); got != "spend --name cloud1 --name cloud2\n" {
		t.Errorf("expected spend asked for both boxes, got %q", got)
	}

	command, _ = standIn(t, "echo "+cloud.Unknown)
	if usd, known, err := (cloud.Vultr{Command: command}).Billed(context.Background(), nil); err != nil || known || usd != 0 {
		t.Errorf("expected unknown to be no answer and no error, got %v %v %v", usd, known, err)
	}
}

func TestTheLimitOfAMakeIsAtLeastFifteenMinutes(t *testing.T) {
	if cloud.DefaultLimit < 15*time.Minute {
		t.Errorf("a make from a snapshot takes six minutes and more: the limit %s is too short", cloud.DefaultLimit)
	}
}

func TestABegunMakeOutlivesTheCancelledCaller(t *testing.T) {
	dir := t.TempDir()
	done := filepath.Join(dir, "done")
	self := filepath.Join(dir, "mw")
	script := "#!/bin/sh\necho \"$*\" >" + filepath.Join(dir, "args") + "\nsleep 1\ntouch " + done + "\n"
	if err := os.WriteFile(self, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := cloud.Vultr{Self: self, MakeLog: filepath.Join(dir, "make.log"), NoScope: true}
	if err := v.Begin(ctx, "cloud1"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	cancel() // the caller is stopped while the make is going
	if _, err := os.Stat(done); err == nil {
		t.Fatalf("expected begin to return before the make ended")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(done); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the make did not finish after its caller was cancelled")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := strings.TrimSpace(read(t, filepath.Join(dir, "args"))); got != "cloud make cloud1" {
		t.Errorf("expected the make run as mw cloud make cloud1, got %q", got)
	}
}

func TestInstancesReadsWhatListPrints(t *testing.T) {
	command, calls := standIn(t, `printf 'a1\tcloud1\tactive\n\n \tcloud2\n\tx\ty\nb2\tcloud2\tlocked\n'`)
	got, err := cloud.Vultr{Command: command}.Instances(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a1" || got[0].Label != "cloud1" || got[0].Status != "active" || got[1].ID != "b2" || got[1].Status != "locked" {
		t.Errorf("expected the two instances with an id and a label, got %+v", got)
	}
	if read(t, calls) != "list\n" {
		t.Errorf("expected the wrapper run as list, got %q", read(t, calls))
	}
}

func TestReapDeletesTheInstanceByItsId(t *testing.T) {
	command, calls := standIn(t, "echo done")
	err := cloud.Vultr{Command: command}.Reap(context.Background(), application.CloudInstance{ID: "i-9", Label: "cloud2"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, calls), "down --yes --name cloud2 --instance i-9\n"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// What terraform says on standard error when Vultr refuses is named, not left
// to read as a box that failed to come up.
func TestAFailedUpNamesWhatVultrRefused(t *testing.T) {
	for _, c := range []struct {
		said string
		want error
	}{
		{"Error: Error creating instance: Unable to create instance: your account has reached its maximum monthly fee limit", application.ErrCloudFeeLimit},
		{"Error: Server add failed: You have reached your instance limit", application.ErrCloudQuota},
		{"Error: this plan is not available in this region", application.ErrCloudSoldOut},
		{"Error: Invalid API token", application.ErrCloudBadToken},
	} {
		// The wrapper's own last line is not the one that says it.
		command, _ := standIn(t, "echo '"+c.said+"' >&2; echo 'vultr-boost: terraform apply failed (status 1)' >&2; exit 1")
		err := cloud.Vultr{Command: command}.Up(context.Background(), "cloud1")
		if !errors.Is(err, c.want) {
			t.Errorf("%q: expected %v, got %v", c.said, c.want, err)
		}
	}
	command, _ := standIn(t, "echo 'Error: something else' >&2; exit 1")
	err := cloud.Vultr{Command: command}.Up(context.Background(), "cloud1")
	for _, refusal := range []error{application.ErrCloudFeeLimit, application.ErrCloudQuota, application.ErrCloudSoldOut, application.ErrCloudBadToken} {
		if errors.Is(err, refusal) {
			t.Errorf("a failure that is none of the refusals was named %v", refusal)
		}
	}
}

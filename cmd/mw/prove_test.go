package main

import (
	"bytes"
	"strings"
	"testing"
)

func runProve(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MW_STAMPS_DIR", t.TempDir())
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"prove"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestProveHelpPrintsUsage(t *testing.T) {
	out, err := runProve(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prove <rig> <commit>") {
		t.Fatalf("no usage in:\n%s", out)
	}
}

func TestProveOfACommitNobodyStampedFailsWithTheMessage(t *testing.T) {
	_, err := runProve(t, "millwright", "deadbeef")
	if err == nil || err.Error() != "no stamp for millwright deadbeef" {
		t.Fatalf("err = %v", err)
	}
	if exitStatus(err) != 1 {
		t.Fatalf("exit status %d, want 1", exitStatus(err))
	}
}

func TestProveWantsARigAndACommit(t *testing.T) {
	if _, err := runProve(t, "millwright"); err == nil {
		t.Fatal("one argument was accepted")
	}
}

package main

import (
	"os"
	"os/exec"
	"testing"
)

// A close-out whose output pipe has lost its reader goes on to its end: the
// claim the dispatch it chains makes is followed by the session that is started
// for it (mw-gq6.1580).
func TestACloseOutWhoseOutputLostItsReaderGoesOn(t *testing.T) {
	done := t.TempDir() + "/done"
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = pr.Close() // nobody reads
	cmd := exec.Command(os.Args[0], "-test.run=^TestBrokenOutputStandIn$")
	cmd.Env = append(os.Environ(), "MW_BROKEN_OUTPUT_HELPER="+done)
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the close-out stand-in died when it wrote to an output nobody reads: %v", err)
	}
	if _, err := os.Stat(done); err != nil {
		t.Fatalf("expected the stand-in to have gone on past its writes: %v", err)
	}
}

// TestBrokenOutputStandIn is the process the test above starts.
func TestBrokenOutputStandIn(t *testing.T) {
	done := os.Getenv("MW_BROKEN_OUTPUT_HELPER")
	if done == "" {
		t.Skip("run by TestACloseOutWhoseOutputLostItsReaderGoesOn")
	}
	surviveBrokenOutput()
	os.Stdout.WriteString("  claimed mw-x.1\n")
	os.Stderr.WriteString("  launched mw-x.1\n")
	if err := os.WriteFile(done, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
}

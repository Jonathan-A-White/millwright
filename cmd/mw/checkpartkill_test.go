package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Killing a part's process takes its whole group: a shell that started a child of
// its own (an npm install and the git clone under it) leaves neither running
// (mw-82lb97i.12).
func TestKillingAPartTakesItsWholeProcessGroup(t *testing.T) {
	childFile := filepath.Join(t.TempDir(), "child.pid")
	// The parent shell starts a long-sleeping child, keeps its pid, and waits.
	cmd := exec.Command("sh", "-c", "sleep 300 & echo $! > "+childFile+"; wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })

	var child int
	for deadline := time.Now().Add(5 * time.Second); child == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if text, err := os.ReadFile(childFile); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(text)))
		}
	}
	if child == 0 || !processAliveAny(child) {
		t.Fatalf("expected the child to be running, pid %d", child)
	}

	if err := killGroup(cmd.Process.Pid, processAliveAny, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	<-reaped
	for deadline := time.Now().Add(5 * time.Second); processAliveAny(child) && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	if processAliveAny(child) {
		t.Errorf("expected the child %d of the part to be gone with it", child)
	}
}

// A process that will not stop when told to is killed outright once the wait is up.
func TestKillingAPartThatIgnoresTheStopKillsItAnyway(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap '' TERM; sleep 300 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	time.Sleep(200 * time.Millisecond)

	if err := killGroup(cmd.Process.Pid, processAliveAny, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reaped:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the process to be killed")
	}
}

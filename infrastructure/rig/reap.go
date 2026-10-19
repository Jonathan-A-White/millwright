package rig

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"syscall"
	"time"
)

// runMarkPrefix begins the name of the mark a reaped command line carries in
// its environment, and everything it starts with it: a server in a session or
// group of its own (Playwright's webServer is one) leaves the group the command
// was started in, but keeps the environment it was given (mw-gq6.625). Each run
// has a variable of its own, so a run inside another keeps the outer's mark.
const runMarkPrefix = "MW_RUN_LINE_"

// newRunMark is a mark no other run has.
func newRunMark() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return runMarkPrefix + strconv.Itoa(os.Getpid()) + "_" + hex.EncodeToString(b[:])
}

// reapBound is how long reapMarked keeps killing what it finds before it gives up.
const reapBound = 5 * time.Second

// reapMarked kills every process of this user whose environment carries mark,
// until none is left or the bound is up: what a command line started and left
// running when it ended. A host with no /proc kills nothing.
func reapMarked(mark string) {
	needle := []byte(mark + "=")
	deadline := time.Now().Add(reapBound)
	for {
		pids := marked(needle)
		if len(pids) == 0 || time.Now().After(deadline) {
			return
		}
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// marked is the pids, not this process's own, whose environment holds an entry
// that begins with needle. A process that has died and waits to be reaped has
// no environment left to read, so it is not among them.
func marked(needle []byte) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		environ, err := os.ReadFile("/proc/" + e.Name() + "/environ")
		if err != nil {
			continue
		}
		for _, entry := range bytes.Split(environ, []byte{0}) {
			if bytes.HasPrefix(entry, needle) {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

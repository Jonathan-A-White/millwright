package application_test

import (
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
)

// mw-gq6.1021: a bd that works on another beads directory is not a blocker.

const quietVault = "/home/jwhite/millwright-vault"

func TestBdBlockerIgnoresABdOfAnotherBeadsDirectory(t *testing.T) {
	for _, tc := range []struct {
		name string
		proc application.Process
		want bool // true: it blocks
	}{
		// The report: a Go test's child, run from the package directory with -C
		// at its throwaway vault.
		{"go test child with -C in tmp", application.Process{Pid: 1550980, Argv: []string{"bd", "-C", "/tmp/TestGatewayReadsAnInbox/001", "list", "--type", "mail"}, Cwd: "/home/jwhite/millwright/application"}, false},
		{"-C long form and = form", application.Process{Pid: 2, Argv: []string{"bd", "--directory=/var/other", "list"}, Cwd: quietVault}, false},
		{"-C the caller's own directory", application.Process{Pid: 3, Argv: []string{"/usr/bin/bd", "-C", quietVault + "/", "list"}, Cwd: "/home/jwhite/x"}, true},
		{"no -C, working directory is the caller's", application.Process{Pid: 4, Argv: []string{"bd", "list"}, Cwd: quietVault}, true},
		{"no -C, working directory is another", application.Process{Pid: 5, Argv: []string{"bd", "list"}, Cwd: "/home/jwhite/millwright"}, false},
		{"relative -C is taken from its working directory", application.Process{Pid: 6, Argv: []string{"bd", "-C", "..", "list"}, Cwd: quietVault + "/sub"}, true},
		{"a sync is the caller's whatever its directory", application.Process{Pid: 7, Argv: []string{"mw", "sync"}, Cwd: "/home/jwhite"}, true},
	} {
		got := application.BdBlocker([]application.Process{tc.proc}, "/tmp", quietVault)
		if (got != "") != tc.want {
			t.Errorf("%s: blocker %q, want blocking %v", tc.name, got, tc.want)
		}
	}
}

func TestBdBlockerWithNoCallerDirectoryKeepsTheOldTest(t *testing.T) {
	other := application.Process{Pid: 9, Argv: []string{"bd", "list"}, Cwd: "/home/jwhite/millwright"}
	if application.BdBlocker([]application.Process{other}, "/tmp", "") == "" {
		t.Errorf("with the caller's directory unknown, a bd is still a blocker")
	}
	inTmp := application.Process{Pid: 10, Argv: []string{"bd", "list"}, Cwd: "/tmp/TestX/001"}
	if application.BdBlocker([]application.Process{inTmp}, "/tmp", "") != "" {
		t.Errorf("a bd in the temporary directory is not a blocker")
	}
}

func TestBdRunsAtOnceBesideATestsBdOfItsOwnVault(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 1550980, Argv: []string{"bd", "-C", "/tmp/TestGatewayReadsAnInbox/001", "list", "--type", "mail"}, Cwd: "/home/jwhite/millwright/application"})
	run := runBdQuietIn(t, procs, quietVault, "-C", quietVault, "show", "step-1")
	if run.err != nil || run.exit != 0 || run.waited != 0 {
		t.Fatalf("exit %d, err %v, waited %s: the test's bd holds no lock on the real vault", run.exit, run.err, run.waited)
	}
}

func TestBdThatTimesOutSaysWhichCommandToRunAgain(t *testing.T) {
	procs := apptest.NewFakeUserProcesses()
	procs.Start(application.Process{Pid: 77, Argv: []string{"bd", "-C", quietVault, "update", "mw-1"}})
	run := runBdQuietIn(t, procs, quietVault, "-C", quietVault, "comment", "mw-1", "two words")
	if run.err == nil || run.exit == 0 {
		t.Fatalf("exit %d, err %v: a bd that never got quiet must fail", run.exit, run.err)
	}
	want := "run again: mw bd -C " + quietVault + " comment mw-1 'two words'"
	if !strings.Contains(run.err.Error(), want) {
		t.Errorf("the error should say %q, it said %q", want, run.err)
	}
}

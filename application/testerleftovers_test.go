package application_test

import (
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw-gq6.409: a process is under a Tester's directory by its working directory
// or by a path on its command line, and a story whose id only starts the same
// (mw-x.10 against mw-x.1) is another story's.
func TestAProcessIsUnderATesterDirByCwdOrCommandLineButNotASibling(t *testing.T) {
	dir := application.TesterDir("mw-x.1")
	if dir != "/tmp/tester-mw-x.1/" {
		t.Fatalf("expected /tmp/tester-mw-x.1/, got %q", dir)
	}
	for name, p := range map[string]application.Process{
		"a preview server by its command line": {Argv: []string{"node", "/tmp/tester-mw-x.1/app/node_modules/.bin/vite", "preview"}, Cwd: "/"},
		"a process by its cwd":                 {Argv: []string{"sleep", "9"}, Cwd: "/tmp/tester-mw-x.1/shots"},
		"a process in the dir itself":          {Cwd: "/tmp/tester-mw-x.1"},
		"a flag naming a path in it":           {Argv: []string{"serve", "--root=/tmp/tester-mw-x.1/main"}},
	} {
		if !p.Under(dir) {
			t.Errorf("expected %s to be under %s", name, dir)
		}
	}
	for name, p := range map[string]application.Process{
		"a sibling story":      {Argv: []string{"node", "/tmp/tester-mw-x.10/app/vite"}, Cwd: "/tmp/tester-mw-x.10/app"},
		"another place":        {Argv: []string{"node", "/home/u/app/vite"}, Cwd: "/home/u/app"},
		"the name without dir": {Argv: []string{"tester-mw-x.1"}, Cwd: "/tmp"},
	} {
		if p.Under(dir) {
			t.Errorf("expected %s not to be under %s", name, dir)
		}
	}
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mw-6ww.119.10.10: mw release-paced built its Gateway with beads.FromConfig and
// never kept a beads connection, so the one ShowBeads of the list was answered
// by a bd show of each story, one after another: 8 minutes for the 232 stories
// of the septuagint list. It reads the stories by id over one connection kept
// open for the run, as mw needs sweep does, and starts no bd show.
func TestReleasePacedReadsTheListOverTheKeptConnection(t *testing.T) {
	asked := aQuietBd(t)
	aBdWithNoUsageReadings(t, asked)
	server := aServerToReach(t)
	mwConfig(t, "vault = \""+t.TempDir()+"\"\nhost = \"laptop\"\n")
	list := filepath.Join(t.TempDir(), "release-order.txt")
	if err := os.WriteFile(list, []byte("mw-t.1\nmw-t.2 # going\nmw-t.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	root := newRootCmd()
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"release-paced", "--dry-run", list})
	if err := root.Execute(); err != nil {
		t.Fatalf("expected the dry run to complete, got %q, %v\n%s", out, err, readAsked(t, asked))
	}
	if got := readAsked(t, asked); strings.Contains(got, " show ") {
		t.Errorf("expected no bd show for the stories of the list, got:\n%s", got)
	}
	if got := server.queries("where i.id in ('"); got == 0 {
		t.Errorf("expected the stories of the list to be read by id over the kept connection, the server was asked no such query; it said:\n%s", out)
	}
	if !strings.Contains(out.String(), "no usage was reported") {
		t.Errorf("expected the dry run to say that no usage was reported, got:\n%s", out)
	}
}

// aBdWithNoUsageReadings makes the bd of aQuietBd answer the usage readings
// with an empty list, as a vault where none was ever kept does.
func aBdWithNoUsageReadings(t *testing.T, asked string) {
	t.Helper()
	bin := filepath.Dir(mustLookBd(t))
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + asked + "\ncase \"$*\" in\n*\" kv get usage.readings\"*) echo '[]' ;;\n*\" kv \"*) echo '{}' ;;\n*) echo '[]' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

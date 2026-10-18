package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
)

// mw fold --help and mw fold supersede --help state the fold rule: the bug's
// check is copied into the fold and the bug is closed as superseded
// (mw-gq6.606).
func TestFoldHelpStatesTheFoldRule(t *testing.T) {
	for _, args := range [][]string{{"fold", "--help"}, {"fold", "supersede", "--help"}} {
		var said bytes.Buffer
		root := newRootCmd()
		root.SetOut(&said)
		root.SetErr(&said)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(said.String(), application.FoldRule) {
			t.Errorf("%v does not print the fold rule:\n%s", args, said.String())
		}
		if strings.Contains(said.String(), "closes only when") {
			t.Errorf("%v still says a folded bug closes only when its check passes", args)
		}
	}
}

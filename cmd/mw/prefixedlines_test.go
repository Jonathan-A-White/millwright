package main

import (
	"strings"
	"testing"
)

func TestPrefixedLinesPrefixesEachWholeLineAndHoldsAPartOne(t *testing.T) {
	var out strings.Builder
	p := &prefixedLines{to: &out, prefix: "mw land: grist smoke: "}
	p.Write([]byte("sending cairn/tutor/hello ...\n  FAIL"))
	if want := "mw land: grist smoke: sending cairn/tutor/hello ...\n"; out.String() != want {
		t.Fatalf("expected only the whole line %q, got %q", want, out.String())
	}
	p.Write([]byte("ED: no name\n"))
	if want := "mw land: grist smoke: sending cairn/tutor/hello ...\nmw land: grist smoke:   FAILED: no name\n"; out.String() != want {
		t.Fatalf("expected both lines prefixed, got %q", out.String())
	}
}

package rig_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/infrastructure/rig"
)

// A command line run with a progress writer in its context has what it prints
// copied there as well as returned, so a caller can see a part at work
// (mw-82lb97i.12).
func TestACommandLinesOutputIsCopiedToTheProgressWriterOfItsContext(t *testing.T) {
	var progress bytes.Buffer
	ctx := application.WithRunProgress(context.Background(), &progress)

	checked, err := rig.NewChecks().RunLine(ctx, "", t.TempDir(), "echo first; echo second")
	if err != nil || !checked.Passed {
		t.Fatalf("expected the command to pass, got %+v, %v", checked, err)
	}
	if !strings.Contains(progress.String(), "first") || !strings.Contains(progress.String(), "second") {
		t.Errorf("expected what the command printed in the progress writer, got %q", progress.String())
	}
	if !strings.Contains(checked.Output, "second") {
		t.Errorf("expected the output returned as before, got %q", checked.Output)
	}
}

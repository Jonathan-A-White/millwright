package postern_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Jonathan-A-White/millwright/infrastructure/postern"
)

// transcribeStandIn writes a shell script standing in for a transcriber and
// returns its path.
func transcribeStandIn(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in transcriber is a shell script")
	}
	path := filepath.Join(t.TempDir(), "hear")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// The command is split on whitespace, the audio's path appended, and what it
// prints, trimmed, is what was heard.
func TestCommandTranscriberRunsTheCommandWithThePathAppended(t *testing.T) {
	hear := transcribeStandIn(t, `printf '  heard %s from %s  \n' "$1" "$2"`)
	transcriber := postern.NewCommandTranscriber(hear + "   --quiet")

	heard, err := transcriber.Transcribe(context.Background(), "/state/direct-v1.webm")
	if err != nil {
		t.Fatalf("transcribing: %v", err)
	}
	if heard != "heard --quiet from /state/direct-v1.webm" {
		t.Fatalf("expected the trimmed transcript, got %q", heard)
	}
}

// A command that fails says why, from what it wrote to standard error.
func TestCommandTranscriberReportsAFailureWithWhatTheCommandSaid(t *testing.T) {
	hear := transcribeStandIn(t, "echo 'no whisper model at /nowhere' >&2\nexit 2\n")

	_, err := postern.NewCommandTranscriber(hear).Transcribe(context.Background(), "/state/x.ogg")
	if err == nil || !strings.Contains(err.Error(), "no whisper model at /nowhere") {
		t.Fatalf("expected the command's own words in the error, got %v", err)
	}
}

// A transcription that runs past its time is stopped, and says so.
func TestCommandTranscriberGivesUpAfterItsTimeout(t *testing.T) {
	hear := transcribeStandIn(t, "sleep 30\n")

	started := time.Now()
	_, err := postern.NewCommandTranscriber(hear, postern.WithTranscribeTimeout(200*time.Millisecond)).Transcribe(context.Background(), "/state/x.ogg")
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("expected a timeout, got %v", err)
	}
	if waited := time.Since(started); waited > 10*time.Second {
		t.Fatalf("expected the command stopped at its timeout, waited %s", waited)
	}
	if postern.DefaultTranscribeTimeout != 5*time.Minute {
		t.Fatalf("expected five minutes by default, got %s", postern.DefaultTranscribeTimeout)
	}
}

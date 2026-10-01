package doctor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/application/apptest"
	"github.com/Jonathan-A-White/millwright/infrastructure/doctor"
)

// ptTools makes a directory of executable stand-ins named names, and a model
// file, and reports the check over them: home, the command set, and PATH
// pointing only at the directory. Nothing here reads the real host.
func ptTools(t *testing.T, command string, names ...string) (*doctor.PosternTranscribe, string) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	model := filepath.Join(dir, "ggml-base.en.bin")
	if err := os.WriteFile(model, []byte("model"), 0o644); err != nil {
		t.Fatalf("writing the model: %v", err)
	}
	check := &doctor.PosternTranscribe{
		Command: command,
		Home:    &apptest.FakeHomeFile{Text: "laptop 2026-09-28T00:00:00Z mw@laptop"},
		Host:    "laptop",
		PathEnv: dir,
		// The service PATH here is the same directory, so only the tests of
		// the service PATH narrow it.
		ServicePath: dir,
		Getenv:      func(key string) string { return map[string]string{"POSTERN_WHISPER_MODEL": model}[key] },
		HomeDir:     dir,
	}
	return check, dir
}

func TestPosternTranscribeIsOKOnAHostThatIsNotHome(t *testing.T) {
	check, _ := ptTools(t, "")
	check.Home = &apptest.FakeHomeFile{Text: "desktop 2026-09-29T00:10:00Z mw@desktop"}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok off the home, got %s (%s)", verdict, reason)
	}
}

func TestPosternTranscribeIsFaultyOnTheHomeWhenTheCommandIsUnset(t *testing.T) {
	check, _ := ptTools(t, "")

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "postern_transcribe_cmd") {
		t.Fatalf("expected faulty naming postern_transcribe_cmd, got %s (%s)", verdict, reason)
	}
}

func TestPosternTranscribeIsFaultyWhenTheCommandIsNotExecutable(t *testing.T) {
	check, _ := ptTools(t, "/no/such/dir/transcribe --fast")

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "/no/such/dir/transcribe") {
		t.Fatalf("expected faulty naming the command, got %s (%s)", verdict, reason)
	}
}

func TestPosternTranscribeIsFaultyNamingWhatTheContribScriptLacks(t *testing.T) {
	contrib := "/opt/mw/contrib/postern-transcribe"
	cases := map[string]struct {
		tools []string
		model bool
		want  string
	}{
		"ffmpeg":      {tools: []string{"whisper-cli", "postern-transcribe"}, model: true, want: "ffmpeg"},
		"whisper-cli": {tools: []string{"ffmpeg", "postern-transcribe"}, model: true, want: "whisper-cli"},
		"model":       {tools: []string{"ffmpeg", "whisper-cli", "postern-transcribe"}, model: false, want: "ggml-base.en.bin"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			check, dir := ptTools(t, contrib, c.tools...)
			check.Command = filepath.Join(dir, "postern-transcribe")
			if !c.model {
				check.Getenv = func(string) string { return "" }
				check.HomeDir = t.TempDir()
			}
			verdict, reason := check.Probe(context.Background())
			if verdict != application.DoctorFaulty || !strings.Contains(reason, c.want) {
				t.Fatalf("expected faulty naming %s, got %s (%s)", c.want, verdict, reason)
			}
		})
	}
}

func TestPosternTranscribeIsOKWhenTheContribScriptHasEverything(t *testing.T) {
	check, dir := ptTools(t, "", "ffmpeg", "whisper-cli", "postern-transcribe")
	check.Command = filepath.Join(dir, "postern-transcribe")

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestPosternTranscribeOnlyChecksTheCommandOfAnotherProgram(t *testing.T) {
	check, dir := ptTools(t, "", "my-transcriber")
	check.Command = filepath.Join(dir, "my-transcriber") + " --lang en"

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

func TestPosternTranscribeHasNoCure(t *testing.T) {
	check, _ := ptTools(t, "")
	if err := check.Cure(context.Background()); err == nil {
		t.Fatal("expected curing to fail: there is no cure")
	}
	if check.Name() != "postern-transcribe" {
		t.Fatalf("unexpected name %q", check.Name())
	}
}

func TestPosternTranscribeIsFaultyWhenWhisperCliIsOnlyOnTheLoginPath(t *testing.T) {
	check, dir := ptTools(t, "", "ffmpeg", "whisper-cli", "postern-transcribe")
	check.Command = filepath.Join(dir, "postern-transcribe")
	check.ServicePath = "/usr/bin:/bin"
	check.HomeDir = t.TempDir()
	check.Getenv = func(key string) string {
		if key == "POSTERN_WHISPER_MODEL" {
			return filepath.Join(dir, "ggml-base.en.bin")
		}
		return ""
	}

	verdict, reason := check.Probe(context.Background())
	if verdict != application.DoctorFaulty || !strings.Contains(reason, "service PATH") || !strings.Contains(reason, "whisper-cli") {
		t.Fatalf("expected faulty naming the service PATH, got %s (%s)", verdict, reason)
	}
}

func TestPosternTranscribeIsOKWhenWhisperCliIsInHomeLocalBin(t *testing.T) {
	check, dir := ptTools(t, "", "ffmpeg", "postern-transcribe")
	check.Command = filepath.Join(dir, "postern-transcribe")
	local := filepath.Join(dir, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "whisper-cli"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if verdict, reason := check.Probe(context.Background()); verdict != application.DoctorOK {
		t.Fatalf("expected ok, got %s (%s)", verdict, reason)
	}
}

package contrib_test

// This test drives contrib/postern-transcribe with stand-ins for ffmpeg and
// whisper-cli on the front of PATH: it never needs either installed, and
// never hears real audio. HOME is a temporary directory, so the default
// model path is one this test makes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// transcribeHome is a temporary HOME with a model at the default path, and a
// bin directory of stand-ins: an ffmpeg that writes its output file and logs
// its arguments, and a whisper-cli that writes a transcript to -of's .txt.
func transcribeHome(t *testing.T) (home, bin string) {
	t.Helper()
	home = t.TempDir()
	model := filepath.Join(home, ".local", "share", "whisper", "ggml-base.en.bin")
	if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin = filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpeg := `#!/bin/sh
echo "$*" > "$HOME/ffmpeg.args"
for last in "$@"; do :; done
printf 'RIFF' > "$last"
`
	whisper := `#!/bin/sh
echo "$*" > "$HOME/whisper.args"
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-of" ]; then out=$2; fi
  shift
done
echo "whisper chatter on stdout"
echo "loading model" >&2
printf '  Ship the storage engine\n\n   as planned.  \n' > "$out.txt"
`
	for name, body := range map[string]string{"ffmpeg": ffmpeg, "whisper-cli": whisper} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, bin
}

func runTranscribe(t *testing.T, home, bin string, args ...string) (string, string, error) {
	t.Helper()
	script, err := filepath.Abs("postern-transcribe")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script, args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":/usr/bin:/bin", "POSTERN_WHISPER_MODEL=", "POSTERN_WHISPER_CLI=", "TMPDIR="+home)
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	err = cmd.Run()
	return out.String(), errs.String(), err
}

func TestPosternTranscribePrintsPlainTextOnly(t *testing.T) {
	home, bin := transcribeHome(t)
	note := filepath.Join(home, "direct-v1.webm")
	if err := os.WriteFile(note, []byte("webm"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, errs, err := runTranscribe(t, home, bin, note)
	if err != nil {
		t.Fatalf("postern-transcribe failed: %v\n%s", err, errs)
	}
	if out != "Ship the storage engine as planned.\n" {
		t.Fatalf("expected one plain paragraph, got %q", out)
	}
	ffmpeg, _ := os.ReadFile(filepath.Join(home, "ffmpeg.args"))
	for _, want := range []string{"-i " + note, "-ar 16000", "-ac 1", "pcm_s16le"} {
		if !strings.Contains(string(ffmpeg), want) {
			t.Errorf("expected ffmpeg asked for %q, got %q", want, ffmpeg)
		}
	}
	whisper, _ := os.ReadFile(filepath.Join(home, "whisper.args"))
	if !strings.Contains(string(whisper), "-m "+filepath.Join(home, ".local", "share", "whisper", "ggml-base.en.bin")) {
		t.Errorf("expected the default model, got %q", whisper)
	}
	leftovers, _ := filepath.Glob(filepath.Join(home, "postern-transcribe.*"))
	if len(leftovers) != 0 {
		t.Errorf("expected the temporary directory removed, found %v", leftovers)
	}
}

func TestPosternTranscribeRefusesWithoutAModel(t *testing.T) {
	home, bin := transcribeHome(t)
	note := filepath.Join(home, "note.ogg")
	if err := os.WriteFile(note, []byte("ogg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".local", "share", "whisper", "ggml-base.en.bin")); err != nil {
		t.Fatal(err)
	}

	out, errs, err := runTranscribe(t, home, bin, note)
	if err == nil || out != "" || !strings.Contains(errs, "no whisper model") {
		t.Fatalf("expected a refusal naming the missing model, got %q / %q / %v", out, errs, err)
	}
}

func TestPosternTranscribeNeedsExactlyOneFile(t *testing.T) {
	home, bin := transcribeHome(t)
	if _, errs, err := runTranscribe(t, home, bin); err == nil || !strings.Contains(errs, "usage") {
		t.Fatalf("expected usage, got %q / %v", errs, err)
	}
}

// bareHome is a HOME with a model, a note and the stand-in whisper-cli at
// bin/whisper-cli; the stand-in ffmpeg is moved to a directory of its own, so
// PATH can be that directory plus the bare /usr/bin:/bin.
func bareHome(t *testing.T) (home, bin, ffbin, note string) {
	t.Helper()
	home, bin = transcribeHome(t)
	ffbin = filepath.Join(home, "ffonly")
	if err := os.MkdirAll(ffbin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(bin, "ffmpeg"), filepath.Join(ffbin, "ffmpeg")); err != nil {
		t.Fatal(err)
	}
	note = filepath.Join(home, "note.ogg")
	if err := os.WriteFile(note, []byte("ogg"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home, bin, ffbin, note
}

func TestPosternTranscribeFindsWhisperCliInHomeLocalBin(t *testing.T) {
	home, bin, ffbin, note := bareHome(t)
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(bin, "whisper-cli"), filepath.Join(local, "whisper-cli")); err != nil {
		t.Fatal(err)
	}

	out, errs, err := runTranscribe(t, home, ffbin, note)
	if err != nil {
		t.Fatalf("postern-transcribe failed: %v\n%s", err, errs)
	}
	if out != "Ship the storage engine as planned.\n" {
		t.Fatalf("expected the fake whisper-cli in ~/.local/bin to be called, got %q", out)
	}
}

func TestPosternTranscribeNamesEveryPlaceItLookedForWhisperCli(t *testing.T) {
	home, _, ffbin, note := bareHome(t)

	out, errs, err := runTranscribe(t, home, ffbin, note)
	if err == nil || out != "" {
		t.Fatalf("expected a refusal, got %q / %v", out, err)
	}
	for _, want := range []string{"PATH", filepath.Join(home, ".local", "bin"), "POSTERN_WHISPER_CLI"} {
		if !strings.Contains(errs, want) {
			t.Errorf("expected the error to name %q, got %q", want, errs)
		}
	}
}

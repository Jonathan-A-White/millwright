package scorer_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Jonathan-A-White/millwright/infrastructure/scorer"
)

func TestToWav16kConvertsAWebmClipToRIFFWave(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed: the audio conversion is not tried here")
	}
	clip, err := os.ReadFile("../../testdata/clip.webm")
	if err != nil {
		t.Fatal(err)
	}
	wav, err := scorer.ToWav16k(clip, "audio/webm")
	if err != nil {
		t.Fatalf("ToWav16k: %v", err)
	}
	if !bytes.HasPrefix(wav, []byte("RIFF")) || !bytes.Equal(wav[8:12], []byte("WAVE")) {
		t.Fatalf("expected a RIFF WAVE file, got %q", wav[:min(12, len(wav))])
	}
	// the fmt chunk: 1 channel at bytes 22-23, 16000 Hz at bytes 24-27 (little endian)
	if wav[22] != 1 || wav[23] != 0 {
		t.Errorf("expected one channel, got % x", wav[22:24])
	}
	if rate := int(wav[24]) | int(wav[25])<<8 | int(wav[26])<<16 | int(wav[27])<<24; rate != 16000 {
		t.Errorf("expected 16000 Hz, got %d", rate)
	}
}

func TestToWav16kRefusesWhenFfmpegIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := scorer.ToWav16k([]byte("anything"), "audio/webm")
	if err == nil || !strings.Contains(err.Error(), "ffmpeg is not installed") {
		t.Fatalf("expected a plain line saying ffmpeg is not installed, got %v", err)
	}
}

func TestToWav16kRefusesAudioFfmpegCannotRead(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed: the audio conversion is not tried here")
	}
	_, err := scorer.ToWav16k([]byte("this is not audio"), "audio/webm")
	if err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("expected an ffmpeg error, got %v", err)
	}
}

func TestToWav16kRefusesEmptyAudio(t *testing.T) {
	if _, err := scorer.ToWav16k(nil, "audio/webm"); err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Fatalf("expected a refusal naming no audio, got %v", err)
	}
}

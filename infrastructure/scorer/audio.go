// Package scorer holds the adapters of application.Scorer: the audio
// conversion every engine shares, and one file for each engine
// (docs/scorers.md).
package scorer

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// ToWav16k converts a recording to the 16 kHz, mono, 16-bit WAV file the
// engines listen to, with `ffmpeg -i pipe:0 -ac 1 -ar 16000 -f wav pipe:1`.
// ffmpeg works out the input's format from its bytes; mime is only named in
// the error when it cannot. It refuses with a plain line when ffmpeg is not
// installed.
func ToWav16k(audio []byte, mime string) ([]byte, error) {
	if len(audio) == 0 {
		return nil, fmt.Errorf("there is no audio to convert")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg is not installed: mw needs it to turn a recording into 16 kHz mono WAV for a scorer (docs/scorers.md)")
	}
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-ac", "1", "-ar", "16000", "-f", "wav", "pipe:1")
	cmd.Stdin = bytes.NewReader(audio)
	var wav, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &wav, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg could not convert the %s audio to 16 kHz mono WAV: %v: %s", mime, err, strings.TrimSpace(stderr.String()))
	}
	if wav.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg made no audio from the %s recording", mime)
	}
	return wav.Bytes(), nil
}

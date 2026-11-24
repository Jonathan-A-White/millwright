package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func voiceHost(t *testing.T, text string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if text == "" {
		return
	}
	path := filepath.Join(home, File)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVoiceKeepAliveWhenDefaultsToTheFirstAtNine(t *testing.T) {
	voiceHost(t, "")
	day, hour, err := VoiceKeepAliveWhen()
	if err != nil || day != 1 || hour != 21 {
		t.Errorf("got day %d hour %d err %v", day, hour, err)
	}
}

func TestVoiceKeepAliveWhenReadsTheTable(t *testing.T) {
	voiceHost(t, "[voice_keepalive]\nday = 15\nhour = 0\n")
	day, hour, err := VoiceKeepAliveWhen()
	if err != nil || day != 15 || hour != 0 {
		t.Errorf("got day %d hour %d err %v", day, hour, err)
	}
}

func TestVoiceKeepAliveWhenRefusesADayOrHourOutOfRange(t *testing.T) {
	for _, text := range []string{"[voice_keepalive]\nday = 0\n", "[voice_keepalive]\nday = 29\n", "[voice_keepalive]\nhour = 24\n", "[voice_keepalive]\nhour = nine\n"} {
		voiceHost(t, text)
		if _, _, err := VoiceKeepAliveWhen(); err == nil || !strings.Contains(err.Error(), "voice_keepalive") {
			t.Errorf("%q: want an error naming the table, got %v", text, err)
		}
	}
}
